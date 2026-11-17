package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type authTransport struct {
	token, origin string
}

func (t authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if t.token != "" {
		r.Header.Set("Authorization", "Bearer "+t.token)
	}
	if t.origin != "" {
		r.Header.Set("Origin", t.origin)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func (e *env) mcpServer(listener string) *httptest.Server {
	h := e.a.MCPHandler()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Host = "board.example"
		h.ServeHTTP(w, r.WithContext(WithInfo(r.Context(), RequestInfo{Listener: listener})))
	}))
}

func (e *env) mcpSession(t *testing.T, srv *httptest.Server, tr authTransport) (*mcp.ClientSession, error) {
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	return c.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: srv.URL, HTTPClient: &http.Client{Transport: tr}}, nil)
}

func text(r *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range r.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestMCP(t *testing.T) {
	e := newEnv(t)
	srv := e.mcpServer("internal")
	defer srv.Close()
	ctx := context.Background()

	s, err := e.mcpSession(t, srv, authTransport{token: e.keys["scout"]})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tools, err := s.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tl := range tools.Tools {
		names[tl.Name] = true
	}
	for _, want := range []string{"whoami", "list_gigs", "get_gig", "create_gig", "move_gig", "add_comment", "report_progress", "summary"} {
		if !names[want] {
			t.Errorf("tool %s missing", want)
		}
	}
	if names["delete_gig"] {
		t.Error("delete_gig listed for a key without gigs:delete")
	}
	call := func(name string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		r, err := s.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return r
	}
	r := call("create_gig", map[string]any{"project": e.ids["scout.p"], "title": "via MCP", "priority": 2, "labels": []string{"mcp"}})
	if r.IsError || !strings.Contains(text(r), `"title":"via MCP"`) {
		t.Fatalf("create_gig: %s", text(r))
	}
	r = call("get_gig", map[string]any{"gig": "GIG-2", "comments": true})
	if r.IsError || !strings.Contains(text(r), `"comments"`) {
		t.Fatalf("get_gig: %s", text(r))
	}
	// another workspace's gig: an error, no data
	r = call("get_gig", map[string]any{"gig": "2"})
	if !r.IsError || strings.Contains(text(r), "atlas secret") {
		t.Fatalf("cross-workspace get_gig: %s", text(r))
	}
	r = call("summary", map[string]any{})
	if r.IsError || !strings.Contains(text(r), "via MCP") {
		t.Fatalf("summary: %s", text(r))
	}
	r = call("update_gig", map[string]any{"gig": "GIG-2", "clear": []string{"labels"}, "priority": 4})
	if r.IsError || !strings.Contains(text(r), `"labels":[]`) || !strings.Contains(text(r), `"priority":4`) {
		t.Fatalf("update_gig: %s", text(r))
	}
	// assignees by username, cleared with clear
	r = call("update_gig", map[string]any{"gig": "GIG-2", "assignees": []string{"@scout"}})
	if want := fmt.Sprintf(`"assignees":[%d]`, e.ids["scout.user"]); r.IsError || !strings.Contains(text(r), want) {
		t.Fatalf("update_gig assignees: %s", text(r))
	}
	r = call("update_gig", map[string]any{"gig": "GIG-2", "assignees": []string{"nobody-here"}})
	if !r.IsError || !strings.Contains(text(r), "No such user") {
		t.Fatalf("unknown assignee: %s", text(r))
	}
	r = call("update_gig", map[string]any{"gig": "GIG-2", "clear": []string{"assignees"}})
	if r.IsError || !strings.Contains(text(r), `"assignees":[]`) {
		t.Fatalf("clear assignees: %s", text(r))
	}
	r = call("create_gig", map[string]any{"project": e.ids["scout.p"], "title": "assigned via MCP", "assignees": []string{"scout"}})
	if want := fmt.Sprintf(`"assignees":[%d]`, e.ids["scout.user"]); r.IsError || !strings.Contains(text(r), want) {
		t.Fatalf("create_gig assignees: %s", text(r))
	}

	// schedules need the permission (not in the member preset): allow it for scout
	if code, out, _ := e.req("PUT", fmt.Sprintf("/users/%d/access", e.ids["scout.user"]), `{"workspace_id":null,"overrides":{"schedules":true}}`, e.ada()); code != 200 {
		t.Fatalf("grant: %d %s", code, out)
	}
	s.Close()
	if s, err = e.mcpSession(t, srv, authTransport{token: e.keys["scout"]}); err != nil {
		t.Fatal(err)
	}
	// recurring gigs: create (weekly, 09:00-10:00 New York), list with upcoming, partial update keeps the rest
	r = call("create_recurring", map[string]any{"project": e.ids["scout.p"], "title": "Weekly report", "freq": "weekly", "weekdays": []int{1, 4},
		"window_start": "09:00", "window_minutes": 60, "labels": []string{"routine"}, "checklist": []string{"draft", "send"}})
	if r.IsError || !strings.Contains(text(r), `"weekdays":[1,4]`) || !strings.Contains(text(r), `"upcoming":[`) || !strings.Contains(text(r), `"checklist":["draft","send"]`) {
		t.Fatalf("create_recurring: %s", text(r))
	}
	var created struct {
		ID         int64 `json:"id"`
		ScheduleID int64 `json:"schedule_id"`
		TemplateID int64 `json:"template_id"`
	}
	_ = json.Unmarshal([]byte(text(r)), &created)
	r = call("update_recurring", map[string]any{"id": created.ID, "window_minutes": 120, "paused": true})
	if r.IsError || !strings.Contains(text(r), `"window_minutes":120`) || !strings.Contains(text(r), `"weekdays":[1,4]`) ||
		!strings.Contains(text(r), `"labels":["routine"]`) || !strings.Contains(text(r), `"paused":true`) || !strings.Contains(text(r), `"checklist":["draft","send"]`) {
		t.Fatalf("update_recurring: %s", text(r))
	}
	r = call("list_recurring", map[string]any{})
	if r.IsError || !strings.Contains(text(r), "Weekly report") || strings.Contains(text(r), "atlas secret") {
		t.Fatalf("list_recurring: %s", text(r))
	}
	if r = call("delete_recurring", map[string]any{"id": e.ids["atlas.rec"]}); !r.IsError {
		t.Fatalf("deleted another workspace's recurring gig: %s", text(r))
	}
	// schedules: list, make one, put the template on it too (a second recurring gig), change its timing
	r = call("list_schedules", map[string]any{})
	if r.IsError || !strings.Contains(text(r), `"name":"Every Mon, Thu 09:00–11:00"`) {
		t.Fatalf("list_schedules: %s", text(r))
	}
	r = call("save_schedule", map[string]any{"name": "Fridays", "freq": "weekly", "weekdays": []int{5}, "window_start": "16:00", "window_minutes": 60})
	var sched struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal([]byte(text(r)), &sched)
	if r.IsError || sched.ID == 0 {
		t.Fatalf("save_schedule: %s", text(r))
	}
	r = call("create_recurring", map[string]any{"project": e.ids["scout.p"], "template_id": created.TemplateID, "schedule_id": sched.ID})
	if r.IsError || !strings.Contains(text(r), `"title":"Weekly report"`) || !strings.Contains(text(r), `"template_uses":2`) {
		t.Fatalf("pair a template and a schedule: %s", text(r))
	}
	r = call("save_schedule", map[string]any{"id": sched.ID, "window_start": "15:00"})
	if r.IsError || !strings.Contains(text(r), `"window_start":"15:00"`) || !strings.Contains(text(r), `"name":"Fridays"`) || !strings.Contains(text(r), `"uses":1`) {
		t.Fatalf("change a schedule: %s", text(r))
	}
	if r = call("delete_schedule", map[string]any{"id": sched.ID}); !r.IsError || !strings.Contains(text(r), "Weekly report") {
		t.Fatalf("deleted a schedule in use: %s", text(r))
	}

	// work items through MCP: subgig with a checklist, dependency, archive, bell
	r = call("create_gig", map[string]any{"project": e.ids["scout.p"], "title": "child", "parent_id": e.ids["scout.gig"], "checklist": []string{"a", "b"}})
	if r.IsError || !strings.Contains(text(r), `"checklist_total":2`) {
		t.Fatalf("create subgig: %s", text(r))
	}
	var child struct {
		Key string `json:"key"`
	}
	_ = json.Unmarshal([]byte(text(r)), &child)
	r = call("link_gigs", map[string]any{"gig": child.Key, "other": fmt.Sprint(e.ids["scout.gig"]), "direction": "blocked_by"})
	if r.IsError || !strings.Contains(text(r), `"blocked_by":1`) {
		t.Fatalf("link_gigs: %s", text(r))
	}
	r = call("list_gigs", map[string]any{"blocked": true})
	if r.IsError || !strings.Contains(text(r), child.Key) {
		t.Fatalf("list blocked: %s", text(r))
	}
	r = call("update_checklist", map[string]any{"gig": child.Key, "add": []string{"c"}})
	if r.IsError || strings.Count(text(r), `"text"`) != 3 {
		t.Fatalf("update_checklist: %s", text(r))
	}
	r = call("archive_gig", map[string]any{"gig": child.Key})
	if r.IsError || !strings.Contains(text(r), `"archived_at":1`) {
		t.Fatalf("archive_gig: %s", text(r))
	}
	if tools, _ := s.ListTools(ctx, nil); tools != nil {
		for _, tl := range tools.Tools {
			if tl.Name == "save_template" {
				t.Error("save_template listed for an agent without the templates permission")
			}
		}
	}
	r = call("notifications", map[string]any{"unread": true})
	if r.IsError || !strings.Contains(text(r), `"unread":`) {
		t.Fatalf("notifications: %s", text(r))
	}

	// no key, wrong listener, foreign Origin: refused before any tool runs
	if _, err := e.mcpSession(t, srv, authTransport{}); err == nil {
		t.Error("MCP without a key connected")
	}
	if _, err := e.mcpSession(t, srv, authTransport{token: e.keys["scout"], origin: "https://evil.example"}); err == nil {
		t.Error("MCP with a foreign Origin connected")
	}
	main := e.mcpServer("main")
	defer main.Close()
	if _, err := e.mcpSession(t, main, authTransport{token: e.keys["scout"]}); err == nil {
		t.Error("listener-restricted key connected on main")
	}
}

// TestMCPNoCreate: an agent whose user may not create gigs
// isn't offered create_gig or clone_gig, keeps update/move/comment, and the API refuses anyway.
func TestMCPNoCreate(t *testing.T) {
	e := newEnv(t)
	srv := e.mcpServer("internal")
	defer srv.Close()
	ctx := context.Background()
	if code, out, _ := e.req("PUT", fmt.Sprintf("/users/%d/access", e.ids["scout.user"]), fmt.Sprintf(`{"workspace_id":%d,"preset":"member","overrides":{"create_gigs":false}}`, e.ids["scout.ws"]), e.ada()); code != 200 {
		t.Fatalf("deny: %d %s", code, out)
	}
	// a consumer agent's MCP key: errands read-only
	body := fmt.Sprintf(`{"name":"mcp","user_id":%d,"workspace":"scout","scopes":["workspace:read","gigs:read","gigs:write","comments:read","comments:write","artifacts:write","runs:write","profile:write","errands:read","errands:output"],"allowed_listeners":["internal"]}`, e.ids["scout.user"])
	code, out, _ := e.req("POST", "/keys", body, e.ada())
	if code != 201 {
		t.Fatalf("mcp key: %d %s", code, out)
	}
	var kc KeyCreated
	_ = json.Unmarshal(out, &kc)
	code, out, _ = e.req("POST", "/workspaces/scout/errands", `{"name":"disk","command":"df -h /","worker":"scout","rule":{"freq":"hours","interval":2,"tz":"UTC"}}`, e.ada())
	if code != 201 {
		t.Fatalf("errand by ada: %d %s", code, out)
	}
	var er struct{ ID int64 }
	_ = json.Unmarshal(out, &er)
	s, err := e.mcpSession(t, srv, authTransport{token: kc.Token})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tools, err := s.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tl := range tools.Tools {
		names[tl.Name] = true
	}
	for _, gone := range []string{"create_gig", "clone_gig", "create_from_template", "save_errand", "pause_errand", "run_errand_now", "delete_errand"} {
		if names[gone] {
			t.Errorf("%s offered to an agent that may not create gigs or manage errands", gone)
		}
	}
	for _, want := range []string{"update_gig", "move_gig", "add_comment", "get_gig", "list_errands", "get_errand", "list_errand_runs", "get_errand_run", "list_errand_templates"} {
		if !names[want] {
			t.Errorf("%s missing", want)
		}
	}
	// reading errands needs only view (no Errands permission)
	for name, args := range map[string]map[string]any{"list_errands": {"workspace": "scout"}, "get_errand": {"id": er.ID}, "list_errand_runs": {"id": er.ID}} {
		r, err := s.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || r.IsError {
			t.Errorf("%s as an agent without Errands: %v %+v", name, err, r)
		}
	}
	if code, out, _ := e.req("POST", fmt.Sprintf("/projects/%d/gigs", e.ids["scout.p"]), `{"title":"x"}`, cred{token: kc.Token, listener: "internal"}); code != 403 {
		t.Fatalf("API create without create_gigs: %d %s", code, out)
	}
}
