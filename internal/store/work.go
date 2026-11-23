package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"regexp"
	"strings"
	"time"
)

// Comment is Markdown text on a gig.
type Comment struct {
	ID             int64  `json:"id"`
	WorkspaceID    int64  `json:"workspace_id"`
	GigID          int64  `json:"gig_id"`
	AuthorID       int64  `json:"author_id"`
	AuthorUsername string `json:"author_username"`
	AuthorKind     string `json:"author_kind"`
	AuthorIsAdmin  bool   `json:"author_is_admin"`
	KeyID          *int64 `json:"key_id"`
	Kind           string `json:"kind"`
	BodyMD         string `json:"body_md"`
	RunID          *int64 `json:"run_id"`
	CreatedAt      int64  `json:"created_at"`
	EditedAt       *int64 `json:"edited_at"`
	Deleted        bool   `json:"deleted"`
	Version        int64  `json:"version"`
}

const commentCols = `c.id, c.workspace_id, c.gig_id, c.author_id, u.username, u.kind, u.is_admin, c.key_id, c.kind,
	CASE WHEN c.deleted_at IS NULL THEN c.body_md ELSE '' END, c.run_id, c.created_at, c.edited_at, c.deleted_at IS NOT NULL, c.version`

func scanComment(row interface{ Scan(...any) error }) (*Comment, error) {
	var c Comment
	var key, run, edited sql.NullInt64
	if err := row.Scan(&c.ID, &c.WorkspaceID, &c.GigID, &c.AuthorID, &c.AuthorUsername, &c.AuthorKind, &c.AuthorIsAdmin,
		&key, &c.Kind, &c.BodyMD, &run, &c.CreatedAt, &edited, &c.Deleted, &c.Version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	c.KeyID, c.RunID, c.EditedAt = ptrInt(key), ptrInt(run), ptrInt(edited)
	return &c, nil
}

// CommentKinds a client may post. "system" is server-only; "resume_request" comes from ResumeGig.
var clientCommentKinds = map[string]bool{"comment": true, "report": true, "question": true}

// AddComment posts a comment.
func AddComment(ctx context.Context, q Q, a Actor, t *Gig, kind, body string, runID *int64) (*Comment, error) {
	if kind == "" {
		kind = "comment"
	}
	if !clientCommentKinds[kind] && kind != "system" && kind != "resume_request" {
		return nil, Invalid("comment kind is comment, report or question")
	}
	if strings.TrimSpace(body) == "" || len(body) > 65536 {
		return nil, Invalid("comment: 1 character to 64 KiB")
	}
	res, err := q.ExecContext(ctx, `INSERT INTO comments (workspace_id, gig_id, author_id, key_id, kind, body_md, run_id, created_at)
		VALUES (?,?,?,?,?,?,?,?)`, t.WorkspaceID, t.ID, a.UserID, nullInt(a.KeyID), kind, body, nullInt(runID), Now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if _, err := q.ExecContext(ctx, `UPDATE gigs SET comment_count = comment_count + 1, updated_at = ? WHERE id = ?`, Now(), t.ID); err != nil {
		return nil, err
	}
	if clientCommentKinds[kind] {
		if err := touchProgress(ctx, q, t.ID); err != nil {
			return nil, err
		}
	}
	if _, err := Activity(ctx, q, a, &t.WorkspaceID, &t.ProjectID, "comment.created", "comment", id, map[string]any{"gig_id": t.ID, "kind": kind}); err != nil {
		return nil, err
	}
	// the bell: @mentions; questions and reports go to the gig's assignees and followers
	if kind != "system" {
		if err := Notify(ctx, q, a, t, &id, "mention", MentionedUsers(ctx, q, body)); err != nil {
			return nil, err
		}
	}
	if kind == "question" || kind == "report" {
		nk := "comment"
		if kind == "question" {
			nk = "question"
		}
		if err := NotifyWatchers(ctx, q, a, t, &id, nk); err != nil {
			return nil, err
		}
	}
	return GetComment(ctx, q, id)
}

// LatestQuestion returns a gig's newest visible question or report (nil if none).
func LatestQuestion(ctx context.Context, q Q, gig int64) (*Comment, error) {
	c, err := scanComment(q.QueryRowContext(ctx, "SELECT "+commentCols+` FROM comments c JOIN users u ON u.id = c.author_id
		WHERE c.gig_id = ? AND c.kind IN ('question','report') AND c.deleted_at IS NULL ORDER BY c.id DESC LIMIT 1`, gig))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return c, err
}

// IsClientCommentKind reports kinds clients may post directly.
func IsClientCommentKind(k string) bool { return k == "" || clientCommentKinds[k] }

// GetComment loads a comment.
func GetComment(ctx context.Context, q Q, id int64) (*Comment, error) {
	return scanComment(q.QueryRowContext(ctx, "SELECT "+commentCols+" FROM comments c JOIN users u ON u.id = c.author_id WHERE c.id = ?", id))
}

// Comments lists a gig's comments after sinceID, oldest first.
func Comments(ctx context.Context, q Q, gig, sinceID int64, limit int) ([]*Comment, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	rows, err := q.QueryContext(ctx, "SELECT "+commentCols+` FROM comments c JOIN users u ON u.id = c.author_id
		WHERE c.gig_id = ? AND c.id > ? ORDER BY c.id LIMIT ?`, gig, sinceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Comment
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// EditComment replaces a comment's text; the old text goes to the activity log.
func EditComment(ctx context.Context, q Q, a Actor, c *Comment, expect int64, body string) (*Comment, error) {
	if err := checkVersion(ctx, q, "comments", c.ID, expect); err != nil {
		return nil, err
	}
	if c.Deleted {
		return nil, Conflict("the comment was deleted")
	}
	if strings.TrimSpace(body) == "" || len(body) > 65536 {
		return nil, Invalid("comment: 1 character to 64 KiB")
	}
	if _, err := q.ExecContext(ctx, `UPDATE comments SET body_md = ?, edited_at = ?, version = version + 1 WHERE id = ?`, body, Now(), c.ID); err != nil {
		return nil, err
	}
	if _, err := Activity(ctx, q, a, &c.WorkspaceID, nil, "comment.edited", "comment", c.ID, map[string]any{"gig_id": c.GigID, "old": c.BodyMD}); err != nil {
		return nil, err
	}
	return GetComment(ctx, q, c.ID)
}

// DeleteComment hides a comment's text (the row stays, so threads keep their shape).
func DeleteComment(ctx context.Context, q Q, a Actor, c *Comment) error {
	if c.Deleted {
		return nil
	}
	if _, err := q.ExecContext(ctx, `UPDATE comments SET deleted_at = ?, version = version + 1 WHERE id = ?`, Now(), c.ID); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, `UPDATE gigs SET comment_count = comment_count - 1 WHERE id = ?`, c.GigID); err != nil {
		return err
	}
	_, err := Activity(ctx, q, a, &c.WorkspaceID, nil, "comment.deleted", "comment", c.ID, map[string]any{"gig_id": c.GigID, "old": c.BodyMD})
	return err
}

// Run is one worker invocation on a gig: a record, nothing more.
type Run struct {
	ID               int64    `json:"id"`
	WorkspaceID      int64    `json:"workspace_id"`
	GigID            int64    `json:"gig_id"`
	WorkerUserID     int64    `json:"worker_user_id"`
	WorkerUsername   string   `json:"worker_username"`
	KeyID            *int64   `json:"key_id"`
	SessionID        *string  `json:"session_id"`
	Mode             string   `json:"mode"`
	Attempt          int64    `json:"attempt"`
	Status           string   `json:"status"`
	StartedAt        int64    `json:"started_at"`
	HeartbeatAt      int64    `json:"heartbeat_at"`
	EndedAt          *int64   `json:"ended_at"`
	DurationMS       *int64   `json:"duration_ms"`
	CostUSD          *float64 `json:"cost_usd"`
	NumTurns         *int64   `json:"num_turns"`
	TokensIn         *int64   `json:"tokens_in"`
	TokensOut        *int64   `json:"tokens_out"`
	TokensCacheRead  *int64   `json:"tokens_cache_read"`
	TokensCacheWrite *int64   `json:"tokens_cache_write"`
	Model            *string  `json:"model"`
	ExitCode         *int64   `json:"exit_code"`
	ErrorReason      *string  `json:"error_reason"`
	TranscriptPath   *string  `json:"transcript_path"`
	LogPath          *string  `json:"log_path"`
	ReportCommentID  *int64   `json:"report_comment_id"`
	ProgressPct      *int64   `json:"progress_pct"`
	ProgressNote     *string  `json:"progress_note"`
	UsagePct         *float64 `json:"usage_pct" doc:"Points of the account's weekly usage meter the run cost (measured by the runner; shared meter)"`
	Version          int64    `json:"version"`
}

const runCols = `r.id, r.workspace_id, r.gig_id, r.worker_user_id, u.username, r.key_id, r.session_id, r.mode, r.attempt, r.status,
	r.started_at, r.heartbeat_at, r.ended_at, r.duration_ms, r.cost_usd, r.num_turns, r.tokens_in, r.tokens_out, r.tokens_cache_read,
	r.tokens_cache_write, r.model, r.exit_code, r.error_reason, r.transcript_path, r.log_path, r.report_comment_id, r.progress_pct,
	r.progress_note, r.version, r.usage_pct`

func scanRun(row interface{ Scan(...any) error }) (*Run, error) {
	var r Run
	var key, ended, dur, turns, tin, tout, tcr, tcw, exit, rep, pct sql.NullInt64
	var sess, model, reason, tp, lp, note sql.NullString
	var cost, usage sql.NullFloat64
	if err := row.Scan(&r.ID, &r.WorkspaceID, &r.GigID, &r.WorkerUserID, &r.WorkerUsername, &key, &sess, &r.Mode, &r.Attempt, &r.Status,
		&r.StartedAt, &r.HeartbeatAt, &ended, &dur, &cost, &turns, &tin, &tout, &tcr, &tcw, &model, &exit, &reason, &tp, &lp, &rep,
		&pct, &note, &r.Version, &usage); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	r.KeyID, r.EndedAt, r.DurationMS, r.NumTurns = ptrInt(key), ptrInt(ended), ptrInt(dur), ptrInt(turns)
	r.TokensIn, r.TokensOut, r.TokensCacheRead, r.TokensCacheWrite = ptrInt(tin), ptrInt(tout), ptrInt(tcr), ptrInt(tcw)
	r.ExitCode, r.ReportCommentID, r.ProgressPct = ptrInt(exit), ptrInt(rep), ptrInt(pct)
	r.SessionID, r.Model, r.ErrorReason, r.TranscriptPath, r.LogPath, r.ProgressNote = ptrStr(sess), ptrStr(model), ptrStr(reason), ptrStr(tp), ptrStr(lp), ptrStr(note)
	r.CostUSD, r.UsagePct = ptrFloat(cost), ptrFloat(usage)
	return &r, nil
}

// SetRunUsage records what a finished run cost on the weekly usage meter (once).
func SetRunUsage(ctx context.Context, q Q, r *Run, pct float64) (*Run, error) {
	if r.Status == "running" {
		return nil, Conflict("the run is still running")
	}
	if r.UsagePct != nil {
		return nil, Conflict("the run's usage is already recorded")
	}
	if pct < 0 || pct > 100 {
		return nil, Invalid("usage_pct is 0 to 100")
	}
	if _, err := q.ExecContext(ctx, `UPDATE runs SET usage_pct = ?, version = version + 1 WHERE id = ?`, pct, r.ID); err != nil {
		return nil, err
	}
	return GetRun(ctx, q, r.ID)
}

// GetRun loads a run.
func GetRun(ctx context.Context, q Q, id int64) (*Run, error) {
	return scanRun(q.QueryRowContext(ctx, "SELECT "+runCols+" FROM runs r JOIN users u ON u.id = r.worker_user_id WHERE r.id = ?", id))
}

// RunFilter selects runs.
type RunFilter struct {
	WorkspaceIDs []int64 // nil = all (admin)
	Projects     []int64 // nil = all
	GigID        *int64
	Status       string
	StartedSince *int64
	Limit        int
}

// ListRuns returns runs, newest first.
func ListRuns(ctx context.Context, q Q, f RunFilter) ([]*Run, error) {
	where, args := []string{"1=1"}, []any{}
	if f.WorkspaceIDs != nil {
		where, args = append(where, "r.workspace_id IN ("+placeholders(len(f.WorkspaceIDs))+")"), append(args, int64Args(f.WorkspaceIDs)...)
	}
	if f.Projects != nil {
		where = append(where, "r.gig_id IN (SELECT id FROM gigs WHERE project_id IN ("+placeholders(len(f.Projects))+"))")
		args = append(args, int64Args(f.Projects)...)
	}
	if f.GigID != nil {
		where, args = append(where, "r.gig_id = ?"), append(args, *f.GigID)
	}
	if f.Status != "" {
		where, args = append(where, "r.status = ?"), append(args, f.Status)
	}
	if f.StartedSince != nil {
		where, args = append(where, "r.started_at >= ?"), append(args, *f.StartedSince)
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := q.QueryContext(ctx, "SELECT "+runCols+" FROM runs r JOIN users u ON u.id = r.worker_user_id WHERE "+
		strings.Join(where, " AND ")+fmt.Sprintf(" ORDER BY r.id DESC LIMIT %d", limit), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// ValidSessionID reports a lower-case UUID (the only form shown in a resume command).
func ValidSessionID(s string) bool { return uuidRe.MatchString(s) }

// StartRun opens a run record and sets the gig's session id.
func StartRun(ctx context.Context, q Q, a Actor, t *Gig, sessionID *string, mode string, attempt int64, model *string) (*Run, error) {
	if sessionID != nil && !ValidSessionID(*sessionID) {
		return nil, Invalid("session_id must be a lower-case UUID")
	}
	if len(mode) > 32 {
		return nil, Invalid("mode: at most 32 characters")
	}
	if attempt <= 0 {
		attempt = 1
	}
	now := Now()
	res, err := q.ExecContext(ctx, `INSERT INTO runs (workspace_id, gig_id, worker_user_id, key_id, session_id, mode, attempt, status,
		started_at, heartbeat_at, model) VALUES (?,?,?,?,?,?,?,'running',?,?,?)`, t.WorkspaceID, t.ID, a.UserID, nullInt(a.KeyID),
		nullStr(sessionID), mode, attempt, now, now, nullStr(model))
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if _, err := q.ExecContext(ctx, `UPDATE gigs SET last_run_id = ?, session_id = COALESCE(?, session_id), resume_requested_at = NULL,
		updated_at = ?, version = version + 1 WHERE id = ?`, id, nullStr(sessionID), now, t.ID); err != nil {
		return nil, err
	}
	if _, err := Activity(ctx, q, a, &t.WorkspaceID, &t.ProjectID, "run.started", "run", id, map[string]any{"gig_id": t.ID, "mode": mode, "attempt": attempt}); err != nil {
		return nil, err
	}
	return GetRun(ctx, q, id)
}

// RunProgress records a heartbeat and an optional timeline entry.
func RunProgress(ctx context.Context, q Q, r *Run, pct *int64, note string) (*Run, error) {
	if r.Status != "running" {
		return nil, Conflict("the run has ended")
	}
	if pct != nil && (*pct < 0 || *pct > 100) {
		return nil, Invalid("pct is 0-100")
	}
	if len(note) > 2048 {
		return nil, Invalid("note: at most 2 KiB")
	}
	now := Now()
	var noteArg any
	if note != "" {
		noteArg = note
	}
	if _, err := q.ExecContext(ctx, `UPDATE runs SET heartbeat_at = ?, progress_pct = COALESCE(?, progress_pct), progress_note = COALESCE(?, progress_note),
		version = version + 1 WHERE id = ?`, now, nullInt(pct), noteArg, r.ID); err != nil {
		return nil, err
	}
	if note != "" || pct != nil {
		if _, err := q.ExecContext(ctx, `INSERT INTO run_events (run_id, at, kind, text, pct) VALUES (?,?,?,?,?)`, r.ID, now, "progress", note, nullInt(pct)); err != nil {
			return nil, err
		}
	}
	return GetRun(ctx, q, r.ID)
}

// RunProgressEvent logs a run.progress activity entry when the run's percentage or note differ from
// what the last entry announced and that entry is at least `every` old. The entry carries only the
// gig id and a signature of the progress (boards read the values from the gig), so the activity
// table never holds the note's text. Old entries are pruned by housekeeping.
func RunProgressEvent(ctx context.Context, q Q, a Actor, r *Run, every time.Duration) error {
	h := fnv.New32a()
	if r.ProgressNote != nil {
		h.Write([]byte(*r.ProgressNote))
	}
	pct := int64(-1)
	if r.ProgressPct != nil {
		pct = *r.ProgressPct
	}
	sig := fmt.Sprintf("%d:%08x", pct, h.Sum32())
	var at int64
	var last string
	err := q.QueryRowContext(ctx, `SELECT at, COALESCE(json_extract(data, '$.sig'), '') FROM activity
		WHERE entity_type = 'run' AND entity_id = ? AND action = 'run.progress' ORDER BY id DESC LIMIT 1`, r.ID).Scan(&at, &last)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if r.ProgressNote == nil && r.ProgressPct == nil {
			return nil // nothing to show yet
		}
	case err != nil:
		return err
	case last == sig || Now()-at < every.Milliseconds():
		return nil
	}
	t, err := GetGig(ctx, q, r.GigID)
	if err != nil {
		return err
	}
	_, err = Activity(ctx, q, a, &t.WorkspaceID, &t.ProjectID, "run.progress", "run", r.ID, map[string]any{"gig_id": t.ID, "sig": sig})
	return err
}

// progressEventKeep: run.progress entries are only news; housekeeping drops them after this.
const progressEventKeep = 24 * time.Hour

// RunEvent is one timeline entry.
type RunEvent struct {
	ID   int64  `json:"id"`
	At   int64  `json:"at"`
	Kind string `json:"kind"`
	Text string `json:"text"`
	Pct  *int64 `json:"pct"`
}

// RunEvents returns a run's timeline.
func RunEvents(ctx context.Context, q Q, run int64) ([]RunEvent, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, at, kind, text, pct FROM run_events WHERE run_id = ? ORDER BY id`, run)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RunEvent{}
	for rows.Next() {
		var e RunEvent
		var pct sql.NullInt64
		if err := rows.Scan(&e.ID, &e.At, &e.Kind, &e.Text, &pct); err != nil {
			return nil, err
		}
		e.Pct = ptrInt(pct)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Finish is what a worker reports at the end of a run.
type Finish struct {
	Status         string // done | needs_human | error | timeout | interrupted | parked
	ReportMD       string
	CommentKind    string // report (default) | question | comment
	MoveToCategory string
	CostUSD        *float64
	NumTurns       *int64
	DurationMS     *int64
	TokensIn       *int64
	TokensOut      *int64
	TokensCacheR   *int64
	TokensCacheW   *int64
	Model          *string
	SessionID      *string
	TranscriptPath *string
	LogPath        *string
	ExitCode       *int64
	ErrorReason    *string
	CheckAt        *int64 // parked only: the gig's start_at moves here (claims skip it until then)
}

var finishStatuses = map[string]bool{"done": true, "needs_human": true, "error": true, "timeout": true, "interrupted": true, "parked": true}

// FinishRun closes a run with what the worker reports, stores the report, moves the gig if asked.
// The server decides nothing here.
func FinishRun(ctx context.Context, q Q, a Actor, r *Run, f Finish) (*Run, *Gig, error) {
	for name, v := range map[string]*int64{"num_turns": f.NumTurns, "duration_ms": f.DurationMS, "tokens_in": f.TokensIn, "tokens_out": f.TokensOut,
		"tokens_cache_read": f.TokensCacheR, "tokens_cache_write": f.TokensCacheW} {
		if v != nil && *v < 0 {
			return nil, nil, Invalid("%s can't be negative", name)
		}
	}
	if f.CostUSD != nil && (*f.CostUSD < 0 || *f.CostUSD > 1e6) {
		return nil, nil, Invalid("cost_usd: 0 to 1,000,000")
	}
	if r.Status != "running" {
		return nil, nil, Conflict("the run has already ended (%s)", r.Status)
	}
	if !finishStatuses[f.Status] {
		return nil, nil, Invalid("status is done, needs_human, error, timeout, interrupted or parked")
	}
	if f.SessionID != nil && !ValidSessionID(*f.SessionID) {
		return nil, nil, Invalid("session_id must be a lower-case UUID")
	}
	if f.CheckAt != nil && (f.Status != "parked" || *f.CheckAt < Now()-60_000 || *f.CheckAt > Now()+7*86_400_000) {
		return nil, nil, Invalid("check_at goes with status parked, from now to 7 days ahead")
	}
	for _, p := range []*string{f.TranscriptPath, f.LogPath, f.Model, f.ErrorReason} {
		if p != nil && len(*p) > 1024 {
			return nil, nil, Invalid("text fields: at most 1024 characters")
		}
	}
	t, err := GetGig(ctx, q, r.GigID)
	if err != nil {
		return nil, nil, err
	}
	var reportID *int64
	asked := false // a question comment already rang the bell
	if strings.TrimSpace(f.ReportMD) != "" {
		kind := f.CommentKind
		if kind == "" {
			kind = "report"
		}
		if !clientCommentKinds[kind] {
			return nil, nil, Invalid("comment_kind is report, question or comment")
		}
		c, err := AddComment(ctx, q, a, t, kind, f.ReportMD, &r.ID)
		if err != nil {
			return nil, nil, err
		}
		reportID = &c.ID
		asked = kind == "question"
	}
	// a run that needs a person (or ended in an error or a timeout) rings the watchers even when the
	// worker sent no question with it
	if !asked && (f.Status == "needs_human" || f.Status == "error" || f.Status == "timeout") {
		nk := "failed"
		if f.Status == "needs_human" {
			nk = "question"
		}
		if err := NotifyWatchers(ctx, q, a, t, reportID, nk); err != nil {
			return nil, nil, err
		}
	}
	now := Now()
	dur := f.DurationMS
	if dur == nil {
		d := now - r.StartedAt
		dur = &d
	}
	// only a run that is still running (two finishes at once, or a finish racing the stale-run
	// janitor: the second one changes nothing and its report comment is rolled back with it)
	res, err := q.ExecContext(ctx, `UPDATE runs SET status = ?, ended_at = ?, heartbeat_at = ?, duration_ms = ?, cost_usd = ?, num_turns = ?,
		tokens_in = ?, tokens_out = ?, tokens_cache_read = ?, tokens_cache_write = ?, model = COALESCE(?, model), session_id = COALESCE(?, session_id),
		transcript_path = ?, log_path = ?, exit_code = ?, error_reason = ?, report_comment_id = ?, version = version + 1 WHERE id = ? AND status = 'running'`,
		f.Status, now, now, nullInt(dur), nullF(f.CostUSD), nullInt(f.NumTurns), nullInt(f.TokensIn), nullInt(f.TokensOut),
		nullInt(f.TokensCacheR), nullInt(f.TokensCacheW), nullStr(f.Model), nullStr(f.SessionID), nullStr(f.TranscriptPath),
		nullStr(f.LogPath), nullInt(f.ExitCode), nullStr(f.ErrorReason), nullInt(reportID), r.ID)
	if err != nil {
		return nil, nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, nil, Conflict("the run has already ended")
	}
	if err := touchProgress(ctx, q, t.ID); err != nil {
		return nil, nil, err
	}
	if f.CostUSD != nil {
		if _, err := q.ExecContext(ctx, `UPDATE gigs SET cost_usd_total = cost_usd_total + ? WHERE id = ?`, *f.CostUSD, t.ID); err != nil {
			return nil, nil, err
		}
	}
	if f.SessionID != nil {
		if _, err := q.ExecContext(ctx, `UPDATE gigs SET session_id = ? WHERE id = ?`, *f.SessionID, t.ID); err != nil {
			return nil, nil, err
		}
	}
	if f.CheckAt != nil {
		if _, err := q.ExecContext(ctx, `UPDATE gigs SET start_at = ?, version = version + 1, updated_at = ? WHERE id = ?`, *f.CheckAt, Now(), t.ID); err != nil {
			return nil, nil, err
		}
	}
	if _, err := Activity(ctx, q, a, &t.WorkspaceID, &t.ProjectID, "run.finished", "run", r.ID, map[string]any{"gig_id": t.ID, "status": f.Status}); err != nil {
		return nil, nil, err
	}
	t, err = GetGig(ctx, q, t.ID)
	if err != nil {
		return nil, nil, err
	}
	if f.MoveToCategory != "" {
		if t, err = MoveGig(ctx, q, a, t, 0, Move{Category: f.MoveToCategory}); err != nil {
			return nil, nil, err
		}
	}
	r, err = GetRun(ctx, q, r.ID)
	return r, t, err
}

func nullF(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}

// Claim picks the next gig for a worker and moves it, in one transaction.
type Claim struct {
	FromCategory string
	ToCategory   string
	Projects     []int64 // nil = all the caller may see
	CreatedBy    []int64 // nil = anyone
	StartDue     bool    // skip gigs whose start date is in the future
	AlwaysOnly   bool    // only always-run gigs (the usage budget says no, or it's daytime)
	DryRun       bool
}

// ClaimPlace is one workspace a claim looks in, and the projects of it the claimer may work in.
type ClaimPlace struct {
	WorkspaceID int64
	Projects    []int64 // nil = every project of the workspace
}

// ClaimGig returns the claimed gig from one workspace, or ErrNotFound when nothing matches (ErrPaused
// when its runs are paused). Only gigs assigned to the caller are handed out.
func ClaimGig(ctx context.Context, q Q, a Actor, ws int64, c Claim) (*Gig, error) {
	return ClaimGigIn(ctx, q, a, []ClaimPlace{{WorkspaceID: ws, Projects: c.Projects}}, c)
}

// ClaimGigIn claims one gig from any of the places, each under its own workspace's runner limits
// (pause, caps). One order across all of them: always-run first, then priority, due date, the
// workspaces the claimer owns before the others, then the sidebar order of projects, statuses and
// cards. ErrPaused only when every place is paused; ErrNotFound when nothing matches. Only gigs
// assigned to the caller are handed out (c.Projects is ignored: each place has its own).
func ClaimGigIn(ctx context.Context, q Q, a Actor, places []ClaimPlace, c Claim) (*Gig, error) {
	if c.FromCategory == "" {
		c.FromCategory = "todo"
	}
	if c.ToCategory == "" {
		c.ToCategory = "doing"
	}
	if !ValidCategory(c.FromCategory) || !ValidCategory(c.ToCategory) {
		return nil, Invalid("unknown status category")
	}
	where := []string{"t.deleted_at IS NULL", "p.deleted_at IS NULL", "p.archived_at IS NULL", "s.category = ?"}
	args := []any{c.FromCategory}
	if c.CreatedBy != nil {
		where, args = append(where, "t.created_by IN ("+placeholders(len(c.CreatedBy))+")"), append(args, int64Args(c.CreatedBy)...)
	}
	// only gigs assigned to the claimer: a workspace may hold gigs for someone else
	where, args = append(where, "EXISTS (SELECT 1 FROM gig_assignees ga WHERE ga.gig_id = t.id AND ga.user_id = ?)"), append(args, a.UserID)
	if c.StartDue {
		where, args = append(where, "(t.start_at IS NULL OR t.start_at <= ?)"), append(args, Now())
	}
	// A windowed gig is handed out only inside its window: not before it opens (even when the caller
	// didn't ask for StartDue), never after it closes, and never once missed (that waits for a human).
	now := Now()
	where = append(where, "(t.not_after IS NULL OR ((t.start_at IS NULL OR t.start_at <= ?) AND t.not_after > ?))", "t.missed_at IS NULL")
	// archived gigs and gigs with an open blocker (a dependency, or a subgig that blocks its parent)
	// are never handed out
	where = append(where, "t.archived_at IS NULL", "NOT "+blockedCond)
	args = append(args, now, now)
	// each place: its workspace, its projects, and that workspace's runner limits
	var alts []string
	paused := 0
	for _, pl := range places {
		if pl.Projects != nil && len(pl.Projects) == 0 {
			continue
		}
		lw, la, err := claimLimits(ctx, q, pl.WorkspaceID, c.AlwaysOnly)
		if errors.Is(err, ErrPaused) {
			paused++
			continue
		}
		if err != nil {
			return nil, err
		}
		cond, cargs := []string{"t.workspace_id = ?"}, []any{pl.WorkspaceID}
		if pl.Projects != nil {
			cond, cargs = append(cond, "t.project_id IN ("+placeholders(len(pl.Projects))+")"), append(cargs, int64Args(pl.Projects)...)
		}
		cond, cargs = append(cond, lw...), append(cargs, la...)
		alts, args = append(alts, "("+strings.Join(cond, " AND ")+")"), append(args, cargs...)
	}
	if len(alts) == 0 {
		if paused > 0 {
			return nil, ErrPaused
		}
		return nil, ErrNotFound
	}
	where = append(where, "("+strings.Join(alts, " OR ")+")")
	args = append(args, a.UserID)
	t, err := scanGig(q.QueryRowContext(ctx, "SELECT "+gigCols+gigFrom+" WHERE "+strings.Join(where, " AND ")+
		" ORDER BY t.always_run DESC, t.priority DESC, t.due_at IS NULL, t.due_at,"+
		" COALESCE((SELECT owner_id FROM workspaces WHERE id = t.workspace_id), 0) = ? DESC, t.workspace_id,"+
		" p.position, s.position, t.position, t.id LIMIT 1", args...))
	if err != nil {
		return nil, err
	}
	if err := fill(ctx, q, []*Gig{t}); err != nil {
		return nil, err
	}
	if c.DryRun {
		return t, nil
	}
	return MoveGig(ctx, q, a, t, 0, Move{Category: c.ToCategory})
}

// ResumeGig records a resume request: a comment (optional), the flag, and a move to todo.
func ResumeGig(ctx context.Context, q Q, a Actor, t *Gig, comment string) (*Gig, error) {
	if t.Deleted {
		return nil, Conflict("the gig is in the trash")
	}
	if t.ArchivedAt != nil {
		return nil, Conflict("the gig is archived; unarchive it first")
	}
	if strings.TrimSpace(comment) != "" {
		if _, err := AddComment(ctx, q, a, t, "resume_request", comment, nil); err != nil {
			return nil, err
		}
	}
	if _, err := q.ExecContext(ctx, `UPDATE gigs SET resume_requested_at = ?, version = version + 1 WHERE id = ?`, Now(), t.ID); err != nil {
		return nil, err
	}
	if _, err := Activity(ctx, q, a, &t.WorkspaceID, &t.ProjectID, "gig.resume_requested", "gig", t.ID, nil); err != nil {
		return nil, err
	}
	t, err := GetGig(ctx, q, t.ID)
	if err != nil {
		return nil, err
	}
	if t.StatusCategory == "todo" {
		return t, nil
	}
	return MoveGig(ctx, q, a, t, 0, Move{Category: "todo"})
}

// Stats are counts for a workspace since a time.
type Stats struct {
	Since       int64   `json:"since"`
	RunsStarted int64   `json:"runs_started"`
	RunsRunning int64   `json:"runs_running"`
	CostUSD     float64 `json:"cost_usd"`
	GigsCreated int64   `json:"gigs_created"`
	GigsDone    int64   `json:"gigs_done"`
}

// WorkspaceStats counts runs, cost and gigs since a time (projects nil = all).
func WorkspaceStats(ctx context.Context, q Q, ws int64, projects []int64, since int64) (*Stats, error) {
	s := &Stats{Since: since}
	pf, args := "", []any{ws, since}
	if projects != nil {
		pf = " AND gig_id IN (SELECT id FROM gigs WHERE project_id IN (" + placeholders(len(projects)) + "))"
		args = append(args, int64Args(projects)...)
	}
	if err := q.QueryRowContext(ctx, `SELECT count(*), COALESCE(SUM(cost_usd), 0) FROM runs WHERE workspace_id = ? AND started_at >= ?`+pf, args...).Scan(&s.RunsStarted, &s.CostUSD); err != nil {
		return nil, err
	}
	args2 := []any{ws}
	if projects != nil {
		args2 = append(args2, int64Args(projects)...)
	}
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM runs WHERE workspace_id = ? AND status = 'running'`+pf, args2...).Scan(&s.RunsRunning); err != nil {
		return nil, err
	}
	tf, targs := "", []any{ws, since}
	if projects != nil {
		tf = " AND project_id IN (" + placeholders(len(projects)) + ")"
		targs = append(targs, int64Args(projects)...)
	}
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM gigs WHERE workspace_id = ? AND created_at >= ? AND deleted_at IS NULL`+tf, targs...).Scan(&s.GigsCreated); err != nil {
		return nil, err
	}
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM gigs WHERE workspace_id = ? AND done_at >= ? AND deleted_at IS NULL`+tf, targs...).Scan(&s.GigsDone); err != nil {
		return nil, err
	}
	return s, nil
}

// Summary renders a plain-text board summary: per project, each status with up to perStatus gigs.
// Data lines only; done and cancelled show a count.
func Summary(ctx context.Context, q Q, ws int64, projects []*Project, perStatus int) (string, error) {
	if perStatus <= 0 {
		perStatus = 5
	}
	var b strings.Builder
	for _, p := range projects {
		if p.Archived {
			continue
		}
		fmt.Fprintf(&b, "%s (%s):\n", p.Name, p.Key)
		sts, err := Statuses(ctx, q, p.ID)
		if err != nil {
			return "", err
		}
		for _, s := range sts {
			id := s.ID
			ts, more, err := ListGigs(ctx, q, GigFilter{WorkspaceID: ws, Projects: []int64{p.ID}, StatusID: &id, Limit: perStatus})
			if err != nil {
				return "", err
			}
			var n int
			_ = q.QueryRowContext(ctx, `SELECT count(*) FROM gigs WHERE status_id = ? AND deleted_at IS NULL`, s.ID).Scan(&n)
			if s.Category == "done" || s.Category == "cancelled" {
				fmt.Fprintf(&b, "- %s: %d gig(s)\n", s.Name, n)
				continue
			}
			var items []string
			for _, t := range ts {
				item := fmt.Sprintf("%s %s", t.Key, oneLine(t.Title, 80))
				if t.Priority > 0 {
					item += fmt.Sprintf(" [P%d]", t.Priority)
				}
				if t.DueAt != nil {
					item += " due " + fmtDate(*t.DueAt)
				}
				item += " (by " + t.CreatedByUsername + ")"
				items = append(items, item)
			}
			line := "-"
			if len(items) > 0 {
				line = strings.Join(items, "; ")
				if more {
					line += "; …"
				}
			}
			fmt.Fprintf(&b, "- %s (%d): %s\n", s.Name, n, line)
		}
	}
	return b.String(), nil
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}

func fmtDate(ms int64) string {
	return timeFromMS(ms).Format("2006-01-02")
}
