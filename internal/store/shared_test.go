package store

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"
)

// TestSharedTemplatesAndSchedules: a shared gig template and schedule (no workspace) are offered to
// every workspace; each recurring gig on them sets its own assignees; editing the content or the
// timing through one workspace's recurring gig gives it a copy of its own and leaves the shared ones
// (and the other workspace) as they were.
func TestSharedTemplatesAndSchedules(t *testing.T) {
	f := newFixture(t)
	tx := func(fn func(tx *sql.Tx) error) { t.Helper(); must(t, f.s.Tx(f.ctx, fn)) }
	var ws2 *Workspace
	var p3 *Project
	var atlas *User
	tx(func(tx *sql.Tx) error {
		var err error
		if atlas, err = CreateUser(f.ctx, tx, Actor{}, NewUser{Username: "atlas", Kind: "agent"}); err != nil {
			return err
		}
		if ws2, err = CreateWorkspace(f.ctx, tx, f.a, "atlas", "Atlas", nil); err != nil {
			return err
		}
		if err = SetMembership(f.ctx, tx, f.a, ws2.ID, atlas.ID, "member"); err != nil {
			return err
		}
		p3, err = CreateProject(f.ctx, tx, f.a, ws2.ID, "main", "Main", "ATL", nil)
		return err
	})

	// shared templates hold nothing of one workspace
	for name, bad := range map[string]*Template{
		"project":  {Name: "x", Title: "x", ProjectID: &f.p1.ID},
		"assignee": {Name: "x", Title: "x", Assignees: []int64{f.agent.ID}},
	} {
		err := f.s.Tx(f.ctx, func(tx *sql.Tx) error { _, err := SaveTemplate(f.ctx, tx, f.a, bad, 0); return err })
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("shared template with a %s: %v", name, err)
		}
	}
	week := int64(7 * 24 * 60)
	var tpl *Template
	var sch *Schedule
	tx(func(tx *sql.Tx) error {
		var err error
		if tpl, err = SaveTemplate(f.ctx, tx, f.a, &Template{Name: "Brief", Title: "Morning brief", Labels: []string{"brief"}, AutoArchive: &week}, 0); err != nil {
			return err
		}
		sch, err = SaveSchedule(f.ctx, tx, f.a, &Schedule{Name: "Mornings", Rule: Rule{Freq: "daily", TZ: "America/New_York", WindowStart: "07:00", WindowMinutes: 120}}, 0)
		return err
	})
	if !tpl.Shared || tpl.WorkspaceID != 0 || !sch.Shared {
		t.Fatalf("shared: %+v %+v", tpl, sch)
	}
	// both workspaces see them, after their own
	for _, ws := range []int64{f.ws.ID, ws2.ID} {
		ts, err := Templates(f.ctx, f.s.R, ws, nil)
		must(t, err)
		ss, err := ListSchedules(f.ctx, f.s.R, ws)
		must(t, err)
		if len(ts) == 0 || ts[len(ts)-1].ID != tpl.ID || len(ss) == 0 || ss[len(ss)-1].ID != sch.ID {
			t.Fatalf("workspace %d lists: %d templates, %d schedules", ws, len(ts), len(ss))
		}
	}

	start := ny(t, "2026-11-12 00:00")
	create := func(project int64, who []int64) *Recurrence {
		var out *Recurrence
		tx(func(tx *sql.Tx) error {
			var err error
			out, err = CreateRecurrence(f.ctx, tx, f.a, &Recurrence{ProjectID: project, TemplateID: tpl.ID, ScheduleID: sch.ID, StartsOn: start, OwnAssignees: who})
			return err
		})
		return out
	}
	r1 := create(f.p1.ID, []int64{f.agent.ID})
	r2 := create(p3.ID, []int64{atlas.ID})
	if !r1.AssigneesOwn || !slices.Equal(r1.Assignees, []int64{f.agent.ID}) || !slices.Equal(r2.Assignees, []int64{atlas.ID}) || !r1.TemplateShared || !r2.ScheduleShared {
		t.Fatalf("own assignees: %+v / %+v", r1, r2)
	}
	if r1.AutoArchive == nil || *r1.AutoArchive != week {
		t.Fatalf("auto-archive from the template: %v", r1.AutoArchive)
	}
	// each workspace's gig goes to its own agent, archives itself like the template says
	res, err := f.s.Housekeeping(f.ctx, ny(t, "2026-11-12 07:05"))
	must(t, err)
	if res.Created != 2 {
		t.Fatalf("housekeeping: %+v", res)
	}
	for _, r := range []*Recurrence{r1, r2} {
		r, _ = GetRecurrence(f.ctx, f.s.R, r.ID)
		g, err := GetGig(f.ctx, f.s.R, *r.LastGigID)
		must(t, err)
		if !slices.Equal(g.Assignees, r.Assignees) || g.AutoArchive == nil || *g.AutoArchive != week || len(g.Labels) != 1 || g.Schedule == nil || g.Schedule.Name != "Mornings" {
			t.Fatalf("gig of %d: %+v", r.ID, g)
		}
	}

	update := func(r *Recurrence, ch func(r *Recurrence)) *Recurrence {
		cur, err := GetRecurrence(f.ctx, f.s.R, r.ID)
		must(t, err)
		cur.OwnAssignees = nil
		ch(cur)
		var out *Recurrence
		tx(func(tx *sql.Tx) error {
			var err error
			out, err = UpdateRecurrence(f.ctx, tx, f.a, cur, 0, RecurrenceChange{})
			return err
		})
		return out
	}
	// only the assignees: still on the shared template
	r2 = update(r2, func(r *Recurrence) { r.OwnAssignees = []int64{atlas.ID, f.admin.ID} })
	if r2.TemplateID != tpl.ID || !r2.AssigneesOwn || len(r2.Assignees) != 2 {
		t.Fatalf("assignees only: %+v", r2)
	}
	// new content: workspace 1 gets its own copy (any project), the shared one and workspace 2 stay
	r1 = update(r1, func(r *Recurrence) { r.Title = "Morning brief, Scout's" })
	if r1.TemplateID == tpl.ID || r1.TemplateShared || r1.AssigneesOwn || !slices.Equal(r1.Assignees, []int64{f.agent.ID}) {
		t.Fatalf("content edit: %+v", r1)
	}
	cp, err := GetTemplate(f.ctx, f.s.R, r1.TemplateID)
	must(t, err)
	if cp.WorkspaceID != f.ws.ID || cp.ProjectID != nil || cp.Title != "Morning brief, Scout's" || cp.AutoArchive == nil {
		t.Fatalf("copy: %+v", cp)
	}
	if again, _ := GetTemplate(f.ctx, f.s.R, tpl.ID); again.Title != "Morning brief" || again.Version != tpl.Version {
		t.Fatalf("shared template changed: %+v", again)
	}
	// new timing: workspace 2 moves to a schedule of its own
	r2 = update(r2, func(r *Recurrence) { r.WindowStart = "08:00" })
	if r2.ScheduleID == sch.ID || r2.ScheduleShared {
		t.Fatalf("timing edit: %+v", r2)
	}
	s2, err := GetSchedule(f.ctx, f.s.R, r2.ScheduleID)
	must(t, err)
	if s2.WorkspaceID != ws2.ID || s2.WindowStart != "08:00" {
		t.Fatalf("schedule copy: %+v", s2)
	}
	if again, _ := GetSchedule(f.ctx, f.s.R, sch.ID); again.WindowStart != "07:00" {
		t.Fatalf("shared schedule changed: %+v", again)
	}
	// a shared template in use can't be deleted (no workspace may lose its recurring gig to it)
	if err := f.s.Tx(f.ctx, func(tx *sql.Tx) error { return DeleteTemplate(f.ctx, tx, f.a, tpl) }); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete a shared template in use: %v", err)
	}
}

// TestSharedErrandTemplate: a shared errand template is used by errands in any workspace; someone
// without No-sandbox errands gets a sandboxed copy of a no-sandbox one, and an errand that changes
// the content gets its own copy (provenance kept); the shared one never changes through an errand.
func TestSharedErrandTemplate(t *testing.T) {
	f := newFixture(t)
	tx := func(fn func(tx *sql.Tx) error) { t.Helper(); must(t, f.s.Tx(f.ctx, fn)) }
	holder, plain := ErrandEdit{CanUnsandbox: true, CanManage: true, CanTimeout: true}, ErrandEdit{}
	var tpl *ErrandTemplate
	tx(func(tx *sql.Tx) error {
		var err error
		tpl, err = SaveErrandTemplate(f.ctx, tx, f.a, &ErrandTemplate{Name: "backup", Command: "backup.sh", Unsandboxed: true}, 0, holder)
		return err
	})
	if !tpl.Shared || tpl.UnsandboxedBy == nil {
		t.Fatalf("shared errand template: %+v", tpl)
	}
	ts, err := ListErrandTemplates(f.ctx, f.s.R, f.ws.ID)
	must(t, err)
	if len(ts) != 1 || ts[0].ID != tpl.ID {
		t.Fatalf("listed: %+v", ts)
	}
	rule := Rule{Freq: "hours", TZ: "UTC"}
	mk := func(e ErrandEdit) *Errand {
		var c *Errand
		tx(func(tx *sql.Tx) error {
			var err error
			c, err = CreateErrand(f.ctx, tx, f.a, NewErrand{WorkspaceID: f.ws.ID, WorkerID: f.agent.ID, TemplateID: tpl.ID, Rule: &rule}, e)
			return err
		})
		return c
	}
	// without the permission: a sandboxed copy in the workspace, the command's author kept
	c1 := mk(plain)
	cp, err := GetErrandTemplate(f.ctx, f.s.R, c1.TemplateID)
	must(t, err)
	if cp.ID == tpl.ID || cp.WorkspaceID != f.ws.ID || cp.Unsandboxed || cp.CommandSetBy != tpl.CommandSetBy {
		t.Fatalf("sandboxed copy: %+v", cp)
	}
	// with it: the shared one itself; a content change through the errand: a copy, still no sandbox
	c2 := mk(holder)
	if c2.TemplateID != tpl.ID {
		t.Fatalf("holder's errand: %+v", c2)
	}
	timeout := int64(60)
	// a timeout change needs Run timeouts (CanTimeout)
	if err := f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		_, err := UpdateErrand(f.ctx, tx, f.a, c1.ID, 0, ErrandChange{TimeoutS: &timeout}, plain)
		return err
	}); !errors.Is(err, ErrDenied) {
		t.Fatalf("timeout without the permission: %v", err)
	}
	tx(func(tx *sql.Tx) error {
		var err error
		c2, err = UpdateErrand(f.ctx, tx, f.a, c2.ID, 0, ErrandChange{TimeoutS: &timeout}, holder)
		return err
	})
	cp2, err := GetErrandTemplate(f.ctx, f.s.R, c2.TemplateID)
	must(t, err)
	if cp2.ID == tpl.ID || cp2.WorkspaceID != f.ws.ID || !cp2.Unsandboxed || cp2.TimeoutS != 60 || cp2.UnsandboxedBy == nil || *cp2.UnsandboxedBy != *tpl.UnsandboxedBy {
		t.Fatalf("content copy: %+v", cp2)
	}
	if again, _ := GetErrandTemplate(f.ctx, f.s.R, tpl.ID); again.Version != tpl.Version || !again.Unsandboxed {
		t.Fatalf("shared errand template changed: %+v", again)
	}
}

// TestAutoArchive: done (and cancelled) gigs with a delay archive themselves once it has passed; others stay.
func TestAutoArchive(t *testing.T) {
	f := newFixture(t)
	hour := int64(60)
	g1 := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "archives", AutoArchive: &hour})
	g2 := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "stays"})
	g3 := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "open", AutoArchive: &hour})
	for _, g := range []*Gig{g1, g2} {
		must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
			_, err := MoveGig(f.ctx, tx, f.a, g, 0, Move{Category: "done"})
			return err
		}))
	}
	g1, _ = GetGig(f.ctx, f.s.R, g1.ID)
	sweep := func(at int64) int {
		var n int
		must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
			var err error
			n, err = autoArchive(f.ctx, tx, at)
			return err
		}))
		return n
	}
	if n := sweep(*g1.DoneAt + 59*60000); n != 0 {
		t.Fatalf("archived early: %d", n)
	}
	if n := sweep(*g1.DoneAt + 61*60000); n != 1 {
		t.Fatalf("archived: %d", n)
	}
	for _, g := range []*Gig{g1, g2, g3} {
		got, _ := GetGig(f.ctx, f.s.R, g.ID)
		if (got.ArchivedAt != nil) != (g.ID == g1.ID) {
			t.Fatalf("%s archived: %v", got.Title, got.ArchivedAt)
		}
	}
	var n int
	must(t, f.s.R.QueryRowContext(f.ctx, `SELECT count(*) FROM activity WHERE action = 'gig.archived' AND entity_id = ?`, g1.ID).Scan(&n))
	if n != 1 {
		t.Fatalf("activity entries: %d", n)
	}
	// a patch sets and clears it; out-of-range values are refused
	var nilv *int64
	bad := int64(2)
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		g, err := PatchGig(f.ctx, tx, f.a, g3, 0, GigPatch{AutoArchive: &nilv})
		if err != nil || g.AutoArchive != nil {
			t.Fatalf("clear: %v %v", err, g)
		}
		badp := &bad
		if _, err := PatchGig(f.ctx, tx, f.a, g, 0, GigPatch{AutoArchive: &badp}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("2 minutes: %v", err)
		}
		return nil
	}))
}

// TestSharedMigration: 0015 keeps every template, schedule, recurring gig and errand of an upgraded
// instance in its workspace (the three tables are rebuilt).
func TestSharedMigration(t *testing.T) {
	skipOnPG(t)
	ctx := context.Background()
	dir := t.TempDir()
	migrateLimit = 14
	s, err := Open(ctx, dir, quiet)
	migrateLimit = 0
	if err != nil {
		t.Fatal(err)
	}
	var ws, pid, uid int64
	must(t, s.Tx(ctx, func(tx *sql.Tx) error {
		u, err := CreateUser(ctx, tx, Actor{}, NewUser{Username: "ada", Kind: "human", IsAdmin: true})
		if err != nil {
			return err
		}
		uid = u.ID
		a := Actor{UserID: u.ID}
		w, err := CreateWorkspace(ctx, tx, a, "ws", "WS", nil)
		if err != nil {
			return err
		}
		p, err := CreateProject(ctx, tx, a, w.ID, "main", "Main", "PRJ", nil)
		ws, pid = w.ID, p.ID
		return err
	}))
	for _, q := range []string{
		`INSERT INTO gig_templates (id, workspace_id, project_id, name, title, labels, created_by, created_at, model, subgigs) VALUES (5, ?1, ?2, 'tpl', 'Title', '["x"]', ?3, 1, 'sonnet', '[{"title":"s"}]')`,
		`INSERT INTO schedules (id, workspace_id, name, freq, created_by, created_at) VALUES (6, ?1, 'daily', 'daily', ?3, 1)`,
		`INSERT INTO recurrences (id, workspace_id, project_id, created_by, template_id, schedule_id, starts_on, created_at) VALUES (7, ?1, ?2, ?3, 5, 6, 1, 1)`,
		`INSERT INTO errand_templates (id, workspace_id, name, command, command_set_by, command_set_at, created_by, created_at) VALUES (8, ?1, 'e', 'true', ?3, 1, ?3, 1)`,
		`INSERT INTO errands (id, workspace_id, template_id, schedule_id, worker_id, created_by, created_at) VALUES (9, ?1, 8, 6, ?3, ?3, 1)`,
	} {
		if _, err := s.W.ExecContext(ctx, q, ws, pid, uid); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	s.Close()
	if s, err = Open(ctx, dir, quiet); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := GetRecurrence(ctx, s.R, 7)
	must(t, err)
	if r.WorkspaceID != ws || r.TemplateShared || r.ScheduleShared || r.Title != "Title" || r.Model != "sonnet" || len(r.Subgigs) != 1 || len(r.Labels) != 1 || r.AssigneesOwn {
		t.Fatalf("recurring gig after 0015: %+v", r)
	}
	c, err := GetErrand(ctx, s.R, 9)
	must(t, err)
	if c.WorkspaceID != ws || c.TemplateID != 8 || c.ScheduleID != 6 {
		t.Fatalf("errand after 0015: %+v", c)
	}
	et, err := GetErrandTemplate(ctx, s.R, 8)
	must(t, err)
	if et.Shared || et.WorkspaceID != ws {
		t.Fatalf("errand template after 0015: %+v", et)
	}
}
