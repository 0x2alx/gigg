package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// Workspace is the isolation and membership unit.
type Workspace struct {
	ID             int64            `json:"id"`
	Slug           string           `json:"slug"`
	Name           string           `json:"name"`
	Color          *string          `json:"color"`
	OwnerID        *int64           `json:"owner_id" doc:"The user the workspace belongs to (display only; grants nothing)"`
	DescriptionMD  string           `json:"description_md"`
	StatusTemplate []StatusTemplate `json:"status_template"`
	Archived       bool             `json:"archived"`
	CreatedAt      int64            `json:"created_at"`
	Version        int64            `json:"version"`
}

// StatusTemplate is one status a new project starts with.
type StatusTemplate struct {
	Name     string  `json:"name"`
	Category string  `json:"category"`
	Color    *string `json:"color,omitempty"`
}

// DefaultStatusTemplate is the built-in template.
var DefaultStatusTemplate = []StatusTemplate{
	{Name: "Backlog", Category: "backlog"},
	{Name: "To-Do", Category: "todo"},
	{Name: "Doing", Category: "doing"},
	{Name: "Waiting", Category: "waiting"},
	{Name: "Done", Category: "done"},
	{Name: "Cancelled", Category: "cancelled"},
}

// Categories in board order.
var Categories = []string{"backlog", "todo", "doing", "waiting", "done", "cancelled"}

// ValidCategory reports a known status category.
func ValidCategory(c string) bool {
	for _, x := range Categories {
		if x == c {
			return true
		}
	}
	return false
}

// MaxStatuses per project.
const MaxStatuses = 12

var slugRe = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)
var keyRe = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,4}$`)

// ValidSlug reports a URL-safe identifier.
func ValidSlug(s string) bool { return slugRe.MatchString(s) }

func validateTemplate(t []StatusTemplate) error {
	if len(t) == 0 || len(t) > MaxStatuses {
		return Invalid("a status template has 1-%d statuses", MaxStatuses)
	}
	seen := map[string]bool{}
	for i := range t {
		if t[i].Name == "" || len(t[i].Name) > 40 {
			return Invalid("status names: 1-40 characters")
		}
		if !ValidCategory(t[i].Category) {
			return Invalid("unknown status category %q", t[i].Category)
		}
		if t[i].Color != nil {
			c, err := NormColor(*t[i].Color)
			if err != nil {
				return err
			}
			t[i].Color = &c
		}
		seen[t[i].Category] = true
	}
	if !seen["todo"] || !seen["done"] {
		return Invalid("a status template needs a todo and a done status")
	}
	return nil
}

const wsCols = `id, slug, name, color, owner_id, description_md, status_template, archived_at IS NOT NULL, created_at, version`

func scanWorkspace(row interface{ Scan(...any) error }) (*Workspace, error) {
	var w Workspace
	var color sql.NullString
	var owner sql.NullInt64
	var tmpl string
	if err := row.Scan(&w.ID, &w.Slug, &w.Name, &color, &owner, &w.DescriptionMD, &tmpl, &w.Archived, &w.CreatedAt, &w.Version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	w.Color = ptrStr(color)
	if owner.Valid {
		w.OwnerID = &owner.Int64
	}
	_ = json.Unmarshal([]byte(tmpl), &w.StatusTemplate)
	return &w, nil
}

// CreateWorkspace inserts a workspace.
func CreateWorkspace(ctx context.Context, q Q, a Actor, slug, name string, tmpl []StatusTemplate) (*Workspace, error) {
	if !ValidSlug(slug) {
		return nil, Invalid("workspace slug: 2-32 lowercase letters, digits and -, starting with a letter")
	}
	if name == "" || len(name) > 64 {
		return nil, Invalid("workspace name: 1-64 characters")
	}
	if tmpl == nil {
		tmpl = DefaultStatusTemplate
	}
	if err := validateTemplate(tmpl); err != nil {
		return nil, err
	}
	// the owner defaults to the user named like the slug (an agent's own workspace), if any
	res, err := q.ExecContext(ctx, `INSERT INTO workspaces (slug, name, status_template, created_at, owner_id)
		VALUES (?,?,?,?,(SELECT id FROM users WHERE username = ?))`,
		slug, name, mustJSON(tmpl), Now(), slug)
	if isUnique(err) {
		return nil, Conflict("workspace %q exists", slug)
	}
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	w, err := GetWorkspace(ctx, q, id)
	if err != nil {
		return nil, err
	}
	if _, err := Activity(ctx, q, a, &id, nil, "workspace.created", "workspace", id, AuditSnapshot("workspace", w, nil)); err != nil {
		return nil, err
	}
	return w, nil
}

// GetWorkspace loads a workspace by id.
func GetWorkspace(ctx context.Context, q Q, id int64) (*Workspace, error) {
	return scanWorkspace(q.QueryRowContext(ctx, "SELECT "+wsCols+" FROM workspaces WHERE id = ?", id))
}

// GetWorkspaceBySlug loads a workspace by slug.
// OwnedWorkspace is the one live workspace a user owns, or ErrNotFound when they own none or several.
func OwnedWorkspace(ctx context.Context, q Q, user int64) (int64, error) {
	rows, err := q.QueryContext(ctx, `SELECT id FROM workspaces WHERE owner_id = ? AND archived_at IS NULL LIMIT 2`, user)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(ids) != 1 {
		return 0, ErrNotFound
	}
	return ids[0], nil
}

func GetWorkspaceBySlug(ctx context.Context, q Q, slug string) (*Workspace, error) {
	return scanWorkspace(q.QueryRowContext(ctx, "SELECT "+wsCols+" FROM workspaces WHERE slug = ?", slug))
}

// ListWorkspaces returns the given workspaces (nil = all).
func ListWorkspaces(ctx context.Context, q Q, ids []int64) ([]*Workspace, error) {
	query, args := "SELECT "+wsCols+" FROM workspaces", []any{}
	if ids != nil {
		query += " WHERE id IN (" + placeholders(len(ids)) + ")"
		args = int64Args(ids)
	}
	rows, err := q.QueryContext(ctx, query+" ORDER BY name COLLATE NOCASE", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Workspace
	for rows.Next() {
		w, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// WorkspacePatch changes a workspace.
type WorkspacePatch struct {
	Name           *string
	Color          **string
	OwnerID        **int64
	DescriptionMD  *string
	StatusTemplate []StatusTemplate
	Archived       *bool
}

// PatchWorkspace applies a change; expect 0 skips the version check.
func PatchWorkspace(ctx context.Context, q Q, a Actor, id, expect int64, p WorkspacePatch) (*Workspace, error) {
	w, err := GetWorkspace(ctx, q, id)
	if err != nil {
		return nil, err
	}
	if expect != 0 && expect != w.Version {
		return nil, ErrVersion
	}
	changed := map[string]any{}
	if p.Name != nil {
		if *p.Name == "" || len(*p.Name) > 64 {
			return nil, Invalid("workspace name: 1-64 characters")
		}
		changed["name"] = *p.Name
	}
	if p.Color != nil {
		if *p.Color == nil {
			changed["color"] = nil
		} else {
			c, err := NormColor(**p.Color)
			if err != nil {
				return nil, err
			}
			changed["color"] = c
		}
	}
	if p.OwnerID != nil {
		if *p.OwnerID == nil {
			changed["owner_id"] = nil
		} else {
			if _, err := GetUser(ctx, q, **p.OwnerID); errors.Is(err, ErrNotFound) {
				return nil, Invalid("owner: no user %d", **p.OwnerID)
			} else if err != nil {
				return nil, err
			}
			changed["owner_id"] = **p.OwnerID
		}
	}
	if p.DescriptionMD != nil {
		if len(*p.DescriptionMD) > 65536 {
			return nil, Invalid("description: at most 64 KiB")
		}
		changed["description_md"] = *p.DescriptionMD
	}
	if p.StatusTemplate != nil {
		if err := validateTemplate(p.StatusTemplate); err != nil {
			return nil, err
		}
		changed["status_template"] = mustJSON(p.StatusTemplate)
	}
	if p.Archived != nil {
		if *p.Archived {
			changed["archived_at"] = Now()
		} else {
			changed["archived_at"] = nil
		}
	}
	if len(changed) == 0 {
		return w, nil
	}
	query, args := "UPDATE workspaces SET version = version + 1", []any{}
	for col, v := range changed {
		query += ", " + col + " = ?"
		args = append(args, v)
	}
	if _, err := q.ExecContext(ctx, query+" WHERE id = ?", append(args, id)...); err != nil {
		return nil, err
	}
	after, err := GetWorkspace(ctx, q, id)
	if err != nil {
		return nil, err
	}
	if _, err := Activity(ctx, q, a, &id, nil, "workspace.updated", "workspace", id, AuditDiff("workspace", w, after)); err != nil {
		return nil, err
	}
	return after, nil
}

// Project is a board inside a workspace.
type Project struct {
	ID            int64   `json:"id"`
	WorkspaceID   int64   `json:"workspace_id"`
	Slug          string  `json:"slug"`
	Name          string  `json:"name"`
	Key           string  `json:"key"`
	Color         *string `json:"color"`
	DescriptionMD string  `json:"description_md"`
	Position      float64 `json:"position"`
	NextGigNumber int64   `json:"-"`
	Archived      bool    `json:"archived"`
	Deleted       bool    `json:"deleted"`
	CreatedAt     int64   `json:"created_at"`
	Version       int64   `json:"version"`
}

const projCols = `id, workspace_id, slug, name, key, color, description_md, position, next_gig_number,
	archived_at IS NOT NULL, deleted_at IS NOT NULL, created_at, version`

func scanProject(row interface{ Scan(...any) error }) (*Project, error) {
	var p Project
	var color sql.NullString
	if err := row.Scan(&p.ID, &p.WorkspaceID, &p.Slug, &p.Name, &p.Key, &color, &p.DescriptionMD, &p.Position,
		&p.NextGigNumber, &p.Archived, &p.Deleted, &p.CreatedAt, &p.Version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	p.Color = ptrStr(color)
	return &p, nil
}

// CreateProject inserts a project with statuses from the workspace template (or the given ones).
func CreateProject(ctx context.Context, q Q, a Actor, ws int64, slug, name, key string, statuses []StatusTemplate) (*Project, error) {
	if !ValidSlug(slug) {
		return nil, Invalid("project slug: 2-32 lowercase letters, digits and -, starting with a letter")
	}
	if name == "" || len(name) > 64 {
		return nil, Invalid("project name: 1-64 characters")
	}
	if !keyRe.MatchString(key) {
		return nil, Invalid("project key: 2-5 capital letters or digits, starting with a letter")
	}
	w, err := GetWorkspace(ctx, q, ws)
	if err != nil {
		return nil, err
	}
	if statuses == nil {
		statuses = w.StatusTemplate
	}
	if err := validateTemplate(statuses); err != nil {
		return nil, err
	}
	var pos float64
	_ = q.QueryRowContext(ctx, `SELECT COALESCE(MAX(position), 0) + 1024 FROM projects WHERE workspace_id = ?`, ws).Scan(&pos)
	res, err := q.ExecContext(ctx, `INSERT INTO projects (workspace_id, slug, name, key, position, created_at) VALUES (?,?,?,?,?,?)`,
		ws, slug, name, key, pos, Now())
	if isUnique(err) {
		return nil, Conflict("a project with that slug or key exists in this workspace")
	}
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if alias, _ := aliasTaken(ctx, q, ws, key); alias {
		return nil, Conflict("key %s is still used by moved gigs' old keys in this workspace", key)
	}
	for i, st := range statuses {
		def := 0
		if st.Category == "todo" && i == firstOf(statuses, "todo") {
			def = 1
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO statuses (workspace_id, project_id, name, category, color, position, is_default)
			VALUES (?,?,?,?,?,?,?)`, ws, id, st.Name, st.Category, nullStr(st.Color), float64(i+1)*1024, def); err != nil {
			return nil, err
		}
	}
	pr, err := GetProject(ctx, q, id)
	if err != nil {
		return nil, err
	}
	if _, err := Activity(ctx, q, a, &ws, &id, "project.created", "project", id, AuditSnapshot("project", pr, nil)); err != nil {
		return nil, err
	}
	return pr, nil
}

func aliasTaken(ctx context.Context, q Q, ws int64, key string) (bool, error) {
	var n int
	// old keys of gigs that live on (in a project that isn't deleted) keep resolving, so their key stays taken
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM gig_aliases a JOIN gigs t ON t.id = a.gig_id JOIN projects p ON p.id = t.project_id
		WHERE a.workspace_id = ? AND a.project_key = ? AND p.deleted_at IS NULL`, ws, key).Scan(&n)
	return n > 0, err
}

func firstOf(t []StatusTemplate, cat string) int {
	for i := range t {
		if t[i].Category == cat {
			return i
		}
	}
	return -1
}

// GetProject loads a project.
func GetProject(ctx context.Context, q Q, id int64) (*Project, error) {
	return scanProject(q.QueryRowContext(ctx, "SELECT "+projCols+" FROM projects WHERE id = ?", id))
}

// ProjectBySlug loads a live project of a workspace by slug or id.
func ProjectBySlug(ctx context.Context, q Q, ws int64, ref string) (*Project, error) {
	p, err := scanProject(q.QueryRowContext(ctx, "SELECT "+projCols+" FROM projects WHERE workspace_id = ? AND (slug = ? OR CAST(id AS TEXT) = ?) AND deleted_at IS NULL",
		ws, ref, ref))
	if err != nil {
		return nil, err
	}
	return p, nil
}

// ResolvePlace reads "workspace" or "workspace/project" (slugs or ids) into ids.
func ResolvePlace(ctx context.Context, q Q, ref string) (ws int64, project *int64, err error) {
	wref, pref, hasProject := strings.Cut(ref, "/")
	var w *Workspace
	if id, e := strconv.ParseInt(wref, 10, 64); e == nil {
		w, err = GetWorkspace(ctx, q, id)
	} else {
		w, err = GetWorkspaceBySlug(ctx, q, wref)
	}
	if err != nil {
		return 0, nil, Invalid("no workspace %q", wref)
	}
	if !hasProject {
		return w.ID, nil, nil
	}
	p, err := ProjectBySlug(ctx, q, w.ID, pref)
	if err != nil {
		return 0, nil, Invalid("no project %q in %s", pref, w.Slug)
	}
	return w.ID, &p.ID, nil
}

// ListProjects returns a workspace's projects (not deleted); only = optional narrowing.
func ListProjects(ctx context.Context, q Q, ws int64, only []int64) ([]*Project, error) {
	query, args := "SELECT "+projCols+" FROM projects WHERE workspace_id = ? AND deleted_at IS NULL", []any{ws}
	if only != nil {
		query += " AND id IN (" + placeholders(len(only)) + ")"
		args = append(args, int64Args(only)...)
	}
	rows, err := q.QueryContext(ctx, query+" ORDER BY position, id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ProjectPatch changes a project.
type ProjectPatch struct {
	Name          *string
	Color         **string
	DescriptionMD *string
	Archived      *bool
}

// PatchProject applies a change.
func PatchProject(ctx context.Context, q Q, a Actor, id, expect int64, p ProjectPatch) (*Project, error) {
	pr, err := GetProject(ctx, q, id)
	if err != nil {
		return nil, err
	}
	if expect != 0 && expect != pr.Version {
		return nil, ErrVersion
	}
	changed := map[string]any{}
	if p.Name != nil {
		if *p.Name == "" || len(*p.Name) > 64 {
			return nil, Invalid("project name: 1-64 characters")
		}
		changed["name"] = *p.Name
	}
	if p.Color != nil {
		if *p.Color == nil {
			changed["color"] = nil
		} else {
			c, err := NormColor(**p.Color)
			if err != nil {
				return nil, err
			}
			changed["color"] = c
		}
	}
	if p.DescriptionMD != nil {
		if len(*p.DescriptionMD) > 65536 {
			return nil, Invalid("description: at most 64 KiB")
		}
		changed["description_md"] = *p.DescriptionMD
	}
	if p.Archived != nil {
		if *p.Archived {
			changed["archived_at"] = Now()
		} else {
			changed["archived_at"] = nil
		}
	}
	if len(changed) == 0 {
		return pr, nil
	}
	query, args := "UPDATE projects SET version = version + 1", []any{}
	for col, v := range changed {
		query += ", " + col + " = ?"
		args = append(args, v)
	}
	if _, err := q.ExecContext(ctx, query+" WHERE id = ?", append(args, id)...); err != nil {
		return nil, err
	}
	after, err := GetProject(ctx, q, id)
	if err != nil {
		return nil, err
	}
	if _, err := Activity(ctx, q, a, &pr.WorkspaceID, &id, "project.updated", "project", id, AuditDiff("project", pr, after)); err != nil {
		return nil, err
	}
	return after, nil
}

// DeleteProject soft-deletes an empty project (gigs must be moved or trashed first).
func DeleteProject(ctx context.Context, q Q, a Actor, p *Project) error {
	var live int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM gigs WHERE project_id = ? AND deleted_at IS NULL`, p.ID).Scan(&live); err != nil {
		return err
	}
	if live > 0 {
		return Conflict("the project still has %d gigs: move or delete them first", live)
	}
	if _, err := q.ExecContext(ctx, `UPDATE projects SET deleted_at = ?, version = version + 1 WHERE id = ?`, Now(), p.ID); err != nil {
		return err
	}
	_, err := Activity(ctx, q, a, &p.WorkspaceID, &p.ID, "project.deleted", "project", p.ID, AuditSnapshot("project", p, nil))
	return err
}

// Status is a column of a project's board.
type Status struct {
	ID          int64   `json:"id"`
	WorkspaceID int64   `json:"workspace_id"`
	ProjectID   int64   `json:"project_id"`
	Name        string  `json:"name"`
	Category    string  `json:"category"`
	Color       *string `json:"color"`
	Position    float64 `json:"position"`
	WIPLimit    *int64  `json:"wip_limit"`
	IsDefault   bool    `json:"is_default"`
	Version     int64   `json:"version"`
}

const statusCols = `id, workspace_id, project_id, name, category, color, position, wip_limit, is_default, version`

func scanStatus(row interface{ Scan(...any) error }) (*Status, error) {
	var s Status
	var color sql.NullString
	var wip sql.NullInt64
	if err := row.Scan(&s.ID, &s.WorkspaceID, &s.ProjectID, &s.Name, &s.Category, &color, &s.Position, &wip, &s.IsDefault, &s.Version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	s.Color, s.WIPLimit = ptrStr(color), ptrInt(wip)
	return &s, nil
}

// Statuses returns a project's statuses in board order.
func Statuses(ctx context.Context, q Q, project int64) ([]*Status, error) {
	rows, err := q.QueryContext(ctx, "SELECT "+statusCols+" FROM statuses WHERE project_id = ? ORDER BY position, id", project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Status
	for rows.Next() {
		s, err := scanStatus(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetStatus loads one status.
func GetStatus(ctx context.Context, q Q, id int64) (*Status, error) {
	return scanStatus(q.QueryRowContext(ctx, "SELECT "+statusCols+" FROM statuses WHERE id = ?", id))
}

// FirstStatusOf returns the first status of a category in a project (the default one for todo).
func FirstStatusOf(ctx context.Context, q Q, project int64, category string) (*Status, error) {
	return scanStatus(q.QueryRowContext(ctx, "SELECT "+statusCols+` FROM statuses WHERE project_id = ? AND category = ?
		ORDER BY is_default DESC, position, id LIMIT 1`, project, category))
}

// DefaultStatus is where new gigs go: the default status, else the first todo, else the first.
func DefaultStatus(ctx context.Context, q Q, project int64) (*Status, error) {
	return scanStatus(q.QueryRowContext(ctx, "SELECT "+statusCols+` FROM statuses WHERE project_id = ?
		ORDER BY is_default DESC, category = 'todo' DESC, position LIMIT 1`, project))
}

// CreateStatus adds a status at the end of a project's board.
func CreateStatus(ctx context.Context, q Q, a Actor, p *Project, name, category string, color *string) (*Status, error) {
	if name == "" || len(name) > 40 {
		return nil, Invalid("status names: 1-40 characters")
	}
	if !ValidCategory(category) {
		return nil, Invalid("unknown status category %q", category)
	}
	if color != nil {
		c, err := NormColor(*color)
		if err != nil {
			return nil, err
		}
		color = &c
	}
	var n int
	var pos float64
	if err := q.QueryRowContext(ctx, `SELECT count(*), COALESCE(MAX(position), 0) + 1024 FROM statuses WHERE project_id = ?`, p.ID).Scan(&n, &pos); err != nil {
		return nil, err
	}
	if n >= MaxStatuses {
		return nil, Conflict("a project has at most %d statuses", MaxStatuses)
	}
	res, err := q.ExecContext(ctx, `INSERT INTO statuses (workspace_id, project_id, name, category, color, position) VALUES (?,?,?,?,?,?)`,
		p.WorkspaceID, p.ID, name, category, nullStr(color), pos)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	st, err := GetStatus(ctx, q, id)
	if err != nil {
		return nil, err
	}
	if _, err := Activity(ctx, q, a, &p.WorkspaceID, &p.ID, "status.created", "status", id, AuditSnapshot("status", st, nil)); err != nil {
		return nil, err
	}
	return st, nil
}

// StatusPatch changes a status.
type StatusPatch struct {
	Name      *string
	Category  *string
	Color     **string
	WIPLimit  **int64
	IsDefault *bool
}

// PatchStatus applies a change.
func PatchStatus(ctx context.Context, q Q, a Actor, s *Status, expect int64, p StatusPatch) (*Status, error) {
	if err := checkVersion(ctx, q, "statuses", s.ID, expect); err != nil {
		return nil, err
	}
	before, err := GetStatus(ctx, q, s.ID) // the caller's copy may be stale
	if err != nil {
		return nil, err
	}
	changed := map[string]any{}
	if p.Name != nil {
		if *p.Name == "" || len(*p.Name) > 40 {
			return nil, Invalid("status names: 1-40 characters")
		}
		changed["name"] = *p.Name
	}
	if p.Category != nil {
		if !ValidCategory(*p.Category) {
			return nil, Invalid("unknown status category %q", *p.Category)
		}
		changed["category"] = *p.Category
	}
	if p.Color != nil {
		if *p.Color == nil {
			changed["color"] = nil
		} else {
			c, err := NormColor(**p.Color)
			if err != nil {
				return nil, err
			}
			changed["color"] = c
		}
	}
	if p.WIPLimit != nil {
		if *p.WIPLimit != nil && **p.WIPLimit < 1 {
			return nil, Invalid("WIP limit is at least 1")
		}
		changed["wip_limit"] = nullInt(*p.WIPLimit)
	}
	if p.IsDefault != nil && *p.IsDefault {
		if _, err := q.ExecContext(ctx, `UPDATE statuses SET is_default = 0 WHERE project_id = ?`, s.ProjectID); err != nil {
			return nil, err
		}
		changed["is_default"] = 1
	}
	if len(changed) == 0 {
		return s, nil
	}
	query, args := "UPDATE statuses SET version = version + 1", []any{}
	for col, v := range changed {
		query += ", " + col + " = ?"
		args = append(args, v)
	}
	if _, err := q.ExecContext(ctx, query+" WHERE id = ?", append(args, s.ID)...); err != nil {
		return nil, err
	}
	if err := checkProjectCategories(ctx, q, s.ProjectID); err != nil {
		return nil, err
	}
	after, err := GetStatus(ctx, q, s.ID)
	if err != nil {
		return nil, err
	}
	if _, err := Activity(ctx, q, a, &s.WorkspaceID, &s.ProjectID, "status.updated", "status", s.ID, AuditDiff("status", before, after)); err != nil {
		return nil, err
	}
	return after, nil
}

func checkProjectCategories(ctx context.Context, q Q, project int64) error {
	var todo, done int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE category = 'todo'), count(*) FILTER (WHERE category = 'done')
		FROM statuses WHERE project_id = ?`, project).Scan(&todo, &done); err != nil {
		return err
	}
	if todo == 0 || done == 0 {
		return Invalid("a project needs at least one todo and one done status")
	}
	return nil
}

// DeleteStatus removes an empty status.
func DeleteStatus(ctx context.Context, q Q, a Actor, s *Status) error {
	var n int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM gigs WHERE status_id = ?`, s.ID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return Conflict("the status still holds %d gigs (including trashed ones): move them first", n)
	}
	var same int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM statuses WHERE project_id = ? AND category = ? AND id != ?`,
		s.ProjectID, s.Category, s.ID).Scan(&same); err != nil {
		return err
	}
	if same == 0 && (s.Category == "todo" || s.Category == "done") {
		return Invalid("a project needs at least one todo and one done status")
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM statuses WHERE id = ?`, s.ID); err != nil {
		return err
	}
	_, err := Activity(ctx, q, a, &s.WorkspaceID, &s.ProjectID, "status.deleted", "status", s.ID, AuditSnapshot("status", s, nil))
	return err
}

// ReorderStatuses sets the board order of a project's statuses (all ids, in order).
func ReorderStatuses(ctx context.Context, q Q, a Actor, p *Project, ids []int64) error {
	cur, err := Statuses(ctx, q, p.ID)
	if err != nil {
		return err
	}
	if len(ids) != len(cur) {
		return Invalid("send every status id of the project, in the new order")
	}
	have := map[int64]bool{}
	for _, s := range cur {
		have[s.ID] = true
	}
	for i, id := range ids {
		if !have[id] {
			return Invalid("status %d is not in this project", id)
		}
		delete(have, id)
		if _, err := q.ExecContext(ctx, `UPDATE statuses SET position = ?, version = version + 1 WHERE id = ?`, float64(i+1)*1024, id); err != nil {
			return err
		}
	}
	_, err = Activity(ctx, q, a, &p.WorkspaceID, &p.ID, "status.reordered", "project", p.ID, map[string]any{"order": ids})
	return err
}

// Label is shared by a workspace's projects.
type Label struct {
	ID          int64   `json:"id"`
	WorkspaceID int64   `json:"workspace_id"`
	Name        string  `json:"name"`
	Color       *string `json:"color"`
	Description string  `json:"description"`
	Version     int64   `json:"version"`
}

func scanLabel(row interface{ Scan(...any) error }) (*Label, error) {
	var l Label
	var color sql.NullString
	if err := row.Scan(&l.ID, &l.WorkspaceID, &l.Name, &color, &l.Description, &l.Version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	l.Color = ptrStr(color)
	return &l, nil
}

// Labels lists a workspace's labels.
func Labels(ctx context.Context, q Q, ws int64) ([]*Label, error) { return LabelsIn(ctx, q, ws, nil) }

// LabelsIn lists a workspace's labels; with projects (not nil), only those on a gig of one of them.
func LabelsIn(ctx context.Context, q Q, ws int64, projects []int64) ([]*Label, error) {
	where, args := `workspace_id = ?`, []any{ws}
	if projects != nil {
		where += ` AND id IN (SELECT gl.label_id FROM gig_labels gl JOIN gigs t ON t.id = gl.gig_id WHERE t.project_id IN (` + placeholders(len(projects)) + `))`
		args = append(args, int64Args(projects)...)
	}
	rows, err := q.QueryContext(ctx, `SELECT id, workspace_id, name, color, description, version FROM labels WHERE `+where+` ORDER BY name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Label
	for rows.Next() {
		l, err := scanLabel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// GetLabel loads a label.
func GetLabel(ctx context.Context, q Q, id int64) (*Label, error) {
	return scanLabel(q.QueryRowContext(ctx, `SELECT id, workspace_id, name, color, description, version FROM labels WHERE id = ?`, id))
}

// LabelByName finds (or, with create, makes) a workspace label by name.
func LabelByName(ctx context.Context, q Q, a Actor, ws int64, name string, create bool) (*Label, error) {
	l, err := scanLabel(q.QueryRowContext(ctx, `SELECT id, workspace_id, name, color, description, version FROM labels WHERE workspace_id = ? AND name = ?`, ws, name))
	if errors.Is(err, ErrNotFound) && create {
		return CreateLabel(ctx, q, a, ws, name, nil, "")
	}
	return l, err
}

// CreateLabel adds a label.
func CreateLabel(ctx context.Context, q Q, a Actor, ws int64, name string, color *string, desc string) (*Label, error) {
	if name == "" || len(name) > 40 {
		return nil, Invalid("label names: 1-40 characters")
	}
	if len(desc) > 200 {
		return nil, Invalid("label description: at most 200 characters")
	}
	if color != nil {
		c, err := NormColor(*color)
		if err != nil {
			return nil, err
		}
		color = &c
	}
	res, err := q.ExecContext(ctx, `INSERT INTO labels (workspace_id, name, color, description) VALUES (?,?,?,?)`, ws, name, nullStr(color), desc)
	if isUnique(err) {
		return nil, Conflict("label %q exists", name)
	}
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	l, err := GetLabel(ctx, q, id)
	if err != nil {
		return nil, err
	}
	if _, err := Activity(ctx, q, a, &ws, nil, "label.created", "label", id, AuditSnapshot("label", l, nil)); err != nil {
		return nil, err
	}
	return l, nil
}

// LabelPatch changes a label.
type LabelPatch struct {
	Name        *string
	Color       **string
	Description *string
}

// PatchLabel applies a change.
func PatchLabel(ctx context.Context, q Q, a Actor, l *Label, expect int64, p LabelPatch) (*Label, error) {
	if err := checkVersion(ctx, q, "labels", l.ID, expect); err != nil {
		return nil, err
	}
	before, err := GetLabel(ctx, q, l.ID) // the caller's copy may be stale
	if err != nil {
		return nil, err
	}
	changed := map[string]any{}
	if p.Name != nil {
		if *p.Name == "" || len(*p.Name) > 40 {
			return nil, Invalid("label names: 1-40 characters")
		}
		changed["name"] = *p.Name
	}
	if p.Color != nil {
		if *p.Color == nil {
			changed["color"] = nil
		} else {
			c, err := NormColor(**p.Color)
			if err != nil {
				return nil, err
			}
			changed["color"] = c
		}
	}
	if p.Description != nil {
		if len(*p.Description) > 200 {
			return nil, Invalid("label description: at most 200 characters")
		}
		changed["description"] = *p.Description
	}
	if len(changed) == 0 {
		return l, nil
	}
	query, args := "UPDATE labels SET version = version + 1", []any{}
	for col, v := range changed {
		query += ", " + col + " = ?"
		args = append(args, v)
	}
	_, err = q.ExecContext(ctx, query+" WHERE id = ?", append(args, l.ID)...)
	if isUnique(err) {
		return nil, Conflict("label %q exists", *p.Name)
	}
	if err != nil {
		return nil, err
	}
	after, err := GetLabel(ctx, q, l.ID)
	if err != nil {
		return nil, err
	}
	if _, err := Activity(ctx, q, a, &l.WorkspaceID, nil, "label.updated", "label", l.ID, AuditDiff("label", before, after)); err != nil {
		return nil, err
	}
	return after, nil
}

// DeleteLabel removes a label from every gig and deletes it.
func DeleteLabel(ctx context.Context, q Q, a Actor, l *Label) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM labels WHERE id = ?`, l.ID); err != nil {
		return err
	}
	_, err := Activity(ctx, q, a, &l.WorkspaceID, nil, "label.deleted", "label", l.ID, AuditSnapshot("label", l, nil))
	return err
}
