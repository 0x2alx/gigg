package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// TestNightlyBackup: one consistent snapshot per local day, never a temporary file left behind,
// the newest `keep` kept, and the copy opens with the data in it.
func TestNightlyBackup(t *testing.T) {
	skipOnPG(t) // VACUUM INTO; PostgreSQL is backed up by its operator
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, dir, quiet)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	must(t, s.Tx(ctx, func(tx *sql.Tx) error {
		_, err := CreateUser(ctx, tx, Actor{}, NewUser{Username: "ada", Kind: "human", IsAdmin: true})
		return err
	}))
	day := time.Date(2026, 11, 13, 3, 30, 0, 0, time.UTC)
	f, err := s.NightlyBackup(ctx, day, 3)
	if err != nil || filepath.Base(f) != "nightly-2026-11-13.db" {
		t.Fatalf("first: %q %v", f, err)
	}
	if f, err := s.NightlyBackup(ctx, day.Add(time.Hour), 3); err != nil || f != "" {
		t.Fatalf("same day again: %q %v", f, err)
	}
	for i := 1; i <= 4; i++ {
		if _, err := s.NightlyBackup(ctx, day.AddDate(0, 0, i), 3); err != nil {
			t.Fatal(err)
		}
	}
	ents, _ := os.ReadDir(filepath.Join(dir, "backups"))
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	want := []string{"nightly-2026-11-15.db", "nightly-2026-11-16.db", "nightly-2026-11-17.db"}
	if len(names) != 3 || names[0] != want[0] || names[2] != want[2] {
		t.Fatalf("kept %v, want %v", names, want)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "backups", want[2])+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM users WHERE username = 'ada'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("snapshot content: %d %v", n, err)
	}
}
