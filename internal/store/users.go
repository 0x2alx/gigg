package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
)

// User is a person or an agent. Agents have no password and can't log in.
type User struct {
	ID          int64           `json:"id"`
	Username    string          `json:"username"`
	Kind        string          `json:"kind"`
	DisplayName string          `json:"display_name"`
	Description string          `json:"description"`
	Color       *string         `json:"color"`
	HasAvatar   bool            `json:"has_avatar"`
	IsAdmin     bool            `json:"is_admin"`
	Disabled    bool            `json:"disabled"`
	Preferences json.RawMessage `json:"preferences,omitempty"`
	CreatedAt   int64           `json:"created_at"`
	LastLoginAt *int64          `json:"last_login_at,omitempty"`
	Version     int64           `json:"version"`
	// HasPassword is filled in for admins only, on human users (see ShowPasswordState)
	HasPassword *bool `json:"has_password,omitempty" doc:"Admins only, humans only: whether the user has a password (without one they can't log in)"`

	PasswordHash string `json:"-"`
}

// ShowPasswordState fills in HasPassword on a human user (for an admin's view of the user list).
func (u *User) ShowPasswordState() {
	if u.Kind != "human" {
		return
	}
	has := u.PasswordHash != ""
	u.HasPassword = &has
}

var usernameRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,31}$`)

// ValidUsername reports whether a username is acceptable.
func ValidUsername(u string) bool { return usernameRe.MatchString(u) }

const userCols = `id, username, kind, display_name, description, color, avatar_sha256 IS NOT NULL, is_admin,
	disabled_at IS NOT NULL, preferences, created_at, last_login_at, version, COALESCE(password_hash, '')`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	var color sql.NullString
	var last sql.NullInt64
	var prefs string
	if err := row.Scan(&u.ID, &u.Username, &u.Kind, &u.DisplayName, &u.Description, &color, &u.HasAvatar, &u.IsAdmin,
		&u.Disabled, &prefs, &u.CreatedAt, &last, &u.Version, &u.PasswordHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	u.Color, u.LastLoginAt = ptrStr(color), ptrInt(last)
	u.Preferences = json.RawMessage(prefs)
	return &u, nil
}

// NewUser is the input to CreateUser.
type NewUser struct {
	Username     string
	Kind         string // human | agent
	DisplayName  string
	PasswordHash string // humans only; may be empty (no login until set)
	IsAdmin      bool
}

// CreateUser inserts a user.
func CreateUser(ctx context.Context, q Q, a Actor, n NewUser) (*User, error) {
	if !ValidUsername(n.Username) {
		return nil, Invalid("username: 2-32 characters, lowercase letters, digits, - and _, starting with a letter")
	}
	if n.Kind != "human" && n.Kind != "agent" {
		return nil, Invalid("kind is human or agent")
	}
	if n.Kind == "agent" && n.PasswordHash != "" {
		return nil, Invalid("agents have no password")
	}
	if len(n.DisplayName) > 64 {
		return nil, Invalid("display name: at most 64 characters")
	}
	var pw any
	if n.PasswordHash != "" {
		pw = n.PasswordHash
	}
	res, err := q.ExecContext(ctx, `INSERT INTO users (username, kind, display_name, password_hash, is_admin, created_at)
		VALUES (?,?,?,?,?,?)`, n.Username, n.Kind, n.DisplayName, pw, n.IsAdmin, Now())
	if isUnique(err) {
		return nil, Conflict("username %q is taken", n.Username)
	}
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if _, err := Activity(ctx, q, a, nil, nil, "user.created", "user", id, map[string]any{"username": n.Username, "kind": n.Kind, "is_admin": n.IsAdmin}); err != nil {
		return nil, err
	}
	return GetUser(ctx, q, id)
}

// GetUser loads a user by id.
func GetUser(ctx context.Context, q Q, id int64) (*User, error) {
	return scanUser(q.QueryRowContext(ctx, "SELECT "+userCols+" FROM users WHERE id = ?", id))
}

// GetUserByName loads a user by username (case-insensitive).
func GetUserByName(ctx context.Context, q Q, name string) (*User, error) {
	return scanUser(q.QueryRowContext(ctx, "SELECT "+userCols+" FROM users WHERE username = ?", name))
}

// ListUsers returns every user (admin view) or the given ids.
func ListUsers(ctx context.Context, q Q, ids []int64) ([]*User, error) {
	query := "SELECT " + userCols + " FROM users"
	var args []any
	if ids != nil {
		query += " WHERE id IN (" + placeholders(len(ids)) + ")"
		args = int64Args(ids)
	}
	rows, err := q.QueryContext(ctx, query+" ORDER BY username", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// SetPassword replaces a human's password hash and revokes their sessions.
func SetPassword(ctx context.Context, q Q, a Actor, userID int64, hash string) error {
	res, err := q.ExecContext(ctx, `UPDATE users SET password_hash = ?, version = version + 1 WHERE id = ? AND kind = 'human'`, hash, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Invalid("only human users have passwords")
	}
	if _, err := q.ExecContext(ctx, `UPDATE sessions SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`, Now(), userID); err != nil {
		return err
	}
	_, err = Activity(ctx, q, a, nil, nil, "user.password_set", "user", userID, nil)
	return err
}

// UserPatch is a partial profile update. Nil fields stay as they are.
type UserPatch struct {
	DisplayName *string
	Description *string
	Color       **string
	Preferences *json.RawMessage
	IsAdmin     *bool
	Disabled    *bool
}

var colorRe = regexp.MustCompile(`^#[0-9A-F]{6}$`)

// NormColor upper-cases a #rrggbb colour and validates it.
func NormColor(c string) (string, error) {
	b := []byte(c)
	for i := range b {
		if b[i] >= 'a' && b[i] <= 'f' {
			b[i] -= 32
		}
	}
	if !colorRe.Match(b) {
		return "", Invalid("colours are #RRGGBB")
	}
	return string(b), nil
}

// PatchUser applies a profile change; expected version 0 skips the check.
func PatchUser(ctx context.Context, q Q, a Actor, id, expect int64, p UserPatch) (*User, error) {
	u, err := GetUser(ctx, q, id)
	if err != nil {
		return nil, err
	}
	if expect != 0 && expect != u.Version {
		return nil, ErrVersion
	}
	changed := map[string]any{}
	set := func(col string, v any) {
		changed[col] = v
	}
	if p.DisplayName != nil {
		if len(*p.DisplayName) > 64 {
			return nil, Invalid("display name: at most 64 characters")
		}
		set("display_name", *p.DisplayName)
	}
	if p.Description != nil {
		if len(*p.Description) > 200 {
			return nil, Invalid("description: at most 200 characters")
		}
		set("description", *p.Description)
	}
	if p.Color != nil {
		if *p.Color == nil {
			set("color", nil)
		} else {
			c, err := NormColor(**p.Color)
			if err != nil {
				return nil, err
			}
			set("color", c)
		}
	}
	if p.Preferences != nil {
		// a merge of top-level keys: each key given replaces that preference, null removes it, the rest stay.
		// Two tabs (or two clients) each changing their own key then can't undo each other's change.
		var in map[string]json.RawMessage
		if json.Unmarshal(*p.Preferences, &in) != nil || in == nil || len(*p.Preferences) > 16384 {
			return nil, Invalid("preferences must be a JSON object under 16 KiB")
		}
		if len(in) > 0 { // {} changes nothing
			cur := map[string]json.RawMessage{}
			if len(u.Preferences) > 0 {
				_ = json.Unmarshal(u.Preferences, &cur) // stored by this function: always an object
				if cur == nil {
					cur = map[string]json.RawMessage{}
				}
			}
			for k, v := range in {
				if string(v) == "null" {
					delete(cur, k)
				} else {
					cur[k] = v
				}
			}
			b, _ := json.Marshal(cur)
			if len(b) > 16384 {
				return nil, Invalid("preferences must be a JSON object under 16 KiB")
			}
			set("preferences", string(b))
		}
	}
	if p.IsAdmin != nil {
		set("is_admin", *p.IsAdmin)
	}
	if p.Disabled != nil {
		if *p.Disabled {
			set("disabled_at", Now())
		} else {
			set("disabled_at", nil)
		}
	}
	if len(changed) == 0 {
		return u, nil
	}
	query, args := "UPDATE users SET version = version + 1", []any{}
	for col, v := range changed {
		query += ", " + col + " = ?"
		args = append(args, v)
	}
	args = append(args, id)
	if _, err := q.ExecContext(ctx, query+" WHERE id = ?", args...); err != nil {
		return nil, err
	}
	if p.Disabled != nil && *p.Disabled {
		if _, err := q.ExecContext(ctx, `UPDATE sessions SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`, Now(), id); err != nil {
			return nil, err
		}
	}
	if _, err := Activity(ctx, q, a, nil, nil, "user.updated", "user", id, changed); err != nil {
		return nil, err
	}
	return GetUser(ctx, q, id)
}

// AdminCount returns the number of enabled admins (the last one can't be demoted).
func AdminCount(ctx context.Context, q Q) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE is_admin = 1 AND disabled_at IS NULL`).Scan(&n)
	return n, err
}
