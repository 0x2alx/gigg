package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
)

// Make shared: one step that turns a workspace's schedule, gig template or errand template into a
// shared one and removes the identical copies the workspaces piled up (what used to be done by hand:
// make a shared item, repoint every user of each copy, delete the copies).
//
// What "identical" means, per kind: the content fields are equal; ignored are the id, the workspace, the
// name, the timestamps, the creator and the version.
//   - Schedule: the timing, that is every field of the Rule (freq, interval, weekdays, month_day, tz,
//     window_start, window_minutes, lead_minutes), compared as Rule.Same does.
//   - Gig template: project (a template for one project is not the same as one for any), title,
//     description, priority, color, labels, checklist, subgigs, custom values, followers, model, effort,
//     always_run, allowed_tools (as a set), auto_archive_minutes and run_timeout_minutes. Left out:
//     assignees. They name people of one workspace's projects and a shared template holds none: every
//     recurring gig moved to the shared template keeps the assignees it had (as its own), so two copies
//     that differ only in who they assign are still the same template.
//   - Errand template: description, command, timeout_s, output and unsandboxed, and the provenance: who
//     set the command (command_set_by) and who turned the sandbox off (unsandboxed_set_by). A copy whose
//     command was written, or sandbox turned off, by someone else is not the same: merging it would
//     give that command the source's author. The new shared template keeps the source's provenance.
//
// A shared template holds no project, custom values, assignees or followers, so a source with a project,
// custom values or followers can't be made shared (it is refused, saying what to clear).

// The kinds Make shared handles.
const (
	ShareSchedule       = "schedule"
	ShareTemplate       = "template"
	ShareErrandTemplate = "errand_template"
)

// ShareItem is a workspace's schedule or template as the plan lists it.
type ShareItem struct {
	ID          int64  `json:"id"`
	WorkspaceID int64  `json:"workspace_id"`
	Workspace   string `json:"workspace" doc:"The workspace's name"`
	Name        string `json:"name"`
	Recurring   int64  `json:"recurring_gigs" doc:"Live recurring gigs using it"`
	Errands     int64  `json:"errands" doc:"Live errands using it"`
	Reason      string `json:"reason,omitempty" doc:"Skipped copies: why it is left alone"`
}

// ShareUser is a recurring gig or an errand that moves to the shared item.
type ShareUser struct {
	Kind        string `json:"kind" enum:"recurring_gig,errand"`
	ID          int64  `json:"id"`
	WorkspaceID int64  `json:"workspace_id"`
	Workspace   string `json:"workspace"`
	ProjectID   *int64 `json:"project_id,omitempty" doc:"Its project (an errand whose project is gone has none)"`
	Label       string `json:"label" doc:"Its template (or errand) name and its schedule"`
	From        int64  `json:"from" doc:"The id of the source or copy it uses now"`
}

// SharePlan is what Make shared does (or did).
type SharePlan struct {
	Kind     string      `json:"kind" enum:"schedule,template,errand_template"`
	DryRun   bool        `json:"dry_run" doc:"Nothing was changed: this is the plan"`
	Source   ShareItem   `json:"source" doc:"The item made shared (it is deleted once its users have moved)"`
	Shared   *ShareItem  `json:"shared,omitempty" doc:"The shared item made (not on a dry run)"`
	Copies   []ShareItem `json:"copies" doc:"Identical copies in workspaces that are merged and deleted"`
	Skipped  []ShareItem `json:"skipped" doc:"Identical copies left alone, with the reason"`
	Users    []ShareUser `json:"users" doc:"Recurring gigs and errands that move to the shared item"`
	Moved    int         `json:"moved" doc:"How many users moved (a dry run: how many would)"`
	DeleteN  int         `json:"deleted" doc:"How many items were deleted (a dry run: would be)"`
	NewName  string      `json:"new_name" doc:"The shared item's name"`
	Warnings []string    `json:"warnings,omitempty"`
}

func workspaceNames(ctx context.Context, q Q) (map[int64]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, name FROM workspaces`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var n string
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

func normStrs(s []string) []string {
	out := slices.Clone(s)
	if out == nil {
		out = []string{}
	}
	return out
}

// templateContent is the key two gig templates are identical by (see the comment at the top).
func templateContent(t *Template) string {
	tools := normStrs(t.AllowedTools)
	slices.Sort(tools)
	custom := t.Custom
	if custom == nil {
		custom = map[string]any{}
	}
	subs := t.Subgigs
	if subs == nil {
		subs = []TemplateSubgig{}
	}
	followers := slices.Clone(uniqIDs(t.Followers))
	slices.Sort(followers)
	return mustJSON([]any{t.ProjectID, t.Title, t.DescriptionMD, t.Priority, t.Color, normStrs(t.Labels), normStrs(t.Checklist), subs, custom,
		followers, t.Model, t.Effort, t.AlwaysRun, tools, t.AutoArchive, t.RunTimeout})
}

// errandTemplateSame: the content, and who set the command and turned the sandbox off (what the
// executor trusts a command by): a copy written or unsandboxed by someone else isn't the same template.
func errandTemplateSame(a, b *ErrandTemplate) bool {
	return a.DescriptionMD == b.DescriptionMD && a.Command == b.Command && a.TimeoutS == b.TimeoutS && a.Output == b.Output && a.Unsandboxed == b.Unsandboxed &&
		a.CommandSetBy == b.CommandSetBy && ptrEq(a.UnsandboxedBy, b.UnsandboxedBy)
}

func ptrEq(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// ShareFind loads the source (a workspace's, not a shared one) and the identical copies of it in every
// workspace (its own included), by workspace and name.
func ShareFind(ctx context.Context, q Q, kind string, id int64) (ShareItem, []ShareItem, error) {
	names, err := workspaceNames(ctx, q)
	if err != nil {
		return ShareItem{}, nil, err
	}
	item := func(id, ws int64, name string, rec, errs int64) ShareItem {
		return ShareItem{ID: id, WorkspaceID: ws, Workspace: names[ws], Name: name, Recurring: rec, Errands: errs}
	}
	var src ShareItem
	copies := []ShareItem{}
	switch kind {
	case ShareSchedule:
		s, err := GetSchedule(ctx, q, id)
		if err != nil {
			return src, nil, err
		}
		if s.Shared {
			return src, nil, Invalid("this schedule is shared already")
		}
		src = item(s.ID, s.WorkspaceID, s.Name, s.Uses, s.ErrandUses)
		rows, err := q.QueryContext(ctx, "SELECT "+schCols+" FROM schedules WHERE workspace_id IS NOT NULL AND deleted_at IS NULL AND id != ? ORDER BY workspace_id, name COLLATE NOCASE, id", id)
		if err != nil {
			return src, nil, err
		}
		defer rows.Close()
		for rows.Next() {
			o, err := scanSchedule(rows)
			if err != nil {
				return src, nil, err
			}
			if o.Rule.Same(s.Rule) {
				copies = append(copies, item(o.ID, o.WorkspaceID, o.Name, o.Uses, o.ErrandUses))
			}
		}
		return src, copies, rows.Err()
	case ShareTemplate:
		t, err := GetTemplate(ctx, q, id)
		if err != nil {
			return src, nil, err
		}
		if t.Shared {
			return src, nil, Invalid("this template is shared already")
		}
		src = item(t.ID, t.WorkspaceID, t.Name, t.Recurring, 0)
		key := templateContent(t)
		rows, err := q.QueryContext(ctx, "SELECT "+tplCols+" FROM gig_templates WHERE workspace_id IS NOT NULL AND deleted_at IS NULL AND id != ? ORDER BY workspace_id, name COLLATE NOCASE, id", id)
		if err != nil {
			return src, nil, err
		}
		defer rows.Close()
		for rows.Next() {
			o, err := scanTemplate(rows)
			if err != nil {
				return src, nil, err
			}
			if templateContent(o) == key {
				copies = append(copies, item(o.ID, o.WorkspaceID, o.Name, o.Recurring, 0))
			}
		}
		return src, copies, rows.Err()
	case ShareErrandTemplate:
		t, err := GetErrandTemplate(ctx, q, id)
		if err != nil {
			return src, nil, err
		}
		if t.Shared {
			return src, nil, Invalid("this template is shared already")
		}
		src = item(t.ID, t.WorkspaceID, t.Name, 0, t.Uses)
		rows, err := q.QueryContext(ctx, "SELECT "+ctCols+" FROM errand_templates t WHERE t.workspace_id IS NOT NULL AND t.deleted_at IS NULL AND t.id != ? ORDER BY t.workspace_id, t.name COLLATE NOCASE, t.id", id)
		if err != nil {
			return src, nil, err
		}
		defer rows.Close()
		for rows.Next() {
			o, err := scanErrandTemplate(rows)
			if err != nil {
				return src, nil, err
			}
			if errandTemplateSame(t, o) {
				copies = append(copies, item(o.ID, o.WorkspaceID, o.Name, 0, o.Uses))
			}
		}
		return src, copies, rows.Err()
	}
	return src, nil, Invalid("unknown kind %q", kind)
}

// ShareUsers lists the live recurring gigs and errands that use any of the items (ids of kind).
func ShareUsers(ctx context.Context, q Q, kind string, ids []int64) ([]ShareUser, error) {
	names, err := workspaceNames(ctx, q)
	if err != nil {
		return nil, err
	}
	in := placeholders(len(ids))
	args := int64Args(ids)
	out := []ShareUser{}
	run := func(userKind, query string) error {
		rows, err := q.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			u := ShareUser{Kind: userKind}
			var proj sql.NullInt64
			if err := rows.Scan(&u.ID, &u.WorkspaceID, &proj, &u.Label, &u.From); err != nil {
				return err
			}
			u.ProjectID = ptrInt(proj)
			u.Workspace = names[u.WorkspaceID]
			out = append(out, u)
		}
		return rows.Err()
	}
	rec := func(col string) string {
		return `SELECT r.id, r.workspace_id, r.project_id, t.title || ' · ' || s.name, r.` + col + ` FROM recurrences r JOIN gig_templates t ON t.id = r.template_id
			JOIN schedules s ON s.id = r.schedule_id WHERE r.` + col + ` IN (` + in + `) AND r.deleted_at IS NULL ORDER BY r.id`
	}
	err2 := func(col string) string {
		return `SELECT c.id, c.workspace_id, c.project_id, t.name || ' · ' || s.name, c.` + col + ` FROM errands c JOIN errand_templates t ON t.id = c.template_id
			JOIN schedules s ON s.id = c.schedule_id WHERE c.` + col + ` IN (` + in + `) AND c.deleted_at IS NULL ORDER BY c.id`
	}
	switch kind {
	case ShareSchedule:
		if err := run("recurring_gig", rec("schedule_id")); err != nil {
			return nil, err
		}
		return out, run("errand", err2("schedule_id"))
	case ShareTemplate:
		return out, run("recurring_gig", rec("template_id"))
	case ShareErrandTemplate:
		return out, run("errand", err2("template_id"))
	}
	return nil, Invalid("unknown kind %q", kind)
}

// ShareCheck refuses a move that would leave two recurring gigs with the same project, template and
// schedule (the pair is unique): they were told apart only by the copy they used.
func ShareCheck(ctx context.Context, q Q, kind string, ids []int64) error {
	var col, other string
	switch kind {
	case ShareSchedule:
		col, other = "schedule_id", "template_id"
	case ShareTemplate:
		col, other = "template_id", "schedule_id"
	default:
		return nil
	}
	rows, err := q.QueryContext(ctx, `SELECT group_concat(r.id), count(*) FROM recurrences r WHERE r.`+col+` IN (`+placeholders(len(ids))+`)
		AND r.deleted_at IS NULL GROUP BY r.project_id, r.`+other+` HAVING count(*) > 1`, int64Args(ids)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		var list string
		var n int
		if err := rows.Scan(&list, &n); err != nil {
			return err
		}
		return Conflict("recurring gigs #%s would become the same recurring gig (same project, template and schedule once the copies are merged). Delete or change all but one first", strings.ReplaceAll(list, ",", ", #"))
	}
	return rows.Err()
}

// ShareApply makes the shared item from the source's content, repoints the users of the source and of
// copies (identical copies the caller may delete, decided by the API) to it and deletes them all, in the
// caller's transaction. It returns the shared item. Every change is audited: the create, each repoint
// (the recurring gig's or errand's own diff) and each delete, plus one summary row on the shared item.
func ShareApply(ctx context.Context, q Q, a Actor, kind string, srcID int64, copyIDs []int64) (*ShareItem, error) {
	ids := append([]int64{srcID}, copyIDs...)
	if err := ShareCheck(ctx, q, kind, ids); err != nil {
		return nil, err
	}
	var sharedID int64
	var name string
	var del []func() error
	switch kind {
	case ShareSchedule:
		src, err := GetSchedule(ctx, q, srcID)
		if err != nil {
			return nil, err
		}
		s, err := SaveSchedule(ctx, q, a, &Schedule{Name: src.Name, Rule: src.Rule}, 0)
		if err != nil {
			return nil, err
		}
		sharedID, name = s.ID, s.Name
		for _, id := range ids {
			if err := repointRecurrences(ctx, q, a, "schedule_id", id, sharedID, false); err != nil {
				return nil, err
			}
			if err := repointErrands(ctx, q, a, "schedule_id", id, sharedID); err != nil {
				return nil, err
			}
			del = append(del, func() error {
				x, err := GetSchedule(ctx, q, id)
				if err != nil {
					return err
				}
				return DeleteSchedule(ctx, q, a, x)
			})
		}
	case ShareTemplate:
		src, err := GetTemplate(ctx, q, srcID)
		if err != nil {
			return nil, err
		}
		switch {
		case src.ProjectID != nil:
			return nil, Invalid("a shared template is for any project: make this template usable in any project first")
		case len(src.Custom) > 0:
			return nil, Invalid("a shared template has no custom field values (fields belong to a workspace): clear them first")
		case len(src.Followers) > 0:
			return nil, Invalid("a shared template has no followers: clear them first")
		}
		n := *src
		n.ID, n.WorkspaceID, n.Assignees, n.Followers = 0, 0, []int64{}, []int64{}
		t, err := SaveTemplate(ctx, q, a, &n, 0)
		if err != nil {
			return nil, err
		}
		sharedID, name = t.ID, t.Name
		for _, id := range ids {
			if err := repointRecurrences(ctx, q, a, "template_id", id, sharedID, true); err != nil {
				return nil, err
			}
			del = append(del, func() error {
				x, err := GetTemplate(ctx, q, id)
				if err != nil {
					return err
				}
				return DeleteTemplate(ctx, q, a, x)
			})
		}
	case ShareErrandTemplate:
		src, err := GetErrandTemplate(ctx, q, srcID)
		if err != nil {
			return nil, err
		}
		var n int
		if err := q.QueryRowContext(ctx, `SELECT count(*) FROM errand_templates WHERE workspace_id IS NULL AND deleted_at IS NULL`).Scan(&n); err != nil {
			return nil, err
		}
		if n >= errandTemplatesPerWS {
			return nil, Invalid("the shared library has at most %d errand templates", errandTemplatesPerWS)
		}
		sharedID, err = copyErrandTemplate(ctx, q, a, src, 0, false)
		if err != nil {
			return nil, err
		}
		name = src.Name
		for _, id := range ids {
			if err := repointErrands(ctx, q, a, "template_id", id, sharedID); err != nil {
				return nil, err
			}
			del = append(del, func() error {
				x, err := GetErrandTemplate(ctx, q, id)
				if err != nil {
					return err
				}
				return DeleteErrandTemplate(ctx, q, a, x)
			})
		}
	default:
		return nil, Invalid("unknown kind %q", kind)
	}
	for _, f := range del {
		if err := f(); err != nil {
			return nil, err
		}
	}
	if _, err := Activity(ctx, q, a, nil, nil, kind+".made_shared", kind, sharedID, map[string]any{"source": srcID, "merged": copyIDs}); err != nil {
		return nil, err
	}
	return &ShareItem{ID: sharedID, Name: name}, nil
}

// repointRecurrences moves the live recurring gigs whose col (schedule_id or template_id) is from to
// to. toShared (a shared template): a recurring gig that took its assignees from the template keeps them
// as its own, since the shared template holds none.
func repointRecurrences(ctx context.Context, q Q, a Actor, col string, from, to int64, toShared bool) error {
	rows, err := q.QueryContext(ctx, `SELECT id FROM recurrences WHERE `+col+` = ? AND deleted_at IS NULL ORDER BY id`, from)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		before, err := GetRecurrence(ctx, q, id)
		if err != nil {
			return err
		}
		set := col + ` = ?`
		args := []any{to}
		if toShared {
			var own any
			if len(before.Assignees) > 0 {
				own = mustJSON(before.Assignees)
			}
			set += `, assignees = ?`
			args = append(args, own)
		}
		args = append(args, id)
		if _, err := q.ExecContext(ctx, `UPDATE recurrences SET `+set+`, version = version + 1 WHERE id = ?`, args...); err != nil {
			return err
		}
		after, err := GetRecurrence(ctx, q, id)
		if err != nil {
			return err
		}
		if _, err := Activity(ctx, q, a, &after.WorkspaceID, &after.ProjectID, "recurrence.updated", "recurrence", id, AuditDiff("recurrence", before, after)); err != nil {
			return err
		}
	}
	return nil
}

// repointErrands moves the live errands whose col (schedule_id or template_id) is from to to.
func repointErrands(ctx context.Context, q Q, a Actor, col string, from, to int64) error {
	rows, err := q.QueryContext(ctx, `SELECT id FROM errands WHERE `+col+` = ? AND deleted_at IS NULL ORDER BY id`, from)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		before, err := GetErrand(ctx, q, id)
		if err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, fmt.Sprintf(`UPDATE errands SET %s = ?, version = version + 1 WHERE id = ?`, col), to, id); err != nil {
			return err
		}
		after, err := GetErrand(ctx, q, id)
		if err != nil {
			return err
		}
		if _, err := Activity(ctx, q, a, &after.WorkspaceID, nil, "errand.updated", "errand", id, AuditDiff("errand", before, after)); err != nil {
			return err
		}
	}
	return nil
}
