package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------- relations

// Relation links two gigs of one workspace: From blocks To, or they relate.
type Relation struct {
	From    int64  `json:"from_gig_id"`
	FromKey string `json:"from_key"`
	To      int64  `json:"to_gig_id"`
	ToKey   string `json:"to_key"`
	Kind    string `json:"kind"`
	Open    bool   `json:"open" doc:"The other gig is not done or cancelled"`
	Title   string `json:"title" doc:"The other gig's title"`
	// the other gig's status, as gigs name theirs (status_name, status_category)
	StatusName     string `json:"status_name" doc:"The other gig's status"`
	StatusCategory string `json:"status_category" doc:"The other gig's status category"`
	CreatedAt      int64  `json:"created_at"`
}

// GigRelations lists the relations touching a gig (the other side's title and state included).
func GigRelations(ctx context.Context, q Q, gig int64) ([]Relation, error) {
	rows, err := q.QueryContext(ctx, `SELECT r.from_gig_id, pf.key || '-' || f.number, r.to_gig_id, pt.key || '-' || t.number, r.kind, r.created_at,
		CASE WHEN r.from_gig_id = ? THEN t.title ELSE f.title END,
		CASE WHEN r.from_gig_id = ? THEN st.name ELSE sf.name END,
		CASE WHEN r.from_gig_id = ? THEN st.category ELSE sf.category END
		FROM gig_relations r
		JOIN gigs f ON f.id = r.from_gig_id JOIN projects pf ON pf.id = f.project_id JOIN statuses sf ON sf.id = f.status_id
		JOIN gigs t ON t.id = r.to_gig_id JOIN projects pt ON pt.id = t.project_id JOIN statuses st ON st.id = t.status_id
		WHERE (r.from_gig_id = ? OR r.to_gig_id = ?) AND f.deleted_at IS NULL AND t.deleted_at IS NULL
		ORDER BY r.created_at`, gig, gig, gig, gig, gig)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Relation{}
	for rows.Next() {
		var r Relation
		if err := rows.Scan(&r.From, &r.FromKey, &r.To, &r.ToKey, &r.Kind, &r.CreatedAt, &r.Title, &r.StatusName, &r.StatusCategory); err != nil {
			return nil, err
		}
		cat := r.StatusCategory
		r.Open = cat != "done" && cat != "cancelled"
		out = append(out, r)
	}
	return out, rows.Err()
}

// Blocks reports whether gig `from` holds gig `to` back, directly or through a chain: "blocks"
// dependencies and subgigs marked blocks_parent (child -> parent) both count, whatever the gigs'
// state (a trashed or archived one may come back).
func Blocks(ctx context.Context, q Q, from, to int64) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, `WITH RECURSIVE
		e(f, t) AS (SELECT from_gig_id, to_gig_id FROM gig_relations WHERE kind = 'blocks'
			UNION ALL SELECT id, parent_id FROM gigs WHERE blocks_parent = 1 AND parent_id IS NOT NULL),
		down(id) AS (SELECT CAST(? AS BIGINT) UNION SELECT e.t FROM e JOIN down ON e.f = down.id)
		SELECT count(*) FROM down WHERE id = ?`, from, to).Scan(&n)
	return n > 0, err
}

// AddRelation links from -> to. Both gigs must be in one workspace; blocking cycles are refused.
func AddRelation(ctx context.Context, q Q, a Actor, from, to *Gig, kind string) error {
	if kind != "blocks" && kind != "relates" {
		return Invalid("kind is blocks or relates")
	}
	if from.ID == to.ID {
		return Invalid("a gig can't be linked to itself")
	}
	if from.WorkspaceID != to.WorkspaceID {
		return Invalid("both gigs must be in the same workspace")
	}
	if kind == "blocks" {
		// would `to` (transitively) block `from`? then this closes a cycle
		if cyc, err := Blocks(ctx, q, to.ID, from.ID); err != nil {
			return err
		} else if cyc {
			return Conflict("That would make a cycle: %s already depends on %s.", from.Key, to.Key)
		}
	}
	if _, err := q.ExecContext(ctx, `INSERT INTO gig_relations (workspace_id, from_gig_id, to_gig_id, kind, created_by, created_at) VALUES (?,?,?,?,?,?) ON CONFLICT DO NOTHING`,
		from.WorkspaceID, from.ID, to.ID, kind, a.UserID, Now()); err != nil {
		return err
	}
	_, err := Activity(ctx, q, a, &from.WorkspaceID, &from.ProjectID, "relation.added", "gig", from.ID, map[string]any{"to": to.Key, "kind": kind})
	return err
}

// RemoveRelation unlinks from -> to.
func RemoveRelation(ctx context.Context, q Q, a Actor, from, to *Gig, kind string) error {
	res, err := q.ExecContext(ctx, `DELETE FROM gig_relations WHERE from_gig_id = ? AND to_gig_id = ? AND kind = ?`, from.ID, to.ID, kind)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	_, err = Activity(ctx, q, a, &from.WorkspaceID, &from.ProjectID, "relation.removed", "gig", from.ID, map[string]any{"to": to.Key, "kind": kind})
	return err
}

// ---------------------------------------------------------------- checklists

// ChecklistItem is one line of a gig's checklist.
type ChecklistItem struct {
	ID        int64   `json:"id"`
	GigID     int64   `json:"gig_id"`
	Position  float64 `json:"position"`
	Text      string  `json:"text"`
	DoneAt    *int64  `json:"done_at"`
	DoneBy    *int64  `json:"done_by"`
	CreatedAt int64   `json:"created_at"`
}

// Checklist lists a gig's items in order.
func Checklist(ctx context.Context, q Q, gig int64) ([]ChecklistItem, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, gig_id, position, text, done_at, done_by, created_at FROM checklist_items WHERE gig_id = ? ORDER BY position, id`, gig)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChecklistItem{}
	for rows.Next() {
		var it ChecklistItem
		var done, by sql.NullInt64
		if err := rows.Scan(&it.ID, &it.GigID, &it.Position, &it.Text, &done, &by, &it.CreatedAt); err != nil {
			return nil, err
		}
		it.DoneAt, it.DoneBy = ptrInt(done), ptrInt(by)
		out = append(out, it)
	}
	return out, rows.Err()
}

func validItemText(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 500 {
		return "", Invalid("checklist text: 1-500 characters")
	}
	return s, nil
}

// AddChecklistItems appends items to a gig's checklist.
func AddChecklistItems(ctx context.Context, q Q, a Actor, t *Gig, texts []string) error {
	var n int
	_ = q.QueryRowContext(ctx, `SELECT count(*) FROM checklist_items WHERE gig_id = ?`, t.ID).Scan(&n)
	if n+len(texts) > 200 {
		return Invalid("a checklist holds at most 200 items")
	}
	var pos float64
	_ = q.QueryRowContext(ctx, `SELECT COALESCE(max(position), 0) FROM checklist_items WHERE gig_id = ?`, t.ID).Scan(&pos)
	for _, raw := range texts {
		text, err := validItemText(raw)
		if err != nil {
			return err
		}
		pos += 1024
		if _, err := q.ExecContext(ctx, `INSERT INTO checklist_items (workspace_id, gig_id, position, text, created_at) VALUES (?,?,?,?,?)`,
			t.WorkspaceID, t.ID, pos, text, Now()); err != nil {
			return err
		}
	}
	return touchGig(ctx, q, a, t, "checklist.added", map[string]any{"items": texts})
}

// ChecklistPatch changes one item.
type ChecklistPatch struct {
	Text  *string
	Done  *bool
	After *int64 // move after this item id (0 = to the top)
}

// PatchChecklistItem edits, ticks or moves an item of gig t.
func PatchChecklistItem(ctx context.Context, q Q, a Actor, t *Gig, id int64, p ChecklistPatch) error {
	var owner int64
	if err := q.QueryRowContext(ctx, `SELECT gig_id FROM checklist_items WHERE id = ?`, id).Scan(&owner); err != nil || owner != t.ID {
		return ErrNotFound
	}
	data := map[string]any{"item": id}
	if p.Text != nil {
		text, err := validItemText(*p.Text)
		if err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `UPDATE checklist_items SET text = ? WHERE id = ?`, text, id); err != nil {
			return err
		}
		data["text"] = text
	}
	if p.Done != nil {
		var err error
		if *p.Done {
			_, err = q.ExecContext(ctx, `UPDATE checklist_items SET done_at = COALESCE(done_at, ?), done_by = COALESCE(done_by, ?) WHERE id = ?`, Now(), a.UserID, id)
		} else {
			_, err = q.ExecContext(ctx, `UPDATE checklist_items SET done_at = NULL, done_by = NULL WHERE id = ?`, id)
		}
		if err != nil {
			return err
		}
		if err := touchProgress(ctx, q, t.ID); err != nil {
			return err
		}
		data["done"] = *p.Done
	}
	if p.After != nil {
		var lo, hi float64
		if *p.After == 0 {
			_ = q.QueryRowContext(ctx, `SELECT COALESCE(min(position), 1024) FROM checklist_items WHERE gig_id = ?`, t.ID).Scan(&hi)
			lo = hi - 2048
		} else {
			if err := q.QueryRowContext(ctx, `SELECT position FROM checklist_items WHERE id = ? AND gig_id = ?`, *p.After, t.ID).Scan(&lo); err != nil {
				return Invalid("no item %d on this gig", *p.After)
			}
			if err := q.QueryRowContext(ctx, `SELECT COALESCE(min(position), ? + 2048) FROM checklist_items WHERE gig_id = ? AND position > ? AND id != ?`,
				lo, t.ID, lo, id).Scan(&hi); err != nil {
				return err
			}
		}
		if _, err := q.ExecContext(ctx, `UPDATE checklist_items SET position = ? WHERE id = ?`, (lo+hi)/2, id); err != nil {
			return err
		}
		data["moved"] = true
	}
	return touchGig(ctx, q, a, t, "checklist.updated", data)
}

// DeleteChecklistItem removes an item of gig t.
func DeleteChecklistItem(ctx context.Context, q Q, a Actor, t *Gig, id int64) error {
	res, err := q.ExecContext(ctx, `DELETE FROM checklist_items WHERE id = ? AND gig_id = ?`, id, t.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return touchGig(ctx, q, a, t, "checklist.removed", map[string]any{"item": id})
}

// touchGig bumps a gig's version/updated time and logs the change.
func touchGig(ctx context.Context, q Q, a Actor, t *Gig, action string, data any) error {
	if _, err := q.ExecContext(ctx, `UPDATE gigs SET updated_at = ?, version = version + 1 WHERE id = ?`, Now(), t.ID); err != nil {
		return err
	}
	_, err := Activity(ctx, q, a, &t.WorkspaceID, &t.ProjectID, action, "gig", t.ID, data)
	return err
}

// ---------------------------------------------------------------- archive

// ArchiveGig archives or unarchives a gig.
func ArchiveGig(ctx context.Context, q Q, a Actor, t *Gig, archive bool) (*Gig, error) {
	if t.Deleted {
		return nil, Conflict("the gig is in the trash")
	}
	var v any
	action := "gig.unarchived"
	if archive {
		v, action = Now(), "gig.archived"
	}
	if _, err := q.ExecContext(ctx, `UPDATE gigs SET archived_at = ?, updated_at = ?, version = version + 1 WHERE id = ?`, v, Now(), t.ID); err != nil {
		return nil, err
	}
	if _, err := Activity(ctx, q, a, &t.WorkspaceID, &t.ProjectID, action, "gig", t.ID, nil); err != nil {
		return nil, err
	}
	return GetGig(ctx, q, t.ID)
}

// ArchiveDone archives a project's done/cancelled gigs finished at or before `before`; returns how many.
// category "done" or "cancelled" limits it to that one; "" takes both.
func ArchiveDone(ctx context.Context, q Q, a Actor, p *Project, before int64, category string) (int, error) {
	cats := []any{"done", "cancelled"}
	switch category {
	case "":
	case "done", "cancelled":
		cats = []any{category, category}
	default:
		return 0, Invalid("category must be done or cancelled")
	}
	rows, err := q.QueryContext(ctx, `SELECT t.id FROM gigs t JOIN statuses s ON s.id = t.status_id WHERE t.project_id = ? AND t.archived_at IS NULL
		AND t.deleted_at IS NULL AND s.category IN (?, ?) AND COALESCE(t.done_at, t.updated_at) <= ? LIMIT 5000`, p.ID, cats[0], cats[1], before)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	now := Now()
	for _, id := range ids {
		if _, err := q.ExecContext(ctx, `UPDATE gigs SET archived_at = ?, updated_at = ?, version = version + 1 WHERE id = ?`, now, now, id); err != nil {
			return 0, err
		}
	}
	if len(ids) > 0 {
		if _, err := Activity(ctx, q, a, &p.WorkspaceID, &p.ID, "project.archived_done", "project", p.ID, map[string]any{"count": len(ids), "before": before}); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

// ---------------------------------------------------------------- custom fields

// CustomField is a workspace-defined gig property.
type CustomField struct {
	ID          int64    `json:"id"`
	WorkspaceID int64    `json:"workspace_id"`
	Name        string   `json:"name"`
	Kind        string   `json:"kind" doc:"text, number, date, select, checkbox or url"`
	Options     []string `json:"options" doc:"select: the choices"`
	Position    float64  `json:"position"`
	CreatedAt   int64    `json:"created_at"`
	Version     int64    `json:"version"`
}

var fieldKinds = map[string]bool{"text": true, "number": true, "date": true, "select": true, "checkbox": true, "url": true}

func (f *CustomField) validate() error {
	f.Name = strings.TrimSpace(f.Name)
	if f.Name == "" || len(f.Name) > 60 {
		return Invalid("field name: 1-60 characters")
	}
	if !fieldKinds[f.Kind] {
		return Invalid("kind is text, number, date, select, checkbox or url")
	}
	if f.Options == nil {
		f.Options = []string{}
	}
	if f.Kind == "select" {
		if len(f.Options) == 0 || len(f.Options) > 50 {
			return Invalid("a select field needs 1-50 options")
		}
		seen := map[string]bool{}
		for i, o := range f.Options {
			o = strings.TrimSpace(o)
			if o == "" || len(o) > 60 || seen[o] {
				return Invalid("options: distinct, 1-60 characters each")
			}
			seen[o], f.Options[i] = true, o
		}
	} else {
		f.Options = []string{}
	}
	return nil
}

// CustomFields lists a workspace's fields in order.
func CustomFields(ctx context.Context, q Q, ws int64) ([]*CustomField, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, workspace_id, name, kind, options, position, created_at, version FROM custom_fields
		WHERE workspace_id = ? AND deleted_at IS NULL ORDER BY position, id`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*CustomField{}
	for rows.Next() {
		f := &CustomField{}
		var opts string
		if err := rows.Scan(&f.ID, &f.WorkspaceID, &f.Name, &f.Kind, &opts, &f.Position, &f.CreatedAt, &f.Version); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(opts), &f.Options)
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetCustomField loads one live field.
func GetCustomField(ctx context.Context, q Q, id int64) (*CustomField, error) {
	f := &CustomField{}
	var opts string
	err := q.QueryRowContext(ctx, `SELECT id, workspace_id, name, kind, options, position, created_at, version FROM custom_fields
		WHERE id = ? AND deleted_at IS NULL`, id).Scan(&f.ID, &f.WorkspaceID, &f.Name, &f.Kind, &opts, &f.Position, &f.CreatedAt, &f.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	_ = json.Unmarshal([]byte(opts), &f.Options)
	return f, err
}

// SaveCustomField creates (ID 0) or updates a field.
func SaveCustomField(ctx context.Context, q Q, a Actor, f *CustomField, expect int64) (*CustomField, error) {
	if err := f.validate(); err != nil {
		return nil, err
	}
	var n int
	_ = q.QueryRowContext(ctx, `SELECT count(*) FROM custom_fields WHERE workspace_id = ? AND deleted_at IS NULL AND id != ?`, f.WorkspaceID, f.ID).Scan(&n)
	if n >= 30 {
		return nil, Invalid("a workspace has at most 30 custom fields")
	}
	var before *CustomField
	if f.ID == 0 {
		var pos float64
		_ = q.QueryRowContext(ctx, `SELECT COALESCE(max(position), 0) + 1024 FROM custom_fields WHERE workspace_id = ?`, f.WorkspaceID).Scan(&pos)
		res, err := q.ExecContext(ctx, `INSERT INTO custom_fields (workspace_id, name, kind, options, position, created_at) VALUES (?,?,?,?,?,?)`,
			f.WorkspaceID, f.Name, f.Kind, mustJSON(f.Options), pos, Now())
		if err != nil {
			if isUnique(err) {
				return nil, Conflict("a field called %q exists", f.Name)
			}
			return nil, err
		}
		f.ID, _ = res.LastInsertId()
	} else {
		cur, err := GetCustomField(ctx, q, f.ID)
		if err != nil {
			return nil, err
		}
		before = cur
		if expect != 0 && expect != cur.Version {
			return nil, ErrVersion
		}
		if cur.Kind != f.Kind {
			return nil, Invalid("a field's kind can't change; make a new field")
		}
		if _, err := q.ExecContext(ctx, `UPDATE custom_fields SET name = ?, options = ?, version = version + 1 WHERE id = ?`, f.Name, mustJSON(f.Options), f.ID); err != nil {
			if isUnique(err) {
				return nil, Conflict("a field called %q exists", f.Name)
			}
			return nil, err
		}
	}
	after, err := GetCustomField(ctx, q, f.ID)
	if err != nil {
		return nil, err
	}
	action, data := "field.created", AuditSnapshot("field", after, nil)
	if before != nil {
		action, data = "field.updated", AuditDiff("field", before, after)
	}
	if _, err := Activity(ctx, q, a, &f.WorkspaceID, nil, action, "field", f.ID, data); err != nil {
		return nil, err
	}
	return after, nil
}

// DeleteCustomField hides a field (its values go with it).
func DeleteCustomField(ctx context.Context, q Q, a Actor, f *CustomField) error {
	if _, err := q.ExecContext(ctx, `UPDATE custom_fields SET deleted_at = ?, name = name || ' #' || id WHERE id = ?`, Now(), f.ID); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM gig_custom_values WHERE field_id = ?`, f.ID); err != nil {
		return err
	}
	_, err := Activity(ctx, q, a, &f.WorkspaceID, nil, "field.deleted", "field", f.ID, AuditSnapshot("field", f, nil))
	return err
}

// normValue checks a value against its field; nil clears.
func (f *CustomField) normValue(v any) (any, error) {
	bad := func(want string) error { return Invalid("%s: expects %s", f.Name, want) }
	switch f.Kind {
	case "text":
		s, ok := v.(string)
		if !ok || len(s) > 2000 {
			return nil, bad("text up to 2000 characters")
		}
		return s, nil
	case "number":
		var x float64
		var err error
		switch n := v.(type) {
		case float64:
			x = n
		case json.Number:
			x, err = n.Float64()
		case string:
			x, err = strconv.ParseFloat(strings.TrimSpace(n), 64)
		default:
			err = errors.New("not a number")
		}
		if err != nil || math.IsNaN(x) || math.IsInf(x, 0) {
			return nil, bad("a finite number")
		}
		return x, nil
	case "date":
		switch d := v.(type) {
		case float64:
			return int64(d), nil
		case string:
			if t, err := time.Parse("2006-01-02", d); err == nil {
				return t.UnixMilli(), nil
			}
		}
		return nil, bad("unix ms or YYYY-MM-DD")
	case "select":
		s, ok := v.(string)
		for _, o := range f.Options {
			if ok && o == s {
				return s, nil
			}
		}
		return nil, bad("one of " + strings.Join(f.Options, ", "))
	case "checkbox":
		b, ok := v.(bool)
		if !ok {
			return nil, bad("true or false")
		}
		return b, nil
	case "url":
		s, ok := v.(string)
		u, err := url.Parse(s)
		if !ok || err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || len(s) > 2000 {
			return nil, bad("an http(s) URL")
		}
		return s, nil
	}
	return nil, bad("a value")
}

// fieldCond: the SQL condition (on gigs t) for one custom field filter, "<field id>:<value>". The
// value's meaning follows the field: a choice matches exactly; a checkbox takes true or false (false:
// not ticked, whether set or not); a number takes 5, >=5, <=5, >5 or <5; text and links contain it
// (ASCII case ignored, like the text search). An empty value matches gigs without a value for it.
// Dates aren't filtered this way.
func fieldCond(ctx context.Context, q Q, ws int64, spec string) (string, []any, error) {
	idStr, val, ok := strings.Cut(spec, ":")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if !ok || err != nil {
		return "", nil, Invalid("field: filters are <field id>:<value>")
	}
	fd, err := GetCustomField(ctx, q, id)
	if err != nil || fd.WorkspaceID != ws {
		return "", nil, Invalid("field: no custom field %d in this workspace", id)
	}
	const has = "EXISTS (SELECT 1 FROM gig_custom_values cv WHERE cv.gig_id = t.id AND cv.field_id = ?"
	if val == "" {
		return "NOT " + has + ")", []any{id}, nil
	}
	switch fd.Kind {
	case "select":
		return has + " AND cv.value = ?)", []any{id, mustJSON(val)}, nil
	case "checkbox":
		switch val {
		case "true":
			return has + " AND cv.value = 'true')", []any{id}, nil
		case "false":
			return "NOT " + has + " AND cv.value = 'true')", []any{id}, nil
		}
		return "", nil, Invalid("field: %s takes true or false", fd.Name)
	case "number":
		op, num := "=", val
		for _, o := range []string{">=", "<=", ">", "<", "="} {
			if strings.HasPrefix(val, o) {
				op, num = o, val[len(o):]
				break
			}
		}
		x, err := strconv.ParseFloat(strings.TrimSpace(num), 64)
		if err != nil || math.IsNaN(x) || math.IsInf(x, 0) {
			return "", nil, Invalid("field: %s takes a number, or >=, <=, > or < and a number", fd.Name)
		}
		// DOUBLE PRECISION: a float in PostgreSQL, REAL affinity in SQLite
		return has + " AND CAST(cv.value AS DOUBLE PRECISION) " + op + " ?)", []any{id, x}, nil
	case "text", "url":
		// values are stored as JSON strings: match the text as JSON writes it (quotes and escapes)
		enc := mustJSON(val)
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(enc[1:len(enc)-1]) + "%"
		return has + ` AND cv.value LIKE ? ESCAPE '\')`, []any{id, like}, nil
	}
	return "", nil, Invalid("field: %s fields can't be filtered", fd.Kind)
}

// SetCustomValues sets (or, with nil, clears) a gig's custom values; keys are field ids.
func SetCustomValues(ctx context.Context, q Q, a Actor, t *Gig, vals map[string]any) error {
	changed := map[string]any{}
	if len(vals) > 30 {
		return Invalid("at most 30 custom values at once")
	}
	for k, v := range vals {
		id, err := strconv.ParseInt(k, 10, 64)
		if err != nil || strconv.FormatInt(id, 10) != k {
			return Invalid("custom: keys are field ids")
		}
		f, err := GetCustomField(ctx, q, id)
		if err != nil || f.WorkspaceID != t.WorkspaceID {
			return Invalid("no custom field %s in this workspace", k)
		}
		if v == nil {
			if _, err := q.ExecContext(ctx, `DELETE FROM gig_custom_values WHERE gig_id = ? AND field_id = ?`, t.ID, id); err != nil {
				return err
			}
			changed[f.Name] = nil
			continue
		}
		nv, err := f.normValue(v)
		if err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO gig_custom_values (gig_id, field_id, value) VALUES (?,?,?)
			ON CONFLICT (gig_id, field_id) DO UPDATE SET value = excluded.value`, t.ID, id, mustJSON(nv)); err != nil {
			return err
		}
		changed[f.Name] = nv
	}
	if len(changed) == 0 {
		return nil
	}
	return touchGig(ctx, q, a, t, "gig.custom", changed)
}

// ---------------------------------------------------------------- templates

// TemplateSubgig is a subgig every gig made from a template gets.
type TemplateSubgig struct {
	Title        string `json:"title" doc:"The subgig's title"`
	BlocksParent bool   `json:"blocks_parent,omitempty" doc:"The parent gig waits (isn't handed out) until this subgig is done"`
}

// Template is a reusable gig blueprint.
type Template struct {
	ID            int64            `json:"id"`
	WorkspaceID   int64            `json:"workspace_id" doc:"0 for a shared template"`
	Shared        bool             `json:"shared" doc:"Shared by every workspace (admins make and change it; recurring gigs set their own assignees)"`
	ProjectID     *int64           `json:"project_id" doc:"null = usable in any project of the workspace"`
	Name          string           `json:"name"`
	Title         string           `json:"title"`
	DescriptionMD string           `json:"description_md"`
	Priority      int64            `json:"priority"`
	Color         *string          `json:"color"`
	Labels        []string         `json:"labels"`
	Assignees     []int64          `json:"assignees"`
	Followers     []int64          `json:"followers" doc:"Users who follow each gig made from it (besides the creator's own setting)"`
	Checklist     []string         `json:"checklist"`
	Subgigs       []TemplateSubgig `json:"subgigs" doc:"Subgigs each gig made from it gets"`
	Custom        map[string]any   `json:"custom"`
	Model         string           `json:"model"`
	Effort        string           `json:"effort"`
	AlwaysRun     bool             `json:"always_run"`
	AllowedTools  []string         `json:"allowed_tools" doc:"Tool allowlist for the runner (permission rules); empty = the runner's usual mode"`
	RunTimeout    *int64           `json:"run_timeout_minutes" doc:"Its gigs' run timeout in minutes (null: the agent's default)"`
	AutoArchive   *int64           `json:"auto_archive_minutes" doc:"Gigs made from it archive themselves this many minutes after done or cancelled (null: never)"`
	StartCategory string           `json:"start_category" enum:"backlog,todo," doc:"Where its gigs start: backlog or todo (the first status of that category in the gig's project), or empty for the project's default status"`
	CreatedBy     int64            `json:"created_by"`
	CreatedAt     int64            `json:"created_at"`
	Version       int64            `json:"version"`
	Recurring     int64            `json:"recurring" doc:"Live recurring gigs that create their gigs from this template"`
}

const tplCols = `id, COALESCE(workspace_id, 0), project_id, name, title, description_md, priority, color, labels, assignees, followers, checklist, custom, created_by, created_at, version, model, effort, always_run, allowed_tools, subgigs, auto_archive_minutes, run_timeout_minutes,
	(SELECT count(*) FROM recurrences r WHERE r.template_id = gig_templates.id AND r.deleted_at IS NULL), start_category`

func scanTemplate(row interface{ Scan(...any) error }) (*Template, error) {
	t := &Template{}
	var proj, autoArch, runTO sql.NullInt64
	var color sql.NullString
	var labels, assignees, followers, checklist, custom, tools, subs string
	if err := row.Scan(&t.ID, &t.WorkspaceID, &proj, &t.Name, &t.Title, &t.DescriptionMD, &t.Priority, &color, &labels, &assignees, &followers, &checklist, &custom,
		&t.CreatedBy, &t.CreatedAt, &t.Version, &t.Model, &t.Effort, &t.AlwaysRun, &tools, &subs, &autoArch, &runTO, &t.Recurring, &t.StartCategory); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	t.ProjectID, t.Color, t.AutoArchive, t.RunTimeout = ptrInt(proj), ptrStr(color), ptrInt(autoArch), ptrInt(runTO)
	t.Shared = t.WorkspaceID == 0
	_ = json.Unmarshal([]byte(labels), &t.Labels)
	_ = json.Unmarshal([]byte(assignees), &t.Assignees)
	t.Followers = []int64{}
	_ = json.Unmarshal([]byte(followers), &t.Followers)
	_ = json.Unmarshal([]byte(checklist), &t.Checklist)
	_ = json.Unmarshal([]byte(custom), &t.Custom)
	t.AllowedTools = []string{}
	_ = json.Unmarshal([]byte(tools), &t.AllowedTools)
	t.Subgigs = []TemplateSubgig{}
	_ = json.Unmarshal([]byte(subs), &t.Subgigs)
	return t, nil
}

// normSubgigs trims and checks a template's subgigs (nil becomes empty).
func normSubgigs(subs []TemplateSubgig) ([]TemplateSubgig, error) {
	if len(subs) > 50 {
		return nil, Invalid("at most 50 subgigs")
	}
	out := make([]TemplateSubgig, 0, len(subs))
	for _, s := range subs {
		s.Title = strings.TrimSpace(s.Title)
		if err := validGigText(s.Title, ""); err != nil {
			return nil, Invalid("subgig title: 1-250 characters")
		}
		out = append(out, s)
	}
	return out, nil
}

func (t *Template) validate() error {
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" || len(t.Name) > 80 {
		return Invalid("template name: 1-80 characters")
	}
	if err := validGigText(t.Title, t.DescriptionMD); err != nil {
		return err
	}
	if t.Priority < 0 || t.Priority > 4 {
		return Invalid("priority is 0-4")
	}
	t.Model = strings.TrimSpace(t.Model)
	if err := ValidRunSettings(t.Model, t.Effort); err != nil {
		return err
	}
	tools, err := NormTools(t.AllowedTools)
	if err != nil {
		return err
	}
	t.AllowedTools = tools
	if t.Color != nil {
		c, err := NormColor(*t.Color)
		if err != nil {
			return err
		}
		t.Color = &c
	}
	if len(t.Checklist) > 200 {
		return Invalid("at most 200 checklist items")
	}
	subs, err := normSubgigs(t.Subgigs)
	if err != nil {
		return err
	}
	t.Subgigs = subs
	for _, c := range t.Checklist {
		if _, err := validItemText(c); err != nil {
			return err
		}
	}
	if t.Labels == nil {
		t.Labels = []string{}
	}
	if t.Assignees == nil {
		t.Assignees = []int64{}
	}
	t.Followers = uniqIDs(t.Followers)
	if len(t.Followers) > MaxFollowers {
		return Invalid("at most %d followers", MaxFollowers)
	}
	if t.Checklist == nil {
		t.Checklist = []string{}
	}
	if t.Custom == nil {
		t.Custom = map[string]any{}
	}
	if len(t.Custom) > 30 {
		return Invalid("at most 30 custom values")
	}
	if err := validAutoArchive(t.AutoArchive); err != nil {
		return err
	}
	if err := validRunTimeout(t.RunTimeout); err != nil {
		return err
	}
	if !validStartCategory(t.StartCategory) {
		return Invalid("start_category: backlog, todo, or empty for the project's default status")
	}
	if t.WorkspaceID == 0 { // shared: nothing that belongs to one workspace
		switch {
		case t.ProjectID != nil:
			return Invalid("a shared template is for any project")
		case len(t.Custom) > 0:
			return Invalid("a shared template has no custom field values (fields belong to a workspace)")
		case len(t.Assignees) > 0:
			return Invalid("a shared template has no assignees: each recurring gig that uses it sets its own")
		case len(t.Followers) > 0:
			return Invalid("a shared template has no followers: each recurring gig that uses it sets its own")
		}
	}
	return validTemplateLists(t.Labels, t.Assignees, 0, nil)
}

// Templates lists the templates a workspace can use: its own (narrowed to projects when given;
// workspace-wide ones always show), then the shared ones. ws 0: only the shared ones.
func Templates(ctx context.Context, q Q, ws int64, projects []int64) ([]*Template, error) {
	where, args := "(workspace_id = ? OR workspace_id IS NULL) AND deleted_at IS NULL", []any{ws}
	if projects != nil {
		where += " AND (project_id IS NULL OR project_id IN (" + placeholders(len(projects)) + "))"
		args = append(args, int64Args(projects)...)
	}
	rows, err := q.QueryContext(ctx, "SELECT "+tplCols+" FROM gig_templates WHERE "+where+" ORDER BY workspace_id IS NULL, name", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Template{}
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetTemplate loads a live template.
func GetTemplate(ctx context.Context, q Q, id int64) (*Template, error) {
	return scanTemplate(q.QueryRowContext(ctx, "SELECT "+tplCols+" FROM gig_templates WHERE id = ? AND deleted_at IS NULL", id))
}

// SaveTemplate creates (ID 0) or replaces a template.
func SaveTemplate(ctx context.Context, q Q, a Actor, t *Template, expect int64) (*Template, error) {
	if err := t.validate(); err != nil {
		return nil, err
	}
	if t.ProjectID != nil {
		p, err := GetProject(ctx, q, *t.ProjectID)
		if err != nil || t.WorkspaceID == 0 || p.WorkspaceID != t.WorkspaceID {
			return nil, Invalid("no such project in this workspace")
		}
		// its gigs' start status must exist there (checked after the project is known to be ours)
		if _, err := startStatus(ctx, q, *t.ProjectID, t.StartCategory); err != nil {
			return nil, err
		}
		if t.ID != 0 {
			var n int
			_ = q.QueryRowContext(ctx, `SELECT count(*) FROM recurrences WHERE template_id = ? AND deleted_at IS NULL AND project_id != ?`, t.ID, *t.ProjectID).Scan(&n)
			if n > 0 {
				return nil, Conflict("recurring gigs in other projects use this template; keep it for any project or for theirs")
			}
		}
	}
	// a template for any project (or shared): the projects its recurring gigs run in must have the
	// start status too, or they would pause at their next occurrence
	if t.ID != 0 && t.StartCategory != "" {
		var n int
		if err := q.QueryRowContext(ctx, `SELECT count(*) FROM recurrences r WHERE r.template_id = ? AND r.deleted_at IS NULL
			AND NOT EXISTS (SELECT 1 FROM statuses s WHERE s.project_id = r.project_id AND s.category = ?)`, t.ID, t.StartCategory).Scan(&n); err != nil {
			return nil, err
		}
		if n > 0 {
			return nil, Invalid("start_category: %d recurring gig(s) using this template are in projects without a %s status; add one there first", n, t.StartCategory)
		}
	}
	// custom values must fit this workspace's fields (as stored on a gig)
	for k, v := range t.Custom {
		id, err := strconv.ParseInt(k, 10, 64)
		if err != nil || strconv.FormatInt(id, 10) != k {
			return nil, Invalid("custom: keys are field ids")
		}
		f, err := GetCustomField(ctx, q, id)
		if err != nil || t.WorkspaceID == 0 || f.WorkspaceID != t.WorkspaceID {
			return nil, Invalid("no custom field %s in this workspace", k)
		}
		if v != nil {
			nv, err := f.normValue(v)
			if err != nil {
				return nil, err
			}
			t.Custom[k] = nv
		}
	}
	if t.ID == 0 {
		var n int
		_ = q.QueryRowContext(ctx, `SELECT count(*) FROM gig_templates WHERE workspace_id IS ? AND deleted_at IS NULL`, wsArg(t.WorkspaceID)).Scan(&n)
		if n >= 200 {
			return nil, Invalid("a workspace (and the shared library) has at most 200 templates")
		}
	}
	var before *Template
	if t.ID == 0 {
		res, err := q.ExecContext(ctx, `INSERT INTO gig_templates (workspace_id, project_id, name, title, description_md, priority, color, labels, assignees, followers,
			checklist, custom, created_by, created_at, model, effort, always_run, allowed_tools, subgigs, auto_archive_minutes, run_timeout_minutes, start_category) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, wsArg(t.WorkspaceID), nullInt(t.ProjectID), t.Name, t.Title, t.DescriptionMD,
			t.Priority, nullStr(t.Color), mustJSON(t.Labels), mustJSON(t.Assignees), mustJSON(t.Followers), mustJSON(t.Checklist), mustJSON(t.Custom), a.UserID, Now(), t.Model, t.Effort, t.AlwaysRun, mustJSON(t.AllowedTools), mustJSON(t.Subgigs), nullInt(t.AutoArchive), nullInt(t.RunTimeout), t.StartCategory)
		if err != nil {
			return nil, err
		}
		t.ID, _ = res.LastInsertId()
	} else {
		cur, err := GetTemplate(ctx, q, t.ID)
		if err != nil {
			return nil, err
		}
		before = cur
		if expect != 0 && expect != cur.Version {
			return nil, ErrVersion
		}
		if _, err := q.ExecContext(ctx, `UPDATE gig_templates SET project_id = ?, name = ?, title = ?, description_md = ?, priority = ?, color = ?, labels = ?,
			assignees = ?, followers = ?, checklist = ?, custom = ?, model = ?, effort = ?, always_run = ?, allowed_tools = ?, subgigs = ?, auto_archive_minutes = ?, run_timeout_minutes = ?, start_category = ?, version = version + 1 WHERE id = ?`, nullInt(t.ProjectID), t.Name, t.Title, t.DescriptionMD, t.Priority,
			nullStr(t.Color), mustJSON(t.Labels), mustJSON(t.Assignees), mustJSON(t.Followers), mustJSON(t.Checklist), mustJSON(t.Custom), t.Model, t.Effort, t.AlwaysRun, mustJSON(t.AllowedTools), mustJSON(t.Subgigs), nullInt(t.AutoArchive), nullInt(t.RunTimeout), t.StartCategory, t.ID); err != nil {
			return nil, err
		}
	}
	after, err := GetTemplate(ctx, q, t.ID)
	if err != nil {
		return nil, err
	}
	action, data := "template.created", AuditSnapshot("template", after, nil)
	if before != nil {
		action, data = "template.updated", AuditDiff("template", before, after)
	}
	if _, err := Activity(ctx, q, a, wsRef(t.WorkspaceID), t.ProjectID, action, "template", t.ID, data); err != nil {
		return nil, err
	}
	return after, nil
}

// DeleteTemplate removes a template (gigs made from it stay). A template recurring gigs use stays.
func DeleteTemplate(ctx context.Context, q Q, a Actor, t *Template) error {
	if names := templateSchedules(ctx, q, t.ID); len(names) > 0 {
		return Conflict("recurring gigs use this template: %s. Delete them or give them another template first", strings.Join(names, ", "))
	}
	if _, err := q.ExecContext(ctx, `UPDATE gig_templates SET deleted_at = ? WHERE id = ?`, Now(), t.ID); err != nil {
		return err
	}
	_, err := Activity(ctx, q, a, wsRef(t.WorkspaceID), t.ProjectID, "template.deleted", "template", t.ID, AuditSnapshot("template", t, nil))
	return err
}

// templateSchedules names the live recurring gigs using a template.
func templateSchedules(ctx context.Context, q Q, id int64) []string {
	rows, err := q.QueryContext(ctx, `SELECT r.id, t.title FROM recurrences r JOIN gig_templates t ON t.id = r.template_id
		WHERE r.template_id = ? AND r.deleted_at IS NULL ORDER BY r.id LIMIT 10`, id)
	if err != nil {
		return []string{"(unknown)"}
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var rid int64
		var title string
		if rows.Scan(&rid, &title) == nil {
			out = append(out, fmt.Sprintf("%q (#%d)", title, rid))
		}
	}
	return out
}

// AddSubgigs creates subgigs of t, each made like t: same project, priority, assignees, followers,
// window and run settings. The assignees aren't notified again for each one (they were for t).
func AddSubgigs(ctx context.Context, q Q, a Actor, t *Gig, subs []TemplateSubgig) error {
	subs, err := normSubgigs(subs)
	if err != nil {
		return err
	}
	// a gig waiting in Backlog keeps its subgigs there too, out of a runner's reach; otherwise they
	// start in the project's default status
	var status *int64
	if t.StatusCategory == "backlog" {
		status = &t.StatusID
	}
	for _, s := range subs {
		parent := t.ID
		if _, err := CreateGig(ctx, q, a, NewGig{ProjectID: t.ProjectID, Title: s.Title, Priority: t.Priority, ParentID: &parent, StatusID: status,
			BlocksParent: s.BlocksParent, StartAt: t.StartAt, DueAt: t.DueAt, NotAfter: t.NotAfter, Assignees: t.Assignees,
			Followers: whoSee(ctx, q, t.WorkspaceID, t.ProjectID, t.Followers, MaxFollowers), CreatedViaKey: t.CreatedViaKeyID, Model: t.Model, Effort: t.Effort, AlwaysRun: t.AlwaysRun, AllowedTools: t.AllowedTools,
			Quiet: true}); err != nil {
			return err
		}
	}
	return nil
}

// templateExtras adds a template's checklist, custom values and subgigs to a gig made from it
// (custom fields deleted since the template was saved are dropped). Each subgig is made like the
// gig: same project, priority, assignees, window and run settings.
func templateExtras(ctx context.Context, q Q, a Actor, t *Gig, checklist []string, custom map[string]any, subs []TemplateSubgig) error {
	if err := AddSubgigs(ctx, q, a, t, subs); err != nil {
		return err
	}
	if len(checklist) > 0 {
		if err := AddChecklistItems(ctx, q, a, t, checklist); err != nil {
			return err
		}
	}
	if len(custom) == 0 {
		return nil
	}
	vals := map[string]any{}
	for k, v := range custom {
		if id, err := strconv.ParseInt(k, 10, 64); err == nil {
			if f, err := GetCustomField(ctx, q, id); err == nil && f.WorkspaceID == t.WorkspaceID {
				vals[k] = v
			}
		}
	}
	return SetCustomValues(ctx, q, a, t, vals)
}

// GigFromTemplate creates a gig in project p from template tpl (fields in over override the template).
func GigFromTemplate(ctx context.Context, q Q, a Actor, tpl *Template, p *Project, title *string, viaKey *int64) (*Gig, error) {
	if (tpl.WorkspaceID != 0 && tpl.WorkspaceID != p.WorkspaceID) || (tpl.ProjectID != nil && *tpl.ProjectID != p.ID) {
		return nil, Invalid("this template isn't for that project")
	}
	n := NewGig{ProjectID: p.ID, Title: tpl.Title, DescriptionMD: tpl.DescriptionMD, Priority: tpl.Priority, Color: tpl.Color,
		Model: tpl.Model, Effort: tpl.Effort, AlwaysRun: tpl.AlwaysRun, AllowedTools: tpl.AllowedTools, AutoArchive: tpl.AutoArchive, RunTimeout: tpl.RunTimeout,
		Labels: tpl.Labels, Assignees: validAssignees(ctx, q, &Recurrence{WorkspaceID: p.WorkspaceID, Assignees: tpl.Assignees}),
		Followers: whoSee(ctx, q, p.WorkspaceID, p.ID, tpl.Followers, MaxFollowers), CreatedViaKey: viaKey}
	var err error
	if n.StatusID, err = startStatus(ctx, q, p.ID, tpl.StartCategory); err != nil {
		return nil, err
	}
	if title != nil && strings.TrimSpace(*title) != "" {
		n.Title = *title
	}
	t, err := CreateGig(ctx, q, a, n)
	if err != nil {
		return nil, err
	}
	if err := templateExtras(ctx, q, a, t, tpl.Checklist, tpl.Custom, tpl.Subgigs); err != nil {
		return nil, err
	}
	if _, err := Activity(ctx, q, a, &t.WorkspaceID, &t.ProjectID, "gig.from_template", "gig", t.ID, map[string]any{"template": tpl.ID, "name": tpl.Name}); err != nil {
		return nil, err
	}
	return GetGig(ctx, q, t.ID)
}

// ---------------------------------------------------------------- saved filters

// SavedFilter is a personal named view (the query is the list endpoint's parameters).
type SavedFilter struct {
	ID          int64             `json:"id"`
	WorkspaceID *int64            `json:"workspace_id"`
	Name        string            `json:"name"`
	Query       map[string]string `json:"query" doc:"List parameters, e.g. {\"label\":\"ops\",\"category\":\"todo\"}"`
	Position    float64           `json:"position"`
	CreatedAt   int64             `json:"created_at"`
}

// SavedFilters lists a user's views.
func SavedFilters(ctx context.Context, q Q, user int64) ([]*SavedFilter, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, workspace_id, name, query, position, created_at FROM saved_filters WHERE user_id = ? ORDER BY position, id`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*SavedFilter{}
	for rows.Next() {
		f := &SavedFilter{}
		var ws sql.NullInt64
		var qs string
		if err := rows.Scan(&f.ID, &ws, &f.Name, &qs, &f.Position, &f.CreatedAt); err != nil {
			return nil, err
		}
		f.WorkspaceID = ptrInt(ws)
		_ = json.Unmarshal([]byte(qs), &f.Query)
		out = append(out, f)
	}
	return out, rows.Err()
}

var filterKeys = map[string]bool{"q": true, "category": true, "status": true, "label": true, "priority_min": true, "assignee": true,
	"created_by": true, "due_before": true, "sort": true, "project": true, "archived": true, "blocked": true, "recurring": true, "always_run": true, "mine": true,
	"cols": true} // cols: the custom field columns a table shows (field ids, comma-separated)

// filterKey: a key a saved filter may hold: one of filterKeys, or field.<field id> (a custom field
// filter, its value as the list's field=<id>:<value> takes it).
func filterKey(k string) bool {
	if id, ok := strings.CutPrefix(k, "field."); ok {
		n, err := strconv.ParseInt(id, 10, 64)
		return err == nil && n > 0 && strconv.FormatInt(n, 10) == id
	}
	return filterKeys[k]
}

// SaveFilter creates (ID 0) or replaces one of the user's views.
func SaveFilter(ctx context.Context, q Q, user int64, f *SavedFilter) (*SavedFilter, error) {
	f.Name = strings.TrimSpace(f.Name)
	if f.Name == "" || len(f.Name) > 60 {
		return nil, Invalid("name: 1-60 characters")
	}
	for k, v := range f.Query {
		if !filterKey(k) || len(v) > 200 {
			return nil, Invalid("unknown or too long filter parameter %q", k)
		}
	}
	if f.ID == 0 {
		var n int
		_ = q.QueryRowContext(ctx, `SELECT count(*) FROM saved_filters WHERE user_id = ?`, user).Scan(&n)
		if n >= 100 {
			return nil, Invalid("at most 100 saved filters")
		}
		var pos float64
		_ = q.QueryRowContext(ctx, `SELECT COALESCE(max(position), 0) + 1024 FROM saved_filters WHERE user_id = ?`, user).Scan(&pos)
		res, err := q.ExecContext(ctx, `INSERT INTO saved_filters (user_id, workspace_id, name, query, position, created_at) VALUES (?,?,?,?,?,?)`,
			user, nullInt(f.WorkspaceID), f.Name, mustJSON(f.Query), pos, Now())
		if err != nil {
			return nil, err
		}
		f.ID, _ = res.LastInsertId()
	} else {
		res, err := q.ExecContext(ctx, `UPDATE saved_filters SET workspace_id = ?, name = ?, query = ? WHERE id = ? AND user_id = ?`,
			nullInt(f.WorkspaceID), f.Name, mustJSON(f.Query), f.ID, user)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, ErrNotFound
		}
	}
	all, err := SavedFilters(ctx, q, user)
	if err != nil {
		return nil, err
	}
	for _, x := range all {
		if x.ID == f.ID {
			return x, nil
		}
	}
	return nil, ErrNotFound
}

// DeleteFilter removes one of the user's views.
func DeleteFilter(ctx context.Context, q Q, user, id int64) error {
	res, err := q.ExecContext(ctx, `DELETE FROM saved_filters WHERE id = ? AND user_id = ?`, id, user)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------- notifications

// Notification is one bell entry.
type Notification struct {
	ID          int64   `json:"id"`
	WorkspaceID int64   `json:"workspace_id"`
	GigID       *int64  `json:"gig_id"`
	GigKey      *string `json:"gig_key"`
	GigTitle    *string `json:"gig_title"`
	CommentID   *int64  `json:"comment_id"`
	Kind        string  `json:"kind" doc:"mention, assigned, question (needs a person), failed (a run ended in an error or timed out), missed or comment (a report)"`
	ActorID     *int64  `json:"actor_user_id"`
	CreatedAt   int64   `json:"created_at"`
	ReadAt      *int64  `json:"read_at"`
}

// Notify records a bell entry for each user (never for the actor themself), deduplicated per
// (user, gig, comment, kind). Users who can no longer see the gig, and users whose bell settings
// leave this kind out, are skipped.
func Notify(ctx context.Context, q Q, a Actor, t *Gig, commentID *int64, kind string, users []int64) error {
	for _, u := range uniqIDs(users) {
		if u == a.UserID {
			continue
		}
		if !UserHasIn(ctx, q, u, t.WorkspaceID, t.ProjectID, "view") || !BellAllows(ctx, q, u, kind) {
			continue
		}
		var dup int
		_ = q.QueryRowContext(ctx, `SELECT count(*) FROM notifications WHERE user_id = ? AND gig_id = ? AND kind = ? AND comment_id IS ? AND read_at IS NULL`,
			u, t.ID, kind, nullInt(commentID)).Scan(&dup)
		if dup > 0 {
			continue
		}
		var actor any
		if a.UserID != 0 {
			actor = a.UserID
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO notifications (user_id, workspace_id, gig_id, comment_id, kind, actor_user_id, created_at) VALUES (?,?,?,?,?,?,?)`,
			u, t.WorkspaceID, t.ID, nullInt(commentID), kind, actor, Now()); err != nil {
			return err
		}
	}
	return nil
}

var mentionRe = regexp.MustCompile(`(?:^|[^\w@/.])@([a-z0-9][a-z0-9_.-]{0,63})`)

// MentionedUsers returns the ids of existing users @mentioned in Markdown text (code spans ignored).
func MentionedUsers(ctx context.Context, q Q, text string) []int64 {
	text = stripCode(text)
	seen := map[string]bool{}
	var out []int64
	for _, m := range mentionRe.FindAllStringSubmatch(strings.ToLower(text), 50) {
		name := strings.TrimRight(m[1], ".-")
		if seen[name] {
			continue
		}
		seen[name] = true
		var id int64
		if err := q.QueryRowContext(ctx, `SELECT id FROM users WHERE username = ?`, name).Scan(&id); err == nil {
			out = append(out, id)
		}
	}
	return out
}

func stripCode(s string) string {
	var b strings.Builder
	fence := false
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fence = !fence
			continue
		}
		if fence {
			continue
		}
		parts := strings.Split(line, "`")
		for i, p := range parts {
			if i%2 == 0 {
				b.WriteString(p)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// Notifications lists a user's bell entries in reach (newest first).
func Notifications(ctx context.Context, q Q, user int64, workspaces []int64, unreadOnly bool, limit int) ([]*Notification, int, error) {
	where, args := "n.user_id = ?", []any{user}
	if workspaces != nil {
		if len(workspaces) == 0 {
			return []*Notification{}, 0, nil
		}
		where += " AND n.workspace_id IN (" + placeholders(len(workspaces)) + ")"
		args = append(args, int64Args(workspaces)...)
	}
	var unread int
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM notifications n WHERE "+where+" AND n.read_at IS NULL", args...).Scan(&unread); err != nil {
		return nil, 0, err
	}
	if unreadOnly {
		where += " AND n.read_at IS NULL"
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := q.QueryContext(ctx, `SELECT n.id, n.workspace_id, n.gig_id, p.key || '-' || t.number, t.title, n.comment_id, n.kind, n.actor_user_id, n.created_at, n.read_at
		FROM notifications n LEFT JOIN gigs t ON t.id = n.gig_id AND t.deleted_at IS NULL LEFT JOIN projects p ON p.id = t.project_id
		WHERE `+where+` ORDER BY n.id DESC LIMIT `+strconv.Itoa(limit), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*Notification{}
	for rows.Next() {
		n := &Notification{}
		var gig, comment, actor, read sql.NullInt64
		var key, title sql.NullString
		if err := rows.Scan(&n.ID, &n.WorkspaceID, &gig, &key, &title, &comment, &n.Kind, &actor, &n.CreatedAt, &read); err != nil {
			return nil, 0, err
		}
		n.GigID, n.CommentID, n.ActorID, n.ReadAt = ptrInt(gig), ptrInt(comment), ptrInt(actor), ptrInt(read)
		n.GigKey, n.GigTitle = ptrStr(key), ptrStr(title)
		out = append(out, n)
	}
	return out, unread, rows.Err()
}

// MarkRead marks some (ids) or all of a user's notifications read.
// workspaces limits it to those workspaces (nil = all).
func MarkRead(ctx context.Context, q Q, user int64, workspaces, ids []int64) (int64, error) {
	where, args := "user_id = ? AND read_at IS NULL", []any{Now(), user}
	if workspaces != nil {
		if len(workspaces) == 0 {
			return 0, nil
		}
		where += " AND workspace_id IN (" + placeholders(len(workspaces)) + ")"
		args = append(args, int64Args(workspaces)...)
	}
	if ids != nil {
		if len(ids) == 0 {
			return 0, nil
		}
		where += " AND id IN (" + placeholders(len(ids)) + ")"
		args = append(args, int64Args(ids)...)
	}
	res, err := q.ExecContext(ctx, `UPDATE notifications SET read_at = ? WHERE `+where, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
