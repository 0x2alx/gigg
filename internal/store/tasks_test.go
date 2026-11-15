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

func (f *fixture) task(t *testing.T, n NewTask) *Task {
	t.Helper()
	var out *Task
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		var err error
		out, err = CreateTask(f.ctx, tx, f.a, n)
		return err
	}))
	return out
}

func (f *fixture) tx(t *testing.T, fn func(tx *sql.Tx) error) {
	t.Helper()
	must(t, f.s.Tx(f.ctx, fn))
}

func TestCreateTaskDefaults(t *testing.T) {
	f := newFixture(t)
	a := f.task(t, NewTask{ProjectID: f.p1.ID, Title: "first", Labels: []string{"bug", "Bug", "ui"}})
	b := f.task(t, NewTask{ProjectID: f.p1.ID, Title: "second"})
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
	a := f.task(t, NewTask{ProjectID: f.p1.ID, Title: "moving"})
	var moved *Task
	f.tx(t, func(tx *sql.Tx) error {
		var err error
		moved, err = MoveTask(f.ctx, tx, f.a, a, a.Version, Move{ProjectID: &f.p2.ID})
		return err
	})
	if moved.Key != "OPS-1" || moved.ProjectID != f.p2.ID || moved.StatusCategory != "todo" || len(moved.Aliases) != 1 || moved.Aliases[0] != "GIG-1" {
		t.Fatalf("moved %+v", moved)
	}
	for _, k := range []string{"GIG-1", "OPS-1"} {
		ids, err := TaskIDsByKey(f.ctx, f.s.R, k, nil)
		if err != nil || len(ids) != 1 || ids[0] != a.ID {
			t.Fatalf("%s -> %v %v", k, ids, err)
		}
	}
	// a stale version is refused
	f.tx(t, func(tx *sql.Tx) error {
		if _, err := MoveTask(f.ctx, tx, f.a, moved, a.Version, Move{Category: "done"}); !errors.Is(err, ErrVersion) {
			t.Fatalf("stale version: %v", err)
		}
		return nil
	})
	// GIG numbering continues after the move; the old number is not reused
	b := f.task(t, NewTask{ProjectID: f.p1.ID, Title: "next"})
	if b.Key != "GIG-2" {
		t.Fatalf("number reused: %s", b.Key)
	}
}

func TestDoneAtAndPositions(t *testing.T) {
	f := newFixture(t)
	a := f.task(t, NewTask{ProjectID: f.p1.ID, Title: "a"})
	b := f.task(t, NewTask{ProjectID: f.p1.ID, Title: "b"})
	c := f.task(t, NewTask{ProjectID: f.p1.ID, Title: "c"})
	f.tx(t, func(tx *sql.Tx) error {
		// put c between a and b, many times over (forces renumbering eventually)
		for i := 0; i < 60; i++ {
			cur, _ := GetTask(f.ctx, tx, c.ID)
			if _, err := MoveTask(f.ctx, tx, f.a, cur, 0, Move{Place: Place{Where: "after", TaskID: a.ID}}); err != nil {
				return err
			}
			cur, _ = GetTask(f.ctx, tx, b.ID)
			if _, err := MoveTask(f.ctx, tx, f.a, cur, 0, Move{Place: Place{Where: "after", TaskID: a.ID}}); err != nil {
				return err
			}
		}
		return nil
	})
	ts, _, _ := ListTasks(f.ctx, f.s.R, TaskFilter{WorkspaceID: f.ws.ID, Projects: []int64{f.p1.ID}})
	if len(ts) != 3 || ts[0].ID != a.ID || ts[1].ID != b.ID || ts[2].ID != c.ID {
		t.Fatalf("order %v %v %v", ts[0].Title, ts[1].Title, ts[2].Title)
	}
	var done *Task
	f.tx(t, func(tx *sql.Tx) error {
		var err error
		cur, _ := GetTask(f.ctx, tx, a.ID)
		if done, err = MoveTask(f.ctx, tx, f.a, cur, 0, Move{Category: "done"}); err != nil {
			return err
		}
		if done.DoneAt == nil {
			t.Fatal("done_at not set")
		}
		back, err := MoveTask(f.ctx, tx, f.a, done, 0, Move{Category: "todo"})
		if err == nil && back.DoneAt != nil {
			t.Fatal("done_at not cleared")
		}
		return err
	})
}

func TestClaimFinishResume(t *testing.T) {
	f := newFixture(t)
	low := f.task(t, NewTask{ProjectID: f.p1.ID, Title: "low", Priority: 1})
	high := f.task(t, NewTask{ProjectID: f.p2.ID, Title: "high", Priority: 4})
	future := int64(1) << 50
	f.task(t, NewTask{ProjectID: f.p1.ID, Title: "later", Priority: 4, StartAt: &future})
	agent := Actor{UserID: f.agent.ID}
	var got *Task
	f.tx(t, func(tx *sql.Tx) error {
		var err error
		if got, err = ClaimTask(f.ctx, tx, agent, f.ws.ID, Claim{StartDue: true, DryRun: true}); err != nil {
			return err
		}
		if got.ID != high.ID || got.StatusCategory != "todo" {
			t.Fatalf("dry run picked %s (%s)", got.Title, got.StatusCategory)
		}
		if got, err = ClaimTask(f.ctx, tx, agent, f.ws.ID, Claim{StartDue: true, Projects: []int64{f.p1.ID}, CreatedBy: []int64{f.admin.ID}}); err != nil {
			return err
		}
		if got.ID != low.ID || got.StatusCategory != "doing" {
			t.Fatalf("claim picked %s (%s)", got.Title, got.StatusCategory)
		}
		if _, err := ClaimTask(f.ctx, tx, agent, f.ws.ID, Claim{StartDue: true, CreatedBy: []int64{f.agent.ID}}); !errors.Is(err, ErrNotFound) {
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
		r, task, err := FinishRun(f.ctx, tx, agent, r, Finish{Status: "needs_human", ReportMD: "Siren or chime?", CommentKind: "question",
			MoveToCategory: "waiting", CostUSD: &cost})
		if err != nil {
			return err
		}
		if r.Status != "needs_human" || r.ReportCommentID == nil || task.StatusCategory != "waiting" || task.CostUSDTotal != 0.44 ||
			task.SessionID == nil || *task.SessionID != sid || task.CommentCount != 1 {
			t.Fatalf("finish: run %+v task %+v", r, task)
		}
		if _, _, err := FinishRun(f.ctx, tx, agent, r, Finish{Status: "done"}); !errors.Is(err, ErrConflict) {
			t.Fatalf("finishing twice: %v", err)
		}
		task, err = ResumeTask(f.ctx, tx, f.a, task, "chime please")
		if err != nil {
			return err
		}
		if task.StatusCategory != "todo" || task.ResumeRequestedAt == nil {
			t.Fatalf("resume: %+v", task)
		}
		cs, _ := Comments(f.ctx, tx, task.ID, *r.ReportCommentID, 0)
		if len(cs) != 1 || cs[0].Kind != "resume_request" || !cs[0].AuthorIsAdmin {
			t.Fatalf("resume comment %+v", cs)
		}
		ev, _ := RunEvents(f.ctx, tx, r.ID)
		if len(ev) != 1 || ev[0].Text != "halfway" {
			t.Fatalf("events %+v", ev)
		}
		return nil
	})
	// a parked run: the task goes back to todo with its start moved to check_at, and a claim skips
	// it until then; the run history says parked, not timeout
	f.tx(t, func(tx *sql.Tx) error {
		task, err := GetTask(f.ctx, tx, got.ID)
		if err != nil {
			return err
		}
		if task, err = MoveTask(f.ctx, tx, f.a, task, 0, Move{Category: "doing"}); err != nil {
			return err
		}
		r, err := StartRun(f.ctx, tx, agent, task, &sid, "resume", 1, nil)
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
		r, task, err = FinishRun(f.ctx, tx, agent, r, Finish{Status: "parked", ReportMD: "Started the long test; checking back.", CommentKind: "comment",
			MoveToCategory: "todo", CheckAt: &at})
		if err != nil {
			return err
		}
		if r.Status != "parked" || task.StatusCategory != "todo" || task.StartAt == nil || *task.StartAt != at {
			t.Fatalf("parked: run %+v task %+v", r, task)
		}
		if c, err := ClaimTask(f.ctx, tx, agent, f.ws.ID, Claim{StartDue: true, CreatedBy: []int64{f.admin.ID}}); err == nil && c.ID == task.ID {
			t.Fatal("a parked task was claimed before check_at")
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

func TestTaskOrder(t *testing.T) {
	for in, want := range map[string]string{
		"due":             "t.due_at IS NULL, t.due_at, t.id",
		"-due":            "t.due_at IS NULL, t.due_at DESC, t.id",
		"-number":         "p.key DESC, t.number DESC, t.id",
		"title,-priority": "t.title COLLATE NOCASE, t.priority DESC, t.id",
	} {
		got, err := taskOrder(in)
		if err != nil || got != want {
			t.Errorf("taskOrder(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := taskOrder("bogus"); err == nil {
		t.Error("taskOrder(bogus): want an error")
	}
}
