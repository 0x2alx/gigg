package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Metrics: counts and sums over a time range, cut into day, week or month buckets in one time zone.
// The callers decide what a reader may see (MetricsScope); this file only counts. Every figure comes
// from rows that already exist: gigs (created_at, done_at, archived_at, occurrence_at, missed_at),
// the activity log (a gig moved into a cancelled status: cancelling stores no time of its own), runs
// and errand runs.

// Limits of one query.
const (
	MetricsMaxDays    = 400   // the longest range
	metricsMaxCycle   = 50000 // gigs read for the median cycle time, per workspace
	metricsMaxGroupBy = 50    // rows of each by_agent, by_model and by_project table
	metricsMaxBuckets = 400   // buckets of a range (a day per bucket for 400 days)
	msDay             = 86400000
)

// MetricsScope is what a caller may read of one workspace.
type MetricsScope struct {
	WorkspaceID int64
	Projects    []int64 // nil: every project; otherwise only these (empty: none, the scope is skipped)
	Errands     bool    // errand runs too (they belong to the workspace, not to a project)
}

// MetricsQuery is one request: a range [From, To) in unix ms, bucketed in Loc.
type MetricsQuery struct {
	Scopes     []MetricsScope
	From, To   int64
	Bucket     string // day | week | month
	Loc        *time.Location
	Assignee   int64  // 0: anyone. Gigs: assigned to this user; runs and errand runs: done by this user
	WorkerKind string // "", human or agent: the same, for the kind of user
}

// MetricPoint is one bucket's figures, or the whole range's.
type MetricPoint struct {
	GigsCreated   int64 `json:"gigs_created"`
	GigsDone      int64 `json:"gigs_done" doc:"Gigs that reached done in the period (a reopened and finished gig counts again)"`
	GigsCancelled int64 `json:"gigs_cancelled" doc:"Gigs moved into a cancelled status in the period (from the activity log)"`
	GigsArchived  int64 `json:"gigs_archived"`

	Runs            int64 `json:"runs" doc:"Agent runs started"`
	RunsDone        int64 `json:"runs_done"`
	RunsNeedsHuman  int64 `json:"runs_needs_human"`
	RunsError       int64 `json:"runs_error"`
	RunsTimeout     int64 `json:"runs_timeout"`
	RunsInterrupted int64 `json:"runs_interrupted"`
	RunsParked      int64 `json:"runs_parked"`
	RunsRunning     int64 `json:"runs_running"`
	RunMS           int64 `json:"run_ms" doc:"Total time of the runs that have a duration"`
	RunsTimed       int64 `json:"runs_timed" doc:"Runs that have a duration"`

	CostUSD     float64 `json:"cost_usd"`
	TokensIn    int64   `json:"tokens_in"`
	TokensOut   int64   `json:"tokens_out"`
	TokensCache int64   `json:"tokens_cache" doc:"Cache reads and writes"`

	ErrandRuns       int64 `json:"errand_runs" doc:"Errand runs recorded (including missed and skipped ones)"`
	ErrandOK         int64 `json:"errand_ok"`
	ErrandFailed     int64 `json:"errand_failed"`
	ErrandTimeout    int64 `json:"errand_timeout"`
	ErrandMissed     int64 `json:"errand_missed"`
	ErrandSkipped    int64 `json:"errand_skipped"`
	ErrandMS         int64 `json:"errand_ms"`
	ErrandOutputByte int64 `json:"errand_output_bytes"`

	RecurringOnTime int64 `json:"recurring_on_time" doc:"Recurring gigs whose window opened in the period and that were done in time"`
	RecurringMissed int64 `json:"recurring_missed" doc:"Recurring gigs whose window opened in the period and closed unfinished"`

	CycleCount  int64  `json:"cycle_count" doc:"Gigs done in the period (the sample of the median)"`
	CycleMedian *int64 `json:"cycle_median_ms" doc:"Median time from created to done, ms (null: none done)"`
}

// MetricBucket is a MetricPoint with its place in time. Start and End clip to the range; Date is the
// local date the bucket starts on (the Monday of a week, the 1st of a month), even when clipped.
type MetricBucket struct {
	Date  string `json:"date"`
	Start int64  `json:"start" doc:"Unix ms, inclusive"`
	End   int64  `json:"end" doc:"Unix ms, exclusive"`
	MetricPoint
}

// MetricTotals is the whole range, with the rates the buckets can't add up.
type MetricTotals struct {
	MetricPoint
	OpenNow         int64    `json:"open_now" doc:"Gigs open right now (not done, cancelled, archived or deleted) in the same scope"`
	RunAvgMS        *int64   `json:"run_avg_ms" doc:"Average run duration (null: no timed run)"`
	SuccessRate     *float64 `json:"success_rate" doc:"Of the runs that ended (not running or parked): done"`
	FailureRate     *float64 `json:"failure_rate" doc:"Of the runs that ended: error"`
	TimeoutRate     *float64 `json:"timeout_rate"`
	InterruptedRate *float64 `json:"interrupted_rate"`
}

// MetricGroup is a row of the cost tables: runs, cost and tokens of one agent, model or project.
type MetricGroup struct {
	ID        int64   `json:"id,omitempty"`
	Name      string  `json:"name" doc:"Agent username, model name (empty: not reported) or project key"`
	Label     string  `json:"label,omitempty" doc:"Agent display name or project name"`
	Runs      int64   `json:"runs"`
	CostUSD   float64 `json:"cost_usd"`
	TokensIn  int64   `json:"tokens_in"`
	TokensOut int64   `json:"tokens_out"`
	RunMS     int64   `json:"run_ms"`
}

// MetricsResult is what GET /metrics returns.
type MetricsResult struct {
	From      int64          `json:"from" doc:"Start of the range, unix ms"`
	To        int64          `json:"to" doc:"End of the range, unix ms (exclusive)"`
	Bucket    string         `json:"bucket"`
	TZ        string         `json:"tz"`
	Buckets   []MetricBucket `json:"buckets"`
	Totals    MetricTotals   `json:"totals"`
	ByAgent   []MetricGroup  `json:"by_agent"`
	ByModel   []MetricGroup  `json:"by_model"`
	ByProject []MetricGroup  `json:"by_project"`
	Truncated bool           `json:"truncated" doc:"A very busy workspace: the median cycle time was computed from the first gigs only"`
}

// MetricBuckets cuts [from, to) into buckets of whole local days, Monday-first weeks or calendar
// months in loc. The first and last bucket clip to the range.
func MetricBuckets(from, to int64, bucket string, loc *time.Location) ([]MetricBucket, error) {
	if to <= from {
		return nil, Invalid("the range is empty")
	}
	if loc == nil {
		loc = time.UTC
	}
	start := time.UnixMilli(from).In(loc)
	y, m, d := start.Date()
	cur := time.Date(y, m, d, 0, 0, 0, 0, loc)
	switch bucket {
	case "day":
	case "week":
		cur = cur.AddDate(0, 0, -((int(cur.Weekday()) + 6) % 7))
	case "month":
		cur = time.Date(y, m, 1, 0, 0, 0, 0, loc)
	default:
		return nil, Invalid("bucket is day, week or month")
	}
	var out []MetricBucket
	for cur.UnixMilli() < to {
		var next time.Time
		switch bucket {
		case "day":
			next = cur.AddDate(0, 0, 1)
		case "week":
			next = cur.AddDate(0, 0, 7)
		default:
			next = cur.AddDate(0, 1, 0)
		}
		if len(out) >= metricsMaxBuckets {
			return nil, Invalid("too many buckets: at most %d (use a coarser bucket or a shorter range)", metricsMaxBuckets)
		}
		out = append(out, MetricBucket{Date: cur.Format("2006-01-02"), Start: max(cur.UnixMilli(), from), End: min(next.UnixMilli(), to)})
		cur = next
	}
	return out, nil
}

// bucketCTE: the buckets as a table b(i, s, e) a query can join its time column to.
func bucketCTE(bs []MetricBucket) string {
	var sb strings.Builder
	sb.WriteString("WITH b(i, s, e) AS (VALUES ")
	for i, b := range bs {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "(%d,%d,%d)", i, b.Start, b.End)
	}
	sb.WriteString(") ")
	return sb.String()
}

// metricsCond builds the filter shared by the queries of one scope.
type metricsCond struct {
	where []string
	args  []any
}

func (c *metricsCond) add(w string, args ...any) {
	c.where = append(c.where, w)
	c.args = append(c.args, args...)
}
func (c *metricsCond) sql() string {
	if len(c.where) == 0 {
		return ""
	}
	return " AND " + strings.Join(c.where, " AND ")
}

// scopeCond: the workspace and projects of a scope, on the given columns.
func scopeCond(sc MetricsScope, wsCol, projCol string) *metricsCond {
	c := &metricsCond{}
	c.add(wsCol+" = ?", sc.WorkspaceID)
	if sc.Projects != nil {
		c.add(projCol+" IN ("+placeholders(len(sc.Projects))+")", int64Args(sc.Projects)...)
	}
	return c
}

// gigPeople: the people filters for gigs, by their assignees (idCol holds the gig id).
func (q *MetricsQuery) gigPeople(c *metricsCond, idCol string) {
	if q.Assignee != 0 {
		c.add("EXISTS (SELECT 1 FROM gig_assignees ga WHERE ga.gig_id = "+idCol+" AND ga.user_id = ?)", q.Assignee)
	}
	if q.WorkerKind != "" {
		c.add("EXISTS (SELECT 1 FROM gig_assignees ga JOIN users gu ON gu.id = ga.user_id WHERE ga.gig_id = "+idCol+" AND gu.kind = ?)", q.WorkerKind)
	}
}

// workerCond: the people filters for a column of user ids.
func (q *MetricsQuery) workerCond(c *metricsCond, col string) {
	if q.Assignee != 0 {
		c.add(col+" = ?", q.Assignee)
	}
	if q.WorkerKind != "" {
		c.add(col+" IN (SELECT id FROM users WHERE kind = ?)", q.WorkerKind)
	}
}

// Metrics counts what the query's scopes hold.
func Metrics(ctx context.Context, db Q, q MetricsQuery) (*MetricsResult, error) {
	if q.To-q.From > MetricsMaxDays*msDay+msDay { // a DST day may be 25 hours; the API checks the dates
		return nil, Invalid("the range is longer than %d days", MetricsMaxDays)
	}
	if q.WorkerKind != "" && q.WorkerKind != "human" && q.WorkerKind != "agent" {
		return nil, Invalid("worker_kind is human or agent")
	}
	bs, err := MetricBuckets(q.From, q.To, q.Bucket, q.Loc)
	if err != nil {
		return nil, err
	}
	res := &MetricsResult{From: q.From, To: q.To, Bucket: q.Bucket, TZ: q.Loc.String(), Buckets: bs,
		ByAgent: []MetricGroup{}, ByModel: []MetricGroup{}, ByProject: []MetricGroup{}}
	cte := bucketCTE(bs)
	cycles := make([][]int64, len(bs))
	agents, models, projects := map[int64]*MetricGroup{}, map[string]*MetricGroup{}, map[int64]*MetricGroup{}
	var allCycles []int64

	for _, sc := range q.Scopes {
		if sc.Projects != nil && len(sc.Projects) == 0 {
			continue
		}
		// gigs created, done, archived
		for _, m := range []struct {
			col string
			set func(p *MetricPoint, n int64)
			// extra: conditions on top of the scope
			extra string
		}{
			{"created_at", func(p *MetricPoint, n int64) { p.GigsCreated += n }, "g.deleted_at IS NULL"},
			{"done_at", func(p *MetricPoint, n int64) { p.GigsDone += n }, "g.deleted_at IS NULL"},
			{"archived_at", func(p *MetricPoint, n int64) { p.GigsArchived += n }, "g.deleted_at IS NULL"},
		} {
			c := scopeCond(sc, "g.workspace_id", "g.project_id")
			q.gigPeople(c, "g.id")
			rows, err := db.QueryContext(ctx, cte+`SELECT b.i, count(*) FROM b JOIN gigs g ON g.`+m.col+` >= b.s AND g.`+m.col+` < b.e
				WHERE `+m.extra+c.sql()+` GROUP BY b.i`, c.args...)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var i int
				var n int64
				if err := rows.Scan(&i, &n); err != nil {
					rows.Close()
					return nil, err
				}
				m.set(&res.Buckets[i].MetricPoint, n)
			}
			if err := closeRows(rows); err != nil {
				return nil, err
			}
		}

		// cancelled: gigs moved into a cancelled status (the log has the time; the gig keeps none)
		{
			c := scopeCond(sc, "a.workspace_id", "a.project_id")
			q.gigPeople(c, "a.entity_id")
			rows, err := db.QueryContext(ctx, cte+`SELECT b.i, count(*) FROM b JOIN activity a ON a.at >= b.s AND a.at < b.e
				WHERE a.action = 'gig.moved' AND json_extract(a.data, '$.to_category') = 'cancelled'
				AND COALESCE(json_extract(a.data, '$.from_category'), '') != 'cancelled'`+c.sql()+` GROUP BY b.i`, c.args...)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var i int
				var n int64
				if err := rows.Scan(&i, &n); err != nil {
					rows.Close()
					return nil, err
				}
				res.Buckets[i].GigsCancelled += n
			}
			if err := closeRows(rows); err != nil {
				return nil, err
			}
		}

		// recurring gigs: on time (done, never missed) or missed, by the time their window opened
		{
			c := scopeCond(sc, "g.workspace_id", "g.project_id")
			q.gigPeople(c, "g.id")
			rows, err := db.QueryContext(ctx, cte+`SELECT b.i, count(*) FILTER (WHERE g.missed_at IS NULL AND g.done_at IS NOT NULL),
				count(*) FILTER (WHERE g.missed_at IS NOT NULL)
				FROM b JOIN gigs g ON g.occurrence_at >= b.s AND g.occurrence_at < b.e
				WHERE g.recurrence_id IS NOT NULL AND g.deleted_at IS NULL`+c.sql()+` GROUP BY b.i`, c.args...)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var i int
				var ok, missed int64
				if err := rows.Scan(&i, &ok, &missed); err != nil {
					rows.Close()
					return nil, err
				}
				res.Buckets[i].RecurringOnTime += ok
				res.Buckets[i].RecurringMissed += missed
			}
			if err := closeRows(rows); err != nil {
				return nil, err
			}
		}

		// cycle time: created to done, per bucket, medians taken in Go
		{
			c := scopeCond(sc, "g.workspace_id", "g.project_id")
			q.gigPeople(c, "g.id")
			rows, err := db.QueryContext(ctx, `SELECT g.created_at, g.done_at FROM gigs g
				WHERE g.done_at >= ? AND g.done_at < ? AND g.deleted_at IS NULL`+c.sql()+` ORDER BY g.done_at LIMIT ?`,
				append([]any{q.From, q.To}, append(c.args, metricsMaxCycle+1)...)...)
			if err != nil {
				return nil, err
			}
			n := 0
			for rows.Next() {
				var created, done int64
				if err := rows.Scan(&created, &done); err != nil {
					rows.Close()
					return nil, err
				}
				if n++; n > metricsMaxCycle {
					res.Truncated = true
					break
				}
				d := max(done-created, 0)
				i := sort.Search(len(bs), func(i int) bool { return bs[i].End > done })
				if i < len(bs) {
					cycles[i] = append(cycles[i], d)
				}
				allCycles = append(allCycles, d)
			}
			if err := closeRows(rows); err != nil {
				return nil, err
			}
		}

		// agent runs: by outcome, duration, cost, tokens
		{
			c := scopeCond(sc, "r.workspace_id", "g.project_id")
			q.workerCond(c, "r.worker_user_id")
			where := c.sql()
			rows, err := db.QueryContext(ctx, cte+`SELECT b.i, count(*),
				count(*) FILTER (WHERE r.status = 'done'), count(*) FILTER (WHERE r.status = 'needs_human'),
				count(*) FILTER (WHERE r.status = 'error'), count(*) FILTER (WHERE r.status = 'timeout'),
				count(*) FILTER (WHERE r.status = 'interrupted'), count(*) FILTER (WHERE r.status = 'parked'),
				count(*) FILTER (WHERE r.status = 'running'),
				COALESCE(sum(r.duration_ms), 0), count(r.duration_ms), COALESCE(sum(r.cost_usd), 0),
				COALESCE(sum(r.tokens_in), 0), COALESCE(sum(r.tokens_out), 0),
				COALESCE(sum(COALESCE(r.tokens_cache_read, 0) + COALESCE(r.tokens_cache_write, 0)), 0)
				FROM b JOIN runs r ON r.started_at >= b.s AND r.started_at < b.e JOIN gigs g ON g.id = r.gig_id
				WHERE 1 = 1`+where+` GROUP BY b.i`, c.args...)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var i int
				var p MetricPoint
				if err := rows.Scan(&i, &p.Runs, &p.RunsDone, &p.RunsNeedsHuman, &p.RunsError, &p.RunsTimeout, &p.RunsInterrupted,
					&p.RunsParked, &p.RunsRunning, &p.RunMS, &p.RunsTimed, &p.CostUSD, &p.TokensIn, &p.TokensOut, &p.TokensCache); err != nil {
					rows.Close()
					return nil, err
				}
				bp := &res.Buckets[i].MetricPoint
				bp.Runs += p.Runs
				bp.RunsDone += p.RunsDone
				bp.RunsNeedsHuman += p.RunsNeedsHuman
				bp.RunsError += p.RunsError
				bp.RunsTimeout += p.RunsTimeout
				bp.RunsInterrupted += p.RunsInterrupted
				bp.RunsParked += p.RunsParked
				bp.RunsRunning += p.RunsRunning
				bp.RunMS += p.RunMS
				bp.RunsTimed += p.RunsTimed
				bp.CostUSD += p.CostUSD
				bp.TokensIn += p.TokensIn
				bp.TokensOut += p.TokensOut
				bp.TokensCache += p.TokensCache
			}
			if err := closeRows(rows); err != nil {
				return nil, err
			}
			// the cost tables
			for _, g := range []struct {
				sel, join, group string
				fill             func(rows interface{ Scan(...any) error }) (*MetricGroup, error)
				into             func(g *MetricGroup)
			}{
				{"r.worker_user_id, u.username, u.display_name", "JOIN users u ON u.id = r.worker_user_id", "r.worker_user_id, u.username, u.display_name",
					scanGroup(true), func(g *MetricGroup) { merge(agents, g.ID, g) }},
				{"0, COALESCE(r.model, ''), ''", "", "COALESCE(r.model, '')",
					scanGroup(false), func(g *MetricGroup) { mergeName(models, g) }},
				{"g.project_id, p.key, p.name", "JOIN projects p ON p.id = g.project_id", "g.project_id, p.key, p.name",
					scanGroup(true), func(g *MetricGroup) { merge(projects, g.ID, g) }},
			} {
				rows, err := db.QueryContext(ctx, `SELECT `+g.sel+`, count(*), COALESCE(sum(r.cost_usd), 0), COALESCE(sum(r.tokens_in), 0),
					COALESCE(sum(r.tokens_out), 0), COALESCE(sum(r.duration_ms), 0)
					FROM runs r JOIN gigs g ON g.id = r.gig_id `+g.join+`
					WHERE r.started_at >= ? AND r.started_at < ?`+where+` GROUP BY `+g.group,
					append([]any{q.From, q.To}, c.args...)...)
				if err != nil {
					return nil, err
				}
				for rows.Next() {
					row, err := g.fill(rows)
					if err != nil {
						rows.Close()
						return nil, err
					}
					g.into(row)
				}
				if err := closeRows(rows); err != nil {
					return nil, err
				}
			}
		}

		// errand runs (workspace-level: only for a scope that sees the whole workspace)
		if sc.Errands && sc.Projects == nil {
			c := scopeCond(sc, "e.workspace_id", "")
			q.workerCond(c, "e.worker_id")
			rows, err := db.QueryContext(ctx, cte+`SELECT b.i, count(*), count(*) FILTER (WHERE e.status = 'ok'),
				count(*) FILTER (WHERE e.status = 'failed'), count(*) FILTER (WHERE e.status = 'timeout'),
				count(*) FILTER (WHERE e.status = 'missed'), count(*) FILTER (WHERE e.status = 'skipped'),
				COALESCE(sum(e.duration_ms), 0), COALESCE(sum(e.output_bytes), 0)
				FROM b JOIN errand_runs e ON e.created_at >= b.s AND e.created_at < b.e
				WHERE 1 = 1`+c.sql()+` GROUP BY b.i`, c.args...)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var i int
				var p MetricPoint
				if err := rows.Scan(&i, &p.ErrandRuns, &p.ErrandOK, &p.ErrandFailed, &p.ErrandTimeout, &p.ErrandMissed, &p.ErrandSkipped,
					&p.ErrandMS, &p.ErrandOutputByte); err != nil {
					rows.Close()
					return nil, err
				}
				bp := &res.Buckets[i].MetricPoint
				bp.ErrandRuns += p.ErrandRuns
				bp.ErrandOK += p.ErrandOK
				bp.ErrandFailed += p.ErrandFailed
				bp.ErrandTimeout += p.ErrandTimeout
				bp.ErrandMissed += p.ErrandMissed
				bp.ErrandSkipped += p.ErrandSkipped
				bp.ErrandMS += p.ErrandMS
				bp.ErrandOutputByte += p.ErrandOutputByte
			}
			if err := closeRows(rows); err != nil {
				return nil, err
			}
		}

		// open right now
		{
			c := scopeCond(sc, "g.workspace_id", "g.project_id")
			q.gigPeople(c, "g.id")
			var n int64
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM gigs g JOIN statuses s ON s.id = g.status_id
				WHERE g.deleted_at IS NULL AND g.archived_at IS NULL AND s.category NOT IN ('done', 'cancelled')`+c.sql(), c.args...).Scan(&n); err != nil {
				return nil, err
			}
			res.Totals.OpenNow += n
		}
	}

	for i := range res.Buckets {
		b := &res.Buckets[i]
		b.CycleCount = int64(len(cycles[i]))
		b.CycleMedian = median(cycles[i])
		addPoint(&res.Totals.MetricPoint, &b.MetricPoint)
	}
	t := &res.Totals
	t.CycleCount, t.CycleMedian = int64(len(allCycles)), median(allCycles)
	if t.RunsTimed > 0 {
		v := t.RunMS / t.RunsTimed
		t.RunAvgMS = &v
	}
	if ended := t.Runs - t.RunsRunning - t.RunsParked; ended > 0 {
		rate := func(n int64) *float64 { v := float64(n) / float64(ended); return &v }
		t.SuccessRate, t.FailureRate, t.TimeoutRate, t.InterruptedRate = rate(t.RunsDone), rate(t.RunsError), rate(t.RunsTimeout), rate(t.RunsInterrupted)
	}
	res.ByAgent, res.ByModel, res.ByProject = topGroups(agents, nil, nil), topGroups(nil, models, nil), topGroups(nil, nil, projects)
	return res, nil
}

func closeRows(rows interface {
	Err() error
	Close() error
}) error {
	err := rows.Err()
	if cerr := rows.Close(); err == nil {
		err = cerr
	}
	return err
}

func scanGroup(withID bool) func(rows interface{ Scan(...any) error }) (*MetricGroup, error) {
	return func(rows interface{ Scan(...any) error }) (*MetricGroup, error) {
		var g MetricGroup
		err := rows.Scan(&g.ID, &g.Name, &g.Label, &g.Runs, &g.CostUSD, &g.TokensIn, &g.TokensOut, &g.RunMS)
		if !withID {
			g.ID = 0
		}
		return &g, err
	}
}

func merge(m map[int64]*MetricGroup, id int64, g *MetricGroup) {
	if cur := m[id]; cur != nil {
		addGroup(cur, g)
		return
	}
	m[id] = g
}

func mergeName(m map[string]*MetricGroup, g *MetricGroup) {
	if cur := m[g.Name]; cur != nil {
		addGroup(cur, g)
		return
	}
	m[g.Name] = g
}

func addGroup(to, g *MetricGroup) {
	to.Runs += g.Runs
	to.CostUSD += g.CostUSD
	to.TokensIn += g.TokensIn
	to.TokensOut += g.TokensOut
	to.RunMS += g.RunMS
}

// topGroups returns the biggest spenders of whichever map is given, most expensive first.
func topGroups(a map[int64]*MetricGroup, m map[string]*MetricGroup, p map[int64]*MetricGroup) []MetricGroup {
	out := []MetricGroup{}
	for _, g := range a {
		out = append(out, *g)
	}
	for _, g := range m {
		out = append(out, *g)
	}
	for _, g := range p {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CostUSD != out[j].CostUSD {
			return out[i].CostUSD > out[j].CostUSD
		}
		if out[i].Runs != out[j].Runs {
			return out[i].Runs > out[j].Runs
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > metricsMaxGroupBy {
		out = out[:metricsMaxGroupBy]
	}
	return out
}

// addPoint adds b's counts and sums to a (medians and cycle counts are taken separately).
func addPoint(a, b *MetricPoint) {
	a.GigsCreated += b.GigsCreated
	a.GigsDone += b.GigsDone
	a.GigsCancelled += b.GigsCancelled
	a.GigsArchived += b.GigsArchived
	a.Runs += b.Runs
	a.RunsDone += b.RunsDone
	a.RunsNeedsHuman += b.RunsNeedsHuman
	a.RunsError += b.RunsError
	a.RunsTimeout += b.RunsTimeout
	a.RunsInterrupted += b.RunsInterrupted
	a.RunsParked += b.RunsParked
	a.RunsRunning += b.RunsRunning
	a.RunMS += b.RunMS
	a.RunsTimed += b.RunsTimed
	a.CostUSD += b.CostUSD
	a.TokensIn += b.TokensIn
	a.TokensOut += b.TokensOut
	a.TokensCache += b.TokensCache
	a.ErrandRuns += b.ErrandRuns
	a.ErrandOK += b.ErrandOK
	a.ErrandFailed += b.ErrandFailed
	a.ErrandTimeout += b.ErrandTimeout
	a.ErrandMissed += b.ErrandMissed
	a.ErrandSkipped += b.ErrandSkipped
	a.ErrandMS += b.ErrandMS
	a.ErrandOutputByte += b.ErrandOutputByte
	a.RecurringOnTime += b.RecurringOnTime
	a.RecurringMissed += b.RecurringMissed
}

// median of v (the mean of the middle two for an even count); nil for none.
func median(v []int64) *int64 {
	if len(v) == 0 {
		return nil
	}
	s := append([]int64(nil), v...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	m := s[len(s)/2]
	if len(s)%2 == 0 {
		m = (s[len(s)/2-1] + s[len(s)/2]) / 2
	}
	return &m
}
