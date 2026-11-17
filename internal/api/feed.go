package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0x2alx/gigg/internal/access"
)

// The change feed (GET /workspaces/{ws}/changes) and live updates (GET /workspaces/{ws}/events,
// Server-Sent Events) read the activity log in ascending id order, with exactly the visibility of
// workspace-activity: role viewer, scopes workspace:read + gigs:read + comments:read, no key/member
// actions for non-admins, project-narrowed keys see only their projects, deleted comment text redacted.

// Tunables (tests shorten them).
var (
	feedPoll     = time.Second      // how often a stream looks for new rows
	feedPing     = 25 * time.Second // keep-alive comment
	feedLifetime = 2 * time.Minute  // a stream ends then; the client reconnects with Last-Event-ID (re-authenticated,
	// so a revoked key, disabled user or removed member stops receiving within this time)
	feedWrite     = 15 * time.Second // per-write deadline: a reader that stops reading doesn't pin the stream
	feedMaxStream = 24               // concurrent streams per user (a tab opens one per workspace, up to 8)
)

type changesIn struct {
	WS      string `path:"ws" doc:"Workspace id, slug, or current"`
	AfterID string `query:"after_id" doc:"Return entries with a larger id (default 0). \"latest\" returns no items and next_after_id = the newest id, to start tailing from now."`
	Limit   int    `query:"limit" minimum:"0" maximum:"500" doc:"Default 100, max 500"`
}

// ChangeFeed is a page of the change feed.
type ChangeFeed struct {
	Items       []ActivityItem `json:"items" doc:"Oldest first"`
	NextAfterID int64          `json:"next_after_id" doc:"Pass as after_id for the next page (the last item's id, or after_id when empty)"`
	More        bool           `json:"more" doc:"More entries are already waiting"`
}

func (a *API) registerFeed() {
	huma.Register(a.h, huma.Operation{OperationID: "workspace-changes", Method: "GET", Path: "/workspaces/{ws}/changes", Tags: []string{"workspaces"},
		Summary:     "Change feed: the workspace's activity after an id, oldest first",
		Description: "Poll with after_id = the previous next_after_id. Same visibility as workspace-activity. For push, GET /workspaces/{ws}/events (text/event-stream) streams the same entries."},
		func(ctx context.Context, in *changesIn) (*struct{ Body *ChangeFeed }, error) {
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			where, args, err := a.feedScope(ctx, p, in.WS)
			if err != nil {
				return nil, err
			}
			limit := in.Limit
			if limit <= 0 || limit > 500 {
				limit = 100
			}
			out := &ChangeFeed{Items: []ActivityItem{}}
			if in.AfterID == "latest" {
				out.NextAfterID, err = a.feedLatest(ctx, where, args)
				return &struct{ Body *ChangeFeed }{out}, storeErr(err)
			}
			after, err := parseAfter(in.AfterID)
			if err != nil {
				return nil, err
			}
			items, err := a.activityAfter(ctx, where, args, after, limit+1, p.User.ID)
			if err != nil {
				return nil, err
			}
			if len(items) > limit {
				items, out.More = items[:limit], true
			}
			if !p.IsInstanceAdmin() {
				redactDeleted(items)
			}
			out.Items, out.NextAfterID = items, after
			if len(items) > 0 {
				out.NextAfterID = items[len(items)-1].ID
			}
			return &struct{ Body *ChangeFeed }{out}, nil
		})
	a.mux.HandleFunc("GET /workspaces/{ws}/events", a.events)
}

func parseAfter(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v < 0 {
		return 0, fail(422, "invalid", `after_id must be a non-negative id or "latest".`)
	}
	return v, nil
}

// feedScope checks the caller may read the workspace's activity and returns the WHERE clause
// (built from constants) that limits rows to what it may see. Same rules as workspace-activity.
func (a *API) feedScope(ctx context.Context, p *access.Principal, ref string) (string, []any, error) {
	w, err := a.workspace(ctx, p, ref)
	if err != nil {
		return "", nil, err
	}
	if err := require(p, w.ID, access.Viewer, "workspace:read"); err != nil {
		return "", nil, err
	}
	if err := need(p, "gigs:read"); err != nil {
		return "", nil, err
	}
	if err := need(p, "comments:read"); err != nil {
		return "", nil, err
	}
	where, args := "workspace_id = ?", []any{w.ID}
	if !p.IsInstanceAdmin() {
		where += " AND action NOT LIKE 'key.%' AND action NOT LIKE 'member.%' AND action NOT LIKE '%.access'"
	}
	if pf := p.ProjectFilter(); pf != nil {
		where += " AND project_id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(pf)), ",") + ")"
		for _, x := range pf {
			args = append(args, x)
		}
	}
	return where, args, nil
}

func (a *API) feedLatest(ctx context.Context, where string, args []any) (int64, error) {
	var id sql.NullInt64
	err := a.st.R.QueryRowContext(ctx, `SELECT MAX(id) FROM activity WHERE `+where, args...).Scan(&id)
	return id.Int64, err
}

// activityAfter reads the log oldest first, after an id. Entries the viewer caused carry their
// request id, so a page can tell its own changes (already applied) from everyone else's.
func (a *API) activityAfter(ctx context.Context, where string, args []any, afterID int64, limit int, viewer int64) ([]ActivityItem, error) {
	args = append(append([]any{}, args...), afterID)
	rows, err := a.st.R.QueryContext(ctx, `SELECT id, at, actor_user_id, key_id, listener, action, entity_type, entity_id, project_id, data, request_id
		FROM activity WHERE (`+where+`) AND id > ? ORDER BY id ASC LIMIT `+strconv.Itoa(limit), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ActivityItem{}
	for rows.Next() {
		var it ActivityItem
		var actor, key, ent, proj sql.NullInt64
		var data, reqID string
		if err := rows.Scan(&it.ID, &it.At, &actor, &key, &it.Listener, &it.Action, &it.EntityType, &ent, &proj, &data, &reqID); err != nil {
			return nil, err
		}
		it.ActorID, it.KeyID, it.EntityID, it.ProjectID = nullPtr(actor), nullPtr(key), nullPtr(ent), nullPtr(proj)
		if actor.Valid && actor.Int64 == viewer {
			it.RequestID = reqID
		}
		_ = json.Unmarshal([]byte(data), &it.Data)
		out = append(out, it)
	}
	return out, rows.Err()
}

func nullPtr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

// ---------------------------------------------------------------- Server-Sent Events

// streamHub counts open streams per principal and ends them all on shutdown.
type streamHub struct {
	mu   sync.Mutex
	open map[string]int
	done chan struct{}
	once sync.Once
}

var hubs sync.Map // *API -> *streamHub

func (a *API) hub() *streamHub {
	h, _ := hubs.LoadOrStore(a, &streamHub{open: map[string]int{}, done: make(chan struct{})})
	return h.(*streamHub)
}

// StopStreams ends every open event stream (call it when the server shuts down).
func (a *API) StopStreams() {
	h := a.hub()
	h.once.Do(func() { close(h.done) })
}

func (h *streamHub) acquire(who string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.open[who] >= feedMaxStream {
		return false
	}
	h.open[who]++
	return true
}

func (h *streamHub) release(who string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.open[who]--; h.open[who] <= 0 {
		delete(h.open, who)
	}
}

// principalID is "k<key id>" for keys, "u<user id>" for sessions.
func principalID(p *access.Principal) string {
	if p.Key != nil {
		return "k" + strconv.FormatInt(p.Key.ID, 10)
	}
	return "u" + strconv.FormatInt(p.User.ID, 10)
}

// events streams the workspace's activity as text/event-stream: one `event: activity` per entry
// (id = activity id, data = the ActivityItem JSON). Starts after Last-Event-ID, else ?after_id=
// (an id or "latest"), else from now. A `: ping` comment every 25 s; the stream ends after feedLifetime (clients reconnect with Last-Event-ID).
func (a *API) events(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p := access.From(ctx)
	if p == nil {
		writeProblem(w, fail(401, "unauthorized", "Log in or send an API key."))
		return
	}
	where, args, err := a.feedScope(ctx, p, r.PathValue("ws"))
	if err != nil {
		writeProblem(w, problemOf(err))
		return
	}
	var after int64
	start := r.Header.Get("Last-Event-ID")
	if start == "" {
		start = r.URL.Query().Get("after_id")
	}
	if start == "" || start == "latest" {
		after, err = a.feedLatest(ctx, where, args)
	} else {
		after, err = parseAfter(start)
	}
	if err != nil {
		writeProblem(w, problemOf(err))
		return
	}
	hub, who := a.hub(), "u"+strconv.FormatInt(p.User.ID, 10) // the stream cap is per user, whatever the credential
	if !hub.acquire(who) {
		writeProblem(w, fail(429, "too_many_streams", fmt.Sprintf("At most %d open event streams per user.", feedMaxStream)))
		return
	}
	defer hub.release(who)

	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{}) // no write timeout for this response
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, "retry: 3000\n: ok\n\n"); err != nil || rc.Flush() != nil {
		return
	}
	poll, ping, life := time.NewTicker(feedPoll), time.NewTicker(feedPing), time.NewTimer(feedLifetime)
	defer poll.Stop()
	defer ping.Stop()
	defer life.Stop()
	admin := p.IsInstanceAdmin()
	// write sends one batch under a deadline (a reader that stops reading doesn't pin the stream) and
	// clears it again: over HTTP/2 a deadline left armed resets the stream when it expires, even with
	// nothing pending, and Chrome reports ERR_HTTP2_PROTOCOL_ERROR between pings.
	write := func(f func() error) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(feedWrite))
		ok := f() == nil && rc.Flush() == nil
		_ = rc.SetWriteDeadline(time.Time{})
		return ok
	}
	// send writes every new visible entry; false ends the stream.
	send := func() bool {
		for {
			items, err := a.activityAfter(ctx, where, args, after, 200, p.User.ID)
			if err != nil {
				if ctx.Err() == nil {
					a.log.Error("events", "err", err)
				}
				return false
			}
			if !admin {
				redactDeleted(items)
			}
			if len(items) > 0 && !write(func() error {
				for _, it := range items {
					b, _ := json.Marshal(it)
					if _, err := fmt.Fprintf(w, "id: %d\nevent: activity\ndata: %s\n\n", it.ID, b); err != nil {
						return err
					}
					after = it.ID
				}
				return nil
			}) {
				return false
			}
			if len(items) < 200 {
				return true
			}
		}
	}
	if !send() {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-hub.done:
			return
		case <-life.C:
			return
		case <-ping.C:
			if !write(func() error { _, err := fmt.Fprint(w, ": ping\n\n"); return err }) {
				return
			}
		case <-poll.C:
			if !send() {
				return
			}
		}
	}
}

// problemOf turns a handler error into a problem (500 for anything unexpected).
func problemOf(err error) *Problem {
	if p, ok := storeErr(err).(*Problem); ok {
		return p
	}
	return fail(500, "internal", "Internal error.")
}

// ---------------------------------------------------------------- MCP

type changesArgs struct {
	Workspace string `json:"workspace,omitempty" jsonschema:"Workspace id or slug; omit to use your key's workspace"`
	AfterID   string `json:"after_id,omitempty" jsonschema:"Entries after this activity id (default 0); \"latest\" returns next_after_id to start tailing from now"`
	Limit     int    `json:"limit,omitempty" jsonschema:"Default 100, max 500"`
}

// addChangesTool registers the `changes` MCP tool (the caller checks gigs:read).
func addChangesTool(s *mcp.Server, call apiCall, ws func(string) string) {
	mcp.AddTool(s, &mcp.Tool{Name: "changes", Description: "What changed in a workspace since an activity id, oldest first. Pass next_after_id back as after_id to keep up; after_id \"latest\" starts from now.", Annotations: ro("Changes")},
		func(ctx context.Context, _ *mcp.CallToolRequest, in changesArgs) (*mcp.CallToolResult, any, error) {
			q := url.Values{}
			if in.AfterID != "" {
				q.Set("after_id", in.AfterID)
			}
			if in.Limit != 0 {
				q.Set("limit", strconv.Itoa(in.Limit))
			}
			return toolResult(call("GET", "/workspaces/"+ws(in.Workspace)+"/changes", q, nil))
		})
}
