package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestWorkItems walks relations, checklists, archive, custom fields, templates, saved filters and
// the bell through the API, as ada (session) and scout (agent key).
func TestWorkItems(t *testing.T) {
	e := newEnv(t)
	ada, scout := e.ada(), e.key("scout")
	pid := e.ids["scout.p"]
	mk := func(title string, c cred) map[string]any {
		t.Helper()
		code, out, _ := e.req("POST", fmt.Sprintf("/projects/%d/tasks", pid), `{"title":"`+title+`","checklist":["one","two"]}`, c)
		e.ok(code, out, 201)
		var m map[string]any
		_ = json.Unmarshal(out, &m)
		return m
	}
	a, b := mk("blocker", ada), mk("blocked", ada)
	ak, bk := a["key"].(string), b["key"].(string)
	if a["checklist_total"].(float64) != 2 {
		t.Fatalf("checklist on create: %v", a)
	}

	// blocks: b can't be claimed while a is open; cycles refused
	code, out, _ := e.req("POST", "/tasks/"+ak+"/relations", `{"task":"`+bk+`","direction":"blocks"}`, scout)
	e.ok(code, out, 200)
	if code, out, _ := e.req("POST", "/tasks/"+bk+"/relations", `{"task":"`+ak+`","direction":"blocks"}`, scout); code != 409 {
		t.Fatalf("cycle: %d %s", code, out)
	}
	code, out, _ = e.req("GET", "/tasks/"+bk, "", scout)
	if !strings.Contains(string(out), `"blocked_by":1`) {
		t.Fatalf("blocked_by: %s", out)
	}
	code, out, _ = e.req("GET", "/tasks/"+bk+"/relations", "", scout)
	if code != 200 || !strings.Contains(string(out), `"kind":"blocks"`) || !strings.Contains(string(out), `"title":"blocker"`) {
		t.Fatalf("relations: %d %s", code, out)
	}
	claimSeq := func() []string {
		var keys []string
		for i := 0; i < 5; i++ {
			code, out, _ := e.req("POST", "/workspaces/scout/tasks/claim", `{}`, scout)
			if code == 204 {
				break
			}
			e.ok(code, out, 200)
			var c struct {
				Key string `json:"key"`
			}
			_ = json.Unmarshal(out, &c)
			keys = append(keys, c.Key)
		}
		return keys
	}
	got := claimSeq()
	for _, k := range got {
		if k == bk {
			t.Fatalf("claimed a blocked task: %v", got)
		}
	}
	if code, out, _ := e.req("GET", "/workspaces/scout/tasks?blocked=true", "", scout); code != 200 || !strings.Contains(string(out), bk) {
		t.Fatalf("blocked filter: %d %s", code, out)
	}

	// checklist: tick, move, delete; counts follow
	code, out, _ = e.req("GET", "/tasks/"+ak+"/checklist", "", scout)
	var cl struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	_ = json.Unmarshal(out, &cl)
	if len(cl.Items) != 2 {
		t.Fatalf("checklist: %s", out)
	}
	code, out, _ = e.req("PATCH", fmt.Sprintf("/tasks/%s/checklist/%d", ak, cl.Items[1].ID), `{"done":true,"after":0}`, scout)
	e.ok(code, out, 200)
	if !strings.Contains(string(out), `"checklist_done":1`) {
		t.Fatalf("tick: %s", out)
	}
	code, out, _ = e.req("GET", "/tasks/"+ak+"/checklist", "", scout)
	if !strings.Contains(string(out), fmt.Sprintf(`"items":[{"id":%d`, cl.Items[1].ID)) {
		t.Fatalf("move to top: %s", out)
	}

	// archive: off the list, back with archived=only; claim never hands it out
	code, out, _ = e.req("POST", "/tasks/"+ak+"/archive", "", scout)
	e.ok(code, out, 200)
	_, out, _ = e.req("GET", "/workspaces/scout/tasks", "", scout)
	if strings.Contains(string(out), ak) {
		t.Fatal("archived task listed")
	}
	_, out, _ = e.req("GET", "/workspaces/scout/tasks?archived=only", "", scout)
	if !strings.Contains(string(out), ak) {
		t.Fatalf("archived=only: %s", out)
	}
	e.req("POST", "/tasks/"+ak+"/unarchive", "", scout)

	// custom fields: agents can't define them by default; values are validated
	if code, _, _ := e.req("POST", "/workspaces/scout/fields", `{"name":"Size","kind":"select","options":["S","M","L"]}`, scout); code != 403 {
		t.Fatalf("agent defines a field: %d", code)
	}
	code, out, _ = e.req("POST", "/workspaces/scout/fields", `{"name":"Size","kind":"select","options":["S","M","L"]}`, ada)
	e.ok(code, out, 201)
	var f struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(out, &f)
	if code, _, _ := e.req("PATCH", "/tasks/"+ak+"/custom", fmt.Sprintf(`{"values":{"%d":"XL"}}`, f.ID), scout); code != 422 {
		t.Fatalf("bad select value: %d", code)
	}
	code, out, _ = e.req("PATCH", "/tasks/"+ak+"/custom", fmt.Sprintf(`{"values":{"%d":"M"}}`, f.ID), scout)
	e.ok(code, out, 200)
	if !strings.Contains(string(out), fmt.Sprintf(`"custom":{"%d":"M"}`, f.ID)) {
		t.Fatalf("custom value: %s", out)
	}
	if code, _, _ := e.req("PATCH", "/tasks/"+ak+"/custom", fmt.Sprintf(`{"values":{"%d":"M"}}`, e.ids["atlas.field"]), scout); code != 422 {
		t.Fatalf("another workspace's field: %d", code)
	}

	// templates: ada saves one; scout (no templates permission) can use it, not change it
	tpl := fmt.Sprintf(`{"name":"Bug","title":"Bug: ","priority":3,"labels":["bug"],"checklist":["repro","fix","test"],"custom":{"%d":"S"}}`, f.ID)
	code, out, _ = e.req("POST", "/workspaces/scout/templates", tpl, ada)
	e.ok(code, out, 201)
	var tp struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(out, &tp)
	if code, _, _ := e.req("POST", "/workspaces/scout/templates", tpl, scout); code != 403 {
		t.Fatalf("agent saves a template: %d", code)
	}
	code, out, _ = e.req("POST", fmt.Sprintf("/templates/%d/use", tp.ID), fmt.Sprintf(`{"project":%d,"title":"Bug: login loops"}`, pid), scout)
	e.ok(code, out, 201)
	if !strings.Contains(string(out), `"title":"Bug: login loops"`) || !strings.Contains(string(out), `"checklist_total":3`) ||
		!strings.Contains(string(out), `"priority":3`) || !strings.Contains(string(out), fmt.Sprintf(`"%d":"S"`, f.ID)) {
		t.Fatalf("from template: %s", out)
	}

	// mentions and questions ring ada's bell, not the author's
	code, out, _ = e.req("POST", "/tasks/"+bk+"/comments", `{"body_md":"@ada can you check? not `+"`@bob`"+`","kind":"question"}`, scout)
	e.ok(code, out, 201)
	code, out, _ = e.req("GET", "/me/notifications?unread=true", "", ada)
	if code != 200 || !strings.Contains(string(out), `"kind":"mention"`) || !strings.Contains(string(out), `"kind":"question"`) || !strings.Contains(string(out), `"unread":2`) {
		t.Fatalf("bell: %d %s", code, out)
	}
	if _, out, _ := e.req("GET", "/me/notifications", "", e.key("atlas")); strings.Contains(string(out), bk) {
		t.Fatal("atlas sees scout's notifications")
	}
	code, out, _ = e.req("POST", "/me/notifications/read", `{"all":true}`, ada)
	if code != 200 || !strings.Contains(string(out), `"marked":2`) {
		t.Fatalf("read all: %d %s", code, out)
	}

	// saved filters are personal
	if code, _, _ := e.req("POST", "/me/filters", `{"name":"Everywhere","query":{"priority_min":"3"}}`, scout); code != 404 {
		t.Fatalf("a workspace-bound key saved a cross-workspace filter: %d", code)
	}
	code, out, _ = e.req("POST", "/me/filters", fmt.Sprintf(`{"workspace_id":%d,"name":"Urgent","query":{"priority_min":"3"}}`, e.ids["scout.ws"]), scout)
	e.ok(code, out, 201)
	if _, out, _ := e.req("GET", "/me/filters", "", ada); strings.Contains(string(out), "Urgent") {
		t.Fatal("ada sees scout's filter")
	}
	if code, _, _ := e.req("POST", "/me/filters", fmt.Sprintf(`{"workspace_id":%d,"name":"x","query":{"drop":"table"}}`, e.ids["scout.ws"]), scout); code != 422 {
		t.Fatalf("unknown filter key: %d", code)
	}
}

// TestBadInputAndIsolation: non-finite numbers are refused, one failing schedule doesn't stop the
// others, credential routes aren't replayed, and a workspace-bound key stays in its workspace.
func TestBadInputAndIsolation(t *testing.T) {
	e := newEnv(t)
	ada, scout := e.ada(), e.key("scout")
	pid := e.ids["scout.p"]

	// NaN/Inf numbers are refused (they used to panic in JSON encoding), also through MCP
	code, out, _ := e.req("POST", "/workspaces/scout/fields", `{"name":"Points","kind":"number"}`, ada)
	e.ok(code, out, 201)
	var f struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(out, &f)
	for _, bad := range []string{`"NaN"`, `"Inf"`, `"-Infinity"`} {
		if code, out, _ := e.req("PATCH", fmt.Sprintf("/tasks/%d/custom", e.ids["scout.task"]), fmt.Sprintf(`{"values":{"%d":%s}}`, f.ID, bad), scout); code != 422 {
			t.Fatalf("number %s: %d %s", bad, code, out)
		}
	}
	if code, _, _ := e.req("PATCH", fmt.Sprintf("/tasks/%d/custom", e.ids["scout.task"]), fmt.Sprintf(`{"values":{"0%d":1}}`, f.ID), scout); code != 422 {
		t.Fatalf("aliased field key accepted: %d", code)
	}

	// a schedule in an archived project, and one whose task creation fails, don't stop the others
	code, out, _ = e.req("POST", fmt.Sprintf("/projects/%d/recurrences", pid), `{"title":"ok","freq":"daily","tz":"UTC","window_start":"00:00","window_minutes":1440}`, ada)
	e.ok(code, out, 201)
	if code, _, _ := e.req("POST", fmt.Sprintf("/projects/%d/recurrences", pid), `{"title":"bad","freq":"daily","labels":[""]}`, ada); code != 422 {
		t.Fatalf("empty label accepted on a schedule: %d", code)
	}
	if code, _, _ := e.req("POST", fmt.Sprintf("/projects/%d/recurrences", pid), `{"title":"far","freq":"daily","starts_on":99999999999999}`, ada); code != 422 {
		t.Fatalf("starts_on far in the future accepted: %d", code)
	}
	code, out, _ = e.req("POST", fmt.Sprintf("/projects/%d/recurrences", pid), `{"title":"healthy","freq":"daily","tz":"UTC","window_start":"00:00","window_minutes":1440}`, ada)
	e.ok(code, out, 201)
	// both due again now with overlap=create (so a task is really created); break one behind the
	// API's back: its label is now invalid for CreateLabel, so creating its task fails
	due := time.Now().UnixMilli() - 60000
	if _, err := e.st.W.Exec(`UPDATE recurrences SET overlap = 'create', next_at = ? WHERE template_id IN (SELECT id FROM task_templates WHERE title IN ('ok','healthy'))`, due); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.W.Exec(`UPDATE task_templates SET labels = '["` + strings.Repeat("x", 60) + `"]' WHERE title = 'ok'`); err != nil {
		t.Fatal(err)
	}
	var before int
	_ = e.st.R.QueryRow(`SELECT created_count FROM recurrences WHERE template_id = (SELECT id FROM task_templates WHERE title = 'healthy')`).Scan(&before)
	res, err := e.st.Housekeeping(context.Background(), time.Now().UnixMilli())
	if err != nil || len(res.Paused) != 1 || len(res.Errors) == 0 || res.Created < 1 {
		t.Fatalf("housekeeping with a bad schedule: %+v %v", res, err)
	}
	var healthy int
	_ = e.st.R.QueryRow(`SELECT created_count FROM recurrences WHERE template_id = (SELECT id FROM task_templates WHERE title = 'healthy')`).Scan(&healthy)
	if healthy != before+1 {
		t.Fatalf("the healthy schedule didn't run: %d", healthy)
	}

	// credential routes are never stored for replay
	code, out, _ = e.reqH("POST", "/me/keys", `{"name":"idem","workspace":"scout","scopes":["tasks:read"]}`, scout, map[string]string{"Idempotency-Key": "k1"})
	e.ok(code, out, 201)
	var n int
	_ = e.st.R.QueryRow(`SELECT count(*) FROM idempotency_keys`).Scan(&n)
	if n != 0 {
		t.Fatalf("a key-creation response was stored for replay")
	}

	// a workspace-bound key can't mark other workspaces' notifications read
	if _, err := e.st.W.Exec(`INSERT INTO notifications (user_id, workspace_id, kind, created_at) VALUES (?, ?, 'mention', 1)`, e.ids["scout.user"], e.ids["atlas.ws"]); err != nil {
		t.Fatal(err)
	}
	e.req("POST", "/me/notifications/read", `{"all":true}`, scout)
	var unread int
	_ = e.st.R.QueryRow(`SELECT count(*) FROM notifications WHERE workspace_id = ? AND read_at IS NULL`, e.ids["atlas.ws"]).Scan(&unread)
	if unread != 1 {
		t.Fatal("scout's key marked another workspace's notifications read")
	}
}
