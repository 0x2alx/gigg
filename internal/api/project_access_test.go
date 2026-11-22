package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestProjectGrant: an agent given one project of another workspace sees and works only that
// project (none of the workspace's other projects or workspace-level things), claims its gigs there
// across workspaces, and loses all of it the moment the grant goes. A key that follows its user
// needs no change; a limited key reaches only its places.
func TestProjectGrant(t *testing.T) {
	e := newEnv(t)
	ada := e.ada()
	side, main := e.ids["atlas.p2"], e.ids["atlas.p"]
	scout := e.ids["scout.user"]
	grant := func(role string) {
		t.Helper()
		m, body := "PUT", `{"role":"`+role+`"}`
		if role == "" {
			m, body = "DELETE", ""
		}
		code, out, _ := e.reqH(m, fmt.Sprintf("/projects/%d/members/%d", side, scout), body, ada, map[string]string{"X-Gigg-CSRF": "1"})
		e.ok(code, out, 204)
	}
	grant("consumer")
	if code, _, _ := e.req("PUT", fmt.Sprintf("/projects/%d/members/%d", side, scout), `{"role":"manager"}`, ada); code != 422 {
		t.Fatalf("manager on one project: %d", code)
	}

	code, out, _ := e.req("POST", "/keys", fmt.Sprintf(`{"name":"follow","user_id":%d,"scopes":["workspace:read","gigs:read","gigs:work","comments:read","comments:write","runs:write","errands:read"],"allowed_listeners":["internal"]}`, scout), ada)
	e.ok(code, out, 201)
	var kc KeyCreated
	_ = json.Unmarshal(out, &kc)
	follow := cred{token: kc.Token, listener: "internal"}

	mk := func(project int64, title, extra string) int64 {
		t.Helper()
		code, out, _ := e.req("POST", fmt.Sprintf("/projects/%d/gigs", project), `{"title":"`+title+`","category":"todo"`+extra+`}`, ada)
		e.ok(code, out, 201)
		var g struct{ ID int64 }
		_ = json.Unmarshal(out, &g)
		return g.ID
	}
	mine := mk(side, "side for scout", fmt.Sprintf(`,"priority":4,"assignees":[%d]`, scout))
	other := mk(side, "side for atlas", "")
	if code, out, _ := e.req("POST", fmt.Sprintf("/projects/%d/gigs", main), fmt.Sprintf(`{"title":"x","assignees":[%d]}`, scout), ada); code != 422 {
		t.Fatalf("scout assigned on atlas/main, which it can't see: %d %s", code, out)
	}

	// what scout sees of atlas: the workspace, one project, its gigs; nothing else
	for _, c := range []struct {
		path      string
		code      int
		has, hasn string
	}{
		{"/workspaces", 200, `"slug":"atlas"`, ""},
		{"/workspaces/atlas/projects", 200, `"slug":"side"`, `"slug":"main"`},
		{"/workspaces/atlas/gigs", 200, "side for scout", "atlas secret"},
		{fmt.Sprintf("/gigs/%d", mine), 200, "side for scout", ""},
		{fmt.Sprintf("/gigs/%d", other), 200, "side for atlas", ""},
		{fmt.Sprintf("/gigs/%d", e.ids["atlas.gig"]), 404, "", "atlas secret"},
		{"/workspaces/atlas/errands", 200, "", "atlas secret"},
		{fmt.Sprintf("/errands/%d", e.ids["atlas.errand"]), 404, "", "atlas secret"},
		{"/workspaces/atlas/templates", 200, "", "atlas secret"},
		{fmt.Sprintf("/templates/%d", e.ids["atlas.template"]), 404, "", "atlas secret"},
		{"/workspaces/atlas/schedules", 403, "", "atlas secret"},
		{"/workspaces/atlas/activity", 200, "", "atlas secret"},
		{fmt.Sprintf("/runs/%d", e.ids["atlas.run"]), 404, "", ""},
	} {
		code, out, _ := e.req("GET", c.path, "", follow)
		if code != c.code || (c.has != "" && !strings.Contains(string(out), c.has)) || (c.hasn != "" && strings.Contains(string(out), c.hasn)) {
			t.Errorf("GET %s: %d %s", c.path, code, out)
		}
	}
	code, out, _ = e.req("GET", "/auth/me", "", follow)
	e.ok(code, out, 200)
	var me MeBody
	_ = json.Unmarshal(out, &me)
	for _, w := range me.Workspaces {
		if w.Slug == "atlas" && (w.Whole || len(w.Permissions) != 0 || len(w.Projects) != 1 || w.Projects[0].ID != side ||
			strings.Join(w.Projects[0].Permissions, ",") != "view,comment,work_gigs,download_attachments,add_attachments") {
			t.Fatalf("me in atlas: %+v", w)
		}
	}

	// MCP with the key that follows its user: no workspace named = every workspace in reach
	srv := e.mcpServer("internal")
	defer srv.Close()
	ms, err := e.mcpSession(t, srv, authTransport{token: kc.Token})
	if err != nil {
		t.Fatal(err)
	}
	defer ms.Close()
	for tool, want := range map[string][]string{
		"list_gigs":     {`"slug":"scout"`, "scout secret gig", `"slug":"atlas"`, "side for scout"},
		"list_projects": {`"slug":"side"`, `"description_md"`},
		"summary":       {`"slug":"atlas"`},
	} {
		r, err := ms.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: map[string]any{}})
		if err != nil || r.IsError {
			t.Fatalf("%s: %v %s", tool, err, text(r))
		}
		got := text(r)
		for _, w := range want {
			if !strings.Contains(got, w) {
				t.Errorf("%s lacks %s: %.400s", tool, w, got)
			}
		}
		if strings.Contains(got, "atlas secret") || strings.Contains(got, `"slug":"main","name":"Main","key":"GIG"`) && strings.Contains(got, fmt.Sprintf(`"id":%d,"workspace_id":%d`, main, e.ids["atlas.ws"])) {
			t.Errorf("%s leaks atlas/main: %.600s", tool, got)
		}
	}

	// a consumer: comments and moves, no edits, nothing workspace-level
	code, out, _ = e.req("POST", fmt.Sprintf("/gigs/%d/comments", mine), `{"body_md":"on it"}`, follow)
	e.ok(code, out, 201)
	for _, c := range []struct{ m, path, body string }{
		{"PATCH", fmt.Sprintf("/gigs/%d", mine), `{"title":"renamed"}`},
		{"POST", fmt.Sprintf("/projects/%d/gigs", side), `{"title":"new"}`},
		{"PATCH", "/workspaces/atlas", `{"name":"x"}`},
		{"POST", "/workspaces/atlas/errands", `{"name":"x","command":"true","rule":{"freq":"daily","tz":"UTC"}}`},
	} {
		if code, out, _ := e.req(c.m, c.path, c.body, follow); code < 400 {
			t.Errorf("%s %s: %d %s", c.m, c.path, code, out)
		}
	}

	// the claim over every workspace in reach: the granted gig (priority 4) first, the other agent's never
	code, out, _ = e.req("POST", "/gigs/claim", `{"dry_run":true}`, follow)
	e.ok(code, out, 200)
	if !strings.Contains(string(out), "side for scout") {
		t.Fatalf("claim: %s", out)
	}

	// limited keys: only their places
	for _, c := range []struct {
		reach string
		code  int
	}{{`["scout"]`, 404}, {`["scout","atlas/side"]`, 200}, {fmt.Sprintf(`["atlas/%d"]`, side), 200}} {
		code, out, _ := e.req("POST", "/keys", fmt.Sprintf(`{"name":"lim","user_id":%d,"reach":%s,"scopes":["gigs:read"],"allowed_listeners":["internal"]}`, scout, c.reach), ada)
		e.ok(code, out, 201)
		var lk KeyCreated
		_ = json.Unmarshal(out, &lk)
		if code, out, _ := e.req("GET", fmt.Sprintf("/gigs/%d", mine), "", cred{token: lk.Token, listener: "internal"}); code != c.code {
			t.Errorf("reach %s: %d %s", c.reach, code, out)
		}
	}
	if code, out, _ := e.req("POST", "/keys", fmt.Sprintf(`{"name":"lim","user_id":%d,"reach":["atlas/main"],"scopes":["gigs:read"]}`, scout), ada); code != 422 {
		t.Errorf("a key for a project its user can't see: %d %s", code, out)
	}

	// the grant goes: everything goes with it, at once, for the same key
	grant("")
	if code, _, _ := e.req("GET", fmt.Sprintf("/gigs/%d", mine), "", follow); code != 404 {
		t.Fatalf("after the grant went: %d", code)
	}
	if code, out, _ := e.req("GET", "/workspaces", "", follow); strings.Contains(string(out), `"slug":"atlas"`) || code != 200 {
		t.Fatalf("atlas still listed: %s", out)
	}
	code, out, _ = e.req("GET", fmt.Sprintf("/gigs/%d", mine), "", ada)
	e.ok(code, out, 200)
	if strings.Contains(string(out), fmt.Sprintf(`"assignees":[%d]`, scout)) {
		t.Fatalf("scout still assigned: %s", out)
	}
	if code, out, _ := e.req("POST", "/gigs/claim", `{"dry_run":true}`, follow); code == 200 && strings.Contains(string(out), "side for scout") {
		t.Fatalf("claimed after the grant went: %s", out)
	}
}
