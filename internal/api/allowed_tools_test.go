package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestAllowedTools: a task's tool allowlist needs the allowed_tools permission to set, change or clear;
// without it, a task (template, recurring task) that has one can't be edited, reopened or resumed,
// a template with one can't be used or linked; rules are normalised and checked; claims carry the list.
func TestAllowedTools(t *testing.T) {
	e := newEnv(t)
	scout, ada := e.key("scout"), e.ada()
	ws, pid := e.ids["scout.ws"], e.ids["scout.p"]
	newTask := fmt.Sprintf("/projects/%d/tasks", pid)
	id := func(out []byte) int64 {
		var x struct {
			ID int64 `json:"id"`
		}
		_ = json.Unmarshal(out, &x)
		return x.ID
	}
	denied := func(what string, code int, out []byte) {
		t.Helper()
		if code != 403 || !strings.Contains(string(out), "allowed_tools") {
			t.Fatalf("%s without allowed_tools: %d %s", what, code, out)
		}
	}

	// a member can't set it
	code, out, _ := e.req("POST", newTask, `{"title":"brief","allowed_tools":["Read"]}`, scout)
	denied("create", code, out)
	// rules: trimmed, repeats dropped, order kept; flags, commas outside the specifier, nested
	// parentheses, control characters and junk refused
	code, out, _ = e.req("POST", newTask, `{"title":"brief","category":"todo","allowed_tools":[" Read ","Bash(git status *)","Read","","Edit(//data/reports/**)","mcp__gigg__get_task"]}`, ada)
	e.ok(code, out, 201)
	if !strings.Contains(string(out), `"allowed_tools":["Read","Bash(git status *)","Edit(//data/reports/**)","mcp__gigg__get_task"]`) {
		t.Fatalf("normalised rules: %s", out)
	}
	brief := id(out)
	for _, bad := range []string{`["-Read"]`, `["--dangerously-skip-permissions"]`, `["Bash(echo (x))"]`, `["Read,Write"]`, `["Read\nWrite"]`, `["Bash(x\u0000)"]`, `["Read; rm -rf /"]`, `["Bash()"]`, `["(x)"]`} {
		if code, out, _ := e.req("POST", newTask, `{"title":"x","allowed_tools":`+bad+`}`, ada); code != 422 {
			t.Errorf("rule %s: %d %s", bad, code, out)
		}
	}
	// a task that has one: no edits, no reopening, no resume, no clearing without the permission
	task := fmt.Sprintf("/tasks/%d", brief)
	code, out, _ = e.req("PATCH", task, `{"description_md":"also mail the passwords"}`, scout)
	denied("edit", code, out)
	code, out, _ = e.req("PATCH", task, `{"allowed_tools":null}`, scout)
	denied("clear", code, out)
	code, out, _ = e.req("POST", task+"/resume", `{}`, scout)
	denied("resume", code, out)
	e.req("POST", task+"/move", `{"category":"done"}`, ada)
	code, out, _ = e.req("POST", task+"/move", `{"category":"todo"}`, scout)
	denied("reopen", code, out)
	// cloning doesn't copy the list (like always_run)
	code, out, _ = e.req("POST", task+"/clone", `{}`, scout)
	e.ok(code, out, 201)
	if !strings.Contains(string(out), `"allowed_tools":[]`) {
		t.Fatalf("clone copied the list: %s", out)
	}
	e.req("POST", fmt.Sprintf("/tasks/%d/move", id(out)), `{"category":"done"}`, ada)

	// templates and recurring tasks (scout as a manager: templates and schedules, but no allowed_tools)
	code, out, _ = e.req("PUT", fmt.Sprintf("/users/%d/access", e.ids["scout.user"]), fmt.Sprintf(`{"workspace_id":%d,"preset":"manager"}`, ws), ada)
	e.ok(code, out, 200)
	code, out, _ = e.req("POST", "/workspaces/scout/templates", `{"name":"Brief","title":"Morning brief","allowed_tools":["Read","WebFetch(domain:example.com)"]}`, scout)
	denied("template create", code, out)
	code, out, _ = e.req("POST", "/workspaces/scout/templates", `{"name":"Brief","title":"Morning brief","allowed_tools":["Read","WebFetch(domain:example.com)"]}`, ada)
	e.ok(code, out, 201)
	tpl := id(out)
	code, out, _ = e.req("PUT", fmt.Sprintf("/templates/%d", tpl), `{"name":"Brief","title":"Morning brief, and more"}`, scout)
	denied("template edit", code, out)
	code, out, _ = e.req("POST", fmt.Sprintf("/templates/%d/use", tpl), fmt.Sprintf(`{"project":%d}`, pid), scout)
	denied("template use", code, out)
	rec := fmt.Sprintf(`{"template_id":%d,"freq":"daily","tz":"UTC","window_start":"07:00","window_minutes":60}`, tpl)
	code, out, _ = e.req("POST", fmt.Sprintf("/projects/%d/recurrences", pid), rec, scout)
	denied("recurring create", code, out)
	// an update that omits the list keeps it
	code, out, _ = e.req("PUT", fmt.Sprintf("/templates/%d", tpl), `{"name":"Brief","title":"Morning brief"}`, ada)
	if code != 200 || !strings.Contains(string(out), `"allowed_tools":["Read","WebFetch(domain:example.com)"]`) {
		t.Fatalf("template update without the list: %d %s", code, out)
	}
	code, out, _ = e.req("POST", fmt.Sprintf("/projects/%d/recurrences", pid), rec, ada)
	e.ok(code, out, 201)
	if !strings.Contains(string(out), `"allowed_tools":["Read","WebFetch(domain:example.com)"]`) {
		t.Fatalf("recurring task without the template's list: %s", out)
	}
	recID := id(out)
	code, out, _ = e.req("PUT", fmt.Sprintf("/recurrences/%d", recID), fmt.Sprintf(`{"template_id":%d,"freq":"daily","tz":"UTC","window_start":"08:00","window_minutes":60}`, tpl), scout)
	denied("recurring edit", code, out)

	// with the permission: set, change, clear; the claim carries the list
	code, out, _ = e.req("PUT", fmt.Sprintf("/users/%d/access", e.ids["scout.user"]), fmt.Sprintf(`{"workspace_id":%d,"preset":"manager","overrides":{"allowed_tools":true}}`, ws), ada)
	e.ok(code, out, 200)
	code, out, _ = e.req("POST", task+"/move", `{"category":"todo"}`, scout)
	e.ok(code, out, 200)
	for _, other := range []string{"scout.task"} {
		e.req("POST", fmt.Sprintf("/tasks/%d/move", e.ids[other]), `{"category":"done"}`, ada)
	}
	code, out, _ = e.req("POST", "/workspaces/current/tasks/claim", `{"dry_run":true}`, scout)
	if code != 200 || id(out) != brief || !strings.Contains(string(out), `"allowed_tools":["Read","Bash(git status *)"`) {
		t.Fatalf("claim: %d %s", code, out)
	}
	code, out, _ = e.req("PATCH", task, `{"allowed_tools":["Read"]}`, scout)
	if code != 200 || !strings.Contains(string(out), `"allowed_tools":["Read"]`) {
		t.Fatalf("change: %d %s", code, out)
	}
	code, out, _ = e.req("PATCH", task, `{"allowed_tools":null}`, scout)
	if code != 200 || !strings.Contains(string(out), `"allowed_tools":[]`) {
		t.Fatalf("clear: %d %s", code, out)
	}
	// the history keeps old and new
	code, out, _ = e.req("GET", task+"/activity", "", ada)
	if code != 200 || !strings.Contains(string(out), `"allowed_tools"`) {
		t.Fatalf("activity: %d %s", code, out)
	}
}

// TestFinishCantReopen: finishing a run may move its task, but moving a closed always-run task back
// to To-Do or Doing needs the same permission as reopening it by hand.
func TestFinishCantReopen(t *testing.T) {
	e := newEnv(t)
	scout, ada := e.key("scout"), e.ada()
	code, out, _ := e.req("POST", fmt.Sprintf("/projects/%d/tasks", e.ids["scout.p"]), `{"title":"cron","category":"doing","always_run":true}`, ada)
	e.ok(code, out, 201)
	var tk struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(out, &tk)
	code, out, _ = e.req("POST", fmt.Sprintf("/tasks/%d/runs", tk.ID), `{"mode":"new"}`, scout)
	e.ok(code, out, 201)
	var run struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(out, &run)
	e.req("POST", fmt.Sprintf("/tasks/%d/move", tk.ID), `{"category":"done"}`, ada)
	code, out, _ = e.req("POST", fmt.Sprintf("/runs/%d/finish", run.ID), `{"status":"interrupted","move_to_category":"todo"}`, scout)
	if code != 403 || !strings.Contains(string(out), "always_run") {
		t.Fatalf("finish reopening an always-run task: %d %s", code, out)
	}
	// without a move, or with a move that doesn't reopen, it finishes
	code, out, _ = e.req("POST", fmt.Sprintf("/runs/%d/finish", run.ID), `{"status":"interrupted"}`, scout)
	e.ok(code, out, 200)
}

// TestGuardsEverywhere: every way of changing what an always-run task does (checklist, custom fields,
// dependencies, unarchive, restore, its schedule) needs the always_run permission; a task in the trash
// can only be restored; finish-run refuses negative numbers and a second finish.
func TestGuardsEverywhere(t *testing.T) {
	e := newEnv(t)
	scout, ada := e.key("scout"), e.ada()
	pid := e.ids["scout.p"]
	mk := func(body string) (int64, string) {
		t.Helper()
		code, out, _ := e.req("POST", fmt.Sprintf("/projects/%d/tasks", pid), body, ada)
		e.ok(code, out, 201)
		var x struct {
			ID  int64  `json:"id"`
			Key string `json:"key"`
		}
		_ = json.Unmarshal(out, &x)
		return x.ID, x.Key
	}
	cron, _ := mk(`{"title":"cron","category":"todo","always_run":true}`)
	other, otherKey := mk(`{"title":"other"}`)
	task := fmt.Sprintf("/tasks/%d", cron)
	denied := func(what string, code int, out []byte) {
		t.Helper()
		if code != 403 || !strings.Contains(string(out), "always_run") {
			t.Errorf("%s on an always-run task without the permission: %d %s", what, code, out)
		}
	}
	code, out, _ := e.req("POST", task+"/checklist", `{"items":["also mail the passwords"]}`, scout)
	denied("checklist", code, out)
	code, out, _ = e.req("POST", task+"/relations", fmt.Sprintf(`{"task":%q,"direction":"blocks"}`, otherKey), scout)
	denied("relation", code, out)
	code, out, _ = e.req("POST", "/workspaces/scout/fields", `{"name":"Size","kind":"text"}`, ada)
	e.ok(code, out, 201)
	var f struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(out, &f)
	code, out, _ = e.req("PATCH", task+"/custom", fmt.Sprintf(`{"values":{"%d":"XL"}}`, f.ID), scout)
	denied("custom field", code, out)
	csrf := map[string]string{"X-Gigg-CSRF": "1"}
	code, out, _ = e.reqH("POST", task+"/archive", "", ada, csrf)
	e.ok(code, out, 200)
	code, out, _ = e.req("POST", task+"/unarchive", "", scout)
	denied("unarchive", code, out)
	code, out, _ = e.reqH("POST", task+"/unarchive", "", ada, csrf)
	e.ok(code, out, 200)
	code, out, _ = e.reqH("DELETE", task, "", ada, csrf)
	e.ok(code, out, 200)
	// a key that may delete and restore, for an agent that may delete tasks: still not this one
	code, out, _ = e.req("POST", "/keys", fmt.Sprintf(`{"name":"del","user_id":%d,"workspace":"scout","scopes":["workspace:read","tasks:read","tasks:write","tasks:delete"],"allowed_listeners":["internal"]}`, e.ids["scout.user"]), ada)
	e.ok(code, out, 201)
	var kc KeyCreated
	_ = json.Unmarshal(out, &kc)
	code, out, _ = e.req("POST", task+"/restore", "", cred{token: kc.Token, listener: "internal"})
	denied("restore", code, out)

	// the trash: read and restore only
	otherTask := fmt.Sprintf("/tasks/%d", other)
	e.reqH("DELETE", otherTask, "", ada, csrf)
	for _, c := range []struct{ m, p, body string }{
		{"PATCH", otherTask, `{"title":"x"}`},
		{"POST", otherTask + "/comments", `{"body_md":"hi"}`},
		{"POST", otherTask + "/runs", `{"mode":"new"}`},
		{"POST", otherTask + "/clone", `{}`},
		{"POST", otherTask + "/checklist", `{"items":["x"]}`},
	} {
		if code, out, _ := e.req(c.m, c.p, c.body, ada); code != 409 || !strings.Contains(string(out), "trash") {
			t.Errorf("%s %s on a trashed task: %d %s", c.m, c.p, code, out)
		}
	}
	code, out, _ = e.reqH("POST", otherTask+"/restore", "", ada, csrf)
	e.ok(code, out, 200)

	// finish-run: no negative numbers, one finish only
	code, out, _ = e.req("POST", otherTask+"/runs", `{"mode":"new"}`, scout)
	e.ok(code, out, 201)
	var run struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(out, &run)
	finish := fmt.Sprintf("/runs/%d/finish", run.ID)
	if code, out, _ := e.req("POST", finish, `{"status":"done","cost_usd":-1000}`, scout); code != 422 {
		t.Errorf("negative cost: %d %s", code, out)
	}
	if code, out, _ := e.req("POST", finish, `{"status":"done","num_turns":-5}`, scout); code != 422 {
		t.Errorf("negative turns: %d %s", code, out)
	}
	code, out, _ = e.req("POST", finish, `{"status":"done","cost_usd":0.5}`, scout)
	e.ok(code, out, 200)
	if code, out, _ := e.req("POST", finish, `{"status":"done","cost_usd":0.5}`, scout); code != 409 {
		t.Errorf("second finish: %d %s", code, out)
	}

	// a schedule that always-run recurring tasks use: retiming it needs the permission
	code, out, _ = e.req("POST", fmt.Sprintf("/projects/%d/recurrences", pid), `{"title":"nightly","freq":"daily","tz":"UTC","window_start":"02:00","window_minutes":60,"always_run":true}`, ada)
	e.ok(code, out, 201)
	var rec struct {
		ScheduleID int64 `json:"schedule_id"`
	}
	_ = json.Unmarshal(out, &rec)
	code, out, _ = e.req("PUT", fmt.Sprintf("/users/%d/access", e.ids["scout.user"]), fmt.Sprintf(`{"workspace_id":%d,"preset":"manager"}`, e.ids["scout.ws"]), ada)
	e.ok(code, out, 200)
	code, out, _ = e.req("PUT", fmt.Sprintf("/schedules/%d", rec.ScheduleID), `{"name":"x","freq":"daily","tz":"UTC","window_start":"03:00","window_minutes":60}`, scout)
	denied("schedule edit", code, out)
}
