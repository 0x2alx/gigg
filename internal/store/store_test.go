package store

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestOpenMigratesAndReopens(t *testing.T) {
	skipOnPG(t)
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, dir, quiet)
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.SchemaVersion(ctx)
	if err != nil || v < 1 {
		t.Fatalf("schema version %d, %v", v, err)
	}
	var mode string
	if err := s.W.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal mode %q %v", mode, err)
	}
	var fk int
	if err := s.R.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign keys on readers: %d %v", fk, err)
	}
	if _, err := s.R.ExecContext(ctx, "CREATE TABLE x (a INTEGER)"); err == nil {
		t.Fatal("reader handle must be read-only")
	}
	s.Close()
	for _, d := range []string{"backups", "files"} {
		if fi, err := os.Stat(filepath.Join(dir, d)); err != nil || !fi.IsDir() {
			t.Fatalf("%s dir missing", d)
		}
	}
	// a fresh database takes no snapshot; reopening applies nothing new
	if m, _ := filepath.Glob(filepath.Join(dir, "backups", "*.db")); len(m) != 0 {
		t.Fatalf("unexpected snapshots %v", m)
	}
	s2, err := Open(ctx, dir, quiet)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if v2, _ := s2.SchemaVersion(ctx); v2 != v {
		t.Fatalf("version changed on reopen: %d -> %d", v, v2)
	}
}

func TestRefusesNewerSchema(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, dir, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.W.ExecContext(ctx, "INSERT INTO schema_migrations VALUES (999, 'future.sql', 0)"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := Open(ctx, dir, quiet); err == nil {
		t.Fatal("a database from a newer binary must not open")
	}
}

func TestMigrationsAreNumbered(t *testing.T) {
	ms, err := loadMigrations()
	if err != nil || len(ms) == 0 {
		t.Fatalf("%v %d", err, len(ms))
	}
	// every migration after the PostgreSQL baseline has its twin in migrations_pg
	if _, _, err := loadPGMigrations(len(ms)); err != nil {
		t.Fatal(err)
	}
}
