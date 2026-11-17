package api

import (
	"encoding/json"
	"fmt"
	"testing"
)

// TestSharedAPI: only instance admins make, change and delete shared templates and schedules; anyone
// who sees a workspace reads them and uses them there; a recurring gig on them sets its own assignees
// and gets copies when its content or timing changes; deleting it keeps the shared ones.
func TestSharedAPI(t *testing.T) {
	e := newEnv(t)
	ada, scout := e.ada(), e.key("scout")
	ws, pid := e.ids["scout.ws"], e.ids["scout.p"]
	code, out, _ := e.req("PUT", fmt.Sprintf("/users/%d/access", e.ids["scout.user"]), fmt.Sprintf(`{"workspace_id":%d,"preset":"member","overrides":{"recurring_gigs":true,"templates":true,"errands":true}}`, ws), ada)
	e.ok(code, out, 200)
	id := func(out []byte) int64 {
		var x struct {
			ID int64 `json:"id"`
		}
		_ = json.Unmarshal(out, &x)
		return x.ID
	}
	// a member (even with Gig templates) can't make or change shared ones
	for p, body := range map[string]string{"/shared/templates": `{"name":"x","title":"x"}`, "/shared/schedules": `{"freq":"daily"}`, "/shared/errand-templates": `{"name":"x","command":"true"}`} {
		if code, out, _ := e.req("POST", p, body, scout); code != 403 {
			t.Fatalf("member POST %s: %d %s", p, code, out)
		}
	}
	code, out, _ = e.req("POST", "/shared/templates", `{"name":"Brief","title":"Morning brief","auto_archive_minutes":720}`, ada)
	e.ok(code, out, 201)
	tpl := id(out)
	if code, out, _ := e.req("POST", "/shared/templates", fmt.Sprintf(`{"name":"x","title":"x","project_id":%d}`, pid), ada); code != 422 {
		t.Fatalf("shared template for one project: %d %s", code, out)
	}
	code, out, _ = e.req("POST", "/shared/schedules", `{"name":"Mornings","freq":"daily","tz":"UTC","window_start":"07:00","window_minutes":60}`, ada)
	e.ok(code, out, 201)
	sch := id(out)
	code, out, _ = e.req("POST", "/shared/errand-templates", `{"name":"ping","command":"true"}`, ada)
	e.ok(code, out, 201)
	etpl := id(out)
	for _, p := range []string{fmt.Sprintf("/templates/%d", tpl), fmt.Sprintf("/schedules/%d", sch)} {
		if code, _, _ := e.req("GET", p, "", scout); code != 200 {
			t.Fatalf("member reads %s: %d", p, code)
		}
	}
	for _, p := range []string{fmt.Sprintf("/templates/%d", tpl), fmt.Sprintf("/schedules/%d", sch), fmt.Sprintf("/errand-templates/%d", etpl)} {
		if code, _, _ := e.req("DELETE", p, "", scout); code != 403 && code != 404 {
			t.Fatalf("member deletes %s: %d", p, code)
		}
	}
	if code, _, _ := e.req("PUT", fmt.Sprintf("/templates/%d", tpl), `{"name":"Brief","title":"hijacked"}`, scout); code != 403 {
		t.Fatalf("member edits a shared template: %d", code)
	}
	if code, _, _ := e.req("PUT", fmt.Sprintf("/schedules/%d", sch), `{"freq":"weekly","weekdays":[1]}`, scout); code != 403 {
		t.Fatalf("member edits a shared schedule: %d", code)
	}
	// the workspace lists show them, after the workspace's own
	for _, p := range []string{fmt.Sprintf("/workspaces/%d/templates", ws), fmt.Sprintf("/workspaces/%d/schedules", ws)} {
		code, out, _ := e.req("GET", p, "", scout)
		e.ok(code, out, 200)
		var l struct {
			Items []struct {
				Shared bool `json:"shared"`
			} `json:"items"`
		}
		_ = json.Unmarshal(out, &l)
		if len(l.Items) == 0 || !l.Items[len(l.Items)-1].Shared {
			t.Fatalf("%s: %s", p, out)
		}
	}
	// a recurring gig on both, assigned to scout
	code, out, _ = e.req("POST", fmt.Sprintf("/projects/%d/recurrences", pid), fmt.Sprintf(`{"template_id":%d,"schedule_id":%d,"assignees":[%d]}`, tpl, sch, e.ids["scout.user"]), scout)
	e.ok(code, out, 201)
	var r struct {
		ID             int64   `json:"id"`
		TemplateID     int64   `json:"template_id"`
		ScheduleID     int64   `json:"schedule_id"`
		Assignees      []int64 `json:"assignees"`
		AssigneesOwn   bool    `json:"assignees_own"`
		TemplateShared bool    `json:"template_shared"`
	}
	_ = json.Unmarshal(out, &r)
	if !r.AssigneesOwn || len(r.Assignees) != 1 || !r.TemplateShared {
		t.Fatalf("recurring gig on shared parts: %s", out)
	}
	// new content and timing: copies in the workspace, the shared ones unchanged
	code, out, _ = e.req("PUT", fmt.Sprintf("/recurrences/%d", r.ID), `{"title":"Scout's brief","freq":"daily","tz":"UTC","window_start":"08:00","window_minutes":60}`, scout)
	e.ok(code, out, 200)
	_ = json.Unmarshal(out, &r)
	if r.TemplateID == tpl || r.ScheduleID == sch || r.TemplateShared {
		t.Fatalf("copies: %s", out)
	}
	code, out, _ = e.req("GET", fmt.Sprintf("/templates/%d", tpl), "", ada)
	e.ok(code, out, 200)
	var tt struct {
		Title string `json:"title"`
	}
	_ = json.Unmarshal(out, &tt)
	if tt.Title != "Morning brief" {
		t.Fatalf("shared template changed: %s", out)
	}
	// admins delete them (nothing uses them now)
	for _, p := range []string{fmt.Sprintf("/templates/%d", tpl), fmt.Sprintf("/schedules/%d", sch), fmt.Sprintf("/errand-templates/%d", etpl)} {
		if code, out, _ := e.reqH("DELETE", p, "", ada, map[string]string{"X-Gigg-CSRF": "1"}); code != 204 {
			t.Fatalf("admin deletes %s: %d %s", p, code, out)
		}
	}
}

// TestRunTimeoutAPI: a gig's run timeout is set (and changed) only with the run_timeout permission; the
// gig carries it for the runner.
func TestRunTimeoutAPI(t *testing.T) {
	e := newEnv(t)
	ada, scout := e.ada(), e.key("scout")
	gigs := fmt.Sprintf("/projects/%d/gigs", e.ids["scout.p"])
	if code, out, _ := e.req("POST", gigs, `{"title":"long","run_timeout_minutes":120}`, scout); code != 403 {
		t.Fatalf("member sets a timeout: %d %s", code, out)
	}
	if code, out, _ := e.req("POST", gigs, `{"title":"too long","run_timeout_minutes":301}`, ada); code != 422 {
		t.Fatalf("over the maximum: %d %s", code, out)
	}
	code, out, _ := e.req("POST", gigs, `{"title":"long","run_timeout_minutes":120}`, ada)
	e.ok(code, out, 201)
	var g struct {
		Key        string `json:"key"`
		RunTimeout *int64 `json:"run_timeout_minutes"`
	}
	_ = json.Unmarshal(out, &g)
	if g.RunTimeout == nil || *g.RunTimeout != 120 {
		t.Fatalf("created: %s", out)
	}
	if code, out, _ := e.req("PATCH", "/gigs/"+g.Key, `{"run_timeout_minutes":null}`, scout); code != 403 {
		t.Fatalf("member clears it: %d %s", code, out)
	}
	// other edits don't need it
	if code, out, _ := e.req("PATCH", "/gigs/"+g.Key, `{"title":"long, renamed"}`, scout); code != 200 {
		t.Fatalf("member renames it: %d %s", code, out)
	}
	code, out, _ = e.req("GET", "/gigs/"+g.Key, "", scout)
	e.ok(code, out, 200)
	_ = json.Unmarshal(out, &g)
	if g.RunTimeout == nil || *g.RunTimeout != 120 {
		t.Fatalf("the runner reads: %s", out)
	}
}
