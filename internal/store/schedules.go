package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Rule is when occurrences happen: the frequency, the days, and the time window in a zone.
type Rule struct {
	Freq          string `json:"freq" enum:"minutes,hours,daily,weekly,monthly" doc:"minutes and hours: crons only"`
	Interval      int64  `json:"interval" doc:"Every N minutes (1-360), hours (1-24), days, weeks or months"`
	Weekdays      []int  `json:"weekdays" doc:"Weekly: 1=Mon .. 7=Sun; minutes/hours: only on these days (empty: every day)"`
	MonthDay      *int64 `json:"month_day" doc:"Monthly: 1-31, -1 = last day (short months use their last day)"`
	TZ            string `json:"tz" doc:"IANA time zone"`
	WindowStart   string `json:"window_start" doc:"When the window opens, local HH:MM"`
	WindowMinutes int64  `json:"window_minutes" doc:"How long the window stays open"`
	LeadMinutes   int64  `json:"lead_minutes" doc:"Create the task this long before the window opens"`
}

// DefaultTZ is the time zone of rules that name none (the server sets it from GIGG_DEFAULT_TZ; the API
// fills in the user's own preference first).
var DefaultTZ = "UTC"

// Validate checks and normalises a rule (defaults: every 1, 00:00 for 24 h, DefaultTZ).
func (r *Rule) Validate() error {
	if r.Interval == 0 {
		r.Interval = 1
	}
	if r.Interval < 1 || r.Interval > 365 {
		return Invalid("interval: 1-365")
	}
	if r.TZ == "" {
		r.TZ = DefaultTZ
	}
	if _, err := time.LoadLocation(r.TZ); err != nil {
		return Invalid("unknown time zone %q", r.TZ)
	}
	if r.WindowStart == "" {
		r.WindowStart = "00:00"
	}
	if _, err := time.Parse("15:04", r.WindowStart); err != nil {
		return Invalid("window_start is HH:MM")
	}
	if r.WindowMinutes == 0 {
		r.WindowMinutes = 1440
	}
	if r.WindowMinutes < 5 || r.WindowMinutes > 44640 {
		return Invalid("window_minutes: 5 to 44640 (31 days)")
	}
	if r.LeadMinutes < 0 || r.LeadMinutes > 10080 {
		return Invalid("lead_minutes: 0 to 10080 (7 days)")
	}
	switch r.Freq {
	case "minutes", "hours":
		// ticks every N from window_start while the window is open, every day or on the weekdays given
		if r.Freq == "minutes" && r.Interval > 360 {
			return Invalid("minutes: every 1-360 minutes")
		}
		if r.Freq == "hours" && r.Interval > 24 {
			return Invalid("hours: every 1-24 hours")
		}
		if r.WindowMinutes > 1440 {
			return Invalid("minute and hour schedules: the daily window is at most 1440 minutes")
		}
		r.MonthDay, r.LeadMinutes = nil, 0
		days, err := normWeekdays(r.Weekdays)
		if err != nil {
			return err
		}
		r.Weekdays = days
	case "daily":
		r.Weekdays, r.MonthDay = []int{}, nil
	case "weekly":
		if len(r.Weekdays) == 0 {
			return Invalid("weekly schedules need at least one weekday (1=Mon .. 7=Sun)")
		}
		seen := map[int]bool{}
		for _, d := range r.Weekdays {
			if d < 1 || d > 7 {
				return Invalid("weekdays are 1 (Mon) to 7 (Sun)")
			}
			seen[d] = true
		}
		days := []int{}
		for d := 1; d <= 7; d++ {
			if seen[d] {
				days = append(days, d)
			}
		}
		r.Weekdays, r.MonthDay = days, nil
	case "monthly":
		if r.MonthDay == nil || *r.MonthDay == 0 || *r.MonthDay < -1 || *r.MonthDay > 31 {
			return Invalid("monthly schedules need month_day 1-31 or -1 (last day)")
		}
		r.Weekdays = []int{}
	default:
		return Invalid("freq is minutes, hours, daily, weekly or monthly")
	}
	return nil
}

// Frequent reports a minute or hour rule (crons only).
func (r Rule) Frequent() bool { return r.Freq == "minutes" || r.Freq == "hours" }

func normWeekdays(in []int) ([]int, error) {
	seen := map[int]bool{}
	for _, d := range in {
		if d < 1 || d > 7 {
			return nil, Invalid("weekdays are 1 (Mon) to 7 (Sun)")
		}
		seen[d] = true
	}
	days := []int{}
	for d := 1; d <= 7; d++ {
		if seen[d] {
			days = append(days, d)
		}
	}
	return days, nil
}

// Step is the time between two slots of a rule (minutes/hours: the interval; otherwise a day).
func (r Rule) Step() time.Duration {
	switch r.Freq {
	case "minutes":
		return time.Duration(max(r.Interval, 1)) * time.Minute
	case "hours":
		return time.Duration(max(r.Interval, 1)) * time.Hour
	}
	return 24 * time.Hour
}

// Next returns the first slot at or after `after` (unix ms): for minute/hour rules a tick, otherwise
// the start of an occurrence window. startsOn anchors "every N days/weeks/months".
func (r Rule) Next(startsOn, after int64) (int64, bool) {
	if !r.Frequent() {
		rec := &Recurrence{StartsOn: startsOn}
		rec.SetRule(r)
		return rec.NextOccurrence(after)
	}
	loc, err := time.LoadLocation(r.TZ)
	if err != nil {
		return 0, false
	}
	hm, err := time.Parse("15:04", r.WindowStart)
	if err != nil {
		return 0, false
	}
	step := r.Step().Milliseconds()
	from := time.UnixMilli(after).In(loc)
	for i := -1; i < 9; i++ { // yesterday's window may run past midnight
		d := time.Date(from.Year(), from.Month(), from.Day()+i, 12, 0, 0, 0, loc)
		if len(r.Weekdays) > 0 {
			wd := int(d.Weekday())
			if wd == 0 {
				wd = 7
			}
			if !slices.Contains(r.Weekdays, wd) {
				continue
			}
		}
		open := time.Date(d.Year(), d.Month(), d.Day(), hm.Hour(), hm.Minute(), 0, 0, loc).UnixMilli()
		end := open + r.WindowMinutes*60_000
		t := open
		if after > open {
			t = open + (after-open+step-1)/step*step
		}
		if t < end {
			return t, true
		}
	}
	return 0, false
}

// Same reports two rules that create the same occurrences (both validated).
func (r Rule) Same(o Rule) bool {
	md := func(p *int64) int64 {
		if p == nil {
			return 0
		}
		return *p
	}
	return r.Freq == o.Freq && r.Interval == o.Interval && slices.Equal(r.Weekdays, o.Weekdays) && md(r.MonthDay) == md(o.MonthDay) &&
		r.TZ == o.TZ && r.WindowStart == o.WindowStart && r.WindowMinutes == o.WindowMinutes && r.LeadMinutes == o.LeadMinutes
}

var weekdayNames = []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}

// Describe names a rule: "Every day all day", "Every weekday 09:00–11:00", "Every 2 weeks on Mon, Thu
// from 22:00 for 4 h", "Monthly on the last day 09:00–10:00" (the zone and create-early appended).
func (r Rule) Describe() string {
	n := r.Interval
	if n < 1 {
		n = 1
	}
	var s string
	switch r.Freq {
	case "minutes", "hours":
		unit := map[string]string{"minutes": "min", "hours": "h"}[r.Freq]
		s = fmt.Sprintf("Every %d %s", n, unit)
		if r.Freq == "hours" && n == 1 {
			s = "Every hour"
		}
		if len(r.Weekdays) > 0 && len(r.Weekdays) < 7 {
			names := []string{}
			for _, d := range r.Weekdays {
				names = append(names, weekdayNames[d-1])
			}
			s += " on " + strings.Join(names, ", ")
			if fmt.Sprint(r.Weekdays) == "[1 2 3 4 5]" {
				s = strings.TrimSuffix(s, " on Mon, Tue, Wed, Thu, Fri") + " on weekdays"
			}
		}
		if w := windowText(r.WindowStart, r.WindowMinutes); w != "" && w != "all day" {
			s += ", " + w
		}
		if r.TZ != "" && r.TZ != DefaultTZ {
			s += " (" + r.TZ + ")"
		}
		return templateName(s)
	case "daily":
		s = "Every day"
		if n > 1 {
			s = fmt.Sprintf("Every %d days", n)
		}
	case "weekly":
		key := fmt.Sprint(r.Weekdays)
		var days string
		switch key {
		case "[1 2 3 4 5]":
			days = "weekday"
		case "[6 7]":
			days = "weekend day"
		case "[1 2 3 4 5 6 7]":
			days = "day"
		default:
			names := []string{}
			for _, d := range r.Weekdays {
				if d >= 1 && d <= 7 {
					names = append(names, weekdayNames[d-1])
				}
			}
			days = strings.Join(names, ", ")
		}
		if n == 1 {
			s = "Every " + days
		} else {
			plural := map[string]string{"weekday": "weekdays", "weekend day": "weekends", "day": "every day"}[days]
			if plural == "" {
				plural = days
			}
			s = fmt.Sprintf("Every %d weeks on %s", n, plural)
		}
	case "monthly":
		on := "the last day"
		if r.MonthDay != nil && *r.MonthDay > 0 {
			on = "the " + ordinal(*r.MonthDay)
		}
		s = "Monthly on " + on
		if n > 1 {
			s = fmt.Sprintf("Every %d months on %s", n, on)
		}
	}
	s += " " + windowText(r.WindowStart, r.WindowMinutes)
	if r.LeadMinutes > 0 {
		s += fmt.Sprintf(", %d min early", r.LeadMinutes)
	}
	if r.TZ != "" && r.TZ != DefaultTZ {
		s += " (" + r.TZ + ")"
	}
	return templateName(s)
}

// DescribeCron names a rule as a cron reads it: a daily/weekly/monthly cron runs at the window's
// start ("Every day at 03:30"), the window and create-early don't apply.
func (r Rule) DescribeCron() string {
	if r.Frequent() {
		return r.Describe()
	}
	x := r
	x.LeadMinutes = 0
	s := x.Describe()
	if w := windowText(x.WindowStart, x.WindowMinutes); w != "" {
		s = strings.Replace(s, " "+w, " at "+x.WindowStart, 1)
	}
	return s
}

func ordinal(n int64) string {
	suf := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suf = "st"
		case 2:
			suf = "nd"
		case 3:
			suf = "rd"
		}
	}
	return fmt.Sprintf("%d%s", n, suf)
}

func windowText(start string, minutes int64) string {
	t, err := time.Parse("15:04", start)
	if err != nil {
		return ""
	}
	from := int64(t.Hour()*60 + t.Minute())
	if from == 0 && minutes == 1440 {
		return "all day"
	}
	end := from + minutes
	if end <= 1440 {
		return fmt.Sprintf("%s–%02d:%02d", start, end/60, end%60)
	}
	d := fmt.Sprintf("%d min", minutes)
	switch {
	case minutes%1440 == 0:
		d = fmt.Sprintf("%d d", minutes/1440)
	case minutes%60 == 0:
		d = fmt.Sprintf("%d h", minutes/60)
	case minutes > 60:
		d = fmt.Sprintf("%d h %d min", minutes/60, minutes%60)
	}
	return "from " + start + " for " + d
}

// Schedule is a named rule of a workspace; recurring tasks pair it with a template.
type Schedule struct {
	ID          int64  `json:"id"`
	WorkspaceID int64  `json:"workspace_id"`
	Name        string `json:"name"`
	Rule
	CreatedBy int64 `json:"created_by"`
	CreatedAt int64 `json:"created_at"`
	Version   int64 `json:"version"`
	Uses      int64 `json:"uses" doc:"Live recurring tasks using this schedule"`
	CronUses  int64 `json:"cron_uses" doc:"Live crons using this schedule"`
}

// schColsPre13 is the column list without the cron count (migration steps before 0013 add ", 0").
const schColsPre13 = `id, workspace_id, name, freq, interval_n, weekdays, month_day, tz, window_start, window_minutes, lead_minutes, created_by, created_at, version,
	(SELECT count(*) FROM recurrences r WHERE r.schedule_id = schedules.id AND r.deleted_at IS NULL)`

const schCols = schColsPre13 + `, (SELECT count(*) FROM crons c WHERE c.schedule_id = schedules.id AND c.deleted_at IS NULL)`

func scanSchedule(row interface{ Scan(...any) error }) (*Schedule, error) {
	s := &Schedule{}
	var weekdays string
	var md sql.NullInt64
	if err := row.Scan(&s.ID, &s.WorkspaceID, &s.Name, &s.Freq, &s.Interval, &weekdays, &md, &s.TZ, &s.WindowStart, &s.WindowMinutes, &s.LeadMinutes,
		&s.CreatedBy, &s.CreatedAt, &s.Version, &s.Uses, &s.CronUses); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	s.MonthDay = ptrInt(md)
	_ = json.Unmarshal([]byte(weekdays), &s.Weekdays)
	if s.Weekdays == nil {
		s.Weekdays = []int{}
	}
	return s, nil
}

// ListSchedules returns a workspace's live schedules, by name.
func ListSchedules(ctx context.Context, q Q, ws int64) ([]*Schedule, error) {
	rows, err := q.QueryContext(ctx, "SELECT "+schCols+" FROM schedules WHERE workspace_id = ? AND deleted_at IS NULL ORDER BY name COLLATE NOCASE, id", ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Schedule{}
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetSchedule loads a live schedule.
func GetSchedule(ctx context.Context, q Q, id int64) (*Schedule, error) {
	return scanSchedule(q.QueryRowContext(ctx, "SELECT "+schCols+" FROM schedules WHERE id = ? AND deleted_at IS NULL", id))
}

// WorkspaceSchedule loads a live schedule of workspace ws (Invalid otherwise).
func WorkspaceSchedule(ctx context.Context, q Q, ws, id int64) (*Schedule, error) {
	s, err := GetSchedule(ctx, q, id)
	if err != nil || s.WorkspaceID != ws {
		return nil, Invalid("no such schedule in this workspace")
	}
	return s, nil
}

// scheduleFor returns a live schedule of the workspace with exactly this rule, or makes one named
// after it.
func scheduleFor(ctx context.Context, q Q, a Actor, ws int64, rule Rule) (*Schedule, error) {
	ss, err := ListSchedules(ctx, q, ws)
	if err != nil {
		return nil, err
	}
	for _, s := range ss {
		if s.Rule.Same(rule) {
			return s, nil
		}
	}
	return SaveSchedule(ctx, q, a, &Schedule{WorkspaceID: ws, Name: rule.Describe(), Rule: rule}, 0)
}

// SaveSchedule creates (ID 0) or replaces a schedule. A changed rule moves every recurring task on
// it: their next occurrence is recomputed from now.
func SaveSchedule(ctx context.Context, q Q, a Actor, s *Schedule, expect int64) (*Schedule, error) {
	if err := s.Rule.Validate(); err != nil {
		return nil, err
	}
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" {
		s.Name = s.Rule.Describe()
	}
	if utf8.RuneCountInString(s.Name) > 80 {
		return nil, Invalid("schedule name: 1-80 characters")
	}
	action := "schedule.updated"
	if s.ID == 0 {
		var n int
		if err := q.QueryRowContext(ctx, `SELECT count(*) FROM schedules WHERE workspace_id = ? AND deleted_at IS NULL`, s.WorkspaceID).Scan(&n); err != nil {
			return nil, err
		}
		if n >= 200 {
			return nil, Invalid("a workspace has at most 200 schedules")
		}
		res, err := q.ExecContext(ctx, `INSERT INTO schedules (workspace_id, name, freq, interval_n, weekdays, month_day, tz, window_start, window_minutes,
			lead_minutes, created_by, created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, s.WorkspaceID, s.Name, s.Freq, s.Interval, mustJSON(s.Weekdays),
			nullInt(s.MonthDay), s.TZ, s.WindowStart, s.WindowMinutes, s.LeadMinutes, a.UserID, Now())
		if err != nil {
			return nil, err
		}
		s.ID, _ = res.LastInsertId()
		action = "schedule.created"
	} else {
		cur, err := GetSchedule(ctx, q, s.ID)
		if err != nil {
			return nil, err
		}
		if expect != 0 && expect != cur.Version {
			return nil, ErrVersion
		}
		if s.Rule.Frequent() && cur.Uses > 0 {
			return nil, Invalid("recurring tasks use this schedule: minute and hour schedules are for crons only")
		}
		if s.Name == cur.Name && cur.Name == cur.Rule.Describe() {
			s.Name = s.Rule.Describe() // a name describing the rule follows it (unless renamed)
		}
		if _, err := q.ExecContext(ctx, `UPDATE schedules SET name = ?, freq = ?, interval_n = ?, weekdays = ?, month_day = ?, tz = ?, window_start = ?,
			window_minutes = ?, lead_minutes = ?, version = version + 1 WHERE id = ?`, s.Name, s.Freq, s.Interval, mustJSON(s.Weekdays), nullInt(s.MonthDay),
			s.TZ, s.WindowStart, s.WindowMinutes, s.LeadMinutes, s.ID); err != nil {
			return nil, err
		}
		if !cur.Rule.Same(s.Rule) {
			if err := rescheduleAll(ctx, q, s.ID); err != nil {
				return nil, err
			}
		}
	}
	if _, err := Activity(ctx, q, a, &s.WorkspaceID, nil, action, "schedule", s.ID, map[string]any{"name": s.Name}); err != nil {
		return nil, err
	}
	return GetSchedule(ctx, q, s.ID)
}

// rescheduleAll recomputes the next occurrence of every live recurring task on a schedule.
func rescheduleAll(ctx context.Context, q Q, schedule int64) error {
	rows, err := q.QueryContext(ctx, "SELECT "+recCols+" FROM "+recFrom+" WHERE r.schedule_id = ? AND r.deleted_at IS NULL", schedule)
	if err != nil {
		return err
	}
	var rs []*Recurrence
	for rows.Next() {
		r, err := scanRecurrence(rows)
		if err != nil {
			rows.Close()
			return err
		}
		rs = append(rs, r)
	}
	rows.Close()
	for _, r := range rs {
		var next any
		if r.MaxCount == nil || r.CreatedCount < *r.MaxCount {
			if at, ok := r.NextOccurrence(maxI(Now(), r.StartsOn)); ok {
				next = at
			}
		}
		if _, err := q.ExecContext(ctx, `UPDATE recurrences SET next_at = ?, version = version + 1 WHERE id = ?`, next, r.ID); err != nil {
			return err
		}
	}
	return rescheduleCrons(ctx, q, "schedule_id", schedule)
}

// DeleteSchedule removes a schedule no recurring task uses.
func DeleteSchedule(ctx context.Context, q Q, a Actor, s *Schedule) error {
	var names []string
	rows, err := q.QueryContext(ctx, `SELECT t.title FROM recurrences r JOIN task_templates t ON t.id = r.template_id
		WHERE r.schedule_id = ? AND r.deleted_at IS NULL ORDER BY r.id LIMIT 10`, s.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var t string
		if rows.Scan(&t) == nil {
			names = append(names, fmt.Sprintf("%q", t))
		}
	}
	if err := rows.Err(); err != nil { // fail closed: an unread list must not allow the delete
		rows.Close()
		return err
	}
	rows.Close()
	if len(names) > 0 {
		return Conflict("recurring tasks use this schedule: %s. Delete them or give them another schedule first", strings.Join(names, ", "))
	}
	if s.CronUses > 0 {
		return Conflict("%d cron(s) use this schedule. Delete them or give them another schedule first", s.CronUses)
	}
	if _, err := q.ExecContext(ctx, `UPDATE schedules SET deleted_at = ?, version = version + 1 WHERE id = ?`, Now(), s.ID); err != nil {
		return err
	}
	_, err = Activity(ctx, q, a, &s.WorkspaceID, nil, "schedule.deleted", "schedule", s.ID, map[string]any{"name": s.Name})
	return err
}

// ScheduleGuards: whether a live recurring task on the schedule makes always-run tasks, or tasks with a
// tool allowlist (changing the schedule then needs those permissions).
func ScheduleGuards(ctx context.Context, q Q, id int64) (alwaysRun, tools bool, err error) {
	err = q.QueryRowContext(ctx, `SELECT COALESCE(MAX(t.always_run), 0), COALESCE(MAX(t.allowed_tools != '[]'), 0)
		FROM recurrences r JOIN task_templates t ON t.id = r.template_id WHERE r.schedule_id = ? AND r.deleted_at IS NULL`, id).Scan(&alwaysRun, &tools)
	return alwaysRun, tools, err
}
