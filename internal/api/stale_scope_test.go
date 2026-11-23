package api

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

// TestStaleNeedsGigsRead: a key without gigs:read gets neither the stale_gigs MCP tool nor the
// REST route behind it.
func TestStaleNeedsGigsRead(t *testing.T) {
	e := newEnv(t)
	body := fmt.Sprintf(`{"name":"ws-only","user_id":%d,"reach":["scout"],"scopes":["workspace:read"],"allowed_listeners":["internal"]}`, e.ids["scout.user"])
	code, out, _ := e.req("POST", "/keys", body, e.ada())
	e.ok(code, out, 201)
	var kc KeyCreated
	_ = json.Unmarshal(out, &kc)
	c := cred{token: kc.Token, listener: "internal"}

	if code, out, _ := e.req("GET", "/workspaces/scout/stale", "", c); code != 403 {
		t.Fatalf("REST stale without gigs:read: %d %s", code, out)
	}
	srv := e.mcpServer("internal")
	defer srv.Close()
	s, err := e.mcpSession(t, srv, authTransport{token: kc.Token})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tools, err := s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range tools.Tools {
		if tl.Name == "stale_gigs" || tl.Name == "summary" {
			t.Fatalf("%s offered without gigs:read", tl.Name)
		}
	}
	// and with it, it is offered
	s2, err := e.mcpSession(t, srv, authTransport{token: e.keys["scout"]})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	tools, err = s2.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tl := range tools.Tools {
		found = found || tl.Name == "stale_gigs"
	}
	if !found {
		t.Fatal("stale_gigs missing with gigs:read")
	}
}
