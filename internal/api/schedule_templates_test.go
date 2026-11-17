package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestRecurringAPI: recurring gigs from content and timing (a template and a schedule are made),
// from template_id and schedule_id, shared templates and schedules, the delete guards, and the
// permission rules (Recurring gigs covers schedules and your own templates; someone else's template
// needs Gig templates; always-run templates need always_run).
func TestRecurringAPI(t *testing.T) {
	e := newEnv(t)
	ada, scout := e.ada(), e.key("scout")
	csrf := map[string]string{"X-Gigg-CSRF": "1"}
	ws, pid := e.ids["scout.ws"], e.ids["scout.p"]
	recs := fmt.Sprintf("/projects/%d/recurrences", pid)
	type rec struct {
		ID           int64    `json:"id"`
		Title        string   `json:"title"`
		TemplateID   int64    `json:"template_id"`
		TemplateName string   `json:"template_name"`
		TemplateUses int64    `json:"template_uses"`
		ScheduleID   int64    `json:"schedule_id"`
		ScheduleName string   `json:"schedule_name"`
		ScheduleUses int64    `json:"schedule_uses"`
		WindowStart  string   `json:"window_start"`
		Checklist    []string `json:"checklist"`
	}
	parse := func(out []byte) rec {
		var r rec
		_ = json.Unmarshal(out, &r)
		return r
	}
	// scout may run schedules, not edit templates
	code, out, _ := e.req("PUT", fmt.Sprintf("/users/%d/access", e.ids["scout.user"]), fmt.Sprintf(`{"workspace_id":%d,"preset":"member","overrides":{"recurring_gigs":true}}`, ws), ada)
	e.ok(code, out, 200)

	if code, _, _ := e.req("POST", recs, `{"freq":"daily"}`, scout); code != 422 {
		t.Fatalf("no title, no template: %d", code)
	}
	if code, _, _ := e.req("POST", recs, `{"title":"x"}`, scout); code != 422 {
		t.Fatalf("no freq, no schedule: %d", code)
	}
	code, out, _ = e.req("POST", recs, `{"title":"Brief","freq":"daily","tz":"UTC","window_start":"07:00","window_minutes":60,"checklist":["news","weather"]}`, scout)
	e.ok(code, out, 201)
	own := parse(out)
	if own.TemplateName != "Brief" || own.ScheduleName != "Every day 07:00–08:00" || len(own.Checklist) != 2 || own.TemplateUses != 1 {
		t.Fatalf("made from the form: %s", out)
	}
	// scout's own template: content edits need only Recurring gigs; the name follows the title
	code, out, _ = e.req("PUT", fmt.Sprintf("/recurrences/%d", own.ID), `{"title":"Brief v2","freq":"daily","tz":"UTC","window_start":"07:00","window_minutes":60}`, scout)
	e.ok(code, out, 200)
	if r := parse(out); r.Title != "Brief v2" || r.TemplateName != "Brief v2" || len(r.Checklist) != 2 || r.ScheduleID != own.ScheduleID {
		t.Fatalf("own edit: %s", out)
	}

	// ada's template on a schedule scout makes
	code, out, _ = e.req("POST", fmt.Sprintf("/workspaces/%d/templates", ws), `{"name":"Report","title":"Weekly report","checklist":["draft"]}`, ada)
	e.ok(code, out, 201)
	var tpl struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(out, &tpl)
	code, out, _ = e.req("POST", fmt.Sprintf("/workspaces/%d/schedules", ws), `{"name":"Fridays","freq":"weekly","weekdays":[5],"tz":"UTC"}`, scout)
	e.ok(code, out, 201)
	var fri struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(out, &fri)
	code, out, _ = e.req("POST", recs, fmt.Sprintf(`{"template_id":%d,"schedule_id":%d}`, tpl.ID, fri.ID), scout)
	e.ok(code, out, 201)
	shared := parse(out)
	if shared.Title != "Weekly report" || shared.ScheduleName != "Fridays" {
		t.Fatalf("paired: %s", out)
	}
	if code, _, _ := e.req("POST", recs, fmt.Sprintf(`{"template_id":%d,"schedule_id":%d}`, tpl.ID, fri.ID), scout); code != 409 {
		t.Fatalf("the same pair twice: %d", code)
	}
	// own settings and ids only: fine; ada's template's content through it needs Gig templates
	code, out, _ = e.req("PUT", fmt.Sprintf("/recurrences/%d", shared.ID), fmt.Sprintf(`{"template_id":%d,"schedule_id":%d,"paused":true}`, tpl.ID, fri.ID), scout)
	e.ok(code, out, 200)
	if code, out, _ := e.req("PUT", fmt.Sprintf("/recurrences/%d", shared.ID), fmt.Sprintf(`{"title":"Hijacked","schedule_id":%d}`, fri.ID), scout); code != 403 || !strings.Contains(string(out), "templates") {
		t.Fatalf("content edit of someone's template: %d %s", code, out)
	}
	// in use: template and schedule can't be deleted; the lists count their recurring gigs
	if code, out, _ := e.reqH("DELETE", fmt.Sprintf("/templates/%d", tpl.ID), "", ada, csrf); code != 409 || !strings.Contains(string(out), "Weekly report") {
		t.Fatalf("delete a template in use: %d %s", code, out)
	}
	if code, out, _ := e.req("DELETE", fmt.Sprintf("/schedules/%d", fri.ID), "", scout); code != 409 || !strings.Contains(string(out), "Weekly report") {
		t.Fatalf("delete a schedule in use: %d %s", code, out)
	}
	code, out, _ = e.req("GET", fmt.Sprintf("/workspaces/%d/templates", ws), "", ada)
	if code != 200 || !strings.Contains(string(out), `"recurring":1`) {
		t.Fatalf("template list: %s", out)
	}
	// changing the schedule moves the recurring gig on it
	code, out, _ = e.req("PUT", fmt.Sprintf("/schedules/%d", fri.ID), `{"name":"Fridays","freq":"weekly","weekdays":[5],"tz":"UTC","window_start":"16:00","window_minutes":60}`, scout)
	e.ok(code, out, 200)
	code, out, _ = e.req("GET", fmt.Sprintf("/recurrences/%d", shared.ID), "", scout)
	if r := parse(out); code != 200 || r.WindowStart != "16:00" {
		t.Fatalf("recurring gig after the schedule change: %s", out)
	}
	// switch it to scout's template: ada's is free again and can go
	code, out, _ = e.req("PUT", fmt.Sprintf("/recurrences/%d", shared.ID), fmt.Sprintf(`{"template_id":%d,"schedule_id":%d}`, own.TemplateID, fri.ID), scout)
	e.ok(code, out, 200)
	if r := parse(out); r.TemplateID != own.TemplateID || r.TemplateUses != 2 || r.Title != "Brief v2" {
		t.Fatalf("switch: %s", out)
	}
	if code, _, _ := e.reqH("DELETE", fmt.Sprintf("/templates/%d", tpl.ID), "", ada, csrf); code != 204 {
		t.Fatalf("delete a template no longer used: %d", code)
	}
	// another workspace's template or schedule: refused; its schedules are invisible
	code, out, _ = e.req("POST", fmt.Sprintf("/workspaces/%d/templates", e.ids["atlas.ws"]), `{"name":"ATLAS","title":"atlas gig"}`, ada)
	e.ok(code, out, 201)
	_ = json.Unmarshal(out, &tpl)
	if code, _, _ := e.req("POST", recs, fmt.Sprintf(`{"template_id":%d,"freq":"daily"}`, tpl.ID), scout); code != 422 {
		t.Fatalf("other workspace's template: %d", code)
	}
	code, out, _ = e.req("POST", fmt.Sprintf("/workspaces/%d/schedules", e.ids["atlas.ws"]), `{"freq":"daily"}`, ada)
	e.ok(code, out, 201)
	var atlasSched struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(out, &atlasSched)
	if code, _, _ := e.req("POST", recs, fmt.Sprintf(`{"title":"x","schedule_id":%d}`, atlasSched.ID), scout); code != 422 {
		t.Fatalf("other workspace's schedule: %d", code)
	}
	if code, _, _ := e.req("GET", fmt.Sprintf("/schedules/%d", atlasSched.ID), "", scout); code != 404 {
		t.Fatalf("other workspace's schedule visible: %d", code)
	}
	// an always-run template needs always_run to schedule
	code, out, _ = e.req("POST", fmt.Sprintf("/workspaces/%d/templates", ws), `{"name":"Errand","title":"cron job","always_run":true}`, ada)
	e.ok(code, out, 201)
	_ = json.Unmarshal(out, &tpl)
	if code, out, _ := e.req("POST", recs, fmt.Sprintf(`{"template_id":%d,"freq":"daily"}`, tpl.ID), scout); code != 403 || !strings.Contains(string(out), "always_run") {
		t.Fatalf("always-run template without the permission: %d %s", code, out)
	}
	// schedules need the permission to change; deleting a recurring gig leaves its template and schedule
	if code, _, _ := e.req("POST", fmt.Sprintf("/workspaces/%d/schedules", e.ids["atlas.ws"]), `{"freq":"daily"}`, e.key("atlas")); code != 403 {
		t.Fatalf("schedule without the permission: %d", code)
	}
	if code, _, _ := e.req("DELETE", fmt.Sprintf("/recurrences/%d", own.ID), "", scout); code != 204 {
		t.Fatalf("delete recurring gig: %d", code)
	}
	code, out, _ = e.req("GET", fmt.Sprintf("/schedules/%d", own.ScheduleID), "", scout)
	if code != 200 || !strings.Contains(string(out), `"uses":0`) {
		t.Fatalf("schedule after its recurring gig went: %d %s", code, out)
	}
	if code, _, _ := e.req("DELETE", fmt.Sprintf("/schedules/%d", own.ScheduleID), "", scout); code != 204 {
		t.Fatalf("delete an unused schedule: %d", code)
	}
}
