package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Q is what queries run on: the writer, the reader pool or a transaction.
type Q interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Errors the API layer maps to HTTP statuses.
var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	ErrVersion  = errors.New("version mismatch")
	ErrInvalid  = errors.New("invalid")
	ErrDenied   = errors.New("forbidden")
)

// Denied wraps ErrDenied (403) with a message for the client.
func Denied(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrDenied, fmt.Sprintf(format, a...))
}

// Invalid wraps ErrInvalid with a message for the client.
func Invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// Conflict wraps ErrConflict with a message for the client.
func Conflict(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrConflict, fmt.Sprintf(format, a...))
}

// Now is the store's clock (tests replace it).
var Now = func() int64 { return time.Now().UnixMilli() }

// Tx runs fn in a write transaction (BEGIN IMMEDIATE via _txlock) and commits if it returns nil.
func (s *Store) Tx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// also on a panic in fn: a transaction left open would hold the only write connection (a long-lived
	// context, such as housekeeping's, never cancels it); after Commit this is a no-op
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// isUnique reports a UNIQUE constraint failure.
func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func nullStr(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// wsArg is a workspace id for SQL: NULL for 0 (a shared template or schedule has no workspace).
func wsArg(ws int64) any {
	if ws == 0 {
		return nil
	}
	return ws
}

// wsRef is a workspace id for Activity: nil for 0 (shared).
func wsRef(ws int64) *int64 {
	if ws == 0 {
		return nil
	}
	return &ws
}

// AutoArchiveMax is the longest auto-archive delay (a leap year, in minutes); the shortest is 5.
const AutoArchiveMax = 527040

// Run timeouts in minutes (gigs; NULL: the agent's default) and seconds (errands).
const (
	RunTimeoutMin, RunTimeoutMax       = 5, 300
	ErrandTimeoutMin, ErrandTimeoutMax = 10, 43200
)

// validStartCategory: where a template's gigs start ("" = the project's default status).
func validStartCategory(c string) bool { return c == "" || c == "backlog" || c == "todo" }

// startStatus: the status a template's gig starts in: the first status of category c in the project,
// or nil (the project's default) for "". Fails closed: a project without such a status (or a failed
// lookup) is an error, never the default, so a Backlog gig can't land in To-Do where a runner claims it.
func startStatus(ctx context.Context, q Q, project int64, c string) (*int64, error) {
	if c == "" {
		return nil, nil
	}
	var id int64
	err := q.QueryRowContext(ctx, `SELECT id FROM statuses WHERE project_id = ? AND category = ? ORDER BY position LIMIT 1`, project, c).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, Invalid("start_category: this project has no %s status; add one, or let its gigs start in the project's default status", c)
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// validRunTimeout checks a gig's run timeout in minutes (nil: the agent's default).
func validRunTimeout(m *int64) error {
	if m != nil && (*m < RunTimeoutMin || *m > RunTimeoutMax) {
		return Invalid("run_timeout_minutes: %d to %d (or none: the agent's default)", RunTimeoutMin, RunTimeoutMax)
	}
	return nil
}

// validAutoArchive checks an auto-archive delay in minutes (nil: never).
func validAutoArchive(m *int64) error {
	if m != nil && (*m < 5 || *m > AutoArchiveMax) {
		return Invalid("auto_archive_minutes: 5 to %d (or none)", AutoArchiveMax)
	}
	return nil
}

func nullInt(i *int64) any {
	if i == nil {
		return nil
	}
	return *i
}

func ptrStr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	s := ns.String
	return &s
}

func ptrInt(ni sql.NullInt64) *int64 {
	if !ni.Valid {
		return nil
	}
	i := ni.Int64
	return &i
}

func ptrFloat(nf sql.NullFloat64) *float64 {
	if !nf.Valid {
		return nil
	}
	f := nf.Float64
	return &f
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// placeholders returns "?,?,?" for n values.
func placeholders(n int) string {
	if n <= 0 {
		return "NULL"
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func int64Args(ids []int64) []any {
	out := make([]any, len(ids))
	for i, v := range ids {
		out[i] = v
	}
	return out
}

// Actor is who does a write, recorded in the activity log.
type Actor struct {
	UserID    int64
	KeyID     *int64
	Listener  string
	IP        string
	RequestID string
}

// Activity appends one audit/event row in the same transaction as the change.
func Activity(ctx context.Context, q Q, a Actor, workspaceID, projectID *int64, action, entityType string, entityID int64, data any) (int64, error) {
	if data == nil {
		data = map[string]any{}
	}
	var actor any
	if a.UserID != 0 {
		actor = a.UserID
	}
	res, err := q.ExecContext(ctx, `INSERT INTO activity (at, workspace_id, project_id, actor_user_id, key_id, listener, ip, request_id, action, entity_type, entity_id, data)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, Now(), nullInt(workspaceID), nullInt(projectID), actor, nullInt(a.KeyID),
		a.Listener, a.IP, a.RequestID, action, entityType, entityID, mustJSON(data))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func i64(v int64) *int64 { return &v }

func timeFromMS(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

// checkVersion compares an If-Match version with the row's version read in this transaction (the
// caller's copy may come from the read pool, so it can be stale). 0 = no check.
func checkVersion(ctx context.Context, q Q, table string, id, expect int64) error {
	if expect == 0 {
		return nil
	}
	var v int64
	if err := q.QueryRowContext(ctx, "SELECT version FROM "+table+" WHERE id = ?", id).Scan(&v); err != nil {
		return err
	}
	if v != expect {
		return ErrVersion
	}
	return nil
}
