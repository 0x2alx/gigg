package store

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
)

func nextN(t *testing.T, r Rule, from string, n int) []string {
	t.Helper()
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	var out []string
	at := ny(t, from)
	for i := 0; i < n; i++ {
		nx, ok := r.Next(ny(t, "2026-01-01 00:00"), at)
		if !ok {
			break
		}
		out = append(out, fmtNY(nx))
		at = nx + 1
	}
	return out
}

// TestFrequentRules: minute and hour schedules tick from the window start while the window is open,
// every day or on the weekdays given; a window may run past midnight; DST days keep wall-clock ticks.
func TestFrequentRules(t *testing.T) {
	ny5 := Rule{Freq: "minutes", Interval: 5, TZ: "America/New_York"}
	eq(t, nextN(t, ny5, "2026-11-16 14:02", 3), "Mon 2026-11-16 14:05 EST", "Mon 2026-11-16 14:10 EST", "Mon 2026-11-16 14:15 EST")
	eq(t, nextN(t, ny5, "2026-11-16 14:05", 1), "Mon 2026-11-16 14:05 EST") // at or after
	must(t, ny5.Validate())
	if ny5.Describe() != "Every 5 min (America/New_York)" && ny5.Describe() != "Every 5 min" {
		t.Errorf("describe: %q", ny5.Describe())
	}
	work := Rule{Freq: "hours", Interval: 2, Weekdays: []int{5, 1, 2, 3, 4}, TZ: "America/New_York", WindowStart: "09:00", WindowMinutes: 480}
	eq(t, nextN(t, work, "2026-11-20 14:30", 3), "Fri 2026-11-20 15:00 EST", "Mon 2026-11-23 09:00 EST", "Mon 2026-11-23 11:00 EST")
	must(t, work.Validate())
	if !strings.HasPrefix(work.Describe(), "Every 2 h on weekdays, 09:00–17:00") {
		t.Errorf("describe: %q", work.Describe())
	}
	night := Rule{Freq: "minutes", Interval: 30, TZ: "America/New_York", WindowStart: "23:00", WindowMinutes: 120}
	eq(t, nextN(t, night, "2026-11-16 23:50", 3), "Tue 2026-11-17 00:00 EST", "Tue 2026-11-17 00:30 EST", "Tue 2026-11-17 23:00 EST")
	// DST end (2026-11-01): every hour keeps the wall clock across the change
	hourly := Rule{Freq: "hours", Interval: 1, TZ: "America/New_York"}
	got := nextN(t, hourly, "2026-11-01 00:30", 3)
	if len(got) != 3 || got[0] != "Sun 2026-11-01 01:00 EDT" {
		t.Errorf("DST: %v", got)
	}
	for _, bad := range []Rule{{Freq: "minutes", Interval: 400}, {Freq: "hours", Interval: 25}, {Freq: "minutes", Interval: 5, Weekdays: []int{8}},
		{Freq: "hours", Interval: 1, WindowMinutes: 2000}} {
		if err := bad.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}

type cronFix struct {
	*fixture
	t *testing.T
}

func (c cronFix) create(n NewCron, can bool) (*Cron, error) {
	var out *Cron
	err := c.s.Tx(c.ctx, func(tx *sql.Tx) error {
		var err error
		out, err = CreateCron(c.ctx, tx, c.a, n, CronEdit{CanUnsandbox: can})
		return err
	})
	return out, err
}

func (c cronFix) claim(at string) []*ClaimedCron {
	c.t.Helper()
	Now = func() int64 { return ny(c.t, at) }
	var out []*ClaimedCron
	must(c.t, c.s.Tx(c.ctx, func(tx *sql.Tx) error {
		var err error
		out, err = ClaimCrons(c.ctx, tx, c.agent.ID, c.ws.ID)
		return err
	}))
	return out
}

func (c cronFix) finish(run int64, f CronFinish) *CronOutcome {
	c.t.Helper()
	var out *CronOutcome
	must(c.t, c.s.Tx(c.ctx, func(tx *sql.Tx) error {
		var err error
		out, err = FinishCronRun(c.ctx, tx, c.agent.ID, run, f)
		return err
	}))
	return out
}

func (c cronFix) runs(id int64) []string {
	c.t.Helper()
	rs, err := ListCronRuns(c.ctx, c.s.R, id, 0, 50)
	must(c.t, err)
	var out []string
	for i := len(rs) - 1; i >= 0; i-- {
		out = append(out, rs[i].Status)
	}
	return out
}

// TestCronClaimAndFinish: due slots run once; a late executor records missed slots and runs the latest
// once if recent; a run still going makes the slot "skipped"; run-now runs once; state moves to
// failing on the first failure (a task when asked) and back on success.
func TestCronClaimAndFinish(t *testing.T) {
	f := newFixture(t)
	defer func(n func() int64) { Now = n }(Now)
	c := cronFix{f, t}
	Now = func() int64 { return ny(t, "2026-11-16 14:02") }
	cr, err := c.create(NewCron{WorkspaceID: f.ws.ID, WorkerID: f.agent.ID, Template: &CronTemplate{Name: "disk check", Command: "df -h /"},
		Rule: &Rule{Freq: "minutes", Interval: 5, TZ: "America/New_York"}, OnFailureTask: true}, false)
	must(t, err)
	if cr.NextAt == nil || fmtNY(*cr.NextAt) != "Mon 2026-11-16 14:05 EST" || cr.State != "never" || cr.TimeoutS != 300 || cr.Output != "gigg" {
		t.Fatalf("created: %+v", cr)
	}
	if got := c.claim("2026-11-16 14:04"); len(got) != 0 {
		t.Fatalf("not due yet: %+v", got)
	}
	got := c.claim("2026-11-16 14:05")
	if len(got) != 1 || got[0].Command != "df -h /" || got[0].CommandSetBy != "ada" || got[0].Unsandboxed {
		t.Fatalf("due: %+v", got)
	}
	first := got[0].RunID
	// the next slot while the first run still goes: skipped, not stacked
	if got := c.claim("2026-11-16 14:10"); len(got) != 0 {
		t.Fatalf("still running: %+v", got)
	}
	o := c.finish(first, CronFinish{Status: "failed", ExitCode: ptr(int64(1)), DurationMS: 900, OutputTail: ptr("disk full"), OutputBytes: 9})
	if o.State != "failing" || !o.BecameFailing || o.TaskKey == "" {
		t.Fatalf("first failure: %+v", o)
	}
	ids, err := TaskIDsByKey(f.ctx, f.s.R, o.TaskKey, []int64{f.ws.ID})
	must(t, err)
	if len(ids) != 1 {
		t.Fatalf("task %s: %v", o.TaskKey, ids)
	}
	task, err := GetTask(f.ctx, f.s.R, ids[0])
	must(t, err)
	if task.CreatedBy != f.agent.ID || !strings.Contains(task.DescriptionMD, "disk full") {
		t.Fatalf("failure task: %+v", task)
	}
	// a late executor: 14:15 .. 14:35 passed; at 14:36 the 14:35 slot is recent: one missed record, one run
	got = c.claim("2026-11-16 14:36")
	if len(got) != 1 || got[0].ScheduledFor == nil || fmtNY(*got[0].ScheduledFor) != "Mon 2026-11-16 14:35 EST" {
		t.Fatalf("late: %+v", got)
	}
	o = c.finish(got[0].RunID, CronFinish{Status: "failed", ExitCode: ptr(int64(2))})
	if o.BecameFailing || o.FailStreak != 2 || o.TaskKey != "" {
		t.Fatalf("second failure: no new task, streak 2: %+v", o)
	}
	o = c.finish(c.claim("2026-11-16 14:40")[0].RunID, CronFinish{Status: "ok", ExitCode: ptr(int64(0))})
	if !o.Recovered || o.State != "ok" {
		t.Fatalf("recovered: %+v", o)
	}
	eq(t, c.runs(cr.ID), "failed", "skipped", "missed", "failed", "ok")
	// run now: once, whatever the schedule
	Now = func() int64 { return ny(t, "2026-11-16 14:41") }
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error { cur, _ := GetCron(f.ctx, tx, cr.ID); _, err := RunCronNow(f.ctx, tx, f.a, cur); return err }))
	got = c.claim("2026-11-16 14:41")
	if len(got) != 1 || got[0].ScheduledFor != nil {
		t.Fatalf("run now: %+v", got)
	}
	c.finish(got[0].RunID, CronFinish{Status: "ok"})
	if got := c.claim("2026-11-16 14:42"); len(got) != 0 {
		t.Fatalf("run now runs once: %+v", got)
	}

	// a daily cron whose executor was away for days: one missed record, no run, next slot tomorrow
	Now = func() int64 { return ny(t, "2026-11-16 08:00") }
	d, err := c.create(NewCron{WorkspaceID: f.ws.ID, WorkerID: f.agent.ID, Template: &CronTemplate{Name: "nightly", Command: "true"},
		Rule: &Rule{Freq: "daily", TZ: "America/New_York", WindowStart: "03:30"}}, false)
	must(t, err)
	got = c.claim("2026-11-19 10:00")
	for _, g := range got {
		if g.CronID == d.ID {
			t.Fatalf("a slot 6.5 h old must not run: %+v", g)
		}
	}
	eq(t, c.runs(d.ID), "missed")
	d, _ = GetCron(f.ctx, f.s.R, d.ID)
	if fmtNY(*d.NextAt) != "Fri 2026-11-20 03:30 EST" {
		t.Fatalf("next: %s", fmtNY(*d.NextAt))
	}

	// the workspace's crons paused: nothing claimed
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error { return PauseCrons(f.ctx, tx, f.a, f.ws.ID) }))
	Now = func() int64 { return ny(t, "2026-11-20 03:31") }
	err = f.s.Tx(f.ctx, func(tx *sql.Tx) error { _, err := ClaimCrons(f.ctx, tx, f.agent.ID, f.ws.ID); return err })
	if !errors.Is(err, ErrCronsPaused) {
		t.Fatalf("paused: %v", err)
	}
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error { return ResumeCrons(f.ctx, tx, f.a, f.ws.ID) }))
	d, _ = GetCron(f.ctx, f.s.R, d.ID)
	if fmtNY(*d.NextAt) != "Sat 2026-11-21 03:30 EST" {
		t.Fatalf("resume starts from the next slot, no catch-up: %s", fmtNY(*d.NextAt))
	}
}

// TestCronUnsandboxed: only someone holding the permission turns the sandbox off; any change by
// someone without it turns it back on; a flag whose setter lost the permission isn't run at all.
func TestCronUnsandboxed(t *testing.T) {
	f := newFixture(t)
	defer func(n func() int64) { Now = n }(Now)
	c := cronFix{f, t}
	Now = func() int64 { return ny(t, "2026-11-16 14:02") }
	n := NewCron{WorkspaceID: f.ws.ID, WorkerID: f.agent.ID, Template: &CronTemplate{Name: "tts check", Command: "curl -s 127.0.0.1:9000/health",
		Unsandboxed: true}, Rule: &Rule{Freq: "minutes", Interval: 5, TZ: "America/New_York"}}
	if _, err := c.create(n, false); !errors.Is(err, ErrInvalid) {
		t.Fatalf("without the permission: %v", err)
	}
	n.Template.ID = 0
	cr, err := c.create(n, true)
	must(t, err)
	if !cr.Unsandboxed {
		t.Fatalf("set: %+v", cr)
	}
	// ada (an admin) holds every permission: the claim hands it out unsandboxed
	got := c.claim("2026-11-16 14:05")
	if len(got) != 1 || !got[0].Unsandboxed {
		t.Fatalf("claim: %+v", got)
	}
	c.finish(got[0].RunID, CronFinish{Status: "ok"})
	// the agent (no permission) changes only the schedule: the flag goes off, and the agent is now the setter
	agent := Actor{UserID: f.agent.ID}
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		cur, _ := GetCron(f.ctx, tx, cr.ID)
		_, err := UpdateCron(f.ctx, tx, agent, cur, 0, CronChange{Rule: &Rule{Freq: "minutes", Interval: 10, TZ: "America/New_York"}}, CronEdit{})
		return err
	}))
	cr, _ = GetCron(f.ctx, f.s.R, cr.ID)
	if cr.Unsandboxed || cr.CommandSetBy != f.agent.ID {
		t.Fatalf("cleared: %+v", cr)
	}
	// and it can't turn it back on
	err = f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		cur, _ := GetCron(f.ctx, tx, cr.ID)
		_, err := UpdateCron(f.ctx, tx, agent, cur, 0, CronChange{Template: &CronTemplate{Name: cur.Name, Command: cur.Command, Unsandboxed: true}}, CronEdit{})
		return err
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("re-enable without the permission: %v", err)
	}
	// a flag set by someone who has since lost the permission: the run is refused, never sandboxed instead
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(f.ctx, `UPDATE cron_templates SET unsandboxed = 1, command_set_by = ? WHERE id = ?`, f.agent.ID, cr.TemplateID)
		return err
	}))
	got = c.claim("2026-11-16 14:20")
	if len(got) != 0 {
		t.Fatalf("must not be handed out: %+v", got)
	}
	rs, _ := ListCronRuns(f.ctx, f.s.R, cr.ID, 0, 1)
	if rs[0].Status != "failed" || rs[0].Error == nil || !strings.Contains(*rs[0].Error, "no longer has") {
		t.Fatalf("refused run: %+v", rs[0])
	}
}

// TestCronHousekeeping: a run whose executor never reported is closed as a timeout; old runs are
// trimmed (the newest 200 and the last 14 days stay).
func TestCronHousekeeping(t *testing.T) {
	f := newFixture(t)
	defer func(n func() int64) { Now = n }(Now)
	c := cronFix{f, t}
	Now = func() int64 { return ny(t, "2026-10-01 00:00") }
	cr, err := c.create(NewCron{WorkspaceID: f.ws.ID, WorkerID: f.agent.ID, Template: &CronTemplate{Name: "x", Command: "true", TimeoutS: 60},
		Rule: &Rule{Freq: "minutes", Interval: 1, TZ: "America/New_York"}}, false)
	must(t, err)
	run := c.claim("2026-10-01 00:01")[0].RunID
	res, err := f.s.Housekeeping(f.ctx, ny(t, "2026-10-01 00:05"))
	must(t, err)
	if r, _ := GetCronRun(f.ctx, f.s.R, run, false); r.Status != "running" {
		t.Fatalf("within timeout + grace: %+v", r)
	}
	res, err = f.s.Housekeeping(f.ctx, ny(t, "2026-10-01 00:08"))
	must(t, err)
	if r, _ := GetCronRun(f.ctx, f.s.R, run, false); r.Status != "timeout" || res.Stale < 1 {
		t.Fatalf("stale: %+v %+v", r, res)
	}
	// 300 old runs: after housekeeping 200 remain
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		for i := 0; i < 300; i++ {
			if _, err := tx.ExecContext(f.ctx, `INSERT INTO cron_runs (cron_id, workspace_id, worker_id, status, created_at) VALUES (?,?,?,'ok',?)`,
				cr.ID, f.ws.ID, f.agent.ID, ny(t, "2026-10-02 00:00")); err != nil {
				return err
			}
		}
		return nil
	}))
	_, err = f.s.Housekeeping(f.ctx, ny(t, "2026-11-16 00:00"))
	must(t, err)
	var n int
	must(t, f.s.R.QueryRowContext(f.ctx, `SELECT count(*) FROM cron_runs WHERE cron_id = ?`, cr.ID).Scan(&n))
	if n < 200 || n > 202 {
		t.Fatalf("retention: %d runs left", n)
	}
}

// TestRecurringRefusesFrequent: minute and hour schedules are for crons only.
func TestRecurringRefusesFrequent(t *testing.T) {
	f := newFixture(t)
	err := f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		_, err := CreateRecurrence(f.ctx, tx, f.a, &Recurrence{ProjectID: f.p1.ID, Title: "x", Freq: "minutes", Interval: 5, TZ: "UTC",
			StartsOn: ny(t, "2026-11-16 00:00")})
		return err
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("recurring task every 5 min: %v", err)
	}
}
