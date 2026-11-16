package store

import (
	"database/sql"
	"errors"
	"testing"
)

// TestParentCycleRefused: a task can't be put under its own subtask, at any depth.
func TestParentCycleRefused(t *testing.T) {
	f := newFixture(t)
	a := f.task(t, NewTask{ProjectID: f.p1.ID, Title: "a"})
	b := f.task(t, NewTask{ProjectID: f.p1.ID, Title: "b", ParentID: &a.ID})
	c := f.task(t, NewTask{ProjectID: f.p1.ID, Title: "c", ParentID: &b.ID})
	err := f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		cur, err := GetTask(f.ctx, tx, a.ID)
		if err != nil {
			return err
		}
		_, err = PatchTask(f.ctx, tx, f.a, cur, 0, TaskPatch{ParentID: ptr(&c.ID)})
		return err
	})
	if err == nil {
		t.Fatal("a task was put under its own sub-subtask")
	}
	// moving it under an unrelated task still works
	d := f.task(t, NewTask{ProjectID: f.p1.ID, Title: "d"})
	f.tx(t, func(tx *sql.Tx) error {
		cur, err := GetTask(f.ctx, tx, a.ID)
		if err != nil {
			return err
		}
		_, err = PatchTask(f.ctx, tx, f.a, cur, 0, TaskPatch{ParentID: ptr(&d.ID)})
		return err
	})
}

// TestPatchWindowChecked: a change that sets both ends with the window closing before it opens is
// refused, as on create; one end alone (the UI moving a window field by field) is not.
func TestPatchWindowChecked(t *testing.T) {
	f := newFixture(t)
	tk := f.task(t, NewTask{ProjectID: f.p1.ID, Title: "w", StartAt: i64(1000), NotAfter: i64(5000)})
	err := f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		_, err := PatchTask(f.ctx, tx, f.a, tk, 0, TaskPatch{StartAt: ptr(i64(6000)), NotAfter: ptr(i64(5500))})
		return err
	})
	if err == nil {
		t.Fatal("window closing before it opens was accepted")
	}
	f.tx(t, func(tx *sql.Tx) error {
		_, err := PatchTask(f.ctx, tx, f.a, tk, 0, TaskPatch{StartAt: ptr(i64(6000))})
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

// TestRestoreIntoDeletedProject: a task whose project was deleted can't be restored into no list.
func TestRestoreIntoDeletedProject(t *testing.T) {
	f := newFixture(t)
	tk := f.task(t, NewTask{ProjectID: f.p2.ID, Title: "orphan"})
	f.tx(t, func(tx *sql.Tx) error {
		_, err := TrashTask(f.ctx, tx, f.a, tk, false)
		return err
	})
	f.tx(t, func(tx *sql.Tx) error { return DeleteProject(f.ctx, tx, f.a, f.p2) })
	err := f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		cur, err := GetTask(f.ctx, tx, tk.ID)
		if err != nil {
			return err
		}
		_, err = TrashTask(f.ctx, tx, f.a, cur, true)
		return err
	})
	if err == nil {
		t.Fatal("restored into a deleted project")
	}
}

// TestOccurrenceNotMadeTwice: next_at landing again on an occurrence already made (an edit inside the
// lead time recomputes it from now) doesn't create a second task.
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
	must(t, f.s.R.QueryRow(`SELECT count(*) FROM tasks WHERE recurrence_id = ?`, r.ID).Scan(&n))
	if res.Created != 0 || n != 1 {
		t.Fatalf("made twice: %+v, %d tasks", res, n)
	}
	got, err := GetRecurrence(f.ctx, f.s.R, r.ID)
	must(t, err)
	if got.NextAt == nil || *got.NextAt != ny(t, "2026-11-13 09:00") {
		t.Fatalf("next not advanced: %v", got.NextAt)
	}
}
