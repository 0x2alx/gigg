package store

import (
	"database/sql"
	"errors"
	"testing"
)

// TestMakeNextNow: "Make the next one now" creates the next occurrence's gig at once, with a window
// starting now; the schedule carries on after it, the scheduler doesn't make that occurrence again,
// and max_count still ends it.
func TestMakeNextNow(t *testing.T) {
	f := newFixture(t)
	var r *Recurrence
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		var err error
		r, err = CreateRecurrence(f.ctx, tx, f.a, &Recurrence{ProjectID: f.p1.ID, Title: "Weekly report", Freq: "daily", TZ: "America/New_York",
			WindowStart: "09:00", WindowMinutes: 60, StartsOn: ny(t, "2026-11-12 00:00"), MaxCount: i64(2), Checklist: []string{"draft", "send"}})
		return err
	}))
	first := ny(t, "2026-11-12 09:00")
	if r.NextAt == nil || *r.NextAt != first {
		t.Fatalf("first occurrence: %v", r.NextAt)
	}
	makeNow := func(at string) (*Gig, error) {
		t.Helper()
		var g *Gig
		err := f.s.Tx(f.ctx, func(tx *sql.Tx) error {
			cur, err := GetRecurrence(f.ctx, tx, r.ID)
			if err != nil {
				return err
			}
			g, err = MakeNextNow(f.ctx, tx, f.a, cur, ny(t, at))
			return err
		})
		return g, err
	}
	now := ny(t, "2026-11-12 07:00")
	g, err := makeNow("2026-11-12 07:00")
	must(t, err)
	if *g.StartAt != now || *g.NotAfter != now+60*60000 || *g.OccurrenceAt != first || *g.RecurrenceID != r.ID || g.ChecklistTotal != 2 {
		t.Fatalf("gig made now: start %d not_after %d occurrence %d checklist %d", *g.StartAt, *g.NotAfter, *g.OccurrenceAt, g.ChecklistTotal)
	}
	cur, _ := GetRecurrence(f.ctx, f.s.R, r.ID)
	if cur.NextAt == nil || *cur.NextAt != ny(t, "2026-11-13 09:00") || cur.CreatedCount != 1 || cur.LastGigID == nil || *cur.LastGigID != g.ID {
		t.Fatalf("after: next %v, created %d", cur.NextAt, cur.CreatedCount)
	}
	// the scheduler doesn't make the 12th's occurrence again
	if res, err := f.s.Housekeeping(f.ctx, ny(t, "2026-11-12 09:05")); err != nil || res.Created != 0 {
		t.Fatalf("housekeeping made it again: %+v %v", res, err)
	}
	// the second (and last, max_count 2) one; then it has ended
	if _, err := makeNow("2026-11-12 10:00"); err != nil {
		t.Fatal(err)
	}
	cur, _ = GetRecurrence(f.ctx, f.s.R, r.ID)
	if cur.NextAt != nil || cur.CreatedCount != 2 {
		t.Fatalf("after the last: next %v, created %d", cur.NextAt, cur.CreatedCount)
	}
	if _, err := makeNow("2026-11-12 10:05"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an ended recurring gig made another: %v", err)
	}
}
