package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0x2alx/gigg/internal/access"
)

// Runner tools: read the caps/pause state, pause and resume. Same rules as POST /runner/pause:
// admins, or the pause_runs permission (per workspace, or on an all-workspaces rule).

type (
	pauseArgs struct {
		Workspaces []string `json:"workspaces,omitempty" jsonschema:"Workspace ids or slugs; omit for every workspace you may pause"`
		Minutes    int64    `json:"minutes" jsonschema:"How long, in minutes (60, 300, 720, 1440...); 0 = until resumed. Crons pause until resumed"`
		Tasks      *bool    `json:"tasks,omitempty" jsonschema:"Pause task runs (default true)"`
		Crons      *bool    `json:"crons,omitempty" jsonschema:"Pause cron runs (default true)"`
	}
	resumeRunsArgs struct {
		Workspaces []string `json:"workspaces,omitempty" jsonschema:"Workspace ids or slugs; omit for every workspace you may resume"`
		Tasks      *bool    `json:"tasks,omitempty" jsonschema:"Resume task runs (default true)"`
		Crons      *bool    `json:"crons,omitempty" jsonschema:"Resume cron runs (default true)"`
	}
)

func addRunnerTools(s *mcp.Server, p *access.Principal, call apiCall) {
	if p.HasScope("tasks:read") {
		mcp.AddTool(s, &mcp.Tool{Name: "runner_status", Description: "Per workspace in reach: the runner's task caps, whether new task runs are paused (and until when), and the last 24 hours of agent work.", Annotations: ro("Runner status")},
			func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
				return toolResult(call("GET", "/runner", nil, nil))
			})
	}
	if !p.AdminWith("admin:workspaces") && !(p.HasScope("runs:write") && p.AllowedAnywhere("pause_runs")) {
		return
	}
	// ids resolves ids or slugs against the workspaces the caller sees.
	ids := func(refs []string) ([]int64, *mcp.CallToolResult) {
		if len(refs) == 0 {
			return nil, nil
		}
		code, body := call("GET", "/runner", nil, nil)
		if code != 200 {
			r, _, _ := toolResult(code, body)
			return nil, r
		}
		var list struct {
			Items []struct {
				WorkspaceID int64  `json:"workspace_id"`
				Slug        string `json:"slug"`
			} `json:"items"`
		}
		if err := json.Unmarshal(body, &list); err != nil {
			r, _, _ := toolResult(500, []byte(`{"status":500,"code":"internal","detail":"Internal error."}`))
			return nil, r
		}
		out := make([]int64, 0, len(refs))
		for _, ref := range refs {
			found := false
			for _, it := range list.Items {
				if it.Slug == ref || strconv.FormatInt(it.WorkspaceID, 10) == ref {
					out, found = append(out, it.WorkspaceID), true
					break
				}
			}
			if !found {
				r, _, _ := toolResult(422, fmt.Appendf(nil, `{"status":422,"code":"invalid","detail":%q}`, "No such workspace: "+ref))
				return nil, r
			}
		}
		return out, nil
	}
	mcp.AddTool(s, &mcp.Tool{Name: "pause_runs", Description: "Pause new agent task runs (always-run tasks included) and cron runs in some or all workspaces, for a number of minutes or until resumed (tasks/crons: pause only one box). Runs already going finish their current step.", Annotations: rw("Pause task runs", false)},
		func(ctx context.Context, _ *mcp.CallToolRequest, in pauseArgs) (*mcp.CallToolResult, any, error) {
			if in.Minutes < 0 {
				return toolResult(422, []byte(`{"status":422,"code":"invalid","detail":"minutes is 0 (until resumed) or more."}`))
			}
			ws, bad := ids(in.Workspaces)
			if bad != nil {
				return bad, nil, nil
			}
			return toolResult(call("POST", "/runner/pause", nil, PauseBody{Workspaces: ws, Minutes: in.Minutes, Tasks: in.Tasks, Crons: in.Crons}))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "resume_runs", Description: "Lift the pause on new agent task runs and cron runs in some or all workspaces (tasks/crons: resume only one box).", Annotations: rw("Resume task runs", false)},
		func(ctx context.Context, _ *mcp.CallToolRequest, in resumeRunsArgs) (*mcp.CallToolResult, any, error) {
			ws, bad := ids(in.Workspaces)
			if bad != nil {
				return bad, nil, nil
			}
			return toolResult(call("POST", "/runner/resume", nil, PauseBody{Workspaces: ws, Tasks: in.Tasks, Crons: in.Crons}))
		})
}
