package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0x2alx/gigg/internal/store"
)

// metricsSeed puts a known week of work into the two workspaces (March 2026, UTC times):
//
//	scout/main   G1 created 03-02 03:30 done 03-02 15:00 (an agent's), G2 created 03-02 10:00 done 03-03 10:00 (ada's,
//	             archived 03-05), G3 created 03-03 open, G4 created 03-09 cancelled, G6 and G7 recurring (03-02 done in
//	             time, 03-03 missed)
//	scout/side   G5 created 03-03 done 03-04
//	atlas/main   A1 created 03-03 done 03-03
//
// runs: scout 03-02 done $1.50, 03-02 error $0.50, 03-03 timeout $0.25; atlas 03-03 done $10.
// errand runs: scout 03-02 ok, 03-03 failed; atlas 03-03 ok.
func (e *env) metricsSeed() {
	e.t.Helper()
	ts := func(s string) int64 {
		t, err := time.Parse("2006-01-02T15:04", s)
		if err != nil {
			e.t.Fatal(err)
		}
		return t.UnixMilli()
	}
	ctx := context.Background()
	if err := e.st.Tx(ctx, func(tx *sql.Tx) error {
		cat := func(proj int64, c string) int64 {
			sts, _ := store.Statuses(ctx, tx, proj)
			for _, s := range sts {
				if s.Category == c {
					return s.ID
				}
			}
			e.t.Fatalf("no %s status in %d", c, proj)
			return 0
		}
		n := 100
		gig := func(ws, proj int64, category, created, done string, extra string) int64 {
			n++
			var d any
			if done != "" {
				d = ts(done)
			}
			res, err := tx.ExecContext(ctx, `INSERT INTO gigs (workspace_id, project_id, number, title, status_id, position, created_by, created_at, updated_at, done_at)
				VALUES (?,?,?,?,?,1,?,?,?,?)`, ws, proj, n, fmt.Sprintf("seed %d", n), cat(proj, category), e.ids["ada"], ts(created), ts(created), d)
			if err != nil {
				e.t.Fatal(err)
			}
			id, _ := res.LastInsertId()
			if extra != "" {
				if _, err := tx.ExecContext(ctx, `UPDATE gigs SET `+extra+` WHERE id = ?`, id); err != nil {
					e.t.Fatal(err)
				}
			}
			return id
		}
		sw, sp, sp2, aw, ap := e.ids["scout.ws"], e.ids["scout.p"], e.ids["scout.p2"], e.ids["atlas.ws"], e.ids["atlas.p"]
		g1 := gig(sw, sp, "done", "2026-03-02T03:30", "2026-03-02T15:00", "")
		g2 := gig(sw, sp, "done", "2026-03-02T10:00", "2026-03-03T10:00", fmt.Sprintf("archived_at = %d", ts("2026-03-05T12:00")))
		gig(sw, sp, "todo", "2026-03-03T12:00", "", "")
		g4 := gig(sw, sp, "cancelled", "2026-03-09T12:00", "", "")
		gig(sw, sp2, "done", "2026-03-03T09:00", "2026-03-04T09:00", "")
		rec := e.ids["scout.rec"]
		gig(sw, sp, "done", "2026-03-02T08:00", "2026-03-02T09:00", fmt.Sprintf("recurrence_id = %d, occurrence_at = %d", rec, ts("2026-03-02T08:00")))
		gig(sw, sp, "todo", "2026-03-03T08:00", "", fmt.Sprintf("recurrence_id = %d, occurrence_at = %d, missed_at = %d", rec, ts("2026-03-03T08:00"), ts("2026-03-03T10:00")))
		a1 := gig(aw, ap, "done", "2026-03-03T09:00", "2026-03-03T12:00", "")
		for g, u := range map[int64]int64{g1: e.ids["scout.user"], g2: e.ids["ada"]} {
			if _, err := tx.ExecContext(ctx, `INSERT INTO gig_assignees (gig_id, user_id) VALUES (?,?)`, g, u); err != nil {
				e.t.Fatal(err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO activity (at, workspace_id, project_id, action, entity_type, entity_id, data) VALUES (?,?,?,?,?,?,?)`,
			ts("2026-03-09T13:00"), sw, sp, "gig.moved", "gig", g4, `{"from_category":"todo","to_category":"cancelled"}`); err != nil {
			e.t.Fatal(err)
		}
		run := func(ws, g, worker int64, status, at, model string, cost float64, dur int64) {
			if _, err := tx.ExecContext(ctx, `INSERT INTO runs (workspace_id, gig_id, worker_user_id, status, started_at, heartbeat_at, duration_ms, cost_usd, tokens_in, tokens_out, model)
				VALUES (?,?,?,?,?,?,?,?,1000,200,?)`, ws, g, worker, status, ts(at), ts(at), dur, cost, model); err != nil {
				e.t.Fatal(err)
			}
		}
		su := e.ids["scout.user"]
		run(sw, g1, su, "done", "2026-03-02T05:00", "sonnet", 1.5, 60000)
		run(sw, g1, su, "error", "2026-03-02T06:00", "opus", 0.5, 30000)
		run(sw, g2, su, "timeout", "2026-03-03T07:00", "sonnet", 0.25, 90000)
		run(aw, a1, e.ids["atlas.user"], "done", "2026-03-03T07:00", "sonnet", 10, 1000)
		errand := func(ws int64, errand int64, worker int64, status, at string) {
			if _, err := tx.ExecContext(ctx, `INSERT INTO errand_runs (errand_id, workspace_id, worker_id, status, duration_ms, output_bytes, created_at) VALUES (?,?,?,?,1000,50,?)`,
				errand, ws, worker, status, ts(at)); err != nil {
				e.t.Fatal(err)
			}
		}
		errand(sw, e.ids["scout.errand"], su, "ok", "2026-03-02T04:00")
		errand(sw, e.ids["scout.errand"], su, "failed", "2026-03-03T04:00")
		errand(aw, e.ids["atlas.errand"], e.ids["atlas.user"], "ok", "2026-03-03T04:00")
		return nil
	}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) metrics(query string, c cred) (int, *store.MetricsResult, []byte) {
	e.t.Helper()
	code, out, _ := e.req("GET", "/metrics?"+query, "", c)
	var m store.MetricsResult
	if code == 200 {
		if err := json.Unmarshal(out, &m); err != nil {
			e.t.Fatalf("metrics json: %v: %s", err, out)
		}
	}
	return code, &m, out
}

const march = "from=2026-03-01&to=2026-03-10"

func TestMetricsCounts(t *testing.T) {
	e := newEnv(t)
	e.metricsSeed()
	code, m, out := e.metrics(march+"&workspace=scout", e.ada())
	e.ok(code, out, 200)
	if len(m.Buckets) != 10 || m.Bucket != "day" || m.TZ != "UTC" {
		t.Fatalf("buckets: %d %s %s", len(m.Buckets), m.Bucket, m.TZ)
	}
	day := func(i int) store.MetricBucket { return m.Buckets[i] } // 0 = 03-01
	if d := day(1); d.Date != "2026-03-02" || d.GigsCreated != 3 || d.GigsDone != 2 || d.Runs != 2 || d.RunsDone != 1 || d.RunsError != 1 || d.CostUSD != 2 ||
		d.TokensIn != 2000 || d.TokensOut != 400 || d.RunMS != 90000 || d.ErrandRuns != 1 || d.ErrandOK != 1 || d.RecurringOnTime != 1 || d.CycleCount != 2 {
		t.Errorf("03-02: %+v", d)
	}
	// the median of 11.5 h and 1 h
	if d := day(1); d.CycleMedian == nil || *d.CycleMedian != (41400000+3600000)/2 {
		t.Errorf("03-02 median: %v", d.CycleMedian)
	}
	if d := day(2); d.GigsCreated != 3 || d.GigsDone != 1 || d.Runs != 1 || d.RunsTimeout != 1 || d.ErrandFailed != 1 || d.RecurringMissed != 1 || *d.CycleMedian != 86400000 {
		t.Errorf("03-03: %+v", d)
	}
	if d := day(3); d.GigsDone != 1 || d.GigsCreated != 0 || d.CycleMedian == nil {
		t.Errorf("03-04: %+v", d)
	}
	if d := day(4); d.GigsArchived != 1 || d.CycleMedian != nil || d.CycleCount != 0 {
		t.Errorf("03-05: %+v", d)
	}
	if d := day(8); d.Date != "2026-03-09" || d.GigsCreated != 1 || d.GigsCancelled != 1 {
		t.Errorf("03-09: %+v", d)
	}
	tt := m.Totals
	if tt.GigsCreated != 7 || tt.GigsDone != 4 || tt.GigsCancelled != 1 || tt.GigsArchived != 1 || tt.Runs != 3 || tt.CostUSD != 2.25 || tt.ErrandRuns != 2 ||
		tt.RecurringOnTime != 1 || tt.RecurringMissed != 1 || tt.OpenNow != 3 || tt.CycleCount != 4 {
		t.Errorf("totals: %+v", tt)
	}
	if tt.RunAvgMS == nil || *tt.RunAvgMS != 60000 || tt.SuccessRate == nil || *tt.SuccessRate < 0.33 || *tt.SuccessRate > 0.34 || *tt.FailureRate < 0.33 || *tt.TimeoutRate < 0.33 || *tt.InterruptedRate != 0 {
		t.Errorf("rates: %+v", tt)
	}
	if len(m.ByAgent) != 1 || m.ByAgent[0].Name != "scout" || m.ByAgent[0].Runs != 3 || m.ByAgent[0].CostUSD != 2.25 {
		t.Errorf("by agent: %+v", m.ByAgent)
	}
	if len(m.ByModel) != 2 || m.ByModel[0].Name != "sonnet" || m.ByModel[0].CostUSD != 1.75 || m.ByModel[1].Name != "opus" {
		t.Errorf("by model: %+v", m.ByModel)
	}
	if len(m.ByProject) != 1 || m.ByProject[0].Name != "GIG" || m.ByProject[0].Runs != 3 {
		t.Errorf("by project: %+v", m.ByProject)
	}

	// weeks (Monday first): the first bucket starts the Monday before the range but clips to it
	code, m, out = e.metrics(march+"&workspace=scout&bucket=week", e.ada())
	e.ok(code, out, 200)
	if len(m.Buckets) != 3 || m.Buckets[0].Date != "2026-02-23" || m.Buckets[1].Date != "2026-03-02" || m.Buckets[2].Date != "2026-03-09" {
		t.Fatalf("weeks: %+v", m.Buckets)
	}
	if w := m.Buckets[1]; w.GigsCreated != 6 || w.GigsDone != 4 || w.Runs != 3 {
		t.Errorf("week of 03-02: %+v", w)
	}
	if m.Buckets[0].Start != m.From {
		t.Errorf("first week not clipped: %d vs %d", m.Buckets[0].Start, m.From)
	}
	// months
	code, m, out = e.metrics("from=2026-02-20&to=2026-03-10&workspace=scout&bucket=month", e.ada())
	e.ok(code, out, 200)
	if len(m.Buckets) != 2 || m.Buckets[0].Date != "2026-02-01" || m.Buckets[1].Date != "2026-03-01" || m.Buckets[1].GigsCreated != 7 || m.Buckets[0].GigsCreated != 0 {
		t.Errorf("months: %+v", m.Buckets)
	}
}

func TestMetricsTimeZone(t *testing.T) {
	e := newEnv(t)
	e.metricsSeed()
	// G1 is created 03-02 03:30 UTC: still 03-01 in New York (UTC-5)
	code, m, out := e.metrics(march+"&workspace=scout&tz=America/New_York", e.ada())
	e.ok(code, out, 200)
	if m.TZ != "America/New_York" || m.Buckets[0].GigsCreated != 1 || m.Buckets[1].GigsCreated != 2 || m.Buckets[0].Start != 1772341200000 {
		t.Fatalf("new york: %s created %d/%d start %d", m.TZ, m.Buckets[0].GigsCreated, m.Buckets[1].GigsCreated, m.Buckets[0].Start)
	}
	// 03-02 in New York ends 03-03 05:00 UTC: G1 and G6 are done on it, G2 (03-03 10:00 UTC) the day after
	if m.Buckets[1].GigsDone != 2 || m.Buckets[2].GigsDone != 1 {
		t.Errorf("done by NY day: %d %d", m.Buckets[1].GigsDone, m.Buckets[2].GigsDone)
	}
	// DST: 03-08 is 23 hours long in New York
	if b := m.Buckets[7]; b.End-b.Start != 23*3600000 {
		t.Errorf("DST day: %d ms", b.End-b.Start)
	}
	// the user's own preference is the default; an explicit tz wins
	code, out, _ = e.req("PATCH", "/me", `{"preferences":{"timezone":"America/New_York"}}`, e.ada())
	e.ok(code, out, 200)
	code, m, out = e.metrics(march+"&workspace=scout", e.ada())
	e.ok(code, out, 200)
	if m.TZ != "America/New_York" || m.Buckets[0].GigsCreated != 1 {
		t.Errorf("preference: %s %d", m.TZ, m.Buckets[0].GigsCreated)
	}
	code, m, out = e.metrics(march+"&workspace=scout&tz=UTC", e.ada())
	e.ok(code, out, 200)
	if m.TZ != "UTC" || m.Buckets[0].GigsCreated != 0 {
		t.Errorf("explicit tz: %s %d", m.TZ, m.Buckets[0].GigsCreated)
	}
	if code, _, _ := e.metrics(march+"&tz=Mars/Olympus", e.ada()); code != 422 {
		t.Errorf("bad tz: %d", code)
	}
}

func TestMetricsFilters(t *testing.T) {
	e := newEnv(t)
	e.metricsSeed()
	ada := e.ada()
	get := func(q string) store.MetricTotals {
		t.Helper()
		code, m, out := e.metrics(march+q, ada)
		e.ok(code, out, 200)
		return m.Totals
	}
	// both workspaces
	if tt := get(""); tt.GigsCreated != 8 || tt.Runs != 4 || tt.CostUSD != 12.25 || tt.ErrandRuns != 3 {
		t.Errorf("all: %+v", tt)
	}
	if tt := get("&workspace=atlas"); tt.GigsCreated != 1 || tt.GigsDone != 1 || tt.Runs != 1 || tt.CostUSD != 10 || tt.ErrandRuns != 1 {
		t.Errorf("atlas: %+v", tt)
	}
	// a project: its gigs and runs, no errands (they belong to the workspace)
	if tt := get(fmt.Sprintf("&project=%d", e.ids["scout.p2"])); tt.GigsCreated != 1 || tt.GigsDone != 1 || tt.Runs != 0 || tt.ErrandRuns != 0 {
		t.Errorf("project: %+v", tt)
	}
	if tt := get(fmt.Sprintf("&project=%d", e.ids["scout.p"])); tt.GigsCreated != 6 || tt.Runs != 3 || tt.ErrandRuns != 0 {
		t.Errorf("main project: %+v", tt)
	}
	// ada the human: the gig she is assigned (G2); she ran nothing
	if tt := get("&workspace=scout&assignee=" + fmt.Sprint(e.ids["ada"])); tt.GigsCreated != 1 || tt.GigsDone != 1 || tt.Runs != 0 || tt.ErrandRuns != 0 {
		t.Errorf("assignee ada: %+v", tt)
	}
	if tt := get("&workspace=scout&assignee=@ada"); tt.GigsCreated != 1 {
		t.Errorf("assignee by name: %+v", tt)
	}
	if tt := get("&workspace=scout&worker_kind=human"); tt.GigsCreated != 1 || tt.Runs != 0 {
		t.Errorf("humans: %+v", tt)
	}
	if tt := get("&workspace=scout&worker_kind=agent"); tt.GigsCreated != 1 || tt.GigsDone != 1 || tt.Runs != 3 || tt.ErrandRuns != 2 || tt.CostUSD != 2.25 {
		t.Errorf("agents: %+v", tt)
	}
	if tt := get("&workspace=scout&assignee=scout"); tt.GigsCreated != 1 || tt.Runs != 3 {
		t.Errorf("assignee scout: %+v", tt)
	}
	// unknown things
	for _, q := range []string{"&workspace=nope", "&project=999999", "&assignee=nobody", "&worker_kind=robot", "&bucket=year"} {
		if code, _, out := e.metrics(march+q, ada); code != 404 && code != 422 {
			t.Errorf("%s: %d %s", q, code, out)
		}
	}
	// a project of another workspace than the one named
	if code, _, _ := e.metrics(march+fmt.Sprintf("&workspace=atlas&project=%d", e.ids["scout.p"]), ada); code != 404 {
		t.Errorf("project outside the workspace: %d", code)
	}
}

func TestMetricsRange(t *testing.T) {
	e := newEnv(t)
	ada := e.ada()
	// 400 days is the longest range, a day per bucket
	code, m, out := e.metrics("from=2025-02-01&to=2026-03-07", ada) // 400 days
	e.ok(code, out, 200)
	if len(m.Buckets) != 400 {
		t.Fatalf("buckets: %d", len(m.Buckets))
	}
	for name, q := range map[string]string{"too long": "from=2025-02-01&to=2026-03-08", "backwards": "from=2026-03-09&to=2026-03-01", "junk": "from=yesterday"} {
		if code, _, out := e.metrics(q, ada); code != 422 {
			t.Errorf("%s: %d %s", name, code, out)
		}
	}
	// one day
	if code, m, out = e.metrics("from=2026-03-01&to=2026-03-01", ada); code != 200 || len(m.Buckets) != 1 {
		t.Errorf("one day: %d %s", code, out)
	}
	// no query: the last 30 days, ending today; the seeded gigs of both workspaces were created just now
	code, m, out = e.metrics("", ada)
	e.ok(code, out, 200)
	if len(m.Buckets) != 30 || m.Totals.GigsCreated != 2 || m.Totals.Runs != 2 || m.Totals.ErrandRuns != 2 {
		t.Errorf("default range: %d buckets, %+v", len(m.Buckets), m.Totals)
	}
	if code, _, _ := e.req("GET", "/metrics", "", cred{listener: "main"}); code != 401 {
		t.Errorf("anonymous: %d", code)
	}
}

func TestMetricsScoping(t *testing.T) {
	e := newEnv(t)
	e.metricsSeed()
	// a runner key reaches only scout; no errands:read, so no errand runs either
	code, m, out := e.metrics(march, e.key("scout"))
	e.ok(code, out, 200)
	if tt := m.Totals; tt.GigsCreated != 7 || tt.Runs != 3 || tt.CostUSD != 2.25 || tt.ErrandRuns != 0 {
		t.Errorf("scout key: %+v", tt)
	}
	for _, a := range m.ByAgent {
		if a.Name != "scout" {
			t.Errorf("another agent's spend: %+v", a)
		}
	}
	for _, q := range []string{"&workspace=atlas", fmt.Sprintf("&project=%d", e.ids["atlas.p"])} {
		if code, _, out := e.metrics(march+q, e.key("scout")); code != 404 || strings.Contains(string(out), "cost_usd") {
			t.Errorf("scout key %s: %d %s", q, code, out)
		}
	}
	// a key without gigs:read
	code, out, _ = e.req("POST", "/keys", fmt.Sprintf(`{"name":"narrow","user_id":%d,"reach":["scout"],"scopes":["workspace:read"]}`, e.ids["scout.user"]), e.ada())
	e.ok(code, out, 201)
	var kc KeyCreated
	_ = json.Unmarshal(out, &kc)
	if code, _, _ := e.metrics(march, cred{token: kc.Token, listener: "internal"}); code != 403 {
		t.Errorf("key without gigs:read: %d", code)
	}
	// bob is a viewer of scout only
	code, out, hdr := e.req("POST", "/auth/login", `{"username":"bob","password":"bob's long password"}`, cred{listener: "main"})
	e.ok(code, out, 200)
	bob := cred{cookie: strings.TrimPrefix(strings.SplitN(hdr.Get("Set-Cookie"), ";", 2)[0], "__Host-gigg_session="), listener: "main", origin: "https://board.example"}
	code, m, out = e.metrics(march, bob)
	e.ok(code, out, 200)
	if tt := m.Totals; tt.GigsCreated != 7 || tt.CostUSD != 2.25 || tt.ErrandRuns != 2 {
		t.Errorf("bob: %+v", tt)
	}
	if code, _, _ := e.metrics(march+"&workspace=atlas", bob); code != 404 {
		t.Errorf("bob on atlas: %d", code)
	}
	// a project grant: scout on atlas's side project only (the workspace's other projects, its errands and its spend stay out)
	code, out, _ = e.reqH("PUT", fmt.Sprintf("/projects/%d/members/%d", e.ids["atlas.p2"], e.ids["scout.user"]), `{"role":"consumer"}`, e.ada(), map[string]string{"X-Gigg-CSRF": "1"})
	e.ok(code, out, 204)
	code, out, _ = e.req("POST", "/keys", fmt.Sprintf(`{"name":"follow","user_id":%d,"scopes":["workspace:read","gigs:read","errands:read"],"allowed_listeners":["internal"]}`, e.ids["scout.user"]), e.ada())
	e.ok(code, out, 201)
	_ = json.Unmarshal(out, &kc)
	follow := cred{token: kc.Token, listener: "internal"}
	code, m, out = e.metrics(march, follow)
	e.ok(code, out, 200)
	// scout's own workspace in full (7 gigs, 3 runs, 2 errand runs); atlas's side project has nothing yet
	if tt := m.Totals; tt.GigsCreated != 7 || tt.Runs != 3 || tt.CostUSD != 2.25 || tt.ErrandRuns != 2 {
		t.Errorf("project grant: %+v", tt)
	}
	if code, _, _ := e.metrics(march+fmt.Sprintf("&project=%d", e.ids["atlas.p"]), follow); code != 404 {
		t.Errorf("project grant, hidden project: %d", code)
	}
	if code, m, out := e.metrics(march+fmt.Sprintf("&project=%d", e.ids["atlas.p2"]), follow); code != 200 || m.Totals.GigsCreated != 0 {
		t.Errorf("project grant, its project: %d %s", code, out)
	}
	if code, m, _ := e.metrics(march+"&workspace=atlas", follow); code != 200 || m.Totals.GigsCreated != 0 || m.Totals.Runs != 0 || m.Totals.ErrandRuns != 0 {
		t.Errorf("project grant, atlas: %d %+v", code, m.Totals)
	}
}

func TestMetricsMCP(t *testing.T) {
	e := newEnv(t)
	e.metricsSeed()
	srv := e.mcpServer("internal")
	defer srv.Close()
	s, err := e.mcpSession(t, srv, authTransport{token: e.keys["scout"]})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	call := func(args map[string]any) *mcp.CallToolResult {
		t.Helper()
		r, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_metrics", Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	r := call(map[string]any{"from": "2026-03-01", "to": "2026-03-10", "bucket": "week"})
	var m store.MetricsResult
	if r.IsError || json.Unmarshal([]byte(text(r)), &m) != nil || len(m.Buckets) != 3 || m.Totals.Runs != 3 || m.Totals.GigsCreated != 7 {
		t.Fatalf("get_metrics: %s", text(r))
	}
	r = call(map[string]any{"from": "2026-03-01", "to": "2026-03-10", "worker_kind": "human", "workspace": "scout"})
	if r.IsError || json.Unmarshal([]byte(text(r)), &m) != nil || m.Totals.Runs != 0 || m.Totals.GigsCreated != 1 {
		t.Fatalf("get_metrics human: %s", text(r))
	}
	// another workspace: an error and no data
	if r = call(map[string]any{"workspace": "atlas"}); !r.IsError || strings.Contains(text(r), "cost_usd") {
		t.Fatalf("get_metrics atlas: %s", text(r))
	}
	if r = call(map[string]any{"bucket": "year"}); !r.IsError {
		t.Fatalf("get_metrics bad bucket: %s", text(r))
	}
}
