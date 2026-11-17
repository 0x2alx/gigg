package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestUnboundKeyForMember: a key without a workspace for a non-admin acts like that user's
// session: exactly the member workspaces, at the membership's role (read live), limited by
// scopes, never admin. Also covers the workspace:write scope the custom-field routes need.
func TestUnboundKeyForMember(t *testing.T) {
	e := newEnv(t)
	bob := e.ids["bob"]

	// admin:* scopes stay on admin users' keys
	code, out, _ := e.req("POST", "/keys", fmt.Sprintf(`{"name":"x","user_id":%d,"scopes":["admin:users"]}`, bob), e.ada())
	if code != 422 {
		t.Fatalf("admin scope on a non-admin unbound key: %d %s", code, out)
	}
	code, out, _ = e.req("POST", "/keys", fmt.Sprintf(`{"name":"bob-all","user_id":%d,"scopes":["workspace:read","workspace:write","gigs:read"],"allowed_listeners":["internal"]}`, bob), e.ada())
	e.ok(code, out, 201)
	var kc KeyCreated
	_ = json.Unmarshal(out, &kc)
	if kc.Key.WorkspaceID != nil {
		t.Fatalf("expected an unbound key: %s", out)
	}
	key := cred{token: kc.Token, listener: "internal"}

	// bob is a viewer of scout only
	code, out, _ = e.req("GET", "/workspaces", "", key)
	e.ok(code, out, 200)
	if !strings.Contains(string(out), `"slug":"scout"`) || strings.Contains(string(out), `"slug":"atlas"`) {
		t.Fatalf("workspaces in reach: %s", out)
	}
	if code, out, _ = e.req("GET", "/workspaces/scout/gigs", "", key); code != 200 || !strings.Contains(string(out), "scout secret gig") {
		t.Fatalf("member workspace gigs: %d %s", code, out)
	}
	for _, p := range []string{"/workspaces/atlas/gigs", "/workspaces/atlas", fmt.Sprintf("/gigs/%d", e.ids["atlas.gig"]), "/workspaces/atlas/fields"} {
		if code, out, _ := e.req("GET", p, "", key); code != 404 {
			t.Errorf("%s from a non-member unbound key: %d %s", p, code, out)
		}
	}
	if code, out, _ := e.req("GET", "/users", "", key); code != 200 || strings.Contains(string(out), `"username":"atlas"`) {
		t.Errorf("/users from an unbound non-admin key lists only co-members: %d %s", code, out)
	}
	for _, p := range []string{"/keys", "/audit"} {
		if code, _, _ := e.req("GET", p, "", key); code != 403 && code != 404 {
			t.Errorf("%s: unbound non-admin key must not be an admin (%d)", p, code)
		}
	}
	// viewer: can't define fields; manager: can, with workspace:write on a key
	field := `{"name":"Effort","kind":"number"}`
	if code, out, _ := e.req("POST", "/workspaces/scout/fields", field, key); code != 403 {
		t.Fatalf("viewer defining a field: %d %s", code, out)
	}
	code, out, _ = e.req("PUT", fmt.Sprintf("/workspaces/scout/members/%d", bob), `{"role":"manager"}`, e.ada())
	e.ok(code, out, 204)
	code, out, _ = e.req("POST", "/workspaces/scout/fields", field, key)
	e.ok(code, out, 201)
	// a key without workspace:write can't, whatever the role
	code, out, _ = e.req("POST", "/keys", fmt.Sprintf(`{"name":"bob-ro","user_id":%d,"workspace":"scout","scopes":["workspace:read"]}`, bob), e.ada())
	e.ok(code, out, 201)
	var ro KeyCreated
	_ = json.Unmarshal(out, &ro)
	if code, out, _ := e.req("POST", "/workspaces/scout/fields", `{"name":"Other","kind":"text"}`, cred{token: ro.Token, listener: "main"}); code != 403 {
		t.Fatalf("field without workspace:write: %d %s", code, out)
	}

	// membership changes apply live: removed from scout, the key reaches nothing
	code, out, _ = e.reqH("DELETE", fmt.Sprintf("/workspaces/scout/members/%d", bob), "", e.ada(), map[string]string{"X-Gigg-CSRF": "1"})
	e.ok(code, out, 204)
	if code, out, _ := e.req("GET", "/workspaces/scout/gigs", "", key); code != 404 {
		t.Fatalf("after removal: %d %s", code, out)
	}
	code, out, _ = e.req("GET", "/workspaces", "", key)
	e.ok(code, out, 200)
	if strings.Contains(string(out), `"slug"`) {
		t.Fatalf("workspaces after removal: %s", out)
	}

	// a member can mint their own unbound key in a session-free way only through an existing
	// unbound key with keys:self; a workspace key still can't widen to unbound
	code, out, _ = e.req("POST", "/me/keys", `{"name":"wide","scopes":["gigs:read"]}`, e.key("scout"))
	if code != 403 {
		t.Fatalf("workspace key minting an unbound key: %d %s", code, out)
	}
}
