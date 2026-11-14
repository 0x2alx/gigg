package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestWorkspaceOwner: a workspace named like a user starts owned by them; /auth/me carries the
// owner for the sidebar; only admins change it (managers keep their own settings).
func TestWorkspaceOwner(t *testing.T) {
	e := newEnv(t)
	scout, bob := e.ids["scout.user"], e.ids["bob"]

	code, out, _ := e.req("GET", "/auth/me", "", e.key("scout"))
	e.ok(code, out, 200)
	if !strings.Contains(string(out), fmt.Sprintf(`"owner_id":%d`, scout)) || !strings.Contains(string(out), `"color":null`) {
		t.Fatalf("me: no owner or colour: %s", out)
	}
	// a manager that isn't an admin: settings yes, owner no
	code, out, _ = e.req("PUT", fmt.Sprintf("/workspaces/scout/members/%d", bob), `{"role":"manager"}`, e.ada())
	e.ok(code, out, 204)
	code, out, _ = e.req("POST", "/keys", fmt.Sprintf(`{"name":"bob","user_id":%d,"scopes":["workspace:read","projects:write"],"allowed_listeners":["internal"]}`, bob), e.ada())
	e.ok(code, out, 201)
	var kc KeyCreated
	_ = json.Unmarshal(out, &kc)
	key := cred{token: kc.Token, listener: "internal"}
	if code, out, _ := e.req("PATCH", "/workspaces/scout", `{"name":"Scout's"}`, key); code != 200 {
		t.Fatalf("manager renames: %d %s", code, out)
	}
	if code, out, _ := e.req("PATCH", "/workspaces/scout", fmt.Sprintf(`{"owner_id":%d}`, bob), key); code != 403 {
		t.Fatalf("manager sets the owner: %d %s", code, out)
	}
	// admins: set, refuse an unknown user, clear
	code, out, _ = e.req("PATCH", "/workspaces/scout", fmt.Sprintf(`{"owner_id":%d}`, bob), e.ada())
	e.ok(code, out, 200)
	if !strings.Contains(string(out), fmt.Sprintf(`"owner_id":%d`, bob)) {
		t.Fatalf("owner not set: %s", out)
	}
	if code, out, _ := e.req("PATCH", "/workspaces/scout", `{"owner_id":999999}`, e.ada()); code != 422 {
		t.Fatalf("unknown owner: %d %s", code, out)
	}
	code, out, _ = e.req("PATCH", "/workspaces/scout", `{"owner_id":null}`, e.ada())
	e.ok(code, out, 200)
	if !strings.Contains(string(out), `"owner_id":null`) {
		t.Fatalf("owner not cleared: %s", out)
	}
}

// TestAdminKeyCoMembers: an admin's key without admin:users still lists the people of every
// workspace (it reaches them all), not just the admins.
func TestAdminKeyCoMembers(t *testing.T) {
	e := newEnv(t)
	code, out, _ := e.req("POST", "/keys", fmt.Sprintf(`{"name":"plain","user_id":%d,"scopes":["workspace:read","tasks:read"],"allowed_listeners":["internal"]}`, e.ids["ada"]), e.ada())
	e.ok(code, out, 201)
	var kc KeyCreated
	_ = json.Unmarshal(out, &kc)
	code, out, _ = e.req("GET", "/users", "", cred{token: kc.Token, listener: "internal"})
	e.ok(code, out, 200)
	for _, u := range []string{"scout", "atlas", "bob"} {
		if !strings.Contains(string(out), `"username":"`+u+`"`) {
			t.Errorf("admin key: %s missing from /users: %s", u, out)
		}
	}
}
