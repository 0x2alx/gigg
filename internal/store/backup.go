package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Nightly snapshots: a consistent copy of the database (VACUUM INTO, like the pre-migration
// snapshots) in <data>/backups/nightly-YYYY-MM-DD.db, written under a temporary name and renamed
// when complete, so a file-level backup of the folder never picks up a half-written copy. The
// newest `keep` are kept. The live gigg.db (and its -wal/-shm) is not safe to copy at file level.

// NightlyName is the snapshot file name for a local date.
func NightlyName(day time.Time) string { return "nightly-" + day.Format("2006-01-02") + ".db" }

// NightlyBackup writes today's snapshot unless it exists, then prunes old ones. It returns the
// path written ("" when today's already existed).
func (s *Store) NightlyBackup(ctx context.Context, day time.Time, keep int) (string, error) {
	if s.pg {
		return "", nil // PostgreSQL is backed up by its operator (pg_dump), not by gigg
	}
	dir := filepath.Join(s.dir, "backups")
	final := filepath.Join(dir, NightlyName(day))
	if _, err := os.Stat(final); err == nil {
		return "", nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	tmp := filepath.Join(dir, "."+NightlyName(day)+".tmp")
	_ = os.Remove(tmp) // a leftover from a crash
	if _, err := s.W.ExecContext(ctx, "VACUUM INTO ?", tmp); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("nightly snapshot: %w", err)
	}
	if f, err := os.Open(tmp); err == nil { // on disk before the rename makes it visible
		_ = f.Sync()
		f.Close()
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return final, s.pruneNightly(keep)
}

func (s *Store) pruneNightly(keep int) error {
	if keep < 1 {
		keep = 1
	}
	dir := filepath.Join(s.dir, "backups")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var names []string
	for _, e := range ents {
		if n := e.Name(); strings.HasPrefix(n, "nightly-") && strings.HasSuffix(n, ".db") && e.Type().IsRegular() {
			names = append(names, n)
		}
	}
	sort.Strings(names) // dates sort as text
	for len(names) > keep {
		if err := os.Remove(filepath.Join(dir, names[0])); err != nil {
			return err
		}
		names = names[1:]
	}
	return nil
}
