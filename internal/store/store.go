// Package store owns the database: opening it, applying migrations, and every query. SQLite by
// default (one writer connection and a small reader pool, WAL mode, foreign keys on); PostgreSQL
// when a postgres:// URL is given (pg.go adapts the SQL; the same single-writer pool).
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"hash/fnv"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver: keeps the build CGO-free and static
)

//go:embed migrations/*.sql migrations_pg/*.sql
var migrationFiles embed.FS

// Store holds the database handles.
type Store struct {
	W   *sql.DB // the only writer
	R   *sql.DB // readers
	dir string
	pg  bool // PostgreSQL (else SQLite)
}

// Postgres reports whether the store runs on PostgreSQL.
func (s *Store) Postgres() bool { return s.pg }

func dsn(path string, readonly bool) string {
	q := "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)&_pragma=temp_store(MEMORY)&_txlock=immediate"
	if readonly {
		q += "&mode=ro"
	}
	return "file:" + path + "?" + q
}

// Open opens (and creates) the SQLite database in dataDir and applies pending migrations. Tests run
// it on PostgreSQL instead when GIGG_TEST_DATABASE_URL is set: one schema per data directory, so
// reopening a directory finds its data again.
func Open(ctx context.Context, dataDir string, log *slog.Logger) (*Store, error) {
	if u := os.Getenv("GIGG_TEST_DATABASE_URL"); u != "" {
		return openTestPG(ctx, dataDir, u, log)
	}
	return OpenDB(ctx, dataDir, "", log)
}

// OpenDB opens the database: PostgreSQL for a postgres:// URL, else SQLite in dataDir (which holds
// the files and backups either way). Pending migrations are applied.
func OpenDB(ctx context.Context, dataDir, databaseURL string, log *slog.Logger) (*Store, error) {
	for _, d := range []string{dataDir, filepath.Join(dataDir, "backups"), filepath.Join(dataDir, "files")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, fmt.Errorf("data directory: %w", err)
		}
	}
	if databaseURL != "" {
		if !isPostgresURL(databaseURL) {
			return nil, errors.New("database URL: only postgres:// (or postgresql://) is supported; leave it empty for SQLite")
		}
		return openPG(ctx, dataDir, databaseURL, log)
	}
	path := filepath.Join(dataDir, "gigg.db")
	w, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	if err := w.PingContext(ctx); err != nil {
		w.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	s := &Store{W: w, dir: dataDir}
	if err := s.migrate(ctx, log); err != nil {
		w.Close()
		return nil, err
	}
	r, err := sql.Open("sqlite", dsn(path, true))
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(4)
	s.R = r
	return s, nil
}

// openPG opens a PostgreSQL database: one writer connection (the store relies on a single writer,
// as with SQLite) and a small reader pool.
func openPG(ctx context.Context, dataDir, url string, log *slog.Logger) (*Store, error) {
	w, err := sql.Open(pgDriverName, url)
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	if err := w.PingContext(ctx); err != nil {
		w.Close()
		return nil, fmt.Errorf("open postgres: %w", pgRedact(err, url))
	}
	s := &Store{W: w, dir: dataDir, pg: true}
	if err := s.migrate(ctx, log); err != nil {
		w.Close()
		return nil, err
	}
	if err := pgLoadIdentity(ctx, w); err != nil {
		w.Close()
		return nil, err
	}
	r, err := sql.Open(pgDriverName, url)
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(4)
	s.R = r
	return s, nil
}

// pgRedact keeps the URL (and its password) out of an error message.
func pgRedact(err error, url string) error {
	if err == nil || url == "" {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), url, "<database url>"))
}

// openTestPG: a schema of its own per data directory (named after it), created on first use.
func openTestPG(ctx context.Context, dataDir, url string, log *slog.Logger) (*Store, error) {
	h := fnv.New64a()
	_, _ = h.Write([]byte(dataDir))
	schema := fmt.Sprintf("t%x", h.Sum64())
	admin, err := sql.Open(pgDriverName, url)
	if err != nil {
		return nil, err
	}
	// citext once, in public (every test schema finds it there), then the test's own schema
	for _, q := range []string{"CREATE EXTENSION IF NOT EXISTS citext SCHEMA public", "CREATE SCHEMA IF NOT EXISTS " + schema} {
		if _, err = admin.ExecContext(ctx, q); err != nil && !strings.Contains(err.Error(), "already exists") && !strings.Contains(err.Error(), "duplicate key") {
			break
		}
		err = nil
	}
	admin.Close()
	if err != nil {
		return nil, err
	}
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	return OpenDB(ctx, dataDir, url+sep+"search_path="+schema+",public", log)
}

// Close closes both handles.
func (s *Store) Close() error { return errors.Join(s.R.Close(), s.W.Close()) }

// Ping checks that the database answers a query.
func (s *Store) Ping(ctx context.Context) error {
	var one int
	return s.R.QueryRowContext(ctx, "SELECT 1").Scan(&one)
}

type migration struct {
	version int
	name    string
	sql     string
}

func loadMigrations() ([]migration, error) {
	var ms []migration
	err := fs.WalkDir(migrationFiles, "migrations", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		base := filepath.Base(p) // NNNN_name.sql
		num, _, ok := strings.Cut(base, "_")
		v, convErr := strconv.Atoi(num)
		if !ok || convErr != nil || v <= 0 {
			return fmt.Errorf("migration %s: name must be NNNN_name.sql", base)
		}
		b, err := migrationFiles.ReadFile(p)
		if err != nil {
			return err
		}
		ms = append(ms, migration{version: v, name: base, sql: string(b)})
		return nil
	})
	sort.Slice(ms, func(i, j int) bool { return ms[i].version < ms[j].version })
	if migrateLimit > 0 && migrateLimit < len(ms) {
		ms = ms[:migrateLimit]
	}
	for i := range ms {
		if ms[i].version != i+1 {
			return nil, fmt.Errorf("migrations must be numbered 1..n without gaps (found %s)", ms[i].name)
		}
	}
	return ms, err
}

// migrateLimit stops the migrations at a version (tests of a later migration build the old schema).
var migrateLimit int

// migrate applies pending migrations in order, each in its own transaction. Before the first
// pending migration on a database that already has data it writes a snapshot
// (backups/pre-<version>.db), once: a restart never overwrites it. Any failure rolls back and is
// returned, so the server refuses to start on a half-migrated schema.
func (s *Store) migrate(ctx context.Context, log *slog.Logger) error {
	ms, err := loadMigrations()
	if err != nil {
		return err
	}
	if s.pg {
		return s.migratePG(ctx, log, len(ms))
	}
	if _, err := s.W.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at INTEGER NOT NULL) STRICT`); err != nil {
		return err
	}
	var current int
	if err := s.W.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&current); err != nil {
		return err
	}
	if current > len(ms) {
		return fmt.Errorf("database schema %d is newer than this binary (%d): refusing to start", current, len(ms))
	}
	fresh := current == 0 // nothing to protect: no snapshot
	for _, m := range ms[current:] {
		if !fresh {
			snap := filepath.Join(s.dir, "backups", fmt.Sprintf("pre-%04d.db", m.version))
			if _, err := os.Stat(snap); errors.Is(err, os.ErrNotExist) {
				// written under a temporary name and renamed: a crash mid-way never leaves a partial
				// file under the final name (which a restart would take as done)
				tmp := snap + ".tmp"
				_ = os.Remove(tmp)
				if _, err := s.W.ExecContext(ctx, "VACUUM INTO ?", tmp); err != nil {
					return fmt.Errorf("snapshot before migration %s: %w", m.name, err)
				}
				if err := os.Rename(tmp, snap); err != nil {
					return fmt.Errorf("snapshot before migration %s: %w", m.name, err)
				}
				log.Info("snapshot before migration", "file", snap)
			}
		}
		if err := s.apply(ctx, m); err != nil {
			return fmt.Errorf("migration %s: %w", m.name, err)
		}
		log.Info("migration applied", "version", m.version, "name", m.name)
		current = m.version
	}
	return nil
}

// noFKHeader marks a migration that rebuilds a table (SQLite's 12-step ALTER procedure): it runs
// with foreign keys off, so dropping the old table doesn't cascade into the tables that reference
// it, and must leave no dangling reference (foreign_key_check) before it commits.
const noFKHeader = "-- gigg:foreign-keys-off"

func (s *Store) apply(ctx context.Context, m migration) error {
	conn, err := s.W.Conn(ctx) // the writer's single connection: the pragma must hit the same one
	if err != nil {
		return err
	}
	defer conn.Close()
	if strings.HasPrefix(m.sql, noFKHeader) && !s.pg {
		if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
			return err
		}
		defer conn.ExecContext(context.WithoutCancel(ctx), "PRAGMA foreign_keys = ON") //nolint:errcheck // the store test checks it is back on
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return err
	}
	if step := postSteps[m.version]; step != nil && !strings.HasSuffix(m.name, "_baseline.sql") { // a baseline has no old data to fix
		if err := step(ctx, tx); err != nil {
			return err
		}
	}
	if strings.HasPrefix(m.sql, noFKHeader) && !s.pg {
		rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
		if err != nil {
			return err
		}
		bad := rows.Next()
		rows.Close()
		if bad {
			return errors.New("the rebuild left dangling foreign keys")
		}
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)",
		m.version, m.name, time.Now().UnixMilli()); err != nil {
		return err
	}
	return tx.Commit()
}

// loadPGMigrations: the PostgreSQL baseline (NNNN_baseline.sql: the SQLite schema at NNNN) and a
// twin for every SQLite migration after it, up to n.
func loadPGMigrations(n int) (base migration, twins []migration, err error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations_pg")
	if err != nil {
		return base, nil, err
	}
	byVersion := map[int]migration{}
	for _, e := range entries {
		num, rest, ok := strings.Cut(e.Name(), "_")
		v, convErr := strconv.Atoi(num)
		if !ok || convErr != nil || v <= 0 {
			return base, nil, fmt.Errorf("migrations_pg/%s: name must be NNNN_name.sql", e.Name())
		}
		b, err := migrationFiles.ReadFile("migrations_pg/" + e.Name())
		if err != nil {
			return base, nil, err
		}
		m := migration{version: v, name: e.Name(), sql: string(b)}
		if rest == "baseline.sql" {
			if base.version != 0 {
				return base, nil, errors.New("migrations_pg: more than one baseline")
			}
			base = m
			continue
		}
		byVersion[v] = m
	}
	if base.version == 0 || base.version > n {
		return base, nil, fmt.Errorf("migrations_pg: no usable baseline (found %d, SQLite has %d)", base.version, n)
	}
	for v := base.version + 1; v <= n; v++ {
		m, ok := byVersion[v]
		if !ok {
			return base, nil, fmt.Errorf("migrations_pg: migration %04d has no PostgreSQL twin", v)
		}
		twins = append(twins, m)
	}
	return base, twins, nil
}

// migratePG: a fresh database gets the baseline (recorded as its number), then the twins of later
// migrations. n is the SQLite migration count, which a database may not be ahead of.
func (s *Store) migratePG(ctx context.Context, log *slog.Logger, n int) error {
	base, twins, err := loadPGMigrations(n)
	if err != nil {
		return err
	}
	if _, err := s.W.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version BIGINT PRIMARY KEY, name TEXT NOT NULL, applied_at BIGINT NOT NULL)`); err != nil {
		return err
	}
	var current int
	if err := s.W.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&current); err != nil {
		return err
	}
	if current > n {
		return fmt.Errorf("database schema %d is newer than this binary (%d): refusing to start", current, n)
	}
	todo := twins
	if current == 0 {
		todo = append([]migration{base}, twins...)
	} else if current < base.version {
		return fmt.Errorf("database schema %d is older than the PostgreSQL baseline (%d)", current, base.version)
	}
	for _, m := range todo {
		if m.version <= current {
			continue
		}
		if err := s.apply(ctx, m); err != nil {
			return fmt.Errorf("migration %s: %w", m.name, err)
		}
		log.Info("migration applied", "version", m.version, "name", m.name)
	}
	return nil
}

// SchemaVersion returns the highest applied migration.
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var v int
	err := s.R.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&v)
	return v, err
}
