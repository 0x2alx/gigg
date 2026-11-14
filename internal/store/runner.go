package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Runner limits and the pause switch, per workspace (migration 0006). They are enforced in
// ClaimTask, so an agent calling the API directly gets no more than its runner would.

// RunnerSettings is a workspace's limits and pause state.
type RunnerSettings struct {
	WorkspaceID int64  `json:"workspace_id"`
	DailyCap    int64  `json:"daily_cap" doc:"Distinct tasks per rolling 24 hours, of any kind"`
	NightCap    int64  `json:"night_cap" doc:"Of those, tasks that aren't always-run (budget-dependent)"`
	PausedUntil *int64 `json:"paused_until" doc:"Unix ms; null = not paused (see paused_open)"`
	PausedOpen  bool   `json:"paused_open" doc:"Paused until someone resumes it"`
	PausedBy    *int64 `json:"paused_by"`
	PausedAt    *int64 `json:"paused_at"`
	Paused      bool   `json:"paused" doc:"Paused right now"`
	Version     int64  `json:"version"`
}

// Runner defaults when a workspace has no row.
const (
	DefaultDailyCap = 15
	DefaultNightCap = 10
)

// IsPaused reports the pause at a time.
func (r *RunnerSettings) IsPaused(now int64) bool {
	return r.PausedOpen || (r.PausedUntil != nil && *r.PausedUntil > now)
}

// GetRunnerSettings loads a workspace's settings (defaults when it has none).
func GetRunnerSettings(ctx context.Context, q Q, ws int64) (*RunnerSettings, error) {
	r := &RunnerSettings{WorkspaceID: ws, DailyCap: DefaultDailyCap, NightCap: DefaultNightCap}
	var until, by, at sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT daily_cap, night_cap, paused_until, paused_open, paused_by, paused_at, version
		FROM runner_settings WHERE workspace_id = ?`, ws).Scan(&r.DailyCap, &r.NightCap, &until, &r.PausedOpen, &by, &at, &r.Version)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	r.PausedUntil, r.PausedBy, r.PausedAt = ptrInt(until), ptrInt(by), ptrInt(at)
	r.Paused = r.IsPaused(Now())
	return r, nil
}

func ensureRunnerRow(ctx context.Context, q Q, ws int64) error {
	_, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO runner_settings (workspace_id) VALUES (?)`, ws)
	return err
}

// SetRunnerCaps changes a workspace's limits.
func SetRunnerCaps(ctx context.Context, q Q, a Actor, ws int64, daily, night *int64) (*RunnerSettings, error) {
	cur, err := GetRunnerSettings(ctx, q, ws)
	if err != nil {
		return nil, err
	}
	d, n := cur.DailyCap, cur.NightCap
	if daily != nil {
		d = *daily
	}
	if night != nil {
		n = *night
	}
	if d < 0 || d > 200 || n < 0 || n > 200 {
		return nil, Invalid("caps are 0-200")
	}
	if err := ensureRunnerRow(ctx, q, ws); err != nil {
		return nil, err
	}
	if _, err := q.ExecContext(ctx, `UPDATE runner_settings SET daily_cap = ?, night_cap = ?, version = version + 1 WHERE workspace_id = ?`, d, n, ws); err != nil {
		return nil, err
	}
	if _, err := Activity(ctx, q, a, &ws, nil, "runner.caps", "workspace", ws, map[string]any{"daily_cap": d, "night_cap": n}); err != nil {
		return nil, err
	}
	return GetRunnerSettings(ctx, q, ws)
}

// PauseRunner pauses a workspace's task runs: until a time, or (until nil) until resumed. Pausing
// again replaces the previous pause.
func PauseRunner(ctx context.Context, q Q, a Actor, ws int64, until *int64) error {
	if until != nil && *until <= Now() {
		return Invalid("the pause must end in the future")
	}
	if err := ensureRunnerRow(ctx, q, ws); err != nil {
		return err
	}
	open := until == nil
	if _, err := q.ExecContext(ctx, `UPDATE runner_settings SET paused_until = ?, paused_open = ?, paused_by = ?, paused_at = ?, version = version + 1
		WHERE workspace_id = ?`, nullInt(until), open, a.UserID, Now(), ws); err != nil {
		return err
	}
	_, err := Activity(ctx, q, a, &ws, nil, "runner.paused", "workspace", ws, map[string]any{"until": until, "until_resumed": open})
	return err
}

// ResumeRunner lifts a workspace's pause.
func ResumeRunner(ctx context.Context, q Q, a Actor, ws int64) error {
	res, err := q.ExecContext(ctx, `UPDATE runner_settings SET paused_until = NULL, paused_open = 0, paused_by = NULL, paused_at = NULL,
		version = version + 1 WHERE workspace_id = ? AND (paused_open = 1 OR paused_until IS NOT NULL)`, ws)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}
	_, err = Activity(ctx, q, a, &ws, nil, "runner.resumed", "workspace", ws, nil)
	return err
}

// RunnerUsage counts a workspace's agent work.
type RunnerUsage struct {
	Tasks24h     int64    `json:"tasks_24h" doc:"Distinct tasks with a run started in the last 24 hours"`
	Always24h    int64    `json:"always_24h" doc:"Of those, always-run tasks"`
	Optional24h  int64    `json:"optional_24h" doc:"Of those, budget-dependent tasks"`
	Runs24h      int64    `json:"runs_24h"`
	UsagePct24h  float64  `json:"usage_pct_24h" doc:"Points of the account's weekly usage meter measured on those runs (shared meter: approximate)"`
	UsagePct7d   float64  `json:"usage_pct_7d"`
	LastRunAt    *int64   `json:"last_run_at"`
	CostUSD7d    float64  `json:"cost_usd_7d"`
	Unmeasured7d int64    `json:"unmeasured_7d" doc:"Runs in the last 7 days without a usage measure"`
	Window       [2]int64 `json:"window" doc:"The 24-hour window counted [from, to] (unix ms)"`
}

// RunnerUsageOf counts the last 24 hours (and 7 days) of a workspace's runs.
func RunnerUsageOf(ctx context.Context, q Q, ws, now int64) (*RunnerUsage, error) {
	u := &RunnerUsage{}
	day := now - int64(24*time.Hour/time.Millisecond)
	week := now - int64(7*24*time.Hour/time.Millisecond)
	u.Window = [2]int64{day, now}
	err := q.QueryRowContext(ctx, `SELECT count(DISTINCT r.task_id),
		count(DISTINCT CASE WHEN t.always_run = 1 THEN r.task_id END),
		count(DISTINCT CASE WHEN t.always_run = 0 THEN r.task_id END),
		count(*), COALESCE(sum(r.usage_pct), 0)
		FROM runs r JOIN tasks t ON t.id = r.task_id WHERE r.workspace_id = ? AND r.started_at > ?`, ws, day).
		Scan(&u.Tasks24h, &u.Always24h, &u.Optional24h, &u.Runs24h, &u.UsagePct24h)
	if err != nil {
		return nil, err
	}
	var last sql.NullInt64
	err = q.QueryRowContext(ctx, `SELECT COALESCE(sum(usage_pct), 0), COALESCE(sum(cost_usd), 0), count(*) FILTER (WHERE usage_pct IS NULL AND status != 'running'),
		max(started_at) FROM runs WHERE workspace_id = ? AND started_at > ?`, ws, week).Scan(&u.UsagePct7d, &u.CostUSD7d, &u.Unmeasured7d, &last)
	u.LastRunAt = ptrInt(last)
	return u, err
}

// claimLimits narrows a claim to what the workspace's runner limits allow; ErrPaused when paused.
func claimLimits(ctx context.Context, q Q, ws int64, alwaysOnly bool) (where []string, args []any, err error) {
	now := Now()
	st, err := GetRunnerSettings(ctx, q, ws)
	if err != nil {
		return nil, nil, err
	}
	if st.IsPaused(now) {
		return nil, nil, ErrPaused
	}
	u, err := RunnerUsageOf(ctx, q, ws, now)
	if err != nil {
		return nil, nil, err
	}
	counted := "t.id IN (SELECT task_id FROM runs WHERE workspace_id = ? AND started_at > ?)"
	day := u.Window[0]
	if u.Runs24h >= RunCap(st) { // resumes, parks and retries of counted tasks have a ceiling too
		return []string{"1 = 0"}, nil, nil
	}
	if u.Tasks24h >= st.DailyCap { // a task already counted may go on (a resume, a parked job's check)
		where, args = append(where, counted), append(args, ws, day)
	}
	switch {
	case alwaysOnly:
		where = append(where, "t.always_run = 1")
	case u.Optional24h >= st.NightCap:
		where, args = append(where, "(t.always_run = 1 OR "+counted+")"), append(args, ws, day)
	}
	return where, args, nil
}

// RunCap is the most runs (of any task) a workspace may start in 24 hours: four per task of the
// daily cap, enough for resumes, parks and retries, not for an endless loop.
func RunCap(st *RunnerSettings) int64 { return 4 * st.DailyCap }

// CheckRunStart applies the same limits when a run is recorded, so a worker that skips the claim
// (or claims without recording) still can't go past them.
func CheckRunStart(ctx context.Context, q Q, t *Task) error {
	now := Now()
	st, err := GetRunnerSettings(ctx, q, t.WorkspaceID)
	if err != nil {
		return err
	}
	if st.IsPaused(now) {
		return ErrPaused
	}
	u, err := RunnerUsageOf(ctx, q, t.WorkspaceID, now)
	if err != nil {
		return err
	}
	if u.Runs24h >= RunCap(st) {
		return Conflict("the workspace started %d runs in the last 24 hours (limit %d)", u.Runs24h, RunCap(st))
	}
	var counted int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM runs WHERE task_id = ? AND started_at > ?`, t.ID, u.Window[0]).Scan(&counted); err != nil {
		return err
	}
	if counted > 0 {
		return nil
	}
	if u.Tasks24h >= st.DailyCap {
		return Conflict("the workspace's daily task cap is reached (%d)", st.DailyCap)
	}
	if !t.AlwaysRun && u.Optional24h >= st.NightCap {
		return Conflict("the workspace's cap for budget-dependent tasks is reached (%d)", st.NightCap)
	}
	return nil
}

// ErrPaused: the workspace's task runs are paused.
var ErrPaused = errors.New("task runs are paused for this workspace")
