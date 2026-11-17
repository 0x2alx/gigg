package store

import (
	"context"
	"database/sql"
	"testing"
)

// A subgig marked blocks_parent holds its parent back (blocked_by, the blocked filter, claim)
// until it is done; an ordinary subgig doesn't.
func TestSubgigBlocksParent(t *testing.T) {
	f := newFixture(t)
	parent := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "ship release", Priority: 4})
	pid := parent.ID
	plain := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "update wiki", ParentID: &pid})
	gate := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "tests pass", ParentID: &pid, BlocksParent: true})
	if !gate.BlocksParent || plain.BlocksParent {
		t.Fatalf("blocks_parent %v %v", gate.BlocksParent, plain.BlocksParent)
	}
	got, _ := GetGig(f.ctx, f.s.R, pid)
	if got.BlockedBy != 1 || got.BlockedBySubgigs != 1 || got.SubgigCount != 2 {
		t.Fatalf("parent: blocked_by %d (subgigs %d), subgigs %d", got.BlockedBy, got.BlockedBySubgigs, got.SubgigCount)
	}
	if ts, _, _ := ListGigs(f.ctx, f.s.R, GigFilter{WorkspaceID: f.ws.ID, BlockedOnly: true}); len(ts) != 1 || ts[0].ID != pid {
		t.Fatalf("blocked filter: %d", len(ts))
	}
	agent := Actor{UserID: f.agent.ID}
	claim := func() *Gig {
		var c *Gig
		f.tx(t, func(tx *sql.Tx) error {
			var err error
			c, err = ClaimGig(f.ctx, tx, agent, f.ws.ID, Claim{DryRun: true})
			return err
		})
		return c
	}
	if c := claim(); c.ID == pid {
		t.Fatal("claimed a parent its subgig holds back")
	}
	// a blocking subgig can't block its own blocker: no cycle through a dependency
	f.tx(t, func(tx *sql.Tx) error {
		if err := AddRelation(f.ctx, tx, f.a, parent, gate, "blocks"); err == nil {
			t.Fatal("a cycle through blocks_parent was allowed")
		}
		return nil
	})
	f.tx(t, func(tx *sql.Tx) error {
		_, err := MoveGig(f.ctx, tx, f.a, gate, 0, Move{Category: "done"})
		return err
	})
	got, _ = GetGig(f.ctx, f.s.R, pid)
	if got.BlockedBy != 0 {
		t.Fatalf("still blocked by %d after the subgig is done", got.BlockedBy)
	}
	if c := claim(); c.ID != pid {
		t.Fatalf("claim picked %s, want the parent (highest priority)", c.Title)
	}
	// blocks_parent needs a parent; clearing the parent clears it
	f.tx(t, func(tx *sql.Tx) error {
		if _, err := CreateGig(f.ctx, tx, f.a, NewGig{ProjectID: f.p1.ID, Title: "orphan", BlocksParent: true}); err == nil {
			t.Fatal("blocks_parent without a parent was accepted")
		}
		var none *int64
		p, err := PatchGig(f.ctx, tx, f.a, plain, 0, GigPatch{BlocksParent: ptr(true)})
		if err != nil || !p.BlocksParent {
			t.Fatalf("patch on: %v", err)
		}
		p, err = PatchGig(f.ctx, tx, f.a, p, 0, GigPatch{ParentID: &none})
		if err != nil || p.BlocksParent {
			t.Fatalf("unparented subgig kept blocks_parent: %v", err)
		}
		return nil
	})
}

// A template's subgigs are created with every gig made from it, by hand or by a recurring gig.
func TestTemplateSubgigs(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	var gig *Gig
	f.tx(t, func(tx *sql.Tx) error {
		tpl, err := SaveTemplate(ctx, tx, f.a, &Template{WorkspaceID: f.ws.ID, Name: "release", Title: "Ship release", Priority: 3,
			Assignees: []int64{f.agent.ID}, Subgigs: []TemplateSubgig{{Title: " tag build "}, {Title: "tests pass", BlocksParent: true}}}, 0)
		if err != nil {
			return err
		}
		if len(tpl.Subgigs) != 2 || tpl.Subgigs[0].Title != "tag build" || !tpl.Subgigs[1].BlocksParent {
			t.Fatalf("stored subgigs %+v", tpl.Subgigs)
		}
		if _, err := SaveTemplate(ctx, tx, f.a, &Template{WorkspaceID: f.ws.ID, Name: "bad", Title: "x", Subgigs: []TemplateSubgig{{Title: " "}}}, 0); err == nil {
			t.Fatal("an empty subgig title was accepted")
		}
		gig, err = GigFromTemplate(ctx, tx, f.a, tpl, f.p1, nil, nil)
		return err
	})
	pid := gig.ID
	subs, _, err := ListGigs(ctx, f.s.R, GigFilter{WorkspaceID: f.ws.ID, ParentID: &pid})
	if err != nil || len(subs) != 2 {
		t.Fatalf("subgigs %d %v", len(subs), err)
	}
	for _, s := range subs {
		if s.Priority != 3 || len(s.Assignees) != 1 || s.Assignees[0] != f.agent.ID {
			t.Fatalf("subgig %s not made like its parent: %+v", s.Title, s)
		}
	}
	if gig.BlockedBy != 1 || gig.SubgigCount != 2 {
		t.Fatalf("parent blocked_by %d subgigs %d", gig.BlockedBy, gig.SubgigCount)
	}
}

func ptr[T any](v T) *T { return &v }

// A recurring gig's template subgigs come with every occurrence, in the same window, and don't
// count as open occurrences of their own.
func TestRecurringSubgigs(t *testing.T) {
	f := newFixture(t)
	start := ny(t, "2026-11-12 00:00")
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		_, err := CreateRecurrence(f.ctx, tx, f.a, &Recurrence{ProjectID: f.p1.ID, Title: "Morning check", Freq: "daily", TZ: "America/New_York",
			WindowStart: "09:00", WindowMinutes: 120, StartsOn: start, Assignees: []int64{f.agent.ID},
			Subgigs: []TemplateSubgig{{Title: "gather", BlocksParent: true}, {Title: "post"}}})
		return err
	}))
	res, err := f.s.Housekeeping(f.ctx, ny(t, "2026-11-12 09:00"))
	must(t, err)
	if res.Created != 1 {
		t.Fatalf("created %+v", res)
	}
	gigs, _, err := ListGigs(f.ctx, f.s.R, GigFilter{WorkspaceID: f.ws.ID})
	must(t, err)
	if len(gigs) != 3 {
		t.Fatalf("gigs: %d, want the occurrence and its 2 subgigs", len(gigs))
	}
	var parent *Gig
	for _, tk := range gigs {
		if tk.ParentID == nil {
			parent = tk
		}
	}
	if parent == nil || parent.RecurrenceID == nil || parent.BlockedBy != 1 || parent.SubgigCount != 2 {
		t.Fatalf("occurrence: %+v", parent)
	}
	for _, tk := range gigs {
		if tk.ParentID != nil && (tk.RecurrenceID != nil || tk.NotAfter == nil || *tk.NotAfter != *parent.NotAfter || len(tk.Assignees) != 1) {
			t.Fatalf("subgig %s: %+v", tk.Title, tk)
		}
	}
	// the next day: the subgigs don't hold the next occurrence back (overlap counts occurrences only)
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		_, err := MoveGig(f.ctx, tx, f.a, parent, 0, Move{Category: "done"})
		return err
	}))
	res, err = f.s.Housekeeping(f.ctx, ny(t, "2026-11-13 09:00"))
	must(t, err)
	if res.Created != 1 {
		t.Fatalf("second day: %+v", res)
	}
}

// A trashed blocking subgig still counts for the cycle check (it can be restored), and an archived
// one stops holding its parent back (agents never get archived gigs).
func TestSubgigGateTrashAndArchive(t *testing.T) {
	f := newFixture(t)
	parent := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "ship"})
	pid := parent.ID
	gate := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "tests", ParentID: &pid, BlocksParent: true})
	f.tx(t, func(tx *sql.Tx) error {
		g, err := TrashGig(f.ctx, tx, f.a, gate, false)
		if err != nil {
			return err
		}
		if err := AddRelation(f.ctx, tx, f.a, parent, g, "blocks"); err == nil {
			t.Fatal("a cycle through a trashed blocking subgig was allowed")
		}
		_, err = TrashGig(f.ctx, tx, f.a, g, true)
		return err
	})
	if got, _ := GetGig(f.ctx, f.s.R, pid); got.BlockedBy != 1 {
		t.Fatalf("restored gate: blocked_by %d", got.BlockedBy)
	}
	f.tx(t, func(tx *sql.Tx) error {
		g, err := GetGig(f.ctx, tx, gate.ID)
		if err != nil {
			return err
		}
		_, err = ArchiveGig(f.ctx, tx, f.a, g, true)
		return err
	})
	if got, _ := GetGig(f.ctx, f.s.R, pid); got.BlockedBy != 0 {
		t.Fatalf("archived gate still blocks: %d", got.BlockedBy)
	}
}
