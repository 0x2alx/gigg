package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/netip"
	"sort"
)

// Session is a browser login.
type Session struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"user_id"`
	CreatedAt  int64  `json:"created_at"`
	LastSeenAt int64  `json:"last_seen_at"`
	ExpiresAt  int64  `json:"expires_at"`
	IP         string `json:"ip"`
	UserAgent  string `json:"user_agent"`
}

// SessionTTL is the sliding lifetime of a session.
const SessionTTL = 30 * 24 * 3600 * 1000

// CreateSession stores a new session for tokenHash.
func CreateSession(ctx context.Context, q Q, userID int64, tokenHash []byte, ip, ua string) (*Session, error) {
	now := Now()
	if len(ua) > 256 {
		ua = ua[:256]
	}
	res, err := q.ExecContext(ctx, `INSERT INTO sessions (user_id, token_hash, created_at, last_seen_at, expires_at, ip, user_agent)
		VALUES (?,?,?,?,?,?,?)`, userID, tokenHash, now, now, now+SessionTTL, ip, ua)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if _, err := q.ExecContext(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`, now, userID); err != nil {
		return nil, err
	}
	return &Session{ID: id, UserID: userID, CreatedAt: now, LastSeenAt: now, ExpiresAt: now + SessionTTL, IP: ip, UserAgent: ua}, nil
}

// SessionByHash returns a live session (not revoked, not expired).
func SessionByHash(ctx context.Context, q Q, tokenHash []byte) (*Session, error) {
	var s Session
	err := q.QueryRowContext(ctx, `SELECT id, user_id, created_at, last_seen_at, expires_at, ip, user_agent FROM sessions
		WHERE token_hash = ? AND revoked_at IS NULL AND expires_at > ?`, tokenHash, Now()).
		Scan(&s.ID, &s.UserID, &s.CreatedAt, &s.LastSeenAt, &s.ExpiresAt, &s.IP, &s.UserAgent)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &s, err
}

// TouchSession slides the expiry (at most once a minute per session).
func TouchSession(ctx context.Context, q Q, id int64) error {
	now := Now()
	_, err := q.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE id = ? AND last_seen_at < ?`,
		now, now+SessionTTL, id, now-60_000)
	return err
}

// RevokeSession ends a session; userID 0 means any user's (admin).
func RevokeSession(ctx context.Context, q Q, id, userID int64) error {
	query, args := `UPDATE sessions SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, []any{Now(), id}
	if userID != 0 {
		query += " AND user_id = ?"
		args = append(args, userID)
	}
	res, err := q.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListSessions returns a user's live sessions.
func ListSessions(ctx context.Context, q Q, userID int64) ([]*Session, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, user_id, created_at, last_seen_at, expires_at, ip, user_agent FROM sessions
		WHERE user_id = ? AND revoked_at IS NULL AND expires_at > ? ORDER BY last_seen_at DESC`, userID, Now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.UserID, &s.CreatedAt, &s.LastSeenAt, &s.ExpiresAt, &s.IP, &s.UserAgent); err != nil {
			return nil, err
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

// APIKey is a bearer credential acting as one user.
type APIKey struct {
	ID               int64    `json:"id"`
	Name             string   `json:"name"`
	Prefix           string   `json:"prefix"`
	UserID           int64    `json:"user_id"`
	WorkspaceID      *int64   `json:"workspace_id"` // nil = admin key
	ProjectIDs       []int64  `json:"project_ids"`  // empty = every project of the workspace
	Scopes           []string `json:"scopes"`
	AllowedListeners []string `json:"allowed_listeners"`
	AllowedCIDRs     []string `json:"allowed_cidrs"`
	ExpiresAt        int64    `json:"expires_at"`
	CreatedAt        int64    `json:"created_at"`
	CreatedByUser    *int64   `json:"created_by_user"`
	CreatedByKey     *int64   `json:"created_by_key"`
	LastUsedAt       *int64   `json:"last_used_at"`
	LastUsedListener *string  `json:"last_used_listener"`
	LastUsedIP       *string  `json:"last_used_ip"`
	Revoked          bool     `json:"revoked"`
	Version          int64    `json:"version"`

	SecretHash []byte `json:"-"`
}

// AllScopes is every scope a key can carry; admin:* scopes only on admin keys.
var AllScopes = []string{
	"workspace:read", "workspace:write", "gigs:read", "gigs:work", "gigs:write", "gigs:delete", "comments:read", "comments:write",
	"artifacts:write", "runs:write", "projects:write", "profile:write", "keys:self", "errands:read", "errands:output", "errands:write", "errands:run",
	"admin:workspaces", "admin:members", "admin:users", "admin:keys", "admin:transfer", "admin:audit",
	"admin:export", "admin:settings",
}

// IsAdminScope reports an admin:* scope.
func IsAdminScope(s string) bool { return len(s) > 6 && s[:6] == "admin:" }

const keyCols = `id, name, prefix, user_id, workspace_id, scopes, allowed_listeners, allowed_cidrs, expires_at, created_at,
	created_by_user, created_by_key, last_used_at, last_used_listener, last_used_ip, revoked_at IS NOT NULL, version, secret_hash`

func scanKey(row interface{ Scan(...any) error }) (*APIKey, error) {
	var k APIKey
	var ws, cbu, cbk, lu sql.NullInt64
	var scopes string
	var listeners, cidrs, lul, lip sql.NullString
	if err := row.Scan(&k.ID, &k.Name, &k.Prefix, &k.UserID, &ws, &scopes, &listeners, &cidrs, &k.ExpiresAt, &k.CreatedAt,
		&cbu, &cbk, &lu, &lul, &lip, &k.Revoked, &k.Version, &k.SecretHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	k.WorkspaceID, k.CreatedByUser, k.CreatedByKey, k.LastUsedAt = ptrInt(ws), ptrInt(cbu), ptrInt(cbk), ptrInt(lu)
	k.LastUsedListener, k.LastUsedIP = ptrStr(lul), ptrStr(lip)
	_ = json.Unmarshal([]byte(scopes), &k.Scopes)
	if listeners.Valid {
		_ = json.Unmarshal([]byte(listeners.String), &k.AllowedListeners)
	}
	if cidrs.Valid {
		_ = json.Unmarshal([]byte(cidrs.String), &k.AllowedCIDRs)
	}
	return &k, nil
}

func (k *APIKey) loadProjects(ctx context.Context, q Q) error {
	rows, err := q.QueryContext(ctx, `SELECT project_id FROM api_key_projects WHERE key_id = ? ORDER BY project_id`, k.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	k.ProjectIDs = []int64{}
	for rows.Next() {
		var p int64
		if err := rows.Scan(&p); err != nil {
			return err
		}
		k.ProjectIDs = append(k.ProjectIDs, p)
	}
	return rows.Err()
}

// NewKey is the input to CreateKey.
type NewKey struct {
	Name             string
	Prefix           string
	SecretHash       []byte
	UserID           int64
	WorkspaceID      *int64
	ProjectIDs       []int64
	Scopes           []string
	AllowedListeners []string // nil = any
	AllowedCIDRs     []string // nil = any
	ExpiresAt        int64
	CreatedByUser    *int64
	CreatedByKey     *int64
}

// NormalizeScopes validates and sorts scopes.
func NormalizeScopes(scopes []string, adminKey bool) ([]string, error) {
	known := map[string]bool{}
	for _, s := range AllScopes {
		known[s] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, s := range scopes {
		if !known[s] {
			return nil, Invalid("unknown scope %q", s)
		}
		if IsAdminScope(s) && !adminKey {
			return nil, Invalid("%s needs an admin key (an admin user, no workspace)", s)
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil, Invalid("a key needs at least one scope")
	}
	sort.Strings(out)
	return out, nil
}

// ValidateCIDRs checks network restrictions.
func ValidateCIDRs(c []string) error {
	for _, s := range c {
		if _, err := netip.ParsePrefix(s); err != nil {
			return Invalid("%q is not a CIDR", s)
		}
	}
	return nil
}

func jsonOrNull(v []string) any {
	if v == nil {
		return nil
	}
	return mustJSON(v)
}

// CreateKey stores a key. The caller has checked who may create it and with what reach.
func CreateKey(ctx context.Context, q Q, a Actor, n NewKey) (*APIKey, error) {
	if n.Name == "" || len(n.Name) > 64 {
		return nil, Invalid("key name: 1-64 characters")
	}
	adminKey := false
	if n.WorkspaceID == nil {
		u, err := GetUser(ctx, q, n.UserID)
		if err != nil {
			return nil, err
		}
		adminKey = u.IsAdmin
	}
	scopes, err := NormalizeScopes(n.Scopes, adminKey)
	if err != nil {
		return nil, err
	}
	if err := ValidateCIDRs(n.AllowedCIDRs); err != nil {
		return nil, err
	}
	if n.ExpiresAt <= Now() {
		return nil, Invalid("expiry must be in the future")
	}
	if n.ExpiresAt > Now()+MaxKeyLifetime {
		return nil, Invalid("keys live at most 3 years")
	}
	res, err := q.ExecContext(ctx, `INSERT INTO api_keys (name, prefix, secret_hash, user_id, workspace_id, scopes, allowed_listeners,
		allowed_cidrs, expires_at, created_at, created_by_user, created_by_key) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		n.Name, n.Prefix, n.SecretHash, n.UserID, nullInt(n.WorkspaceID), mustJSON(scopes), jsonOrNull(n.AllowedListeners),
		jsonOrNull(n.AllowedCIDRs), n.ExpiresAt, Now(), nullInt(n.CreatedByUser), nullInt(n.CreatedByKey))
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	for _, p := range n.ProjectIDs {
		if _, err := q.ExecContext(ctx, `INSERT INTO api_key_projects (key_id, project_id)
			SELECT ?, id FROM projects WHERE id = ? AND workspace_id = ?`, id, p, nullInt(n.WorkspaceID)); err != nil {
			return nil, err
		}
	}
	k, err := GetKey(ctx, q, id)
	if err != nil {
		return nil, err
	}
	if len(k.ProjectIDs) != len(n.ProjectIDs) {
		return nil, Invalid("projects must belong to the key's workspace")
	}
	_, err = Activity(ctx, q, a, n.WorkspaceID, nil, "key.created", "key", id, map[string]any{
		"name": n.Name, "prefix": n.Prefix, "user_id": n.UserID, "scopes": scopes, "projects": n.ProjectIDs,
		"allowed_listeners": n.AllowedListeners, "allowed_cidrs": n.AllowedCIDRs, "expires_at": n.ExpiresAt})
	return k, err
}

// GetKey loads a key with its project narrowing.
func GetKey(ctx context.Context, q Q, id int64) (*APIKey, error) {
	k, err := scanKey(q.QueryRowContext(ctx, "SELECT "+keyCols+" FROM api_keys WHERE id = ?", id))
	if err != nil {
		return nil, err
	}
	return k, k.loadProjects(ctx, q)
}

// KeyByPrefix loads a key for authentication.
func KeyByPrefix(ctx context.Context, q Q, prefix string) (*APIKey, error) {
	k, err := scanKey(q.QueryRowContext(ctx, "SELECT "+keyCols+" FROM api_keys WHERE prefix = ?", prefix))
	if err != nil {
		return nil, err
	}
	return k, k.loadProjects(ctx, q)
}

// ListKeys returns keys acting as userID, or all keys when userID is 0.
func ListKeys(ctx context.Context, q Q, userID int64) ([]*APIKey, error) {
	query, args := "SELECT "+keyCols+" FROM api_keys", []any{}
	if userID != 0 {
		query += " WHERE user_id = ?"
		args = append(args, userID)
	}
	rows, err := q.QueryContext(ctx, query+" ORDER BY id", args...)
	if err != nil {
		return nil, err
	}
	var out []*APIKey
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, k)
	}
	rows.Close()
	for _, k := range out {
		if err := k.loadProjects(ctx, q); err != nil {
			return nil, err
		}
	}
	return out, rows.Err()
}

// TouchKey records a use (at most once a minute per key and listener).
func TouchKey(ctx context.Context, q Q, id int64, listener, ip string) error {
	now := Now()
	_, err := q.ExecContext(ctx, `UPDATE api_keys SET last_used_at = ?, last_used_listener = ?, last_used_ip = ?
		WHERE id = ? AND (last_used_at IS NULL OR last_used_at < ? OR last_used_listener IS NOT ? OR last_used_ip IS NOT ?)`,
		now, listener, ip, id, now-60_000, listener, ip)
	return err
}

// RevokeKey revokes a key and every key created by it (recursively): a key's children are only as
// trustworthy as the key that made them.
func RevokeKey(ctx context.Context, q Q, a Actor, k *APIKey) error {
	if _, err := q.ExecContext(ctx, `UPDATE api_keys SET revoked_at = ?, version = version + 1 WHERE id = ? AND revoked_at IS NULL`, Now(), k.ID); err != nil {
		return err
	}
	if _, err := Activity(ctx, q, a, k.WorkspaceID, nil, "key.revoked", "key", k.ID, map[string]any{"prefix": k.Prefix}); err != nil {
		return err
	}
	return revokeChildren(ctx, q, a, k.ID)
}

func revokeChildren(ctx context.Context, q Q, a Actor, parent int64) error {
	rows, err := q.QueryContext(ctx, `WITH RECURSIVE d(id) AS (SELECT id FROM api_keys WHERE created_by_key = ?
		UNION SELECT k.id FROM api_keys k JOIN d ON k.created_by_key = d.id) SELECT id FROM d`, parent)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		res, err := q.ExecContext(ctx, `UPDATE api_keys SET revoked_at = ?, version = version + 1 WHERE id = ? AND revoked_at IS NULL`, Now(), id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			if _, err := Activity(ctx, q, a, nil, nil, "key.revoked", "key", id, map[string]any{"reason": "parent key revoked or rotated", "parent": parent}); err != nil {
				return err
			}
		}
	}
	return nil
}

// MaxKeyLifetime caps expires_at.
const MaxKeyLifetime = int64(3 * 366 * 24 * 3600 * 1000)

// RotateKey replaces a key's secret (same id, prefix changes).
func RotateKey(ctx context.Context, q Q, a Actor, k *APIKey, prefix string, secretHash []byte) (*APIKey, error) {
	if _, err := q.ExecContext(ctx, `UPDATE api_keys SET prefix = ?, secret_hash = ?, version = version + 1 WHERE id = ? AND revoked_at IS NULL`,
		prefix, secretHash, k.ID); err != nil {
		return nil, err
	}
	if _, err := Activity(ctx, q, a, k.WorkspaceID, nil, "key.rotated", "key", k.ID, map[string]any{"old_prefix": k.Prefix, "prefix": prefix}); err != nil {
		return nil, err
	}
	// a rotation usually means the old secret leaked: whatever it created goes too
	if err := revokeChildren(ctx, q, a, k.ID); err != nil {
		return nil, err
	}
	return GetKey(ctx, q, k.ID)
}
