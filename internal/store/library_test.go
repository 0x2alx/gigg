package store

import (
	"context"
	"database/sql"
	"testing"
)

// TestLibraryMigration: 0019 gives library:write to the unlimited keys of admins that carry gigs:write
// or errands:write (what changing the shared library took before), and to no other key. The scope alone
// opens nothing: the API asks the scope of the kind next to it (gigs:write for gig templates and
// schedules, errands:write for errand templates), so no key gains what it couldn't do before (see
// TestLibraryScopePerKind in internal/api).
func TestLibraryMigration(t *testing.T) {
	skipOnPG(t)
	ctx := context.Background()
	dir := t.TempDir()
	migrateLimit = 18
	s, err := Open(ctx, dir, quiet)
	migrateLimit = 0
	if err != nil {
		t.Fatal(err)
	}
	var ada, bob int64
	must(t, s.Tx(ctx, func(tx *sql.Tx) error {
		a, err := CreateUser(ctx, tx, Actor{}, NewUser{Username: "ada", Kind: "human", IsAdmin: true})
		if err != nil {
			return err
		}
		b, err := CreateUser(ctx, tx, Actor{UserID: a.ID}, NewUser{Username: "bob", Kind: "agent"})
		if err != nil {
			return err
		}
		ada, bob = a.ID, b.ID
		return nil
	}))
	for _, k := range []struct {
		name, scopes string
		user         int64
		bound        int
	}{
		{"adm-gigs", `["gigs:read","gigs:write"]`, ada, 0},
		{"adm-errands", `["errands:write"]`, ada, 0},
		{"adm-read", `["gigs:read"]`, ada, 0},
		{"adm-bound", `["gigs:write"]`, ada, 1},
		{"bob", `["gigs:write","errands:write"]`, bob, 0},
	} {
		if _, err := s.W.ExecContext(ctx, `INSERT INTO api_keys (name, prefix, secret_hash, user_id, bound, scopes, expires_at, created_at)
			VALUES (?, ?, x'00', ?, ?, ?, 9000000000000, 1)`, k.name, "gk_"+k.name, k.user, k.bound, k.scopes); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	if s, err = Open(ctx, dir, quiet); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for name, want := range map[string]string{
		"adm-gigs":    `["gigs:read","gigs:write","library:write","attachments:read"]`,
		"adm-errands": `["errands:write","library:write"]`,
		"adm-read":    `["gigs:read","attachments:read"]`,
		"adm-bound":   `["gigs:write"]`,
		"bob":         `["gigs:write","errands:write"]`,
	} {
		var got string
		must(t, s.R.QueryRowContext(ctx, `SELECT scopes FROM api_keys WHERE name = ?`, name).Scan(&got))
		if got != want {
			t.Fatalf("%s: %s, want %s", name, got, want)
		}
	}
}

// TestLibraryGrants: shared_library counts only on the all-workspaces level, needs view there and in
// every workspace, is in no preset, and a workspace or project rule can't hold it.
func TestLibraryGrants(t *testing.T) {
	all := func(preset string, ov map[string]bool) *AccessRule {
		return &AccessRule{UserID: new(int64), Preset: preset, Overrides: ov}
	}
	ws := func(id int64, preset string, ov map[string]bool) *AccessRule {
		return &AccessRule{UserID: new(int64), WorkspaceID: &id, Preset: preset, Overrides: ov}
	}
	workspaces := []int64{1, 2}
	g := NewGrants(false, []*AccessRule{all("viewer", map[string]bool{"shared_library": true})}, workspaces, nil)
	if !g.Has(0, "shared_library") || g.Has(1, "shared_library") {
		t.Fatalf("all-workspaces holder: %v %v", g.Has(0, "shared_library"), g.Has(1, "shared_library"))
	}
	if d := g.Decide(1, "shared_library"); d.Source != "all_workspaces" {
		t.Fatalf("asked about one workspace: %+v", d)
	}
	// no preset gives it, manager included
	if g := NewGrants(false, []*AccessRule{all("manager", nil)}, workspaces, nil); g.Has(0, "shared_library") {
		t.Fatal("manager preset gives shared_library")
	}
	// view taken away in one workspace: not held
	g = NewGrants(false, []*AccessRule{all("viewer", map[string]bool{"shared_library": true}), ws(2, "", map[string]bool{"view": false})}, workspaces, nil)
	if d := g.Decide(0, "shared_library"); d.Allowed || d.Source != "needs_view" {
		t.Fatalf("one workspace unseen: %+v", d)
	}
	// no view on the all-workspaces level (only per workspace): not held
	g = NewGrants(false, []*AccessRule{all("", map[string]bool{"shared_library": true}), ws(1, "viewer", nil), ws(2, "viewer", nil)}, workspaces, nil)
	if g.Has(0, "shared_library") {
		t.Fatal("held without view on the all-workspaces level")
	}
	// admins hold it everywhere
	if g := NewGrants(true, nil, workspaces, nil); !g.Has(0, "shared_library") || !g.Has(1, "shared_library") {
		t.Fatal("admin")
	}
	// a group's all-workspaces rule gives it too
	gid := int64(7)
	g = NewGrants(false, []*AccessRule{{GroupID: &gid, GroupName: "lib", Preset: "viewer", Overrides: map[string]bool{"shared_library": true}}}, workspaces, nil)
	if !g.Has(0, "shared_library") {
		t.Fatal("group all-workspaces rule")
	}
}

// TestLibraryProjectViewDeny: a project rule that takes view away in one project takes the shared
// library away too: a holder must see everything their change moves, projects included.
func TestLibraryProjectViewDeny(t *testing.T) {
	workspaces := []int64{1, 2}
	projects := map[int64][]int64{1: {11, 12}, 2: {21}}
	all := &AccessRule{UserID: new(int64), Preset: "viewer", Overrides: map[string]bool{"shared_library": true}}
	hide := func(ws, p int64) *AccessRule {
		return &AccessRule{UserID: new(int64), WorkspaceID: &ws, ProjectID: &p, Overrides: map[string]bool{"view": false}}
	}
	if g := NewGrants(false, []*AccessRule{all}, workspaces, projects); !g.Has(0, "shared_library") {
		t.Fatal("no project hidden: not held")
	}
	g := NewGrants(false, []*AccessRule{all, hide(1, 12)}, workspaces, projects)
	if !g.Has(1, "view") {
		t.Fatal("the workspace is still viewable")
	}
	if d := g.Decide(0, "shared_library"); d.Allowed || d.Source != "needs_view" {
		t.Fatalf("a project hidden: %+v", d)
	}
}
