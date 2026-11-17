package store

import (
	"context"
	"database/sql"
	"testing"
)

func TestNormTools(t *testing.T) {
	got, err := NormTools([]string{" Read", "Bash(git log --oneline *)", "", "Read", "mcp__gigg__list_gigs", "Edit(//data/x/**)", "WebFetch(domain:example.com)", `Bash(curl -s "api.example.com/v1/forecast?city=Paris,FR")`})
	if err != nil || len(got) != 6 || got[0] != "Read" || got[1] != "Bash(git log --oneline *)" {
		t.Fatalf("%v %v", got, err)
	}
	if got, err := NormTools(nil); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("nil: %v %v", got, err)
	}
	for _, bad := range []string{"-p", "--allowedTools", "Read,Write", "Bash(a(b))", "Bash(a)b)", "Read\tWrite", "Read Write", "Bash(\n)", "1Read", "Read)", "Bash(x)y"} {
		if _, err := NormTools([]string{bad}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	many := make([]string, MaxToolRules+1)
	for i := range many {
		many[i] = "Bash(x" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + ")"
	}
	if _, err := NormTools(many); err == nil {
		t.Error("too many rules accepted")
	}
}

// TestSchedulerNeedsToolsPermission: a recurring gig whose template has a tool allowlist makes gigs
// only while its creator still holds allowed_tools there.
func TestSchedulerNeedsToolsPermission(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir(), quiet)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var a Actor
	u, err := CreateUser(ctx, s.W, a, NewUser{Username: "atlas", Kind: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	w, err := CreateWorkspace(ctx, s.W, a, "atlas", "Atlas", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetRule(ctx, s.W, a, "user", u.ID, &w.ID, "manager", nil); err != nil {
		t.Fatal(err)
	}
	r := &Recurrence{WorkspaceID: w.ID, CreatedBy: u.ID, AllowedTools: []string{"Read"}}
	if schedulerMayRun(ctx, s.R, r) {
		t.Fatal("ran without allowed_tools")
	}
	if r.AllowedTools = nil; !schedulerMayRun(ctx, s.R, r) {
		t.Fatal("a plain recurring gig should run for a manager")
	}
	if err := SetRule(ctx, s.W, a, "user", u.ID, &w.ID, "manager", map[string]bool{"allowed_tools": true}); err != nil {
		t.Fatal(err)
	}
	if r.AllowedTools = []string{"Read"}; !schedulerMayRun(ctx, s.R, r) {
		t.Fatal("didn't run with allowed_tools")
	}
}

// TestStaleRuns: a run with no heartbeat for StaleRunAfter is closed as interrupted by housekeeping;
// a fresh one is left alone.
func TestStaleRuns(t *testing.T) {
	f := newFixture(t)
	var tk *Gig
	var old, fresh *Run
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		var err error
		if tk, err = CreateGig(f.ctx, tx, f.a, NewGig{ProjectID: f.p1.ID, Title: "long job"}); err != nil {
			return err
		}
		if old, err = StartRun(f.ctx, tx, Actor{UserID: f.agent.ID}, tk, nil, "new", 1, nil); err != nil {
			return err
		}
		fresh, err = StartRun(f.ctx, tx, Actor{UserID: f.agent.ID}, tk, nil, "new", 1, nil)
		return err
	}))
	now := Now()
	if _, err := f.s.W.ExecContext(f.ctx, "UPDATE runs SET heartbeat_at = ? WHERE id = ?", now-StaleRunAfter.Milliseconds()-1000, old.ID); err != nil {
		t.Fatal(err)
	}
	res, err := f.s.Housekeeping(f.ctx, now)
	if err != nil || res.Stale != 1 {
		t.Fatalf("housekeeping: %+v %v", res, err)
	}
	for id, want := range map[int64]string{old.ID: "interrupted", fresh.ID: "running"} {
		var st string
		if err := f.s.R.QueryRowContext(f.ctx, "SELECT status FROM runs WHERE id = ?", id).Scan(&st); err != nil || st != want {
			t.Fatalf("run %d: %q %v, want %s", id, st, err, want)
		}
	}
}
