package store

import (
	"database/sql"
	"errors"
	"testing"
)

// TestParentCycleRefused: a gig can't be put under its own subgig, at any depth.
func TestParentCycleRefused(t *testing.T) {
	f := newFixture(t)
	a := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "a"})
	b := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "b", ParentID: &a.ID})
	c := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "c", ParentID: &b.ID})
	err := f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		cur, err := GetGig(f.ctx, tx, a.ID)
		if err != nil {
			return err
		}
		_, err = PatchGig(f.ctx, tx, f.a, cur, 0, GigPatch{ParentID: ptr(&c.ID)})
		return err
	})
	if err == nil {
		t.Fatal("a gig was put under its own sub-subgig")
	}
	// moving it under an unrelated gig still works
	d := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "d"})
	f.tx(t, func(tx *sql.Tx) error {
		cur, err := GetGig(f.ctx, tx, a.ID)
		if err != nil {
			return err
		}
		_, err = PatchGig(f.ctx, tx, f.a, cur, 0, GigPatch{ParentID: ptr(&d.ID)})
		return err
	})
}

// TestPatchWindowChecked: a change that sets both ends with the window closing before it opens is
// refused, as on create; one end alone (the UI moving a window field by field) is not.
func TestPatchWindowChecked(t *testing.T) {
	f := newFixture(t)
	tk := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "w", StartAt: i64(1000), NotAfter: i64(5000)})
	err := f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		_, err := PatchGig(f.ctx, tx, f.a, tk, 0, GigPatch{StartAt: ptr(i64(6000)), NotAfter: ptr(i64(5500))})
		return err
	})
	if err == nil {
		t.Fatal("window closing before it opens was accepted")
	}
	f.tx(t, func(tx *sql.Tx) error {
		_, err := PatchGig(f.ctx, tx, f.a, tk, 0, GigPatch{StartAt: ptr(i64(6000))})
		return err
	})
}

// TestStaleVersionRefused: If-Match is compared with the row read in the transaction, not the caller's copy.
func TestStaleVersionRefused(t *testing.T) {
	f := newFixture(t)
	sts, err := Statuses(f.ctx, f.s.R, f.p1.ID)
	must(t, err)
	old := sts[0]
	f.tx(t, func(tx *sql.Tx) error {
		_, err := PatchStatus(f.ctx, tx, f.a, old, old.Version, StatusPatch{Name: ptr("First")})
		return err
	})
	err = f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		_, err := PatchStatus(f.ctx, tx, f.a, old, old.Version, StatusPatch{Name: ptr("Second")}) // same stale copy and version
		return err
	})
	if !errors.Is(err, ErrVersion) {
		t.Fatalf("second write with the old version: %v", err)
	}
}

// TestRestoreIntoDeletedProject: a gig whose project was deleted can't be restored into no list.
func TestRestoreIntoDeletedProject(t *testing.T) {
	f := newFixture(t)
	tk := f.gig(t, NewGig{ProjectID: f.p2.ID, Title: "orphan"})
	f.tx(t, func(tx *sql.Tx) error {
		_, err := TrashGig(f.ctx, tx, f.a, tk, false)
		return err
	})
	f.tx(t, func(tx *sql.Tx) error { return DeleteProject(f.ctx, tx, f.a, f.p2) })
	err := f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		cur, err := GetGig(f.ctx, tx, tk.ID)
		if err != nil {
			return err
		}
		_, err = TrashGig(f.ctx, tx, f.a, cur, true)
		return err
	})
	if err == nil {
		t.Fatal("restored into a deleted project")
	}
}

// TestOccurrenceNotMadeTwice: next_at landing again on an occurrence already made (an edit inside the
// lead time recomputes it from now) doesn't create a second gig.
func TestOccurrenceNotMadeTwice(t *testing.T) {
	f := newFixture(t)
	var r *Recurrence
	f.tx(t, func(tx *sql.Tx) error {
		var err error
		r, err = CreateRecurrence(f.ctx, tx, f.a, &Recurrence{ProjectID: f.p1.ID, Title: "Daily", Freq: "daily", TZ: "America/New_York",
			WindowStart: "09:00", WindowMinutes: 60, LeadMinutes: 120, Overlap: "create", StartsOn: ny(t, "2026-11-12 00:00")})
		return err
	})
	res, err := f.s.Housekeeping(f.ctx, ny(t, "2026-11-12 07:00"))
	must(t, err)
	if res.Created != 1 {
		t.Fatalf("first pass: %+v", res)
	}
	f.tx(t, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(f.ctx, `UPDATE recurrences SET next_at = ? WHERE id = ?`, ny(t, "2026-11-12 09:00"), r.ID)
		return err
	})
	res, err = f.s.Housekeeping(f.ctx, ny(t, "2026-11-12 07:30"))
	must(t, err)
	var n int
	must(t, f.s.R.QueryRow(`SELECT count(*) FROM gigs WHERE recurrence_id = ?`, r.ID).Scan(&n))
	if res.Created != 0 || n != 1 {
		t.Fatalf("made twice: %+v, %d gigs", res, n)
	}
	got, err := GetRecurrence(f.ctx, f.s.R, r.ID)
	must(t, err)
	if got.NextAt == nil || *got.NextAt != ny(t, "2026-11-13 09:00") {
		t.Fatalf("next not advanced: %v", got.NextAt)
	}
}

// TestArchivedBlockerFrees: an open blocker that gets archived no longer blocks (as with subgigs).
func TestArchivedBlockerFrees(t *testing.T) {
	f := newFixture(t)
	blocker := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "blocker"})
	held := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "held"})
	f.tx(t, func(tx *sql.Tx) error { return AddRelation(f.ctx, tx, f.a, blocker, held, "blocks") })
	got := f.get(t, held.ID)
	if got.BlockedBy != 1 {
		t.Fatalf("blocked_by = %d before archiving, want 1", got.BlockedBy)
	}
	f.tx(t, func(tx *sql.Tx) error {
		cur, err := GetGig(f.ctx, tx, blocker.ID)
		if err != nil {
			return err
		}
		_, err = ArchiveGig(f.ctx, tx, f.a, cur, true)
		return err
	})
	if got = f.get(t, held.ID); got.BlockedBy != 0 {
		t.Fatalf("blocked_by = %d after archiving the blocker, want 0", got.BlockedBy)
	}
}

// TestDeletedProjectFreesNames: a deleted project's slug and key can be used again, and its key then
// resolves to the new project's gigs only.
func TestDeletedProjectFreesNames(t *testing.T) {
	f := newFixture(t)
	var old *Project
	f.tx(t, func(tx *sql.Tx) error {
		var err error
		old, err = CreateProject(f.ctx, tx, f.a, f.ws.ID, "infra", "Infra", "INF", nil)
		return err
	})
	gone := f.gig(t, NewGig{ProjectID: old.ID, Title: "old"})
	f.tx(t, func(tx *sql.Tx) error {
		cur, err := GetGig(f.ctx, tx, gone.ID)
		if err != nil {
			return err
		}
		if _, err = TrashGig(f.ctx, tx, f.a, cur, false); err != nil {
			return err
		}
		return DeleteProject(f.ctx, tx, f.a, old)
	})
	// a live project still holds its names
	err := f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		_, err := CreateProject(f.ctx, tx, f.a, f.ws.ID, "gigg", "again", "GIG", nil)
		return err
	})
	if err == nil {
		t.Fatal("a live project's slug and key were taken twice")
	}
	var fresh *Project
	f.tx(t, func(tx *sql.Tx) error {
		var err error
		fresh, err = CreateProject(f.ctx, tx, f.a, f.ws.ID, "infra", "Infra again", "INF", nil)
		return err
	})
	now := f.gig(t, NewGig{ProjectID: fresh.ID, Title: "new"})
	ids, err := GigIDsByKey(f.ctx, f.s.R, "INF-1", nil)
	if err != nil || len(ids) != 1 || ids[0] != now.ID {
		t.Fatalf("INF-1 = %v (%v), want only the new project's gig %d", ids, err, now.ID)
	}
}

func (f *fixture) get(t *testing.T, id int64) *Gig {
	t.Helper()
	tk, err := GetGig(f.ctx, f.s.R, id)
	if err != nil {
		t.Fatal(err)
	}
	return tk
}
