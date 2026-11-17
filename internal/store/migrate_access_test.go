package store

import (
	"context"
	"strings"
	"testing"
)

// TestAccessMigrationRefusesOldRoles: a schema-5 database with only an admin goes through 0006 (and
// the old tables are gone); one that holds roles is refused, not half-converted.
func TestAccessMigrationRefusesOldRoles(t *testing.T) {
	ctx := context.Background()
	for _, withRoles := range []bool{false, true} {
		dir := t.TempDir()
		migrateLimit = 5
		s, err := Open(ctx, dir, quiet)
		migrateLimit = 0
		if err != nil {
			t.Fatal(err)
		}
		now := Now()
		for _, q := range []string{
			`INSERT INTO users (id, username, kind, display_name, is_admin, created_at) VALUES (1, 'ada', 'human', 'ada', 1, ?1), (2, 'maria', 'human', 'maria', 0, ?1)`,
			`INSERT INTO workspaces (id, slug, name, status_template, created_at) VALUES (1, 'home', 'home', '[]', ?1)`,
		} {
			if _, err := s.W.ExecContext(ctx, q, now); err != nil {
				t.Fatal(err)
			}
		}
		if withRoles {
			if _, err := s.W.ExecContext(ctx, `INSERT INTO memberships (workspace_id, user_id, role, created_at) VALUES (1, 2, 'member', ?)`, now); err != nil {
				t.Fatal(err)
			}
		}
		s.Close()

		s, err = Open(ctx, dir, quiet)
		if withRoles {
			if err == nil || !strings.Contains(err.Error(), "isn't supported") {
				t.Fatalf("roles: want a refusal, got %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		var n int
		_ = s.R.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE name IN ('memberships', 'user_permissions', 'group_permissions')`).Scan(&n)
		s.Close()
		if n != 0 {
			t.Fatalf("%d old tables left", n)
		}
	}
}
