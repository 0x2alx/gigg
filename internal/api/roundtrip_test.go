package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestPutTakesGetBody: what GET returns for a recurring gig, a template or a schedule can be sent back
// to PUT as is (its read-only fields are ignored), and an edit made on that body sticks.
func TestPutTakesGetBody(t *testing.T) {
	e := newEnv(t)
	ada := e.ada()
	ws, pid := e.ids["scout.ws"], e.ids["scout.p"]
	code, out, _ := e.req("POST", fmt.Sprintf("/projects/%d/recurrences", pid),
		`{"title":"Brief","freq":"daily","tz":"UTC","window_start":"07:00","window_minutes":60,"lead_minutes":30,"checklist":["news"]}`, ada)
	e.ok(code, out, 201)
	var rec struct {
		ID         int64 `json:"id"`
		TemplateID int64 `json:"template_id"`
		ScheduleID int64 `json:"schedule_id"`
	}
	_ = json.Unmarshal(out, &rec)

	// (a recurring gig's GET body names its template, so its content comes from there: the edit here is
	// one of its own settings)
	for _, c := range []struct {
		name, path, field string
		value             any
	}{
		{"recurring gig", fmt.Sprintf("/recurrences/%d", rec.ID), "paused", true},
		{"template", fmt.Sprintf("/templates/%d", rec.TemplateID), "name", "Brief tpl"},
		{"schedule", fmt.Sprintf("/schedules/%d", rec.ScheduleID), "name", "Mornings"},
	} {
		code, out, _ := e.req("GET", c.path, "", ada)
		e.ok(code, out, 200)
		var body map[string]any
		if err := json.Unmarshal(out, &body); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		body[c.field] = c.value
		b, _ := json.Marshal(body)
		code, out, _ = e.req("PUT", c.path, string(b), ada)
		if code != 200 {
			t.Fatalf("%s: GET body back to PUT: %d %s", c.name, code, out)
		}
		var got map[string]any
		_ = json.Unmarshal(out, &got)
		if got[c.field] != c.value {
			t.Fatalf("%s: the edit didn't stick: %s", c.name, out)
		}
	}
	// a field no GET returns is still refused, so a misspelt one doesn't pass silently
	if code, out, _ := e.req("PUT", fmt.Sprintf("/schedules/%d", rec.ScheduleID), `{"name":"x","freq":"daily","tz":"UTC","windw_start":"03:00"}`, ada); code != 422 {
		t.Fatalf("unknown field: %d %s", code, out)
	}
	_ = ws
}

// TestUnknownFieldHints: a field the body doesn't take is named with the one it likely meant, and
// the detail lists what the body takes.
func TestUnknownFieldHints(t *testing.T) {
	e := newEnv(t)
	ada := e.ada()
	code, out, _ := e.req("POST", fmt.Sprintf("/projects/%d/gigs", e.ids["scout.p"]), `{"title":"x","status_id":3}`, ada)
	if code != 422 || !strings.Contains(string(out), `did you mean \"status\"?`) || !strings.Contains(string(out), "this body takes: ") {
		t.Fatalf("status_id: %d %s", code, out)
	}
	code, out, _ = e.req("POST", fmt.Sprintf("/gigs/%d/relations", e.ids["scout.gig"]), `{"gig":"SC-1","drection":"blocks"}`, ada)
	if code != 422 || !strings.Contains(string(out), `did you mean \"direction\"?`) {
		t.Fatalf("drection: %d %s", code, out)
	}
	// nothing close: no guess, still the list
	code, out, _ = e.req("POST", fmt.Sprintf("/gigs/%d/relations", e.ids["scout.gig"]), `{"gig":"SC-1","direction":"blocks","zzzzzz":1}`, ada)
	if code != 422 || strings.Contains(string(out), "did you mean") || !strings.Contains(string(out), "this body takes: direction, gig") {
		t.Fatalf("no near name: %d %s", code, out)
	}
}
