package store

import (
	"database/sql"
	"errors"
	"testing"
)

// A template's start_category decides where its gigs land: a recurring gig made from a backlog
// template creates its gigs in Backlog (out of the runner's reach), and so does "use template";
// the default stays the project's default status.
func TestStartCategory(t *testing.T) {
	f := newFixture(t)
	start := ny(t, "2026-11-12 00:00")

	// a bad value is refused
	_, err := SaveTemplate(f.ctx, f.s.W, f.a, &Template{WorkspaceID: f.ws.ID, Name: "bad", Title: "x", StartCategory: "doing"}, 0)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("start_category doing: %v", err)
	}

	var r *Recurrence
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		var err error
		r, err = CreateRecurrence(f.ctx, tx, f.a, &Recurrence{ProjectID: f.p1.ID, Title: "Daily review", Freq: "daily", TZ: "America/New_York",
			WindowStart: "09:00", WindowMinutes: 120, StartsOn: start, Assignees: []int64{f.agent.ID}, StartCategory: "backlog"})
		return err
	}))
	if r.StartCategory != "backlog" {
		t.Fatalf("recurrence start_category: %q", r.StartCategory)
	}
	tpl, err := GetTemplate(f.ctx, f.s.R, r.TemplateID)
	must(t, err)
	if tpl.StartCategory != "backlog" {
		t.Fatalf("its template: %q", tpl.StartCategory)
	}
	if res, err := f.s.Housekeeping(f.ctx, ny(t, "2026-11-12 09:00")); err != nil || res.Created != 1 {
		t.Fatalf("created: %+v %v", res, err)
	}
	gigs, _, err := ListGigs(f.ctx, f.s.R, GigFilter{WorkspaceID: f.ws.ID, Category: "backlog"})
	must(t, err)
	if len(gigs) != 1 || gigs[0].Title != "Daily review" {
		t.Fatalf("not in backlog: %+v", gigs)
	}

	// an edit that doesn't name it keeps it; the template's own content is compared with it
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		cur, err := GetRecurrence(f.ctx, tx, r.ID)
		if err != nil {
			return err
		}
		next := *cur
		next.Title = "Daily review (renamed)"
		_, err = UpdateRecurrence(f.ctx, tx, f.a, &next, cur.Version, RecurrenceChange{})
		return err
	}))
	cur, err := GetRecurrence(f.ctx, f.s.R, r.ID)
	must(t, err)
	if cur.StartCategory != "backlog" || cur.Title != "Daily review (renamed)" {
		t.Fatalf("after edit: %q %q", cur.StartCategory, cur.Title)
	}

	// "use template" lands there too; back to "" lands in the project's default (To-Do)
	tpl, err = GetTemplate(f.ctx, f.s.R, r.TemplateID)
	must(t, err)
	g, err := GigFromTemplate(f.ctx, f.s.W, f.a, tpl, f.p1, nil, nil)
	must(t, err)
	if g.StatusCategory != "backlog" {
		t.Fatalf("from template: %s", g.StatusCategory)
	}
	tpl.StartCategory = ""
	tpl, err = SaveTemplate(f.ctx, f.s.W, f.a, tpl, 0)
	must(t, err)
	g, err = GigFromTemplate(f.ctx, f.s.W, f.a, tpl, f.p1, nil, nil)
	must(t, err)
	if g.StatusCategory != "todo" {
		t.Fatalf("default: %s", g.StatusCategory)
	}
	// SameContent sees the difference
	a, b := *cur, *cur
	b.StartCategory = ""
	if SameContent(&a, &b) {
		t.Fatal("SameContent ignores start_category")
	}
}
