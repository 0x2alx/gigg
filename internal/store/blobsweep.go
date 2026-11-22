package store

import (
	"context"
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// The blob sweep. blobs.refcount is kept roughly by the code that adds and removes users of a file,
// but cascades (a project or workspace deleted) never pass through that code, so the sweep recounts
// from what still points at each blob. A blob nobody uses gets orphaned_at; once that is older than
// grace (the snapshot window), its file and row go. Files on disk with no row at all (a crash between
// writing the file and committing the row) go once their modification time is older than grace.

var blobName = regexp.MustCompile(`^[0-9a-f]{64}$`)

// blobRefs is every column that points at a blob.
const blobRefs = `SELECT avatar_sha256 AS sha FROM users WHERE avatar_sha256 IS NOT NULL
	UNION ALL SELECT blob_sha256 FROM attachments WHERE blob_sha256 IS NOT NULL
	UNION ALL SELECT thumb_sha256 FROM attachments WHERE thumb_sha256 IS NOT NULL`

// SweepBlobs recounts blob users, marks unused blobs and removes those unused for longer than grace.
// It returns how many files it removed.
func (s *Store) SweepBlobs(ctx context.Context, grace time.Duration) (int, error) {
	now := Now()
	cutoff := now - grace.Milliseconds()
	var gone []string
	removed := 0
	err := s.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE blobs SET refcount = (SELECT count(*) FROM (`+blobRefs+`) r WHERE r.sha = blobs.sha256)`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE blobs SET orphaned_at = CASE WHEN refcount > 0 THEN NULL ELSE coalesce(orphaned_at, ?) END`, now); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT sha256 FROM blobs WHERE refcount = 0 AND orphaned_at <= ?`, cutoff)
		if err != nil {
			return err
		}
		for rows.Next() {
			var sha string
			if err := rows.Scan(&sha); err != nil {
				rows.Close()
				return err
			}
			gone = append(gone, sha)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		// removed inside the transaction: uploads write their file inside theirs, so the two can't
		// interleave (one writer), and a file reused by an upload is never removed under it
		for _, sha := range gone {
			if _, err := tx.ExecContext(ctx, `DELETE FROM blobs WHERE sha256 = ?`, sha); err != nil {
				return err
			}
			if blobName.MatchString(sha) && os.Remove(s.BlobPath(sha)) == nil {
				removed++
			}
		}
		return nil
	})
	if err != nil {
		return removed, err
	}
	// files no row knows about, older than grace
	known := map[string]bool{}
	rows, err := s.R.QueryContext(ctx, `SELECT sha256 FROM blobs`)
	if err != nil {
		return removed, err
	}
	for rows.Next() {
		var sha string
		if err := rows.Scan(&sha); err == nil {
			known[sha] = true
		}
	}
	rows.Close()
	root := filepath.Join(s.dir, "files")
	old := time.UnixMilli(cutoff)
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !blobName.MatchString(name) && filepath.Ext(name) != ".tmp" {
			return nil // not ours
		}
		if known[name] {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().After(old) {
			return nil
		}
		if blobName.MatchString(name) && s.isReferenced(ctx, name) {
			return nil // committed after the snapshot of known above
		}
		if os.Remove(p) == nil {
			removed++
		}
		return nil
	})
	return removed, err
}

func (s *Store) isReferenced(ctx context.Context, sha string) bool {
	var n int
	err := s.R.QueryRowContext(ctx, `SELECT count(*) FROM blobs WHERE sha256 = ?`, sha).Scan(&n)
	return err != nil || n > 0
}
