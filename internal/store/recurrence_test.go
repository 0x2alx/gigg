package store

import (
	"database/sql"
	"strings"
	"testing"
	"time"
)

func ny(t *testing.T, s string) int64 {
	t.Helper()
	loc, _ := time.LoadLocation("America/New_York")
	v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
	if err != nil {
		t.Fatal(err)
	}
	return v.UnixMilli()
}

func fmtNY(ms int64) string {
	loc, _ := time.LoadLocation("America/New_York")
	return time.UnixMilli(ms).In(loc).Format("Mon 2006-01-02 15:04 MST")
}

func upcoming(t *testing.T, r *Recurrence, from string, n int) []string {
	t.Helper()
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, ms := range r.Upcoming(ny(t, from), n) {
		out = append(out, fmtNY(ms))
	}
	return out
}

func eq(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, " | ") != strings.Join(want, " | ") {
		t.Errorf("got  %s\nwant %s", strings.Join(got, " | "), strings.Join(want, " | "))
	}
}

func TestNextOccurrence(t *testing.T) {
	base := func() *Recurrence {
		return &Recurrence{Title: "x", TZ: "America/New_York", WindowStart: "09:00", WindowMinutes: 120}
	}
	t.Run("daily, across the DST change keeps 09:00 local", func(t *testing.T) {
		r := base()
		r.Freq, r.StartsOn = "daily", ny(t, "2026-10-30 00:00")
		eq(t, upcoming(t, r, "2026-10-31 10:00", 3),
			"Sun 2026-11-01 09:00 EST", "Mon 2026-11-02 09:00 EST", "Tue 2026-11-03 09:00 EST")
		eq(t, upcoming(t, r, "2026-10-30 08:59", 1), "Fri 2026-10-30 09:00 EDT")
	})
	t.Run("every 3 days counts from the start date", func(t *testing.T) {
		r := base()
		r.Freq, r.Interval, r.StartsOn = "daily", 3, ny(t, "2026-11-01 00:00")
		eq(t, upcoming(t, r, "2026-11-02 00:00", 3), "Wed 2026-11-04 09:00 EST", "Sat 2026-11-07 09:00 EST", "Tue 2026-11-10 09:00 EST")
	})
	t.Run("weekly Mon+Fri, every 2 weeks", func(t *testing.T) {
		r := base()
		r.Freq, r.Interval, r.Weekdays, r.StartsOn = "weekly", 2, []int{5, 1}, ny(t, "2026-11-11 00:00") // a Wednesday
		eq(t, upcoming(t, r, "2026-11-11 00:00", 4),
			"Fri 2026-11-13 09:00 EST", "Mon 2026-11-23 09:00 EST", "Fri 2026-11-27 09:00 EST", "Mon 2026-12-07 09:00 EST")
	})
	t.Run("monthly on the 31st falls back to the last day", func(t *testing.T) {
		r := base()
		r.Freq, r.MonthDay, r.StartsOn = "monthly", i64(31), ny(t, "2027-01-01 00:00")
		eq(t, upcoming(t, r, "2027-01-01 00:00", 4),
			"Sun 2027-01-31 09:00 EST", "Sun 2027-02-28 09:00 EST", "Wed 2027-03-31 09:00 EDT", "Fri 2027-04-30 09:00 EDT")
	})
	t.Run("monthly last day, every 6 months, ends", func(t *testing.T) {
		r := base()
		r.Freq, r.MonthDay, r.Interval, r.StartsOn = "monthly", i64(-1), 6, ny(t, "2028-01-15 00:00")
		r.EndsOn = i64(ny(t, "2029-01-01 00:00"))
		eq(t, upcoming(t, r, "2028-01-01 00:00", 5), "Mon 2028-01-31 09:00 EST", "Mon 2028-07-31 09:00 EDT")
	})
	t.Run("starts_on in the middle of a day skips that day's window if it already opened", func(t *testing.T) {
		r := base()
		r.Freq, r.StartsOn = "daily", ny(t, "2026-11-12 10:00")
		eq(t, upcoming(t, r, "2026-11-12 10:00", 1), "Fri 2026-11-13 09:00 EST")
	})
	t.Run("validation", func(t *testing.T) {
		for _, r := range []*Recurrence{
			{Title: "x", TZ: "Mars/Olympus", WindowStart: "09:00", Freq: "daily"},
			{Title: "x", TZ: "UTC", WindowStart: "9am", Freq: "daily"},
			{Title: "x", TZ: "UTC", WindowStart: "09:00", Freq: "weekly"},
			{Title: "x", TZ: "UTC", WindowStart: "09:00", Freq: "monthly", MonthDay: i64(0)},
			{Title: "x", TZ: "UTC", WindowStart: "09:00", Freq: "daily", WindowMinutes: 2},
		} {
			if r.Validate() == nil {
				t.Errorf("accepted %+v", r)
			}
		}
	})
}

// TestHousekeeping drives the janitor with a fake clock: creation with lead time, the window
// skipping while one is in progress, missed windows, catch-up after downtime, and max_count.
func TestHousekeeping(t *testing.T) {
	f := newFixture(t)
	start := ny(t, "2026-11-12 00:00")
	var r *Recurrence
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		var err error
		r, err = CreateRecurrence(f.ctx, tx, f.a, &Recurrence{ProjectID: f.p1.ID, Title: "Morning check", Freq: "daily", TZ: "America/New_York",
			WindowStart: "09:00", WindowMinutes: 120, LeadMinutes: 30, StartsOn: start, MaxCount: i64(4), Labels: []string{"routine"},
			Assignees: []int64{f.agent.ID}})
		return err
	}))
	if r.NextAt == nil || *r.NextAt != ny(t, "2026-11-12 09:00") {
		t.Fatalf("first occurrence: %v", r.NextAt)
	}
	run := func(at string) JanitorResult {
		t.Helper()
		res, err := f.s.Housekeeping(f.ctx, ny(t, at))
		must(t, err)
		return res
	}

	if res := run("2026-11-12 08:29"); res.Created != 0 {
		t.Fatalf("created before the lead time: %+v", res)
	}
	if res := run("2026-11-12 08:30"); res.Created != 1 {
		t.Fatalf("lead time: %+v", res)
	}
	tasks, _, err := ListTasks(f.ctx, f.s.R, TaskFilter{WorkspaceID: f.ws.ID})
	must(t, err)
	if len(tasks) != 1 {
		t.Fatalf("tasks: %d", len(tasks))
	}
	tk := tasks[0]
	if *tk.StartAt != ny(t, "2026-11-12 09:00") || *tk.NotAfter != ny(t, "2026-11-12 11:00") || *tk.DueAt != *tk.NotAfter ||
		*tk.RecurrenceID != r.ID || len(tk.Labels) != 1 || len(tk.Assignees) != 1 {
		t.Fatalf("occurrence task: %+v", tk)
	}
	move := func(tk *Task, cat string) {
		t.Helper()
		must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
			cur, err := GetTask(f.ctx, tx, tk.ID)
			if err != nil {
				return err
			}
			_, err = MoveTask(f.ctx, tx, f.a, cur, 0, Move{Category: cat})
			return err
		}))
	}
	// someone is still working on the first one when the next comes up: skipped (overlap skip),
	// and a task in progress is never marked missed
	move(tk, "doing")
	if res := run("2026-11-13 08:30"); res.Created != 0 || res.Skipped != 1 || res.Missed != 0 {
		t.Fatalf("overlap: %+v", res)
	}
	move(tk, "done")
	if res := run("2026-11-14 08:30"); res.Created != 1 {
		t.Fatalf("second: %+v", res)
	}
	tasks, _, _ = ListTasks(f.ctx, f.s.R, TaskFilter{WorkspaceID: f.ws.ID, Category: "todo"})
	second := tasks[0]
	// nobody touched the second: next morning it is missed (moved to waiting, a note in local time)
	// and no longer blocks the next occurrence
	if res := run("2026-11-15 08:30"); res.Missed != 1 || res.Created != 1 {
		t.Fatalf("missed: %+v", res)
	}
	second, err = GetTask(f.ctx, f.s.R, second.ID)
	must(t, err)
	if second.MissedAt == nil || second.StatusCategory != "waiting" {
		t.Fatalf("missed task: missed=%v status=%s", second.MissedAt, second.StatusCategory)
	}
	var note string
	must(t, f.s.R.QueryRow(`SELECT body_md FROM comments WHERE task_id = ? AND kind = 'system'`, second.ID).Scan(&note))
	if !strings.Contains(note, "Sat 2026-11-14 11:00 EST") {
		t.Errorf("note: %q", note)
	}
	// down for two days: windows that passed entirely are skipped, not created stale
	if res := run("2026-11-18 08:45"); res.Created != 1 || res.Skipped != 2 || res.Missed != 1 {
		t.Fatalf("catch-up: %+v", res)
	}
	r, err = GetRecurrence(f.ctx, f.s.R, r.ID)
	must(t, err)
	if r.CreatedCount != 4 || r.SkippedCount != 3 || r.NextAt == nil {
		t.Fatalf("counts: created=%d skipped=%d", r.CreatedCount, r.SkippedCount)
	}
	// max_count 4 reached: the schedule finishes
	run("2026-11-19 08:45")
	r, _ = GetRecurrence(f.ctx, f.s.R, r.ID)
	if r.NextAt != nil || r.CreatedCount != 4 {
		t.Fatalf("max_count: next=%v created=%d", r.NextAt, r.CreatedCount)
	}
}

// TestClaimRespectsWindow: claim (real clock) never hands out a task before its window opens,
// after it closes, or once missed; plain tasks are unaffected.
func TestClaimRespectsWindow(t *testing.T) {
	f := newFixture(t)
	now := Now()
	hour := int64(3600_000)
	mk := func(title string, start, notAfter *int64) {
		must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
			_, err := CreateTask(f.ctx, tx, f.a, NewTask{ProjectID: f.p1.ID, Title: title, Priority: 4, StartAt: start, NotAfter: notAfter})
			return err
		}))
	}
	mk("not yet open", i64(now+hour), i64(now+2*hour))
	mk("closed", i64(now-2*hour), i64(now-hour))
	claim := func() string {
		var got *Task
		must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
			var err error
			got, err = ClaimTask(f.ctx, tx, Actor{UserID: f.agent.ID}, f.ws.ID, Claim{DryRun: true})
			if err == ErrNotFound {
				return nil
			}
			return err
		}))
		if got == nil {
			return ""
		}
		return got.Title
	}
	if got := claim(); got != "" {
		t.Fatalf("claimed %q outside its window", got)
	}
	mk("open now", i64(now-hour), i64(now+hour))
	if got := claim(); got != "open now" {
		t.Fatalf("claim: %q", got)
	}
}
