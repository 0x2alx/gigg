package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpFor opens an MCP session as a key of bob (see libEnv).
func (l libEnv) mcpFor(c cred) (func(name string, args map[string]any) *mcp.CallToolResult, map[string]bool) {
	l.t.Helper()
	srv := l.mcpServer("internal")
	l.t.Cleanup(srv.Close)
	s, err := l.mcpSession(l.t, srv, authTransport{token: c.token})
	if err != nil {
		l.t.Fatal(err)
	}
	l.t.Cleanup(func() { s.Close() })
	tools, err := s.ListTools(context.Background(), nil)
	if err != nil {
		l.t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tl := range tools.Tools {
		names[tl.Name] = true
	}
	return func(name string, args map[string]any) *mcp.CallToolResult {
		l.t.Helper()
		r, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			l.t.Fatalf("%s: %v", name, err)
		}
		return r
	}, names
}

// mkKeyNoLib: a key of bob without library:write.
func (l libEnv) mkKeyNoLib(name string, scopes ...string) cred {
	l.t.Helper()
	list, _ := json.Marshal(append([]string{"gigs:read", "errands:read", "workspace:read"}, scopes...))
	code, out, _ := l.req("POST", "/keys", fmt.Sprintf(`{"name":%q,"user_id":%d,"scopes":%s,"allowed_listeners":["internal"]}`, name, l.bob, list), l.ada())
	l.ok(code, out, 201)
	var kc KeyCreated
	_ = json.Unmarshal(out, &kc)
	return cred{token: kc.Token, listener: "internal"}
}

func decode(t *testing.T, r *mcp.CallToolResult, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(text(r)), v); err != nil {
		t.Fatalf("not JSON: %v: %s", err, text(r))
	}
}

func TestMCPMakeShared(t *testing.T) {
	l := newLibEnv(t)
	ada := l.ada()
	scout, atlas := fmt.Sprint(l.ids["scout.ws"]), fmt.Sprint(l.ids["atlas.ws"])
	src := idOf(t, l.post("/workspaces/"+scout+"/schedules", `{"name":"Mornings",`+daily7+`}`, ada, 201))
	cp := idOf(t, l.post("/workspaces/"+atlas+"/schedules", `{"name":"Mornings",`+daily7+`}`, ada, 201))
	tpl := idOf(t, l.post("/workspaces/"+scout+"/templates", `{"name":"T","title":"t"}`, ada, 201))
	etpl := idOf(t, l.post("/workspaces/"+scout+"/errand-templates", `{"name":"ping","command":"true"}`, ada, 201))

	call, names := l.mcpFor(l.mkKey("mcp", "gigs:write", "errands:write"))
	if !names["make_shared"] {
		t.Fatal("make_shared not listed")
	}
	// the default is a dry run: the plan, nothing changed
	r := call("make_shared", map[string]any{"kind": "schedule", "id": src})
	if r.IsError {
		t.Fatalf("plan: %s", text(r))
	}
	plan := planOut{}
	decode(t, r, &plan)
	if !plan.DryRun || len(plan.Copies) != 1 || plan.Copies[0].ID != cp || plan.Shared != nil {
		t.Fatalf("plan: %s", text(r))
	}
	if code, _, _ := l.req("GET", fmt.Sprintf("/schedules/%d", cp), "", l.ada()); code != 200 {
		t.Fatalf("the plan changed something: %d", code)
	}
	// confirming needs expect_copies
	r = call("make_shared", map[string]any{"kind": "schedule", "id": src, "dry_run": false})
	if !r.IsError || !strings.Contains(text(r), "expect_copies") {
		t.Fatalf("confirm without expect_copies: %s", text(r))
	}
	// copies that changed: 409, nothing happens
	r = call("make_shared", map[string]any{"kind": "schedule", "id": src, "dry_run": false, "expect_copies": []int64{}})
	if !r.IsError || !strings.Contains(text(r), "409") {
		t.Fatalf("stale expect_copies: %s", text(r))
	}
	r = call("make_shared", map[string]any{"kind": "schedule", "id": src, "dry_run": false, "expect_copies": []int64{cp}})
	if r.IsError {
		t.Fatalf("confirm: %s", text(r))
	}
	plan = planOut{}
	decode(t, r, &plan)
	if plan.DryRun || plan.Shared == nil || plan.Deleted != 2 {
		t.Fatalf("confirmed plan: %s", text(r))
	}
	if code, _, _ := l.req("GET", fmt.Sprintf("/schedules/%d", cp), "", l.ada()); code != 404 {
		t.Fatalf("the copy is still there: %d", code)
	}
	// the other kinds, by name; a bad kind
	for kind, id := range map[string]int64{"template": tpl, "errand_template": etpl} {
		if r := call("make_shared", map[string]any{"kind": kind, "id": id}); r.IsError || !strings.Contains(text(r), `"dry_run":true`) {
			t.Fatalf("%s plan: %s", kind, text(r))
		}
	}
	if r := call("make_shared", map[string]any{"kind": "gig", "id": 1}); !r.IsError {
		t.Fatalf("bad kind: %s", text(r))
	}

	// without library:write: 403 naming it, for the plan and the confirm
	noLib, _ := l.mcpFor(l.mkKeyNoLib("nolib", "gigs:write", "errands:write"))
	for _, args := range []map[string]any{{"kind": "template", "id": tpl}, {"kind": "template", "id": tpl, "dry_run": false, "expect_copies": []int64{}}} {
		if r := noLib("make_shared", args); !r.IsError || !strings.Contains(text(r), "403") || !strings.Contains(text(r), "library:write") {
			t.Fatalf("no library:write: %s", text(r))
		}
	}
	// without the kind's scope: 403 naming it
	gigsOnly, _ := l.mcpFor(l.mkKey("gigs", "gigs:write"))
	if r := gigsOnly("make_shared", map[string]any{"kind": "errand_template", "id": etpl}); !r.IsError || !strings.Contains(text(r), "errands:write") {
		t.Fatalf("no errands:write: %s", text(r))
	}
	// no write scope at all: not listed
	if _, n := l.mcpFor(l.mkKeyNoLib("ro")); n["make_shared"] || n["import_json"] {
		t.Fatal("make_shared or import_json listed for a read-only key")
	}
}

func TestMCPSharedSave(t *testing.T) {
	l := newLibEnv(t)
	call, names := l.mcpFor(l.mkKey("mcp", "gigs:write", "errands:write"))
	if !names["save_errand_template"] {
		t.Fatal("save_errand_template not listed")
	}
	made := map[string]int64{}
	for tool, args := range map[string]map[string]any{
		"save_template":        {"name": "Shared brief", "title": "brief", "shared": true},
		"save_schedule":        {"name": "Shared mornings", "freq": "daily", "tz": "UTC", "window_start": "07:00", "window_minutes": 60, "shared": true},
		"save_errand_template": {"name": "shared ping", "command": "true", "shared": true},
	} {
		r := call(tool, args)
		if r.IsError {
			t.Fatalf("%s: %s", tool, text(r))
		}
		var out struct {
			ID     int64 `json:"id"`
			Shared bool  `json:"shared"`
		}
		decode(t, r, &out)
		if !out.Shared || out.ID == 0 {
			t.Fatalf("%s did not make a shared item: %s", tool, text(r))
		}
		made[tool] = out.ID
	}
	for path, name := range map[string]string{"/shared/templates": "Shared brief", "/shared/schedules": "Shared mornings", "/shared/errand-templates": "shared ping"} {
		if code, out := l.status("GET", path, "", l.ada()); code != 200 || !strings.Contains(out, name) {
			t.Fatalf("%s: %d %s", path, code, out)
		}
	}
	// editing a shared one by id works under the same checks
	if r := call("save_template", map[string]any{"id": made["save_template"], "name": "Shared brief", "title": "brief 2"}); r.IsError || !strings.Contains(text(r), `"title":"brief 2"`) {
		t.Fatalf("edit shared template: %s", text(r))
	}
	if r := call("save_schedule", map[string]any{"id": made["save_schedule"], "window_minutes": 90}); r.IsError || !strings.Contains(text(r), `"window_minutes":90`) {
		t.Fatalf("edit shared schedule: %s", text(r))
	}
	if r := call("save_errand_template", map[string]any{"id": made["save_errand_template"], "command": "true --twice"}); r.IsError || !strings.Contains(text(r), "true --twice") || !strings.Contains(text(r), "shared ping") {
		t.Fatalf("edit shared errand template: %s", text(r))
	}
	// a workspace's errand template, without shared
	if r := call("save_errand_template", map[string]any{"workspace": fmt.Sprint(l.ids["scout.ws"]), "name": "local", "command": "true"}); r.IsError || strings.Contains(text(r), `"shared":true`) {
		t.Fatalf("workspace errand template: %s", text(r))
	}
	// without library:write the shared options are refused with 403; the workspace ones still work
	noLib, _ := l.mcpFor(l.mkKeyNoLib("nolib", "gigs:write", "errands:write"))
	for tool, args := range map[string]map[string]any{
		"save_template":        {"name": "x", "title": "x", "shared": true},
		"save_schedule":        {"name": "x", "freq": "daily", "shared": true},
		"save_errand_template": {"name": "x", "command": "true", "shared": true},
	} {
		if r := noLib(tool, args); !r.IsError || !strings.Contains(text(r), "403") || !strings.Contains(text(r), "library:write") {
			t.Fatalf("%s shared without library:write: %s", tool, text(r))
		}
	}
	if r := noLib("save_template", map[string]any{"name": "local", "title": "x", "workspace": fmt.Sprint(l.ids["scout.ws"])}); r.IsError {
		t.Fatalf("workspace template without library:write: %s", text(r))
	}
}

func TestMCPImportJSON(t *testing.T) {
	l := newLibEnv(t)
	call, names := l.mcpFor(l.mkKey("mcp", "gigs:write", "errands:write"))
	if !names["import_json"] {
		t.Fatal("import_json not listed")
	}
	type res struct {
		Row     int      `json:"row"`
		Kind    string   `json:"kind"`
		Name    string   `json:"name"`
		Status  string   `json:"status"`
		ID      int64    `json:"id"`
		Key     string   `json:"key"`
		Message string   `json:"message"`
		Ignored []string `json:"ignored"`
	}
	type out struct {
		DryRun  bool   `json:"dry_run"`
		Made    int    `json:"made"`
		Ready   int    `json:"ready"`
		Failed  int    `json:"failed"`
		Skipped int    `json:"skipped"`
		Order   string `json:"order"`
		Results []res  `json:"results"`
	}
	// a mixed list, pasted in the wrong order: the recurring gig names a template and a schedule of the
	// same list; the gig, the errand template and the errand come along
	list := `[
	  {"kind":"gig","title":"Check the backups","priority":2,"labels":["ops"],"assignees":["scout"],"id":99,"workspace":"x"},
	  {"kind":"recurring","template":"weekly report","schedule":"Weekday mornings"},
	  {"kind":"template","name":"Weekly report","title":"Write the weekly report","labels":["report"]},
	  {"kind":"schedule","name":"Weekday mornings","freq":"weekly","weekdays":[1,2,3,4,5],"window_start":"09:00","window_minutes":60,"tz":"UTC"},
	  {"kind":"errand_template","name":"Ping","command":"true"},
	  {"kind":"errand","name":"Nightly","command":"true","rule":{"freq":"daily","tz":"UTC"},"worker":"scout"}
	]`
	scout := fmt.Sprint(l.ids["scout.ws"])
	// dry run: the plan, nothing made
	r := call("import_json", map[string]any{"json": list, "workspace": scout, "dry_run": true})
	var o out
	decode(t, r, &o)
	if r.IsError || !o.DryRun || o.Ready != 6 || o.Made != 0 || o.Order == "" || len(o.Results) != 6 || o.Results[0].Status != "ready" {
		t.Fatalf("dry run: %s", text(r))
	}
	if !strings.Contains(text(r), `"ignored":["id","workspace"]`) {
		t.Fatalf("dropped fields not reported: %s", text(r))
	}
	if code, out := l.status("GET", "/workspaces/"+scout+"/schedules", "", l.ada()); code != 200 || strings.Contains(out, "Weekday mornings") {
		t.Fatalf("dry run made a schedule: %s", out)
	}
	// for real
	r = call("import_json", map[string]any{"json": list, "workspace": scout})
	o = out{}
	decode(t, r, &o)
	if r.IsError || o.Made != 6 || o.Failed != 0 || o.Skipped != 0 {
		t.Fatalf("import: %s", text(r))
	}
	for _, x := range o.Results {
		if x.Status != "made" || (x.ID == 0 && x.Key == "") {
			t.Fatalf("row %d: %+v", x.Row, x)
		}
	}
	if !strings.HasPrefix(o.Results[0].Key, "SCOUT-") && !strings.Contains(o.Results[0].Key, "-") {
		t.Fatalf("gig key: %+v", o.Results[0])
	}
	code, rec := l.status("GET", "/workspaces/"+scout+"/recurrences", "", l.ada())
	if code != 200 || !strings.Contains(rec, "Weekday mornings") || !strings.Contains(rec, "Weekly report") {
		t.Fatalf("recurring gig: %d %s", code, rec)
	}
	// the object form of json works too, and only the failing row fails, its dependants skipped
	r = call("import_json", map[string]any{"workspace": scout, "json": []any{
		map[string]any{"kind": "schedule", "name": "Bad", "freq": "weekly", "window_start": "9am"},
		map[string]any{"kind": "recurring_gig", "template_id": "x", "template": "Weekly report", "schedule": "Bad"},
		map[string]any{"kind": "gig", "title": "Still made"},
		map[string]any{"kind": "gig"},
		map[string]any{"kind": "nonsense"},
		map[string]any{"kind": "gig", "title": "Who", "assignees": []string{"nobody"}},
	}})
	o = out{}
	decode(t, r, &o)
	st := []string{}
	for _, x := range o.Results {
		st = append(st, x.Status)
	}
	if strings.Join(st, ",") != "failed,skipped,made,skipped,skipped,skipped" || o.Made != 1 || o.Failed != 1 || o.Skipped != 4 {
		t.Fatalf("statuses %v: %s", st, text(r))
	}
	if !strings.Contains(o.Results[0].Message, "window_start") || !strings.Contains(o.Results[1].Message, "wasn't created") || !strings.Contains(o.Results[5].Message, "Unknown assignee: nobody") {
		t.Fatalf("messages: %s", text(r))
	}
	// whole-input problems
	for _, bad := range []any{"", "{nope", "[]", `[{"kind":"gig","title":"x"}] trailing`} {
		if r := call("import_json", map[string]any{"json": bad, "workspace": scout}); !r.IsError {
			t.Fatalf("input %.20q accepted: %s", fmt.Sprint(bad), text(r))
		}
	}
	// the size cap is the UI's (the transport's own request cap is the same size, so test it directly)
	if _, why := runImport(importArgs{JSON: `[{"kind":"gig","title":"` + strings.Repeat("x", importMaxBytes) + `"}]`}, nil, nil); !strings.Contains(why, "over 1 MB") {
		t.Fatalf("size cap: %q", why)
	}
	many := make([]any, importMaxObjects+1)
	for i := range many {
		many[i] = map[string]any{"kind": "gig", "title": "x"}
	}
	if r := call("import_json", map[string]any{"json": many, "workspace": scout}); !r.IsError || !strings.Contains(text(r), "At most 100") {
		t.Fatalf("101 objects: %s", text(r))
	}
}

// A permission failure: the runner key of an agent (gigs:write, no errands:write) imports a list: its gig
// is made; the errand template is refused by the create call (403), and what depends on nothing else goes on.
// A shared import without library:write is refused per row.
func TestMCPImportJSONPermissions(t *testing.T) {
	e := newEnv(t)
	srv := e.mcpServer("internal")
	defer srv.Close()
	s, err := e.mcpSession(t, srv, authTransport{token: e.keys["scout"]})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: "import_json", Arguments: map[string]any{"json": []any{
		map[string]any{"kind": "gig", "title": "Allowed", "project": 1},
		map[string]any{"kind": "errand_template", "name": "Nope", "command": "true"},
		map[string]any{"kind": "template", "name": "Shared nope", "title": "x"},
	}, "workspace": fmt.Sprint(e.ids["scout.ws"]), "project": e.ids["scout.p"]}})
	if err != nil {
		t.Fatal(err)
	}
	if r.IsError {
		t.Fatalf("whole call failed: %s", text(r))
	}
	var o struct {
		Made    int `json:"made"`
		Failed  int `json:"failed"`
		Results []struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"results"`
	}
	decode(t, r, &o)
	if o.Made < 1 || o.Results[0].Status != "made" || o.Results[1].Status != "failed" || !strings.Contains(o.Results[1].Message, "errands:write") {
		t.Fatalf("scope failure: %s", text(r))
	}
	// shared, without the Shared library permission: failed per row, nothing made
	r, _ = s.CallTool(context.Background(), &mcp.CallToolParams{Name: "import_json", Arguments: map[string]any{"shared": true, "workspace": fmt.Sprint(e.ids["scout.ws"]), "json": []any{
		map[string]any{"kind": "template", "name": "Shared nope", "title": "x"},
	}}})
	o.Results = nil
	decode(t, r, &o)
	if len(o.Results) != 1 || o.Results[0].Status != "failed" {
		t.Fatalf("shared import without permission: %s", text(r))
	}
}
