package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestProjectErrands: Errands given for one project of another workspace: the agent makes, changes
// and runs errands there and nowhere else in that workspace; it can't reach the workspace's other
// templates or schedules by id; changing its errand's command never changes another project's errand
// through a template they share; its executor claims across workspaces; and the grant going takes the
// write access away while the errands stay visible to whoever sees the project.
func TestProjectErrands(t *testing.T) {
	e := newEnv(t)
	ada := e.ada()
	side, main, atlasWS := e.ids["atlas.p2"], e.ids["atlas.p"], e.ids["atlas.ws"]
	scout := e.ids["scout.user"]
	rule := func(overrides string) {
		t.Helper()
		code, out, _ := e.req("PUT", fmt.Sprintf("/users/%d/access", scout), fmt.Sprintf(`{"workspace_id":%d,"project_id":%d,"preset":"consumer","overrides":%s}`, atlasWS, side, overrides), ada)
		e.ok(code, out, 200)
	}
	rule(`{"errands":true}`)

	code, out, _ := e.req("POST", "/keys", fmt.Sprintf(`{"name":"errands","user_id":%d,"scopes":["workspace:read","errands:read","errands:write","errands:run","errands:output"],"allowed_listeners":["internal"]}`, scout), ada)
	e.ok(code, out, 201)
	var kc KeyCreated
	_ = json.Unmarshal(out, &kc)
	key := cred{token: kc.Token, listener: "internal"}

	type errandJSON struct {
		ID         int64  `json:"id"`
		ProjectID  *int64 `json:"project_id"`
		TemplateID int64  `json:"template_id"`
		Command    string `json:"command"`
		Version    int64  `json:"version"`
	}
	read := func(out []byte) errandJSON {
		var c errandJSON
		if err := json.Unmarshal(out, &c); err != nil {
			t.Fatalf("errand: %v %s", err, out)
		}
		return c
	}

	// no project named: the first one where scout may make errands (side), not atlas's first (main)
	code, out, _ = e.req("POST", "/workspaces/atlas/errands", `{"name":"side job","command":"echo side","rule":{"freq":"daily","tz":"UTC"}}`, key)
	e.ok(code, out, 201)
	mine := read(out)
	if mine.ProjectID == nil || *mine.ProjectID != side {
		t.Fatalf("project: %v", mine.ProjectID)
	}

	// nowhere else in atlas, and none of atlas's other templates or schedules by id
	for _, c := range []struct {
		body string
		code int
	}{
		{fmt.Sprintf(`{"name":"x","command":"true","rule":{"freq":"daily","tz":"UTC"},"project":%d}`, main), 403},
		{fmt.Sprintf(`{"template_id":%d,"rule":{"freq":"daily","tz":"UTC"}}`, e.ids["atlas.errandtpl"]), 422},
		{fmt.Sprintf(`{"name":"x","command":"true","schedule_id":%d}`, e.ids["atlas.sched"]), 422},
	} {
		if code, out, _ := e.req("POST", "/workspaces/atlas/errands", c.body, key); code != c.code {
			t.Errorf("POST %s: %d %s", c.body, code, out)
		}
	}
	if code, out, _ := e.req("PATCH", fmt.Sprintf("/errands/%d", mine.ID), fmt.Sprintf(`{"project":%d}`, main), key); code != 403 {
		t.Errorf("moving it to main: %d %s", code, out)
	}
	for _, c := range []struct{ m, path, body string }{
		{"PATCH", fmt.Sprintf("/errands/%d", e.ids["atlas.errand"]), `{"command":"true"}`},
		{"POST", fmt.Sprintf("/errands/%d/run-now", e.ids["atlas.errand"]), ""},
		{"POST", "/workspaces/atlas/errand-templates", `{"name":"t","command":"true"}`},
		{"PUT", fmt.Sprintf("/errand-templates/%d", mine.TemplateID), `{"name":"t","command":"rm -rf"}`},
	} {
		if code, out, _ := e.req(c.m, c.path, c.body, key); code < 400 {
			t.Errorf("%s %s: %d %s", c.m, c.path, code, out)
		}
	}

	// what it sees: its project's errands and the templates they use; nothing of main
	for _, c := range []struct{ path, has, hasn string }{
		{"/workspaces/atlas/errands", "side job", "atlas secret"},
		{"/workspaces/atlas/errand-templates", "side job", "atlas secret"},
		{fmt.Sprintf("/errand-templates/%d", mine.TemplateID), "side job", ""},
	} {
		code, out, _ := e.req("GET", c.path, "", key)
		if code != 200 || !strings.Contains(string(out), c.has) || (c.hasn != "" && strings.Contains(string(out), c.hasn)) {
			t.Errorf("GET %s: %d %s", c.path, code, out)
		}
	}
	if code, _, _ := e.req("GET", fmt.Sprintf("/errand-templates/%d", e.ids["atlas.errandtpl"]), "", key); code != 404 {
		t.Errorf("atlas's template: %d", code)
	}

	// a whole-workspace manager reuses scout's template for an errand in main; scout changing its own
	// command then gets its own copy, and main's errand keeps running what it ran
	code, out, _ = e.req("POST", "/workspaces/atlas/errands", fmt.Sprintf(`{"template_id":%d,"rule":{"freq":"daily","tz":"UTC"},"project":%d,"worker":"atlas"}`, mine.TemplateID, main), ada)
	e.ok(code, out, 201)
	theirs := read(out)
	code, out, _ = e.req("PATCH", fmt.Sprintf("/errands/%d", mine.ID), `{"command":"echo changed"}`, key)
	e.ok(code, out, 200)
	changed := read(out)
	if changed.Command != "echo changed" || changed.TemplateID == mine.TemplateID {
		t.Errorf("scout's errand: %+v (template was %d)", changed, mine.TemplateID)
	}
	code, out, _ = e.req("GET", fmt.Sprintf("/errands/%d", theirs.ID), "", ada)
	e.ok(code, out, 200)
	if got := read(out); got.Command != "echo side" || got.TemplateID != mine.TemplateID {
		t.Errorf("main's errand changed through scout: %+v", got)
	}
	// with no other errand on it, scout's edits go to its template in place
	code, out, _ = e.req("PATCH", fmt.Sprintf("/errands/%d", mine.ID), `{"command":"echo again"}`, key)
	e.ok(code, out, 200)
	if again := read(out); again.TemplateID != changed.TemplateID {
		t.Errorf("an unshared template was copied: %d -> %d", changed.TemplateID, again.TemplateID)
	}

	// its executor claims across workspaces; a paused workspace is skipped, by name
	code, out, _ = e.req("POST", fmt.Sprintf("/errands/%d/run-now", mine.ID), "", key)
	e.ok(code, out, 200)
	code, out, _ = e.req("POST", "/errands/claim", `{}`, key)
	e.ok(code, out, 200)
	if !strings.Contains(string(out), "echo again") || !strings.Contains(string(out), `"paused":false`) {
		t.Fatalf("claim: %s", out)
	}
	code, out, _ = e.req("POST", fmt.Sprintf("/errands/%d/run-now", mine.ID), "", key)
	e.ok(code, out, 200)
	code, out, _ = e.req("POST", "/runner/pause", fmt.Sprintf(`{"workspaces":[%d],"minutes":0,"gigs":false,"errands":true}`, atlasWS), ada)
	e.ok(code, out, 200)
	code, out, _ = e.req("POST", "/errands/claim", `{}`, key)
	e.ok(code, out, 200)
	if strings.Contains(string(out), "echo again") || !strings.Contains(string(out), `"paused_workspaces":["atlas"]`) {
		t.Errorf("paused claim: %s", out)
	}

	// the grant goes: the errand stays visible to scout (it sees the project), but no more changes
	rule(`{}`)
	if code, out, _ := e.req("PATCH", fmt.Sprintf("/errands/%d", mine.ID), `{"command":"true"}`, key); code != 403 {
		t.Errorf("after the grant went: %d %s", code, out)
	}
	if code, out, _ := e.req("GET", fmt.Sprintf("/errands/%d", mine.ID), "", key); code != 200 {
		t.Errorf("seeing it after: %d %s", code, out)
	}
}
