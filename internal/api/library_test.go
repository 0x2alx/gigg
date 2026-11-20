package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestSharedLibraryPermission: someone who isn't an instance admin changes the shared library (gig
// templates, schedules, errand templates) with Shared library on an all-workspaces rule, view on that
// level and in every workspace, and a key not limited to some places that carries library:write. The
// settings with a permission of their own (always-run, tool allowlists, timeouts, no sandbox) need
// it on the all-workspaces level too.
func TestSharedLibraryPermission(t *testing.T) {
	e := newEnv(t)
	ada, bob := e.ada(), e.ids["bob"]
	ws := e.ids["scout.ws"]
	rule := func(level, body string) (int, []byte) {
		code, out, _ := e.req("PUT", fmt.Sprintf("/users/%d/access", bob), fmt.Sprintf(`{"workspace_id":%s,%s}`, level, body), ada)
		return code, out
	}
	mkKey := func(name, extra, scopes string) cred {
		code, out, _ := e.req("POST", "/keys", fmt.Sprintf(`{"name":%q,"user_id":%d,%s"scopes":[%s],"allowed_listeners":["internal"]}`, name, bob, extra, scopes), ada)
		e.ok(code, out, 201)
		var kc KeyCreated
		_ = json.Unmarshal(out, &kc)
		return cred{token: kc.Token, listener: "internal"}
	}
	write := `"gigs:read","gigs:write","errands:read","errands:write","workspace:read"`
	lib := mkKey("lib", "", write+`,"library:write"`)
	noScope := mkKey("noscope", "", write)
	bound := mkKey("bound", `"reach":["scout"],`, write+`,"library:write"`)
	tplBody := `{"name":"Brief","title":"Morning brief"}`

	// without the permission: 403, whatever the key
	if code, out, _ := e.req("POST", "/shared/templates", tplBody, lib); code != 403 {
		t.Fatalf("no permission: %d %s", code, out)
	}
	// a workspace or a project rule can't hold it
	if code, out := rule(fmt.Sprint(ws), `"preset":"viewer","overrides":{"shared_library":true}`); code != 422 {
		t.Fatalf("workspace rule: %d %s", code, out)
	}
	if code, out := rule(fmt.Sprint(ws), fmt.Sprintf(`"project_id":%d,"overrides":{"shared_library":true}`, e.ids["scout.p"])); code != 422 {
		t.Fatalf("project rule: %d %s", code, out)
	}
	// on the all-workspaces level, without view there: still not held
	code, out := rule("null", `"overrides":{"shared_library":true}`)
	e.ok(code, out, 200)
	if code, out, _ := e.req("POST", "/shared/templates", tplBody, lib); code != 403 {
		t.Fatalf("no view: %d %s", code, out)
	}
	code, out = rule("null", `"preset":"viewer","overrides":{"shared_library":true}`)
	e.ok(code, out, 200)

	// /auth/me: on the all-workspaces level, not in a workspace
	code, out, _ = e.req("GET", "/auth/me", "", lib)
	e.ok(code, out, 200)
	var me struct {
		All        []string `json:"all_workspaces"`
		Caps       []string `json:"capabilities"`
		Workspaces []struct {
			Permissions []string `json:"permissions"`
		} `json:"workspaces"`
	}
	_ = json.Unmarshal(out, &me)
	if !strings.Contains(strings.Join(me.All, ","), "shared_library") || !strings.Contains(strings.Join(me.Caps, ","), "shared_library") ||
		len(me.Workspaces) != 2 || strings.Contains(strings.Join(me.Workspaces[0].Permissions, ","), "shared_library") {
		t.Fatalf("/auth/me: %s", out)
	}

	// the key needs library:write, and must not be limited to some places
	if code, out, _ := e.req("POST", "/shared/templates", tplBody, noScope); code != 403 || !strings.Contains(string(out), "library:write") {
		t.Fatalf("no scope: %d %s", code, out)
	}
	if code, out, _ := e.req("POST", "/shared/templates", tplBody, bound); code != 403 {
		t.Fatalf("limited key: %d %s", code, out)
	}

	// make, change and delete all three
	id := func(out []byte) int64 {
		var x struct {
			ID int64 `json:"id"`
		}
		_ = json.Unmarshal(out, &x)
		return x.ID
	}
	code, out, _ = e.req("POST", "/shared/templates", tplBody, lib)
	e.ok(code, out, 201)
	tpl := id(out)
	code, out, _ = e.req("PUT", fmt.Sprintf("/templates/%d", tpl), `{"name":"Brief","title":"Morning brief v2"}`, lib)
	e.ok(code, out, 200)
	code, out, _ = e.req("POST", "/shared/schedules", `{"name":"Mornings","freq":"daily","tz":"UTC","window_start":"07:00","window_minutes":60}`, lib)
	e.ok(code, out, 201)
	sch := id(out)
	code, out, _ = e.req("PUT", fmt.Sprintf("/schedules/%d", sch), `{"name":"Mornings","freq":"daily","tz":"UTC","window_start":"08:00","window_minutes":60}`, lib)
	e.ok(code, out, 200)
	code, out, _ = e.req("POST", "/shared/errand-templates", `{"name":"ping","command":"true"}`, lib)
	e.ok(code, out, 201)
	etpl := id(out)
	code, out, _ = e.req("PUT", fmt.Sprintf("/errand-templates/%d", etpl), `{"name":"ping","command":"true --twice"}`, lib)
	e.ok(code, out, 200)

	// guarded settings: on the all-workspaces level
	for _, c := range []struct{ path, body string }{
		{"/shared/templates", `{"name":"A","title":"a","always_run":true}`},
		{"/shared/templates", `{"name":"T","title":"t","allowed_tools":["Read"]}`},
		{"/shared/templates", `{"name":"R","title":"r","run_timeout_minutes":30}`},
		{"/shared/errand-templates", `{"name":"slow","command":"true","timeout_s":600}`},
	} {
		if code, out, _ := e.req("POST", c.path, c.body, lib); code != 403 {
			t.Fatalf("%s %s without the permission: %d %s", c.path, c.body, code, out)
		}
	}
	if code, out, _ := e.req("POST", "/shared/errand-templates", `{"name":"bare","command":"true","unsandboxed":true}`, lib); code < 400 {
		t.Fatalf("no-sandbox shared errand template without the permission: %d %s", code, out)
	}
	// ... a workspace's grant doesn't count for the library
	code, out = rule(fmt.Sprint(ws), `"preset":"manager"`)
	e.ok(code, out, 200)
	if code, out, _ := e.req("POST", "/shared/templates", `{"name":"A","title":"a","always_run":true}`, lib); code != 403 {
		t.Fatalf("workspace manager sets always_run on a shared template: %d %s", code, out)
	}
	code, out = rule("null", `"preset":"viewer","overrides":{"shared_library":true,"always_run":true,"allowed_tools":true,"run_timeout":true,"errand_unsandboxed":true}`)
	e.ok(code, out, 200)
	for _, c := range []struct{ path, body string }{
		{"/shared/templates", `{"name":"A","title":"a","always_run":true}`},
		{"/shared/templates", `{"name":"T","title":"t","allowed_tools":["Read"]}`},
		{"/shared/templates", `{"name":"R","title":"r","run_timeout_minutes":30}`},
		{"/shared/errand-templates", `{"name":"slow","command":"true","timeout_s":600}`},
		{"/shared/errand-templates", `{"name":"bare","command":"true","unsandboxed":true}`},
	} {
		code, out, _ := e.req("POST", c.path, c.body, lib)
		e.ok(code, out, 201)
	}

	// someone who doesn't see every workspace loses it
	code, out = rule(fmt.Sprint(e.ids["atlas.ws"]), `"overrides":{"view":false}`)
	e.ok(code, out, 200)
	if code, out, _ := e.req("DELETE", fmt.Sprintf("/templates/%d", tpl), "", lib); code != 403 {
		t.Fatalf("not every workspace seen: %d %s", code, out)
	}
	code, out, _ = e.req("GET", fmt.Sprintf("/users/%d/access", bob), "", ada)
	e.ok(code, out, 200)
	var acc struct {
		Grid []struct {
			WorkspaceID int64 `json:"workspace_id"`
			Permissions []struct {
				Name   string `json:"name"`
				Source string `json:"source"`
			} `json:"permissions"`
		} `json:"grid"`
	}
	_ = json.Unmarshal(out, &acc)
	for _, r := range acc.Grid {
		for _, p := range r.Permissions {
			if p.Name == "shared_library" && ((r.WorkspaceID == 0 && p.Source != "needs_view") || (r.WorkspaceID != 0 && p.Source != "all_workspaces")) {
				t.Fatalf("grid row %d: %s", r.WorkspaceID, p.Source)
			}
		}
	}
	code, out = rule(fmt.Sprint(e.ids["atlas.ws"]), `"preset":""`)
	e.ok(code, out, 200)

	// deletes
	for _, p := range []string{fmt.Sprintf("/templates/%d", tpl), fmt.Sprintf("/schedules/%d", sch), fmt.Sprintf("/errand-templates/%d", etpl)} {
		code, out, _ := e.req("DELETE", p, "", lib)
		e.ok(code, out, 204)
	}

	// an admin key carries library:write too (the migration gives it to the ones in use)
	code, out, _ = e.req("POST", "/keys", fmt.Sprintf(`{"name":"adm","user_id":%d,"scopes":["gigs:read","gigs:write"],"allowed_listeners":["internal"]}`, e.ids["ada"]), ada)
	e.ok(code, out, 201)
	var kc KeyCreated
	_ = json.Unmarshal(out, &kc)
	if code, out, _ := e.req("POST", "/shared/templates", tplBody, cred{token: kc.Token, listener: "internal"}); code != 403 || !strings.Contains(string(out), "library:write") {
		t.Fatalf("admin key without library:write: %d %s", code, out)
	}
}
