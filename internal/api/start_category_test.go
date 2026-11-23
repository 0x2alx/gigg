package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestRecurrenceStartCategoryPut: a PUT that echoes the GET body (template_id included) still
// changes where its gigs start; naming another template with a different start, on create or
// update, is refused rather than silently taking the template's.
func TestRecurrenceStartCategoryPut(t *testing.T) {
	e := newEnv(t)
	recs := fmt.Sprintf("/projects/%d/recurrences", e.ids["scout.p"])
	code, out, _ := e.req("POST", recs, `{"title":"Daily review","freq":"daily","window_start":"09:00","window_minutes":60}`, e.ada())
	e.ok(code, out, 201)
	var cur map[string]any
	_ = json.Unmarshal(out, &cur)
	id := int64(cur["id"].(float64))

	// the GET body back, with start_category changed
	code, out, _ = e.req("GET", fmt.Sprintf("/recurrences/%d", id), "", e.ada())
	e.ok(code, out, 200)
	_ = json.Unmarshal(out, &cur)
	cur["start_category"] = "backlog"
	b, _ := json.Marshal(cur)
	code, out, _ = e.req("PUT", fmt.Sprintf("/recurrences/%d", id), string(b), e.ada())
	e.ok(code, out, 200)
	if !strings.Contains(string(out), `"start_category":"backlog"`) {
		t.Fatalf("echoed PUT ignored start_category: %s", out)
	}
	tplID := int64(cur["template_id"].(float64))
	code, out, _ = e.req("GET", fmt.Sprintf("/templates/%d", tplID), "", e.ada())
	e.ok(code, out, 200)
	if !strings.Contains(string(out), `"start_category":"backlog"`) {
		t.Fatalf("not written to its template: %s", out)
	}

	// another template, which starts in the default: a clashing start_category is refused
	code, out, _ = e.req("POST", "/workspaces/scout/templates", fmt.Sprintf(`{"name":"Plain","title":"Plain","project_id":%d}`, e.ids["scout.p"]), e.ada())
	e.ok(code, out, 201)
	var plain struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(out, &plain)
	if code, out, _ := e.req("POST", recs, fmt.Sprintf(`{"template_id":%d,"freq":"weekly","weekdays":[1],"start_category":"backlog"}`, plain.ID), e.ada()); code != 422 || !strings.Contains(string(out), "start_category") {
		t.Fatalf("create with a clashing template: %d %s", code, out)
	}
	code, out, _ = e.req("GET", fmt.Sprintf("/recurrences/%d", id), "", e.ada())
	e.ok(code, out, 200)
	_ = json.Unmarshal(out, &cur)
	cur["template_id"] = plain.ID // start_category still "backlog" from the echo
	b, _ = json.Marshal(cur)
	if code, out, _ := e.req("PUT", fmt.Sprintf("/recurrences/%d", id), string(b), e.ada()); code != 422 || !strings.Contains(string(out), "start_category") {
		t.Fatalf("switch to a clashing template: %d %s", code, out)
	}
	// the same switch with the template's own value goes through
	cur["start_category"] = ""
	b, _ = json.Marshal(cur)
	code, out, _ = e.req("PUT", fmt.Sprintf("/recurrences/%d", id), string(b), e.ada())
	e.ok(code, out, 200)
	if !strings.Contains(string(out), `"start_category":""`) || !strings.Contains(string(out), fmt.Sprintf(`"template_id":%d`, plain.ID)) {
		t.Fatalf("switch: %s", out)
	}

	// someone else's template: an echoed PUT that changes start_category still needs Gig templates
	if code, out, _ := e.req("PUT", fmt.Sprintf("/users/%d/access", e.ids["scout.user"]), `{"workspace_id":null,"overrides":{"recurring_gigs":true,"create_gigs":true,"templates":false}}`, e.ada()); code != 200 {
		t.Fatalf("grant: %d %s", code, out)
	}
	scout := e.key("scout")
	echo := func(start *string) (int, []byte) {
		code, out, _ := e.req("GET", fmt.Sprintf("/recurrences/%d", id), "", e.ada())
		e.ok(code, out, 200)
		body := map[string]any{}
		_ = json.Unmarshal(out, &body)
		if start != nil {
			body["start_category"] = *start
		}
		b, _ := json.Marshal(body)
		code, out, _ = e.req("PUT", fmt.Sprintf("/recurrences/%d", id), string(b), scout)
		return code, out
	}
	if code, out := echo(nil); code != 200 { // unchanged content: scout may edit the recurring gig itself
		t.Fatalf("scout's unchanged echo: %d %s", code, out)
	}
	backlog := "backlog"
	if code, out := echo(&backlog); code != 403 {
		t.Fatalf("changed ada's template without Gig templates: %d %s", code, out)
	}
}
