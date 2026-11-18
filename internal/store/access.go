package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"sort"
	"strings"
)

// Access model (migrations 0006, 0017). A rule gives a user or a group a preset and/or per-permission
// overrides (allow or deny) in one project, in one workspace, or in all workspaces (workspace_id
// NULL, which covers future ones too). For one user, one place and one permission the first rule
// with an opinion decides, most specific first:
//
//	admin (everything) > the user's rule for this project > the user's rule for this workspace >
//	the user's rule for all workspaces > the groups' rules for this project > the groups' rules for
//	this workspace > the groups' rules for all workspaces > off.
//
// Project rules hold only the gig-level permissions (ProjectPermissions): what a gig in that project
// allows. Everything else (errands, templates, settings...) is decided per workspace, and needs
// "view" of the whole workspace: someone whose only rules there are project rules sees that
// workspace with just those projects in it.
//
// Inside one rule an override beats the preset; a permission the preset doesn't include has no
// opinion (inherit), it is not a deny. Between groups on the same level any allow beats a deny.
// Every permission needs "view" at the same place.

// Permission is one thing a user may do in a workspace.
type Permission struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Section     string `json:"section"`
	Project     bool   `json:"project"` // can be given per project (see ProjectPermissions)
}

// Permissions, in display order.
var Permissions = []Permission{
	{"view", "See the workspace", "See the workspace, its projects, gigs, comments and runs. Every other permission needs it.", "Basics", false},
	{"comment", "Comment", "Write comments and edit or delete your own.", "Basics", false},
	{"work_gigs", "Work on gigs", "Move gigs between statuses of their project, tick checklist items, ask for a resume; claim gigs assigned to you and report runs (agents).", "Gigs", false},
	{"edit_gigs", "Edit gigs", "Change and assign gigs: title, description, dates, labels, run settings, checklist items and dependencies; move them to another project.", "Gigs", false},
	{"create_gigs", "Create gigs", "Create one-off gigs, clone gigs, and create gigs from templates (with Edit gigs).", "Gigs", false},
	{"delete_gigs", "Delete gigs", "Move gigs to the trash and restore them (with Edit gigs).", "Gigs", false},
	{"archive_gigs", "Archive gigs", "Archive and unarchive gigs (with Edit gigs).", "Gigs", false},
	{"always_run", "Always-run gigs", "Mark gigs to run whatever the usage budget (cron-like gigs). The gig cap and pauses still apply.", "Runner", false},
	{"run_timeout", "Run timeouts", "Set how long an agent may run on a gig (templates and recurring gigs too) or how long an errand may run, instead of the default. The agent's host caps it.", "Runner", false},
	{"allowed_tools", "Gig tool allowlists", "Give gigs (templates, recurring gigs) the list of tools the agent's runner may use for them (for Claude Code: --allowedTools), and change gigs that have one. Whatever limits the agent's own setup imposes (settings, sandbox) still apply.", "Runner", false},
	{"errands", "Errands", "Create, change, pause and delete errands (commands an agent's executor runs on a schedule, sandboxed); in a whole workspace, its errand templates too. Given for one project: that project's errands, and changing one's command never changes other errands' template. An errand's worker is yourself, or any agent with Manage the workspace.", "Runner", false},
	{"errand_unsandboxed", "No-sandbox errands", "Let an errand run outside its agent's sandbox. Any change to such an errand by someone without this permission turns its sandbox back on.", "Runner", false},
	{"pause_runs", "Pause gig runs", "Pause and resume new agent gig runs and errand runs; with an all-workspaces rule, every workspace at once. Changing the caps stays with admins.", "Runner", false},
	{"recurring_gigs", "Recurring gigs", "Create, change, pause and stop recurring gigs (with Edit gigs; they make gigs with Create gigs).", "Planning", false},
	{"templates", "Gig templates", "Create, change and delete gig templates, with Edit gigs (using one only needs Create gigs).", "Planning", false},
	{"custom_fields", "Custom fields", "Define the workspace's custom fields.", "Planning", false},
	{"saved_filters", "Saved filters", "Save personal filtered views.", "Planning", false},
	{"manage_workspace", "Manage the workspace", "Projects, columns, sections and labels, and other people's comments.", "Admin", false},
}

// ProjectPermissions are the permissions a project rule can hold: what can be done to the gigs and
// errands of one project. The others are about the workspace as a whole.
var ProjectPermissions = []string{"view", "comment", "work_gigs", "edit_gigs", "create_gigs", "delete_gigs", "archive_gigs", "errands"}

// ProjectRoles are the presets a project rule can use (manager is about the workspace).
var ProjectRoles = []string{"viewer", "consumer", "member"}

// IsProjectPermission reports a permission that can be decided per project.
func IsProjectPermission(name string) bool { return slices.Contains(ProjectPermissions, name) }

func init() {
	for i := range Permissions {
		Permissions[i].Project = IsProjectPermission(Permissions[i].Name)
	}
}

// Presets fill in permissions; roles are presets. Each one includes the one before it:
//   - viewer sees, and changes nothing;
//   - consumer works on what it's given: comments, status moves, ticks, claims and runs;
//   - member also makes and shapes gigs;
//   - manager also runs the workspace (planning, errands, runner settings). No-sandbox errands stay
//     a grant of their own, and admins are above every preset.
var Presets = map[string][]string{
	"viewer":   {"view"},
	"consumer": {"view", "comment", "work_gigs"},
	"member":   {"view", "comment", "work_gigs", "edit_gigs", "create_gigs", "delete_gigs", "archive_gigs", "saved_filters"},
	"manager": {"view", "comment", "work_gigs", "edit_gigs", "create_gigs", "delete_gigs", "archive_gigs", "saved_filters",
		"recurring_gigs", "templates", "custom_fields", "manage_workspace", "errands", "pause_runs", "always_run", "run_timeout", "allowed_tools"},
}

// Roles (presets) in increasing order of power.
var Roles = []string{"viewer", "consumer", "member", "manager"}

// ValidRole reports a preset name.
func ValidRole(r string) bool { return slices.Contains(Roles, r) }

// ValidPermission reports a known permission.
func ValidPermission(name string) bool {
	return slices.ContainsFunc(Permissions, func(p Permission) bool { return p.Name == name })
}

// AccessRule is one row of access_rules.
type AccessRule struct {
	ID          int64           `json:"id"`
	UserID      *int64          `json:"user_id,omitempty"`
	GroupID     *int64          `json:"group_id,omitempty"`
	GroupName   string          `json:"group_name,omitempty"`
	WorkspaceID *int64          `json:"workspace_id"`         // nil: all workspaces, future ones included
	ProjectID   *int64          `json:"project_id,omitempty"` // set: one project of WorkspaceID only
	Preset      string          `json:"preset"`               // "" = none
	Overrides   map[string]bool `json:"overrides"`            // permission -> allow/deny; absent = the preset decides
	CreatedAt   int64           `json:"created_at"`
}

// opinion: the rule's say on one permission.
func (r *AccessRule) opinion(perm string) (allowed, has bool) {
	if v, ok := r.Overrides[perm]; ok {
		return v, true
	}
	if slices.Contains(Presets[r.Preset], perm) {
		return true, true
	}
	return false, false
}

const ruleCols = `r.id, r.user_id, r.group_id, COALESCE(g.name, ''), r.workspace_id, r.project_id, r.preset, r.overrides, r.created_at`

func scanRules(rows *sql.Rows) ([]*AccessRule, error) {
	defer rows.Close()
	out := []*AccessRule{}
	for rows.Next() {
		r := &AccessRule{}
		var ov string
		if err := rows.Scan(&r.ID, &r.UserID, &r.GroupID, &r.GroupName, &r.WorkspaceID, &r.ProjectID, &r.Preset, &ov, &r.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(ov), &r.Overrides); err != nil || r.Overrides == nil {
			r.Overrides = map[string]bool{}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RulesOfUser lists a user's own rules.
func RulesOfUser(ctx context.Context, q Q, user int64) ([]*AccessRule, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+ruleCols+` FROM access_rules r LEFT JOIN groups g ON g.id = r.group_id
		WHERE r.user_id = ? ORDER BY r.workspace_id IS NOT NULL, r.workspace_id, r.project_id IS NOT NULL, r.project_id`, user)
	if err != nil {
		return nil, err
	}
	return scanRules(rows)
}

// RulesOfGroup lists a group's rules.
func RulesOfGroup(ctx context.Context, q Q, group int64) ([]*AccessRule, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+ruleCols+` FROM access_rules r LEFT JOIN groups g ON g.id = r.group_id
		WHERE r.group_id = ? ORDER BY r.workspace_id IS NOT NULL, r.workspace_id, r.project_id IS NOT NULL, r.project_id`, group)
	if err != nil {
		return nil, err
	}
	return scanRules(rows)
}

// Decision explains one permission: the answer and the rule level that gave it.
type Decision struct {
	Name    string `json:"name"`
	Allowed bool   `json:"allowed"`
	Source  string `json:"source"`          // admin | user_project | user | user_all | group_project | group | group_all | needs_view | default
	Group   string `json:"group,omitempty"` // the deciding group
}

// Grants is everything needed to answer "may this user do X in workspace W (project P)".
type Grants struct {
	Admin      bool
	userWS     map[int64]*AccessRule
	userAll    *AccessRule
	userProj   map[int64]*AccessRule
	groupWS    map[int64][]*AccessRule
	groupAll   []*AccessRule
	groupProj  map[int64][]*AccessRule
	workspaces []int64           // every workspace id
	projects   map[int64][]int64 // live projects per workspace, in sidebar order
	projectWS  map[int64]int64   // every project (deleted too) -> its workspace
}

func newGrants(admin bool) *Grants {
	return &Grants{Admin: admin, userWS: map[int64]*AccessRule{}, groupWS: map[int64][]*AccessRule{},
		userProj: map[int64]*AccessRule{}, groupProj: map[int64][]*AccessRule{}, projects: map[int64][]int64{}, projectWS: map[int64]int64{}}
}

func (g *Grants) add(r *AccessRule) {
	if r.Overrides == nil {
		r.Overrides = map[string]bool{}
	}
	switch {
	case r.UserID != nil && r.ProjectID != nil:
		g.userProj[*r.ProjectID] = r
	case r.UserID != nil && r.WorkspaceID == nil:
		g.userAll = r
	case r.UserID != nil:
		g.userWS[*r.WorkspaceID] = r
	case r.ProjectID != nil:
		g.groupProj[*r.ProjectID] = append(g.groupProj[*r.ProjectID], r)
	case r.WorkspaceID == nil:
		g.groupAll = append(g.groupAll, r)
	default:
		g.groupWS[*r.WorkspaceID] = append(g.groupWS[*r.WorkspaceID], r)
	}
}

// LoadGrants reads a user's rules, their groups' rules and the workspace list.
func LoadGrants(ctx context.Context, q Q, u *User) (*Grants, error) {
	g := newGrants(u.IsAdmin)
	rows, err := q.QueryContext(ctx, `SELECT `+ruleCols+` FROM access_rules r LEFT JOIN groups g ON g.id = r.group_id
		WHERE r.user_id = ? OR r.group_id IN (SELECT group_id FROM group_members WHERE user_id = ?) ORDER BY g.name, r.id`, u.ID, u.ID)
	if err != nil {
		return nil, err
	}
	rules, err := scanRules(rows)
	if err != nil {
		return nil, err
	}
	for _, r := range rules {
		g.add(r)
	}
	rows, err = q.QueryContext(ctx, `SELECT id FROM workspaces ORDER BY id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		g.workspaces = append(g.workspaces, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = q.QueryContext(ctx, `SELECT id, workspace_id, deleted_at IS NOT NULL FROM projects ORDER BY workspace_id, position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, ws int64
		var deleted bool
		if err := rows.Scan(&id, &ws, &deleted); err != nil {
			return nil, err
		}
		g.projectWS[id] = ws
		if !deleted {
			g.projects[ws] = append(g.projects[ws], id)
		}
	}
	return g, rows.Err()
}

func groupOpinion(rules []*AccessRule, perm string) (d Decision, has bool) {
	for _, r := range rules {
		if v, ok := r.opinion(perm); ok {
			if v {
				return Decision{Allowed: true, Group: r.GroupName}, true
			}
			if !has {
				d, has = Decision{Allowed: false, Group: r.GroupName}, true
			}
		}
	}
	return d, has
}

// raw decides without the view dependency. ws 0 asks about the all-workspaces level only; project
// 0 about the workspace as a whole (project rules are skipped, and so are they for permissions
// that aren't per project).
func (g *Grants) raw(ws, project int64, perm string) Decision {
	if g.Admin {
		return Decision{Name: perm, Allowed: true, Source: "admin"}
	}
	// a project's rules count only for that project's own workspace (a caller passing a project of
	// another workspace gets the workspace's answer, never that project's rules)
	if pw, ok := g.projectWS[project]; project != 0 && (!ok || pw != ws) {
		project = 0
	}
	inProject := project != 0 && IsProjectPermission(perm)
	if r := g.userProj[project]; inProject && r != nil {
		if v, ok := r.opinion(perm); ok {
			return Decision{Name: perm, Allowed: v, Source: "user_project"}
		}
	}
	if r := g.userWS[ws]; r != nil {
		if v, ok := r.opinion(perm); ok {
			return Decision{Name: perm, Allowed: v, Source: "user"}
		}
	}
	if g.userAll != nil {
		if v, ok := g.userAll.opinion(perm); ok {
			return Decision{Name: perm, Allowed: v, Source: "user_all"}
		}
	}
	if inProject {
		if d, ok := groupOpinion(g.groupProj[project], perm); ok {
			d.Name, d.Source = perm, "group_project"
			return d
		}
	}
	if d, ok := groupOpinion(g.groupWS[ws], perm); ok {
		d.Name, d.Source = perm, "group"
		return d
	}
	if d, ok := groupOpinion(g.groupAll, perm); ok {
		d.Name, d.Source = perm, "group_all"
		return d
	}
	return Decision{Name: perm, Source: "default"}
}

// Decide answers one permission for the workspace as a whole, with its source.
func (g *Grants) Decide(ws int64, perm string) Decision { return g.DecideIn(ws, 0, perm) }

// DecideIn answers one permission in one project of a workspace (project 0: the workspace as a
// whole), with its source.
func (g *Grants) DecideIn(ws, project int64, perm string) Decision {
	d := g.raw(ws, project, perm)
	if perm != "view" && d.Allowed && !g.raw(ws, project, "view").Allowed {
		return Decision{Name: perm, Source: "needs_view"}
	}
	return d
}

// Has reports one permission for the workspace as a whole.
func (g *Grants) Has(ws int64, perm string) bool { return g.DecideIn(ws, 0, perm).Allowed }

// HasIn reports one permission in one project of a workspace.
func (g *Grants) HasIn(ws, project int64, perm string) bool {
	return g.DecideIn(ws, project, perm).Allowed
}

// ProjectsWith lists the live projects of ws where perm is held, in sidebar order.
func (g *Grants) ProjectsWith(ws int64, perm string) []int64 {
	out := []int64{}
	for _, p := range g.projects[ws] {
		if g.HasIn(ws, p, perm) {
			out = append(out, p)
		}
	}
	return out
}

// ProjectFilter is the project narrowing of ws for reading: nil when every project can be seen
// (whole-workspace view, and no project rule hides one), otherwise the projects that can.
func (g *Grants) ProjectFilter(ws int64) []int64 {
	seen := g.ProjectsWith(ws, "view")
	if g.Has(ws, "view") && len(seen) == len(g.projects[ws]) {
		return nil
	}
	return seen
}

// Sees reports whether a workspace exists for the user: whole, or at least one of its projects.
func (g *Grants) Sees(ws int64) bool {
	return g.Has(ws, "view") || len(g.ProjectsWith(ws, "view")) > 0
}

// Anywhere reports a permission held in at least one workspace (or one project, for gig-level
// permissions).
func (g *Grants) Anywhere(perm string) bool {
	if g.Admin {
		return true
	}
	for _, ws := range g.workspaces {
		if g.Has(ws, perm) || (IsProjectPermission(perm) && len(g.ProjectsWith(ws, perm)) > 0) {
			return true
		}
	}
	return false
}

// Visible lists the workspaces the user can see, whole or in part.
func (g *Grants) Visible() []int64 {
	out := []int64{}
	for _, ws := range g.workspaces {
		if g.Sees(ws) {
			out = append(out, ws)
		}
	}
	return out
}

// Set lists the permissions held for a workspace as a whole.
func (g *Grants) Set(ws int64) []string { return g.SetIn(ws, 0) }

// SetIn lists the permissions held in one project (project 0: the workspace as a whole).
func (g *Grants) SetIn(ws, project int64) []string {
	out := []string{}
	for _, p := range Permissions {
		if project != 0 && !p.Project {
			continue
		}
		if g.HasIn(ws, project, p.Name) {
			out = append(out, p.Name)
		}
	}
	return out
}

// RoleLabel names the closest preset a permission set reaches (for display only).
func RoleLabel(admin bool, perms []string) string {
	switch {
	case admin:
		return "admin"
	case slices.Contains(perms, "manage_workspace"):
		return "manager"
	case slices.Contains(perms, "edit_gigs"):
		return "member"
	case slices.Contains(perms, "work_gigs"):
		return "consumer"
	case slices.Contains(perms, "view"):
		return "viewer"
	}
	return ""
}

// UserHas resolves one permission for the workspace as a whole, for background work (scheduler,
// notifications): the user must exist and be active.
func UserHas(ctx context.Context, q Q, userID, ws int64, perm string) bool {
	return UserHasIn(ctx, q, userID, ws, 0, perm)
}

// UserHasIn resolves one permission in one project (assignees, watchers, notifications about a
// gig): the user must exist and be active.
func UserHasIn(ctx context.Context, q Q, userID, ws, project int64, perm string) bool {
	u, err := GetUser(ctx, q, userID)
	if err != nil || u.Disabled {
		return false
	}
	g, err := LoadGrants(ctx, q, u)
	return err == nil && g.HasIn(ws, project, perm)
}

// UserSees reports whether an active user sees a place: a project (view there), or a workspace
// (whole, or some of its projects).
func UserSees(ctx context.Context, q Q, userID, ws int64, project *int64) bool {
	u, g, err := UserGrants(ctx, q, userID)
	if err != nil || u.Disabled {
		return false
	}
	if project != nil {
		return g.HasIn(ws, *project, "view")
	}
	return g.Sees(ws)
}

// UserGrants loads the grants of a user by id.
func UserGrants(ctx context.Context, q Q, userID int64) (*User, *Grants, error) {
	u, err := GetUser(ctx, q, userID)
	if err != nil {
		return nil, nil, err
	}
	g, err := LoadGrants(ctx, q, u)
	return u, g, err
}

// AccessRow is one workspace (or the all-workspaces level, WorkspaceID 0), or one project of a
// workspace, of a user's resolved grid.
type AccessRow struct {
	WorkspaceID int64      `json:"workspace_id"`         // 0 = all workspaces (what a new workspace starts with)
	ProjectID   int64      `json:"project_id,omitempty"` // a project with a rule of its own (the user's or a group's)
	Role        string     `json:"role"`                 // closest preset, display only
	Permissions []Decision `json:"permissions"`          // a project row lists the project permissions only
}

// AccessGrid resolves every permission in every workspace, plus the all-workspaces level, and the
// project permissions of every project a rule names, right after its workspace.
func (g *Grants) AccessGrid() []AccessRow {
	out := make([]AccessRow, 0, len(g.workspaces)+1)
	row := func(ws, project int64) AccessRow {
		r := AccessRow{WorkspaceID: ws, ProjectID: project, Permissions: []Decision{}}
		set := []string{}
		for _, p := range Permissions {
			if project != 0 && !p.Project {
				continue
			}
			d := g.DecideIn(ws, project, p.Name)
			r.Permissions = append(r.Permissions, d)
			if d.Allowed {
				set = append(set, p.Name)
			}
		}
		r.Role = RoleLabel(g.Admin, set)
		return r
	}
	for _, ws := range append([]int64{0}, g.workspaces...) {
		out = append(out, row(ws, 0))
		for _, p := range g.projects[ws] {
			if g.userProj[p] != nil || len(g.groupProj[p]) > 0 {
				out = append(out, row(ws, p))
			}
		}
	}
	return out
}

// Membership is a user who can see a workspace, whole or some of its projects.
type Membership struct {
	WorkspaceID int64               `json:"workspace_id"`
	UserID      int64               `json:"user_id"`
	Role        string              `json:"role"`             // closest preset of what they can do in the whole workspace ("" = only some projects)
	Preset      string              `json:"preset,omitempty"` // their own preset in this workspace ("" = none: access comes from elsewhere); admins only
	Source      string              `json:"source,omitempty"` // where "view" comes from: user | user_all | group | group_all; admins only
	Whole       bool                `json:"whole"`            // sees the whole workspace (else only Projects)
	Projects    []ProjectMembership `json:"projects"`         // projects with a rule of their own, or (not Whole) the ones they see
}

// ProjectMembership is what a member can do in one project, when it differs from the workspace or
// is all they have there.
type ProjectMembership struct {
	ProjectID int64  `json:"project_id"`
	Role      string `json:"role"`             // closest preset of what they can do in it ("" = can't see it)
	Preset    string `json:"preset,omitempty"` // their own preset for this project; admins only
	Source    string `json:"source,omitempty"` // where "view" comes from; admins only
}

// Members lists every active user who can see a workspace (admins excluded unless they have a rule).
func Members(ctx context.Context, q Q, ws int64) ([]Membership, error) {
	users, err := ListUsers(ctx, q, nil)
	if err != nil {
		return nil, err
	}
	sort.Slice(users, func(i, j int) bool { return users[i].Username < users[j].Username })
	out := []Membership{}
	for _, u := range users {
		if u.Disabled {
			continue
		}
		g, err := LoadGrants(ctx, q, u)
		if err != nil {
			return nil, err
		}
		own := g.userWS[ws]
		ownProject := false
		for _, p := range g.projects[ws] {
			ownProject = ownProject || g.userProj[p] != nil
		}
		if u.IsAdmin && own == nil && !ownProject {
			continue
		}
		g.Admin = false // list what the rules give; admins see everything anyway
		if !g.Sees(ws) {
			continue
		}
		d := g.Decide(ws, "view")
		m := Membership{WorkspaceID: ws, UserID: u.ID, Whole: d.Allowed, Projects: []ProjectMembership{}}
		if d.Allowed {
			m.Role, m.Source = RoleLabel(false, g.Set(ws)), d.Source
		}
		if own != nil {
			m.Preset = own.Preset
		}
		for _, p := range g.projects[ws] {
			r := g.userProj[p]
			if r == nil && len(g.groupProj[p]) == 0 && d.Allowed {
				continue // same as the workspace
			}
			pd := g.DecideIn(ws, p, "view")
			if !pd.Allowed && r == nil && !d.Allowed {
				continue // not theirs, nothing to say
			}
			pm := ProjectMembership{ProjectID: p}
			if pd.Allowed {
				pm.Role, pm.Source = RoleLabel(false, g.SetIn(ws, p)), pd.Source
			}
			if r != nil {
				pm.Preset = r.Preset
			}
			m.Projects = append(m.Projects, pm)
		}
		out = append(out, m)
	}
	return out, nil
}

// UsersWith lists active users holding perm in ws as a whole (admins included).
func UsersWith(ctx context.Context, q Q, ws int64, perm string) ([]int64, error) {
	return UsersWithIn(ctx, q, ws, 0, perm)
}

// UsersWithIn lists active users holding perm in one project of ws (admins included).
func UsersWithIn(ctx context.Context, q Q, ws, project int64, perm string) ([]int64, error) {
	users, err := ListUsers(ctx, q, nil)
	if err != nil {
		return nil, err
	}
	out := []int64{}
	for _, u := range users {
		if u.Disabled {
			continue
		}
		g, err := LoadGrants(ctx, q, u)
		if err != nil {
			return nil, err
		}
		if g.HasIn(ws, project, perm) {
			out = append(out, u.ID)
		}
	}
	return out, nil
}

// SetRule writes (or, with no preset and no overrides, removes) the rule of a user or a group at
// one level. subject is "user" or "group"; ws nil = all workspaces; project set = one project of ws
// (project permissions and the viewer, consumer and member presets only).
func SetRule(ctx context.Context, q Q, a Actor, subject string, id int64, ws, project *int64, preset string, overrides map[string]bool) error {
	col := map[string]string{"user": "user_id", "group": "group_id"}[subject]
	if col == "" {
		return Invalid("subject is user or group")
	}
	if preset != "" && !ValidRole(preset) {
		return Invalid("preset is viewer, consumer, member, manager or empty")
	}
	for name := range overrides {
		if !ValidPermission(name) {
			return Invalid("unknown permission %q", name)
		}
	}
	if project != nil {
		if ws == nil {
			return Invalid("a project rule needs its workspace")
		}
		var n int
		if err := q.QueryRowContext(ctx, `SELECT count(*) FROM projects WHERE id = ? AND workspace_id = ? AND deleted_at IS NULL`, *project, *ws).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return Invalid("no such project in that workspace")
		}
		if preset != "" && !slices.Contains(ProjectRoles, preset) {
			return Invalid("a project rule's preset is viewer, consumer, member or empty")
		}
		for name := range overrides {
			if !IsProjectPermission(name) {
				return Invalid("%q is about the whole workspace; a project rule can't hold it", name)
			}
		}
	}
	if overrides == nil {
		overrides = map[string]bool{}
	}
	var before int
	_ = q.QueryRowContext(ctx, `SELECT count(*) FROM access_rules WHERE `+col+` = ? AND workspace_id IS ? AND project_id IS ?`,
		id, nullInt(ws), nullInt(project)).Scan(&before)
	if _, err := q.ExecContext(ctx, `DELETE FROM access_rules WHERE `+col+` = ? AND workspace_id IS ? AND project_id IS ?`,
		id, nullInt(ws), nullInt(project)); err != nil {
		return err
	}
	removed := preset == "" && len(overrides) == 0
	if !removed {
		ov, _ := json.Marshal(overrides)
		if _, err := q.ExecContext(ctx, `INSERT INTO access_rules (`+col+`, workspace_id, project_id, preset, overrides, created_at) VALUES (?,?,?,?,?,?)`,
			id, nullInt(ws), nullInt(project), preset, string(ov), Now()); err != nil {
			if isFK(err) {
				return Invalid("no such %s or workspace", subject)
			}
			return err
		}
	}
	if removed && before == 0 {
		return nil
	}
	if err := dropUnseenAssignees(ctx, q); err != nil {
		return err
	}
	_, err := Activity(ctx, q, a, ws, project, subject+".access", subject, id, map[string]any{"workspace_id": ws, "project_id": project,
		"preset": preset, "overrides": overrides})
	return err
}

// SetMembership sets a user's preset in one workspace, keeping their overrides there; role ""
// removes their own rule for the workspace (access from groups or all-workspace rules remains).
func SetMembership(ctx context.Context, q Q, a Actor, ws, user int64, role string) error {
	if role != "" && !ValidRole(role) {
		return Invalid("role is viewer, consumer, member or manager")
	}
	ov := map[string]bool{}
	if role != "" {
		rules, err := RulesOfUser(ctx, q, user)
		if err != nil {
			return err
		}
		for _, r := range rules {
			if r.WorkspaceID != nil && *r.WorkspaceID == ws && r.ProjectID == nil {
				ov = r.Overrides
			}
		}
	}
	return SetRule(ctx, q, a, "user", user, &ws, nil, role, ov)
}

// SetProjectMembership sets a user's preset in one project, keeping their overrides there; role ""
// removes their own rule for the project (what the workspace's rules give remains).
func SetProjectMembership(ctx context.Context, q Q, a Actor, ws, project, user int64, role string) error {
	if role != "" && !slices.Contains(ProjectRoles, role) {
		return Invalid("a project role is viewer, consumer or member")
	}
	ov := map[string]bool{}
	if role != "" {
		rules, err := RulesOfUser(ctx, q, user)
		if err != nil {
			return err
		}
		for _, r := range rules {
			if r.ProjectID != nil && *r.ProjectID == project {
				ov = r.Overrides
			}
		}
	}
	return SetRule(ctx, q, a, "user", user, &ws, &project, role, ov)
}

// dropUnseenAssignees removes assignees who can no longer see their gig's project.
func dropUnseenAssignees(ctx context.Context, q Q) error {
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT a.user_id, t.workspace_id, t.project_id FROM gig_assignees a JOIN gigs t ON t.id = a.gig_id`)
	if err != nil {
		return err
	}
	type place struct{ user, ws, project int64 }
	var places []place
	for rows.Next() {
		var p place
		if err := rows.Scan(&p.user, &p.ws, &p.project); err != nil {
			rows.Close()
			return err
		}
		places = append(places, p)
	}
	rows.Close()
	grants := map[int64]*Grants{}
	for _, p := range places {
		g, ok := grants[p.user]
		if !ok {
			if _, g, err = UserGrants(ctx, q, p.user); err != nil {
				g = nil
			}
			grants[p.user] = g
		}
		if g != nil && g.HasIn(p.ws, p.project, "view") {
			continue
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM gig_assignees WHERE user_id = ? AND gig_id IN (SELECT id FROM gigs WHERE project_id = ?)`, p.user, p.project); err != nil {
			return err
		}
	}
	return nil
}

func isFK(err error) bool {
	return err != nil && strings.Contains(err.Error(), "FOREIGN KEY constraint failed")
}

// NewGrants builds grants from rules in memory (tests, previews). User rules need UserID set (any
// value), group rules GroupID. projects lists each workspace's projects (for project rules).
func NewGrants(admin bool, rules []*AccessRule, workspaces []int64, projects map[int64][]int64) *Grants {
	g := newGrants(admin)
	g.workspaces = workspaces
	for ws, ps := range projects {
		g.projects[ws] = ps
	}
	for _, r := range rules {
		if r.UserID == nil && r.GroupID == nil {
			r.UserID = new(int64)
		}
		g.add(r)
	}
	return g
}
