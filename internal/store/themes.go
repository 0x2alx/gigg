package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// UserTheme is a custom theme saved on a user (the theme JSON, sanitised).
type UserTheme struct {
	ID        string          `json:"id"`
	Theme     json.RawMessage `json:"theme"`
	UpdatedAt int64           `json:"updated_at"`
}

// ThemeState is what the theme screen needs: custom themes and the selection.
type ThemeState struct {
	Custom      []UserTheme `json:"custom"`
	SelectedID  *string     `json:"selected_id"`
	MatchSystem bool        `json:"match_system"`
}

// MaxUserThemes per user; the oldest go first.
const MaxUserThemes = 50

// Themes returns a user's custom themes (newest first) and selection.
func Themes(ctx context.Context, q Q, user int64) (*ThemeState, error) {
	st := &ThemeState{Custom: []UserTheme{}}
	var sel sql.NullString
	if err := q.QueryRowContext(ctx, `SELECT theme_selected_id, theme_match_system FROM users WHERE id = ?`, user).Scan(&sel, &st.MatchSystem); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	st.SelectedID = ptrStr(sel)
	rows, err := q.QueryContext(ctx, `SELECT theme_id, theme, updated_at FROM user_themes WHERE user_id = ? ORDER BY updated_at DESC, id DESC`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var t UserTheme
		var s string
		if err := rows.Scan(&t.ID, &s, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.Theme = json.RawMessage(s)
		st.Custom = append(st.Custom, t)
	}
	return st, rows.Err()
}

// PutTheme stores (or replaces) a custom theme; returns whether older themes were dropped.
func PutTheme(ctx context.Context, q Q, a Actor, user int64, id string, sanitized []byte) (bool, error) {
	if _, err := q.ExecContext(ctx, `INSERT INTO user_themes (user_id, theme_id, theme, updated_at) VALUES (?,?,?,?)
		ON CONFLICT (user_id, theme_id) DO UPDATE SET theme = excluded.theme, updated_at = excluded.updated_at`,
		user, id, string(sanitized), Now()); err != nil {
		return false, err
	}
	res, err := q.ExecContext(ctx, `DELETE FROM user_themes WHERE user_id = ? AND id NOT IN
		(SELECT id FROM user_themes WHERE user_id = ? ORDER BY updated_at DESC, id DESC LIMIT ?)`, user, user, MaxUserThemes)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	_, err = Activity(ctx, q, a, nil, nil, "theme.saved", "user", user, map[string]any{"theme_id": id})
	return n > 0, err
}

// DeleteTheme removes a custom theme.
func DeleteTheme(ctx context.Context, q Q, user int64, id string) error {
	res, err := q.ExecContext(ctx, `DELETE FROM user_themes WHERE user_id = ? AND theme_id = ?`, user, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	_, err = q.ExecContext(ctx, `UPDATE users SET theme_selected_id = NULL WHERE id = ? AND theme_selected_id = ?`, user, id)
	return err
}

// SelectTheme sets the selection and the match-the-system switch.
func SelectTheme(ctx context.Context, q Q, user int64, id *string, matchSystem bool) error {
	_, err := q.ExecContext(ctx, `UPDATE users SET theme_selected_id = ?, theme_match_system = ?, version = version + 1 WHERE id = ?`,
		nullStr(id), matchSystem, user)
	return err
}

// ---- blobs (avatars; more file kinds later): content-addressed files under <data>/files/ab/cd/<sha256>

// BlobPath is where a blob lives.
func (s *Store) BlobPath(sha string) string {
	return filepath.Join(s.dir, "files", sha[:2], sha[2:4], sha)
}

// WriteBlob stores data (deduplicated) and returns its sha256.
func (s *Store) WriteBlob(data []byte) (string, error) {
	sha, _, err := s.WriteBlobNew(data)
	return sha, err
}

// WriteBlobNew is WriteBlob that also says whether it created the file (false: it was already there),
// so a caller whose transaction fails can remove a file only it wrote (RemoveBlob, still inside that
// transaction).
func (s *Store) WriteBlobNew(data []byte) (string, bool, error) {
	h := sha256.Sum256(data)
	sha := hex.EncodeToString(h[:])
	p := s.BlobPath(sha)
	if _, err := os.Stat(p); err == nil {
		now := time.Now() // fresh again: the sweep leaves recent files alone
		_ = os.Chtimes(p, now, now)
		return sha, false, nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", false, err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		_ = os.Remove(tmp)
		return "", false, err
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return "", false, err
	}
	return sha, true, nil
}

// RemoveBlob deletes a blob file this caller just created, from inside the write transaction that
// failed (one writer: no other upload can have started using it meanwhile).
func (s *Store) RemoveBlob(sha string) {
	if blobName.MatchString(sha) {
		_ = os.Remove(s.BlobPath(sha))
	}
}

// RemoveUnusedBlob deletes a blob file no row points at; call it inside a write transaction (one
// writer: an upload that reuses the file does so inside its own).
func (s *Store) RemoveUnusedBlob(ctx context.Context, q Q, sha string) {
	if blobName.MatchString(sha) && !isReferenced(ctx, q, sha) {
		_ = os.Remove(s.BlobPath(sha))
	}
}

// SetAvatar points a user at an avatar blob (sha "" removes it) and keeps blob refcounts.
func SetAvatar(ctx context.Context, q Q, a Actor, user int64, sha string, size int) error {
	var old sql.NullString
	if err := q.QueryRowContext(ctx, `SELECT avatar_sha256 FROM users WHERE id = ?`, user).Scan(&old); err != nil {
		return err
	}
	if sha != "" {
		if _, err := q.ExecContext(ctx, `INSERT INTO blobs (sha256, size, refcount) VALUES (?,?,1)
			ON CONFLICT (sha256) DO UPDATE SET refcount = refcount + 1`, sha, size); err != nil {
			return err
		}
	}
	if old.Valid {
		if _, err := q.ExecContext(ctx, `UPDATE blobs SET refcount = refcount - 1 WHERE sha256 = ?`, old.String); err != nil {
			return err
		}
	}
	if _, err := q.ExecContext(ctx, `UPDATE users SET avatar_sha256 = ?, version = version + 1 WHERE id = ?`, nullStr(strPtrOrNil(sha)), user); err != nil {
		return err
	}
	_, err := Activity(ctx, q, a, nil, nil, "user.avatar", "user", user, map[string]any{"set": sha != ""})
	return err
}

// AvatarSHA returns a user's avatar blob id ("" when none).
func AvatarSHA(ctx context.Context, q Q, user int64) (string, error) {
	var s sql.NullString
	err := q.QueryRowContext(ctx, `SELECT avatar_sha256 FROM users WHERE id = ?`, user).Scan(&s)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return s.String, err
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
