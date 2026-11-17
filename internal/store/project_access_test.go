package store

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"
)

// projFix: workspace "home" (owned by agent "own") with projects main and tools, agent "guest" with a
// consumer rule on home/tools only, user "mem" (member of the whole workspace, viewer on tools), and
// group "crew" with a consumer rule on home/main.
type projFix struct {
	s                     *Store
	ctx                   context.Context
	a                     Actor
	ws, main, tools       int64
	own, guest, mem, crew int64
	crewUser              int64
}

func newProjFix(t *testing.T) *projFix {
	t.Helper()
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir(), quiet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	f := &projFix{s: s, ctx: ctx}
	must(t, s.Tx(ctx, func(tx *sql.Tx) error {
		ada, err := CreateUser(ctx, tx, Actor{}, NewUser{Username: "ada", Kind: "human", IsAdmin: true})
		if err != nil {
			return err
		}
		f.a = Actor{UserID: ada.ID}
		users := map[string]*int64{"own": &f.own, "guest": &f.guest, "mem": &f.mem, "cu": &f.crewUser}
		for _, name := range []string{"own", "guest", "mem", "cu"} {
			u, err := CreateUser(ctx, tx, f.a, NewUser{Username: name, Kind: "agent"})
			if err != nil {
				return err
			}
			*users[name] = u.ID
		}
		w, err := CreateWorkspace(ctx, tx, f.a, "home", "Home", nil)
		if err != nil {
			return err
		}
		f.ws = w.ID
		if _, err := tx.ExecContext(ctx, `UPDATE workspaces SET owner_id = ? WHERE id = ?`, f.own, f.ws); err != nil {
			return err
		}
		for slug, id := range map[string]*int64{"main": &f.main, "tools": &f.tools} {
			p, err := CreateProject(ctx, tx, f.a, f.ws, slug, slug, map[string]string{"main": "MAIN", "tools": "TLS"}[slug], nil)
			if err != nil {
				return err
			}
			*id = p.ID
		}
		if err := SetMembership(ctx, tx, f.a, f.ws, f.own, "member"); err != nil {
			return err
		}
		if err := SetMembership(ctx, tx, f.a, f.ws, f.mem, "member"); err != nil {
			return err
		}
		if err := SetProjectMembership(ctx, tx, f.a, f.ws, f.tools, f.guest, "consumer"); err != nil {
			return err
		}
		g, err := CreateGroup(ctx, tx, f.a, "crew", "")
		if err != nil {
			return err
		}
		f.crew = g.ID
		if _, err := UpdateGroup(ctx, tx, f.a, g, 0, nil, nil, []int64{f.crewUser}); err != nil {
			return err
		}
		return SetRule(ctx, tx, f.a, "group", f.crew, &f.ws, &f.main, "consumer", nil)
	}))
	return f
}

func (f *projFix) grants(t *testing.T, user int64) *Grants {
	t.Helper()
	_, g, err := UserGrants(f.ctx, f.s.R, user)
	must(t, err)
	return g
}

// TestProjectRules: a project rule gives that project only; the most specific rule wins per
// permission (an off override on a project lowers a workspace member there); group project rules
// count; workspace-level permissions are never decided per project.
func TestProjectRules(t *testing.T) {
	f := newProjFix(t)
	g := f.grants(t, f.guest)
	if g.Has(f.ws, "view") || !g.Sees(f.ws) || !slices.Equal(g.Visible(), []int64{f.ws}) {
		t.Fatalf("guest sees the workspace only through its project: has=%v sees=%v visible=%v", g.Has(f.ws, "view"), g.Sees(f.ws), g.Visible())
	}
	if !g.HasIn(f.ws, f.tools, "work_gigs") || !g.HasIn(f.ws, f.tools, "comment") || g.HasIn(f.ws, f.tools, "edit_gigs") || g.HasIn(f.ws, f.main, "view") {
		t.Fatal("guest: consumer on tools, nothing on main")
	}
	if !slices.Equal(g.ProjectFilter(f.ws), []int64{f.tools}) || !slices.Equal(g.ProjectsWith(f.ws, "work_gigs"), []int64{f.tools}) {
		t.Fatalf("guest's filter %v", g.ProjectFilter(f.ws))
	}
	if g.HasIn(f.ws, f.tools, "errands") || g.Has(f.ws, "templates") {
		t.Fatal("a project rule gives nothing workspace-level")
	}
	if d := g.DecideIn(f.ws, f.tools, "work_gigs"); d.Source != "user_project" {
		t.Fatalf("source %+v", d)
	}

	// a workspace member with an off override on tools: lowered there only
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		return SetRule(f.ctx, tx, f.a, "user", f.mem, &f.ws, &f.tools, "viewer", map[string]bool{"edit_gigs": false, "work_gigs": false})
	}))
	m := f.grants(t, f.mem)
	if m.HasIn(f.ws, f.tools, "edit_gigs") || m.HasIn(f.ws, f.tools, "work_gigs") || !m.HasIn(f.ws, f.tools, "create_gigs") || !m.HasIn(f.ws, f.main, "edit_gigs") {
		t.Fatal("member: off on tools (a preset only adds; create_gigs still comes from the workspace), on elsewhere")
	}
	if m.ProjectFilter(f.ws) != nil {
		t.Fatal("member sees every project: no narrowing")
	}

	// group project rule
	c := f.grants(t, f.crewUser)
	if !c.HasIn(f.ws, f.main, "work_gigs") || c.HasIn(f.ws, f.tools, "view") || c.DecideIn(f.ws, f.main, "work_gigs").Source != "group_project" {
		t.Fatal("crew: consumer on main through the group")
	}

	// what a project rule can't hold
	for name, fn := range map[string]func(tx *sql.Tx) error{
		"manager preset": func(tx *sql.Tx) error {
			return SetRule(f.ctx, tx, f.a, "user", f.guest, &f.ws, &f.tools, "manager", nil)
		},
		"workspace permission": func(tx *sql.Tx) error {
			return SetRule(f.ctx, tx, f.a, "user", f.guest, &f.ws, &f.tools, "", map[string]bool{"errands": true})
		},
		"project of another ws": func(tx *sql.Tx) error {
			other := f.ws + 99
			return SetRule(f.ctx, tx, f.a, "user", f.guest, &other, &f.tools, "viewer", nil)
		},
		"project without its ws": func(tx *sql.Tx) error { return SetRule(f.ctx, tx, f.a, "user", f.guest, nil, &f.tools, "viewer", nil) },
		"manager via membership": func(tx *sql.Tx) error { return SetProjectMembership(f.ctx, tx, f.a, f.ws, f.tools, f.guest, "manager") },
	} {
		if err := f.s.Tx(f.ctx, fn); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: want a validation error, got %v", name, err)
		}
	}

	// members: guest listed with tools only
	ms, err := Members(f.ctx, f.s.R, f.ws)
	must(t, err)
	found := false
	for _, mb := range ms {
		if mb.UserID == f.guest {
			found = true
			if mb.Whole || len(mb.Projects) != 1 || mb.Projects[0].ProjectID != f.tools || mb.Projects[0].Role != "consumer" || mb.Projects[0].Preset != "consumer" {
				t.Fatalf("guest membership %+v", mb)
			}
		}
	}
	if !found {
		t.Fatal("guest isn't listed as a member")
	}
}

// TestProjectAssignees: assignees must see the gig's project; losing the project rule unassigns.
func TestProjectAssignees(t *testing.T) {
	f := newProjFix(t)
	var onTools *Gig
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		if _, err := CreateGig(f.ctx, tx, f.a, NewGig{ProjectID: f.main, Title: "x", Assignees: []int64{f.guest}}); err == nil {
			t.Error("guest assigned on main, which it can't see")
		}
		var err error
		onTools, err = CreateGig(f.ctx, tx, f.a, NewGig{ProjectID: f.tools, Title: "y", Assignees: []int64{f.guest}})
		return err
	}))
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		return SetProjectMembership(f.ctx, tx, f.a, f.ws, f.tools, f.guest, "")
	}))
	g, err := GetGig(f.ctx, f.s.R, onTools.ID)
	must(t, err)
	if len(g.Assignees) != 0 {
		t.Fatalf("guest still assigned after losing tools: %v", g.Assignees)
	}
}

// TestClaimAcross: one claim over several workspaces: priority first, then the claimer's own
// workspace; each workspace's pause applies to its own gigs only.
func TestClaimAcross(t *testing.T) {
	f := newProjFix(t)
	var gw int64 // guest's own workspace
	var mine, theirs *Gig
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		w, err := CreateWorkspace(f.ctx, tx, f.a, "guest", "Guest", nil)
		if err != nil {
			return err
		}
		gw = w.ID
		if _, err := tx.ExecContext(f.ctx, `UPDATE workspaces SET owner_id = ? WHERE id = ?`, f.guest, gw); err != nil {
			return err
		}
		if err := SetMembership(f.ctx, tx, f.a, gw, f.guest, "consumer"); err != nil {
			return err
		}
		p, err := CreateProject(f.ctx, tx, f.a, gw, "main", "Main", "GST", nil)
		if err != nil {
			return err
		}
		if mine, err = CreateGig(f.ctx, tx, f.a, NewGig{ProjectID: p.ID, Title: "own", Priority: 2, Assignees: []int64{f.guest}}); err != nil {
			return err
		}
		if theirs, err = CreateGig(f.ctx, tx, f.a, NewGig{ProjectID: f.tools, Title: "granted", Priority: 3, Assignees: []int64{f.guest}}); err != nil {
			return err
		}
		for _, g := range []*Gig{mine, theirs} {
			if _, err := MoveGig(f.ctx, tx, f.a, g, 0, Move{Category: "todo"}); err != nil {
				return err
			}
		}
		return nil
	}))
	places := []ClaimPlace{{WorkspaceID: gw}, {WorkspaceID: f.ws, Projects: []int64{f.tools}}}
	claim := func() *Gig {
		var got *Gig
		err := f.s.Tx(f.ctx, func(tx *sql.Tx) error {
			var err error
			got, err = ClaimGigIn(f.ctx, tx, Actor{UserID: f.guest}, places, Claim{DryRun: true})
			return err
		})
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		must(t, err)
		return got
	}
	if g := claim(); g == nil || g.ID != theirs.ID {
		t.Fatalf("priority 3 in the granted project comes first: %+v", g)
	}
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(f.ctx, `UPDATE gigs SET priority = 2 WHERE id = ?`, theirs.ID)
		return err
	}))
	if g := claim(); g == nil || g.ID != mine.ID {
		t.Fatalf("same priority: the claimer's own workspace first: %+v", g)
	}
	// pausing the guest's own workspace leaves the granted project's gig
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(f.ctx, `INSERT INTO runner_settings (workspace_id, paused_open) VALUES (?, 1)
			ON CONFLICT (workspace_id) DO UPDATE SET paused_open = 1`, gw)
		return err
	}))
	if g := claim(); g == nil || g.ID != theirs.ID {
		t.Fatalf("own workspace paused: the granted gig: %+v", g)
	}
	// a gig in tools assigned to someone else is never handed out
	must(t, f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(f.ctx, `DELETE FROM gig_assignees WHERE gig_id = ?`, theirs.ID)
		return err
	}))
	if g := claim(); g != nil {
		t.Fatalf("unassigned gig claimed: %+v", g)
	}
}

// TestKeyReachMigration: 0017 turns a workspace key into a key bound to that workspace, a
// project-narrowed one into a key bound to those projects, and keeps unbound keys unbound.
func TestKeyReachMigration(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	migrateLimit = 16
	s, err := Open(ctx, dir, quiet)
	migrateLimit = 0
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO users (id, username, kind, display_name, is_admin, created_at) VALUES (1, 'ada', 'human', 'ada', 1, 1)`,
		`INSERT INTO workspaces (id, slug, name, status_template, created_at) VALUES (1, 'home', 'home', '[]', 1)`,
		`INSERT INTO projects (id, workspace_id, slug, name, key, created_at) VALUES (5, 1, 'main', 'Main', 'MAIN', 1), (6, 1, 'tools', 'Tools', 'TLS', 1)`,
		`INSERT INTO api_keys (id, name, prefix, secret_hash, user_id, workspace_id, scopes, expires_at, created_at) VALUES
			(1, 'ws', 'gk_a', x'00', 1, 1, '["gigs:read"]', 9000000000000, 1),
			(2, 'narrow', 'gk_b', x'00', 1, 1, '["gigs:read"]', 9000000000000, 1),
			(3, 'admin', 'gk_c', x'00', 1, NULL, '["admin:keys"]', 9000000000000, 1)`,
		`INSERT INTO api_key_projects (key_id, project_id) VALUES (2, 6), (2, 5)`,
	} {
		if _, err := s.W.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	s.Close()
	if s, err = Open(ctx, dir, quiet); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p5, p6 := int64(5), int64(6)
	for id, want := range map[int64][]KeyPlace{1: {{WorkspaceID: 1}}, 2: {{WorkspaceID: 1, ProjectID: &p5}, {WorkspaceID: 1, ProjectID: &p6}}, 3: {}} {
		k, err := GetKey(ctx, s.R, id)
		must(t, err)
		if k.Bound != (id != 3) || len(k.Reach) != len(want) {
			t.Fatalf("key %d: bound=%v reach=%+v", id, k.Bound, k.Reach)
		}
		for i := range want {
			if k.Reach[i].WorkspaceID != want[i].WorkspaceID || (want[i].ProjectID == nil) != (k.Reach[i].ProjectID == nil) ||
				(want[i].ProjectID != nil && *k.Reach[i].ProjectID != *want[i].ProjectID) {
				t.Fatalf("key %d place %d: %+v", id, i, k.Reach[i])
			}
		}
	}
	if k, _ := GetKey(ctx, s.R, 2); k.WholeWorkspace(1) || !k.ReachesProject(1, 5) || k.ReachesProject(1, 7) || *k.Current() != 1 {
		t.Fatal("narrowed key's reach")
	}
	// a bound key whose places all go reaches nothing (never everything)
	if _, err := s.W.ExecContext(ctx, `DELETE FROM api_key_reach WHERE key_id = 2`); err != nil {
		t.Fatal(err)
	}
	if k, _ := GetKey(ctx, s.R, 2); !k.Bound || k.ReachesWorkspace(1) {
		t.Fatal("a bound key with no places left must reach nothing")
	}
}
