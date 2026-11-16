package store

import (
	"context"
	"database/sql"
	"fmt"
)

func init() { postSteps[7] = migrateScheduleTemplates }

// templateName: a template name (1-80 characters) from a task title, cut on a character boundary.
func templateName(title string) string {
	r := []rune(title)
	if len(r) > 80 {
		r = r[:80]
	}
	return string(r)
}

// migrateScheduleTemplates gives every live schedule a template holding its content (0007).
func migrateScheduleTemplates(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, workspace_id, project_id, created_by, title, description_md, priority, color, labels, assignees,
		model, effort, always_run, created_at FROM recurrences WHERE deleted_at IS NULL AND template_id IS NULL`)
	if err != nil {
		return err
	}
	type rec struct {
		id, ws, project, by, prio, at  int64
		title, desc, labels, assignees string
		model, effort                  string
		color                          sql.NullString
		always                         bool
	}
	var rs []rec
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.id, &r.ws, &r.project, &r.by, &r.title, &r.desc, &r.prio, &r.color, &r.labels, &r.assignees,
			&r.model, &r.effort, &r.always, &r.at); err != nil {
			rows.Close()
			return err
		}
		rs = append(rs, r)
	}
	rows.Close()
	for _, r := range rs {
		res, err := tx.ExecContext(ctx, `INSERT INTO task_templates (workspace_id, project_id, name, title, description_md, priority, color, labels,
			assignees, checklist, custom, created_by, created_at, model, effort, always_run) VALUES (?,?,?,?,?,?,?,?,?,'[]','{}',?,?,?,?,?)`,
			r.ws, r.project, templateName(r.title), r.title, r.desc, r.prio, r.color, r.labels, r.assignees, r.by, r.at, r.model, r.effort, r.always)
		if err != nil {
			return fmt.Errorf("schedule %d: %w", r.id, err)
		}
		tid, _ := res.LastInsertId()
		if _, err := tx.ExecContext(ctx, `UPDATE recurrences SET template_id = ?, owns_template = 1 WHERE id = ?`, tid, r.id); err != nil {
			return err
		}
	}
	return nil
}

func init() { postSteps[8] = nameMigratedSchedules }

// nameMigratedSchedules names the schedules 0008 made from recurrences after their rule.
func nameMigratedSchedules(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "SELECT "+schColsPre13+", 0 FROM schedules WHERE name GLOB 'schedule [0-9]*'")
	if err != nil {
		return err
	}
	var ss []*Schedule
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			rows.Close()
			return err
		}
		ss = append(ss, s)
	}
	rows.Close()
	for _, s := range ss {
		if _, err := tx.ExecContext(ctx, `UPDATE schedules SET name = ? WHERE id = ?`, s.Describe(), s.ID); err != nil {
			return err
		}
	}
	return nil
}
