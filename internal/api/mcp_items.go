package api

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0x2alx/gigg/internal/access"
)

// MCP tools for relations, checklists, archive, custom fields, templates, notifications and saved
// filters. Like every tool they call the REST API in-process with the caller's credentials, so the
// same checks apply; tools are only listed when the key's scopes (and capabilities) allow them.

type (
	linkArgs struct {
		Gig       string `json:"gig" jsonschema:"Gig id or key"`
		Other     string `json:"other" jsonschema:"The other gig (id or key, same workspace)"`
		Direction string `json:"direction" jsonschema:"blocks (gig blocks other), blocked_by (other blocks gig) or relates"`
	}
	checklistArgs struct {
		Gig   string   `json:"gig" jsonschema:"Gig id or key"`
		Add   []string `json:"add,omitempty" jsonschema:"Items to append"`
		Check []int64  `json:"check,omitempty" jsonschema:"Item ids to tick"`
		Clear []int64  `json:"uncheck,omitempty" jsonschema:"Item ids to untick"`
		Del   []int64  `json:"remove,omitempty" jsonschema:"Item ids to remove"`
	}
	customArgs struct {
		Gig    string         `json:"gig" jsonschema:"Gig id or key"`
		Values map[string]any `json:"values" jsonschema:"Field id -> value (null clears); see list_fields"`
	}
	useTemplateArgs struct {
		Template int64  `json:"template" jsonschema:"Template id (see list_templates)"`
		Project  int64  `json:"project" jsonschema:"Project id"`
		Title    string `json:"title,omitempty" jsonschema:"Override the template's title"`
	}
	listTemplatesArgs struct {
		Workspace string `json:"workspace,omitempty" jsonschema:"Workspace id or slug; omit to use your key's workspace"`
		Project   int64  `json:"project,omitempty" jsonschema:"Only templates usable in this project"`
	}
	saveTemplateArgs struct {
		Workspace     string         `json:"workspace,omitempty" jsonschema:"Workspace id or slug; omit to use your key's workspace"`
		ID            int64          `json:"id,omitempty" jsonschema:"Template id to replace (omit to create)"`
		Project       *int64         `json:"project_id,omitempty" jsonschema:"Limit to one project"`
		Name          string         `json:"name"`
		Title         string         `json:"title"`
		DescriptionMD string         `json:"description_md,omitempty"`
		Priority      int64          `json:"priority,omitempty" jsonschema:"0-4"`
		Labels        []string       `json:"labels,omitempty"`
		Assignees     []int64        `json:"assignees,omitempty"`
		Checklist     []string       `json:"checklist,omitempty"`
		Subgigs       []subgigArg    `json:"subgigs,omitempty" jsonschema:"Subgigs each gig made from it gets: title, and blocks_parent when the gig should wait for it"`
		Custom        map[string]any `json:"custom,omitempty"`
		Model         string         `json:"model,omitempty" jsonschema:"Model a runner should ask for (default: the agent's)"`
		Effort        string         `json:"effort,omitempty" jsonschema:"low, medium, high, xhigh or max (default: the agent's)"`
		AlwaysRun     bool           `json:"always_run,omitempty" jsonschema:"Gigs made from it run whatever the usage budget (needs the always_run permission)"`
		AllowedTools  []string       `json:"allowed_tools,omitempty" jsonschema:"Tool allowlist its gigs get for the runner (permission rules; on update omit to keep, [] clears; needs the allowed_tools permission)"`
	}
	notifArgs struct {
		Unread bool `json:"unread,omitempty" jsonschema:"Only unread"`
		Limit  int  `json:"limit,omitempty"`
	}
	readArgs struct {
		IDs []int64 `json:"ids,omitempty" jsonschema:"Notification ids; omit to mark all read"`
	}
	archiveArgs struct {
		Gig     string `json:"gig" jsonschema:"Gig id or key"`
		Restore bool   `json:"restore,omitempty" jsonschema:"true = unarchive"`
	}
)

func addItemTools(s *mcp.Server, p *access.Principal, call apiCall, ws func(string) string) {
	t := url.PathEscape
	has := p.HasScope
	if has("gigs:read") {
		mcp.AddTool(s, &mcp.Tool{Name: "list_relations", Description: "A gig's dependencies: what blocks it, what it blocks, related gigs (with open/done state).", Annotations: ro("List relations")},
			func(ctx context.Context, _ *mcp.CallToolRequest, in gigArg) (*mcp.CallToolResult, any, error) {
				return toolResult(call("GET", "/gigs/"+t(in.Gig)+"/relations", nil, nil))
			})
		mcp.AddTool(s, &mcp.Tool{Name: "get_checklist", Description: "A gig's checklist items (ids, text, done).", Annotations: ro("Get checklist")},
			func(ctx context.Context, _ *mcp.CallToolRequest, in gigArg) (*mcp.CallToolResult, any, error) {
				return toolResult(call("GET", "/gigs/"+t(in.Gig)+"/checklist", nil, nil))
			})
		mcp.AddTool(s, &mcp.Tool{Name: "list_fields", Description: "The workspace's custom gig fields (ids, kinds, select options).", Annotations: ro("List custom fields")},
			func(ctx context.Context, _ *mcp.CallToolRequest, in wsArg) (*mcp.CallToolResult, any, error) {
				return toolResult(call("GET", "/workspaces/"+ws(in.Workspace)+"/fields", nil, nil))
			})
		mcp.AddTool(s, &mcp.Tool{Name: "list_templates", Description: "Gig templates you can create gigs from.", Annotations: ro("List templates")},
			func(ctx context.Context, _ *mcp.CallToolRequest, in listTemplatesArgs) (*mcp.CallToolResult, any, error) {
				q := url.Values{}
				if in.Project != 0 {
					q.Set("project", strconv.FormatInt(in.Project, 10))
				}
				return toolResult(call("GET", "/workspaces/"+ws(in.Workspace)+"/templates", q, nil))
			})
		mcp.AddTool(s, &mcp.Tool{Name: "notifications", Description: "Your notifications: @mentions, assignments, questions on your gigs, missed windows.", Annotations: ro("Notifications")},
			func(ctx context.Context, _ *mcp.CallToolRequest, in notifArgs) (*mcp.CallToolResult, any, error) {
				q := url.Values{}
				if in.Unread {
					q.Set("unread", "true")
				}
				if in.Limit != 0 {
					q.Set("limit", strconv.Itoa(in.Limit))
				}
				return toolResult(call("GET", "/me/notifications", q, nil))
			})
		mcp.AddTool(s, &mcp.Tool{Name: "mark_notifications_read", Description: "Mark notifications read (some ids, or all).", Annotations: rw("Mark read", false)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in readArgs) (*mcp.CallToolResult, any, error) {
				b := map[string]any{"all": true}
				if len(in.IDs) > 0 {
					b = map[string]any{"ids": in.IDs}
				}
				return toolResult(call("POST", "/me/notifications/read", nil, b))
			})
	}
	// ticking needs gigs:work and Work on gigs; adding and removing items gigs:write and Edit gigs
	canTick := has("gigs:work") && p.AllowedAnywhere("work_gigs")
	canItems := has("gigs:write") && p.AllowedAnywhere("edit_gigs")
	if !has("gigs:write") {
		if canTick {
			addChecklistTool(s, call)
		}
		return
	}
	if p.AllowedAnywhere("edit_gigs") {
		mcp.AddTool(s, &mcp.Tool{Name: "link_gigs", Description: "Add a dependency: blocks, blocked_by or relates. A blocked gig isn't handed out by claim until its blockers are done.", Annotations: rw("Link gigs", false)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in linkArgs) (*mcp.CallToolResult, any, error) {
				return toolResult(call("POST", "/gigs/"+t(in.Gig)+"/relations", nil, map[string]any{"gig": in.Other, "direction": in.Direction}))
			})
		mcp.AddTool(s, &mcp.Tool{Name: "unlink_gigs", Description: "Remove a dependency.", Annotations: rw("Unlink gigs", true)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in linkArgs) (*mcp.CallToolResult, any, error) {
				return toolResult(call("DELETE", "/gigs/"+t(in.Gig)+"/relations", url.Values{"gig": {in.Other}, "direction": {in.Direction}}, nil))
			})
	}
	if canTick || canItems {
		addChecklistTool(s, call)
	}
	if p.AllowedAnywhere("archive_gigs") {
		mcp.AddTool(s, &mcp.Tool{Name: "archive_gig", Description: "Archive a gig (off the board and lists, not trash), or bring it back with restore.", Annotations: rw("Archive gig", false)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in archiveArgs) (*mcp.CallToolResult, any, error) {
				op := "/archive"
				if in.Restore {
					op = "/unarchive"
				}
				return toolResult(call("POST", "/gigs/"+t(in.Gig)+op, nil, nil))
			})
	}
	mcp.AddTool(s, &mcp.Tool{Name: "set_custom_fields", Description: "Set or clear a gig's custom field values (field ids from list_fields).", Annotations: rw("Set custom fields", false)},
		func(ctx context.Context, _ *mcp.CallToolRequest, in customArgs) (*mcp.CallToolResult, any, error) {
			return toolResult(call("PATCH", "/gigs/"+t(in.Gig)+"/custom", nil, map[string]any{"values": in.Values}))
		})
	if p.AllowedAnywhere("create_gigs") {
		mcp.AddTool(s, &mcp.Tool{Name: "create_from_template", Description: "Create a gig from a template (labels, checklist, custom values included).", Annotations: rw("Create from template", false)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in useTemplateArgs) (*mcp.CallToolResult, any, error) {
				b := map[string]any{"project": in.Project}
				setIf(b, "title", in.Title)
				return toolResult(call("POST", fmt.Sprintf("/templates/%d/use", in.Template), nil, b))
			})
	}
	if p.AllowedAnywhere("templates") {
		mcp.AddTool(s, &mcp.Tool{Name: "save_template", Description: "Create a gig template, or replace one (id).", Annotations: rw("Save template", false)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in saveTemplateArgs) (*mcp.CallToolResult, any, error) {
				b := map[string]any{"name": in.Name, "title": in.Title}
				setIf(b, "description_md", in.DescriptionMD)
				if in.Project != nil {
					b["project_id"] = *in.Project
				}
				if in.Priority != 0 {
					b["priority"] = in.Priority
				}
				if in.Subgigs != nil {
					b["subgigs"] = in.Subgigs
				}
				for k, v := range map[string]any{"labels": in.Labels, "assignees": in.Assignees, "checklist": in.Checklist} {
					switch x := v.(type) {
					case []string:
						if x != nil {
							b[k] = x
						}
					case []int64:
						if x != nil {
							b[k] = x
						}
					}
				}
				if in.Custom != nil {
					b["custom"] = in.Custom
				}
				setIf(b, "model", in.Model)
				setIf(b, "effort", in.Effort)
				if in.AlwaysRun {
					b["always_run"] = true
				}
				if in.AllowedTools != nil {
					b["allowed_tools"] = in.AllowedTools
				}
				if in.ID != 0 {
					return toolResult(call("PUT", fmt.Sprintf("/templates/%d", in.ID), nil, b))
				}
				return toolResult(call("POST", "/workspaces/"+ws(in.Workspace)+"/templates", nil, b))
			})
	}
}

// addChecklistTool: update_checklist; the API answers each part with its own permission.
func addChecklistTool(s *mcp.Server, call apiCall) {
	t := url.PathEscape
	mcp.AddTool(s, &mcp.Tool{Name: "update_checklist", Description: "Tick or untick checklist items (Work on gigs), or add and remove them (Edit gigs), in one call; returns the checklist.", Annotations: rw("Update checklist", false)},
		func(ctx context.Context, _ *mcp.CallToolRequest, in checklistArgs) (*mcp.CallToolResult, any, error) {
			if len(in.Add) > 0 {
				if code, body := call("POST", "/gigs/"+t(in.Gig)+"/checklist", nil, map[string]any{"items": in.Add}); code >= 300 {
					return toolResult(code, body)
				}
			}
			for _, set := range []struct {
				ids  []int64
				done bool
			}{{in.Check, true}, {in.Clear, false}} {
				for _, id := range set.ids {
					if code, body := call("PATCH", fmt.Sprintf("/gigs/%s/checklist/%d", t(in.Gig), id), nil, map[string]any{"done": set.done}); code >= 300 {
						return toolResult(code, body)
					}
				}
			}
			for _, id := range in.Del {
				if code, body := call("DELETE", fmt.Sprintf("/gigs/%s/checklist/%d", t(in.Gig), id), nil, nil); code >= 300 {
					return toolResult(code, body)
				}
			}
			return toolResult(call("GET", "/gigs/"+t(in.Gig)+"/checklist", nil, nil))
		})
}
