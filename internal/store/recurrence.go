package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Recurrence is a recurring task: the link between a template (what each task looks like) and a
// schedule (when), in a project, with its own start/end, max count, overlap, pause and state. Each
// occurrence becomes a normal task. The content fields are read through the template, the timing
// fields through the schedule; the same template and schedule can be paired once per project.
type Recurrence struct {
	ID            int64    `json:"id"`
	WorkspaceID   int64    `json:"workspace_id"`
	ProjectID     int64    `json:"project_id"`
	CreatedBy     int64    `json:"created_by"`
	Title         string   `json:"title"`
	DescriptionMD string   `json:"description_md"`
	Priority      int64    `json:"priority"`
	Color         *string  `json:"color"`
	Labels        []string `json:"labels"`
	Assignees     []int64  `json:"assignees"`
	Model         string   `json:"model"`
	Effort        string   `json:"effort"`
	AlwaysRun     bool     `json:"always_run"`
	AllowedTools  []string `json:"allowed_tools"`
	Freq          string   `json:"freq"`           // daily | weekly | monthly
	Interval      int64    `json:"interval"`       // every N days/weeks/months
	Weekdays      []int    `json:"weekdays"`       // weekly: 1=Mon .. 7=Sun
	MonthDay      *int64   `json:"month_day"`      // monthly: 1-31, -1 = last day
	TZ            string   `json:"tz"`             // IANA zone
	WindowStart   string   `json:"window_start"`   // HH:MM local
	WindowMinutes int64    `json:"window_minutes"` // how long the window stays open
	LeadMinutes   int64    `json:"lead_minutes"`   // create the task this long before the window opens
	StartsOn      int64    `json:"starts_on"`
	EndsOn        *int64   `json:"ends_on"`
	MaxCount      *int64   `json:"max_count"`
	Overlap       string   `json:"overlap"` // skip | create (when the previous occurrence is still open)
	NextAt        *int64   `json:"next_at"`
	CreatedCount  int64    `json:"created_count"`
	SkippedCount  int64    `json:"skipped_count"`
	LastTaskID    *int64   `json:"last_task_id"`
	Paused        bool     `json:"paused"`
	CreatedAt     int64    `json:"created_at"`
	Version       int64    `json:"version"`
	// the template
	TemplateID   int64          `json:"template_id"`
	TemplateName string         `json:"template_name"`
	TemplateBy   int64          `json:"template_created_by"`
	TemplateUses int64          `json:"template_uses" doc:"Live recurring tasks using the template, this one included"`
	Checklist    []string       `json:"checklist"`
	Custom       map[string]any `json:"custom"`
	// the schedule
	ScheduleID   int64  `json:"schedule_id"`
	ScheduleName string `json:"schedule_name"`
	ScheduleUses int64  `json:"schedule_uses" doc:"Live recurring tasks using the schedule, this one included"`
}

// recCols reads a recurring task with its template's content and its schedule's timing; select
// FROM recFrom.
const recCols = `r.id, r.workspace_id, r.project_id, r.created_by, t.title, t.description_md, t.priority, t.color, t.labels, t.assignees,
	s.freq, s.interval_n, s.weekdays, s.month_day, s.tz, s.window_start, s.window_minutes, s.lead_minutes, r.starts_on, r.ends_on, r.max_count,
	r.overlap, r.next_at, r.created_count, r.skipped_count, r.last_task_id, r.paused_at IS NOT NULL, r.created_at, r.version, t.model, t.effort,
	t.always_run, t.allowed_tools, t.id, t.name, t.created_by, t.checklist, t.custom,
	(SELECT count(*) FROM recurrences u WHERE u.template_id = t.id AND u.deleted_at IS NULL),
	s.id, s.name, (SELECT count(*) FROM recurrences u WHERE u.schedule_id = s.id AND u.deleted_at IS NULL)`

const recFrom = `recurrences r JOIN task_templates t ON t.id = r.template_id JOIN schedules s ON s.id = r.schedule_id`

func scanRecurrence(row interface{ Scan(...any) error }) (*Recurrence, error) {
	var r Recurrence
	var color sql.NullString
	var labels, assignees, weekdays, checklist, custom, tools string
	var md, ends, max, next, last sql.NullInt64
	if err := row.Scan(&r.ID, &r.WorkspaceID, &r.ProjectID, &r.CreatedBy, &r.Title, &r.DescriptionMD, &r.Priority, &color, &labels, &assignees,
		&r.Freq, &r.Interval, &weekdays, &md, &r.TZ, &r.WindowStart, &r.WindowMinutes, &r.LeadMinutes, &r.StartsOn, &ends, &max, &r.Overlap,
		&next, &r.CreatedCount, &r.SkippedCount, &last, &r.Paused, &r.CreatedAt, &r.Version, &r.Model, &r.Effort, &r.AlwaysRun, &tools,
		&r.TemplateID, &r.TemplateName, &r.TemplateBy, &checklist, &custom, &r.TemplateUses,
		&r.ScheduleID, &r.ScheduleName, &r.ScheduleUses); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	r.Color, r.MonthDay, r.EndsOn, r.MaxCount, r.NextAt, r.LastTaskID = ptrStr(color), ptrInt(md), ptrInt(ends), ptrInt(max), ptrInt(next), ptrInt(last)
	_ = json.Unmarshal([]byte(labels), &r.Labels)
	_ = json.Unmarshal([]byte(assignees), &r.Assignees)
	_ = json.Unmarshal([]byte(weekdays), &r.Weekdays)
	_ = json.Unmarshal([]byte(checklist), &r.Checklist)
	_ = json.Unmarshal([]byte(custom), &r.Custom)
	r.AllowedTools = []string{}
	_ = json.Unmarshal([]byte(tools), &r.AllowedTools)
	if r.Checklist == nil {
		r.Checklist = []string{}
	}
	if r.Custom == nil {
		r.Custom = map[string]any{}
	}
	if r.Labels == nil {
		r.Labels = []string{}
	}
	if r.Assignees == nil {
		r.Assignees = []int64{}
	}
	if r.Weekdays == nil {
		r.Weekdays = []int{}
	}
	return &r, nil
}

// Validate checks and normalises a recurring task: its content, its timing and its own settings.
func (r *Recurrence) Validate() error {
	if strings.TrimSpace(r.Title) == "" || len(r.Title) > 250 {
		return Invalid("title: 1-250 characters")
	}
	if len(r.DescriptionMD) > 65536 {
		return Invalid("description: at most 64 KiB")
	}
	if r.Priority < 0 || r.Priority > 4 {
		return Invalid("priority is 0-4")
	}
	r.Model = strings.TrimSpace(r.Model)
	if err := ValidRunSettings(r.Model, r.Effort); err != nil {
		return err
	}
	if r.Color != nil {
		c, err := NormColor(*r.Color)
		if err != nil {
			return err
		}
		r.Color = &c
	}
	rule := r.Rule()
	if err := rule.Validate(); err != nil {
		return err
	}
	r.SetRule(rule)
	if r.Overlap == "" {
		r.Overlap = "skip"
	}
	if r.Overlap != "skip" && r.Overlap != "create" {
		return Invalid("overlap is skip or create")
	}
	if r.MaxCount != nil && *r.MaxCount < 1 {
		return Invalid("max_count is at least 1")
	}
	if r.Labels == nil {
		r.Labels = []string{}
	}
	if r.Assignees == nil {
		r.Assignees = []int64{}
	}
	return validTemplateLists(r.Labels, r.Assignees, r.StartsOn, r.EndsOn)
}

// Rule is the recurring task's timing (its schedule's).
func (r *Recurrence) Rule() Rule {
	return Rule{Freq: r.Freq, Interval: r.Interval, Weekdays: r.Weekdays, MonthDay: r.MonthDay, TZ: r.TZ, WindowStart: r.WindowStart,
		WindowMinutes: r.WindowMinutes, LeadMinutes: r.LeadMinutes}
}

// SetRule copies timing into the recurring task.
func (r *Recurrence) SetRule(x Rule) {
	r.Freq, r.Interval, r.Weekdays, r.MonthDay, r.TZ = x.Freq, x.Interval, x.Weekdays, x.MonthDay, x.TZ
	r.WindowStart, r.WindowMinutes, r.LeadMinutes = x.WindowStart, x.WindowMinutes, x.LeadMinutes
}

// Bounds shared by schedules and templates: labels as CreateLabel takes them, a sane number of
// assignees, and dates between 2000 and 2200 (keeps the occurrence search short).
const (
	minDate = 946684800000  // 2000-01-01
	maxDate = 7258118400000 // 2200-01-01
)

func validTemplateLists(labels []string, assignees []int64, startsOn int64, endsOn *int64) error {
	if len(labels) > 20 {
		return Invalid("at most 20 labels")
	}
	for _, l := range labels {
		if l == "" || len(l) > 40 {
			return Invalid("label names: 1-40 characters")
		}
	}
	if len(assignees) > 50 {
		return Invalid("at most 50 assignees")
	}
	if startsOn != 0 && (startsOn < minDate || startsOn > maxDate) {
		return Invalid("starts_on: between 2000 and 2200")
	}
	if endsOn != nil && (*endsOn < minDate || *endsOn > maxDate) {
		return Invalid("ends_on: between 2000 and 2200")
	}
	return nil
}

// NextOccurrence returns the start of the first occurrence window at or after `after` (unix ms),
// or false if the schedule has ended.
func (r *Recurrence) NextOccurrence(after int64) (int64, bool) {
	loc, err := time.LoadLocation(r.TZ)
	if err != nil {
		return 0, false
	}
	hm, _ := time.Parse("15:04", r.WindowStart)
	start := time.UnixMilli(r.StartsOn).In(loc)
	startDay := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
	from := time.UnixMilli(after).In(loc)
	day := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, loc)
	if day.Before(startDay) {
		day = startDay
	}
	for i := 0; i < 366*int(r.Interval)+62; i++ {
		d := day.AddDate(0, 0, i)
		if r.matches(d, startDay) {
			at := time.Date(d.Year(), d.Month(), d.Day(), hm.Hour(), hm.Minute(), 0, 0, loc)
			if at.UnixMilli() < after || at.UnixMilli() < r.StartsOn {
				continue
			}
			if r.EndsOn != nil && at.UnixMilli() > *r.EndsOn {
				return 0, false
			}
			return at.UnixMilli(), true
		}
	}
	return 0, false
}

func (r *Recurrence) matches(d, startDay time.Time) bool {
	switch r.Freq {
	case "daily":
		days := int64(daysBetween(startDay, d))
		return days >= 0 && days%r.Interval == 0
	case "weekly":
		wd := int(d.Weekday())
		if wd == 0 {
			wd = 7
		}
		ok := false
		for _, w := range r.Weekdays {
			if w == wd {
				ok = true
			}
		}
		if !ok {
			return false
		}
		weeks := int64(daysBetween(mondayOf(startDay), mondayOf(d)) / 7)
		return weeks >= 0 && weeks%r.Interval == 0
	case "monthly":
		months := int64((d.Year()-startDay.Year())*12 + int(d.Month()) - int(startDay.Month()))
		if months < 0 || months%r.Interval != 0 {
			return false
		}
		last := time.Date(d.Year(), d.Month()+1, 0, 0, 0, 0, 0, d.Location()).Day()
		want := int(*r.MonthDay)
		if want == -1 || want > last {
			want = last
		}
		return d.Day() == want
	}
	return false
}

func daysBetween(a, b time.Time) int {
	// calendar days, robust to DST: compare dates at noon UTC
	ua := time.Date(a.Year(), a.Month(), a.Day(), 12, 0, 0, 0, time.UTC)
	ub := time.Date(b.Year(), b.Month(), b.Day(), 12, 0, 0, 0, time.UTC)
	return int(ub.Sub(ua).Hours() / 24)
}

func mondayOf(d time.Time) time.Time {
	wd := int(d.Weekday())
	if wd == 0 {
		wd = 7
	}
	return d.AddDate(0, 0, 1-wd)
}

// Upcoming lists the next n occurrence starts after `after`.
func (r *Recurrence) Upcoming(after int64, n int) []int64 {
	var out []int64
	t := after
	for len(out) < n {
		at, ok := r.NextOccurrence(t)
		if !ok {
			break
		}
		out = append(out, at)
		t = at + 60_000
	}
	return out
}

// CreateRecurrence stores a recurring task and computes its first occurrence. The template is
// r.TemplateID, or a new one made from r's content (named after the title, this project); the
// schedule is r.ScheduleID, or a live schedule of the workspace with exactly r's timing, or a new
// one (named after its rule).
func CreateRecurrence(ctx context.Context, q Q, a Actor, r *Recurrence) (*Recurrence, error) {
	var n int
	_ = q.QueryRowContext(ctx, `SELECT count(*) FROM recurrences WHERE project_id = ? AND deleted_at IS NULL`, r.ProjectID).Scan(&n)
	if n >= 200 {
		return nil, Invalid("a project has at most 200 recurring tasks")
	}
	p, err := GetProject(ctx, q, r.ProjectID)
	if err != nil || p.Deleted {
		return nil, Invalid("no such project")
	}
	r.WorkspaceID = p.WorkspaceID
	if r.TemplateID != 0 {
		tpl, err := RecurrenceTemplate(ctx, q, r.WorkspaceID, r.ProjectID, r.TemplateID)
		if err != nil {
			return nil, err
		}
		r.fromTemplate(tpl)
	}
	if r.ScheduleID != 0 {
		s, err := WorkspaceSchedule(ctx, q, r.WorkspaceID, r.ScheduleID)
		if err != nil {
			return nil, err
		}
		r.fromSchedule(s)
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if r.TemplateID == 0 {
		tpl, err := SaveTemplate(ctx, q, a, r.newTemplate(), 0)
		if err != nil {
			return nil, err
		}
		r.fromTemplate(tpl)
	}
	if r.ScheduleID == 0 {
		s, err := scheduleFor(ctx, q, a, r.WorkspaceID, r.Rule())
		if err != nil {
			return nil, err
		}
		r.fromSchedule(s)
	}
	if err := pairFree(ctx, q, r.ProjectID, r.TemplateID, r.ScheduleID, 0); err != nil {
		return nil, err
	}
	if r.StartsOn == 0 {
		r.StartsOn = Now()
	}
	var next any
	if at, ok := r.NextOccurrence(r.StartsOn); ok {
		next = at
	}
	res, err := q.ExecContext(ctx, `INSERT INTO recurrences (workspace_id, project_id, created_by, template_id, schedule_id, starts_on, ends_on,
		max_count, overlap, next_at, created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		r.WorkspaceID, r.ProjectID, a.UserID, r.TemplateID, r.ScheduleID, r.StartsOn, nullInt(r.EndsOn), nullInt(r.MaxCount), r.Overlap, next, Now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if _, err := Activity(ctx, q, a, &r.WorkspaceID, &r.ProjectID, "recurrence.created", "recurrence", id,
		map[string]any{"title": r.Title, "template": r.TemplateID, "schedule": r.ScheduleID}); err != nil {
		return nil, err
	}
	return GetRecurrence(ctx, q, id)
}

// pairFree: a template and a schedule are paired at most once per project.
func pairFree(ctx context.Context, q Q, project, tpl, sched, except int64) error {
	var n int
	_ = q.QueryRowContext(ctx, `SELECT count(*) FROM recurrences WHERE project_id = ? AND template_id = ? AND schedule_id = ? AND deleted_at IS NULL
		AND id != ?`, project, tpl, sched, except).Scan(&n)
	if n > 0 {
		return Conflict("this template already runs on this schedule in this project")
	}
	return nil
}

// RecurrenceTemplate loads a template a recurring task in project (of workspace ws) may use: same
// workspace, and for that project or any project.
func RecurrenceTemplate(ctx context.Context, q Q, ws, project, id int64) (*Template, error) {
	t, err := GetTemplate(ctx, q, id)
	if err != nil || t.WorkspaceID != ws {
		return nil, Invalid("no such template in this workspace")
	}
	if t.ProjectID != nil && *t.ProjectID != project {
		return nil, Invalid("that template is for another project")
	}
	return t, nil
}

// fromTemplate copies a template's content (and identity) into the recurring task.
func (r *Recurrence) fromTemplate(t *Template) {
	r.TemplateID, r.TemplateName, r.TemplateBy = t.ID, t.Name, t.CreatedBy
	r.Title, r.DescriptionMD, r.Priority, r.Color, r.Labels, r.Assignees = t.Title, t.DescriptionMD, t.Priority, t.Color, t.Labels, t.Assignees
	r.Checklist, r.Custom, r.Model, r.Effort, r.AlwaysRun, r.AllowedTools = t.Checklist, t.Custom, t.Model, t.Effort, t.AlwaysRun, t.AllowedTools
}

// fromSchedule copies a schedule's timing (and identity) into the recurring task.
func (r *Recurrence) fromSchedule(s *Schedule) {
	r.ScheduleID, r.ScheduleName = s.ID, s.Name
	r.SetRule(s.Rule)
}

// newTemplate: the template a recurring task made from the task form gets (this project only).
func (r *Recurrence) newTemplate() *Template {
	project := r.ProjectID
	return &Template{WorkspaceID: r.WorkspaceID, ProjectID: &project, Name: templateName(r.Title), Title: r.Title, DescriptionMD: r.DescriptionMD,
		Priority: r.Priority, Color: r.Color, Labels: r.Labels, Assignees: r.Assignees, Checklist: r.Checklist, Custom: r.Custom,
		Model: r.Model, Effort: r.Effort, AlwaysRun: r.AlwaysRun, AllowedTools: r.AllowedTools}
}

// GetRecurrence loads a live recurring task.
func GetRecurrence(ctx context.Context, q Q, id int64) (*Recurrence, error) {
	return scanRecurrence(q.QueryRowContext(ctx, "SELECT "+recCols+" FROM "+recFrom+" WHERE r.id = ? AND r.deleted_at IS NULL", id))
}

// ListRecurrences returns a workspace's recurring tasks (projects nil = all).
func ListRecurrences(ctx context.Context, q Q, ws int64, projects []int64) ([]*Recurrence, error) {
	query, args := "SELECT "+recCols+" FROM "+recFrom+" WHERE r.workspace_id = ? AND r.deleted_at IS NULL", []any{ws}
	if projects != nil {
		query += " AND r.project_id IN (" + placeholders(len(projects)) + ")"
		args = append(args, int64Args(projects)...)
	}
	rows, err := q.QueryContext(ctx, query+" ORDER BY t.title COLLATE NOCASE, r.id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Recurrence{}
	for rows.Next() {
		r, err := scanRecurrence(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RecurrenceChange says what an update does with the template and the schedule: use another one
// (an id), or write r's content / timing into the current one (0; every recurring task sharing it
// follows). A nil Checklist or Custom in r keeps the template's.
type RecurrenceChange struct {
	Template int64
	Schedule int64
}

// UpdateRecurrence replaces a recurring task's own settings (the caller merged the patch), applies
// the template and schedule change, and recomputes the next occurrence from now.
func UpdateRecurrence(ctx context.Context, q Q, a Actor, r *Recurrence, expect int64, ch RecurrenceChange) (*Recurrence, error) {
	if expect != 0 && expect != r.Version {
		return nil, ErrVersion
	}
	cur, err := GetRecurrence(ctx, q, r.ID)
	if err != nil {
		return nil, err
	}
	if ch.Template != 0 {
		tpl, err := RecurrenceTemplate(ctx, q, r.WorkspaceID, r.ProjectID, ch.Template)
		if err != nil {
			return nil, err
		}
		r.fromTemplate(tpl)
	} else {
		if r.Checklist == nil {
			r.Checklist = cur.Checklist
		}
		if r.Custom == nil {
			r.Custom = cur.Custom
		}
	}
	if ch.Schedule != 0 {
		s, err := WorkspaceSchedule(ctx, q, r.WorkspaceID, ch.Schedule)
		if err != nil {
			return nil, err
		}
		r.fromSchedule(s)
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if ch.Template == 0 {
		tpl, err := GetTemplate(ctx, q, cur.TemplateID)
		if err != nil {
			return nil, err
		}
		if tpl.Name == templateName(cur.Title) {
			tpl.Name = templateName(r.Title) // a template named after its task follows the title
		}
		tpl.Title, tpl.DescriptionMD, tpl.Priority, tpl.Color, tpl.Labels, tpl.Assignees = r.Title, r.DescriptionMD, r.Priority, r.Color, r.Labels, r.Assignees
		tpl.Checklist, tpl.Custom, tpl.Model, tpl.Effort, tpl.AlwaysRun = r.Checklist, r.Custom, r.Model, r.Effort, r.AlwaysRun
		if r.AllowedTools != nil {
			tpl.AllowedTools = r.AllowedTools
		}
		if !tpl.sameContent(cur) || tpl.Name != cur.TemplateName {
			if _, err := SaveTemplate(ctx, q, a, tpl, 0); err != nil {
				return nil, err
			}
		}
		r.TemplateID = cur.TemplateID
	}
	if ch.Schedule == 0 {
		r.ScheduleID = cur.ScheduleID
		if !r.Rule().Same(cur.Rule()) {
			s, err := GetSchedule(ctx, q, cur.ScheduleID)
			if err != nil {
				return nil, err
			}
			s.Rule = r.Rule()                                        // SaveSchedule renames it too while its name describes the rule
			if _, err := SaveSchedule(ctx, q, a, s, 0); err != nil { // moves every recurring task on it
				return nil, err
			}
		}
	}
	if err := pairFree(ctx, q, r.ProjectID, r.TemplateID, r.ScheduleID, r.ID); err != nil {
		return nil, err
	}
	var next any
	if at, ok := r.NextOccurrence(maxI(Now(), r.StartsOn)); ok {
		next = at
	}
	var paused any
	if r.Paused {
		paused = Now()
	}
	if _, err := q.ExecContext(ctx, `UPDATE recurrences SET template_id=?, schedule_id=?, starts_on=?, ends_on=?, max_count=?, overlap=?, next_at=?,
		paused_at=?, created_by=?, version = version + 1 WHERE id = ?`,
		r.TemplateID, r.ScheduleID, r.StartsOn, nullInt(r.EndsOn), nullInt(r.MaxCount), r.Overlap, next, paused, a.UserID, r.ID); err != nil {
		return nil, err
	}
	if _, err := Activity(ctx, q, a, &r.WorkspaceID, &r.ProjectID, "recurrence.updated", "recurrence", r.ID,
		map[string]any{"title": r.Title, "template": r.TemplateID, "schedule": r.ScheduleID}); err != nil {
		return nil, err
	}
	return GetRecurrence(ctx, q, r.ID)
}

// sameContent reports whether the template already holds the recurring task's content.
func (t *Template) sameContent(r *Recurrence) bool {
	return t.Title == r.Title && t.DescriptionMD == r.DescriptionMD && t.Priority == r.Priority && eqStrPtr(t.Color, r.Color) &&
		mustJSON(t.Labels) == mustJSON(r.Labels) && mustJSON(t.Assignees) == mustJSON(r.Assignees) &&
		mustJSON(t.Checklist) == mustJSON(r.Checklist) && mustJSON(t.Custom) == mustJSON(r.Custom) &&
		t.Model == r.Model && t.Effort == r.Effort && t.AlwaysRun == r.AlwaysRun && (r.AllowedTools == nil || SameTools(t.AllowedTools, r.AllowedTools))
}

// SameContent reports whether two versions of a recurring task create the same tasks (nil
// checklist/custom in b mean "unchanged").
func SameContent(a, b *Recurrence) bool {
	c := *b
	if c.Checklist == nil {
		c.Checklist = a.Checklist
	}
	if c.Custom == nil {
		c.Custom = a.Custom
	}
	if c.Labels == nil {
		c.Labels = []string{}
	}
	if c.Assignees == nil {
		c.Assignees = []int64{}
	}
	t := Template{Title: a.Title, DescriptionMD: a.DescriptionMD, Priority: a.Priority, Color: a.Color, Labels: a.Labels, Assignees: a.Assignees,
		Checklist: a.Checklist, Custom: a.Custom, Model: a.Model, Effort: a.Effort, AlwaysRun: a.AlwaysRun, AllowedTools: a.AllowedTools}
	return t.sameContent(&c)
}

func eqStrPtr(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// DeleteRecurrence stops a recurring task (its tasks, template and schedule stay).
func DeleteRecurrence(ctx context.Context, q Q, a Actor, r *Recurrence) error {
	if _, err := q.ExecContext(ctx, `UPDATE recurrences SET deleted_at = ?, next_at = NULL, version = version + 1 WHERE id = ?`, Now(), r.ID); err != nil {
		return err
	}
	_, err := Activity(ctx, q, a, &r.WorkspaceID, &r.ProjectID, "recurrence.deleted", "recurrence", r.ID, map[string]any{"title": r.Title})
	return err
}

func maxI(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// JanitorResult reports what one housekeeping pass did.
type JanitorResult struct {
	Created int
	Skipped int
	Missed  int
	Stale   int      // runs closed as interrupted: no heartbeat for StaleRunAfter
	Paused  []int64  // schedules paused because creating their task failed
	Errors  []string // what went wrong (logged by the caller); one bad row never stops the rest
}

// Housekeeping runs the scheduled data work: create due occurrences, mark missed windows. It only
// writes rows inside gigg (no processes, no outbound calls).
func (s *Store) Housekeeping(ctx context.Context, now int64) (JanitorResult, error) {
	var res JanitorResult
	// idempotency replays expire after a day (own statement: nothing below may hold it back)
	if _, err := s.W.ExecContext(ctx, "DELETE FROM idempotency_keys WHERE created_at < ?", now-24*3600*1000); err != nil {
		res.Errors = append(res.Errors, "idempotency purge: "+err.Error())
	}
	if n, err := closeStaleRuns(ctx, s, now); err != nil {
		res.Errors = append(res.Errors, "stale runs: "+err.Error())
	} else {
		res.Stale = n
	}
	err := s.Tx(ctx, func(tx *sql.Tx) error {
		// missed first: a task whose window closed must not count as "still open" for overlap
		res.Missed = markMissed(ctx, tx, now, &res)
		rows, err := tx.QueryContext(ctx, "SELECT "+recCols+" FROM "+recFrom+` JOIN projects p ON p.id = r.project_id
			WHERE r.deleted_at IS NULL AND r.paused_at IS NULL AND p.deleted_at IS NULL AND p.archived_at IS NULL
			AND r.next_at IS NOT NULL AND r.next_at - s.lead_minutes * 60000 <= ?`, now)
		if err != nil {
			return err
		}
		var due []*Recurrence
		for rows.Next() {
			r, err := scanRecurrence(rows)
			if err != nil {
				rows.Close()
				return err
			}
			due = append(due, r)
		}
		rows.Close()
		for _, r := range due {
			if !schedulerMayRun(ctx, tx, r) {
				continue // its owner may no longer run schedules here: wait (windows that pass are skipped later)
			}
			// each schedule in its own savepoint: a failure pauses that schedule and the rest go on
			if _, err := tx.ExecContext(ctx, "SAVEPOINT rec"); err != nil {
				return err
			}
			c, sk, err := materialize(ctx, tx, r, now)
			if err != nil {
				if _, e := tx.ExecContext(ctx, "ROLLBACK TO rec"); e != nil {
					return e
				}
				res.Errors = append(res.Errors, fmt.Sprintf("recurrence %d: %v (paused)", r.ID, err))
				res.Paused = append(res.Paused, r.ID)
				if _, e := tx.ExecContext(ctx, `UPDATE recurrences SET paused_at = ?, version = version + 1 WHERE id = ?`, now, r.ID); e != nil {
					return e
				}
				if _, e := Activity(ctx, tx, Actor{Listener: "scheduler"}, &r.WorkspaceID, &r.ProjectID, "recurrence.paused", "recurrence", r.ID,
					map[string]any{"reason": err.Error()}); e != nil {
					return e
				}
			} else {
				res.Created += c
				res.Skipped += sk
			}
			if _, err := tx.ExecContext(ctx, "RELEASE rec"); err != nil {
				return err
			}
		}
		return nil
	})
	return res, err
}

// schedulerMayRun: the schedule's owner is still active, still a member (or an admin), and still
// allowed to create tasks and run schedules (and to make always-run tasks, or tasks with a tool
// allowlist, when the template has them).
func schedulerMayRun(ctx context.Context, q Q, r *Recurrence) bool {
	u, g, err := UserGrants(ctx, q, r.CreatedBy)
	if err != nil || u.Disabled {
		return false
	}
	return g.Has(r.WorkspaceID, "schedules") && g.Has(r.WorkspaceID, "create_tasks") && g.Has(r.WorkspaceID, "edit_tasks") &&
		(!r.AlwaysRun || g.Has(r.WorkspaceID, "always_run")) && (len(r.AllowedTools) == 0 || g.Has(r.WorkspaceID, "allowed_tools"))
}

// materialize creates (or skips) every occurrence of r that is due, oldest first, at most 31 per pass.
func materialize(ctx context.Context, tx *sql.Tx, r *Recurrence, now int64) (created, skipped int, err error) {
	sys := Actor{UserID: r.CreatedBy, Listener: "scheduler"}
	for i := 0; i < 31 && r.NextAt != nil && *r.NextAt-r.LeadMinutes*60000 <= now; i++ {
		at := *r.NextAt
		end := at + r.WindowMinutes*60000
		if r.MaxCount != nil && r.CreatedCount >= *r.MaxCount {
			r.NextAt = nil
			break
		}
		open := 0
		if r.Overlap == "skip" {
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM tasks t JOIN statuses s ON s.id = t.status_id WHERE t.recurrence_id = ?
				AND t.deleted_at IS NULL AND t.missed_at IS NULL AND s.category NOT IN ('done','cancelled')`, r.ID).Scan(&open); err != nil {
				return created, skipped, err
			}
		}
		switch {
		case end <= now:
			skipped++ // its whole window already passed (e.g. the server was down): don't create a stale task
			r.SkippedCount++
		case open > 0:
			skipped++
			r.SkippedCount++
		default:
			start, due := at, end
			t, err := CreateTask(ctx, tx, sys, NewTask{ProjectID: r.ProjectID, Title: r.Title, DescriptionMD: r.DescriptionMD, Priority: r.Priority,
				Color: r.Color, StartAt: &start, DueAt: &due, NotAfter: &due, Labels: r.Labels, Assignees: validAssignees(ctx, tx, r),
				RecurrenceID: &r.ID, OccurrenceAt: &start, Model: r.Model, Effort: r.Effort, AlwaysRun: r.AlwaysRun,
				AllowedTools: r.AllowedTools})
			if err != nil {
				return created, skipped, err
			}
			if err := templateExtras(ctx, tx, sys, t, r.Checklist, r.Custom); err != nil {
				return created, skipped, err
			}
			created++
			r.CreatedCount++
			r.LastTaskID = &t.ID
		}
		next, ok := r.NextOccurrence(at + 60_000)
		if ok {
			r.NextAt = &next
		} else {
			r.NextAt = nil
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE recurrences SET next_at = ?, created_count = ?, skipped_count = ?, last_task_id = ? WHERE id = ?`,
		nullInt(r.NextAt), r.CreatedCount, r.SkippedCount, nullInt(r.LastTaskID), r.ID)
	return created, skipped, err
}

// validAssignees drops template assignees who are no longer members (or admins).
func validAssignees(ctx context.Context, q Q, r *Recurrence) []int64 {
	out := []int64{}
	for _, u := range r.Assignees {
		if _, g, err := UserGrants(ctx, q, u); err == nil && g.Has(r.WorkspaceID, "view") {
			out = append(out, u)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// markMissed: tasks still in todo/backlog whose window closed are marked missed and moved to the
// waiting column (the inbox), with a system comment.
func markMissed(ctx context.Context, tx *sql.Tx, now int64, res *JanitorResult) int {
	rows, err := tx.QueryContext(ctx, `SELECT t.id FROM tasks t JOIN statuses s ON s.id = t.status_id JOIN projects p ON p.id = t.project_id
		WHERE t.not_after IS NOT NULL AND t.not_after <= ? AND t.missed_at IS NULL AND t.deleted_at IS NULL AND t.archived_at IS NULL
		AND s.category IN ('backlog','todo') LIMIT 200`, now)
	if err != nil {
		res.Errors = append(res.Errors, "missed: "+err.Error())
		return 0
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	n := 0
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, "SAVEPOINT miss"); err != nil {
			res.Errors = append(res.Errors, "missed: "+err.Error())
			return n
		}
		if err := missOne(ctx, tx, id, now); err != nil {
			// the extras failed (note, bell, move): roll them back but still mark it missed, so the
			// same task isn't retried every minute
			_, _ = tx.ExecContext(ctx, "ROLLBACK TO miss")
			_, _ = tx.ExecContext(ctx, `UPDATE tasks SET missed_at = ?, version = version + 1 WHERE id = ?`, now, id)
			res.Errors = append(res.Errors, fmt.Sprintf("missed task %d: %v", id, err))
		}
		_, _ = tx.ExecContext(ctx, "RELEASE miss")
		n++
	}
	return n
}

func missOne(ctx context.Context, tx *sql.Tx, id, now int64) error {
	t, err := GetTask(ctx, tx, id)
	if err != nil {
		return err
	}
	sys := Actor{UserID: t.CreatedBy, Listener: "scheduler"}
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET missed_at = ?, version = version + 1 WHERE id = ?`, now, id); err != nil {
		return err
	}
	loc := time.UTC // the schedule's zone when there is one, so the note reads in local time
	var tz sql.NullString
	if t.RecurrenceID != nil {
		_ = tx.QueryRowContext(ctx, `SELECT s.tz FROM recurrences r JOIN schedules s ON s.id = r.schedule_id WHERE r.id = ?`, *t.RecurrenceID).Scan(&tz)
		if l, err := time.LoadLocation(tz.String); err == nil && tz.Valid {
			loc = l
		}
	}
	if _, err := AddComment(ctx, tx, sys, t, "system", fmt.Sprintf("Missed its window: nobody started it before %s.",
		time.UnixMilli(*t.NotAfter).In(loc).Format("Mon 2006-01-02 15:04 MST")), nil); err != nil {
		return err
	}
	cur, err := GetTask(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := Notify(ctx, tx, Actor{Listener: "scheduler"}, cur, nil, "missed", append([]int64{cur.CreatedBy}, cur.Assignees...)); err != nil {
		return err
	}
	if _, err := MoveTask(ctx, tx, sys, cur, 0, Move{Category: "waiting"}); err != nil && !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrConflict) {
		return err // no waiting status, or an archived project: the task stays where it is
	}
	return nil
}

// StaleRunAfter: a run that has sent no heartbeat (start, progress) for this long is closed as
// interrupted, so a runner that died doesn't leave it "running" for ever (set from GIGG_STALE_RUN_HOURS).
var StaleRunAfter = 6 * time.Hour

// closeStaleRuns closes the runs that went quiet; their tasks stay where they are, for a person or the
// next runner pass to pick up.
func closeStaleRuns(ctx context.Context, s *Store, now int64) (int, error) {
	n := 0
	err := s.Tx(ctx, func(tx *sql.Tx) error {
		cutoff := now - StaleRunAfter.Milliseconds()
		rows, err := tx.QueryContext(ctx, `SELECT r.id, r.workspace_id, r.task_id, t.project_id, r.started_at FROM runs r JOIN tasks t ON t.id = r.task_id
			WHERE r.status = 'running' AND r.heartbeat_at < ?`, cutoff)
		if err != nil {
			return err
		}
		type stale struct{ id, ws, task, project, started int64 }
		var list []stale
		for rows.Next() {
			var x stale
			if err := rows.Scan(&x.id, &x.ws, &x.task, &x.project, &x.started); err != nil {
				rows.Close()
				return err
			}
			list = append(list, x)
		}
		rows.Close()
		reason := fmt.Sprintf("no heartbeat for %s: closed by the server", StaleRunAfter)
		for _, x := range list {
			if _, err := tx.ExecContext(ctx, `UPDATE runs SET status = 'interrupted', ended_at = ?, duration_ms = ?, error_reason = ?, version = version + 1
				WHERE id = ? AND status = 'running'`, now, now-x.started, reason, x.id); err != nil {
				return err
			}
			if _, err := Activity(ctx, tx, Actor{Listener: "scheduler"}, &x.ws, &x.project, "run.stale", "run", x.id, map[string]any{"task_id": x.task, "reason": reason}); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}
