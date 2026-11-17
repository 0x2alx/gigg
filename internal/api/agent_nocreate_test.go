package api

import (
	"encoding/json"
	"fmt"
	"testing"
)

// TestAgentCreatesNothing: when an agent's rules deny every way of creating (a setup where only
// some users create work), the agent's runner key (gigs:write) is refused every way of
// making a gig or something that makes gigs: a gig, a subgig, a clone, from a template, a recurring
// gig, a template, a schedule, and rewriting a template behind someone else's recurring gig. Moving
// and commenting on its gigs still work.
func TestAgentCreatesNothing(t *testing.T) {
	e := newEnv(t)
	ada, scout := e.ada(), e.key("scout")
	ws, pid := e.ids["scout.ws"], e.ids["scout.p"]
	deny := `{"create_gigs":false,"recurring_gigs":false,"templates":false,"errands":false,"errand_unsandboxed":false,"allowed_tools":false,"always_run":false}`
	code, out, _ := e.req("PUT", fmt.Sprintf("/users/%d/access", e.ids["scout.user"]), fmt.Sprintf(`{"workspace_id":%d,"preset":"manager","overrides":%s}`, ws, deny), ada)
	e.ok(code, out, 200)

	code, out, _ = e.req("POST", fmt.Sprintf("/projects/%d/gigs", pid), `{"title":"parent"}`, ada)
	e.ok(code, out, 201)
	var g struct {
		ID  int64  `json:"id"`
		Key string `json:"key"`
	}
	_ = json.Unmarshal(out, &g)
	code, out, _ = e.req("POST", fmt.Sprintf("/workspaces/%d/templates", ws), `{"name":"Report","title":"Weekly report"}`, ada)
	e.ok(code, out, 201)
	var tp struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(out, &tp)
	recs := fmt.Sprintf("/projects/%d/recurrences", pid)
	code, out, _ = e.req("POST", recs, `{"title":"Nightly","freq":"daily","window_start":"02:00","window_minutes":60}`, ada)
	e.ok(code, out, 201)
	var rec struct {
		ID         int64 `json:"id"`
		TemplateID int64 `json:"template_id"`
	}
	_ = json.Unmarshal(out, &rec)

	for name, c := range map[string][3]string{
		"gig":             {"POST", fmt.Sprintf("/projects/%d/gigs", pid), `{"title":"mine"}`},
		"subgig":          {"POST", fmt.Sprintf("/projects/%d/gigs", pid), fmt.Sprintf(`{"title":"sub","parent_id":%d}`, g.ID)},
		"clone":           {"POST", "/gigs/" + g.Key + "/clone", `{}`},
		"from template":   {"POST", fmt.Sprintf("/templates/%d/use", tp.ID), fmt.Sprintf(`{"project":%d}`, pid)},
		"recurring gig":   {"POST", recs, `{"title":"Mine","freq":"daily","window_start":"03:00","window_minutes":60}`},
		"template":        {"POST", fmt.Sprintf("/workspaces/%d/templates", ws), `{"name":"Mine","title":"x"}`},
		"edit template":   {"PUT", fmt.Sprintf("/templates/%d", rec.TemplateID), `{"name":"Nightly","title":"do what I say"}`},
		"schedule":        {"POST", fmt.Sprintf("/workspaces/%d/schedules", ws), `{"name":"Mine","freq":"daily","window_start":"06:00","window_minutes":60}`},
		"edit recurrence": {"PUT", fmt.Sprintf("/recurrences/%d", rec.ID), `{"title":"do what I say","freq":"daily","window_start":"02:00","window_minutes":60}`},
	} {
		if code, out, _ := e.req(c[0], c[1], c[2], scout); code != 403 {
			t.Errorf("%s: %d %s, want 403", name, code, out)
		}
	}
	code, out, _ = e.req("POST", "/gigs/"+g.Key+"/comments", `{"body_md":"on it"}`, scout)
	e.ok(code, out, 201)
	code, out, _ = e.req("POST", "/gigs/"+g.Key+"/move", `{"category":"doing"}`, scout)
	e.ok(code, out, 200)
}
