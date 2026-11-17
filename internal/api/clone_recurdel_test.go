package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestCloneCopiesChecklistAndFields: a clone gets the checklist (every item unticked) and the custom
// field values.
func TestCloneCopiesChecklistAndFields(t *testing.T) {
	e := newEnv(t)
	ada := e.ada()
	ws := e.ids["scout.ws"]
	code, out, _ := e.req("POST", fmt.Sprintf("/workspaces/%d/fields", ws), `{"name":"Size","kind":"text"}`, ada)
	e.ok(code, out, 201)
	var f struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(out, &f)
	body := fmt.Sprintf(`{"title":"source","checklist":["one","two"],"custom":{"%d":"L"}}`, f.ID)
	code, out, _ = e.req("POST", fmt.Sprintf("/projects/%d/gigs", e.ids["scout.p"]), body, ada)
	e.ok(code, out, 201)
	var src struct {
		Key string `json:"key"`
	}
	_ = json.Unmarshal(out, &src)
	var ck struct {
		Items []struct {
			ID     int64  `json:"id"`
			Text   string `json:"text"`
			DoneAt *int64 `json:"done_at"`
		} `json:"items"`
	}
	_, out, _ = e.req("GET", "/gigs/"+src.Key+"/checklist", "", ada)
	_ = json.Unmarshal(out, &ck)
	code, out, _ = e.req("PATCH", fmt.Sprintf("/gigs/%s/checklist/%d", src.Key, ck.Items[0].ID), `{"done":true}`, ada)
	e.ok(code, out, 200)

	code, out, _ = e.req("POST", "/gigs/"+src.Key+"/clone", `{}`, ada)
	e.ok(code, out, 201)
	var c struct {
		Key    string         `json:"key"`
		Custom map[string]any `json:"custom"`
	}
	_ = json.Unmarshal(out, &c)
	if c.Custom[fmt.Sprint(f.ID)] != "L" {
		t.Fatalf("custom not copied: %s", out)
	}
	_, out, _ = e.req("GET", "/gigs/"+c.Key+"/checklist", "", ada)
	ck.Items = nil
	_ = json.Unmarshal(out, &ck)
	if len(ck.Items) != 2 || ck.Items[0].Text != "one" || ck.Items[0].DoneAt != nil || ck.Items[1].DoneAt != nil {
		t.Fatalf("checklist not copied unticked: %s", out)
	}
}

// TestDeleteRecurrenceWithTemplateAndSchedule: deleting a recurring gig may also delete its template
// and schedule, but not while another recurring gig uses them (then nothing is deleted).
func TestDeleteRecurrenceWithTemplateAndSchedule(t *testing.T) {
	e := newEnv(t)
	ada := e.ada()
	recs := fmt.Sprintf("/projects/%d/recurrences", e.ids["scout.p"])
	csrf := map[string]string{"X-Gigg-CSRF": "1"}
	type rec struct {
		ID         int64 `json:"id"`
		TemplateID int64 `json:"template_id"`
		ScheduleID int64 `json:"schedule_id"`
	}
	var a, b, c rec
	code, out, _ := e.req("POST", recs, `{"title":"Nightly","freq":"daily","window_start":"02:00","window_minutes":60}`, ada)
	e.ok(code, out, 201)
	_ = json.Unmarshal(out, &a)
	// b shares a's template (another timing), c shares a's schedule (other content)
	code, out, _ = e.req("POST", recs, fmt.Sprintf(`{"template_id":%d,"freq":"daily","window_start":"05:00","window_minutes":60}`, a.TemplateID), ada)
	e.ok(code, out, 201)
	_ = json.Unmarshal(out, &b)
	code, out, _ = e.req("POST", recs, fmt.Sprintf(`{"title":"Other","schedule_id":%d,"freq":"daily","window_start":"02:00","window_minutes":60}`, a.ScheduleID), ada)
	e.ok(code, out, 201)
	_ = json.Unmarshal(out, &c)
	if b.TemplateID != a.TemplateID || c.ScheduleID != a.ScheduleID || b.ScheduleID == a.ScheduleID || c.TemplateID == a.TemplateID {
		t.Fatalf("not shared as meant: %+v %+v %+v", a, b, c)
	}
	for _, q := range []string{"?template=true", "?schedule=true"} {
		code, out, _ := e.reqH("DELETE", fmt.Sprintf("/recurrences/%d%s", a.ID, q), "", ada, csrf)
		if code != 409 || !strings.Contains(string(out), "recurring gigs use") {
			t.Fatalf("shared %s: %d %s", q, code, out)
		}
		if code, _, _ := e.req("GET", fmt.Sprintf("/recurrences/%d", a.ID), "", ada); code != 200 {
			t.Fatalf("refused delete still removed the recurring gig: %d", code)
		}
	}
	for _, r := range []rec{b, c} {
		code, out, _ = e.reqH("DELETE", fmt.Sprintf("/recurrences/%d", r.ID), "", ada, csrf)
		e.ok(code, out, 204)
	}
	code, out, _ = e.reqH("DELETE", fmt.Sprintf("/recurrences/%d?template=true&schedule=true", a.ID), "", ada, csrf)
	e.ok(code, out, 204)
	for _, p := range []string{fmt.Sprintf("/templates/%d", a.TemplateID), fmt.Sprintf("/schedules/%d", a.ScheduleID), fmt.Sprintf("/recurrences/%d", a.ID)} {
		if code, _, _ := e.req("GET", p, "", ada); code != 404 {
			t.Fatalf("%s still there: %d", p, code)
		}
	}
}
