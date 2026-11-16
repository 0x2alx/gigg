package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// TestRecurringLinks: a recurring task pairs a template and a schedule. From the task form both are
// made (an identical schedule is reused); templates and schedules are shared freely, a pair once per
// project; edits to either move every recurring task on it; neither can be deleted while used, and
// deleting a recurring task leaves both.
func TestRecurringLinks(t *testing.T) {
	f := newFixture(t)
	start := ny(t, "2026-11-12 00:00")
	tx := func(fn func(tx *sql.Tx) error) { t.Helper(); must(t, f.s.Tx(f.ctx, fn)) }
	create := func(r *Recurrence) (*Recurrence, error) {
		var out *Recurrence
		err := f.s.Tx(f.ctx, func(tx *sql.Tx) error {
			var err error
			out, err = CreateRecurrence(f.ctx, tx, f.a, r)
			return err
		})
		return out, err
	}
	r1, err := create(&Recurrence{ProjectID: f.p1.ID, Title: "Daily brief", Freq: "daily", TZ: "America/New_York", WindowStart: "09:00",
		WindowMinutes: 120, StartsOn: start, Checklist: []string{"news", "weather"}, Model: "sonnet"})
	must(t, err)
	if r1.TemplateName != "Daily brief" || r1.ScheduleName != "Every day 09:00–11:00 (America/New_York)" || r1.TemplateUses != 1 || r1.ScheduleUses != 1 || len(r1.Checklist) != 2 {
		t.Fatalf("made from the form: %+v", r1)
	}
	tpl, err := GetTemplate(f.ctx, f.s.R, r1.TemplateID)
	must(t, err)
	if tpl.ProjectID == nil || *tpl.ProjectID != f.p1.ID || tpl.Recurring != 1 || tpl.Model != "sonnet" {
		t.Fatalf("template: %+v", tpl)
	}
	// each task gets the checklist
	res, err := f.s.Housekeeping(f.ctx, ny(t, "2026-11-12 09:05"))
	must(t, err)
	if res.Created != 1 {
		t.Fatalf("housekeeping: %+v", res)
	}
	r1, _ = GetRecurrence(f.ctx, f.s.R, r1.ID)
	items, err := Checklist(f.ctx, f.s.R, *r1.LastTaskID)
	must(t, err)
	if len(items) != 2 || items[0].Text != "news" {
		t.Fatalf("checklist on the task: %+v", items)
	}

	// the same template on another schedule
	r2, err := create(&Recurrence{ProjectID: f.p1.ID, TemplateID: r1.TemplateID, Freq: "weekly", Weekdays: []int{6}, TZ: "America/New_York",
		WindowStart: "11:00", StartsOn: start})
	must(t, err)
	if r2.Title != "Daily brief" || r2.TemplateUses != 2 || r2.ScheduleID == r1.ScheduleID {
		t.Fatalf("shared template: %+v", r2)
	}
	// the same pair twice in a project: refused; another project's template: refused
	if _, err := create(&Recurrence{ProjectID: f.p1.ID, TemplateID: r1.TemplateID, ScheduleID: r1.ScheduleID}); !errors.Is(err, ErrConflict) {
		t.Fatalf("same pair twice: %v", err)
	}
	if _, err := create(&Recurrence{ProjectID: f.p2.ID, TemplateID: r1.TemplateID, ScheduleID: r1.ScheduleID}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("other project's template: %v", err)
	}
	// other content on identical timing reuses the schedule
	r3, err := create(&Recurrence{ProjectID: f.p2.ID, Title: "Water plants", Freq: "daily", TZ: "America/New_York", WindowStart: "09:00",
		WindowMinutes: 120, StartsOn: start})
	must(t, err)
	if r3.ScheduleID != r1.ScheduleID || r3.ScheduleUses != 2 {
		t.Fatalf("identical timing should reuse the schedule: %+v", r3)
	}

	// editing the schedule moves both recurring tasks on it
	s1, err := GetSchedule(f.ctx, f.s.R, r1.ScheduleID)
	must(t, err)
	s1.WindowStart = "10:00"
	tx(func(tx *sql.Tx) error { _, err := SaveSchedule(f.ctx, tx, f.a, s1, 0); return err })
	for _, id := range []int64{r1.ID, r3.ID} {
		r, _ := GetRecurrence(f.ctx, f.s.R, id)
		if r.WindowStart != "10:00" || r.NextAt == nil || fmtNY(*r.NextAt)[15:] != "10:00 EST" {
			t.Fatalf("recurring task %d after the schedule edit: %+v %v", id, r, r.NextAt)
		}
	}
	// in use: no delete, either object
	s1, _ = GetSchedule(f.ctx, f.s.R, r1.ScheduleID)
	if err := f.s.Tx(f.ctx, func(tx *sql.Tx) error { return DeleteSchedule(f.ctx, tx, f.a, s1) }); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete a schedule in use: %v", err)
	}
	tpl, _ = GetTemplate(f.ctx, f.s.R, r1.TemplateID)
	if err := f.s.Tx(f.ctx, func(tx *sql.Tx) error { return DeleteTemplate(f.ctx, tx, f.a, tpl) }); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete a template in use: %v", err)
	}

	// content edits through a recurring task write its template (the other one follows; the name
	// follows the title while it is the title)
	r1, _ = GetRecurrence(f.ctx, f.s.R, r1.ID)
	upd := *r1
	upd.Title, upd.Checklist, upd.Custom = "Evening brief", nil, nil
	tx(func(tx *sql.Tx) error {
		_, err := UpdateRecurrence(f.ctx, tx, f.a, &upd, 0, RecurrenceChange{})
		return err
	})
	r2, _ = GetRecurrence(f.ctx, f.s.R, r2.ID)
	if r2.Title != "Evening brief" || r2.TemplateName != "Evening brief" || len(r2.Checklist) != 2 {
		t.Fatalf("shared template edit: %+v", r2)
	}
	// switching r2 onto r1's pair is refused; onto a fresh schedule works
	upd = *r2
	if err := f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		_, err := UpdateRecurrence(f.ctx, tx, f.a, &upd, 0, RecurrenceChange{Schedule: r1.ScheduleID})
		return err
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("switch onto an existing pair: %v", err)
	}
	var s3 *Schedule
	tx(func(tx *sql.Tx) error {
		var err error
		s3, err = SaveSchedule(f.ctx, tx, f.a, &Schedule{WorkspaceID: f.ws.ID, Name: "Mondays", Rule: Rule{Freq: "weekly", Weekdays: []int{1}}}, 0)
		return err
	})
	r2, _ = GetRecurrence(f.ctx, f.s.R, r2.ID)
	upd = *r2
	tx(func(tx *sql.Tx) error {
		_, err := UpdateRecurrence(f.ctx, tx, f.a, &upd, 0, RecurrenceChange{Schedule: s3.ID})
		return err
	})
	r2, _ = GetRecurrence(f.ctx, f.s.R, r2.ID)
	if r2.ScheduleID != s3.ID || r2.ScheduleName != "Mondays" || r2.TZ != DefaultTZ {
		t.Fatalf("switched schedule: %+v", r2)
	}
	// timing edits through a recurring task write its schedule (r3 shares it and moves too)
	r1, _ = GetRecurrence(f.ctx, f.s.R, r1.ID)
	upd = *r1
	upd.WindowStart = "08:00"
	tx(func(tx *sql.Tx) error {
		_, err := UpdateRecurrence(f.ctx, tx, f.a, &upd, 0, RecurrenceChange{})
		return err
	})
	r3, _ = GetRecurrence(f.ctx, f.s.R, r3.ID)
	if r3.WindowStart != "08:00" || r3.ScheduleName != "Every day 08:00–10:00 (America/New_York)" {
		t.Fatalf("shared schedule edit through a recurring task: %+v", r3)
	}

	// deleting a recurring task leaves its template and schedule
	r1, _ = GetRecurrence(f.ctx, f.s.R, r1.ID)
	tx(func(tx *sql.Tx) error { return DeleteRecurrence(f.ctx, tx, f.a, r1) })
	if _, err := GetTemplate(f.ctx, f.s.R, r1.TemplateID); err != nil {
		t.Fatalf("template after the delete: %v", err)
	}
	if _, err := GetSchedule(f.ctx, f.s.R, r1.ScheduleID); err != nil {
		t.Fatalf("schedule after the delete: %v", err)
	}
}

// TestScheduleTemplateMigration: 0007 gives every live schedule its own template with its content;
// 0008 moves its timing into a schedule of its own, named after the rule.
func TestScheduleTemplateMigration(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	migrateLimit = 6
	s, err := Open(ctx, dir, quiet)
	migrateLimit = 0
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO users (id, username, kind, display_name, is_admin, created_at) VALUES (1, 'ada', 'human', 'ada', 1, 1)`,
		`INSERT INTO workspaces (id, slug, name, status_template, created_at) VALUES (1, 'home', 'Home', '[]', 1)`,
		`INSERT INTO projects (id, workspace_id, slug, name, key, created_at) VALUES (1, 1, 'main', 'Main', 'MAIN', 1)`,
		`INSERT INTO recurrences (id, workspace_id, project_id, created_by, title, description_md, priority, color, labels, assignees, freq,
			starts_on, created_at, model, effort, always_run) VALUES (1, 1, 1, 1, 'Water the plants', 'Both rooms', 2, '#00aa00', '["home"]', '[1]',
			'daily', 1, 1, 'haiku', 'low', 1)`,
		`INSERT INTO recurrences (id, workspace_id, project_id, created_by, title, freq, starts_on, created_at, deleted_at)
			VALUES (2, 1, 1, 1, 'Gone', 'daily', 1, 1, 1)`,
	} {
		if _, err := s.W.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	s.Close()
	s, err = Open(ctx, dir, quiet)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := GetRecurrence(ctx, s.R, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r.TemplateName != "Water the plants" || r.ScheduleName != "Every day all day" || r.Freq != "daily" || r.WindowMinutes != 1440 || r.Title != "Water the plants" || r.DescriptionMD != "Both rooms" ||
		r.Priority != 2 || r.Color == nil || *r.Color != "#00aa00" || len(r.Labels) != 1 || len(r.Assignees) != 1 ||
		r.Model != "haiku" || r.Effort != "low" || !r.AlwaysRun {
		t.Fatalf("migrated schedule: %+v", r)
	}
	var n int
	_ = s.R.QueryRowContext(ctx, `SELECT count(*) FROM task_templates`).Scan(&n)
	if n != 1 {
		t.Fatalf("templates: %d (deleted schedules get none)", n)
	}
	_ = s.R.QueryRowContext(ctx, `SELECT count(*) FROM schedules WHERE deleted_at IS NULL`).Scan(&n)
	if n != 1 {
		t.Fatalf("live schedules: %d", n)
	}
}

// TestWorkspaceOwnerMigration: 0009 gives a workspace the user named like its slug, if any.
func TestWorkspaceOwnerMigration(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	migrateLimit = 8
	s, err := Open(ctx, dir, quiet)
	migrateLimit = 0
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO users (id, username, kind, created_at) VALUES (1, 'scout', 'agent', 1)`,
		`INSERT INTO workspaces (id, slug, name, status_template, created_at) VALUES (1, 'scout', 'Scout', '[]', 1)`,
		`INSERT INTO workspaces (id, slug, name, status_template, created_at) VALUES (2, 'home', 'Home', '[]', 1)`,
	} {
		if _, err := s.W.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	s.Close()
	s, err = Open(ctx, dir, quiet)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	w1, _ := GetWorkspace(ctx, s.R, 1)
	w2, _ := GetWorkspace(ctx, s.R, 2)
	if w1.OwnerID == nil || *w1.OwnerID != 1 || w2.OwnerID != nil {
		t.Fatalf("owners: %v %v", w1.OwnerID, w2.OwnerID)
	}
	// a new workspace named like a user starts owned by them; patching sets and clears
	var a Actor
	w3, err := CreateWorkspace(ctx, s.W, a, "scout-2", "Other", nil)
	if err != nil || w3.OwnerID != nil {
		t.Fatalf("no such user: %v %v", w3, err)
	}
	none := (*int64)(nil)
	one := int64(1)
	pOne := &one
	if w, err := PatchWorkspace(ctx, s.W, a, w3.ID, 0, WorkspacePatch{OwnerID: &pOne}); err != nil || *w.OwnerID != 1 {
		t.Fatalf("set: %v %v", w, err)
	}
	if w, err := PatchWorkspace(ctx, s.W, a, w3.ID, 0, WorkspacePatch{OwnerID: &none}); err != nil || w.OwnerID != nil {
		t.Fatalf("clear: %v %v", w, err)
	}
}
