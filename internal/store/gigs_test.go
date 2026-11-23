package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

type fixture struct {
	s      *Store
	ctx    context.Context
	a      Actor
	admin  *User
	agent  *User
	ws     *Workspace
	p1, p2 *Project
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir(), quiet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	f := &fixture{s: s, ctx: ctx}
	must(t, s.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		if f.admin, err = CreateUser(ctx, tx, Actor{}, NewUser{Username: "ada", Kind: "human", IsAdmin: true}); err != nil {
			return err
		}
		if f.agent, err = CreateUser(ctx, tx, Actor{}, NewUser{Username: "scout", Kind: "agent"}); err != nil {
			return err
		}
		f.a = Actor{UserID: f.admin.ID}
		if f.ws, err = CreateWorkspace(ctx, tx, f.a, "scout", "Scout", nil); err != nil {
			return err
		}
		if err = SetMembership(ctx, tx, f.a, f.ws.ID, f.agent.ID, "member"); err != nil {
			return err
		}
		if f.p1, err = CreateProject(ctx, tx, f.a, f.ws.ID, "gigg", "gigg", "GIG", nil); err != nil {
			return err
		}
		f.p2, err = CreateProject(ctx, tx, f.a, f.ws.ID, "ops", "Ops", "OPS", nil)
		return err
	}))
	return f
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) gig(t *testing.T, n NewGig) *Gig {
	t.Helper()
	var out *Gig
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		var err error
		out, err = CreateGig(f.ctx, tx, f.a, n)
		return err
	}))
	return out
}

func (f *fixture) tx(t *testing.T, fn func(tx *sql.Tx) error) {
	t.Helper()
	must(t, f.s.Tx(f.ctx, fn))
}

func TestCreateGigDefaults(t *testing.T) {
	f := newFixture(t)
	a := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "first", Labels: []string{"bug", "Bug", "ui"}})
	b := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "second"})
	if a.Key != "GIG-1" || b.Key != "GIG-2" || a.StatusCategory != "todo" || a.StatusName != "To-Do" {
		t.Fatalf("keys/status: %s %s %s", a.Key, b.Key, a.StatusName)
	}
	if len(a.Labels) != 2 || b.Position <= a.Position || a.CreatedByUsername != "ada" || !a.CreatedByAdmin {
		t.Fatalf("labels %v positions %v %v", a.Labels, a.Position, b.Position)
	}
	sts, _ := Statuses(f.ctx, f.s.R, f.p1.ID)
	if len(sts) != 6 || sts[0].Category != "backlog" || !sts[1].IsDefault {
		t.Fatalf("statuses %+v", sts[0])
	}
}

func TestMoveBetweenProjectsKeepsAlias(t *testing.T) {
	f := newFixture(t)
	a := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "moving"})
	var moved *Gig
	f.tx(t, func(tx *sql.Tx) error {
		var err error
		moved, err = MoveGig(f.ctx, tx, f.a, a, a.Version, Move{ProjectID: &f.p2.ID})
		return err
	})
	if moved.Key != "OPS-1" || moved.ProjectID != f.p2.ID || moved.StatusCategory != "todo" || len(moved.Aliases) != 1 || moved.Aliases[0] != "GIG-1" {
		t.Fatalf("moved %+v", moved)
	}
	for _, k := range []string{"GIG-1", "OPS-1"} {
		ids, err := GigIDsByKey(f.ctx, f.s.R, k, nil)
		if err != nil || len(ids) != 1 || ids[0] != a.ID {
			t.Fatalf("%s -> %v %v", k, ids, err)
		}
	}
	// a stale version is refused
	f.tx(t, func(tx *sql.Tx) error {
		if _, err := MoveGig(f.ctx, tx, f.a, moved, a.Version, Move{Category: "done"}); !errors.Is(err, ErrVersion) {
			t.Fatalf("stale version: %v", err)
		}
		return nil
	})
	// GIG numbering continues after the move; the old number is not reused
	b := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "next"})
	if b.Key != "GIG-2" {
		t.Fatalf("number reused: %s", b.Key)
	}
}

func TestDoneAtAndPositions(t *testing.T) {
	f := newFixture(t)
	a := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "a"})
	b := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "b"})
	c := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "c"})
	f.tx(t, func(tx *sql.Tx) error {
		// put c between a and b, many times over (forces renumbering eventually)
		for i := 0; i < 60; i++ {
			cur, _ := GetGig(f.ctx, tx, c.ID)
			if _, err := MoveGig(f.ctx, tx, f.a, cur, 0, Move{Place: Place{Where: "after", GigID: a.ID}}); err != nil {
				return err
			}
			cur, _ = GetGig(f.ctx, tx, b.ID)
			if _, err := MoveGig(f.ctx, tx, f.a, cur, 0, Move{Place: Place{Where: "after", GigID: a.ID}}); err != nil {
				return err
			}
		}
		return nil
	})
	ts, _, _ := ListGigs(f.ctx, f.s.R, GigFilter{WorkspaceID: f.ws.ID, Projects: []int64{f.p1.ID}})
	if len(ts) != 3 || ts[0].ID != a.ID || ts[1].ID != b.ID || ts[2].ID != c.ID {
		t.Fatalf("order %v %v %v", ts[0].Title, ts[1].Title, ts[2].Title)
	}
	var done *Gig
	f.tx(t, func(tx *sql.Tx) error {
		var err error
		cur, _ := GetGig(f.ctx, tx, a.ID)
		if done, err = MoveGig(f.ctx, tx, f.a, cur, 0, Move{Category: "done"}); err != nil {
			return err
		}
		if done.DoneAt == nil {
			t.Fatal("done_at not set")
		}
		back, err := MoveGig(f.ctx, tx, f.a, done, 0, Move{Category: "todo"})
		if err == nil && back.DoneAt != nil {
			t.Fatal("done_at not cleared")
		}
		return err
	})
}

func TestClaimFinishResume(t *testing.T) {
	f := newFixture(t)
	low := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "low", Priority: 1})
	high := f.gig(t, NewGig{ProjectID: f.p2.ID, Title: "high", Priority: 4})
	future := int64(1) << 50
	f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "later", Priority: 4, StartAt: &future})
	agent := Actor{UserID: f.agent.ID}
	var got *Gig
	f.tx(t, func(tx *sql.Tx) error {
		var err error
		if got, err = ClaimGig(f.ctx, tx, agent, f.ws.ID, Claim{StartDue: true, DryRun: true}); err != nil {
			return err
		}
		if got.ID != high.ID || got.StatusCategory != "todo" {
			t.Fatalf("dry run picked %s (%s)", got.Title, got.StatusCategory)
		}
		if got, err = ClaimGig(f.ctx, tx, agent, f.ws.ID, Claim{StartDue: true, Projects: []int64{f.p1.ID}, CreatedBy: []int64{f.admin.ID}}); err != nil {
			return err
		}
		if got.ID != low.ID || got.StatusCategory != "doing" {
			t.Fatalf("claim picked %s (%s)", got.Title, got.StatusCategory)
		}
		if _, err := ClaimGig(f.ctx, tx, agent, f.ws.ID, Claim{StartDue: true, CreatedBy: []int64{f.agent.ID}}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("filter by creator: %v", err)
		}
		return nil
	})
	sid := "6f1c2a3b-4d5e-4f60-8a7b-9c0d1e2f3a4b"
	f.tx(t, func(tx *sql.Tx) error {
		r, err := StartRun(f.ctx, tx, agent, got, &sid, "new", 1, nil)
		if err != nil {
			return err
		}
		pct := int64(40)
		if _, err := RunProgress(f.ctx, tx, r, &pct, "halfway"); err != nil {
			return err
		}
		cost := 0.44
		r, gig, err := FinishRun(f.ctx, tx, agent, r, Finish{Status: "needs_human", ReportMD: "Siren or chime?", CommentKind: "question",
			MoveToCategory: "waiting", CostUSD: &cost})
		if err != nil {
			return err
		}
		if r.Status != "needs_human" || r.ReportCommentID == nil || gig.StatusCategory != "waiting" || gig.CostUSDTotal != 0.44 ||
			gig.SessionID == nil || *gig.SessionID != sid || gig.CommentCount != 1 {
			t.Fatalf("finish: run %+v gig %+v", r, gig)
		}
		if _, _, err := FinishRun(f.ctx, tx, agent, r, Finish{Status: "done"}); !errors.Is(err, ErrConflict) {
			t.Fatalf("finishing twice: %v", err)
		}
		gig, err = ResumeGig(f.ctx, tx, f.a, gig, "chime please")
		if err != nil {
			return err
		}
		if gig.StatusCategory != "todo" || gig.ResumeRequestedAt == nil {
			t.Fatalf("resume: %+v", gig)
		}
		cs, _ := Comments(f.ctx, tx, gig.ID, *r.ReportCommentID, 0)
		if len(cs) != 1 || cs[0].Kind != "resume_request" || !cs[0].AuthorIsAdmin {
			t.Fatalf("resume comment %+v", cs)
		}
		ev, _ := RunEvents(f.ctx, tx, r.ID)
		if len(ev) != 1 || ev[0].Text != "halfway" {
			t.Fatalf("events %+v", ev)
		}
		return nil
	})
	// a parked run: the gig goes back to todo with its start moved to check_at, and a claim skips
	// it until then; the run history says parked, not timeout
	f.tx(t, func(tx *sql.Tx) error {
		gig, err := GetGig(f.ctx, tx, got.ID)
		if err != nil {
			return err
		}
		if gig, err = MoveGig(f.ctx, tx, f.a, gig, 0, Move{Category: "doing"}); err != nil {
			return err
		}
		r, err := StartRun(f.ctx, tx, agent, gig, &sid, "resume", 1, nil)
		if err != nil {
			return err
		}
		late := Now() + 8*86_400_000
		if _, _, err := FinishRun(f.ctx, tx, agent, r, Finish{Status: "parked", CheckAt: &late}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("check_at beyond 7 days: %v", err)
		}
		at := Now() + 20*60_000
		if _, _, err := FinishRun(f.ctx, tx, agent, r, Finish{Status: "done", CheckAt: &at}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("check_at without parked: %v", err)
		}
		r, gig, err = FinishRun(f.ctx, tx, agent, r, Finish{Status: "parked", ReportMD: "Started the long test; checking back.", CommentKind: "comment",
			MoveToCategory: "todo", CheckAt: &at})
		if err != nil {
			return err
		}
		if r.Status != "parked" || gig.StatusCategory != "todo" || gig.StartAt == nil || *gig.StartAt != at {
			t.Fatalf("parked: run %+v gig %+v", r, gig)
		}
		if c, err := ClaimGig(f.ctx, tx, agent, f.ws.ID, Claim{StartDue: true, CreatedBy: []int64{f.admin.ID}}); err == nil && c.ID == gig.ID {
			t.Fatal("a parked gig was claimed before check_at")
		}
		return nil
	})
	f.tx(t, func(tx *sql.Tx) error {
		bad := "not-a-uuid"
		if _, err := StartRun(f.ctx, tx, agent, got, &bad, "new", 1, nil); !errors.Is(err, ErrInvalid) {
			t.Fatalf("bad session id accepted: %v", err)
		}
		return nil
	})
	sum, err := Summary(f.ctx, f.s.R, f.ws.ID, []*Project{f.p1, f.p2}, 5)
	if err != nil || !strings.Contains(sum, "gigg (GIG):") || !strings.Contains(sum, "- To-Do (2): GIG-2 later [P4] (by ada); GIG-1 low [P1] (by ada)") {
		t.Fatalf("summary:\n%s %v", sum, err)
	}
}

func TestActivityIsAppendOnly(t *testing.T) {
	f := newFixture(t)
	if _, err := f.s.W.Exec(`UPDATE activity SET action = 'x'`); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("update allowed: %v", err)
	}
	if _, err := f.s.W.Exec(`DELETE FROM activity`); err == nil {
		t.Fatal("delete allowed")
	}
	var n int
	_ = f.s.R.QueryRow(`SELECT count(*) FROM activity WHERE action = 'project.created'`).Scan(&n)
	if n != 2 {
		t.Fatalf("project.created rows: %d", n)
	}
}

func TestStatusRules(t *testing.T) {
	f := newFixture(t)
	f.tx(t, func(tx *sql.Tx) error {
		sts, _ := Statuses(f.ctx, tx, f.p1.ID)
		done := sts[4]
		if done.Category != "done" {
			t.Fatalf("expected done, got %s", done.Category)
		}
		if err := DeleteStatus(f.ctx, tx, f.a, done); !errors.Is(err, ErrInvalid) {
			t.Fatalf("deleting the only done status: %v", err)
		}
		return nil
	})
}

func TestGigOrder(t *testing.T) {
	for in, want := range map[string]string{
		"due":             "t.due_at IS NULL, t.due_at, t.id",
		"-due":            "t.due_at IS NULL, t.due_at DESC, t.id",
		"-number":         "p.key DESC, t.number DESC, t.id",
		"title,-priority": "lower(t.title), t.priority DESC, t.id",
	} {
		got, err := gigOrder(in)
		if err != nil || got != want {
			t.Errorf("gigOrder(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := gigOrder("bogus"); err == nil {
		t.Error("gigOrder(bogus): want an error")
	}
}

func TestSearchByKey(t *testing.T) {
	f := newFixture(t)
	a := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "first"})
	f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "second"})
	for _, q := range []string{a.Key, strings.ToLower(a.Key), " " + a.Key} {
		ts, _, err := ListGigs(f.ctx, f.s.R, GigFilter{WorkspaceID: f.ws.ID, Q: q, SearchComments: true})
		if err != nil || len(ts) != 1 || ts[0].ID != a.ID {
			t.Errorf("q=%q: got %d gigs, %v; want %s", q, len(ts), err, a.Key)
		}
	}
	if ts, _, _ := ListGigs(f.ctx, f.s.R, GigFilter{WorkspaceID: f.ws.ID, Q: "sec"}); len(ts) != 1 {
		t.Errorf("text search: got %d gigs, want 1", len(ts))
	}
	// moved to another project: its old key still finds it
	old := a.Key
	f.tx(t, func(tx *sql.Tx) error {
		_, err := MoveGig(f.ctx, tx, f.a, a, a.Version, Move{ProjectID: &f.p2.ID})
		return err
	})
	ts, _, err := ListGigs(f.ctx, f.s.R, GigFilter{WorkspaceID: f.ws.ID, Q: old})
	if err != nil || len(ts) != 1 || ts[0].ID != a.ID {
		t.Errorf("old key %s: got %d gigs, %v", old, len(ts), err)
	}
}
