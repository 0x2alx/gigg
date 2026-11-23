package store

import (
	"os"
	"testing"
)

// skipOnPG: the test is about SQLite itself (migrating old SQLite schemas, VACUUM, PRAGMAs);
// PostgreSQL starts from its baseline.
func skipOnPG(t *testing.T) {
	t.Helper()
	if os.Getenv("GIGG_TEST_DATABASE_URL") != "" {
		t.Skip("SQLite only")
	}
}

func TestPGRewrite(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`SELECT a FROM t WHERE b = ? AND c IS ? AND d IS NOT ?`,
			`SELECT a FROM t WHERE b = $1 AND c IS NOT DISTINCT FROM $2 AND d IS DISTINCT FROM $3`},
		{`SELECT '?' || x FROM t WHERE y = ? AND z LIKE ? ESCAPE '\'`,
			`SELECT '?' || x FROM t WHERE y = $1 AND z ILIKE $2 ESCAPE '\'`},
		{`INSERT INTO t VALUES (?1, ?2, ?1)`,
			`INSERT INTO t VALUES ($1, $2, $1)`},
		{`SELECT json_extract(a.data, '$.gig_id') FROM activity a`,
			`SELECT (a.data::jsonb ->> 'gig_id') FROM activity a`},
		{`SELECT group_concat(r.id), count(*) FROM r WHERE s = 'it''s'`,
			`SELECT string_agg(CAST(r.id AS TEXT), ','), count(*) FROM r WHERE s = 'it''s'`},
		{`SELECT group_concat(id || COALESCE(CAST(x AS TEXT), ''), '|') FROM t`,
			`SELECT string_agg(CAST(id || COALESCE(CAST(x AS TEXT), '') AS TEXT), '|') FROM t`},
	} {
		if got := pgRewrite(c.in); got != c.want {
			t.Errorf("pgRewrite(%q)\n got  %q\n want %q", c.in, got, c.want)
		}
	}
	if tb, ok := pgInsertInto("  INSERT INTO \"gigs\" (a) VALUES (?)"); !ok || tb != "gigs" {
		t.Errorf("insert table: %q %v", tb, ok)
	}
	if _, ok := pgInsertInto("INSERT INTO gigs (a) VALUES (?) RETURNING id"); ok {
		t.Error("RETURNING insert rewritten")
	}
}
