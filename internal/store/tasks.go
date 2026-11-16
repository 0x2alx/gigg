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

// Task is a card on a project's board.
type Task struct {
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
	RecurrenceID      *int64         `json:"recurrence_id"`
	OccurrenceAt      *int64         `json:"occurrence_at"`
	NotAfter          *int64         `json:"not_after"` // the window closes: not claimable after this
	MissedAt          *int64         `json:"missed_at"`
	ArchivedAt        *int64         `json:"archived_at"`
	SubtaskCount      int64          `json:"subtask_count"`
	SubtaskDone       int64          `json:"subtask_done"`
	ChecklistTotal    int64          `json:"checklist_total"`
	ChecklistDone     int64          `json:"checklist_done"`
	BlockedBy         int64          `json:"blocked_by" doc:"Open tasks blocking this one: its blockers (dependencies) and its open subtasks marked blocks_parent"`
	BlockedBySubtasks int64          `json:"blocked_by_subtasks" doc:"How many of blocked_by are its own subtasks (blocks_parent)"`
	BlocksParent      bool           `json:"blocks_parent" doc:"A subtask that holds its parent back while it is open"`
	Custom            map[string]any `json:"custom" doc:"Custom field values by field id"`
	Deleted           bool           `json:"deleted"`
	Labels            []TaskTag      `json:"labels"`
	Assignees         []int64        `json:"assignees"`
	Aliases           []string       `json:"aliases"`
	Model             string         `json:"model" doc:"Model a runner should ask for ('' = the agent's default)"`
	Effort            string         `json:"effort" doc:"Effort level a runner should ask for ('' = the agent's default)"`
	AlwaysRun         bool           `json:"always_run" doc:"Worked whatever the usage budget, day or night (within the task cap and pauses)"`
	AllowedTools      []string       `json:"allowed_tools" doc:"The only tools a runner should allow (permission rules); empty = the runner's usual mode"`
	Version           int64          `json:"version"`
	LastRun           *RunBrief      `json:"last_run,omitempty" doc:"The task's latest run, in brief (absent when it has none)"`
	Question          *QuestionBrief `json:"question,omitempty" doc:"Waiting tasks in lists and on the board, with the comments:read scope: the latest question or report nobody else has answered, in brief"`
}

// RunBrief is a task's latest run as a card shows it.
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

// QuestionBrief is the latest question on a waiting task, cut to a card's length.
type QuestionBrief struct {
	CommentID int64  `json:"comment_id"`
	AuthorID  int64  `json:"author_id"`
	CreatedAt int64  `json:"created_at"`
	Kind      string `json:"kind" doc:"question or report"`
	Text      string `json:"text" doc:"The comment's Markdown, at most 280 characters"`
}

const questionBriefLen = 280

// TaskTag is a label as shown on a task.
type TaskTag struct {
	ID    int64   `json:"id"`
	Name  string  `json:"name"`
	Color *string `json:"color"`
}

const taskCols = `t.id, p.key || '-' || t.number, t.workspace_id, t.project_id, t.number, t.title, t.description_md, t.status_id,
	s.name, s.category, t.position, t.priority, t.color, t.start_at, t.due_at, t.done_at, t.parent_id, t.cloned_from_id,
	t.created_by, u.username, u.is_admin, t.created_via_key_id, t.created_at, t.updated_at, t.session_id, t.last_run_id,
	t.resume_requested_at, t.cost_usd_total, t.comment_count, t.deleted_at IS NOT NULL, t.version,
	t.recurrence_id, t.occurrence_at, t.not_after, t.missed_at, t.archived_at,
	(SELECT count(*) FROM tasks c WHERE c.parent_id = t.id AND c.deleted_at IS NULL),
	(SELECT count(*) FROM tasks c JOIN statuses cs ON cs.id = c.status_id WHERE c.parent_id = t.id AND c.deleted_at IS NULL AND cs.category IN ('done','cancelled')),
	(SELECT count(*) FROM checklist_items ci WHERE ci.task_id = t.id),
	(SELECT count(*) FROM checklist_items ci WHERE ci.task_id = t.id AND ci.done_at IS NOT NULL),
	(SELECT count(*) FROM task_relations r JOIN tasks b ON b.id = r.from_task_id JOIN statuses bs ON bs.id = b.status_id
		WHERE r.to_task_id = t.id AND r.kind = 'blocks' AND b.deleted_at IS NULL AND b.archived_at IS NULL AND bs.category NOT IN ('done','cancelled')),
	` + blockingSubtasks + `,
	t.model, t.effort, t.always_run, t.allowed_tools, t.blocks_parent`

// blockingSubtasks counts t's open subtasks that hold it back (blocks_parent; not archived ones,
// which agents never get either), except those that
// also have a "blocks" link to it (already counted with the dependencies).
const blockingSubtasks = `(SELECT count(*) FROM tasks c JOIN statuses cs ON cs.id = c.status_id
		WHERE c.parent_id = t.id AND c.blocks_parent = 1 AND c.deleted_at IS NULL AND c.archived_at IS NULL AND cs.category NOT IN ('done','cancelled')
		AND NOT EXISTS (SELECT 1 FROM task_relations r WHERE r.from_task_id = c.id AND r.to_task_id = t.id AND r.kind = 'blocks'))`

// blockedCond: t has an open blocker, a "blocks" dependency or a subtask that blocks its parent.
// Archived blockers don't count (as with subtasks): archiving parks a task, so it would block forever.
const blockedCond = `(EXISTS (SELECT 1 FROM task_relations r JOIN tasks b ON b.id = r.from_task_id JOIN statuses bs ON bs.id = b.status_id
		WHERE r.to_task_id = t.id AND r.kind = 'blocks' AND b.deleted_at IS NULL AND b.archived_at IS NULL AND bs.category NOT IN ('done','cancelled'))
	OR EXISTS (SELECT 1 FROM tasks c JOIN statuses cs ON cs.id = c.status_id
		WHERE c.parent_id = t.id AND c.blocks_parent = 1 AND c.deleted_at IS NULL AND c.archived_at IS NULL AND cs.category NOT IN ('done','cancelled')))`

const taskFrom = ` FROM tasks t JOIN projects p ON p.id = t.project_id JOIN statuses s ON s.id = t.status_id JOIN users u ON u.id = t.created_by`

func scanTask(row interface{ Scan(...any) error }) (*Task, error) {
	var t Task
	var color, sess sql.NullString
	var start, due, done, parent, cloned, viaKey, lastRun, resume, rec, occ, notAfter, missed, archived sql.NullInt64
	var tools string
	if err := row.Scan(&t.ID, &t.Key, &t.WorkspaceID, &t.ProjectID, &t.Number, &t.Title, &t.DescriptionMD, &t.StatusID,
		&t.StatusName, &t.StatusCategory, &t.Position, &t.Priority, &color, &start, &due, &done, &parent, &cloned,
		&t.CreatedBy, &t.CreatedByUsername, &t.CreatedByAdmin, &viaKey, &t.CreatedAt, &t.UpdatedAt, &sess, &lastRun,
		&resume, &t.CostUSDTotal, &t.CommentCount, &t.Deleted, &t.Version, &rec, &occ, &notAfter, &missed, &archived,
		&t.SubtaskCount, &t.SubtaskDone, &t.ChecklistTotal, &t.ChecklistDone, &t.BlockedBy, &t.BlockedBySubtasks, &t.Model, &t.Effort, &t.AlwaysRun, &tools, &t.BlocksParent); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	t.Color, t.SessionID = ptrStr(color), ptrStr(sess)
	t.StartAt, t.DueAt, t.DoneAt, t.ParentID, t.ClonedFromID = ptrInt(start), ptrInt(due), ptrInt(done), ptrInt(parent), ptrInt(cloned)
	t.CreatedViaKeyID, t.LastRunID, t.ResumeRequestedAt = ptrInt(viaKey), ptrInt(lastRun), ptrInt(resume)
	t.RecurrenceID, t.OccurrenceAt, t.NotAfter, t.MissedAt = ptrInt(rec), ptrInt(occ), ptrInt(notAfter), ptrInt(missed)
	t.ArchivedAt = ptrInt(archived)
	t.Labels, t.Assignees, t.Aliases, t.Custom = []TaskTag{}, []int64{}, []string{}, map[string]any{}
	t.AllowedTools = []string{}
	_ = json.Unmarshal([]byte(tools), &t.AllowedTools)
	t.BlockedBy += t.BlockedBySubtasks
	return &t, nil
}

// fill loads labels, assignees and aliases for a batch of tasks.
func fill(ctx context.Context, q Q, ts []*Task) error {
	if len(ts) == 0 {
		return nil
	}
	byID := map[int64]*Task{}
	ids := make([]int64, 0, len(ts))
	for _, t := range ts {
		byID[t.ID] = t
		ids = append(ids, t.ID)
	}
	in := placeholders(len(ids))
	rows, err := q.QueryContext(ctx, `SELECT tl.task_id, l.id, l.name, l.color FROM task_labels tl JOIN labels l ON l.id = tl.label_id
		WHERE tl.task_id IN (`+in+`) ORDER BY l.name`, int64Args(ids)...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var tid int64
		var tag TaskTag
		var color sql.NullString
		if err := rows.Scan(&tid, &tag.ID, &tag.Name, &color); err != nil {
			rows.Close()
			return err
		}
		tag.Color = ptrStr(color)
		byID[tid].Labels = append(byID[tid].Labels, tag)
	}
	rows.Close()
	rows, err = q.QueryContext(ctx, `SELECT task_id, user_id FROM task_assignees WHERE task_id IN (`+in+`) ORDER BY user_id`, int64Args(ids)...)
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
	rows, err = q.QueryContext(ctx, `SELECT v.task_id, v.field_id, v.value FROM task_custom_values v JOIN custom_fields f ON f.id = v.field_id
		WHERE f.deleted_at IS NULL AND v.task_id IN (`+in+`)`, int64Args(ids)...)
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
	rows, err = q.QueryContext(ctx, `SELECT task_id, project_key || '-' || number FROM task_aliases WHERE task_id IN (`+in+`)`, int64Args(ids)...)
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

// GetTask loads a task by id (including trashed ones).
func GetTask(ctx context.Context, q Q, id int64) (*Task, error) {
	t, err := scanTask(q.QueryRowContext(ctx, "SELECT "+taskCols+taskFrom+" WHERE t.id = ?", id))
	if err != nil {
		return nil, err
	}
	return t, fill(ctx, q, []*Task{t})
}

var taskKeyRe = regexp.MustCompile(`^([A-Z][A-Z0-9]{1,4})-([1-9][0-9]{0,9})$`)

// ParseTaskKey splits "GIG-12".
func ParseTaskKey(k string) (string, int64, bool) {
	m := taskKeyRe.FindStringSubmatch(k)
	if m == nil {
		return "", 0, false
	}
	n, _ := strconv.ParseInt(m[2], 10, 64)
	return m[1], n, true
}

// TaskIDsByKey returns the ids of tasks whose current key or alias is key, within the given workspaces
// (nil = all). More than one id means the key is ambiguous across workspaces. Tasks of a deleted project
// don't count: its key may belong to a new project now.
func TaskIDsByKey(ctx context.Context, q Q, key string, workspaces []int64) ([]int64, error) {
	pk, n, ok := ParseTaskKey(key)
	if !ok {
		return nil, ErrNotFound
	}
	query := `SELECT t.id, t.workspace_id FROM tasks t JOIN projects p ON p.id = t.project_id WHERE p.key = ? AND t.number = ? AND p.deleted_at IS NULL
		UNION SELECT a.task_id, a.workspace_id FROM task_aliases a JOIN tasks t ON t.id = a.task_id JOIN projects p ON p.id = t.project_id
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

// TaskFilter selects tasks in one workspace.
type TaskFilter struct {
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
	ParentID       *int64 // subtasks of this task
	BlockedOnly    bool   // only tasks with an open blocker
	SearchComments bool   // q also matches comment text (only for callers who may read comments)
	Sort           string // board (default), -priority, due, created, -updated, ...
	Limit          int
	AfterID        int64 // cursor: tasks are paged by (sort key, id); for simplicity cursor = offset
	Offset         int
}

// ListTasks returns tasks matching f.
// fillRuns attaches each task's latest run, in brief.
func fillRuns(ctx context.Context, q Q, ts []*Task) error {
	byRun := map[int64]*Task{}
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

// AttachQuestions sets Question on the waiting tasks: their latest question or report comment, cut
// short, until someone other than its author comments after it (an answer, a resume request). Callers
// check the comments:read scope first.
func AttachQuestions(ctx context.Context, q Q, ts []*Task) error {
	byID := map[int64]*Task{}
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
	// per task, the newest question/report through the (task_id, id) index, newest first, stopping at the first
	vals := strings.TrimSuffix(strings.Repeat("(?),", len(ids)), ",")
	args := append(int64Args(ids), questionBriefLen+1)
	rows, err := q.QueryContext(ctx, `WITH w(tid) AS (VALUES `+vals+`),
		qs AS (SELECT (SELECT x.id FROM comments x WHERE x.task_id = w.tid AND x.kind IN ('question', 'report') AND x.deleted_at IS NULL
			ORDER BY x.id DESC LIMIT 1) AS id FROM w)
		SELECT c.id, c.task_id, c.author_id, c.created_at, c.kind, substr(c.body_md, 1, ?) FROM comments c JOIN qs ON qs.id = c.id
		WHERE NOT EXISTS (SELECT 1 FROM comments y WHERE y.task_id = c.task_id AND y.id > c.id AND y.author_id != c.author_id
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

func ListTasks(ctx context.Context, q Q, f TaskFilter) ([]*Task, bool, error) {
	where := []string{"t.workspace_id = ?"}
	args := []any{f.WorkspaceID}
	if f.Projects != nil {
		where = append(where, "t.project_id IN ("+placeholders(len(f.Projects))+")")
		args = append(args, int64Args(f.Projects)...)
	}
	switch {
	case f.OnlyDeleted: // the trash: not tasks of a deleted project (they can't come back)
		where = append(where, "t.deleted_at IS NOT NULL", "p.deleted_at IS NULL")
	case !f.IncludeDeleted:
		where = append(where, "t.deleted_at IS NULL", "p.deleted_at IS NULL")
	}
	switch {
	case f.OnlyDeleted: // the trash shows a trashed task whether or not it was archived first
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
	if f.StatusID != nil {
		where, args = append(where, "t.status_id = ?"), append(args, *f.StatusID)
	}
	if f.Category != "" {
		where, args = append(where, "s.category = ?"), append(args, f.Category)
	}
	if f.Label != "" {
		where = append(where, "EXISTS (SELECT 1 FROM task_labels tl JOIN labels l ON l.id = tl.label_id WHERE tl.task_id = t.id AND l.name = ?)")
		args = append(args, f.Label)
	}
	if f.PriorityMin != nil {
		where, args = append(where, "t.priority >= ?"), append(args, *f.PriorityMin)
	}
	if f.Assignee != nil {
		where, args = append(where, "EXISTS (SELECT 1 FROM task_assignees ta WHERE ta.task_id = t.id AND ta.user_id = ?)"), append(args, *f.Assignee)
	}
	if f.CreatedBy != nil {
		where, args = append(where, "t.created_by = ?"), append(args, *f.CreatedBy)
	}
	if f.Q != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(f.Q) + "%"
		text := `t.title LIKE ? ESCAPE '\' OR t.description_md LIKE ? ESCAPE '\'`
		args = append(args, like, like)
		if f.SearchComments {
			text += ` OR EXISTS (SELECT 1 FROM comments c WHERE c.task_id = t.id AND c.deleted_at IS NULL AND c.body_md LIKE ? ESCAPE '\')`
			args = append(args, like)
		}
		// a key ("GIG-12", any case) also finds that task, by its current key or one it had before a move
		if key, n, ok := ParseTaskKey(strings.ToUpper(strings.TrimSpace(f.Q))); ok {
			text += ` OR (p.key = ? AND t.number = ?) OR EXISTS (SELECT 1 FROM task_aliases a WHERE a.task_id = t.id AND a.project_key = ? AND a.number = ?)`
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
	order, err := taskOrder(f.Sort)
	if err != nil {
		return nil, false, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	query := "SELECT " + taskCols + taskFrom + " WHERE " + strings.Join(where, " AND ") + " ORDER BY " + order +
		fmt.Sprintf(" LIMIT %d OFFSET %d", limit+1, f.Offset)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	var out []*Task
	for rows.Next() {
		t, err := scanTask(rows)
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

// sortCols: each sort field's ORDER BY terms, ascending and descending. Tasks without a due date come
// last either way, and the number sorts by project key first (numbers repeat across projects). The
// web's all-workspaces table merges these lists and sorts them the same way (web/src/views/tableSort.ts).
var sortCols = map[string][2]string{
	"priority": {"t.priority", "t.priority DESC"},
	"due":      {"t.due_at IS NULL, t.due_at", "t.due_at IS NULL, t.due_at DESC"},
	"created":  {"t.created_at", "t.created_at DESC"},
	"updated":  {"t.updated_at", "t.updated_at DESC"},
	"number":   {"p.key, t.number", "p.key DESC, t.number DESC"},
	"title":    {"t.title COLLATE NOCASE", "t.title COLLATE NOCASE DESC"},
}

func taskOrder(s string) (string, error) {
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

// NewTask is the input to CreateTask.
type NewTask struct {
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
	ClonedFromID  *int64
	CreatedViaKey *int64
	NotAfter      *int64 // window closes
	RecurrenceID  *int64
	OccurrenceAt  *int64
	Model         string
	Effort        string
	AlwaysRun     bool
	AllowedTools  []string
	BlocksParent  bool // a subtask that holds its parent back while open (needs ParentID)
	Quiet         bool // no "assigned" notification (subtasks made together with their parent)
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

func validTaskText(title, desc string) error {
	if strings.TrimSpace(title) == "" || len(title) > 250 {
		return Invalid("title: 1-250 characters")
	}
	if len(desc) > 65536 {
		return Invalid("description: at most 64 KiB")
	}
	return nil
}

// CreateTask inserts a task at the bottom of its status column.
func CreateTask(ctx context.Context, q Q, a Actor, n NewTask) (*Task, error) {
	if err := validTaskText(n.Title, n.DescriptionMD); err != nil {
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
		if par, err := GetTask(ctx, q, *n.ParentID); err != nil || par.WorkspaceID != p.WorkspaceID {
			return nil, Invalid("parent task must be in the same workspace")
		}
	} else if n.BlocksParent {
		return nil, Invalid("blocks_parent needs a parent task")
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
	if n.NotAfter != nil && n.StartAt != nil && *n.NotAfter <= *n.StartAt {
		return nil, Invalid("the window must close after it opens")
	}
	res, err := q.ExecContext(ctx, `INSERT INTO tasks (workspace_id, project_id, number, title, description_md, status_id, position,
		priority, color, start_at, due_at, done_at, parent_id, cloned_from_id, created_by, created_via_key_id, created_at, updated_at,
		not_after, recurrence_id, occurrence_at, model, effort, always_run, allowed_tools, blocks_parent)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, p.WorkspaceID, p.ID, num, n.Title, n.DescriptionMD, st.ID, pos, n.Priority,
		nullStr(n.Color), nullInt(n.StartAt), nullInt(n.DueAt), doneAt, nullInt(n.ParentID), nullInt(n.ClonedFromID),
		a.UserID, nullInt(n.CreatedViaKey), now, now, nullInt(n.NotAfter), nullInt(n.RecurrenceID), nullInt(n.OccurrenceAt),
		n.Model, n.Effort, n.AlwaysRun, mustJSON(tools), n.BlocksParent)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if err := setLabels(ctx, q, a, p.WorkspaceID, id, n.Labels); err != nil {
		return nil, err
	}
	if err := setAssignees(ctx, q, p.WorkspaceID, id, n.Assignees); err != nil {
		return nil, err
	}
	data := map[string]any{"key": fmt.Sprintf("%s-%d", p.Key, num), "title": n.Title, "status": st.Name}
	if n.ClonedFromID != nil {
		data["cloned_from"] = *n.ClonedFromID
	}
	if _, err := Activity(ctx, q, a, &p.WorkspaceID, &p.ID, "task.created", "task", id, data); err != nil {
		return nil, err
	}
	t, err := GetTask(ctx, q, id)
	if err != nil {
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
	if err := q.QueryRowContext(ctx, `UPDATE projects SET next_task_number = next_task_number + 1 WHERE id = ? RETURNING next_task_number - 1`, project).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func setLabels(ctx context.Context, q Q, a Actor, ws, task int64, names []string) error {
	if names == nil {
		return nil
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM task_labels WHERE task_id = ?`, task); err != nil {
		return err
	}
	if len(names) > 20 {
		return Invalid("at most 20 labels per task")
	}
	for _, name := range names {
		l, err := LabelByName(ctx, q, a, ws, strings.TrimSpace(name), true)
		if err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO task_labels (task_id, label_id) VALUES (?,?)`, task, l.ID); err != nil {
			return err
		}
	}
	return nil
}

// MaxAssignees bounds a task's assignee list.
const MaxAssignees = 50

func setAssignees(ctx context.Context, q Q, ws, task int64, users []int64) error {
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
	if _, err := q.ExecContext(ctx, `DELETE FROM task_assignees WHERE task_id = ?`, task); err != nil {
		return err
	}
	for _, u := range uniq {
		if _, g, err := UserGrants(ctx, q, u); err != nil || !g.Has(ws, "view") {
			return Invalid("user %d can't see this workspace", u)
		}
		if _, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO task_assignees (task_id, user_id) VALUES (?,?)`, task, u); err != nil {
			return err
		}
	}
	return nil
}

// TaskPatch is a partial task update; nil = unchanged, **T set to nil = cleared.
type TaskPatch struct {
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
	Model         *string
	Effort        *string
	AlwaysRun     *bool
	AllowedTools  []string // nil = unchanged, empty = cleared
	BlocksParent  *bool
}

// PatchTask applies a change and records old and new values (old text in full).
func PatchTask(ctx context.Context, q Q, a Actor, t *Task, expect int64, p TaskPatch) (*Task, error) {
	if expect != 0 && expect != t.Version {
		return nil, ErrVersion
	}
	changed := map[string]any{}
	data := map[string]any{}
	if p.Title != nil {
		if err := validTaskText(*p.Title, ""); err != nil {
			return nil, err
		}
		changed["title"], data["title"] = *p.Title, map[string]any{"old": t.Title, "new": *p.Title}
	}
	if p.DescriptionMD != nil {
		if err := validTaskText("x", *p.DescriptionMD); err != nil {
			return nil, err
		}
		changed["description_md"], data["description_md"] = *p.DescriptionMD, map[string]any{"old": t.DescriptionMD}
	}
	if p.Priority != nil {
		if *p.Priority < 0 || *p.Priority > 4 {
			return nil, Invalid("priority is 0-4")
		}
		changed["priority"], data["priority"] = *p.Priority, map[string]any{"old": t.Priority, "new": *p.Priority}
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
		changed["color"], data["color"] = v, v
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
			changed["model"], data["model"] = m, map[string]any{"old": t.Model, "new": m}
		}
		if p.Effort != nil {
			changed["effort"], data["effort"] = e, map[string]any{"old": t.Effort, "new": e}
		}
	}
	if p.AlwaysRun != nil {
		changed["always_run"], data["always_run"] = *p.AlwaysRun, map[string]any{"old": t.AlwaysRun, "new": *p.AlwaysRun}
	}
	if p.AllowedTools != nil {
		tools, err := NormTools(p.AllowedTools)
		if err != nil {
			return nil, err
		}
		if !SameTools(tools, t.AllowedTools) {
			changed["allowed_tools"], data["allowed_tools"] = mustJSON(tools), map[string]any{"old": t.AllowedTools, "new": tools}
		}
	}
	if p.StartAt != nil {
		changed["start_at"], data["start_at"] = nullInt(*p.StartAt), nullInt(*p.StartAt)
	}
	if p.DueAt != nil {
		changed["due_at"], data["due_at"] = nullInt(*p.DueAt), nullInt(*p.DueAt)
	}
	if p.NotAfter != nil {
		changed["not_after"], data["not_after"] = nullInt(*p.NotAfter), nullInt(*p.NotAfter)
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
				return nil, Invalid("a task can't be its own parent")
			}
			if par, err := GetTask(ctx, q, **p.ParentID); err != nil || par.WorkspaceID != t.WorkspaceID {
				return nil, Invalid("parent task must be in the same workspace")
			}
			// no cycles: the new parent can't be this task's own subtask, at any depth
			var loop int
			if err := q.QueryRowContext(ctx, `WITH RECURSIVE up(id) AS (SELECT parent_id FROM tasks WHERE id = ?
				UNION SELECT t.parent_id FROM tasks t JOIN up ON t.id = up.id WHERE t.parent_id IS NOT NULL)
				SELECT count(*) FROM up WHERE id = ?`, **p.ParentID, t.ID).Scan(&loop); err != nil {
				return nil, err
			}
			if loop > 0 {
				return nil, Invalid("that task is a subtask of this one; a task can't be under its own subtask")
			}
		}
		changed["parent_id"], data["parent_id"] = nullInt(*p.ParentID), nullInt(*p.ParentID)
	}
	parent := t.ParentID
	if p.ParentID != nil {
		parent = *p.ParentID
	}
	switch {
	case p.BlocksParent != nil && *p.BlocksParent && parent == nil:
		return nil, Invalid("blocks_parent needs a parent task")
	case p.BlocksParent != nil && *p.BlocksParent != t.BlocksParent:
		changed["blocks_parent"], data["blocks_parent"] = *p.BlocksParent, map[string]any{"old": t.BlocksParent, "new": *p.BlocksParent}
	case parent == nil && t.BlocksParent:
		changed["blocks_parent"] = false // no parent left to hold back
	}
	if parent != nil && (changed["blocks_parent"] == true || p.ParentID != nil && t.BlocksParent && changed["blocks_parent"] == nil) {
		// the parent mustn't (transitively) hold this task back, or neither could ever start
		if cyc, err := Blocks(ctx, q, *parent, t.ID); err != nil {
			return nil, err
		} else if cyc {
			return nil, Conflict("the parent already depends on %s: blocking it would be a cycle", t.Key)
		}
	}
	if p.Labels != nil {
		if err := setLabels(ctx, q, a, t.WorkspaceID, t.ID, p.Labels); err != nil {
			return nil, err
		}
		data["labels"] = p.Labels
	}
	if p.Assignees != nil {
		if err := setAssignees(ctx, q, t.WorkspaceID, t.ID, p.Assignees); err != nil {
			return nil, err
		}
		data["assignees"] = p.Assignees
	}
	if len(data) == 0 {
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
	query, args := "UPDATE tasks SET version = version + 1, updated_at = ?", []any{Now()}
	for col, v := range changed {
		query += ", " + col + " = ?"
		args = append(args, v)
	}
	if _, err := q.ExecContext(ctx, query+" WHERE id = ?", append(args, t.ID)...); err != nil {
		return nil, err
	}
	if _, err := Activity(ctx, q, a, &t.WorkspaceID, &t.ProjectID, "task.updated", "task", t.ID, data); err != nil {
		return nil, err
	}
	if err := Notify(ctx, q, a, t, nil, "assigned", notifyAssigned); err != nil {
		return nil, err
	}
	if err := Notify(ctx, q, a, t, nil, "mention", notifyMention); err != nil {
		return nil, err
	}
	return GetTask(ctx, q, t.ID)
}

// Place says where a task goes inside a status column.
type Place struct {
	Where  string // top | bottom | before | after
	TaskID int64  // for before/after
}

// placePosition computes a position for a new slot in status column st, renumbering the column if
// the gap is too small.
func placePosition(ctx context.Context, q Q, st int64, pl Place) (float64, error) {
	for attempt := 0; attempt < 2; attempt++ {
		var pos float64
		var err error
		switch pl.Where {
		case "", "bottom":
			err = q.QueryRowContext(ctx, `SELECT COALESCE(MAX(position), 0) + 1024 FROM tasks WHERE status_id = ?`, st).Scan(&pos)
			return pos, err
		case "top":
			err = q.QueryRowContext(ctx, `SELECT COALESCE(MIN(position), 2048) - 1024 FROM tasks WHERE status_id = ?`, st).Scan(&pos)
			return pos, err
		case "before", "after":
			var ref float64
			if err := q.QueryRowContext(ctx, `SELECT position FROM tasks WHERE id = ? AND status_id = ?`, pl.TaskID, st).Scan(&ref); err != nil {
				return 0, Invalid("task %d is not in that status", pl.TaskID)
			}
			var other sql.NullFloat64
			if pl.Where == "before" {
				err = q.QueryRowContext(ctx, `SELECT MAX(position) FROM tasks WHERE status_id = ? AND position < ?`, st, ref).Scan(&other)
				if !other.Valid {
					other.Float64 = ref - 2048
				}
			} else {
				err = q.QueryRowContext(ctx, `SELECT MIN(position) FROM tasks WHERE status_id = ? AND position > ?`, st, ref).Scan(&other)
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
	return 0, errors.New("could not place task")
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func renumberColumn(ctx context.Context, q Q, st int64) error {
	_, err := q.ExecContext(ctx, `UPDATE tasks SET position = (SELECT 1024.0 * r FROM (SELECT id, row_number() OVER (ORDER BY position, id) AS r
		FROM tasks WHERE status_id = ?) x WHERE x.id = tasks.id) WHERE status_id = ?`, st, st)
	return err
}

// Move is a change of status, project and/or position.
type Move struct {
	StatusID  *int64 // in the target project
	Category  string // alternative to StatusID: first status of this category
	ProjectID *int64 // same workspace; nil = stay
	Place     Place
}

// MoveTask moves a task. Moving to another project gives it the next number there and keeps the
// old key as an alias. Entering a done status sets done_at; leaving it clears it.
func MoveTask(ctx context.Context, q Q, a Actor, t *Task, expect int64, m Move) (*Task, error) {
	if expect != 0 && expect != t.Version {
		return nil, ErrVersion
	}
	if t.Deleted {
		return nil, Conflict("the task is in the trash: restore it first")
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
		if (m.Place.Where == "before" || m.Place.Where == "after") && m.Place.TaskID == t.ID {
			return nil, Invalid("can't place a task relative to itself")
		}
		if pos, err = placePosition(ctx, q, st.ID, m.Place); err != nil {
			return nil, err
		}
	}
	now := Now()
	data := map[string]any{"from_status": t.StatusName, "to_status": st.Name, "from_category": t.StatusCategory, "to_category": st.Category}
	sets, args := []string{"status_id = ?", "position = ?", "updated_at = ?", "version = version + 1"}, []any{st.ID, pos, now}
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
		if _, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO task_aliases (task_id, workspace_id, project_key, number) VALUES (?,?,?,?)`,
			t.ID, t.WorkspaceID, oldKey, oldNum); err != nil {
			return nil, err
		}
		sets = append(sets, "project_id = ?", "number = ?")
		args = append(args, tp.ID, num)
		data["from_project"], data["to_project"], data["old_key"], data["new_key"] = t.ProjectID, tp.ID, t.Key, fmt.Sprintf("%s-%d", tp.Key, num)
	}
	if _, err := q.ExecContext(ctx, "UPDATE tasks SET "+strings.Join(sets, ", ")+" WHERE id = ?", append(args, t.ID)...); err != nil {
		return nil, err
	}
	if _, err := Activity(ctx, q, a, &t.WorkspaceID, &tp.ID, "task.moved", "task", t.ID, data); err != nil {
		return nil, err
	}
	return GetTask(ctx, q, t.ID)
}

// CloneTask copies a task into a project of the same workspace (default: its own), at the bottom of
// the target's default status. Title, description, priority, colour, dates, labels, assignees, model,
// effort and custom field values are copied, and with checklist the checklist (every item unticked);
// always_run and allowed_tools are not (setting those needs their own permissions), nor subtasks,
// dependencies, comments or runs.
func CloneTask(ctx context.Context, q Q, a Actor, t *Task, project *int64, title *string, viaKey *int64, checklist bool) (*Task, error) {
	n := NewTask{ProjectID: t.ProjectID, Title: t.Title, DescriptionMD: t.DescriptionMD, Priority: t.Priority, Color: t.Color, Model: t.Model, Effort: t.Effort,
		StartAt: t.StartAt, DueAt: t.DueAt, Assignees: t.Assignees, ClonedFromID: &t.ID, CreatedViaKey: viaKey}
	if project != nil {
		n.ProjectID = *project
		tp, err := GetProject(ctx, q, *project)
		if err != nil || tp.WorkspaceID != t.WorkspaceID {
			return nil, Invalid("project %d is not in this workspace", *project)
		}
	}
	if title != nil {
		n.Title = *title
	}
	n.Labels = []string{}
	for _, l := range t.Labels {
		n.Labels = append(n.Labels, l.Name)
	}
	c, err := CreateTask(ctx, q, a, n)
	if err != nil {
		return nil, err
	}
	var items []string
	if checklist {
		ck, err := Checklist(ctx, q, t.ID)
		if err != nil {
			return nil, err
		}
		for _, it := range ck {
			items = append(items, it.Text)
		}
	}
	if len(items) == 0 && len(t.Custom) == 0 {
		return c, nil
	}
	if err := templateExtras(ctx, q, a, c, items, t.Custom, nil); err != nil {
		return nil, err
	}
	return GetTask(ctx, q, c.ID)
}

// TrashTask soft-deletes (restore = false) or restores a task.
func TrashTask(ctx context.Context, q Q, a Actor, t *Task, restore bool) (*Task, error) {
	action := "task.deleted"
	if restore {
		if !t.Deleted {
			return t, nil
		}
		// a task of a deleted project would come back into no list
		var gone int
		if err := q.QueryRowContext(ctx, `SELECT count(*) FROM projects WHERE id = ? AND deleted_at IS NOT NULL`, t.ProjectID).Scan(&gone); err != nil {
			return nil, err
		}
		if gone > 0 {
			return nil, Conflict("its project was deleted, so the task has nowhere to come back to")
		}
		if _, err := q.ExecContext(ctx, `UPDATE tasks SET deleted_at = NULL, deleted_by = NULL, updated_at = ?, version = version + 1 WHERE id = ?`, Now(), t.ID); err != nil {
			return nil, err
		}
		action = "task.restored"
	} else {
		if t.Deleted {
			return t, nil
		}
		if _, err := q.ExecContext(ctx, `UPDATE tasks SET deleted_at = ?, deleted_by = ?, updated_at = ?, version = version + 1 WHERE id = ?`, Now(), a.UserID, Now(), t.ID); err != nil {
			return nil, err
		}
	}
	if _, err := Activity(ctx, q, a, &t.WorkspaceID, &t.ProjectID, action, "task", t.ID, map[string]any{"key": t.Key}); err != nil {
		return nil, err
	}
	return GetTask(ctx, q, t.ID)
}
