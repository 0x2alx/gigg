package api

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// TestFeedSeesNoProject: someone who views a workspace while a project rule hides every one of its
// projects reads an empty activity list, change feed and event stream, not an error. The project
// filter is then an empty list, and an empty IN list is a syntax error on PostgreSQL (SQLite
// takes it), so this test matters most when run against PostgreSQL (dev/check-pg.sh).
func TestFeedSeesNoProject(t *testing.T) {
	e := newEnv(t)
	ada, scout, ws := e.ada(), e.ids["scout.user"], e.ids["scout.ws"]
	for _, p := range []int64{e.ids["scout.p"], e.ids["scout.p2"]} {
		code, out, _ := e.req("PUT", fmt.Sprintf("/users/%d/access", scout), fmt.Sprintf(`{"workspace_id":%d,"project_id":%d,"overrides":{"view":false}}`, ws, p), ada)
		e.ok(code, out, 200)
	}
	c := e.key("scout")

	code, out, _ := e.req("GET", "/workspaces/scout/activity", "", c)
	e.ok(code, out, 200)
	var act struct{ Items []ActivityItem }
	if err := json.Unmarshal(out, &act); err != nil {
		t.Fatal(err)
	}
	for _, it := range act.Items {
		if it.ProjectID != nil {
			t.Fatalf("activity shows a hidden project's entry: %+v", it)
		}
	}
	for _, path := range []string{"/workspaces/scout/changes", "/workspaces/scout/changes?after_id=latest"} {
		for _, it := range e.feed(path, c).Items {
			if it.ProjectID != nil {
				t.Fatalf("%s shows a hidden project's entry: %+v", path, it)
			}
		}
	}

	srv := e.sseServer(t)
	s, code := e.sseOpen(t, srv, "/workspaces/scout/events", bearer(e.keys["scout"]))
	if code != 200 {
		t.Fatalf("event stream: %d", code)
	}
	defer s.cancel()
	// a comment in a hidden project: the stream stays open and carries nothing of it
	code, out, _ = e.req("POST", fmt.Sprintf("/gigs/%d/comments", e.ids["scout.gig"]), `{"body_md":"hidden"}`, ada)
	e.ok(code, out, 201)
	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case ev, open := <-s.events:
			if !open {
				t.Fatal("event stream ended (a failed poll?)")
			}
			if ev.event == "activity" {
				t.Fatalf("hidden project's activity on the stream: %s", ev.data)
			}
		case <-deadline:
			return
		}
	}
}
