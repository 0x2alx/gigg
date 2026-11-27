package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Gig is a card on a project's board.
type Gig struct {
	ID                int64          `json:"id"`
	Key               string         `json:"key"` // e.g. GIG-12
	WorkspaceID       int64          `json:"workspace_id"`
	ProjectID         int64          `json:"project_id"`
	Number            int64          `json:"number"`
	Title             string         `json:"title"`
	DescriptionMD     string         `json:"description_md"`
	StatusID          int64          `json:"status_id"`
	StatusName        string         `json:"status_name"`
	StatusCategory    string         `json:"status_category"`
	Position          float64        `json:"position"`
	Priority          int64          `json:"priority"`
	Color             *string        `json:"color"`
	StartAt           *int64         `json:"start_at"`
	DueAt             *int64         `json:"due_at"`
	DoneAt            *int64         `json:"done_at"`
	ParentID          *int64         `json:"parent_id"`
	ClonedFromID      *int64         `json:"cloned_from_id"`
	CreatedBy         int64          `json:"created_by"`
	CreatedByUsername string         `json:"created_by_username"`
	CreatedByAdmin    bool           `json:"created_by_admin"`
	CreatedViaKeyID   *int64         `json:"created_via_key_id"`
	CreatedAt         int64          `json:"created_at"`
	UpdatedAt         int64          `json:"updated_at"`
	SessionID         *string        `json:"session_id"`
	LastRunID         *int64         `json:"last_run_id"`
	ResumeRequestedAt *int64         `json:"resume_requested_at"`
	CostUSDTotal      float64        `json:"cost_usd_total"`
	CommentCount      int64          `json:"comment_count"`
	AttachmentCount   int64          `json:"attachment_count" doc:"Files, links and commits attached"`
	LastProgressAt    int64          `json:"last_progress_at" doc:"Unix ms of the latest comment, checklist tick or untick, status change or finished run (the creation time when none)"`
	RecurrenceID      *int64         `json:"recurrence_id"`
	OccurrenceAt      *int64         `json:"occurrence_at"`
	NotAfter          *int64         `json:"not_after"` // the window closes: not claimable after this
	MissedAt          *int64         `json:"missed_at"`
	ArchivedAt        *int64         `json:"archived_at"`
	RunTimeout        *int64         `json:"run_timeout_minutes" doc:"How long a runner lets one run go, in minutes (null: the agent's default)"`
	AutoArchive       *int64         `json:"auto_archive_minutes" doc:"Archive it this many minutes after it is done or cancelled (null: never)"`
	Schedule          *GigSchedule   `json:"schedule,omitempty" doc:"Gigs made by a recurring gig: its schedule, named and in words"`
	SubgigCount       int64          `json:"subgig_count"`
	SubgigDone        int64          `json:"subgig_done"`
	ChecklistTotal    int64          `json:"checklist_total"`
	ChecklistDone     int64          `json:"checklist_done"`
	BlockedBy         int64          `json:"blocked_by" doc:"Open gigs blocking this one: its blockers (dependencies) and its open subgigs marked blocks_parent"`
	BlockedBySubgigs  int64          `json:"blocked_by_subgigs" doc:"How many of blocked_by are its own subgigs (blocks_parent)"`
	BlocksParent      bool           `json:"blocks_parent" doc:"A subgig that holds its parent back while it is open"`
	Custom            map[string]any `json:"custom" doc:"Custom field values by field id"`
	Deleted           bool           `json:"deleted"`
	Labels            []GigTag       `json:"labels"`
	Assignees         []int64        `json:"assignees"`
	Followers         []int64        `json:"followers" doc:"Users who follow it: they get its bells (reports, questions, failed runs, missed windows) as its assignees do"`
	Aliases           []string       `json:"aliases"`
	Model             string         `json:"model" doc:"Model a runner should ask for ('' = the agent's default)"`
	Effort            string         `json:"effort" doc:"Effort level a runner should ask for ('' = the agent's default)"`
	AlwaysRun         bool           `json:"always_run" doc:"Worked whatever the usage budget, day or night (within the gig cap and pauses)"`
	AllowedTools      []string       `json:"allowed_tools" doc:"The only tools a runner should allow (permission rules); empty = the runner's usual mode"`
	Version           int64          `json:"version"`
	LastRun           *RunBrief      `json:"last_run,omitempty" doc:"The gig's latest run, in brief (absent when it has none)"`
	Question          *QuestionBrief `json:"question,omitempty" doc:"Waiting gigs in lists and on the board, with the comments:read scope: the latest question or report nobody else has answered, in brief"`
}

// RunBrief is a gig's latest run as a card shows it.
type RunBrief struct {
	ID           int64   `json:"id"`
	Status       string  `json:"status"`
	WorkerUserID int64   `json:"worker_user_id"`
	StartedAt    int64   `json:"started_at"`
	HeartbeatAt  int64   `json:"heartbeat_at"`
	EndedAt      *int64  `json:"ended_at"`
	Model        *string `json:"model"`
	ProgressPct  *int64  `json:"progress_pct"`
	ProgressNote *string `json:"progress_note"`
	ErrorReason  *string `json:"error_reason"`
}

// QuestionBrief is the latest question on a waiting gig, cut to a card's length.
type QuestionBrief struct {
	CommentID int64  `json:"comment_id"`
	AuthorID  int64  `json:"author_id"`
	CreatedAt int64  `json:"created_at"`
	Kind      string `json:"kind" doc:"question or report"`
	Text      string `json:"text" doc:"The comment's Markdown, at most 280 characters"`
}

const questionBriefLen = 280

// GigSchedule is the schedule of the recurring gig that made a gig.
type GigSchedule struct {
	Name string `json:"name"`
	Text string `json:"text" doc:"The rule in words, e.g. Every weekday 09:00–11:00"`
}

// GigTag is a label as shown on a gig.
type GigTag struct {
	ID    int64   `json:"id"`
	Name  string  `json:"name"`
	Color *string `json:"color"`
}

const gigCols = `t.id, p.key || '-' || t.number, t.workspace_id, t.project_id, t.number, t.title, t.description_md, t.status_id,
	s.name, s.category, t.position, t.priority, t.color, t.start_at, t.due_at, t.done_at, t.parent_id, t.cloned_from_id,
	t.created_by, u.username, u.is_admin, t.created_via_key_id, t.created_at, t.updated_at, t.session_id, t.last_run_id,
	t.resume_requested_at, t.cost_usd_total, t.comment_count, t.deleted_at IS NOT NULL, t.version,
	t.recurrence_id, t.occurrence_at, t.not_after, t.missed_at, t.archived_at,
	(SELECT count(*) FROM gigs c WHERE c.parent_id = t.id AND c.deleted_at IS NULL),
	(SELECT count(*) FROM gigs c JOIN statuses cs ON cs.id = c.status_id WHERE c.parent_id = t.id AND c.deleted_at IS NULL AND cs.category IN ('done','cancelled')),
	(SELECT count(*) FROM checklist_items ci WHERE ci.gig_id = t.id),
	(SELECT count(*) FROM checklist_items ci WHERE ci.gig_id = t.id AND ci.done_at IS NOT NULL),
	(SELECT count(*) FROM gig_relations r JOIN gigs b ON b.id = r.from_gig_id JOIN statuses bs ON bs.id = b.status_id
		WHERE r.to_gig_id = t.id AND r.kind = 'blocks' AND b.deleted_at IS NULL AND b.archived_at IS NULL AND bs.category NOT IN ('done','cancelled')),
	` + blockingSubgigs + `,
	t.model, t.effort, t.always_run, t.allowed_tools, t.blocks_parent, t.auto_archive_minutes, t.run_timeout_minutes,
	(SELECT count(*) FROM attachments at WHERE at.gig_id = t.id), coalesce(t.last_progress_at, t.created_at)`

// blockingSubgigs counts t's open subgigs that hold it back (blocks_parent; not archived ones,
// which agents never get either), except those that
// also have a "blocks" link to it (already counted with the dependencies).
const blockingSubgigs = `(SELECT count(*) FROM gigs c JOIN statuses cs ON cs.id = c.status_id
		WHERE c.parent_id = t.id AND c.blocks_parent = 1 AND c.deleted_at IS NULL AND c.archived_at IS NULL AND cs.category NOT IN ('done','cancelled')
		AND NOT EXISTS (SELECT 1 FROM gig_relations r WHERE r.from_gig_id = c.id AND r.to_gig_id = t.id AND r.kind = 'blocks'))`

// blockedCond: t has an open blocker, a "blocks" dependency or a subgig that blocks its parent.
// Archived blockers don't count (as with subgigs): archiving parks a gig, so it would block forever.
const blockedCond = `(EXISTS (SELECT 1 FROM gig_relations r JOIN gigs b ON b.id = r.from_gig_id JOIN statuses bs ON bs.id = b.status_id
		WHERE r.to_gig_id = t.id AND r.kind = 'blocks' AND b.deleted_at IS NULL AND b.archived_at IS NULL AND bs.category NOT IN ('done','cancelled'))
	OR EXISTS (SELECT 1 FROM gigs c JOIN statuses cs ON cs.id = c.status_id
		WHERE c.parent_id = t.id AND c.blocks_parent = 1 AND c.deleted_at IS NULL AND c.archived_at IS NULL AND cs.category NOT IN ('done','cancelled')))`

const gigFrom = ` FROM gigs t JOIN projects p ON p.id = t.project_id JOIN statuses s ON s.id = t.status_id JOIN users u ON u.id = t.created_by`

func scanGig(row interface{ Scan(...any) error }) (*Gig, error) {
	var t Gig
	var color, sess sql.NullString
	var start, due, done, parent, cloned, viaKey, lastRun, resume, rec, occ, notAfter, missed, archived, autoArch, runTO sql.NullInt64
	var tools string
	if err := row.Scan(&t.ID, &t.Key, &t.WorkspaceID, &t.ProjectID, &t.Number, &t.Title, &t.DescriptionMD, &t.StatusID,
		&t.StatusName, &t.StatusCategory, &t.Position, &t.Priority, &color, &start, &due, &done, &parent, &cloned,
		&t.CreatedBy, &t.CreatedByUsername, &t.CreatedByAdmin, &viaKey, &t.CreatedAt, &t.UpdatedAt, &sess, &lastRun,
		&resume, &t.CostUSDTotal, &t.CommentCount, &t.Deleted, &t.Version, &rec, &occ, &notAfter, &missed, &archived,
		&t.SubgigCount, &t.SubgigDone, &t.ChecklistTotal, &t.ChecklistDone, &t.BlockedBy, &t.BlockedBySubgigs, &t.Model, &t.Effort, &t.AlwaysRun, &tools, &t.BlocksParent, &autoArch, &runTO, &t.AttachmentCount, &t.LastProgressAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	t.Color, t.SessionID = ptrStr(color), ptrStr(sess)
	t.StartAt, t.DueAt, t.DoneAt, t.ParentID, t.ClonedFromID = ptrInt(start), ptrInt(due), ptrInt(done), ptrInt(parent), ptrInt(cloned)
	t.CreatedViaKeyID, t.LastRunID, t.ResumeRequestedAt = ptrInt(viaKey), ptrInt(lastRun), ptrInt(resume)
	t.RecurrenceID, t.OccurrenceAt, t.NotAfter, t.MissedAt = ptrInt(rec), ptrInt(occ), ptrInt(notAfter), ptrInt(missed)
	t.ArchivedAt, t.AutoArchive, t.RunTimeout = ptrInt(archived), ptrInt(autoArch), ptrInt(runTO)
	t.Labels, t.Assignees, t.Followers, t.Aliases, t.Custom = []GigTag{}, []int64{}, []int64{}, []string{}, map[string]any{}
	t.AllowedTools = []string{}
	_ = json.Unmarshal([]byte(tools), &t.AllowedTools)
	t.BlockedBy += t.BlockedBySubgigs
	return &t, nil
}

// fill loads labels, assignees and aliases for a batch of gigs.
func fill(ctx context.Context, q Q, ts []*Gig) error {
	if len(ts) == 0 {
		return nil
	}
	byID := map[int64]*Gig{}
	ids := make([]int64, 0, len(ts))
	for _, t := range ts {
		byID[t.ID] = t
		ids = append(ids, t.ID)
	}
	in := placeholders(len(ids))
	rows, err := q.QueryContext(ctx, `SELECT tl.gig_id, l.id, l.name, l.color FROM gig_labels tl JOIN labels l ON l.id = tl.label_id
		WHERE tl.gig_id IN (`+in+`) ORDER BY l.name`, int64Args(ids)...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var tid int64
		var tag GigTag
		var color sql.NullString
		if err := rows.Scan(&tid, &tag.ID, &tag.Name, &color); err != nil {
			rows.Close()
			return err
		}
		tag.Color = ptrStr(color)
		byID[tid].Labels = append(byID[tid].Labels, tag)
	}
	rows.Close()
	rows, err = q.QueryContext(ctx, `SELECT gig_id, user_id FROM gig_assignees WHERE gig_id IN (`+in+`) ORDER BY user_id`, int64Args(ids)...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var tid, uid int64
		if err := rows.Scan(&tid, &uid); err != nil {
			rows.Close()
			return err
		}
		byID[tid].Assignees = append(byID[tid].Assignees, uid)
	}
	rows.Close()
	rows, err = q.QueryContext(ctx, `SELECT gig_id, user_id FROM gig_followers WHERE gig_id IN (`+in+`) ORDER BY user_id`, int64Args(ids)...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var tid, uid int64
		if err := rows.Scan(&tid, &uid); err != nil {
			rows.Close()
			return err
		}
		byID[tid].Followers = append(byID[tid].Followers, uid)
	}
	rows.Close()
	rows, err = q.QueryContext(ctx, `SELECT v.gig_id, v.field_id, v.value FROM gig_custom_values v JOIN custom_fields f ON f.id = v.field_id
		WHERE f.deleted_at IS NULL AND v.gig_id IN (`+in+`)`, int64Args(ids)...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var tid, fid int64
		var raw string
		if err := rows.Scan(&tid, &fid, &raw); err != nil {
			rows.Close()
			return err
		}
		var v any
		_ = json.Unmarshal([]byte(raw), &v)
		byID[tid].Custom[strconv.FormatInt(fid, 10)] = v
	}
	rows.Close()
	if err := fillRuns(ctx, q, ts); err != nil {
		return err
	}
	if err := fillSchedules(ctx, q, ts); err != nil {
		return err
	}
	rows, err = q.QueryContext(ctx, `SELECT gig_id, project_key || '-' || number FROM gig_aliases WHERE gig_id IN (`+in+`)`, int64Args(ids)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var tid int64
		var k string
		if err := rows.Scan(&tid, &k); err != nil {
			return err
		}
		byID[tid].Aliases = append(byID[tid].Aliases, k)
	}
	return rows.Err()
}

// GetGig loads a gig by id (including trashed ones).
func GetGig(ctx context.Context, q Q, id int64) (*Gig, error) {
	t, err := scanGig(q.QueryRowContext(ctx, "SELECT "+gigCols+gigFrom+" WHERE t.id = ?", id))
	if err != nil {
		return nil, err
	}
	return t, fill(ctx, q, []*Gig{t})
}

var gigKeyRe = regexp.MustCompile(`^([A-Z][A-Z0-9]{1,4})-([1-9][0-9]{0,9})$`)

// ParseGigKey splits "GIG-12".
func ParseGigKey(k string) (string, int64, bool) {
	m := gigKeyRe.FindStringSubmatch(k)
	if m == nil {
		return "", 0, false
	}
	n, _ := strconv.ParseInt(m[2], 10, 64)
	return m[1], n, true
}

// GigIDsByKey returns the ids of gigs whose current key or alias is key, within the given workspaces
// (nil = all). More than one id means the key is ambiguous across workspaces. Gigs of a deleted project
// don't count: its key may belong to a new project now.
func GigIDsByKey(ctx context.Context, q Q, key string, workspaces []int64) ([]int64, error) {
	pk, n, ok := ParseGigKey(key)
	if !ok {
		return nil, ErrNotFound
	}
	query := `SELECT t.id, t.workspace_id FROM gigs t JOIN projects p ON p.id = t.project_id WHERE p.key = ? AND t.number = ? AND p.deleted_at IS NULL
		UNION SELECT a.gig_id, a.workspace_id FROM gig_aliases a JOIN gigs t ON t.id = a.gig_id JOIN projects p ON p.id = t.project_id
			WHERE a.project_key = ? AND a.number = ? AND p.deleted_at IS NULL`
	rows, err := q.QueryContext(ctx, query, pk, n, pk, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	allowed := map[int64]bool{}
	for _, w := range workspaces {
		allowed[w] = true
	}
	var out []int64
	for rows.Next() {
		var id, ws int64
		if err := rows.Scan(&id, &ws); err != nil {
			return nil, err
		}
		if workspaces == nil || allowed[ws] {
			out = append(out, id)
		}
	}
	return out, rows.Err()
}

// GigFilter selects gigs in one workspace.
type GigFilter struct {
	WorkspaceID    int64
	Projects       []int64 // nil = all projects of the workspace
	StatusID       *int64
	Category       string
	Label          string
	PriorityMin    *int64
	Assignee       *int64
	CreatedBy      *int64
	Q              string
	DueBefore      *int64
	UpdatedSince   *int64
	IncludeDeleted bool
	OnlyDeleted    bool
	Archived       string // "" = hide archived, "only", "include"
	ParentID       *int64 // subgigs of this gig
	BlockedOnly    bool   // only gigs with an open blocker
	Recurring      string // "" = all, "hide" = no gig a recurring gig made, "only" = only those
	AlwaysRun      string // "" = all, "hide" = no always-run gig, "only" = only those
	SearchComments bool   // q also matches comment text (only for callers who may read comments)
	Sort           string // board (default), -priority, due, created, -updated, ...
	Limit          int
	AfterID        int64 // cursor: gigs are paged by (sort key, id); for simplicity cursor = offset
	Offset         int
}

// ListGigs returns gigs matching f.
// fillSchedules names the schedule behind each gig a recurring gig made (the recurring gig's current
// schedule; none once the recurring gig is deleted).
func fillSchedules(ctx context.Context, q Q, ts []*Gig) error {
	byRec := map[int64][]*Gig{}
	recs := []int64{}
	for _, t := range ts {
		if t.RecurrenceID != nil {
			if _, ok := byRec[*t.RecurrenceID]; !ok {
				recs = append(recs, *t.RecurrenceID)
			}
			byRec[*t.RecurrenceID] = append(byRec[*t.RecurrenceID], t)
		}
	}
	if len(recs) == 0 {
		return nil
	}
	rows, err := q.QueryContext(ctx, `SELECT r.id, s.name, s.freq, s.interval_n, s.weekdays, s.month_day, s.tz, s.window_start, s.window_minutes, s.lead_minutes
		FROM recurrences r JOIN schedules s ON s.id = r.schedule_id WHERE r.deleted_at IS NULL AND r.id IN (`+placeholders(len(recs))+`)`, int64Args(recs)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name, weekdays string
		var md sql.NullInt64
		var r Rule
		if err := rows.Scan(&id, &name, &r.Freq, &r.Interval, &weekdays, &md, &r.TZ, &r.WindowStart, &r.WindowMinutes, &r.LeadMinutes); err != nil {
			return err
		}
		r.MonthDay = ptrInt(md)
		_ = json.Unmarshal([]byte(weekdays), &r.Weekdays)
		gs := &GigSchedule{Name: name, Text: r.Describe()}
		for _, t := range byRec[id] {
			t.Schedule = gs
		}
	}
	return rows.Err()
}

// fillRuns attaches each gig's latest run, in brief.
func fillRuns(ctx context.Context, q Q, ts []*Gig) error {
	byRun := map[int64]*Gig{}
	runs := []int64{}
	for _, t := range ts {
		if t.LastRunID != nil {
			byRun[*t.LastRunID] = t
			runs = append(runs, *t.LastRunID)
		}
	}
	if len(runs) == 0 {
		return nil
	}
	rows, err := q.QueryContext(ctx, `SELECT id, status, worker_user_id, started_at, heartbeat_at, ended_at, model, progress_pct, progress_note, error_reason
		FROM runs WHERE id IN (`+placeholders(len(runs))+`)`, int64Args(runs)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var r RunBrief
		var ended, pct sql.NullInt64
		var model, note, reason sql.NullString
		if err := rows.Scan(&r.ID, &r.Status, &r.WorkerUserID, &r.StartedAt, &r.HeartbeatAt, &ended, &model, &pct, &note, &reason); err != nil {
			return err
		}
		r.EndedAt, r.ProgressPct = ptrInt(ended), ptrInt(pct)
		r.Model, r.ProgressNote, r.ErrorReason = ptrStr(model), ptrStr(note), ptrStr(reason)
		if t := byRun[r.ID]; t != nil {
			t.LastRun = &r
		}
	}
	return rows.Err()
}

// AttachQuestions sets Question on the waiting gigs: their latest question or report comment, cut
// short, until someone other than its author comments after it (an answer, a resume request). Callers
// check the comments:read scope first.
func AttachQuestions(ctx context.Context, q Q, ts []*Gig) error {
	byID := map[int64]*Gig{}
	ids := []int64{}
	for _, t := range ts {
		if t.StatusCategory == "waiting" {
			byID[t.ID] = t
			ids = append(ids, t.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	// per gig, the newest question/report through the (gig_id, id) index, newest first, stopping at the first
	vals := strings.TrimSuffix(strings.Repeat("(CAST(? AS BIGINT)),", len(ids)), ",") // typed: PostgreSQL would take text
	args := append(int64Args(ids), questionBriefLen+1)
	rows, err := q.QueryContext(ctx, `WITH w(tid) AS (VALUES `+vals+`),
		qs AS (SELECT (SELECT x.id FROM comments x WHERE x.gig_id = w.tid AND x.kind IN ('question', 'report') AND x.deleted_at IS NULL
			ORDER BY x.id DESC LIMIT 1) AS id FROM w)
		SELECT c.id, c.gig_id, c.author_id, c.created_at, c.kind, substr(c.body_md, 1, ?) FROM comments c JOIN qs ON qs.id = c.id
		WHERE NOT EXISTS (SELECT 1 FROM comments y WHERE y.gig_id = c.gig_id AND y.id > c.id AND y.author_id != c.author_id
			AND y.kind != 'system' AND y.deleted_at IS NULL)`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var qb QuestionBrief
		var tid int64
		if err := rows.Scan(&qb.CommentID, &tid, &qb.AuthorID, &qb.CreatedAt, &qb.Kind, &qb.Text); err != nil {
			return err
		}
		if r := []rune(qb.Text); len(r) > questionBriefLen {
			qb.Text = string(r[:questionBriefLen-1]) + "…"
		}
		if t := byID[tid]; t != nil {
			t.Question = &qb
		}
	}
	return rows.Err()
}

func ListGigs(ctx context.Context, q Q, f GigFilter) ([]*Gig, bool, error) {
	where := []string{"t.workspace_id = ?"}
	args := []any{f.WorkspaceID}
	if f.Projects != nil {
		where = append(where, "t.project_id IN ("+placeholders(len(f.Projects))+")")
		args = append(args, int64Args(f.Projects)...)
	}
	switch {
	case f.OnlyDeleted: // the trash: not gigs of a deleted project (they can't come back)
		where = append(where, "t.deleted_at IS NOT NULL", "p.deleted_at IS NULL")
	case !f.IncludeDeleted:
		where = append(where, "t.deleted_at IS NULL", "p.deleted_at IS NULL")
	}
	switch {
	case f.OnlyDeleted: // the trash shows a trashed gig whether or not it was archived first
	case f.Archived == "only":
		where = append(where, "t.archived_at IS NOT NULL")
	case f.Archived == "include":
	default:
		where = append(where, "t.archived_at IS NULL")
	}
	if f.ParentID != nil {
		where, args = append(where, "t.parent_id = ?"), append(args, *f.ParentID)
	}
	if f.BlockedOnly {
		where = append(where, blockedCond)
	}
	switch f.Recurring {
	case "hide":
		where = append(where, "t.recurrence_id IS NULL")
	case "only":
		where = append(where, "t.recurrence_id IS NOT NULL")
	}
	switch f.AlwaysRun {
	case "hide":
		where = append(where, "t.always_run = 0")
	case "only":
		where = append(where, "t.always_run = 1")
	}
	if f.StatusID != nil {
		where, args = append(where, "t.status_id = ?"), append(args, *f.StatusID)
	}
	if f.Category != "" {
		where, args = append(where, "s.category = ?"), append(args, f.Category)
	}
	if f.Label != "" {
		where = append(where, "EXISTS (SELECT 1 FROM gig_labels tl JOIN labels l ON l.id = tl.label_id WHERE tl.gig_id = t.id AND l.name = ?)")
		args = append(args, f.Label)
	}
	if f.PriorityMin != nil {
		where, args = append(where, "t.priority >= ?"), append(args, *f.PriorityMin)
	}
	if f.Assignee != nil {
		where, args = append(where, "EXISTS (SELECT 1 FROM gig_assignees ta WHERE ta.gig_id = t.id AND ta.user_id = ?)"), append(args, *f.Assignee)
	}
	if f.CreatedBy != nil {
		where, args = append(where, "t.created_by = ?"), append(args, *f.CreatedBy)
	}
	if f.Q != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(f.Q) + "%"
		text := `t.title LIKE ? ESCAPE '\' OR t.description_md LIKE ? ESCAPE '\'`
		args = append(args, like, like)
		if f.SearchComments {
			text += ` OR EXISTS (SELECT 1 FROM comments c WHERE c.gig_id = t.id AND c.deleted_at IS NULL AND c.body_md LIKE ? ESCAPE '\')`
			args = append(args, like)
		}
		// a key ("GIG-12", any case) also finds that gig, by its current key or one it had before a move
		if key, n, ok := ParseGigKey(strings.ToUpper(strings.TrimSpace(f.Q))); ok {
			text += ` OR (p.key = ? AND t.number = ?) OR EXISTS (SELECT 1 FROM gig_aliases a WHERE a.gig_id = t.id AND a.project_key = ? AND a.number = ?)`
			args = append(args, key, n, key, n)
		}
		where = append(where, "("+text+")")
	}
	if f.DueBefore != nil {
		where, args = append(where, "t.due_at IS NOT NULL AND t.due_at < ?"), append(args, *f.DueBefore)
	}
	if f.UpdatedSince != nil {
		where, args = append(where, "t.updated_at >= ?"), append(args, *f.UpdatedSince)
	}
	order, err := gigOrder(f.Sort)
	if err != nil {
		return nil, false, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	query := "SELECT " + gigCols + gigFrom + " WHERE " + strings.Join(where, " AND ") + " ORDER BY " + order +
		fmt.Sprintf(" LIMIT %d OFFSET %d", limit+1, f.Offset)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	var out []*Gig
	for rows.Next() {
		t, err := scanGig(rows)
		if err != nil {
			rows.Close()
			return nil, false, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil { // a read cut short must not look like a shorter list
		rows.Close()
		return nil, false, err
	}
	rows.Close()
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	return out, more, fill(ctx, q, out)
}

// sortCols: each sort field's ORDER BY terms, ascending and descending. Gigs without a due date come
// last either way, and the number sorts by project key first (numbers repeat across projects). The
// web's all-workspaces table merges these lists and sorts them the same way (web/src/views/tableSort.ts).
var sortCols = map[string][2]string{
	"priority": {"t.priority", "t.priority DESC"},
	"due":      {"t.due_at IS NULL, t.due_at", "t.due_at IS NULL, t.due_at DESC"},
	"created":  {"t.created_at", "t.created_at DESC"},
	"updated":  {"t.updated_at", "t.updated_at DESC"},
	"number":   {"p.key, t.number", "p.key DESC, t.number DESC"},
	"title":    {"lower(t.title)", "lower(t.title) DESC"},
}

func gigOrder(s string) (string, error) {
	if s == "" || s == "board" {
		return "p.position, t.project_id, s.position, t.position, t.id", nil
	}
	var parts []string
	for _, f := range strings.Split(s, ",") {
		desc := strings.HasPrefix(f, "-")
		cols, ok := sortCols[strings.TrimPrefix(f, "-")]
		if !ok {
			return "", Invalid("unknown sort field %q", f)
		}
		col := cols[0]
		if desc {
			col = cols[1]
		}
		parts = append(parts, col)
	}
	return strings.Join(parts, ", ") + ", t.id", nil
}

// NewGig is the input to CreateGig.
type NewGig struct {
	ProjectID     int64
	Title         string
	DescriptionMD string
	StatusID      *int64
	Priority      int64
	Color         *string
	StartAt       *int64
	DueAt         *int64
	ParentID      *int64
	Labels        []string // names; created in the workspace if missing
	Assignees     []int64
	Followers     []int64 // besides the creator, who follows when their "follow the gigs I create" setting is on
	ClonedFromID  *int64
	CreatedViaKey *int64
	NotAfter      *int64 // window closes
	RecurrenceID  *int64
	OccurrenceAt  *int64
	Model         string
	Effort        string
	AlwaysRun     bool
	AllowedTools  []string
	BlocksParent  bool   // a subgig that holds its parent back while open (needs ParentID)
	AutoArchive   *int64 // minutes after done/cancelled (nil: never)
	RunTimeout    *int64 // minutes a runner lets one run go (nil: the agent's default)
	Quiet         bool   // no "assigned" notification (subgigs made together with their parent)
}

// Efforts are the claude -p effort levels (” = the agent's default).
var Efforts = []string{"low", "medium", "high", "xhigh", "max"}

// ValidRunSettings checks a model name and an effort level. The model ends up as one argument of
// claude -p (never through a shell): letters, digits and . _ : - [ ], starting with a letter or digit.
func ValidRunSettings(model, effort string) error {
	if len(model) > 64 {
		return Invalid("model: at most 64 characters")
	}
	for i, c := range model {
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || i > 0 && strings.ContainsRune("._:-[]", c)
		if !ok {
			return Invalid("model: letters, digits and . _ : - [ ] only, starting with a letter or digit")
		}
	}
	if effort != "" && !slices.Contains(Efforts, effort) {
		return Invalid("effort is low, medium, high, xhigh, max or empty")
	}
	return nil
}

func validGigText(title, desc string) error {
	if strings.TrimSpace(title) == "" || len(title) > 250 {
		return Invalid("title: 1-250 characters")
	}
	if len(desc) > 65536 {
		return Invalid("description: at most 64 KiB")
	}
	return nil
}

// CreateGig inserts a gig at the bottom of its status column.
func CreateGig(ctx context.Context, q Q, a Actor, n NewGig) (*Gig, error) {
	if err := validGigText(n.Title, n.DescriptionMD); err != nil {
		return nil, err
	}
	if n.Priority < 0 || n.Priority > 4 {
		return nil, Invalid("priority is 0-4")
	}
	tools, err := NormTools(n.AllowedTools)
	if err != nil {
		return nil, err
	}
	if err := ValidRunSettings(n.Model, n.Effort); err != nil {
		return nil, err
	}
	if n.Color != nil {
		c, err := NormColor(*n.Color)
		if err != nil {
			return nil, err
		}
		n.Color = &c
	}
	p, err := GetProject(ctx, q, n.ProjectID)
	if err != nil {
		return nil, err
	}
	if p.Deleted || p.Archived {
		return nil, Conflict("the project is archived or deleted")
	}
	var st *Status
	if n.StatusID != nil {
		st, err = GetStatus(ctx, q, *n.StatusID)
		if err != nil || st.ProjectID != p.ID {
			return nil, Invalid("status %d is not in this project", *n.StatusID)
		}
	} else if st, err = DefaultStatus(ctx, q, p.ID); err != nil {
		return nil, err
	}
	if n.ParentID != nil {
		if par, err := GetGig(ctx, q, *n.ParentID); err != nil || par.WorkspaceID != p.WorkspaceID {
			return nil, Invalid("parent gig must be in the same workspace")
		}
	} else if n.BlocksParent {
		return nil, Invalid("blocks_parent needs a parent gig")
	}
	pos, err := placePosition(ctx, q, st.ID, Place{Where: "bottom"})
	if err != nil {
		return nil, err
	}
	num, err := nextNumber(ctx, q, p.ID)
	if err != nil {
		return nil, err
	}
	now := Now()
	var doneAt any
	if st.Category == "done" {
		doneAt = now
	}
	if err := validRunTimeout(n.RunTimeout); err != nil {
		return nil, err
	}
	if err := validAutoArchive(n.AutoArchive); err != nil {
		return nil, err
	}
	if n.NotAfter != nil && n.StartAt != nil && *n.NotAfter <= *n.StartAt {
		return nil, Invalid("the window must close after it opens")
	}
	res, err := q.ExecContext(ctx, `INSERT INTO gigs (workspace_id, project_id, number, title, description_md, status_id, position,
		priority, color, start_at, due_at, done_at, parent_id, cloned_from_id, created_by, created_via_key_id, created_at, updated_at,
		not_after, recurrence_id, occurrence_at, model, effort, always_run, allowed_tools, blocks_parent, auto_archive_minutes, run_timeout_minutes)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, p.WorkspaceID, p.ID, num, n.Title, n.DescriptionMD, st.ID, pos, n.Priority,
		nullStr(n.Color), nullInt(n.StartAt), nullInt(n.DueAt), doneAt, nullInt(n.ParentID), nullInt(n.ClonedFromID),
		a.UserID, nullInt(n.CreatedViaKey), now, now, nullInt(n.NotAfter), nullInt(n.RecurrenceID), nullInt(n.OccurrenceAt),
		n.Model, n.Effort, n.AlwaysRun, mustJSON(tools), n.BlocksParent, nullInt(n.AutoArchive), nullInt(n.RunTimeout))
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if err := setLabels(ctx, q, a, p.WorkspaceID, id, n.Labels); err != nil {
		return nil, err
	}
	if err := setAssignees(ctx, q, p.WorkspaceID, p.ID, id, OwnerDefault(ctx, q, p.WorkspaceID, p.ID, n.Assignees)); err != nil {
		return nil, err
	}
	followers := slices.Clone(n.Followers)
	if a.UserID != 0 && len(uniqIDs(followers)) < MaxFollowers && !slices.Contains(followers, a.UserID) && FollowsOwnGigs(ctx, q, a.UserID) &&
		UserHasIn(ctx, q, a.UserID, p.WorkspaceID, p.ID, "view") {
		followers = append(followers, a.UserID)
	}
	if err := setFollowers(ctx, q, p.WorkspaceID, p.ID, id, followers); err != nil {
		return nil, err
	}
	t, err := GetGig(ctx, q, id)
	if err != nil {
		return nil, err
	}
	extra := map[string]any{"key": t.Key, "status": st.Name}
	if n.ClonedFromID != nil {
		extra["cloned_from"] = *n.ClonedFromID
	}
	if _, err := Activity(ctx, q, a, &p.WorkspaceID, &p.ID, "gig.created", "gig", id, AuditSnapshot("gig", t, extra)); err != nil {
		return nil, err
	}
	if !n.Quiet {
		if err := Notify(ctx, q, a, t, nil, "assigned", t.Assignees); err != nil {
			return nil, err
		}
	}
	if err := Notify(ctx, q, a, t, nil, "mention", MentionedUsers(ctx, q, n.DescriptionMD)); err != nil {
		return nil, err
	}
	return t, nil
}

func nextNumber(ctx context.Context, q Q, project int64) (int64, error) {
	var n int64
	if err := q.QueryRowContext(ctx, `UPDATE projects SET next_gig_number = next_gig_number + 1 WHERE id = ? RETURNING next_gig_number - 1`, project).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func setLabels(ctx context.Context, q Q, a Actor, ws, gig int64, names []string) error {
	if names == nil {
		return nil
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM gig_labels WHERE gig_id = ?`, gig); err != nil {
		return err
	}
	if len(names) > 20 {
		return Invalid("at most 20 labels per gig")
	}
	for _, name := range names {
		l, err := LabelByName(ctx, q, a, ws, strings.TrimSpace(name), true)
		if err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO gig_labels (gig_id, label_id) VALUES (?,?) ON CONFLICT DO NOTHING`, gig, l.ID); err != nil {
			return err
		}
	}
	return nil
}

// OwnerDefault: what a new gig, workspace template or recurring gig is assigned to when it's created
// with nobody: the workspace's owner, if they can be assigned there (active, sees the project; project
// 0: the whole workspace). Creation only; later edits may leave it unassigned.
func OwnerDefault(ctx context.Context, q Q, ws, project int64, users []int64) []int64 {
	if len(users) > 0 || ws == 0 {
		return users
	}
	var owner sql.NullInt64
	if err := q.QueryRowContext(ctx, `SELECT owner_id FROM workspaces WHERE id = ?`, ws).Scan(&owner); err != nil || !owner.Valid {
		return users
	}
	if _, g, err := UserGrants(ctx, q, owner.Int64); err != nil || !g.HasIn(ws, project, "view") {
		return users
	}
	return []int64{owner.Int64}
}

// MaxAssignees bounds a gig's assignee list.
const MaxAssignees = 50

func setAssignees(ctx context.Context, q Q, ws, project, gig int64, users []int64) error {
	if users == nil {
		return nil
	}
	// each id once, and a bounded list: every id costs a grants lookup inside the write transaction
	seen := map[int64]bool{}
	uniq := users[:0:0]
	for _, u := range users {
		if !seen[u] {
			seen[u] = true
			uniq = append(uniq, u)
		}
	}
	if len(uniq) > MaxAssignees {
		return Invalid("at most %d assignees", MaxAssignees)
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM gig_assignees WHERE gig_id = ?`, gig); err != nil {
		return err
	}
	for _, u := range uniq {
		if _, g, err := UserGrants(ctx, q, u); err != nil || !g.HasIn(ws, project, "view") {
			return Invalid("user %d can't see this project", u)
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO gig_assignees (gig_id, user_id) VALUES (?,?) ON CONFLICT DO NOTHING`, gig, u); err != nil {
			return err
		}
	}
	return nil
}

// GigPatch is a partial gig update; nil = unchanged, **T set to nil = cleared.
type GigPatch struct {
	Title         *string
	DescriptionMD *string
	Priority      *int64
	Color         **string
	StartAt       **int64
	DueAt         **int64
	NotAfter      **int64
	ParentID      **int64
	Labels        []string
	Assignees     []int64
	Followers     []int64 // nil = unchanged
	Model         *string
	Effort        *string
	AlwaysRun     *bool
	AllowedTools  []string // nil = unchanged, empty = cleared
	BlocksParent  *bool
	AutoArchive   **int64 // minutes after done/cancelled; *nil = never
	RunTimeout    **int64 // minutes; *nil = the agent's default
}

// PatchGig applies a change; the activity log gets the old and new value of each field that changed.
func PatchGig(ctx context.Context, q Q, a Actor, t *Gig, expect int64, p GigPatch) (*Gig, error) {
	if expect != 0 && expect != t.Version {
		return nil, ErrVersion
	}
	before, err := GetGig(ctx, q, t.ID) // the caller's copy may come from the read pool
	if err != nil {
		return nil, err
	}
	changed := map[string]any{}
	touched := false // labels or assignees
	if p.Title != nil {
		if err := validGigText(*p.Title, ""); err != nil {
			return nil, err
		}
		changed["title"] = *p.Title
	}
	if p.DescriptionMD != nil {
		if err := validGigText("x", *p.DescriptionMD); err != nil {
			return nil, err
		}
		changed["description_md"] = *p.DescriptionMD
	}
	if p.Priority != nil {
		if *p.Priority < 0 || *p.Priority > 4 {
			return nil, Invalid("priority is 0-4")
		}
		changed["priority"] = *p.Priority
	}
	if p.Color != nil {
		var v any
		if *p.Color != nil {
			c, err := NormColor(**p.Color)
			if err != nil {
				return nil, err
			}
			v = c
		}
		changed["color"] = v
	}
	if p.Model != nil || p.Effort != nil {
		m, e := t.Model, t.Effort
		if p.Model != nil {
			m = strings.TrimSpace(*p.Model)
		}
		if p.Effort != nil {
			e = *p.Effort
		}
		if err := ValidRunSettings(m, e); err != nil {
			return nil, err
		}
		if p.Model != nil {
			changed["model"] = m
		}
		if p.Effort != nil {
			changed["effort"] = e
		}
	}
	if p.AlwaysRun != nil {
		changed["always_run"] = *p.AlwaysRun
	}
	if p.AllowedTools != nil {
		tools, err := NormTools(p.AllowedTools)
		if err != nil {
			return nil, err
		}
		if !SameTools(tools, t.AllowedTools) {
			changed["allowed_tools"] = mustJSON(tools)
		}
	}
	if p.StartAt != nil {
		changed["start_at"] = nullInt(*p.StartAt)
	}
	if p.DueAt != nil {
		changed["due_at"] = nullInt(*p.DueAt)
	}
	if p.NotAfter != nil {
		changed["not_after"] = nullInt(*p.NotAfter)
		if *p.NotAfter == nil || **p.NotAfter > Now() {
			changed["missed_at"] = nil // a reopened window clears "missed"
		}
	}
	// a change that sets both ends of the window must close it after it opens (as on create); one end
	// alone is taken as given (the UI saves each date on its own, so moving a window is two changes)
	if p.StartAt != nil && p.NotAfter != nil && *p.StartAt != nil && *p.NotAfter != nil && **p.NotAfter <= **p.StartAt {
		return nil, Invalid("the window must close after it opens")
	}
	if p.ParentID != nil {
		if *p.ParentID != nil {
			if **p.ParentID == t.ID {
				return nil, Invalid("a gig can't be its own parent")
			}
			if par, err := GetGig(ctx, q, **p.ParentID); err != nil || par.WorkspaceID != t.WorkspaceID {
				return nil, Invalid("parent gig must be in the same workspace")
			}
			// no cycles: the new parent can't be this gig's own subgig, at any depth
			var loop int
			if err := q.QueryRowContext(ctx, `WITH RECURSIVE up(id) AS (SELECT parent_id FROM gigs WHERE id = ?
				UNION SELECT t.parent_id FROM gigs t JOIN up ON t.id = up.id WHERE t.parent_id IS NOT NULL)
				SELECT count(*) FROM up WHERE id = ?`, **p.ParentID, t.ID).Scan(&loop); err != nil {
				return nil, err
			}
			if loop > 0 {
				return nil, Invalid("that gig is a subgig of this one; a gig can't be under its own subgig")
			}
		}
		changed["parent_id"] = nullInt(*p.ParentID)
	}
	parent := t.ParentID
	if p.ParentID != nil {
		parent = *p.ParentID
	}
	switch {
	case p.BlocksParent != nil && *p.BlocksParent && parent == nil:
		return nil, Invalid("blocks_parent needs a parent gig")
	case p.BlocksParent != nil && *p.BlocksParent != t.BlocksParent:
		changed["blocks_parent"] = *p.BlocksParent
	case parent == nil && t.BlocksParent:
		changed["blocks_parent"] = false // no parent left to hold back
	}
	if parent != nil && (changed["blocks_parent"] == true || p.ParentID != nil && t.BlocksParent && changed["blocks_parent"] == nil) {
		// the parent mustn't (transitively) hold this gig back, or neither could ever start
		if cyc, err := Blocks(ctx, q, *parent, t.ID); err != nil {
			return nil, err
		} else if cyc {
			return nil, Conflict("the parent already depends on %s: blocking it would be a cycle", t.Key)
		}
	}
	if p.AutoArchive != nil && !eqIntPtr(*p.AutoArchive, t.AutoArchive) {
		if err := validAutoArchive(*p.AutoArchive); err != nil {
			return nil, err
		}
		changed["auto_archive_minutes"] = nullInt(*p.AutoArchive)
	}
	if p.RunTimeout != nil && !eqIntPtr(*p.RunTimeout, t.RunTimeout) {
		if err := validRunTimeout(*p.RunTimeout); err != nil {
			return nil, err
		}
		changed["run_timeout_minutes"] = nullInt(*p.RunTimeout)
	}
	if p.Labels != nil {
		if err := setLabels(ctx, q, a, t.WorkspaceID, t.ID, p.Labels); err != nil {
			return nil, err
		}
		touched = true // a table of their own: the diff below shows them
	}
	if p.Assignees != nil {
		if err := setAssignees(ctx, q, t.WorkspaceID, t.ProjectID, t.ID, p.Assignees); err != nil {
			return nil, err
		}
		touched = true // a table of their own: the diff below shows them
	}
	if p.Followers != nil {
		if err := setFollowers(ctx, q, t.WorkspaceID, t.ProjectID, t.ID, p.Followers); err != nil {
			return nil, err
		}
		touched = true
	}
	if len(changed) == 0 && !touched {
		return t, nil
	}
	var notifyAssigned, notifyMention []int64
	if p.Assignees != nil {
		for _, u := range p.Assignees {
			if !slices.Contains(t.Assignees, u) {
				notifyAssigned = append(notifyAssigned, u)
			}
		}
	}
	if p.DescriptionMD != nil {
		old := MentionedUsers(ctx, q, t.DescriptionMD)
		for _, u := range MentionedUsers(ctx, q, *p.DescriptionMD) {
			if !slices.Contains(old, u) {
				notifyMention = append(notifyMention, u)
			}
		}
	}
	query, args := "UPDATE gigs SET version = version + 1, updated_at = ?", []any{Now()}
	for col, v := range changed {
		query += ", " + col + " = ?"
		args = append(args, v)
	}
	if _, err := q.ExecContext(ctx, query+" WHERE id = ?", append(args, t.ID)...); err != nil {
		return nil, err
	}
	after, err := GetGig(ctx, q, t.ID)
	if err != nil {
		return nil, err
	}
	if _, err := Activity(ctx, q, a, &t.WorkspaceID, &t.ProjectID, "gig.updated", "gig", t.ID, AuditDiff("gig", before, after)); err != nil {
		return nil, err
	}
	if err := Notify(ctx, q, a, t, nil, "assigned", notifyAssigned); err != nil {
		return nil, err
	}
	if err := Notify(ctx, q, a, t, nil, "mention", notifyMention); err != nil {
		return nil, err
	}
	return after, nil
}

// Place says where a gig goes inside a status column.
type Place struct {
	Where string // top | bottom | before | after
	GigID int64  // for before/after
}

// placePosition computes a position for a new slot in status column st, renumbering the column if
// the gap is too small.
func placePosition(ctx context.Context, q Q, st int64, pl Place) (float64, error) {
	for attempt := 0; attempt < 2; attempt++ {
		var pos float64
		var err error
		switch pl.Where {
		case "", "bottom":
			err = q.QueryRowContext(ctx, `SELECT COALESCE(MAX(position), 0) + 1024 FROM gigs WHERE status_id = ?`, st).Scan(&pos)
			return pos, err
		case "top":
			err = q.QueryRowContext(ctx, `SELECT COALESCE(MIN(position), 2048) - 1024 FROM gigs WHERE status_id = ?`, st).Scan(&pos)
			return pos, err
		case "before", "after":
			var ref float64
			if err := q.QueryRowContext(ctx, `SELECT position FROM gigs WHERE id = ? AND status_id = ?`, pl.GigID, st).Scan(&ref); err != nil {
				return 0, Invalid("gig %d is not in that status", pl.GigID)
			}
			var other sql.NullFloat64
			if pl.Where == "before" {
				err = q.QueryRowContext(ctx, `SELECT MAX(position) FROM gigs WHERE status_id = ? AND position < ?`, st, ref).Scan(&other)
				if !other.Valid {
					other.Float64 = ref - 2048
				}
			} else {
				err = q.QueryRowContext(ctx, `SELECT MIN(position) FROM gigs WHERE status_id = ? AND position > ?`, st, ref).Scan(&other)
				if !other.Valid {
					other.Float64 = ref + 2048
				}
			}
			if err != nil {
				return 0, err
			}
			mid := (ref + other.Float64) / 2
			if abs(mid-ref) > 1e-6 && abs(mid-other.Float64) > 1e-6 {
				return mid, nil
			}
			if err := renumberColumn(ctx, q, st); err != nil {
				return 0, err
			}
		default:
			return 0, Invalid("position is top, bottom, before or after")
		}
	}
	return 0, errors.New("could not place gig")
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func renumberColumn(ctx context.Context, q Q, st int64) error {
	_, err := q.ExecContext(ctx, `UPDATE gigs SET position = (SELECT 1024.0 * r FROM (SELECT id, row_number() OVER (ORDER BY position, id) AS r
		FROM gigs WHERE status_id = ?) x WHERE x.id = gigs.id) WHERE status_id = ?`, st, st)
	return err
}

// Move is a change of status, project and/or position.
type Move struct {
	StatusID  *int64 // in the target project
	Category  string // alternative to StatusID: first status of this category
	ProjectID *int64 // same workspace; nil = stay
	Place     Place
}

// MoveGig moves a gig. Moving to another project gives it the next number there and keeps the
// old key as an alias. Entering a done status sets done_at; leaving it clears it.
func MoveGig(ctx context.Context, q Q, a Actor, t *Gig, expect int64, m Move) (*Gig, error) {
	if expect != 0 && expect != t.Version {
		return nil, ErrVersion
	}
	if t.Deleted {
		return nil, Conflict("the gig is in the trash: restore it first")
	}
	target := t.ProjectID
	if m.ProjectID != nil {
		target = *m.ProjectID
	}
	tp, err := GetProject(ctx, q, target)
	if err != nil || tp.WorkspaceID != t.WorkspaceID {
		return nil, Invalid("project %d is not in this workspace", target)
	}
	if tp.Deleted || tp.Archived {
		return nil, Conflict("the target project is archived or deleted")
	}
	var st *Status
	switch {
	case m.StatusID != nil:
		st, err = GetStatus(ctx, q, *m.StatusID)
		if err != nil || st.ProjectID != tp.ID {
			return nil, Invalid("status %d is not in the target project", *m.StatusID)
		}
	case m.Category != "":
		if !ValidCategory(m.Category) {
			return nil, Invalid("unknown status category %q", m.Category)
		}
		if st, err = FirstStatusOf(ctx, q, tp.ID, m.Category); err != nil {
			return nil, Invalid("the target project has no %s status", m.Category)
		}
	case target != t.ProjectID:
		if st, err = FirstStatusOf(ctx, q, tp.ID, t.StatusCategory); err != nil {
			st, err = DefaultStatus(ctx, q, tp.ID)
			if err != nil {
				return nil, err
			}
		}
	default:
		st, err = GetStatus(ctx, q, t.StatusID)
		if err != nil {
			return nil, err
		}
	}
	if m.Place.Where == "" {
		m.Place.Where = "bottom"
		if st.ID == t.StatusID {
			m.Place.Where = "keep"
		}
	}
	pos := t.Position
	if m.Place.Where != "keep" {
		if (m.Place.Where == "before" || m.Place.Where == "after") && m.Place.GigID == t.ID {
			return nil, Invalid("can't place a gig relative to itself")
		}
		if pos, err = placePosition(ctx, q, st.ID, m.Place); err != nil {
			return nil, err
		}
	}
	now := Now()
	data := map[string]any{"from_status": t.StatusName, "to_status": st.Name, "from_category": t.StatusCategory, "to_category": st.Category}
	sets, args := []string{"status_id = ?", "position = ?", "updated_at = ?", "version = version + 1"}, []any{st.ID, pos, now}
	if st.ID != t.StatusID {
		sets, args = append(sets, "last_progress_at = ?"), append(args, now) // a reorder in place isn't progress
	}
	if st.Category == "done" && t.StatusCategory != "done" {
		sets = append(sets, "done_at = ?")
		args = append(args, now)
	} else if st.Category != "done" && t.StatusCategory == "done" {
		sets = append(sets, "done_at = NULL")
	}
	if tp.ID != t.ProjectID {
		num, err := nextNumber(ctx, q, tp.ID)
		if err != nil {
			return nil, err
		}
		oldKey, oldNum := strings.SplitN(t.Key, "-", 2)[0], t.Number
		if _, err := q.ExecContext(ctx, `INSERT INTO gig_aliases (gig_id, workspace_id, project_key, number) VALUES (?,?,?,?) ON CONFLICT DO NOTHING`,
			t.ID, t.WorkspaceID, oldKey, oldNum); err != nil {
			return nil, err
		}
		sets = append(sets, "project_id = ?", "number = ?")
		args = append(args, tp.ID, num)
		data["from_project"], data["to_project"], data["old_key"], data["new_key"] = t.ProjectID, tp.ID, t.Key, fmt.Sprintf("%s-%d", tp.Key, num)
	}
	if _, err := q.ExecContext(ctx, "UPDATE gigs SET "+strings.Join(sets, ", ")+" WHERE id = ?", append(args, t.ID)...); err != nil {
		return nil, err
	}
	if _, err := Activity(ctx, q, a, &t.WorkspaceID, &tp.ID, "gig.moved", "gig", t.ID, data); err != nil {
		return nil, err
	}
	if tp.ID != t.ProjectID {
		// the move is logged under the target project; someone who sees only the source project needs
		// an entry of their own, so their live board drops the card. It says nothing of the target.
		if _, err := Activity(ctx, q, a, &t.WorkspaceID, &t.ProjectID, "gig.moved_out", "gig", t.ID, map[string]any{"old_key": t.Key}); err != nil {
			return nil, err
		}
	}
	return GetGig(ctx, q, t.ID)
}

// CloneGig copies a gig into a project of the same workspace (default: its own), at the bottom of
// the target's default status. Title, description, priority, colour, dates, labels, assignees,
// followers (those who can see the target), model, effort and custom field values are copied, and the checklist (every item unticked);
// always_run and allowed_tools are not (setting those needs their own permissions), nor subgigs,
// dependencies, comments or runs.
func CloneGig(ctx context.Context, q Q, a Actor, t *Gig, project *int64, title *string, viaKey *int64) (*Gig, error) {
	n := NewGig{ProjectID: t.ProjectID, Title: t.Title, DescriptionMD: t.DescriptionMD, Priority: t.Priority, Color: t.Color, Model: t.Model, Effort: t.Effort,
		StartAt: t.StartAt, DueAt: t.DueAt, Assignees: t.Assignees, ClonedFromID: &t.ID, CreatedViaKey: viaKey, AutoArchive: t.AutoArchive}
	if project != nil {
		n.ProjectID = *project
		tp, err := GetProject(ctx, q, *project)
		if err != nil || tp.WorkspaceID != t.WorkspaceID {
			return nil, Invalid("project %d is not in this workspace", *project)
		}
	}
	n.Followers = whoSee(ctx, q, t.WorkspaceID, n.ProjectID, t.Followers, MaxFollowers)
	if title != nil {
		n.Title = *title
	}
	n.Labels = []string{}
	for _, l := range t.Labels {
		n.Labels = append(n.Labels, l.Name)
	}
	c, err := CreateGig(ctx, q, a, n)
	if err != nil {
		return nil, err
	}
	var items []string
	ck, err := Checklist(ctx, q, t.ID)
	if err != nil {
		return nil, err
	}
	for _, it := range ck {
		items = append(items, it.Text)
	}
	if len(items) == 0 && len(t.Custom) == 0 {
		return c, nil
	}
	if err := templateExtras(ctx, q, a, c, items, t.Custom, nil); err != nil {
		return nil, err
	}
	return GetGig(ctx, q, c.ID)
}

// TrashGig soft-deletes (restore = false) or restores a gig.
func TrashGig(ctx context.Context, q Q, a Actor, t *Gig, restore bool) (*Gig, error) {
	action := "gig.deleted"
	if restore {
		if !t.Deleted {
			return t, nil
		}
		// a gig of a deleted project would come back into no list
		var gone int
		if err := q.QueryRowContext(ctx, `SELECT count(*) FROM projects WHERE id = ? AND deleted_at IS NOT NULL`, t.ProjectID).Scan(&gone); err != nil {
			return nil, err
		}
		if gone > 0 {
			return nil, Conflict("its project was deleted, so the gig has nowhere to come back to")
		}
		if _, err := q.ExecContext(ctx, `UPDATE gigs SET deleted_at = NULL, deleted_by = NULL, updated_at = ?, version = version + 1 WHERE id = ?`, Now(), t.ID); err != nil {
			return nil, err
		}
		action = "gig.restored"
	} else {
		if t.Deleted {
			return t, nil
		}
		if _, err := q.ExecContext(ctx, `UPDATE gigs SET deleted_at = ?, deleted_by = ?, updated_at = ?, version = version + 1 WHERE id = ?`, Now(), a.UserID, Now(), t.ID); err != nil {
			return nil, err
		}
	}
	if _, err := Activity(ctx, q, a, &t.WorkspaceID, &t.ProjectID, action, "gig", t.ID, map[string]any{"key": t.Key}); err != nil {
		return nil, err
	}
	return GetGig(ctx, q, t.ID)
}
