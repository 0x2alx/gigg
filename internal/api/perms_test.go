package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestAccess: the member preset has no schedules; a group can give it for all workspaces, the
// user's own rules beat the group (the workspace rule beats the all-workspaces one), presets and
// overrides combine, and only an admin's browser session changes any of it.
func TestAccess(t *testing.T) {
	e := newEnv(t)
	scout := e.key("scout")
	ws := e.ids["scout.ws"]
	sched := fmt.Sprintf("/projects/%d/recurrences", e.ids["scout.p"])
	body := `{"title":"Nightly","freq":"daily","window_start":"02:00","window_minutes":60}`
	newGig := fmt.Sprintf("/projects/%d/gigs", e.ids["scout.p"])

	if code, out, _ := e.req("POST", newGig, `{"title":"one-off"}`, scout); code != 201 {
		t.Fatalf("member one-off gig: %d %s", code, out)
	}
	code, out, _ := e.req("POST", sched, body, scout)
	if code != 403 || !strings.Contains(string(out), "schedules") {
		t.Fatalf("member schedule: %d %s", code, out)
	}
	code, out, _ = e.req("GET", "/auth/me", "", scout)
	if code != 200 || strings.Contains(string(out), `"schedules"`) || !strings.Contains(string(out), `"create_gigs"`) || !strings.Contains(string(out), `"role":"member"`) {
		t.Fatalf("me permissions: %s", out)
	}
	code, out, _ = e.req("GET", "/permissions", "", scout)
	if code != 200 || !strings.Contains(string(out), `"always_run"`) || !strings.Contains(string(out), `"presets"`) {
		t.Fatalf("permission list: %d %s", code, out)
	}

	// a group allowing schedules in all workspaces, with scout in it
	code, out, _ = e.req("POST", "/groups", fmt.Sprintf(`{"name":"planners","members":[%d]}`, e.ids["scout.user"]), e.ada())
	e.ok(code, out, 201)
	var g struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(out, &g)
	gAccess := fmt.Sprintf("/groups/%d/access", g.ID)
	code, out, _ = e.req("PUT", gAccess, `{"workspace_id":null,"overrides":{"schedules":true}}`, e.ada())
	if code != 200 || !strings.Contains(string(out), `"rules":[{`) {
		t.Fatalf("group rule: %d %s", code, out)
	}
	if code, out, _ := e.req("POST", sched, body, scout); code != 201 {
		t.Fatalf("schedule through the group: %d %s", code, out)
	}
	// the user's own all-workspaces deny beats the group's allow
	uAccess := fmt.Sprintf("/users/%d/access", e.ids["scout.user"])
	code, out, _ = e.req("PUT", uAccess, `{"workspace_id":null,"overrides":{"schedules":false}}`, e.ada())
	e.ok(code, out, 200)
	if !strings.Contains(string(out), `"name":"schedules","allowed":false,"source":"user_all"`) || !strings.Contains(string(out), `"planners"`) {
		t.Fatalf("resolved access: %s", out)
	}
	if code, _, _ := e.req("POST", sched, body, scout); code != 403 {
		t.Fatalf("user deny over group allow: %d", code)
	}
	// and the user's rule for this workspace beats their all-workspaces rule
	code, out, _ = e.req("PUT", uAccess, fmt.Sprintf(`{"workspace_id":%d,"preset":"member","overrides":{"schedules":true}}`, ws), e.ada())
	e.ok(code, out, 200)
	if code, out, _ := e.req("POST", sched, body, scout); code != 201 {
		t.Fatalf("workspace allow over all-workspaces deny: %d %s", code, out)
	}
	// a deny on one permission keeps the rest of the preset
	e.req("PUT", uAccess, fmt.Sprintf(`{"workspace_id":%d,"preset":"member","overrides":{"create_gigs":false}}`, ws), e.ada())
	if code, _, _ := e.req("POST", newGig, `{"title":"nope"}`, scout); code != 403 {
		t.Fatalf("create_gigs denied: %d", code)
	}
	if code, _, _ := e.req("GET", newGig, "", scout); code != 200 {
		t.Fatalf("still sees the project: %d", code)
	}
	// without their own rule for the workspace, scout can't see it, unless a group gives it
	e.req("PUT", uAccess, fmt.Sprintf(`{"workspace_id":%d,"preset":""}`, ws), e.ada())
	if code, _, _ := e.req("GET", newGig, "", scout); code != 404 {
		t.Fatalf("no rule, no workspace: %d", code)
	}
	e.req("PUT", gAccess, `{"workspace_id":null,"preset":"viewer","overrides":{"schedules":true}}`, e.ada())
	if code, _, _ := e.req("GET", newGig, "", scout); code != 200 {
		t.Fatalf("viewer through the group: %d", code)
	}
	if code, _, _ := e.req("POST", newGig, `{"title":"x"}`, scout); code != 403 {
		t.Fatalf("viewer can't create: %d", code)
	}
	code, out, _ = e.req("GET", fmt.Sprintf("/workspaces/%d/members", ws), "", e.ada())
	if code != 200 || !strings.Contains(string(out), fmt.Sprintf(`"user_id":%d,"role":"viewer","source":"group_all"`, e.ids["scout.user"])) {
		t.Fatalf("members show group access: %s", out)
	}
	// a bad permission name or preset is refused
	if code, _, _ := e.req("PUT", uAccess, `{"workspace_id":null,"overrides":{"fly":true}}`, e.ada()); code != 422 {
		t.Fatalf("unknown permission: %d", code)
	}
	if code, _, _ := e.req("PUT", uAccess, `{"workspace_id":null,"preset":"god"}`, e.ada()); code != 422 {
		t.Fatalf("unknown preset: %d", code)
	}

	// keys never change rules or groups, not even admin keys
	code, out, _ = e.req("POST", "/keys", `{"name":"users","scopes":["admin:users","admin:members"],"allowed_listeners":["internal"]}`, e.ada())
	e.ok(code, out, 201)
	var k KeyCreated
	_ = json.Unmarshal(out, &k)
	uk := cred{token: k.Token, listener: "internal"}
	for _, c := range []struct{ m, p, b string }{
		{"PUT", uAccess, `{"workspace_id":null,"overrides":{"custom_fields":true}}`},
		{"PUT", gAccess, `{"workspace_id":null,"preset":"manager"}`},
		{"POST", "/groups", `{"name":"evil"}`},
		{"PUT", fmt.Sprintf("/workspaces/%d/members/%d", ws, e.ids["scout.user"]), `{"role":"manager"}`},
		{"DELETE", fmt.Sprintf("/workspaces/%d/members/%d", ws, e.ids["scout.user"]), ``},
		{"PATCH", fmt.Sprintf("/groups/%d", g.ID), `{"members":[]}`},
		{"DELETE", fmt.Sprintf("/groups/%d", g.ID), ``},
	} {
		if code, out, _ := e.req(c.m, c.p, c.b, uk); code != 403 {
			t.Errorf("%s %s with an admin:users key: %d %s", c.m, c.p, code, out)
		}
	}
	if code, _, _ := e.req("GET", uAccess, "", uk); code != 200 {
		t.Errorf("admin key reading access: %d", code)
	}
	if code, _, _ := e.req("GET", "/groups", "", scout); code != 403 {
		t.Errorf("agent listing groups: %d", code)
	}
}
