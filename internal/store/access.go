package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"sort"
	"strings"
)

// Access model (migration 0006). A rule gives a user or a group a preset and/or per-permission
// overrides (allow or deny) in one workspace or in all workspaces (workspace_id NULL, which covers
// future ones too). For one user, one workspace and one permission the first rule with an opinion
// decides, most specific first:
//
//	admin (everything) > the user's rule for this workspace > the user's rule for all workspaces >
//	the groups' rules for this workspace > the groups' rules for all workspaces > off.
//
// Inside one rule an override beats the preset; a permission the preset doesn't include has no
// opinion (inherit), it is not a deny. Between groups on the same level any allow beats a deny.
// Every permission needs "view" in the same workspace.

// Permission is one thing a user may do in a workspace.
type Permission struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Section     string `json:"section"`
}

// Permissions, in display order.
var Permissions = []Permission{
	{"view", "See the workspace", "See the workspace, its projects, tasks, comments and runs. Every other permission needs it.", "Basics"},
	{"comment", "Comment", "Write comments and edit or delete your own.", "Basics"},
	{"edit_tasks", "Edit tasks", "Change, move and assign tasks; claim tasks and report runs (agents).", "Tasks"},
	{"create_tasks", "Create tasks", "Create one-off tasks, clone tasks, and create tasks from templates (with Edit tasks).", "Tasks"},
	{"delete_tasks", "Delete tasks", "Move tasks to the trash and restore them.", "Tasks"},
	{"archive_tasks", "Archive tasks", "Archive and unarchive tasks.", "Tasks"},
	{"relations", "Dependencies", "Add and remove blocking/related links between tasks.", "Tasks"},
	{"checklists", "Checklists", "Add, tick and remove checklist items.", "Tasks"},
	{"always_run", "Always-run tasks", "Mark tasks to run whatever the usage budget (cron-like tasks). The task cap and pauses still apply.", "Runner"},
	{"allowed_tools", "Task tool allowlists", "Give tasks (templates, recurring tasks) the list of tools the agent's runner may use for them (claude -p --allowedTools), and change tasks that have one. Whatever limits the agent's own setup imposes (settings, sandbox) still apply.", "Runner"},
	{"pause_runs", "Pause task runs", "Pause and resume new agent task runs (PAUSE TASKS); with an all-workspaces rule, every workspace at once. Changing the caps stays with admins.", "Runner"},
	{"schedules", "Recurring tasks", "Create, change, pause and stop recurring task schedules.", "Planning"},
	{"templates", "Task templates", "Create, change and delete task templates (using one only needs Create tasks).", "Planning"},
	{"custom_fields", "Custom fields", "Define the workspace's custom fields.", "Planning"},
	{"saved_filters", "Saved filters", "Save personal filtered views.", "Planning"},
	{"manage_workspace", "Manage the workspace", "Projects, columns, sections and labels, and other people's comments.", "Admin"},
}

// Presets fill in permissions; roles are presets.
var Presets = map[string][]string{
	"viewer":    {"view"},
	"commenter": {"view", "comment"},
	"member":    {"view", "comment", "edit_tasks", "create_tasks", "delete_tasks", "archive_tasks", "relations", "checklists", "saved_filters"},
	"manager": {"view", "comment", "edit_tasks", "create_tasks", "delete_tasks", "archive_tasks", "relations", "checklists", "saved_filters",
		"schedules", "templates", "custom_fields", "manage_workspace"},
}

// Roles (presets) in increasing order of power.
var Roles = []string{"viewer", "commenter", "member", "manager"}

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
	WorkspaceID *int64          `json:"workspace_id"` // nil: all workspaces, future ones included
	Preset      string          `json:"preset"`       // "" = none
	Overrides   map[string]bool `json:"overrides"`    // permission -> allow/deny; absent = the preset decides
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

const ruleCols = `r.id, r.user_id, r.group_id, COALESCE(g.name, ''), r.workspace_id, r.preset, r.overrides, r.created_at`

func scanRules(rows *sql.Rows) ([]*AccessRule, error) {
	defer rows.Close()
	out := []*AccessRule{}
	for rows.Next() {
		r := &AccessRule{}
		var ov string
		if err := rows.Scan(&r.ID, &r.UserID, &r.GroupID, &r.GroupName, &r.WorkspaceID, &r.Preset, &ov, &r.CreatedAt); err != nil {
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
		WHERE r.user_id = ? ORDER BY r.workspace_id IS NOT NULL, r.workspace_id`, user)
	if err != nil {
		return nil, err
	}
	return scanRules(rows)
}

// RulesOfGroup lists a group's rules.
func RulesOfGroup(ctx context.Context, q Q, group int64) ([]*AccessRule, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+ruleCols+` FROM access_rules r LEFT JOIN groups g ON g.id = r.group_id
		WHERE r.group_id = ? ORDER BY r.workspace_id IS NOT NULL, r.workspace_id`, group)
	if err != nil {
		return nil, err
	}
	return scanRules(rows)
}

// Decision explains one permission: the answer and the rule level that gave it.
type Decision struct {
	Name    string `json:"name"`
	Allowed bool   `json:"allowed"`
	Source  string `json:"source"`          // admin | user | user_all | group | group_all | needs_view | default
	Group   string `json:"group,omitempty"` // the deciding group
}

// Grants is everything needed to answer "may this user do X in workspace W".
type Grants struct {
	Admin      bool
	userWS     map[int64]*AccessRule
	userAll    *AccessRule
	groupWS    map[int64][]*AccessRule
	groupAll   []*AccessRule
	workspaces []int64 // every workspace id
}

// LoadGrants reads a user's rules, their groups' rules and the workspace list.
func LoadGrants(ctx context.Context, q Q, u *User) (*Grants, error) {
	g := &Grants{Admin: u.IsAdmin, userWS: map[int64]*AccessRule{}, groupWS: map[int64][]*AccessRule{}}
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
		switch {
		case r.UserID != nil && r.WorkspaceID == nil:
			g.userAll = r
		case r.UserID != nil:
			g.userWS[*r.WorkspaceID] = r
		case r.WorkspaceID == nil:
			g.groupAll = append(g.groupAll, r)
		default:
			g.groupWS[*r.WorkspaceID] = append(g.groupWS[*r.WorkspaceID], r)
		}
	}
	rows, err = q.QueryContext(ctx, `SELECT id FROM workspaces ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		g.workspaces = append(g.workspaces, id)
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

// raw decides without the view dependency. ws 0 asks about the all-workspaces level only.
func (g *Grants) raw(ws int64, perm string) Decision {
	if g.Admin {
		return Decision{Name: perm, Allowed: true, Source: "admin"}
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

// Decide answers one permission in one workspace, with its source.
func (g *Grants) Decide(ws int64, perm string) Decision {
	d := g.raw(ws, perm)
	if perm != "view" && d.Allowed && !g.raw(ws, "view").Allowed {
		return Decision{Name: perm, Source: "needs_view"}
	}
	return d
}

// Has reports one permission in one workspace.
func (g *Grants) Has(ws int64, perm string) bool { return g.Decide(ws, perm).Allowed }

// Anywhere reports a permission held in at least one workspace.
func (g *Grants) Anywhere(perm string) bool {
	if g.Admin {
		return true
	}
	for _, ws := range g.workspaces {
		if g.Has(ws, perm) {
			return true
		}
	}
	return false
}

// Visible lists the workspaces the user can see.
func (g *Grants) Visible() []int64 {
	out := []int64{}
	for _, ws := range g.workspaces {
		if g.Has(ws, "view") {
			out = append(out, ws)
		}
	}
	return out
}

// Set lists the permissions held in a workspace.
func (g *Grants) Set(ws int64) []string {
	out := []string{}
	for _, p := range Permissions {
		if g.Has(ws, p.Name) {
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
	case slices.Contains(perms, "edit_tasks"):
		return "member"
	case slices.Contains(perms, "comment"):
		return "commenter"
	case slices.Contains(perms, "view"):
		return "viewer"
	}
	return ""
}

// UserHas resolves one permission for background work (scheduler, notifications, assignees): the
// user must exist and be active.
func UserHas(ctx context.Context, q Q, userID, ws int64, perm string) bool {
	u, err := GetUser(ctx, q, userID)
	if err != nil || u.Disabled {
		return false
	}
	g, err := LoadGrants(ctx, q, u)
	return err == nil && g.Has(ws, perm)
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

// AccessRow is one workspace (or the all-workspaces level, WorkspaceID 0) of a user's resolved grid.
type AccessRow struct {
	WorkspaceID int64      `json:"workspace_id"` // 0 = all workspaces (what a new workspace starts with)
	Role        string     `json:"role"`         // closest preset, display only
	Permissions []Decision `json:"permissions"`
}

// AccessGrid resolves every permission in every workspace, plus the all-workspaces level.
func (g *Grants) AccessGrid() []AccessRow {
	out := make([]AccessRow, 0, len(g.workspaces)+1)
	for _, ws := range append([]int64{0}, g.workspaces...) {
		row := AccessRow{WorkspaceID: ws, Permissions: make([]Decision, 0, len(Permissions))}
		set := []string{}
		for _, p := range Permissions {
			d := g.Decide(ws, p.Name)
			row.Permissions = append(row.Permissions, d)
			if d.Allowed {
				set = append(set, p.Name)
			}
		}
		row.Role = RoleLabel(g.Admin, set)
		out = append(out, row)
	}
	return out
}

// Membership is a user who can see a workspace.
type Membership struct {
	WorkspaceID int64  `json:"workspace_id"`
	UserID      int64  `json:"user_id"`
	Role        string `json:"role"`             // closest preset of what they can do there
	Preset      string `json:"preset,omitempty"` // their own preset in this workspace ("" = none: access comes from elsewhere); admins only
	Source      string `json:"source,omitempty"` // where "view" comes from: user | user_all | group | group_all; admins only
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
		if u.IsAdmin && own == nil {
			continue
		}
		g.Admin = false // list what the rules give; admins see everything anyway
		d := g.Decide(ws, "view")
		if !d.Allowed {
			continue
		}
		m := Membership{WorkspaceID: ws, UserID: u.ID, Role: RoleLabel(false, g.Set(ws)), Source: d.Source}
		if own != nil {
			m.Preset = own.Preset
		}
		out = append(out, m)
	}
	return out, nil
}

// UsersWith lists active users holding perm in ws (admins included).
func UsersWith(ctx context.Context, q Q, ws int64, perm string) ([]int64, error) {
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
		if g.Has(ws, perm) {
			out = append(out, u.ID)
		}
	}
	return out, nil
}

// SetRule writes (or, with no preset and no overrides, removes) the rule of a user or a group at
// one level. subject is "user" or "group"; ws nil = all workspaces.
func SetRule(ctx context.Context, q Q, a Actor, subject string, id int64, ws *int64, preset string, overrides map[string]bool) error {
	col := map[string]string{"user": "user_id", "group": "group_id"}[subject]
	if col == "" {
		return Invalid("subject is user or group")
	}
	if preset != "" && !ValidRole(preset) {
		return Invalid("preset is viewer, commenter, member, manager or empty")
	}
	for name := range overrides {
		if !ValidPermission(name) {
			return Invalid("unknown permission %q", name)
		}
	}
	if overrides == nil {
		overrides = map[string]bool{}
	}
	var before int
	_ = q.QueryRowContext(ctx, `SELECT count(*) FROM access_rules WHERE `+col+` = ? AND workspace_id IS ?`, id, nullInt(ws)).Scan(&before)
	if _, err := q.ExecContext(ctx, `DELETE FROM access_rules WHERE `+col+` = ? AND workspace_id IS ?`, id, nullInt(ws)); err != nil {
		return err
	}
	removed := preset == "" && len(overrides) == 0
	if !removed {
		ov, _ := json.Marshal(overrides)
		if _, err := q.ExecContext(ctx, `INSERT INTO access_rules (`+col+`, workspace_id, preset, overrides, created_at) VALUES (?,?,?,?,?)`,
			id, nullInt(ws), preset, string(ov), Now()); err != nil {
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
	_, err := Activity(ctx, q, a, ws, nil, subject+".access", subject, id, map[string]any{"workspace_id": ws, "preset": preset, "overrides": overrides})
	return err
}

// SetMembership sets a user's preset in one workspace, keeping their overrides there; role ""
// removes their own rule for the workspace (access from groups or all-workspace rules remains).
func SetMembership(ctx context.Context, q Q, a Actor, ws, user int64, role string) error {
	if role != "" && !ValidRole(role) {
		return Invalid("role is viewer, commenter, member or manager")
	}
	ov := map[string]bool{}
	if role != "" {
		rules, err := RulesOfUser(ctx, q, user)
		if err != nil {
			return err
		}
		for _, r := range rules {
			if r.WorkspaceID != nil && *r.WorkspaceID == ws {
				ov = r.Overrides
			}
		}
	}
	return SetRule(ctx, q, a, "user", user, &ws, role, ov)
}

// dropUnseenAssignees removes assignees who can no longer see their task's workspace.
func dropUnseenAssignees(ctx context.Context, q Q) error {
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT a.user_id, t.workspace_id FROM task_assignees a JOIN tasks t ON t.id = a.task_id`)
	if err != nil {
		return err
	}
	type pair struct{ user, ws int64 }
	var pairs []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.user, &p.ws); err != nil {
			rows.Close()
			return err
		}
		pairs = append(pairs, p)
	}
	rows.Close()
	for _, p := range pairs {
		if _, g, err := UserGrants(ctx, q, p.user); err == nil && g.Has(p.ws, "view") {
			continue
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM task_assignees WHERE user_id = ? AND task_id IN (SELECT id FROM tasks WHERE workspace_id = ?)`, p.user, p.ws); err != nil {
			return err
		}
	}
	return nil
}

func isFK(err error) bool {
	return err != nil && strings.Contains(err.Error(), "FOREIGN KEY constraint failed")
}

// NewGrants builds grants from rules in memory (tests, previews). Group rules need GroupID set.
func NewGrants(admin bool, rules []*AccessRule, workspaces []int64) *Grants {
	g := &Grants{Admin: admin, userWS: map[int64]*AccessRule{}, groupWS: map[int64][]*AccessRule{}, workspaces: workspaces}
	for _, r := range rules {
		if r.Overrides == nil {
			r.Overrides = map[string]bool{}
		}
		switch {
		case r.GroupID == nil && r.WorkspaceID == nil:
			g.userAll = r
		case r.GroupID == nil:
			g.userWS[*r.WorkspaceID] = r
		case r.WorkspaceID == nil:
			g.groupAll = append(g.groupAll, r)
		default:
			g.groupWS[*r.WorkspaceID] = append(g.groupWS[*r.WorkspaceID], r)
		}
	}
	return g
}
