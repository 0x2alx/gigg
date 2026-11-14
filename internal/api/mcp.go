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
		if o := r.Header.Get("Origin"); o != "" && (a.cfg.PublicURL == nil || !strings.EqualFold(o, a.cfg.PublicURL.Scheme+"://"+a.cfg.PublicURL.Host)) {
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

// Tool inputs. Times are unix milliseconds; tasks are ids or keys like GIG-12.
type (
	wsArg struct {
		Workspace string `json:"workspace,omitempty" jsonschema:"Workspace id or slug; omit to use your key's workspace"`
	}
	listTasksArgs struct {
		Workspace   string `json:"workspace,omitempty" jsonschema:"Workspace id or slug; omit to use your key's workspace"`
		Project     int64  `json:"project,omitempty" jsonschema:"Project id"`
		Category    string `json:"category,omitempty" jsonschema:"backlog, todo, doing, waiting, done or cancelled"`
		Label       string `json:"label,omitempty" jsonschema:"Label name"`
		Q           string `json:"q,omitempty" jsonschema:"Text search in titles, descriptions and comments"`
		PriorityMin int64  `json:"priority_min,omitempty" jsonschema:"0-4"`
		Sort        string `json:"sort,omitempty" jsonschema:"board (default) or e.g. -priority,due"`
		Limit       int    `json:"limit,omitempty" jsonschema:"Default 50, max 500"`
		Cursor      string `json:"cursor,omitempty"`
		Archived    string `json:"archived,omitempty" jsonschema:"only or include (archived tasks are hidden by default)"`
		Parent      int64  `json:"parent,omitempty" jsonschema:"Only subtasks of this task id"`
		Blocked     bool   `json:"blocked,omitempty" jsonschema:"Only tasks with an open blocker"`
	}
	taskArg struct {
		Task string `json:"task" jsonschema:"Task id or key, e.g. GIG-12"`
	}
	getTaskArgs struct {
		Task     string `json:"task" jsonschema:"Task id or key, e.g. GIG-12"`
		Comments bool   `json:"comments,omitempty" jsonschema:"Include the comments"`
	}
	commentsArgs struct {
		Task    string `json:"task" jsonschema:"Task id or key"`
		SinceID int64  `json:"since_id,omitempty" jsonschema:"Only comments after this id"`
	}
	createTaskArgs struct {
		Project       int64          `json:"project" jsonschema:"Project id (see list_projects)"`
		Title         string         `json:"title"`
		DescriptionMD string         `json:"description_md,omitempty" jsonschema:"Markdown"`
		Category      string         `json:"category,omitempty" jsonschema:"Status category to start in (default: the project's default status)"`
		Priority      int64          `json:"priority,omitempty" jsonschema:"0 none, 1 low, 2 medium, 3 high, 4 urgent"`
		Labels        []string       `json:"labels,omitempty" jsonschema:"Label names (created if missing)"`
		Color         string         `json:"color,omitempty" jsonschema:"#RRGGBB"`
		StartAt       int64          `json:"start_at,omitempty" jsonschema:"Unix ms"`
		DueAt         int64          `json:"due_at,omitempty" jsonschema:"Unix ms"`
		NotAfter      int64          `json:"not_after,omitempty" jsonschema:"Unix ms: the window closes; not claimable after it, and marked missed if untouched"`
		Parent        int64          `json:"parent_id,omitempty" jsonschema:"Make it a subtask of this task id"`
		Checklist     []string       `json:"checklist,omitempty" jsonschema:"Checklist items"`
		Custom        map[string]any `json:"custom,omitempty" jsonschema:"Custom field values by field id"`
		Model         string         `json:"model,omitempty" jsonschema:"Model for the agent's claude -p: opus, sonnet, haiku, fable or a full id (default: the agent's)"`
		Effort        string         `json:"effort,omitempty" jsonschema:"low, medium, high, xhigh or max (default: the agent's)"`
		AlwaysRun     bool           `json:"always_run,omitempty" jsonschema:"Run it whatever the usage budget (needs the always_run permission)"`
		AllowedTools  []string       `json:"allowed_tools,omitempty" jsonschema:"Tools the runner may use for it: Claude Code permission rules, one per entry (e.g. Read, Bash(git status *)); needs the allowed_tools permission"`
	}
	updateTaskArgs struct {
		Task          string   `json:"task" jsonschema:"Task id or key"`
		Title         string   `json:"title,omitempty"`
		DescriptionMD *string  `json:"description_md,omitempty" jsonschema:"Markdown; replaces the description"`
		Priority      *int64   `json:"priority,omitempty" jsonschema:"0-4"`
		Labels        []string `json:"labels,omitempty" jsonschema:"Replaces the labels"`
		Color         string   `json:"color,omitempty" jsonschema:"#RRGGBB"`
		StartAt       int64    `json:"start_at,omitempty" jsonschema:"Unix ms"`
		DueAt         int64    `json:"due_at,omitempty" jsonschema:"Unix ms"`
		NotAfter      int64    `json:"not_after,omitempty" jsonschema:"Unix ms: the window closes (reopening a missed task clears missed)"`
		Model         *string  `json:"model,omitempty" jsonschema:"Model for claude -p; empty = the agent's default"`
		Effort        *string  `json:"effort,omitempty" jsonschema:"low, medium, high, xhigh, max; empty = the agent's default"`
		AlwaysRun     *bool    `json:"always_run,omitempty" jsonschema:"Run it whatever the usage budget (needs the always_run permission)"`
		AllowedTools  []string `json:"allowed_tools,omitempty" jsonschema:"Replaces the runner tool allowlist ([] clears; needs the allowed_tools permission)"`
		Clear         []string `json:"clear,omitempty" jsonschema:"Fields to clear: color, start_at, due_at, not_after, labels"`
	}
	moveTaskArgs struct {
		Task     string `json:"task" jsonschema:"Task id or key"`
		Category string `json:"category,omitempty" jsonschema:"Target status category (first status of it)"`
		Status   int64  `json:"status,omitempty" jsonschema:"Target status id (alternative to category)"`
		Project  int64  `json:"project,omitempty" jsonschema:"Another project of the same workspace (the task gets a new key; the old one keeps working)"`
		Position string `json:"position,omitempty" jsonschema:"top, bottom, before or after"`
		Relative int64  `json:"relative_to,omitempty" jsonschema:"Task id for before/after"`
	}
	cloneTaskArgs struct {
		Task    string `json:"task" jsonschema:"Task id or key"`
		Project int64  `json:"project,omitempty" jsonschema:"Target project in the same workspace"`
		Title   string `json:"title,omitempty"`
	}
	addCommentArgs struct {
		Task   string `json:"task" jsonschema:"Task id or key"`
		BodyMD string `json:"body_md" jsonschema:"Markdown"`
		Kind   string `json:"kind,omitempty" jsonschema:"comment (default), report or question"`
	}
	resumeArgs struct {
		Task      string `json:"task" jsonschema:"Task id or key"`
		CommentMD string `json:"comment_md,omitempty" jsonschema:"Answer or instructions for the next run"`
	}
	listRecArgs struct {
		Workspace string `json:"workspace,omitempty" jsonschema:"Workspace id or slug; omit to use your key's workspace"`
		Project   int64  `json:"project,omitempty" jsonschema:"Only this project"`
	}
	recArg struct {
		ID int64 `json:"id" jsonschema:"Recurring task id"`
	}
	recArgs struct {
		ID            int64    `json:"id,omitempty" jsonschema:"update_recurring: the recurring task id"`
		Project       int64    `json:"project,omitempty" jsonschema:"create_recurring: project id"`
		Title         string   `json:"title,omitempty"`
		DescriptionMD *string  `json:"description_md,omitempty" jsonschema:"Markdown, copied into each task"`
		Priority      *int64   `json:"priority,omitempty" jsonschema:"0-4"`
		Labels        []string `json:"labels,omitempty"`
		Assignees     []int64  `json:"assignees,omitempty" jsonschema:"User ids"`
		Freq          string   `json:"freq,omitempty" jsonschema:"daily, weekly or monthly"`
		Interval      int64    `json:"interval,omitempty" jsonschema:"Every N days/weeks/months (default 1)"`
		Weekdays      []int    `json:"weekdays,omitempty" jsonschema:"Weekly: 1=Mon .. 7=Sun"`
		MonthDay      *int64   `json:"month_day,omitempty" jsonschema:"Monthly: 1-31, -1 = last day"`
		TZ            string   `json:"tz,omitempty" jsonschema:"IANA zone; default: your time zone preference, else the instance default"`
		WindowStart   string   `json:"window_start,omitempty" jsonschema:"Local HH:MM when the window opens (default 00:00)"`
		WindowMinutes int64    `json:"window_minutes,omitempty" jsonschema:"How long the window stays open, minutes (default 1440)"`
		LeadMinutes   *int64   `json:"lead_minutes,omitempty" jsonschema:"Create the task this many minutes before the window opens"`
		EndsOn        *int64   `json:"ends_on,omitempty" jsonschema:"Unix ms; 0 clears"`
		MaxCount      *int64   `json:"max_count,omitempty" jsonschema:"Stop after this many tasks; 0 clears"`
		Overlap       string   `json:"overlap,omitempty" jsonschema:"skip (default: no new task while the last is still open) or create"`
		Paused        *bool    `json:"paused,omitempty"`
		Model         *string  `json:"model,omitempty" jsonschema:"Model for claude -p; empty = the agent's default"`
		Effort        *string  `json:"effort,omitempty" jsonschema:"low, medium, high, xhigh, max; empty = the agent's default"`
		AlwaysRun     *bool    `json:"always_run,omitempty" jsonschema:"Its tasks run whatever the usage budget (needs the always_run permission)"`
		AllowedTools  []string `json:"allowed_tools,omitempty" jsonschema:"Tool allowlist its tasks get for the runner (permission rules; [] clears; needs the allowed_tools permission)"`
		TemplateID    *int64   `json:"template_id,omitempty" jsonschema:"Make each task from this template (see list_templates); content fields are then ignored. update_recurring: switches to it"`
		Checklist     []string `json:"checklist,omitempty" jsonschema:"Checklist items each task gets (stored in the template)"`
		ScheduleID    *int64   `json:"schedule_id,omitempty" jsonschema:"Use this schedule (see list_schedules); timing fields are then ignored. update_recurring: switches to it"`
	}
	progressArgs struct {
		Task string `json:"task" jsonschema:"Task id or key (its running run gets the update)"`
		Pct  *int64 `json:"pct,omitempty" jsonschema:"0-100"`
		Note string `json:"note,omitempty" jsonschema:"Short progress note"`
	}
)

func (a *API) mcpServer(r *http.Request) *mcp.Server {
	p := access.From(r.Context())
	call := a.caller(r)
	s := mcp.NewServer(&mcp.Implementation{Name: "gigg", Title: "gigg task board", Version: version.Version}, nil)
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
		mcp.AddTool(s, &mcp.Tool{Name: "list_projects", Description: "Projects (boards) of a workspace, with ids and task key prefixes.", Annotations: ro("List projects")},
			func(ctx context.Context, _ *mcp.CallToolRequest, in wsArg) (*mcp.CallToolResult, any, error) {
				return toolResult(call("GET", "/workspaces/"+ws(in.Workspace)+"/projects", nil, nil))
			})
	}
	if has("tasks:read") {
		mcp.AddTool(s, &mcp.Tool{Name: "summary", Description: "Plain-text board summary: per project, each status with up to 5 tasks.", Annotations: ro("Board summary")},
			func(ctx context.Context, _ *mcp.CallToolRequest, in wsArg) (*mcp.CallToolResult, any, error) {
				return toolResult(call("GET", "/workspaces/"+ws(in.Workspace)+"/summary", nil, nil))
			})
		mcp.AddTool(s, &mcp.Tool{Name: "list_tasks", Description: "Find tasks; filters combine. Board order unless sort is given.", Annotations: ro("List tasks")},
			func(ctx context.Context, _ *mcp.CallToolRequest, in listTasksArgs) (*mcp.CallToolResult, any, error) {
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
				return toolResult(call("GET", "/workspaces/"+ws(in.Workspace)+"/tasks", q, nil))
			})
		mcp.AddTool(s, &mcp.Tool{Name: "get_task", Description: "One task by id or key, optionally with its comments.", Annotations: ro("Get task")},
			func(ctx context.Context, _ *mcp.CallToolRequest, in getTaskArgs) (*mcp.CallToolResult, any, error) {
				code, body := call("GET", "/tasks/"+t(in.Task), nil, nil)
				if code != 200 || !in.Comments || !has("comments:read") {
					return toolResult(code, body)
				}
				cc, cb := call("GET", "/tasks/"+t(in.Task)+"/comments", nil, nil)
				if cc != 200 {
					return toolResult(cc, cb)
				}
				return toolResult(200, []byte(`{"task":`+string(body)+`,"comments":`+string(cb)+`}`))
			})
		mcp.AddTool(s, &mcp.Tool{Name: "list_runs", Description: "A task's agent runs (sessions, status, cost), newest first.", Annotations: ro("List runs")},
			func(ctx context.Context, _ *mcp.CallToolRequest, in taskArg) (*mcp.CallToolResult, any, error) {
				return toolResult(call("GET", "/tasks/"+t(in.Task)+"/runs", nil, nil))
			})
	}
	if has("comments:read") {
		mcp.AddTool(s, &mcp.Tool{Name: "list_comments", Description: "A task's comments with author facts (kind, admin or not), oldest first.", Annotations: ro("List comments")},
			func(ctx context.Context, _ *mcp.CallToolRequest, in commentsArgs) (*mcp.CallToolResult, any, error) {
				q := url.Values{}
				if in.SinceID != 0 {
					q.Set("since_id", strconv.FormatInt(in.SinceID, 10))
				}
				return toolResult(call("GET", "/tasks/"+t(in.Task)+"/comments", q, nil))
			})
	}
	if has("tasks:write") {
		mcp.AddTool(s, &mcp.Tool{Name: "create_task", Description: "Create a task in a project.", Annotations: rw("Create task", false)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in createTaskArgs) (*mcp.CallToolResult, any, error) {
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
				if len(in.Checklist) > 0 {
					b["checklist"] = in.Checklist
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
				return toolResult(call("POST", fmt.Sprintf("/projects/%d/tasks", in.Project), nil, b))
			})
		mcp.AddTool(s, &mcp.Tool{Name: "update_task", Description: "Change a task's title, description, priority, labels, colour, dates, or how an agent runs it (model, effort, always_run, allowed_tools).", Annotations: rw("Update task", false)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in updateTaskArgs) (*mcp.CallToolResult, any, error) {
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
				for _, f := range in.Clear {
					switch f {
					case "color", "start_at", "due_at", "not_after", "labels":
						b[f] = nil
					default:
						return toolResult(422, []byte(`{"status":422,"code":"invalid","detail":"clear accepts color, start_at, due_at, not_after, labels"}`))
					}
				}
				return toolResult(call("PATCH", "/tasks/"+t(in.Task), nil, b))
			})
		mcp.AddTool(s, &mcp.Tool{Name: "move_task", Description: "Move a task to another status (by category or id), position, or project of the same workspace.", Annotations: rw("Move task", false)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in moveTaskArgs) (*mcp.CallToolResult, any, error) {
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
					b["task"] = in.Relative
				}
				return toolResult(call("POST", "/tasks/"+t(in.Task)+"/move", nil, b))
			})
		mcp.AddTool(s, &mcp.Tool{Name: "clone_task", Description: "Copy a task, optionally into another project of the workspace.", Annotations: rw("Clone task", false)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in cloneTaskArgs) (*mcp.CallToolResult, any, error) {
				b := map[string]any{}
				if in.Project != 0 {
					b["project"] = in.Project
				}
				setIf(b, "title", in.Title)
				return toolResult(call("POST", "/tasks/"+t(in.Task)+"/clone", nil, b))
			})
		mcp.AddTool(s, &mcp.Tool{Name: "resume_task", Description: "Ask for the task's agent session to be resumed by the runner (records the request; runs nothing).", Annotations: rw("Request resume", false)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in resumeArgs) (*mcp.CallToolResult, any, error) {
				return toolResult(call("POST", "/tasks/"+t(in.Task)+"/resume", nil, map[string]any{"comment_md": in.CommentMD}))
			})
	}
	if has("tasks:delete") {
		mcp.AddTool(s, &mcp.Tool{Name: "delete_task", Description: "Move a task to the trash (restore_task undoes it).", Annotations: rw("Delete task", true)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in taskArg) (*mcp.CallToolResult, any, error) {
				return toolResult(call("DELETE", "/tasks/"+t(in.Task), nil, nil))
			})
		mcp.AddTool(s, &mcp.Tool{Name: "restore_task", Description: "Restore a task from the trash.", Annotations: rw("Restore task", false)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in taskArg) (*mcp.CallToolResult, any, error) {
				return toolResult(call("POST", "/tasks/"+t(in.Task)+"/restore", nil, nil))
			})
	}
	if has("comments:write") {
		mcp.AddTool(s, &mcp.Tool{Name: "add_comment", Description: "Comment on a task (Markdown).", Annotations: rw("Add comment", false)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in addCommentArgs) (*mcp.CallToolResult, any, error) {
				b := map[string]any{"body_md": in.BodyMD}
				setIf(b, "kind", in.Kind)
				return toolResult(call("POST", "/tasks/"+t(in.Task)+"/comments", nil, b))
			})
	}
	if has("runs:write") && has("tasks:read") {
		mcp.AddTool(s, &mcp.Tool{Name: "report_progress", Description: "Report progress on the task's running run (percent and/or a short note).", Annotations: rw("Report progress", false)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in progressArgs) (*mcp.CallToolResult, any, error) {
				code, body := call("GET", "/tasks/"+t(in.Task)+"/runs", nil, nil)
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
				return toolResult(409, []byte(`{"status":409,"code":"conflict","detail":"The task has no running run."}`))
			})
	}
	if has("tasks:read") {
		mcp.AddTool(s, &mcp.Tool{Name: "list_recurring", Description: "Recurring tasks: each pairs a template (what the task looks like) with a schedule (when) in a project and creates a task per occurrence, inside its time window. With upcoming occurrences.", Annotations: ro("List recurring tasks")},
			func(ctx context.Context, _ *mcp.CallToolRequest, in listRecArgs) (*mcp.CallToolResult, any, error) {
				q := url.Values{}
				if in.Project != 0 {
					q.Set("project", fmt.Sprint(in.Project))
				}
				return toolResult(call("GET", "/workspaces/"+ws(in.Workspace)+"/recurrences", q, nil))
			})
	}
	if has("tasks:write") && p.AllowedAnywhere("schedules") {
		mcp.AddTool(s, &mcp.Tool{Name: "create_recurring", Description: "Create a recurring task: a template (template_id, or the task content given here, saved as a new template) on a schedule (schedule_id, or the timing given here; an identical schedule is reused, else one is made). Every day/week/month gigg creates a task whose start is the window opening and whose due/not_after is the window closing. Untouched tasks are marked missed when the window closes; claim never hands one out outside its window.", Annotations: rw("Create recurring task", false)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in recArgs) (*mcp.CallToolResult, any, error) {
				return toolResult(call("POST", fmt.Sprintf("/projects/%d/recurrences", in.Project), nil, in.merge(map[string]any{})))
			})
		mcp.AddTool(s, &mcp.Tool{Name: "update_recurring", Description: "Change a recurring task; fields you omit stay as they are. template_id / schedule_id switch to another template / schedule; content fields change its template and timing fields its schedule in place (every recurring task sharing them follows). The next occurrence is recomputed from now.", Annotations: rw("Update recurring task", false)},
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
		mcp.AddTool(s, &mcp.Tool{Name: "delete_recurring", Description: "Stop a recurring task. Tasks it already created, its template and its schedule stay.", Annotations: rw("Delete recurring task", true)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in recArg) (*mcp.CallToolResult, any, error) {
				return toolResult(call("DELETE", fmt.Sprintf("/recurrences/%d", in.ID), nil, nil))
			})
	}
	addScheduleTools(s, p, call, ws)
	if has("tasks:read") {
		addChangesTool(s, call, ws)
	}
	addItemTools(s, p, call, ws)
	addRunnerTools(s, p, call)
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
	if in.ScheduleID != nil {
		b["schedule_id"] = *in.ScheduleID
	}
	return b
}

// content: the call changes the task content.
func (in recArgs) content() bool {
	return in.Title != "" || in.DescriptionMD != nil || in.Priority != nil || in.Labels != nil || in.Assignees != nil || in.Model != nil ||
		in.Effort != nil || in.AlwaysRun != nil || in.AllowedTools != nil || in.Checklist != nil
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
