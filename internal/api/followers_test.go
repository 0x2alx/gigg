package api

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/0x2alx/gigg/internal/store"
)

// TestFollowAPI: followers on create, PUT/GET/DELETE /gigs/{id}/follow with a read-only key (a
// personal setting, like the bell), and PATCH followers needing an edit.
func TestFollowAPI(t *testing.T) {
	e := newEnv(t)
	ada := e.ada()
	pid, scout := e.ids["scout.p"], e.ids["scout.user"]
	code, out, _ := e.req("POST", "/keys", fmt.Sprintf(`{"name":"ro","user_id":%d,"reach":["scout"],"scopes":["workspace:read","gigs:read"],"allowed_listeners":["internal"]}`, scout), ada)
	e.ok(code, out, 201)
	var kc KeyCreated
	_ = json.Unmarshal(out, &kc)
	ro := cred{token: kc.Token, listener: "internal"}

	code, out, _ = e.req("POST", fmt.Sprintf("/projects/%d/gigs", pid), fmt.Sprintf(`{"title":"Watch","followers":[%d]}`, scout), ada)
	e.ok(code, out, 201)
	var g store.Gig
	_ = json.Unmarshal(out, &g)
	if !slices.Contains(g.Followers, scout) {
		t.Fatalf("followers on create: %v", g.Followers)
	}
	gig := "/gigs/" + g.Key
	code, out, _ = e.req("DELETE", gig+"/follow", "", ro)
	e.ok(code, out, 200)
	_ = json.Unmarshal(out, &g)
	if slices.Contains(g.Followers, scout) {
		t.Fatalf("still following after DELETE: %v", g.Followers)
	}
	code, out, _ = e.req("PUT", gig+"/follow", "", ro)
	e.ok(code, out, 200)
	code, out, _ = e.req("GET", gig+"/follow", "", ro)
	e.ok(code, out, 200)
	var f followOut
	_ = json.Unmarshal(out, &f)
	if !f.Following || !slices.Contains(f.Followers, scout) {
		t.Fatalf("GET follow: %s", out)
	}
	// other people's follows are an edit
	code, out, _ = e.req("PATCH", gig, `{"followers":[]}`, ro)
	if code != 403 {
		t.Fatalf("read-only key PATCH followers: %d %s", code, out)
	}
	code, out, _ = e.req("PATCH", gig, `{"followers":null}`, ada)
	e.ok(code, out, 200)
	_ = json.Unmarshal(out, &g)
	if len(g.Followers) != 0 {
		t.Fatalf("followers after clearing: %v", g.Followers)
	}
}
