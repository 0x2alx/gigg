package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// libEnv: bob holds Shared library on the all-workspaces level and manages both workspaces; his keys
// differ only in their scopes.
type libEnv struct {
	shareEnv
	bob int64
}

func newLibEnv(t *testing.T) libEnv {
	e := newEnv(t)
	l := libEnv{shareEnv{e, t}, e.ids["bob"]}
	l.rule("null", `"preset":"viewer","overrides":{"shared_library":true}`)
	l.rule(fmt.Sprint(e.ids["scout.ws"]), `"preset":"manager"`)
	l.rule(fmt.Sprint(e.ids["atlas.ws"]), `"preset":"manager"`)
	return l
}

func (l libEnv) rule(level, body string) {
	l.t.Helper()
	code, out, _ := l.req("PUT", fmt.Sprintf("/users/%d/access", l.bob), fmt.Sprintf(`{"workspace_id":%s,%s}`, level, body), l.ada())
	l.ok(code, out, 200)
}

// mkKey makes an unlimited key for bob with library:write and the given scopes besides the read ones.
func (l libEnv) mkKey(name string, scopes ...string) cred {
	l.t.Helper()
	list, _ := json.Marshal(append([]string{"gigs:read", "errands:read", "workspace:read", "library:write"}, scopes...))
	code, out, _ := l.req("POST", "/keys", fmt.Sprintf(`{"name":%q,"user_id":%d,"scopes":%s,"allowed_listeners":["internal"]}`, name, l.bob, list), l.ada())
	l.ok(code, out, 201)
	var kc KeyCreated
	_ = json.Unmarshal(out, &kc)
	return cred{token: kc.Token, listener: "internal"}
}

func (l libEnv) status(method, path, body string, c cred) (int, string) {
	code, out, _ := l.req(method, path, body, c)
	return code, string(out)
}

// TestLibraryScopePerKind: library:write comes next to the scope of the kind: gigs:write for gig
// templates and schedules, errands:write for errand templates. A key with one of them and library:write
// doesn't reach the other kind (migration 0019 gave library:write to keys with either).
func TestLibraryScopePerKind(t *testing.T) {
	l := newLibEnv(t)
	ada := l.ada()
	gigsKey := l.mkKey("gigs", "gigs:write")
	errKey := l.mkKey("errs", "errands:write")

	tpl := idOf(t, l.post("/shared/templates", `{"name":"Brief","title":"brief"}`, ada, 201))
	sch := idOf(t, l.post("/shared/schedules", `{"name":"Mornings",`+daily7+`}`, ada, 201))
	etpl := idOf(t, l.post("/shared/errand-templates", `{"name":"ping","command":"true"}`, ada, 201))
	tplPath, schPath, etplPath := fmt.Sprintf("/templates/%d", tpl), fmt.Sprintf("/schedules/%d", sch), fmt.Sprintf("/errand-templates/%d", etpl)
	tplBody, schBody, etplBody := `{"name":"Brief","title":"brief 2"}`, `{"name":"Mornings","freq":"daily","tz":"UTC","window_start":"08:00","window_minutes":60}`, `{"name":"ping","command":"true --twice"}`

	type call struct{ method, path, body string }
	gigsCalls := []call{{"POST", "/shared/templates", tplBody}, {"PUT", tplPath, tplBody}, {"POST", "/shared/schedules", schBody}, {"PUT", schPath, schBody}}
	errCalls := []call{{"POST", "/shared/errand-templates", etplBody}, {"PUT", etplPath, etplBody}}
	for _, c := range gigsCalls {
		if code, out := l.status(c.method, c.path, c.body, errKey); code != 403 || !strings.Contains(out, "gigs:write") {
			t.Fatalf("errands-only key: %s %s: %d %s", c.method, c.path, code, out)
		}
	}
	for _, c := range errCalls {
		if code, out := l.status(c.method, c.path, c.body, gigsKey); code != 403 || !strings.Contains(out, "errands:write") {
			t.Fatalf("gigs-only key: %s %s: %d %s", c.method, c.path, code, out)
		}
	}
	// deletes too
	if code, out := l.status("DELETE", tplPath, "", errKey); code != 403 {
		t.Fatalf("errands-only key deletes a shared template: %d %s", code, out)
	}
	if code, out := l.status("DELETE", schPath, "", errKey); code != 403 {
		t.Fatalf("errands-only key deletes a shared schedule: %d %s", code, out)
	}
	if code, out := l.status("DELETE", etplPath, "", gigsKey); code != 403 {
		t.Fatalf("gigs-only key deletes a shared errand template: %d %s", code, out)
	}
	// each key still does its own kind
	for _, c := range gigsCalls {
		if code, out := l.status(c.method, c.path, c.body, gigsKey); code >= 400 {
			t.Fatalf("gigs key: %s %s: %d %s", c.method, c.path, code, out)
		}
	}
	for _, c := range errCalls {
		if code, out := l.status(c.method, c.path, c.body, errKey); code >= 400 {
			t.Fatalf("errands key: %s %s: %d %s", c.method, c.path, code, out)
		}
	}
	if code, out := l.status("DELETE", tplPath, "", gigsKey); code != 204 {
		t.Fatalf("gigs key deletes a shared template: %d %s", code, out)
	}
	if code, out := l.status("DELETE", etplPath, "", errKey); code != 204 {
		t.Fatalf("errands key deletes a shared errand template: %d %s", code, out)
	}
}

// TestMakeSharedScopePerKind: Make shared asks the scope of the kind next to library:write, and says so.
func TestMakeSharedScopePerKind(t *testing.T) {
	l := newLibEnv(t)
	ada := l.ada()
	scout := fmt.Sprint(l.ids["scout.ws"])
	gigsKey := l.mkKey("gigs", "gigs:write")
	errKey := l.mkKey("errs", "errands:write")
	sch := idOf(t, l.post("/workspaces/"+scout+"/schedules", `{"name":"Mornings",`+daily7+`}`, ada, 201))
	tpl := idOf(t, l.post("/workspaces/"+scout+"/templates", `{"name":"T","title":"t"}`, ada, 201))
	etpl := idOf(t, l.post("/workspaces/"+scout+"/errand-templates", `{"name":"ping","command":"true"}`, ada, 201))
	for path, c := range map[string]struct {
		bad  cred
		want string
	}{
		fmt.Sprintf("/schedules/%d/make-shared", sch):         {errKey, "gigs:write"},
		fmt.Sprintf("/templates/%d/make-shared", tpl):         {errKey, "gigs:write"},
		fmt.Sprintf("/errand-templates/%d/make-shared", etpl): {gigsKey, "errands:write"},
	} {
		for _, body := range []string{`{"dry_run":true}`, `{}`} {
			if code, out := l.status("POST", path, body, c.bad); code != 403 || !strings.Contains(out, c.want) {
				t.Fatalf("%s %s: %d %s, want 403 naming %s", path, body, code, out, c.want)
			}
		}
	}
	l.post(fmt.Sprintf("/schedules/%d/make-shared", sch), `{"dry_run":true}`, gigsKey, 200)
	l.post(fmt.Sprintf("/errand-templates/%d/make-shared", etpl), `{"dry_run":true}`, errKey, 200)
}

// TestRetimeSharedScheduleErrandsGuard: retiming a shared schedule that errands use needs Errands (on the
// all-workspaces level) and the errands:write scope, as retiming a workspace's schedule does.
func TestRetimeSharedScheduleErrandsGuard(t *testing.T) {
	l := newLibEnv(t)
	ada := l.ada()
	both := l.mkKey("both", "gigs:write", "errands:write")
	gigsKey := l.mkKey("gigs", "gigs:write")
	sch := idOf(t, l.post("/shared/schedules", `{"name":"Mornings",`+daily7+`}`, ada, 201))
	atlas := fmt.Sprint(l.ids["atlas.ws"])
	l.post("/workspaces/"+atlas+"/errands", fmt.Sprintf(`{"name":"ping","command":"true","worker":"atlas","schedule_id":%d}`, sch), ada, 201)
	path := fmt.Sprintf("/schedules/%d", sch)
	body := `{"name":"Mornings","freq":"daily","tz":"UTC","window_start":"08:00","window_minutes":60}`

	if code, out := l.status("PUT", path, body, gigsKey); code != 403 || !strings.Contains(out, "errands:write") {
		t.Fatalf("no errands:write scope: %d %s", code, out)
	}
	if code, out := l.status("PUT", path, body, both); code != 403 {
		t.Fatalf("no Errands permission on the all-workspaces level: %d %s", code, out)
	}
	l.rule("null", `"preset":"viewer","overrides":{"shared_library":true,"errands":true}`)
	if code, out := l.status("PUT", path, body, both); code != 200 {
		t.Fatalf("with both: %d %s", code, out)
	}
	if code, out := l.status("PUT", path, body, gigsKey); code != 403 {
		t.Fatalf("scope still missing: %d %s", code, out)
	}
}
