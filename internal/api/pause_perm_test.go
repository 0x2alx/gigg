package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestPausePermission: pause_runs lets a non-admin pause and resume, per workspace or (with an
// all-workspaces rule and an unbound key) everywhere; a bound key stays in its workspace; the MCP
// tools follow the same rules.
func TestPausePermission(t *testing.T) {
	e := newEnv(t)
	atlas, scoutWS, atlasWS := e.ids["atlas.user"], e.ids["scout.ws"], e.ids["atlas.ws"]
	slugs := func(out []byte) int { return strings.Count(string(out), `"slug"`) }

	// without the permission: 403, also for a member of the workspace
	if code, out, _ := e.req("POST", "/runner/pause", `{"minutes":60}`, e.key("scout")); code != 403 || !strings.Contains(string(out), "pause_runs") {
		t.Fatalf("member without pause_runs: %d %s", code, out)
	}
	// an operator-style grant: everywhere, viewer + pause_runs
	code, out, _ := e.req("PUT", fmt.Sprintf("/users/%d/access", atlas), `{"workspace_id":null,"preset":"viewer","overrides":{"pause_runs":true}}`, e.ada())
	e.ok(code, out, 200)
	code, out, _ = e.req("POST", "/keys", fmt.Sprintf(`{"name":"ops","user_id":%d,"scopes":["workspace:read","gigs:read","runs:write"],"allowed_listeners":["internal"]}`, atlas), e.ada())
	e.ok(code, out, 201)
	var kc KeyCreated
	_ = json.Unmarshal(out, &kc)
	ops := cred{token: kc.Token, listener: "internal"}

	// the bound runner key reaches only its own workspace
	code, out, _ = e.req("POST", "/runner/pause", `{"minutes":60}`, e.key("atlas"))
	if code != 200 || slugs(out) != 1 || !strings.Contains(string(out), `"slug":"atlas"`) {
		t.Fatalf("bound key pause all: %d %s", code, out)
	}
	if code, _, _ := e.req("POST", "/runner/pause", fmt.Sprintf(`{"workspaces":[%d]}`, scoutWS), e.key("atlas")); code != 422 {
		t.Fatalf("bound key, other workspace: %d", code)
	}
	// the unbound key: every workspace
	code, out, _ = e.req("POST", "/runner/pause", `{}`, ops)
	if code != 200 || slugs(out) != 2 || strings.Contains(string(out), `"paused":false`) {
		t.Fatalf("unbound pause all: %d %s", code, out)
	}
	code, out, _ = e.req("POST", "/runner/resume", fmt.Sprintf(`{"workspaces":[%d]}`, scoutWS), ops)
	if code != 200 || slugs(out) != 1 || !strings.Contains(string(out), `"paused":false`) {
		t.Fatalf("unbound resume one: %d %s", code, out)
	}
	// caps stay admin-only
	if code, _, _ := e.req("PATCH", fmt.Sprintf("/workspaces/%d/runner", scoutWS), `{"daily_cap":99}`, ops); code != 403 {
		t.Fatalf("caps by a pauser: %d", code)
	}
	// a key without runs:write can't, even with the permission
	code, out, _ = e.req("POST", "/keys", fmt.Sprintf(`{"name":"ro","user_id":%d,"scopes":["gigs:read"],"allowed_listeners":["internal"]}`, atlas), e.ada())
	e.ok(code, out, 201)
	_ = json.Unmarshal(out, &kc)
	if code, out, _ := e.req("POST", "/runner/pause", `{}`, cred{token: kc.Token, listener: "internal"}); code != 403 || !strings.Contains(string(out), "runs:write") {
		t.Fatalf("no runs:write: %d %s", code, out)
	}

	// MCP: runner_status for readers; pause_runs/resume_runs only where allowed, slugs accepted
	srv := e.mcpServer("internal")
	defer srv.Close()
	ctx := context.Background()
	tools := func(token string) (*mcp.ClientSession, map[string]bool) {
		s, err := e.mcpSession(t, srv, authTransport{token: token})
		if err != nil {
			t.Fatal(err)
		}
		l, err := s.ListTools(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]bool{}
		for _, tl := range l.Tools {
			m[tl.Name] = true
		}
		return s, m
	}
	sm, names := tools(e.keys["scout"])
	defer sm.Close()
	if !names["runner_status"] || names["pause_runs"] || names["resume_runs"] {
		t.Fatalf("scout's tools: %v", names)
	}
	so, names := tools(ops.token)
	defer so.Close()
	if !names["pause_runs"] || !names["resume_runs"] {
		t.Fatalf("ops tools: %v", names)
	}
	call := func(name string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		r, err := so.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return r
	}
	r := call("pause_runs", map[string]any{"workspaces": []string{"scout"}, "minutes": 0})
	if r.IsError || !strings.Contains(text(r), `"paused_open":true`) || strings.Count(text(r), `"slug"`) != 1 {
		t.Fatalf("pause_runs by slug: %s", text(r))
	}
	if r := call("pause_runs", map[string]any{"workspaces": []string{"nope"}, "minutes": 60}); !r.IsError || !strings.Contains(text(r), "nope") {
		t.Fatalf("unknown slug: %s", text(r))
	}
	if r := call("pause_runs", map[string]any{"minutes": -1}); !r.IsError {
		t.Fatalf("negative minutes: %s", text(r))
	}
	r = call("resume_runs", map[string]any{})
	if r.IsError || strings.Contains(text(r), `"paused":true`) || strings.Count(text(r), `"slug"`) != 2 {
		t.Fatalf("resume_runs all: %s", text(r))
	}
	r = call("runner_status", map[string]any{})
	if r.IsError || !strings.Contains(text(r), `"daily_cap"`) {
		t.Fatalf("runner_status: %s", text(r))
	}

	// taking the permission away in one workspace: a list naming it is refused, "all" skips it
	code, out, _ = e.req("PUT", fmt.Sprintf("/users/%d/access", atlas), fmt.Sprintf(`{"workspace_id":%d,"preset":"viewer","overrides":{"pause_runs":false}}`, scoutWS), e.ada())
	e.ok(code, out, 200)
	if code, _, _ := e.req("POST", "/runner/pause", fmt.Sprintf(`{"workspaces":[%d,%d]}`, atlasWS, scoutWS), ops); code != 403 {
		t.Fatalf("list with a denied workspace: %d", code)
	}
	code, out, _ = e.req("POST", "/runner/pause", `{"minutes":30}`, ops)
	if code != 200 || slugs(out) != 1 || !strings.Contains(string(out), `"slug":"atlas"`) {
		t.Fatalf("all minus denied: %d %s", code, out)
	}
}
