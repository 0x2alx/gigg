package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestUserPasswordState: admins see which human users have no password (they can't log in yet);
// agents carry no flag, and nobody else sees it.
func TestUserPasswordState(t *testing.T) {
	e := newEnv(t)
	code, out, _ := e.req("POST", "/users", `{"username":"carol","kind":"human"}`, e.ada())
	e.ok(code, out, 201)

	code, out, _ = e.req("GET", "/users", "", e.ada())
	e.ok(code, out, 200)
	var list struct {
		Items []struct {
			Username    string `json:"username"`
			HasPassword *bool  `json:"has_password"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, u := range list.Items {
		got[u.Username] = "absent"
		if u.HasPassword != nil {
			got[u.Username] = fmt.Sprint(*u.HasPassword)
		}
	}
	for name, want := range map[string]string{"ada": "true", "bob": "true", "carol": "false", "scout": "absent"} {
		if got[name] != want {
			t.Errorf("admin list: %s has_password = %s, want %s", name, got[name], want)
		}
	}
	code, out, _ = e.req("GET", fmt.Sprintf("/users/%d", e.ids["bob"]), "", e.ada())
	e.ok(code, out, 200)
	if !strings.Contains(string(out), `"has_password":true`) {
		t.Errorf("admin get-user: no has_password: %s", out)
	}

	// a non-admin (an agent's workspace key) never sees the flag
	for _, p := range []string{"/users", fmt.Sprintf("/users/%d", e.ids["scout.user"])} {
		code, out, _ := e.req("GET", p, "", e.key("scout"))
		e.ok(code, out, 200)
		if strings.Contains(string(out), "has_password") {
			t.Errorf("%s as a non-admin shows has_password: %s", p, out)
		}
	}
}
