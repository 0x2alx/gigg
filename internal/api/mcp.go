package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0x2alx/gigg/internal/access"
	"github.com/0x2alx/gigg/internal/version"
)

// MCPHandler serves MCP (Streamable HTTP, stateless, JSON responses) for bearer keys. Every tool is
// a call to the REST API in-process with the caller's own credentials, so MCP can never do more
// than the key can do through the API, and parity follows from the API.
func (a *API) MCPHandler() http.Handler {
	h := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { return a.mcpServer(r) },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, DisableLocalhostProtection: true, MaxRequestBodyBytes: 1 << 20})
	return a.authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" && (a.cfg.PublicURL == nil || !a.originOK(r, o)) {
			writeProblem(w, fail(403, "forbidden", "Origin not allowed for MCP."))
			return
		}
		p := access.From(r.Context())
		if p == nil || p.Key == nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="gigg"`)
			writeProblem(w, fail(401, "unauthorized", "MCP needs an API key: Authorization: Bearer <key>."))
			return
		}
		h.ServeHTTP(w, r)
	}))
}

// apiCall runs one REST call in-process with the original request's credentials.
type apiCall func(method, path string, query url.Values, body any) (int, []byte)

func (a *API) caller(r *http.Request) apiCall {
	inner := a.Handler()
	return func(method, path string, query url.Values, body any) (code int, out []byte) {
		var rd io.Reader = http.NoBody
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		u := Prefix + path
		if len(query) > 0 {
			u += "?" + query.Encode()
		}
		req := httptest.NewRequestWithContext(r.Context(), method, u, rd)
		req.Host = r.Host
		req.RemoteAddr = r.RemoteAddr
		req.Header.Set("Authorization", r.Header.Get("Authorization"))
		for _, v := range r.Header.Values("X-Forwarded-For") {
			req.Header.Add("X-Forwarded-For", v)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		// MCP tools run outside the HTTP server's panic recovery: a panic here would end the process.
		defer func() {
			if v := recover(); v != nil {
				a.log.Error("panic in MCP tool call", "panic", fmt.Sprint(v), "path", path)
				code, out = 500, []byte(`{"status":500,"code":"internal","detail":"Internal error."}`)
			}
		}()
		inner.ServeHTTP(rec, req)
		return rec.Code, rec.Body.Bytes()
	}
}

func toolResult(code int, body []byte) (*mcp.CallToolResult, any, error) {
	text := strings.TrimSpace(string(body))
	if code == http.StatusNoContent {
		text = "Nothing (204)."
	}
	if code >= 400 {
		var p Problem
		if json.Unmarshal(body, &p) == nil && p.Code != "" {
			text = fmt.Sprintf("Error %d %s: %s", p.Status, p.Code, p.Detail)
			for _, e := range p.Errors {
				text += "\n- " + e.Location + " " + e.Message
			}
		}
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
}

func ro(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true}
}
func rw(title string, destructive bool) *mcp.ToolAnnotations {
	d, open := destructive, false
	return &mcp.ToolAnnotations{Title: title, DestructiveHint: &d, OpenWorldHint: &open}
}

// Tool inputs. Times are unix milliseconds; gigs are ids or keys like GIG-12.
type (
	wsArg struct {
		Workspace string `json:"workspace,omitempty" jsonschema:"Workspace id or slug; omit to use your key's workspace"`
	}
	listGigsArgs struct {
		Workspace   string `json:"workspace,omitempty" jsonschema:"Workspace id or slug; omit to use your key's workspace"`
		Project     int64  `json:"project,omitempty" jsonschema:"Project id"`
		Category    string `json:"category,omitempty" jsonschema:"backlog, todo, doing, waiting, done or cancelled"`
		Label       string `json:"label,omitempty" jsonschema:"Label name"`
		Q           string `json:"q,omitempty" jsonschema:"Text search in titles, descriptions and comments"`
		PriorityMin int64  `json:"priority_min,omitempty" jsonschema:"0-4"`
		Sort        string `json:"sort,omitempty" jsonschema:"board (default) or e.g. -priority,due"`
		Limit       int    `json:"limit,omitempty" jsonschema:"Default 50, max 500"`
		Cursor      string `json:"cursor,omitempty"`
		Archived    string `json:"archived,omitempty" jsonschema:"only or include (archived gigs are hidden by default)"`
		Parent      int64  `json:"parent,omitempty" jsonschema:"Only subgigs of this gig id"`
		Blocked     bool   `json:"blocked,omitempty" jsonschema:"Only gigs with an open blocker"`
	}
	gigArg struct {
		Gig string `json:"gig" jsonschema:"Gig id or key, e.g. GIG-12"`
	}
	getGigArgs struct {
		Gig      string `json:"gig" jsonschema:"Gig id or key, e.g. GIG-12"`
		Comments bool   `json:"comments,omitempty" jsonschema:"Include the comments"`
	}
	commentsArgs struct {
		Gig     string `json:"gig" jsonschema:"Gig id or key"`
		SinceID int64  `json:"since_id,omitempty" jsonschema:"Only comments after this id"`
	}
	createGigArgs struct {
		Project       int64          `json:"project" jsonschema:"Project id (see list_projects)"`
		Title         string         `json:"title"`
		DescriptionMD string         `json:"description_md,omitempty" jsonschema:"Markdown"`
		Category      string         `json:"category,omitempty" jsonschema:"Status category to start in (default: the project's default status)"`
		Priority      int64          `json:"priority,omitempty" jsonschema:"0 none, 1 low, 2 medium, 3 high, 4 urgent"`
		Labels        []string       `json:"labels,omitempty" jsonschema:"Label names (created if missing)"`
		Assignees     []string       `json:"assignees,omitempty" jsonschema:"Usernames (or user ids) to assign; they must be able to see the workspace"`
		Color         string         `json:"color,omitempty" jsonschema:"#RRGGBB"`
		StartAt       int64          `json:"start_at,omitempty" jsonschema:"Unix ms"`
		DueAt         int64          `json:"due_at,omitempty" jsonschema:"Unix ms"`
		NotAfter      int64          `json:"not_after,omitempty" jsonschema:"Unix ms: the window closes; not claimable after it, and marked missed if untouched"`
		Parent        int64          `json:"parent_id,omitempty" jsonschema:"Make it a subgig of this gig id"`
		BlocksParent  bool           `json:"blocks_parent,omitempty" jsonschema:"With parent_id: the parent waits (isn't handed out) until this subgig is done"`
		Checklist     []string       `json:"checklist,omitempty" jsonschema:"Checklist items"`
		Custom        map[string]any `json:"custom,omitempty" jsonschema:"Custom field values by field id"`
		Model         string         `json:"model,omitempty" jsonschema:"Model a runner should ask for: an alias such as opus or sonnet, or a full id (default: the agent's)"`
		Effort        string         `json:"effort,omitempty" jsonschema:"low, medium, high, xhigh or max (default: the agent's)"`
		AlwaysRun     bool           `json:"always_run,omitempty" jsonschema:"Run it whatever the usage budget (needs the always_run permission)"`
		AllowedTools  []string       `json:"allowed_tools,omitempty" jsonschema:"The only tools a runner should allow for it: Claude Code-style permission rules, one per entry (e.g. Read, Bash(git status *)); needs the allowed_tools permission"`
	}
	updateGigArgs struct {
		Gig           string   `json:"gig" jsonschema:"Gig id or key"`
		Title         string   `json:"title,omitempty"`
		DescriptionMD *string  `json:"description_md,omitempty" jsonschema:"Markdown; replaces the description"`
		Priority      *int64   `json:"priority,omitempty" jsonschema:"0-4"`
		Labels        []string `json:"labels,omitempty" jsonschema:"Replaces the labels"`
		Assignees     []string `json:"assignees,omitempty" jsonschema:"Usernames (or user ids); replaces the assignees (clear: assignees removes them all)"`
		Color         string   `json:"color,omitempty" jsonschema:"#RRGGBB"`
		StartAt       int64    `json:"start_at,omitempty" jsonschema:"Unix ms"`
		DueAt         int64    `json:"due_at,omitempty" jsonschema:"Unix ms"`
		NotAfter      int64    `json:"not_after,omitempty" jsonschema:"Unix ms: the window closes (reopening a missed gig clears missed)"`
		Model         *string  `json:"model,omitempty" jsonschema:"Model a runner should ask for; empty = the agent's default"`
		Effort        *string  `json:"effort,omitempty" jsonschema:"low, medium, high, xhigh, max; empty = the agent's default"`
		AlwaysRun     *bool    `json:"always_run,omitempty" jsonschema:"Run it whatever the usage budget (needs the always_run permission)"`
		AllowedTools  []string `json:"allowed_tools,omitempty" jsonschema:"Replaces the runner tool allowlist ([] clears; needs the allowed_tools permission)"`
		BlocksParent  *bool    `json:"blocks_parent,omitempty" jsonschema:"For a subgig: true = its parent waits until it is done"`
		AutoArchive   int64    `json:"auto_archive_minutes,omitempty" jsonschema:"Archive it this many minutes (5-527040) after it is done or cancelled"`
		Clear         []string `json:"clear,omitempty" jsonschema:"Fields to clear: color, start_at, due_at, not_after, labels, assignees, auto_archive_minutes"`
	}
	moveGigArgs struct {
		Gig      string `json:"gig" jsonschema:"Gig id or key"`
		Category string `json:"category,omitempty" jsonschema:"Target status category (first status of it)"`
		Status   int64  `json:"status,omitempty" jsonschema:"Target status id (alternative to category)"`
		Project  int64  `json:"project,omitempty" jsonschema:"Another project of the same workspace (the gig gets a new key; the old one keeps working)"`
		Position string `json:"position,omitempty" jsonschema:"top, bottom, before or after"`
		Relative int64  `json:"relative_to,omitempty" jsonschema:"Gig id for before/after"`
	}
	cloneGigArgs struct {
		Gig     string `json:"gig" jsonschema:"Gig id or key"`
		Project int64  `json:"project,omitempty" jsonschema:"Target project in the same workspace"`
		Title   string `json:"title,omitempty"`
	}
	addCommentArgs struct {
		Gig    string `json:"gig" jsonschema:"Gig id or key"`
		BodyMD string `json:"body_md" jsonschema:"Markdown"`
		Kind   string `json:"kind,omitempty" jsonschema:"comment (default), report or question"`
	}
	resumeArgs struct {
		Gig       string `json:"gig" jsonschema:"Gig id or key"`
		CommentMD string `json:"comment_md,omitempty" jsonschema:"Answer or instructions for the next run"`
	}
	listRecArgs struct {
		Workspace string `json:"workspace,omitempty" jsonschema:"Workspace id or slug; omit to use your key's workspace"`
		Project   int64  `json:"project,omitempty" jsonschema:"Only this project"`
	}
	recArg struct {
		ID int64 `json:"id" jsonschema:"Recurring gig id"`
	}
	recArgs struct {
		ID            int64       `json:"id,omitempty" jsonschema:"update_recurring: the recurring gig id"`
		Project       int64       `json:"project,omitempty" jsonschema:"create_recurring: project id"`
		Title         string      `json:"title,omitempty"`
		DescriptionMD *string     `json:"description_md,omitempty" jsonschema:"Markdown, copied into each gig"`
		Priority      *int64      `json:"priority,omitempty" jsonschema:"0-4"`
		Labels        []string    `json:"labels,omitempty"`
		Assignees     []int64     `json:"assignees,omitempty" jsonschema:"User ids"`
		Freq          string      `json:"freq,omitempty" jsonschema:"daily, weekly or monthly"`
		Interval      int64       `json:"interval,omitempty" jsonschema:"Every N days/weeks/months (default 1)"`
		Weekdays      []int       `json:"weekdays,omitempty" jsonschema:"Weekly: 1=Mon .. 7=Sun"`
		MonthDay      *int64      `json:"month_day,omitempty" jsonschema:"Monthly: 1-31, -1 = last day"`
		TZ            string      `json:"tz,omitempty" jsonschema:"IANA zone; default: your time zone preference, else the instance default"`
		WindowStart   string      `json:"window_start,omitempty" jsonschema:"Local HH:MM when the window opens (default 00:00)"`
		WindowMinutes int64       `json:"window_minutes,omitempty" jsonschema:"How long the window stays open, minutes (default 1440)"`
		LeadMinutes   *int64      `json:"lead_minutes,omitempty" jsonschema:"Create the gig this many minutes before the window opens"`
		EndsOn        *int64      `json:"ends_on,omitempty" jsonschema:"Unix ms; 0 clears"`
		MaxCount      *int64      `json:"max_count,omitempty" jsonschema:"Stop after this many gigs; 0 clears"`
		Overlap       string      `json:"overlap,omitempty" jsonschema:"skip (default: no new gig while the last is still open) or create"`
		Paused        *bool       `json:"paused,omitempty"`
		Model         *string     `json:"model,omitempty" jsonschema:"Model a runner should ask for; empty = the agent's default"`
		Effort        *string     `json:"effort,omitempty" jsonschema:"low, medium, high, xhigh, max; empty = the agent's default"`
		AlwaysRun     *bool       `json:"always_run,omitempty" jsonschema:"Its gigs run whatever the usage budget (needs the always_run permission)"`
		AllowedTools  []string    `json:"allowed_tools,omitempty" jsonschema:"Tool allowlist its gigs get for the runner (permission rules; [] clears; needs the allowed_tools permission)"`
		TemplateID    *int64      `json:"template_id,omitempty" jsonschema:"Make each gig from this template (see list_templates); content fields are then ignored. update_recurring: switches to it"`
		Checklist     []string    `json:"checklist,omitempty" jsonschema:"Checklist items each gig gets (stored in the template)"`
		Subgigs       []subgigArg `json:"subgigs,omitempty" jsonschema:"Subgigs each gig gets (stored in the template): title, and blocks_parent when the gig should wait for it; [] clears"`
		ScheduleID    *int64      `json:"schedule_id,omitempty" jsonschema:"Use this schedule (see list_schedules); timing fields are then ignored. update_recurring: switches to it"`
	}
	subgigArg struct {
		Title        string `json:"title"`
		BlocksParent bool   `json:"blocks_parent,omitempty" jsonschema:"The parent gig waits until this subgig is done"`
	}
	progressArgs struct {
		Gig  string `json:"gig" jsonschema:"Gig id or key (its running run gets the update)"`
		Pct  *int64 `json:"pct,omitempty" jsonschema:"0-100"`
		Note string `json:"note,omitempty" jsonschema:"Short progress note"`
	}
)

// mcpUserIDs turns usernames (or numeric ids) into user ids for a gig's assignees, through the API's
// user list (the caller's co-members, or everyone for admins: no more than the key can see). Whether
// each may be assigned (sees the gig's workspace) is the gig API's check, as for the UI.
func mcpUserIDs(call apiCall, names []string) ([]int64, *mcp.CallToolResult) {
	ids := make([]int64, 0, len(names))
	var known map[string]int64
	for _, n := range names {
		n = strings.TrimPrefix(strings.TrimSpace(n), "@")
		if id, err := strconv.ParseInt(n, 10, 64); err == nil && id > 0 {
			ids = append(ids, id)
			continue
		}
		if known == nil {
			code, body := call("GET", "/users", nil, nil)
			if code != http.StatusOK {
				res, _, _ := toolResult(code, body)
				return nil, res
			}
			var out struct {
				Items []struct {
					ID       int64  `json:"id"`
					Username string `json:"username"`
				} `json:"items"`
			}
			if err := json.Unmarshal(body, &out); err != nil {
				res, _, _ := toolResult(http.StatusInternalServerError, []byte(`{"status":500,"detail":"user list unreadable"}`))
				return nil, res
			}
			known = map[string]int64{}
			for _, u := range out.Items {
				known[strings.ToLower(u.Username)] = u.ID
			}
		}
		id, ok := known[strings.ToLower(n)]
		if !ok {
			res, _, _ := toolResult(422, []byte(fmt.Sprintf(`{"status":422,"code":"invalid","detail":%q}`, "No such user among your workspaces' members: "+n)))
			return nil, res
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// addToolIf registers a tool only when the caller could use it: an agent that may not create gigs
// isn't shown create_gig.
func addToolIf[In any](ok bool, s *mcp.Server, t *mcp.Tool, h mcp.ToolHandlerFor[In, any]) {
	if ok {
		mcp.AddTool(s, t, h)
	}
}

func (a *API) mcpServer(r *http.Request) *mcp.Server {
	p := access.From(r.Context())
	call := a.caller(r)
	s := mcp.NewServer(&mcp.Implementation{Name: "gigg", Title: "gigg gig board", Version: version.Version}, nil)
	ws := func(ref string) string {
		if ref == "" {
			return "current"
		}
		return url.PathEscape(ref)
	}
	t := func(ref string) string { return url.PathEscape(ref) }
	has := p.HasScope

	if has("workspace:read") {
		mcp.AddTool(s, &mcp.Tool{Name: "whoami", Description: "Your identity, workspaces, role and key scopes.", Annotations: ro("Who am I")},
			func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
				return toolResult(call("GET", "/auth/me", nil, nil))
			})
		mcp.AddTool(s, &mcp.Tool{Name: "list_projects", Description: "Projects (boards) of a workspace, with ids and gig key prefixes.", Annotations: ro("List projects")},
			func(ctx context.Context, _ *mcp.CallToolRequest, in wsArg) (*mcp.CallToolResult, any, error) {
				return toolResult(call("GET", "/workspaces/"+ws(in.Workspace)+"/projects", nil, nil))
			})
	}
	if has("gigs:read") {
		mcp.AddTool(s, &mcp.Tool{Name: "summary", Description: "Plain-text board summary: per project, each status with up to 5 gigs.", Annotations: ro("Board summary")},
			func(ctx context.Context, _ *mcp.CallToolRequest, in wsArg) (*mcp.CallToolResult, any, error) {
				return toolResult(call("GET", "/workspaces/"+ws(in.Workspace)+"/summary", nil, nil))
			})
		mcp.AddTool(s, &mcp.Tool{Name: "list_gigs", Description: "Find gigs; filters combine. Board order unless sort is given.", Annotations: ro("List gigs")},
			func(ctx context.Context, _ *mcp.CallToolRequest, in listGigsArgs) (*mcp.CallToolResult, any, error) {
				q := url.Values{}
				if in.Parent != 0 {
					q.Set("parent", strconv.FormatInt(in.Parent, 10))
				}
				if in.Blocked {
					q.Set("blocked", "true")
				}
				for k, v := range map[string]string{"category": in.Category, "label": in.Label, "q": in.Q, "sort": in.Sort, "cursor": in.Cursor, "archived": in.Archived} {
					if v != "" {
						q.Set(k, v)
					}
				}
				if in.Project != 0 {
					q.Set("project", strconv.FormatInt(in.Project, 10))
				}
				if in.PriorityMin != 0 {
					q.Set("priority_min", strconv.FormatInt(in.PriorityMin, 10))
				}
				if in.Limit != 0 {
					q.Set("limit", strconv.Itoa(in.Limit))
				}
				return toolResult(call("GET", "/workspaces/"+ws(in.Workspace)+"/gigs", q, nil))
			})
		mcp.AddTool(s, &mcp.Tool{Name: "get_gig", Description: "One gig by id or key, optionally with its comments.", Annotations: ro("Get gig")},
			func(ctx context.Context, _ *mcp.CallToolRequest, in getGigArgs) (*mcp.CallToolResult, any, error) {
				code, body := call("GET", "/gigs/"+t(in.Gig), nil, nil)
				if code != 200 || !in.Comments || !has("comments:read") {
					return toolResult(code, body)
				}
				cc, cb := call("GET", "/gigs/"+t(in.Gig)+"/comments", nil, nil)
				if cc != 200 {
					return toolResult(cc, cb)
				}
				return toolResult(200, []byte(`{"gig":`+string(body)+`,"comments":`+string(cb)+`}`))
			})
		mcp.AddTool(s, &mcp.Tool{Name: "list_runs", Description: "A gig's agent runs (sessions, status, cost), newest first.", Annotations: ro("List runs")},
			func(ctx context.Context, _ *mcp.CallToolRequest, in gigArg) (*mcp.CallToolResult, any, error) {
				return toolResult(call("GET", "/gigs/"+t(in.Gig)+"/runs", nil, nil))
			})
	}
	if has("comments:read") {
		mcp.AddTool(s, &mcp.Tool{Name: "list_comments", Description: "A gig's comments with author facts (kind, admin or not), oldest first.", Annotations: ro("List comments")},
			func(ctx context.Context, _ *mcp.CallToolRequest, in commentsArgs) (*mcp.CallToolResult, any, error) {
				q := url.Values{}
				if in.SinceID != 0 {
					q.Set("since_id", strconv.FormatInt(in.SinceID, 10))
				}
				return toolResult(call("GET", "/gigs/"+t(in.Gig)+"/comments", q, nil))
			})
	}
	if has("gigs:write") {
		addToolIf(p.AllowedAnywhere("create_gigs"), s, &mcp.Tool{Name: "create_gig", Description: "Create a gig in a project.", Annotations: rw("Create gig", false)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in createGigArgs) (*mcp.CallToolResult, any, error) {
				b := map[string]any{"title": in.Title}
				setIf(b, "description_md", in.DescriptionMD)
				setIf(b, "category", in.Category)
				setIf(b, "color", in.Color)
				if in.Priority != 0 {
					b["priority"] = in.Priority
				}
				if in.Labels != nil {
					b["labels"] = in.Labels
				}
				if in.StartAt != 0 {
					b["start_at"] = in.StartAt
				}
				if in.DueAt != 0 {
					b["due_at"] = in.DueAt
				}
				if in.NotAfter != 0 {
					b["not_after"] = in.NotAfter
				}
				if in.Parent != 0 {
					b["parent_id"] = in.Parent
				}
				if in.BlocksParent {
					b["blocks_parent"] = true
				}
				if len(in.Checklist) > 0 {
					b["checklist"] = in.Checklist
				}
				if in.Assignees != nil {
					ids, res := mcpUserIDs(call, in.Assignees)
					if res != nil {
						return res, nil, nil
					}
					b["assignees"] = ids
				}
				if len(in.Custom) > 0 {
					b["custom"] = in.Custom
				}
				setIf(b, "model", in.Model)
				setIf(b, "effort", in.Effort)
				if in.AlwaysRun {
					b["always_run"] = true
				}
				if len(in.AllowedTools) > 0 {
					b["allowed_tools"] = in.AllowedTools
				}
				return toolResult(call("POST", fmt.Sprintf("/projects/%d/gigs", in.Project), nil, b))
			})
		addToolIf(p.AllowedAnywhere("edit_gigs"), s, &mcp.Tool{Name: "update_gig", Description: "Change a gig's title, description, priority, labels, assignees, colour, dates, or how an agent runs it (model, effort, always_run, allowed_tools).", Annotations: rw("Update gig", false)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in updateGigArgs) (*mcp.CallToolResult, any, error) {
				b := map[string]any{}
				setIf(b, "title", in.Title)
				setIf(b, "color", in.Color)
				if in.DescriptionMD != nil {
					b["description_md"] = *in.DescriptionMD
				}
				if in.Priority != nil {
					b["priority"] = *in.Priority
				}
				if in.Labels != nil {
					b["labels"] = in.Labels
				}
				if in.Assignees != nil {
					ids, res := mcpUserIDs(call, in.Assignees)
					if res != nil {
						return res, nil, nil
					}
					b["assignees"] = ids
				}
				if in.StartAt != 0 {
					b["start_at"] = in.StartAt
				}
				if in.DueAt != 0 {
					b["due_at"] = in.DueAt
				}
				if in.NotAfter != 0 {
					b["not_after"] = in.NotAfter
				}
				for k, v := range map[string]*string{"model": in.Model, "effort": in.Effort} {
					if v != nil {
						b[k] = *v
					}
				}
				if in.AlwaysRun != nil {
					b["always_run"] = *in.AlwaysRun
				}
				if in.AllowedTools != nil {
					b["allowed_tools"] = in.AllowedTools
				}
				if in.BlocksParent != nil {
					b["blocks_parent"] = *in.BlocksParent
				}
				if in.AutoArchive != 0 {
					b["auto_archive_minutes"] = in.AutoArchive
				}
				for _, f := range in.Clear {
					switch f {
					case "color", "start_at", "due_at", "not_after", "labels", "assignees", "auto_archive_minutes":
						b[f] = nil
					default:
						return toolResult(422, []byte(`{"status":422,"code":"invalid","detail":"clear accepts color, start_at, due_at, not_after, labels, assignees, auto_archive_minutes"}`))
					}
				}
				return toolResult(call("PATCH", "/gigs/"+t(in.Gig), nil, b))
			})
		addToolIf(p.AllowedAnywhere("create_gigs"), s, &mcp.Tool{Name: "clone_gig", Description: "Copy a gig, optionally into another project of the workspace.", Annotations: rw("Clone gig", false)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in cloneGigArgs) (*mcp.CallToolResult, any, error) {
				b := map[string]any{}
				if in.Project != 0 {
					b["project"] = in.Project
				}
				setIf(b, "title", in.Title)
				return toolResult(call("POST", "/gigs/"+t(in.Gig)+"/clone", nil, b))
			})
	}
	if has("gigs:work") { // a consumer's actions: status moves, resume requests (Work on gigs)
		mcp.AddTool(s, &mcp.Tool{Name: "move_gig", Description: "Move a gig to another status (by category or id) or position. Moving it to another project of the workspace also needs Edit gigs and gigs:write.", Annotations: rw("Move gig", false)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in moveGigArgs) (*mcp.CallToolResult, any, error) {
				b := map[string]any{}
				setIf(b, "category", in.Category)
				setIf(b, "position", in.Position)
				if in.Status != 0 {
					b["status"] = in.Status
				}
				if in.Project != 0 {
					b["project"] = in.Project
				}
				if in.Relative != 0 {
					b["gig"] = in.Relative
				}
				return toolResult(call("POST", "/gigs/"+t(in.Gig)+"/move", nil, b))
			})
		mcp.AddTool(s, &mcp.Tool{Name: "resume_gig", Description: "Ask for the gig's agent session to be resumed by the runner (records the request; runs nothing).", Annotations: rw("Request resume", false)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in resumeArgs) (*mcp.CallToolResult, any, error) {
				return toolResult(call("POST", "/gigs/"+t(in.Gig)+"/resume", nil, map[string]any{"comment_md": in.CommentMD}))
			})
	}
	if has("gigs:delete") {
		mcp.AddTool(s, &mcp.Tool{Name: "delete_gig", Description: "Move a gig to the trash (restore_gig undoes it).", Annotations: rw("Delete gig", true)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in gigArg) (*mcp.CallToolResult, any, error) {
				return toolResult(call("DELETE", "/gigs/"+t(in.Gig), nil, nil))
			})
		mcp.AddTool(s, &mcp.Tool{Name: "restore_gig", Description: "Restore a gig from the trash.", Annotations: rw("Restore gig", false)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in gigArg) (*mcp.CallToolResult, any, error) {
				return toolResult(call("POST", "/gigs/"+t(in.Gig)+"/restore", nil, nil))
			})
	}
	if has("comments:write") {
		mcp.AddTool(s, &mcp.Tool{Name: "add_comment", Description: "Comment on a gig (Markdown).", Annotations: rw("Add comment", false)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in addCommentArgs) (*mcp.CallToolResult, any, error) {
				b := map[string]any{"body_md": in.BodyMD}
				setIf(b, "kind", in.Kind)
				return toolResult(call("POST", "/gigs/"+t(in.Gig)+"/comments", nil, b))
			})
	}
	if has("runs:write") && has("gigs:read") {
		mcp.AddTool(s, &mcp.Tool{Name: "report_progress", Description: "Report progress on the gig's running run (percent and/or a short note).", Annotations: rw("Report progress", false)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in progressArgs) (*mcp.CallToolResult, any, error) {
				code, body := call("GET", "/gigs/"+t(in.Gig)+"/runs", nil, nil)
				if code != 200 {
					return toolResult(code, body)
				}
				var runs struct {
					Items []struct {
						ID     int64  `json:"id"`
						Status string `json:"status"`
					} `json:"items"`
				}
				_ = json.Unmarshal(body, &runs)
				for _, r := range runs.Items {
					if r.Status == "running" {
						b := map[string]any{"note": in.Note}
						if in.Pct != nil {
							b["pct"] = *in.Pct
						}
						return toolResult(call("POST", fmt.Sprintf("/runs/%d/progress", r.ID), nil, b))
					}
				}
				return toolResult(409, []byte(`{"status":409,"code":"conflict","detail":"The gig has no running run."}`))
			})
	}
	if has("gigs:read") {
		mcp.AddTool(s, &mcp.Tool{Name: "list_recurring", Description: "Recurring gigs: each pairs a template (what the gig looks like) with a schedule (when) in a project and creates a gig per occurrence, inside its time window. With upcoming occurrences.", Annotations: ro("List recurring gigs")},
			func(ctx context.Context, _ *mcp.CallToolRequest, in listRecArgs) (*mcp.CallToolResult, any, error) {
				q := url.Values{}
				if in.Project != 0 {
					q.Set("project", fmt.Sprint(in.Project))
				}
				return toolResult(call("GET", "/workspaces/"+ws(in.Workspace)+"/recurrences", q, nil))
			})
	}
	if has("gigs:write") && p.AllowedAnywhere("schedules") {
		mcp.AddTool(s, &mcp.Tool{Name: "create_recurring", Description: "Create a recurring gig: a template (template_id, or the gig content given here, saved as a new template) on a schedule (schedule_id, or the timing given here; an identical schedule is reused, else one is made). Every day/week/month gigg creates a gig whose start is the window opening and whose due/not_after is the window closing. Untouched gigs are marked missed when the window closes; claim never hands one out outside its window.", Annotations: rw("Create recurring gig", false)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in recArgs) (*mcp.CallToolResult, any, error) {
				return toolResult(call("POST", fmt.Sprintf("/projects/%d/recurrences", in.Project), nil, in.merge(map[string]any{})))
			})
		mcp.AddTool(s, &mcp.Tool{Name: "update_recurring", Description: "Change a recurring gig; fields you omit stay as they are. template_id / schedule_id switch to another template / schedule; content fields change its template and timing fields its schedule in place (every recurring gig sharing them follows). The next occurrence is recomputed from now.", Annotations: rw("Update recurring gig", false)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in recArgs) (*mcp.CallToolResult, any, error) {
				code, body := call("GET", fmt.Sprintf("/recurrences/%d", in.ID), nil, nil)
				if code != 200 {
					return toolResult(code, body)
				}
				var cur map[string]any
				_ = json.Unmarshal(body, &cur)
				b := map[string]any{}
				keys := []string{"starts_on", "ends_on", "max_count", "overlap", "paused"}
				if in.content() && in.TemplateID == nil {
					keys = append(keys, "title", "description_md", "priority", "color", "labels", "assignees", "model", "effort", "always_run")
				} else {
					b["template_id"] = cur["template_id"]
				}
				if in.timing() && in.ScheduleID == nil {
					keys = append(keys, "freq", "interval", "weekdays", "month_day", "tz", "window_start", "window_minutes", "lead_minutes")
				} else {
					b["schedule_id"] = cur["schedule_id"]
				}
				for _, k := range keys {
					if v, ok := cur[k]; ok && v != nil {
						b[k] = v
					}
				}
				return toolResult(call("PUT", fmt.Sprintf("/recurrences/%d", in.ID), nil, in.merge(b)))
			})
		mcp.AddTool(s, &mcp.Tool{Name: "delete_recurring", Description: "Stop a recurring gig. Gigs it already created stay; its template and schedule too, unless also_template / also_schedule (refused while another recurring gig uses them).", Annotations: rw("Delete recurring gig", true)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
				ID           int64 `json:"id"`
				AlsoTemplate bool  `json:"also_template,omitempty" jsonschema:"also delete its template (when no other recurring gig uses it)"`
				AlsoSchedule bool  `json:"also_schedule,omitempty" jsonschema:"also delete its schedule (when no other recurring gig uses it)"`
			}) (*mcp.CallToolResult, any, error) {
				q := []string{}
				if in.AlsoTemplate {
					q = append(q, "template=true")
				}
				if in.AlsoSchedule {
					q = append(q, "schedule=true")
				}
				path := fmt.Sprintf("/recurrences/%d", in.ID)
				if len(q) > 0 {
					path += "?" + strings.Join(q, "&")
				}
				return toolResult(call("DELETE", path, nil, nil))
			})
	}
	addScheduleTools(s, p, call, ws)
	if has("gigs:read") {
		addChangesTool(s, call, ws)
	}
	addItemTools(s, p, call, ws)
	addRunnerTools(s, p, call)
	addErrandTools(s, p, call, ws)
	return s
}

// merge overlays the fields the caller gave onto b (a schedule body).
func (in recArgs) merge(b map[string]any) map[string]any {
	setIf(b, "title", in.Title)
	setIf(b, "freq", in.Freq)
	setIf(b, "tz", in.TZ)
	setIf(b, "window_start", in.WindowStart)
	setIf(b, "overlap", in.Overlap)
	if in.DescriptionMD != nil {
		b["description_md"] = *in.DescriptionMD
	}
	if in.Priority != nil {
		b["priority"] = *in.Priority
	}
	if in.Labels != nil {
		b["labels"] = in.Labels
	}
	if in.Assignees != nil {
		b["assignees"] = in.Assignees
	}
	if in.Interval != 0 {
		b["interval"] = in.Interval
	}
	if in.Weekdays != nil {
		b["weekdays"] = in.Weekdays
	}
	if in.MonthDay != nil {
		b["month_day"] = *in.MonthDay
	}
	if in.WindowMinutes != 0 {
		b["window_minutes"] = in.WindowMinutes
	}
	if in.LeadMinutes != nil {
		b["lead_minutes"] = *in.LeadMinutes
	}
	for k, v := range map[string]*string{"model": in.Model, "effort": in.Effort} {
		if v != nil {
			b[k] = *v
		}
	}
	if in.AlwaysRun != nil {
		b["always_run"] = *in.AlwaysRun
	}
	if in.AllowedTools != nil {
		b["allowed_tools"] = in.AllowedTools
	}
	for k, v := range map[string]*int64{"ends_on": in.EndsOn, "max_count": in.MaxCount} {
		if v != nil {
			if *v == 0 {
				delete(b, k)
			} else {
				b[k] = *v
			}
		}
	}
	if in.Paused != nil {
		b["paused"] = *in.Paused
	}
	if in.TemplateID != nil {
		b["template_id"] = *in.TemplateID
	}
	if in.Checklist != nil {
		b["checklist"] = in.Checklist
	}
	if in.Subgigs != nil {
		b["subgigs"] = in.Subgigs
	}
	if in.ScheduleID != nil {
		b["schedule_id"] = *in.ScheduleID
	}
	return b
}

// content: the call changes the gig content.
func (in recArgs) content() bool {
	return in.Title != "" || in.DescriptionMD != nil || in.Priority != nil || in.Labels != nil || in.Assignees != nil || in.Model != nil ||
		in.Effort != nil || in.AlwaysRun != nil || in.AllowedTools != nil || in.Checklist != nil || in.Subgigs != nil
}

// timing: the call changes the timing.
func (in recArgs) timing() bool {
	return in.Freq != "" || in.Interval != 0 || in.Weekdays != nil || in.MonthDay != nil || in.TZ != "" || in.WindowStart != "" ||
		in.WindowMinutes != 0 || in.LeadMinutes != nil
}

func setIf(m map[string]any, k, v string) {
	if v != "" {
		m[k] = v
	}
}
