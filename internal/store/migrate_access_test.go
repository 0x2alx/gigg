package store

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

// TestAccessMigrationKeepsAbilities builds a schema-5 database with people, agents, roles, groups
// and switches, works out what everyone could do under the old rules, migrates to 0006 and checks
// that every user can do exactly the same in every workspace, that the old tables are gone, and
// that new workspaces start the way the old switches said.
func TestAccessMigrationKeepsAbilities(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	migrateLimit = 5
	s, err := Open(ctx, dir, quiet)
	migrateLimit = 0
	if err != nil {
		t.Fatal(err)
	}
	ex := func(q string, args ...any) {
		t.Helper()
		if _, err := s.W.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	now := Now()
	users := []struct {
		name, kind string
		admin      bool
	}{{"ada", "human", true}, {"maria", "human", false}, {"bob", "human", false}, {"scout", "agent", false}, {"helper", "agent", false}, {"atlas", "agent", false}}
	for i, u := range users {
		ex(`INSERT INTO users (id, username, kind, display_name, is_admin, created_at) VALUES (?,?,?,?,?,?)`, i+1, u.name, u.kind, u.name, u.admin, now)
	}
	for i, w := range []string{"home", "scout", "helper"} {
		ex(`INSERT INTO workspaces (id, slug, name, status_template, created_at) VALUES (?,?,?,'[]',?)`, i+1, w, w, now)
	}
	// roles: maria member of home, viewer of scout; bob commenter of home; scout manager of scout,
	// member of home; helper member of scout and helper; atlas nothing
	for _, m := range []struct {
		ws, user int
		role     string
	}{{1, 2, "member"}, {2, 2, "viewer"}, {1, 3, "commenter"}, {2, 4, "manager"}, {1, 4, "member"}, {2, 5, "member"}, {3, 5, "member"}} {
		ex(`INSERT INTO memberships (workspace_id, user_id, role, created_at) VALUES (?,?,?,?)`, m.ws, m.user, m.role, now)
	}
	// groups: "agents" (scout, helper) allows templates, denies archive_tasks; "trusted" (helper) allows archive_tasks
	ex(`INSERT INTO groups (id, name, created_at) VALUES (1, 'agents', ?), (2, 'trusted', ?)`, now, now)
	ex(`INSERT INTO group_members VALUES (1, 4), (1, 5), (2, 5)`)
	ex(`INSERT INTO group_permissions VALUES (1, 'templates', 1), (1, 'archive_tasks', 0), (2, 'archive_tasks', 1)`)
	// own switches: scout schedules on, maria custom_fields on (useless as a member), bob relations off
	ex(`INSERT INTO user_permissions VALUES (4, 'schedules', 1), (2, 'custom_fields', 1), (3, 'relations', 0)`)

	type key struct {
		user int64
		ws   int64
	}
	before := map[key]map[string]bool{}
	tx, err := s.W.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for uid := int64(2); uid <= 6; uid++ {
		var kind string
		_ = tx.QueryRowContext(ctx, `SELECT kind FROM users WHERE id = ?`, uid).Scan(&kind)
		caps := oldCapsFor(t, tx, uid, kind == "agent")
		for ws := int64(1); ws <= 3; ws++ {
			var role string
			_ = tx.QueryRowContext(ctx, `SELECT role FROM memberships WHERE user_id = ? AND workspace_id = ?`, uid, ws).Scan(&role)
			before[key{uid, ws}] = oldAbilities(role, caps)
		}
	}
	tx.Rollback()
	s.Close()

	s, err = Open(ctx, dir, quiet) // applies 0006
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for k, old := range before {
		_, g, err := UserGrants(ctx, s.R, k.user)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range Permissions {
			if g.Has(k.ws, p.Name) != old[p.Name] {
				t.Errorf("user %d, workspace %d, %s: %v after, %v before", k.user, k.ws, p.Name, g.Has(k.ws, p.Name), old[p.Name])
			}
		}
	}
	for _, tbl := range []string{"memberships", "user_permissions", "group_permissions"} {
		var n int
		_ = s.R.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE name = ?`, tbl).Scan(&n)
		if n != 0 {
			t.Errorf("%s still exists", tbl)
		}
	}
	// a new workspace, with scout added as a member: his own old switch carries on (schedules),
	// templates comes from the agents group (not in the preset); the member preset is his own rule,
	// so it beats the group's archive deny (user rules beat group rules)
	must(t, s.Tx(ctx, func(tx *sql.Tx) error {
		ex2 := func(q string, args ...any) error { _, err := tx.ExecContext(ctx, q, args...); return err }
		if err := ex2(`INSERT INTO workspaces (id, slug, name, status_template, created_at) VALUES (4, 'new', 'new', '[]', ?)`, now); err != nil {
			return err
		}
		return SetMembership(ctx, tx, Actor{UserID: 1}, 4, 4, "member")
	}))
	_, g, _ := UserGrants(ctx, s.R, 4)
	got := fmt.Sprint(g.Has(4, "schedules"), g.Has(4, "templates"), g.Has(4, "archive_tasks"), g.Has(4, "create_tasks"), g.Has(4, "manage_workspace"))
	if got != "true true true true false" {
		t.Errorf("new workspace for scout: schedules templates archive create manage = %s", got)
	}
}

// oldCapsFor mirrors migrateAccess's resolution (own setting > any group allowing > any group
// denying > kind default), written out separately so a slip in one shows up against the other.
func oldCapsFor(t *testing.T, q Q, uid int64, agent bool) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, c := range oldCaps {
		var own sql.NullBool
		_ = q.QueryRowContext(context.Background(), `SELECT allowed FROM user_permissions WHERE user_id = ? AND capability = ?`, uid, c.name).Scan(&own)
		var allows, denies int
		_ = q.QueryRowContext(context.Background(), `SELECT count(*) FILTER (WHERE p.allowed = 1), count(*) FILTER (WHERE p.allowed = 0)
			FROM group_permissions p JOIN group_members m ON m.group_id = p.group_id WHERE m.user_id = ? AND p.capability = ?`, uid, c.name).Scan(&allows, &denies)
		switch {
		case own.Valid:
			out[c.name] = own.Bool
		case allows > 0:
			out[c.name] = true
		case denies > 0:
			out[c.name] = false
		case agent:
			out[c.name] = c.agent
		default:
			out[c.name] = c.human
		}
	}
	return out
}
