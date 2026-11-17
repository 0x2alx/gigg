package store

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
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

type errandFix struct {
	*fixture
	t *testing.T
}

func (c errandFix) create(n NewErrand, can bool) (*Errand, error) {
	var out *Errand
	err := c.s.Tx(c.ctx, func(tx *sql.Tx) error {
		var err error
		out, err = CreateErrand(c.ctx, tx, c.a, n, ErrandEdit{CanUnsandbox: can})
		return err
	})
	return out, err
}

func (c errandFix) createAs(a Actor, n NewErrand) (*Errand, error) {
	var out *Errand
	err := c.s.Tx(c.ctx, func(tx *sql.Tx) error {
		var err error
		out, err = CreateErrand(c.ctx, tx, a, n, ErrandEdit{})
		return err
	})
	return out, err
}

// GigByKeyForTest returns a gig's description.
func GigByKeyForTest(f *fixture, key string) (string, error) {
	var d string
	err := f.s.R.QueryRowContext(f.ctx, `SELECT t.description_md FROM gigs t JOIN projects p ON p.id = t.project_id
		WHERE p.key || '-' || t.number = ?`, key).Scan(&d)
	return d, err
}

func (c errandFix) claim(at string) []*ClaimedErrand {
	c.t.Helper()
	Now = func() int64 { return ny(c.t, at) }
	var out []*ClaimedErrand
	must(c.t, c.s.Tx(c.ctx, func(tx *sql.Tx) error {
		var err error
		out, err = ClaimErrands(c.ctx, tx, c.agent.ID, c.ws.ID, 0)
		return err
	}))
	return out
}

func (c errandFix) finish(run int64, f ErrandFinish) *ErrandOutcome {
	c.t.Helper()
	var out *ErrandOutcome
	must(c.t, c.s.Tx(c.ctx, func(tx *sql.Tx) error {
		var err error
		out, err = FinishErrandRun(c.ctx, tx, c.agent.ID, run, f)
		return err
	}))
	return out
}

func (c errandFix) runs(id int64) []string {
	c.t.Helper()
	rs, err := ListErrandRuns(c.ctx, c.s.R, id, 0, 50)
	must(c.t, err)
	var out []string
	for i := len(rs) - 1; i >= 0; i-- {
		out = append(out, rs[i].Status)
	}
	return out
}

// TestErrandClaimAndFinish: due slots run once; a late executor records missed slots and runs the latest
// once if recent; a run still going makes the slot "skipped"; run-now runs once; state moves to
// failing on the first failure (a gig when asked) and back on success.
func TestErrandClaimAndFinish(t *testing.T) {
	f := newFixture(t)
	defer func(n func() int64) { Now = n }(Now)
	c := errandFix{f, t}
	Now = func() int64 { return ny(t, "2026-11-16 14:02") }
	cr, err := c.create(NewErrand{WorkspaceID: f.ws.ID, WorkerID: f.agent.ID, Template: &ErrandTemplate{Name: "disk check", Command: "df -h /"},
		Rule: &Rule{Freq: "minutes", Interval: 5, TZ: "America/New_York"}, OnFailureGig: true}, false)
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
	o := c.finish(first, ErrandFinish{Status: "failed", ExitCode: ptr(int64(1)), DurationMS: 900, OutputTail: ptr("disk full"), OutputBytes: 9})
	if o.State != "failing" || !o.BecameFailing || o.GigKey == "" {
		t.Fatalf("first failure: %+v", o)
	}
	ids, err := GigIDsByKey(f.ctx, f.s.R, o.GigKey, []int64{f.ws.ID})
	must(t, err)
	if len(ids) != 1 {
		t.Fatalf("gig %s: %v", o.GigKey, ids)
	}
	gig, err := GetGig(f.ctx, f.s.R, ids[0])
	must(t, err)
	if gig.CreatedBy != f.agent.ID || strings.Contains(gig.DescriptionMD, "disk full") || !strings.Contains(gig.DescriptionMD, "/errands/") {
		t.Fatalf("failure gig: %+v", gig)
	}
	// a late executor: 14:15 .. 14:35 passed; at 14:36 the 14:35 slot is recent: one missed record, one run
	got = c.claim("2026-11-16 14:36")
	if len(got) != 1 || got[0].ScheduledFor == nil || fmtNY(*got[0].ScheduledFor) != "Mon 2026-11-16 14:35 EST" {
		t.Fatalf("late: %+v", got)
	}
	o = c.finish(got[0].RunID, ErrandFinish{Status: "failed", ExitCode: ptr(int64(2))})
	if o.BecameFailing || o.FailStreak != 2 || o.GigKey != "" {
		t.Fatalf("second failure: no new gig, streak 2: %+v", o)
	}
	o = c.finish(c.claim("2026-11-16 14:40")[0].RunID, ErrandFinish{Status: "ok", ExitCode: ptr(int64(0))})
	if !o.Recovered || o.State != "ok" {
		t.Fatalf("recovered: %+v", o)
	}
	eq(t, c.runs(cr.ID), "failed", "skipped", "missed", "failed", "ok")
	// run now: once, whatever the schedule
	Now = func() int64 { return ny(t, "2026-11-16 14:41") }
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		cur, _ := GetErrand(f.ctx, tx, cr.ID)
		_, err := RunErrandNow(f.ctx, tx, f.a, cur)
		return err
	}))
	got = c.claim("2026-11-16 14:41")
	if len(got) != 1 || got[0].ScheduledFor != nil {
		t.Fatalf("run now: %+v", got)
	}
	c.finish(got[0].RunID, ErrandFinish{Status: "ok"})
	if got := c.claim("2026-11-16 14:42"); len(got) != 0 {
		t.Fatalf("run now runs once: %+v", got)
	}

	// a daily errand whose executor was away for days: one missed record, no run, next slot tomorrow
	Now = func() int64 { return ny(t, "2026-11-16 08:00") }
	d, err := c.create(NewErrand{WorkspaceID: f.ws.ID, WorkerID: f.agent.ID, Template: &ErrandTemplate{Name: "nightly", Command: "true"},
		Rule: &Rule{Freq: "daily", TZ: "America/New_York", WindowStart: "03:30"}}, false)
	must(t, err)
	got = c.claim("2026-11-19 10:00")
	for _, g := range got {
		if g.ErrandID == d.ID {
			t.Fatalf("a slot 6.5 h old must not run: %+v", g)
		}
	}
	eq(t, c.runs(d.ID), "missed")
	d, _ = GetErrand(f.ctx, f.s.R, d.ID)
	if fmtNY(*d.NextAt) != "Fri 2026-11-20 03:30 EST" {
		t.Fatalf("next: %s", fmtNY(*d.NextAt))
	}

	// the workspace's errands paused: nothing claimed
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error { return PauseErrands(f.ctx, tx, f.a, f.ws.ID) }))
	Now = func() int64 { return ny(t, "2026-11-20 03:31") }
	err = f.s.Tx(f.ctx, func(tx *sql.Tx) error { _, err := ClaimErrands(f.ctx, tx, f.agent.ID, f.ws.ID, 0); return err })
	if !errors.Is(err, ErrErrandsPaused) {
		t.Fatalf("paused: %v", err)
	}
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error { return ResumeErrands(f.ctx, tx, f.a, f.ws.ID) }))
	d, _ = GetErrand(f.ctx, f.s.R, d.ID)
	if fmtNY(*d.NextAt) != "Sat 2026-11-21 03:30 EST" {
		t.Fatalf("resume starts from the next slot, no catch-up: %s", fmtNY(*d.NextAt))
	}
}

// TestErrandUnsandboxed: only someone holding the permission turns the sandbox off; any change by
// someone without it turns it back on; a flag whose setter lost the permission isn't run at all.
func TestErrandUnsandboxed(t *testing.T) {
	f := newFixture(t)
	defer func(n func() int64) { Now = n }(Now)
	c := errandFix{f, t}
	Now = func() int64 { return ny(t, "2026-11-16 14:02") }
	n := NewErrand{WorkspaceID: f.ws.ID, WorkerID: f.agent.ID, Template: &ErrandTemplate{Name: "tts check", Command: "curl -s 127.0.0.1:9000/health",
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
	c.finish(got[0].RunID, ErrandFinish{Status: "ok"})
	if cr.UnsandboxedBy == nil || *cr.UnsandboxedBy != f.admin.ID || got[0].UnsandboxedSetBy != "ada" {
		t.Fatalf("who turned it off: %+v / %+v", cr, got[0])
	}
	// the agent (no permission) changes only the schedule: the flag goes off; the command keeps its author
	agent := Actor{UserID: f.agent.ID}
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		_, err := UpdateErrand(f.ctx, tx, agent, cr.ID, 0, ErrandChange{Rule: &Rule{Freq: "minutes", Interval: 10, TZ: "America/New_York"}}, ErrandEdit{})
		return err
	}))
	cr, _ = GetErrand(f.ctx, f.s.R, cr.ID)
	if cr.Unsandboxed || cr.UnsandboxedBy != nil || cr.CommandSetBy != f.admin.ID {
		t.Fatalf("cleared: %+v", cr)
	}
	// and it can't turn it back on
	on := true
	err = f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		_, err := UpdateErrand(f.ctx, tx, agent, cr.ID, 0, ErrandChange{Unsandboxed: &on}, ErrandEdit{})
		return err
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("re-enable without the permission: %v", err)
	}
	// a flag set by someone who has since lost the permission: the run is refused, never sandboxed instead
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(f.ctx, `UPDATE errand_templates SET unsandboxed = 1, unsandboxed_set_by = ? WHERE id = ?`, f.agent.ID, cr.TemplateID)
		return err
	}))
	got = c.claim("2026-11-16 14:20")
	if len(got) != 0 {
		t.Fatalf("must not be handed out: %+v", got)
	}
	rs, _ := ListErrandRuns(f.ctx, f.s.R, cr.ID, 0, 1)
	if rs[0].Status != "failed" || rs[0].Error == nil || !strings.Contains(*rs[0].Error, "no longer has") {
		t.Fatalf("refused run: %+v", rs[0])
	}
}

// TestErrandProvenance: the command's author changes only with the
// command text; someone without errand_unsandboxed touching an errand on a shared no-sandbox template gets a
// sandboxed copy (the other errands keep theirs); output host -> gigg only by the author or a manager.
func TestErrandProvenance(t *testing.T) {
	f := newFixture(t)
	defer func(n func() int64) { Now = n }(Now)
	c := errandFix{f, t}
	Now = func() int64 { return ny(t, "2026-11-16 14:02") }
	agent := Actor{UserID: f.agent.ID}
	every := &Rule{Freq: "minutes", Interval: 5, TZ: "America/New_York"}
	// a timeout edit by ada on the agent's command keeps the agent as its author
	var mine *Errand
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		var err error
		mine, err = CreateErrand(f.ctx, tx, agent, NewErrand{WorkspaceID: f.ws.ID, WorkerID: f.agent.ID,
			Template: &ErrandTemplate{Name: "mine", Command: "echo agent"}, Rule: every}, ErrandEdit{})
		return err
	}))
	ten := int64(600)
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		_, err := UpdateErrand(f.ctx, tx, f.a, mine.ID, 0, ErrandChange{TimeoutS: &ten}, ErrandEdit{CanUnsandbox: true, CanManage: true})
		return err
	}))
	mine, _ = GetErrand(f.ctx, f.s.R, mine.ID)
	if mine.TimeoutS != 600 || mine.CommandSetBy != f.agent.ID {
		t.Fatalf("timeout edit re-attributed the command: %+v", mine)
	}
	// a shared no-sandbox template, two errands on it (ada's)
	one, err := c.create(NewErrand{WorkspaceID: f.ws.ID, WorkerID: f.agent.ID, Template: &ErrandTemplate{Name: "local", Command: "curl 127.0.0.1",
		Unsandboxed: true, Output: "host"}, Rule: every}, true)
	must(t, err)
	two, err := c.create(NewErrand{WorkspaceID: f.ws.ID, WorkerID: f.agent.ID, TemplateID: one.TemplateID, Rule: every}, true)
	must(t, err)
	// asking to keep the flag on doesn't skip the copy, and changes the shared template nowhere
	on, cmd := true, "curl evil"
	err = f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		_, err := UpdateErrand(f.ctx, tx, agent, two.ID, 0, ErrandChange{Unsandboxed: &on, Command: &cmd}, ErrandEdit{})
		return err
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("non-holder sending unsandboxed=true: %v", err)
	}
	// nor straight on the template
	shared, _ := GetErrandTemplate(f.ctx, f.s.R, one.TemplateID)
	edited := *shared
	edited.Command = "curl evil"
	err = f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		_, err := SaveErrandTemplate(f.ctx, tx, agent, &edited, 0, ErrandEdit{})
		return err
	})
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("non-holder saving a no-sandbox template: %v", err)
	}
	if shared, _ = GetErrandTemplate(f.ctx, f.s.R, one.TemplateID); shared.Command != "curl 127.0.0.1" || !shared.Unsandboxed {
		t.Fatalf("shared template changed: %+v", shared)
	}
	// the agent retimes the second: it gets a sandboxed copy, the first keeps the shared template
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		_, err := UpdateErrand(f.ctx, tx, agent, two.ID, 0, ErrandChange{Rule: &Rule{Freq: "minutes", Interval: 10, TZ: "America/New_York"}}, ErrandEdit{})
		return err
	}))
	one, _ = GetErrand(f.ctx, f.s.R, one.ID)
	two, _ = GetErrand(f.ctx, f.s.R, two.ID)
	if !one.Unsandboxed || two.Unsandboxed || two.TemplateID == one.TemplateID || two.CommandSetBy != f.admin.ID || two.Command != one.Command {
		t.Fatalf("copy on write: one %+v two %+v", one, two)
	}
	// the agent makes a new errand on the shared template: a sandboxed copy too, the shared one untouched
	three, err := c.createAs(agent, NewErrand{WorkspaceID: f.ws.ID, WorkerID: f.agent.ID, TemplateID: one.TemplateID, Rule: every})
	must(t, err)
	one, _ = GetErrand(f.ctx, f.s.R, one.ID)
	if three.Unsandboxed || three.TemplateID == one.TemplateID || !one.Unsandboxed {
		t.Fatalf("new errand on a shared no-sandbox template: three %+v one %+v", three, one)
	}
	// output host -> gigg: not the agent (not the author, no Manage), yes ada
	gigg := "gigg"
	err = f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		_, err := UpdateErrand(f.ctx, tx, agent, three.ID, 0, ErrandChange{Output: &gigg}, ErrandEdit{})
		return err
	})
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("host -> gigg by the agent: %v", err)
	}
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		_, err := UpdateErrand(f.ctx, tx, f.a, three.ID, 0, ErrandChange{Output: &gigg}, ErrandEdit{CanUnsandbox: true, CanManage: true})
		return err
	}))
	// a stale version is refused (the errand is read again inside the transaction)
	err = f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		_, err := UpdateErrand(f.ctx, tx, f.a, three.ID, three.Version, ErrandChange{TimeoutS: &ten}, ErrandEdit{CanUnsandbox: true})
		return err
	})
	if !errors.Is(err, ErrVersion) {
		t.Fatalf("stale version: %v", err)
	}
}

// TestErrandClaimLimit: a claim starts at most limit runs; the rest stay due for the next one. A report
// past 64 KiB keeps the last 64 KiB on a character boundary; the failure gig carries no output.
func TestErrandClaimLimit(t *testing.T) {
	f := newFixture(t)
	defer func(n func() int64) { Now = n }(Now)
	c := errandFix{f, t}
	Now = func() int64 { return ny(t, "2026-11-16 14:02") }
	var ids []int64
	for _, name := range []string{"a", "b", "c"} {
		cr, err := c.create(NewErrand{WorkspaceID: f.ws.ID, WorkerID: f.agent.ID, Template: &ErrandTemplate{Name: name, Command: "true"},
			Rule: &Rule{Freq: "minutes", Interval: 5, TZ: "America/New_York"}, OnFailureGig: name == "a"}, false)
		must(t, err)
		ids = append(ids, cr.ID)
	}
	Now = func() int64 { return ny(t, "2026-11-16 14:05") }
	var got []*ClaimedErrand
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		var err error
		got, err = ClaimErrands(f.ctx, tx, f.agent.ID, f.ws.ID, 1)
		return err
	}))
	if len(got) != 1 || got[0].ErrandID != ids[0] {
		t.Fatalf("limit 1: %+v", got)
	}
	rest := c.claim("2026-11-16 14:05")
	if len(rest) != 2 {
		t.Fatalf("the others stay due: %+v", rest)
	}
	big := strings.Repeat("é", 40000) + "END" // 80003 bytes
	o := c.finish(got[0].RunID, ErrandFinish{Status: "failed", ExitCode: new(int64), OutputTail: &big, OutputBytes: int64(len(big))})
	r, _ := GetErrandRun(f.ctx, f.s.R, got[0].RunID, true)
	if r.OutputTail == nil || len(*r.OutputTail) > ErrandOutputMax || !strings.HasSuffix(*r.OutputTail, "END") || !utf8.ValidString(*r.OutputTail) {
		t.Fatalf("tail kept: %d bytes", len(*r.OutputTail))
	}
	if o.GigKey == "" {
		t.Fatalf("failure gig: %+v", o)
	}
	gig, err := GigByKeyForTest(f, o.GigKey)
	must(t, err)
	if strings.Contains(gig, "éé") || strings.Contains(gig, "END") || !strings.Contains(gig, "not instructions") {
		t.Fatalf("failure gig text: %q", gig)
	}
}

// TestErrandHousekeeping: a run whose executor never reported is closed as a timeout; old runs are
// trimmed (the newest 200 and the last 14 days stay).
func TestErrandHousekeeping(t *testing.T) {
	f := newFixture(t)
	defer func(n func() int64) { Now = n }(Now)
	c := errandFix{f, t}
	Now = func() int64 { return ny(t, "2026-10-01 00:00") }
	cr, err := c.create(NewErrand{WorkspaceID: f.ws.ID, WorkerID: f.agent.ID, Template: &ErrandTemplate{Name: "x", Command: "true", TimeoutS: 60},
		Rule: &Rule{Freq: "minutes", Interval: 1, TZ: "America/New_York"}}, false)
	must(t, err)
	run := c.claim("2026-10-01 00:01")[0].RunID
	res, err := f.s.Housekeeping(f.ctx, ny(t, "2026-10-01 00:05"))
	must(t, err)
	if r, _ := GetErrandRun(f.ctx, f.s.R, run, false); r.Status != "running" {
		t.Fatalf("within timeout + grace: %+v", r)
	}
	res, err = f.s.Housekeeping(f.ctx, ny(t, "2026-10-01 00:08"))
	must(t, err)
	if r, _ := GetErrandRun(f.ctx, f.s.R, run, false); r.Status != "timeout" || res.Stale < 1 {
		t.Fatalf("stale: %+v %+v", r, res)
	}
	// 300 old runs: after housekeeping 200 remain
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		for i := 0; i < 300; i++ {
			if _, err := tx.ExecContext(f.ctx, `INSERT INTO errand_runs (errand_id, workspace_id, worker_id, status, created_at) VALUES (?,?,?,'ok',?)`,
				cr.ID, f.ws.ID, f.agent.ID, ny(t, "2026-10-02 00:00")); err != nil {
				return err
			}
		}
		return nil
	}))
	_, err = f.s.Housekeeping(f.ctx, ny(t, "2026-11-16 00:00"))
	must(t, err)
	var n int
	must(t, f.s.R.QueryRowContext(f.ctx, `SELECT count(*) FROM errand_runs WHERE errand_id = ?`, cr.ID).Scan(&n))
	if n < 200 || n > 202 {
		t.Fatalf("retention: %d runs left", n)
	}
}

// TestRecurringRefusesFrequent: minute and hour schedules are for errands only.
func TestRecurringRefusesFrequent(t *testing.T) {
	f := newFixture(t)
	err := f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		_, err := CreateRecurrence(f.ctx, tx, f.a, &Recurrence{ProjectID: f.p1.ID, Title: "x", Freq: "minutes", Interval: 5, TZ: "UTC",
			StartsOn: ny(t, "2026-11-16 00:00")})
		return err
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("recurring gig every 5 min: %v", err)
	}
}
