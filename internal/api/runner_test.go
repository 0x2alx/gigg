package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestRunnerControls: always_run needs its permission; claims respect the daily cap (distinct
// gigs, a counted gig may go on), the night cap (budget-dependent gigs only) and always_only;
// the pause stops every claim and only admins set or lift it; a run's usage is recorded once.
func TestRunnerControls(t *testing.T) {
	e := newEnv(t)
	scout := e.key("scout")
	ws := e.ids["scout.ws"]
	newGig := fmt.Sprintf("/projects/%d/gigs", e.ids["scout.p"])
	mk := func(title string, always bool) int64 {
		t.Helper()
		code, out, _ := e.req("POST", newGig, fmt.Sprintf(`{"title":%q,"category":"todo","always_run":%v,"model":"sonnet","effort":"low"}`, title, always), scout)
		e.ok(code, out, 201)
		var x struct {
			ID        int64  `json:"id"`
			Model     string `json:"model"`
			AlwaysRun bool   `json:"always_run"`
		}
		_ = json.Unmarshal(out, &x)
		if x.Model != "sonnet" || x.AlwaysRun != always {
			t.Fatalf("gig fields: %s", out)
		}
		return x.ID
	}
	claim := func(body string) (int, int64) {
		t.Helper()
		code, out, _ := e.req("POST", "/workspaces/current/gigs/claim", body, scout)
		var x struct {
			ID int64 `json:"id"`
		}
		_ = json.Unmarshal(out, &x)
		return code, x.ID
	}
	run := func(gig int64) int64 {
		t.Helper()
		code, out, _ := e.req("POST", fmt.Sprintf("/gigs/%d/runs", gig), `{"mode":"new"}`, scout)
		e.ok(code, out, 201)
		var x struct {
			ID int64 `json:"id"`
		}
		_ = json.Unmarshal(out, &x)
		return x.ID
	}

	// always_run needs the permission, even for a member; bad model names are refused
	if code, out, _ := e.req("POST", newGig, `{"title":"errand","always_run":true}`, scout); code != 403 || !strings.Contains(string(out), "always_run") {
		t.Fatalf("always_run without permission: %d %s", code, out)
	}
	if code, _, _ := e.req("POST", newGig, `{"title":"x","model":"opus; rm -rf /"}`, scout); code != 422 {
		t.Fatalf("bad model name: %d", code)
	}
	if code, _, _ := e.req("POST", newGig, `{"title":"x","effort":"ludicrous"}`, scout); code != 422 {
		t.Fatalf("bad effort: %d", code)
	}
	// without always_run, an always-run gig's content and reopening are off limits too
	code, out, _ := e.req("POST", newGig, `{"title":"ada errand","always_run":true}`, e.ada())
	e.ok(code, out, 201)
	var errand struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(out, &errand)
	if code, _, _ := e.req("PATCH", fmt.Sprintf("/gigs/%d", errand.ID), `{"description_md":"rm everything"}`, scout); code != 403 {
		t.Fatalf("editing an always-run gig without the permission: %d", code)
	}
	e.req("POST", fmt.Sprintf("/gigs/%d/move", errand.ID), `{"category":"done"}`, e.ada())
	if code, _, _ := e.req("POST", fmt.Sprintf("/gigs/%d/move", errand.ID), `{"category":"todo"}`, scout); code != 403 {
		t.Fatalf("reopening an always-run gig without the permission: %d", code)
	}
	if code, _, _ := e.req("POST", fmt.Sprintf("/gigs/%d/resume", errand.ID), `{}`, scout); code != 403 {
		t.Fatalf("resuming an always-run gig without the permission: %d", code)
	}
	code, out, _ = e.req("PUT", fmt.Sprintf("/users/%d/access", e.ids["scout.user"]), fmt.Sprintf(`{"workspace_id":%d,"preset":"member","overrides":{"always_run":true}}`, ws), e.ada())
	e.ok(code, out, 200)

	// the fixture's own gig is out of the way
	if code, out, _ := e.req("POST", fmt.Sprintf("/gigs/%d/move", e.ids["scout.gig"]), `{"category":"done"}`, e.ada()); code != 200 {
		t.Fatalf("fixture gig: %d %s", code, out)
	}
	// caps: 2 more gigs today, 1 more of them budget-dependent
	code, out, _ = e.req("PATCH", "/workspaces/current/runner", `{"daily_cap":2,"night_cap":1}`, scout)
	if code != 403 {
		t.Fatalf("agent changing caps: %d %s", code, out)
	}
	// the fixture's own runs count too: caps are set on top of them
	type usage struct {
		Usage struct {
			Gigs     int64 `json:"gigs_24h"`
			Always   int64 `json:"always_24h"`
			Optional int64 `json:"optional_24h"`
		} `json:"usage"`
	}
	state := func() usage {
		t.Helper()
		code, out, _ := e.req("GET", "/workspaces/current/runner", "", scout)
		e.ok(code, out, 200)
		var u usage
		_ = json.Unmarshal(out, &u)
		return u
	}
	base := state().Usage
	code, out, _ = e.req("PATCH", fmt.Sprintf("/workspaces/%d/runner", ws), fmt.Sprintf(`{"daily_cap":%d,"night_cap":%d}`, base.Gigs+2, base.Optional+1), e.ada())
	e.ok(code, out, 200)
	brief := mk("brief", true)
	b := mk("b", false)
	c := mk("c", false)

	if code, id := claim(`{"always_only":true,"dry_run":true}`); code != 200 || id != brief {
		t.Fatalf("always_only claims the always-run gig: %d %d", code, id)
	}
	if code, id := claim(`{}`); code != 200 || id != brief {
		t.Fatalf("always-run gigs come first: %d %d", code, id)
	}
	run(brief)
	if code, id := claim(`{"always_only":true}`); code != 204 {
		t.Fatalf("always_only with no always-run gig left: %d %d", code, id)
	}
	code, got := claim(`{}`)
	if code != 200 || (got != b && got != c) {
		t.Fatalf("optional claim: %d %d", code, got)
	}
	r := run(got)
	other := b + c - got
	// 2 distinct gigs today = the daily cap: a new gig waits ...
	if code, id := claim(`{}`); code != 204 {
		t.Fatalf("daily cap: %d %d", code, id)
	}
	if u := state().Usage; u.Gigs != base.Gigs+2 || u.Always != base.Always+1 || u.Optional != base.Optional+1 {
		t.Fatalf("runner state: %+v (base %+v)", u, base)
	}
	// recording a run on a new gig is held to the same caps (a worker that skips the claim)
	if code, out, _ := e.req("POST", fmt.Sprintf("/gigs/%d/runs", other), `{"mode":"new"}`, scout); code != 409 || !strings.Contains(string(out), "cap") {
		t.Fatalf("start-run past the daily cap: %d %s", code, out)
	}
	// ... but a counted one may go on (a resume)
	if code, out, _ := e.req("POST", fmt.Sprintf("/gigs/%d/move", got), `{"category":"todo"}`, scout); code != 200 {
		t.Fatalf("back to todo: %d %s", code, out)
	}
	if code, id := claim(`{}`); code != 200 || id != got {
		t.Fatalf("a counted gig goes on under the cap: %d %d", code, id)
	}
	// night cap: with room in the daily cap, a second optional gig still waits; an always-run one doesn't
	e.req("PATCH", fmt.Sprintf("/workspaces/%d/runner", ws), `{"daily_cap":50}`, e.ada())
	if code, id := claim(`{"dry_run":true}`); code != 204 {
		t.Fatalf("night cap keeps %d waiting: %d %d", other, code, id)
	}
	brief2 := mk("brief 2", true)
	if code, id := claim(`{"dry_run":true}`); code != 200 || id != brief2 {
		t.Fatalf("always-run past the night cap: %d %d", code, id)
	}

	// pause: admins only; every claim stops; resume lifts it
	if code, _, _ := e.req("POST", "/runner/pause", `{}`, scout); code != 403 {
		t.Fatalf("agent pausing: %d", code)
	}
	code, out, _ = e.req("POST", "/runner/pause", `{"minutes":60}`, e.ada())
	if code != 200 || !strings.Contains(string(out), `"paused":true`) || strings.Contains(string(out), `"paused_until":null`) {
		t.Fatalf("pause all for an hour: %d %s", code, out)
	}
	if code, _ := claim(`{"always_only":true}`); code != 423 {
		t.Fatalf("claim while paused: %d", code)
	}
	code, out, _ = e.req("POST", "/runner/pause", fmt.Sprintf(`{"workspaces":[%d]}`, ws), e.ada())
	if code != 200 || !strings.Contains(string(out), `"paused_open":true`) {
		t.Fatalf("pause until resumed: %s", out)
	}
	if code, _, _ := e.req("POST", "/runner/resume", `{}`, scout); code != 403 {
		t.Fatalf("agent resuming: %d", code)
	}
	if code, _, _ := e.req("POST", "/runner/pause", `{"workspaces":[99999]}`, e.ada()); code != 422 {
		t.Fatalf("unknown workspace: %d", code)
	}
	if code, _, _ := e.req("POST", fmt.Sprintf("/gigs/%d/runs", got), `{"mode":"resume"}`, scout); code != 423 {
		t.Fatalf("start-run while paused: %d", code)
	}
	// resuming one workspace leaves the others paused (atlas sorts before scout, and after it)
	e.req("POST", "/runner/pause", `{}`, e.ada())
	code, out, _ = e.req("POST", "/runner/resume", fmt.Sprintf(`{"workspaces":[%d]}`, e.ids["atlas.ws"]), e.ada())
	if code != 200 || strings.Count(string(out), `"slug"`) != 1 {
		t.Fatalf("resume one: %d %s", code, out)
	}
	code, out, _ = e.req("GET", "/runner", "", e.ada())
	if !strings.Contains(string(out), fmt.Sprintf(`"workspace_id":%d,"daily_cap":50,"night_cap":%d,"paused_until":null,"paused_open":true`, ws, base.Optional+1)) {
		t.Fatalf("scout should still be paused: %s", out)
	}
	code, out, _ = e.req("POST", "/runner/resume", `{}`, e.ada())
	if code != 200 || strings.Contains(string(out), `"paused":true`) {
		t.Fatalf("resume: %s", out)
	}
	if code, _ := claim(`{"dry_run":true}`); code != 200 {
		t.Fatalf("claim after resume: %d", code)
	}

	// usage: not while running, once after
	if code, _, _ := e.req("POST", fmt.Sprintf("/runs/%d/usage", r), `{"usage_pct":1.5}`, scout); code != 409 {
		t.Fatalf("usage on a running run: %d", code)
	}
	if code, out, _ := e.req("POST", fmt.Sprintf("/runs/%d/finish", r), `{"status":"done"}`, scout); code != 200 {
		t.Fatalf("finish: %d %s", code, out)
	}
	code, out, _ = e.req("POST", fmt.Sprintf("/runs/%d/usage", r), `{"usage_pct":1.5}`, scout)
	if code != 200 || !strings.Contains(string(out), `"usage_pct":1.5`) {
		t.Fatalf("usage: %d %s", code, out)
	}
	if code, _, _ := e.req("POST", fmt.Sprintf("/runs/%d/usage", r), `{"usage_pct":2}`, scout); code != 409 {
		t.Fatalf("usage twice: %d", code)
	}
	if code, _, _ := e.req("POST", fmt.Sprintf("/runs/%d/usage", r), `{"usage_pct":-5}`, e.ada()); code != 422 {
		t.Fatalf("negative usage: %d", code)
	}
	code, out, _ = e.req("GET", "/runner", "", e.ada())
	if code != 200 || !strings.Contains(string(out), `"usage_pct_24h":1.5`) {
		t.Fatalf("runner list: %s", out)
	}
	// clearing model and effort with null; clearing always_run needs the permission too
	code, out, _ = e.req("PATCH", fmt.Sprintf("/gigs/%d", brief2), `{"model":null,"effort":"","always_run":false}`, scout)
	if code != 200 || !strings.Contains(string(out), `"model":"","effort":"","always_run":false`) {
		t.Fatalf("clear run settings: %d %s", code, out)
	}
}
