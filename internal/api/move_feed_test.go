package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestMoveReachesSourceViewers: a gig moved to another project shows in the change feed of someone
// who sees only the project it left (gig.moved_out, saying nothing of the target), so their live
// board drops the card; whoever sees the target gets gig.moved as before, and the gig's own history
// shows the move once.
func TestMoveReachesSourceViewers(t *testing.T) {
	e := newEnv(t)
	ada, bob, ws := e.ada(), e.ids["bob"], e.ids["scout.ws"]
	src, dst, gig := e.ids["scout.p"], e.ids["scout.p2"], e.ids["scout.gig"]
	// bob views the workspace; a project rule hides the target project from him
	code, out, _ := e.req("PUT", fmt.Sprintf("/users/%d/access", bob), fmt.Sprintf(`{"workspace_id":%d,"project_id":%d,"overrides":{"view":false}}`, ws, dst), ada)
	e.ok(code, out, 200)
	code, out, _ = e.req("POST", "/keys", fmt.Sprintf(`{"name":"src","user_id":%d,"scopes":["workspace:read","gigs:read","comments:read"],"allowed_listeners":["internal"]}`, bob), ada)
	e.ok(code, out, 201)
	var kc KeyCreated
	_ = json.Unmarshal(out, &kc)
	srcOnly := cred{token: kc.Token, listener: "internal"}
	after := e.feed("/workspaces/scout/changes?after_id=latest", srcOnly).NextAfterID
	afterAll := e.feed("/workspaces/scout/changes?after_id=latest", e.key("scout")).NextAfterID

	code, out, _ = e.req("POST", fmt.Sprintf("/gigs/%d/move", gig), fmt.Sprintf(`{"project":%d}`, dst), ada)
	e.ok(code, out, 200)

	items := e.feed(fmt.Sprintf("/workspaces/scout/changes?after_id=%d", after), srcOnly).Items
	if len(items) != 1 || items[0].Action != "gig.moved_out" || items[0].EntityID == nil || *items[0].EntityID != gig ||
		items[0].ProjectID == nil || *items[0].ProjectID != src {
		t.Fatalf("source-only viewer's feed after the move: %+v", items)
	}
	b, _ := json.Marshal(items[0].Data)
	if strings.Contains(string(b), "new_key") || strings.Contains(string(b), "to_project") || strings.Contains(string(b), "SID-") {
		t.Fatalf("gig.moved_out tells of the target: %s", b)
	}
	// whoever sees both projects gets both entries
	var acts []string
	for _, it := range e.feed(fmt.Sprintf("/workspaces/scout/changes?after_id=%d", afterAll), e.key("scout")).Items {
		acts = append(acts, it.Action)
	}
	if strings.Join(acts, ",") != "gig.moved,gig.moved_out" {
		t.Fatalf("whole-workspace feed after the move: %v", acts)
	}
	// the gig's history names the move once
	code, out, _ = e.req("GET", fmt.Sprintf("/gigs/%d/activity", gig), "", e.key("scout"))
	e.ok(code, out, 200)
	if n := strings.Count(string(out), `"action":"gig.moved`); n != 1 {
		t.Fatalf("history shows %d move entries: %s", n, out)
	}
	// a move within the project (a status change) logs one entry, as before
	after = e.feed("/workspaces/scout/changes?after_id=latest", srcOnly).NextAfterID
	code, out, _ = e.req("POST", fmt.Sprintf("/gigs/%d/move", gig), `{"category":"done"}`, ada)
	e.ok(code, out, 200)
	if items := e.feed(fmt.Sprintf("/workspaces/scout/changes?after_id=%d", after), srcOnly).Items; len(items) != 0 {
		t.Fatalf("a move inside the other project reached the source viewer: %+v", items)
	}
}
