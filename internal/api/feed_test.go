package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (e *env) feed(path string, c cred) ChangeFeed {
	e.t.Helper()
	code, out, _ := e.req("GET", path, "", c)
	e.ok(code, out, 200)
	var f ChangeFeed
	if err := json.Unmarshal(out, &f); err != nil {
		e.t.Fatal(err)
	}
	return f
}

// allChanges pages through the whole feed.
func (e *env) allChanges(ws string, c cred, limit int) []ActivityItem {
	e.t.Helper()
	var all []ActivityItem
	after := int64(0)
	for i := 0; i < 1000; i++ {
		f := e.feed(fmt.Sprintf("/workspaces/%s/changes?after_id=%d&limit=%d", ws, after, limit), c)
		if len(f.Items) > limit {
			e.t.Fatalf("page of %d > limit %d", len(f.Items), limit)
		}
		all = append(all, f.Items...)
		if len(f.Items) > 0 && f.NextAfterID != f.Items[len(f.Items)-1].ID {
			e.t.Fatalf("next_after_id %d, last item %d", f.NextAfterID, f.Items[len(f.Items)-1].ID)
		}
		if len(f.Items) == 0 && f.NextAfterID != after {
			e.t.Fatalf("empty page moved next_after_id to %d", f.NextAfterID)
		}
		after = f.NextAfterID
		if !f.More {
			return all
		}
	}
	e.t.Fatal("feed never ended")
	return nil
}

func TestFeedOrderingPagingAndParity(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 3; i++ {
		code, out, _ := e.req("POST", fmt.Sprintf("/gigs/%d/comments", e.ids["scout.gig"]), `{"body_md":"c`+strconv.Itoa(i)+`"}`, e.key("scout"))
		e.ok(code, out, 201)
	}
	all := e.allChanges("current", e.key("scout"), 2)
	if len(all) < 5 {
		t.Fatalf("only %d entries", len(all))
	}
	for i := 1; i < len(all); i++ {
		if all[i].ID <= all[i-1].ID {
			t.Fatalf("not ascending at %d: %d then %d", i, all[i-1].ID, all[i].ID)
		}
	}
	// exactly what workspace-activity shows, in the other order
	code, out, _ := e.req("GET", "/workspaces/current/activity?limit=500", "", e.key("scout"))
	e.ok(code, out, 200)
	var act struct{ Items []ActivityItem }
	_ = json.Unmarshal(out, &act)
	if len(act.Items) != len(all) {
		t.Fatalf("feed %d entries, activity %d", len(all), len(act.Items))
	}
	for i := range all {
		if all[i].ID != act.Items[len(act.Items)-1-i].ID {
			t.Fatalf("entry %d differs", i)
		}
	}
	for _, it := range all {
		if strings.HasPrefix(it.Action, "key.") || strings.HasPrefix(it.Action, "member.") {
			t.Fatalf("non-admin sees %s", it.Action)
		}
	}
	// the admin sees key and member entries of the same workspace
	adminAll := e.allChanges(strconv.FormatInt(e.ids["scout.ws"], 10), e.ada(), 500)
	sawKey := false
	for _, it := range adminAll {
		sawKey = sawKey || strings.HasPrefix(it.Action, "key.") || strings.HasPrefix(it.Action, "member.")
	}
	if !sawKey || len(adminAll) <= len(all) {
		t.Fatalf("admin feed: %d entries, key/member seen %v", len(adminAll), sawKey)
	}

	// latest: nothing, but the cursor to tail from now
	f := e.feed("/workspaces/current/changes?after_id=latest", e.key("scout"))
	if len(f.Items) != 0 || f.NextAfterID != all[len(all)-1].ID {
		t.Fatalf("latest: %+v (last %d)", f, all[len(all)-1].ID)
	}
	code, out, _ = e.req("POST", fmt.Sprintf("/gigs/%d/comments", e.ids["scout.gig"]), `{"body_md":"new"}`, e.key("scout"))
	e.ok(code, out, 201)
	f = e.feed(fmt.Sprintf("/workspaces/current/changes?after_id=%d", f.NextAfterID), e.key("scout"))
	if len(f.Items) != 1 || f.Items[0].Action != "comment.created" || f.More {
		t.Fatalf("after latest: %+v", f)
	}
	// bad after_id
	if code, _, _ := e.req("GET", "/workspaces/current/changes?after_id=abc", "", e.key("scout")); code != 422 {
		t.Fatalf("after_id=abc: %d", code)
	}
}

func TestFeedPermissions(t *testing.T) {
	e := newEnv(t)
	// another workspace: not found, by slug and by id
	for _, ws := range []string{"atlas", strconv.FormatInt(e.ids["atlas.ws"], 10)} {
		code, out, _ := e.req("GET", "/workspaces/"+ws+"/changes", "", e.key("scout"))
		if code != 404 || strings.Contains(string(out), "atlas secret") {
			t.Fatalf("atlas workspace via scout's key: %d %s", code, out)
		}
	}
	// a key without comments:read is refused
	body := fmt.Sprintf(`{"name":"nocomments","user_id":%d,"workspace":"scout","scopes":["workspace:read","gigs:read"]}`, e.ids["scout.user"])
	code, out, _ := e.req("POST", "/keys", body, e.ada())
	e.ok(code, out, 201)
	var kc KeyCreated
	_ = json.Unmarshal(out, &kc)
	if code, out, _ := e.req("GET", "/workspaces/current/changes", "", cred{token: kc.Token, listener: "main"}); code != 403 {
		t.Fatalf("without comments:read: %d %s", code, out)
	}
	// a project-narrowed key sees only its project
	body = fmt.Sprintf(`{"name":"narrow","user_id":%d,"workspace":"scout","project_ids":[%d],"scopes":["workspace:read","gigs:read","gigs:write","comments:read"]}`,
		e.ids["scout.user"], e.ids["scout.p2"])
	code, out, _ = e.req("POST", "/keys", body, e.ada())
	e.ok(code, out, 201)
	_ = json.Unmarshal(out, &kc)
	narrow := cred{token: kc.Token, listener: "main"}
	code, out, _ = e.req("POST", fmt.Sprintf("/projects/%d/gigs", e.ids["scout.p2"]), `{"title":"side gig"}`, e.ada())
	e.ok(code, out, 201)
	got := e.allChanges("current", narrow, 100)
	if len(got) == 0 {
		t.Fatal("narrowed key sees nothing")
	}
	for _, it := range got {
		if it.ProjectID == nil || *it.ProjectID != e.ids["scout.p2"] {
			t.Fatalf("narrowed key sees %s of project %v", it.Action, it.ProjectID)
		}
	}
	// redaction: the text of a deleted comment is gone for non-admins, kept for the admin
	code, out, _ = e.req("POST", fmt.Sprintf("/gigs/%d/comments", e.ids["scout.gig"]), `{"body_md":"soon gone"}`, e.key("scout"))
	e.ok(code, out, 201)
	var c struct{ ID int64 }
	_ = json.Unmarshal(out, &c)
	code, out, _ = e.req("DELETE", fmt.Sprintf("/comments/%d", c.ID), "", e.key("scout"))
	e.ok(code, out, 204)
	for _, who := range []struct {
		c    cred
		ws   string
		want bool
	}{{e.key("scout"), "current", false}, {e.ada(), "scout", true}} {
		code, out, _ := e.req("GET", "/workspaces/"+who.ws+"/changes?limit=500", "", who.c)
		e.ok(code, out, 200)
		if has := strings.Contains(string(out), "soon gone"); has != who.want {
			t.Fatalf("deleted text visible=%v for %s, want %v", has, who.ws, who.want)
		}
		if !strings.Contains(string(out), "comment.deleted") {
			t.Fatal("comment.deleted missing")
		}
	}
}

func TestFeedMCP(t *testing.T) {
	e := newEnv(t)
	srv := e.mcpServer("internal")
	defer srv.Close()
	s, err := e.mcpSession(t, srv, authTransport{token: e.keys["scout"]})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: "changes", Arguments: map[string]any{"limit": 1}})
	if err != nil || r.IsError {
		t.Fatalf("changes: %v %s", err, text(r))
	}
	var f ChangeFeed
	if err := json.Unmarshal([]byte(text(r)), &f); err != nil || len(f.Items) != 1 || !f.More {
		t.Fatalf("changes: %v %s", err, text(r))
	}
	r, _ = s.CallTool(context.Background(), &mcp.CallToolParams{Name: "changes", Arguments: map[string]any{"workspace": "atlas"}})
	if !r.IsError || strings.Contains(text(r), "atlas secret") {
		t.Fatalf("changes on atlas: %s", text(r))
	}
}

// ---------------------------------------------------------------- SSE

type sseEvent struct {
	id    int64
	event string
	data  string
}

// sseServer serves the API over real HTTP (cookie requests on the UI listener, keys on internal).
func (e *env) sseServer(t *testing.T) *httptest.Server {
	oldPoll := feedPoll
	feedPoll = 20 * time.Millisecond
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l := "internal"
		if r.Header.Get("Cookie") != "" {
			l = "main"
		}
		e.h.ServeHTTP(w, r.WithContext(WithInfo(r.Context(), RequestInfo{Listener: l, RequestID: "sse"})))
	}))
	t.Cleanup(func() { feedPoll = oldPoll })
	t.Cleanup(srv.Close)
	t.Cleanup(e.a.StopStreams) // runs first: open streams end so Close doesn't wait
	return srv
}

type sseConn struct {
	resp   *http.Response
	events chan sseEvent
	cancel context.CancelFunc
}

func (e *env) sseOpen(t *testing.T, srv *httptest.Server, path string, hdr map[string]string) (*sseConn, int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+Prefix+path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		cancel()
		return nil, resp.StatusCode
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	c := &sseConn{resp: resp, events: make(chan sseEvent, 100), cancel: cancel}
	go func() {
		defer close(c.events)
		sc := bufio.NewScanner(resp.Body)
		var ev sseEvent
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if ev.event != "" {
					c.events <- ev
				}
				ev = sseEvent{}
			case strings.HasPrefix(line, "id: "):
				ev.id, _ = strconv.ParseInt(line[4:], 10, 64)
			case strings.HasPrefix(line, "event: "):
				ev.event = line[7:]
			case strings.HasPrefix(line, "data: "):
				ev.data = line[6:]
			}
		}
	}()
	t.Cleanup(c.close)
	return c, 200
}

func (c *sseConn) close() { c.cancel(); c.resp.Body.Close() }

func (c *sseConn) next(t *testing.T) sseEvent {
	t.Helper()
	select {
	case ev, ok := <-c.events:
		if !ok {
			t.Fatal("stream ended")
		}
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("no event within 5 s")
	}
	return sseEvent{}
}

func bearer(tok string) map[string]string { return map[string]string{"Authorization": "Bearer " + tok} }

func TestEventsStreamAndResume(t *testing.T) {
	e := newEnv(t)
	srv := e.sseServer(t)
	comment := func(text string) {
		t.Helper()
		code, out, _ := e.req("POST", fmt.Sprintf("/gigs/%d/comments", e.ids["scout.gig"]), `{"body_md":"`+text+`"}`, e.key("scout"))
		e.ok(code, out, 201)
	}
	latest := e.feed("/workspaces/current/changes?after_id=latest", e.key("scout")).NextAfterID

	// default start is "now": nothing old is replayed
	c, _ := e.sseOpen(t, srv, "/workspaces/current/events", bearer(e.keys["scout"]))
	comment("live one")
	ev := c.next(t)
	var it ActivityItem
	if err := json.Unmarshal([]byte(ev.data), &it); err != nil {
		t.Fatal(err)
	}
	if ev.event != "activity" || ev.id <= latest || it.ID != ev.id || it.Action != "comment.created" {
		t.Fatalf("event %+v (latest before %d)", ev, latest)
	}
	c.close()

	// resume with Last-Event-ID: exactly what happened since
	comment("while away 1")
	comment("while away 2")
	h := bearer(e.keys["scout"])
	h["Last-Event-ID"] = strconv.FormatInt(ev.id, 10)
	c2, _ := e.sseOpen(t, srv, "/workspaces/current/events", h)
	a, b := c2.next(t), c2.next(t)
	if a.id <= ev.id || b.id <= a.id {
		t.Fatalf("resume ids %d, %d after %d", a.id, b.id, ev.id)
	}
	c2.close()

	// ?after_id=0 replays from the start, ascending
	c3, _ := e.sseOpen(t, srv, "/workspaces/current/events?after_id=0", bearer(e.keys["scout"]))
	first := c3.next(t)
	second := c3.next(t)
	if first.id <= 0 || second.id <= first.id {
		t.Fatalf("replay ids %d, %d", first.id, second.id)
	}
	c3.close()

	// a browser session works too (GET, so no CSRF)
	c4, code := e.sseOpen(t, srv, "/workspaces/scout/events", map[string]string{"Cookie": "__Host-gigg_session=" + e.cookie})
	if code != 200 {
		t.Fatalf("session stream: %d", code)
	}
	comment("for the browser")
	if ev := c4.next(t); !strings.Contains(ev.data, "comment.created") {
		t.Fatalf("session event %+v", ev)
	}
	// shutdown ends open streams
	e.a.StopStreams()
	deadline := time.After(5 * time.Second)
	for open := true; open; {
		select {
		case _, open = <-c4.events:
		case <-deadline:
			t.Fatal("stream still open after StopStreams")
		}
	}
}

func TestEventsPermissionsAndCap(t *testing.T) {
	e := newEnv(t)
	srv := e.sseServer(t)
	for _, ws := range []string{"atlas", strconv.FormatInt(e.ids["atlas.ws"], 10)} {
		if _, code := e.sseOpen(t, srv, "/workspaces/"+ws+"/events", bearer(e.keys["scout"])); code != 404 {
			t.Fatalf("atlas events via scout's key: %d", code)
		}
	}
	if _, code := e.sseOpen(t, srv, "/workspaces/scout/events", nil); code != 401 {
		t.Fatalf("anonymous: %d", code)
	}
	if _, code := e.sseOpen(t, srv, "/workspaces/current/events?after_id=x", bearer(e.keys["scout"])); code != 422 {
		t.Fatalf("bad after_id: %d", code)
	}
	// scout's stream never carries atlas's activity
	c, _ := e.sseOpen(t, srv, "/workspaces/current/events?after_id=0", bearer(e.keys["scout"]))
	code, out, _ := e.req("POST", fmt.Sprintf("/gigs/%d/comments", e.ids["atlas.gig"]), `{"body_md":"atlas secret again"}`, e.key("atlas"))
	e.ok(code, out, 201)
	code, out, _ = e.req("POST", fmt.Sprintf("/gigs/%d/comments", e.ids["scout.gig"]), `{"body_md":"marker"}`, e.key("scout"))
	e.ok(code, out, 201)
	var markerID int64
	_ = json.Unmarshal(out, &struct{ ID *int64 }{&markerID})
	for {
		ev := c.next(t)
		var it ActivityItem
		_ = json.Unmarshal([]byte(ev.data), &it)
		if it.ProjectID != nil && (*it.ProjectID == e.ids["atlas.p"] || *it.ProjectID == e.ids["atlas.p2"]) {
			t.Fatalf("atlas activity in scout's stream: %s", ev.data)
		}
		if it.Action == "comment.created" && it.EntityID != nil && *it.EntityID == markerID {
			break
		}
	}
	// per-principal cap
	old := feedMaxStream
	feedMaxStream = 2
	defer func() { feedMaxStream = old }()
	e.sseOpen(t, srv, "/workspaces/current/events", bearer(e.keys["scout"]))
	if _, code := e.sseOpen(t, srv, "/workspaces/current/events", bearer(e.keys["scout"])); code != 429 {
		t.Fatalf("third stream: %d, want 429", code)
	}
	if _, code := e.sseOpen(t, srv, "/workspaces/current/events", bearer(e.keys["atlas"])); code != 200 {
		t.Fatalf("atlas's own stream: %d", code)
	}
}

// TestEventsHTTP2DeadlineCleared: over HTTP/2 a write deadline left armed after a write resets the
// stream when it expires (Chrome: ERR_HTTP2_PROTOCOL_ERROR). With pings slower than the deadline the
// stream must survive several pings and end cleanly at its lifetime.
func TestEventsHTTP2DeadlineCleared(t *testing.T) {
	e := newEnv(t)
	oldPing, oldWrite, oldLife := feedPing, feedWrite, feedLifetime
	feedPing, feedWrite, feedLifetime = 150*time.Millisecond, 50*time.Millisecond, 800*time.Millisecond
	t.Cleanup(func() { feedPing, feedWrite, feedLifetime = oldPing, oldWrite, oldLife })
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.h.ServeHTTP(w, r.WithContext(WithInfo(r.Context(), RequestInfo{Listener: "internal", RequestID: "h2"})))
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	t.Cleanup(e.a.StopStreams)

	req, _ := http.NewRequest("GET", srv.URL+Prefix+"/workspaces/current/events", nil)
	for k, v := range bearer(e.keys["scout"]) {
		req.Header.Set(k, v)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.ProtoMajor != 2 || resp.StatusCode != 200 {
		t.Fatalf("proto %s, status %d", resp.Proto, resp.StatusCode)
	}
	pings := 0
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if sc.Text() == ": ping" {
			pings++
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("stream broke after %d pings: %v", pings, err)
	}
	if pings < 3 {
		t.Fatalf("only %d pings before the stream ended", pings)
	}
}

// The change feed names the request behind an entry only for the viewer's own entries, so a page
// can skip the echoes of its own writes without learning anything about anyone else's.
func TestFeedOwnRequestID(t *testing.T) {
	e := newEnv(t)
	path := fmt.Sprintf("/gigs/%d/comments", e.ids["scout.gig"])
	code, out, _ := e.req("POST", path, `{"body_md":"mine"}`, e.ada())
	e.ok(code, out, 201)
	// another principal writes through the API too, so its entry has a request id to leak
	code, out, _ = e.req("POST", path, `{"body_md":"theirs"}`, e.key("scout"))
	e.ok(code, out, 201)
	own, others := 0, 0
	for _, it := range e.feed("/workspaces/scout/changes?after_id=0&limit=500", e.ada()).Items {
		if it.ActorID != nil && *it.ActorID == e.ids["ada"] {
			// the fixtures were written straight to the store, without a request
			if it.RequestID == "test" {
				own++
			}
			continue
		}
		if it.Action == "comment.created" {
			others++
		}
		if it.RequestID != "" {
			t.Fatalf("someone else's entry %d (%s) shows request id %q", it.ID, it.Action, it.RequestID)
		}
	}
	if own == 0 {
		t.Fatal("the comment ada just wrote carries no request id")
	}
	if others == 0 {
		t.Fatal("the scout key's comment isn't in ada's feed: the leak check checked nothing")
	}
	seen := 0
	for _, it := range e.feed("/workspaces/current/changes?after_id=0&limit=500", e.key("scout")).Items {
		if it.ActorID != nil && *it.ActorID == e.ids["ada"] {
			seen++
			if it.RequestID != "" {
				t.Fatalf("ada's entry %d leaks its request id to the scout key", it.ID)
			}
		}
	}
	if seen == 0 {
		t.Fatal("none of ada's entries is in the scout key's feed: the leak check checked nothing")
	}
}
