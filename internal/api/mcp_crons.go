package api

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0x2alx/gigg/internal/access"
)

// Cron tools: commands an agent's executor runs on a schedule, sandboxed, with a run history.

type (
	cronSaveArgs struct {
		ID            int64  `json:"id,omitempty" jsonschema:"A cron to change (omit to make one); fields you omit stay as they are"`
		Workspace     string `json:"workspace,omitempty" jsonschema:"New cron: workspace id or slug; omit to use your key's workspace"`
		TemplateID    *int64 `json:"template_id,omitempty" jsonschema:"Use this cron template (see list_cron_templates) instead of name/command/..."`
		Name          string `json:"name,omitempty" jsonschema:"New cron: required unless template_id"`
		DescriptionMD string `json:"description_md,omitempty"`
		Command       string `json:"command,omitempty" jsonschema:"Run by bash in the worker agent's sandbox (no access to localhost services: use MCP from a task for those)"`
		TimeoutS      int64  `json:"timeout_s,omitempty" jsonschema:"Seconds, 10-3600 (default 300)"`
		Output        string `json:"output,omitempty" jsonschema:"gigg (default: the last 64 KiB of output is kept with each run) or host (only exit code and timing reach gigg)"`
		Unsandboxed   *bool  `json:"unsandboxed,omitempty" jsonschema:"Run outside the sandbox: needs the No-sandbox crons permission; any change by someone without it turns it off"`
		ScheduleID    *int64 `json:"schedule_id,omitempty" jsonschema:"Use this schedule (see list_schedules) instead of freq/interval/..."`
		Freq          string `json:"freq,omitempty" jsonschema:"minutes, hours, daily, weekly or monthly (new cron: required unless schedule_id)"`
		Interval      int64  `json:"interval,omitempty" jsonschema:"Every N minutes (1-360), hours (1-24), days, weeks or months (default 1)"`
		Weekdays      []int  `json:"weekdays,omitempty" jsonschema:"weekly: the days; minutes/hours: only on these days. 1=Mon .. 7=Sun"`
		MonthDay      *int64 `json:"month_day,omitempty" jsonschema:"monthly: 1-31, -1 = last day"`
		TZ            string `json:"tz,omitempty" jsonschema:"IANA zone; default: your time zone preference"`
		At            string `json:"at,omitempty" jsonschema:"Local HH:MM: daily/weekly/monthly run time; minutes/hours: when ticking starts each day (default 00:00)"`
		ForMinutes    int64  `json:"for_minutes,omitempty" jsonschema:"minutes/hours: how long ticking lasts each day (default 1440 = all day)"`
		Worker        string `json:"worker,omitempty" jsonschema:"Username of the agent whose executor runs it (default: you)"`
		OnFailureTask *bool  `json:"on_failure_task,omitempty" jsonschema:"The first failure opens a task for the worker"`
		Paused        bool   `json:"paused,omitempty" jsonschema:"New cron: create it paused"`
	}
	cronIDArg struct {
		ID int64 `json:"id" jsonschema:"Cron id"`
	}
	cronPauseArgs struct {
		ID     int64 `json:"id" jsonschema:"Cron id"`
		Resume bool  `json:"resume,omitempty" jsonschema:"true = resume (from its next slot; no catch-up)"`
	}
	cronRunsArgs struct {
		ID     int64 `json:"id" jsonschema:"Cron id"`
		Before int64 `json:"before,omitempty" jsonschema:"Only runs with a smaller id (paging)"`
		Limit  int   `json:"limit,omitempty" jsonschema:"Default 20, at most 200"`
	}
	cronRunArg struct {
		ID int64 `json:"id" jsonschema:"Cron run id (see list_cron_runs)"`
	}
)

func addCronTools(s *mcp.Server, p *access.Principal, call apiCall, ws func(string) string) {
	if p.HasScope("crons:read") {
		mcp.AddTool(s, &mcp.Tool{Name: "list_crons", Description: "The workspace's crons (commands an agent's executor runs on a schedule): state (ok, failing, never), schedule in words, worker, last run and next slot. Failing first.", Annotations: ro("List crons")},
			func(ctx context.Context, _ *mcp.CallToolRequest, in wsArg) (*mcp.CallToolResult, any, error) {
				return toolResult(call("GET", "/workspaces/"+ws(in.Workspace)+"/crons", nil, nil))
			})
		mcp.AddTool(s, &mcp.Tool{Name: "get_cron", Description: "One cron with its command, schedule and last run.", Annotations: ro("Get cron")},
			func(ctx context.Context, _ *mcp.CallToolRequest, in cronIDArg) (*mcp.CallToolResult, any, error) {
				return toolResult(call("GET", fmt.Sprintf("/crons/%d", in.ID), nil, nil))
			})
		mcp.AddTool(s, &mcp.Tool{Name: "list_cron_runs", Description: "A cron's runs, newest first: status (ok, failed, timeout, missed, skipped), exit code, duration. get_cron_run has the output.", Annotations: ro("List cron runs")},
			func(ctx context.Context, _ *mcp.CallToolRequest, in cronRunsArgs) (*mcp.CallToolResult, any, error) {
				limit := in.Limit
				if limit == 0 {
					limit = 20
				}
				return toolResult(call("GET", fmt.Sprintf("/crons/%d/runs?limit=%d&before=%d", in.ID, limit, in.Before), nil, nil))
			})
		mcp.AddTool(s, &mcp.Tool{Name: "get_cron_run", Description: "One cron run, with its output tail when your key may read cron output.", Annotations: ro("Get cron run")},
			func(ctx context.Context, _ *mcp.CallToolRequest, in cronRunArg) (*mcp.CallToolResult, any, error) {
				return toolResult(call("GET", fmt.Sprintf("/cron-runs/%d", in.ID), nil, nil))
			})
		mcp.AddTool(s, &mcp.Tool{Name: "list_cron_templates", Description: "The workspace's cron templates (command, timeout, output, sandbox), with how many crons use each.", Annotations: ro("List cron templates")},
			func(ctx context.Context, _ *mcp.CallToolRequest, in wsArg) (*mcp.CallToolResult, any, error) {
				return toolResult(call("GET", "/workspaces/"+ws(in.Workspace)+"/cron-templates", nil, nil))
			})
	}
	if !p.HasScope("crons:write") || !p.AllowedAnywhere("crons") {
		return
	}
	mcp.AddTool(s, &mcp.Tool{Name: "save_cron", Description: "Make a cron, or change one (id): a command your executor runs on a schedule, sandboxed, with a run history (no tasks). Changing its command, name or timeout changes its template (shared by the template's other crons).", Annotations: rw("Save cron", false)},
		func(ctx context.Context, _ *mcp.CallToolRequest, in cronSaveArgs) (*mcp.CallToolResult, any, error) {
			b := map[string]any{}
			setIf(b, "name", in.Name)
			setIf(b, "description_md", in.DescriptionMD)
			setIf(b, "command", in.Command)
			setIf(b, "output", in.Output)
			setIf(b, "worker", in.Worker)
			if in.TimeoutS != 0 {
				b["timeout_s"] = in.TimeoutS
			}
			if in.Unsandboxed != nil {
				b["unsandboxed"] = *in.Unsandboxed
			}
			if in.TemplateID != nil {
				b["template_id"] = *in.TemplateID
			}
			if in.ScheduleID != nil {
				b["schedule_id"] = *in.ScheduleID
			}
			if in.OnFailureTask != nil {
				b["on_failure_task"] = *in.OnFailureTask
			}
			if in.Freq != "" {
				r := map[string]any{"freq": in.Freq}
				setIf(r, "tz", in.TZ)
				setIf(r, "window_start", in.At)
				if in.Interval != 0 {
					r["interval"] = in.Interval
				}
				if in.Weekdays != nil {
					r["weekdays"] = in.Weekdays
				}
				if in.MonthDay != nil {
					r["month_day"] = *in.MonthDay
				}
				if in.ForMinutes != 0 {
					r["window_minutes"] = in.ForMinutes
				} else if in.Freq != "minutes" && in.Freq != "hours" {
					r["window_minutes"] = 60
				}
				b["rule"] = r
			}
			if in.ID != 0 {
				return toolResult(call("PATCH", fmt.Sprintf("/crons/%d", in.ID), nil, b))
			}
			if in.Paused {
				b["paused"] = true
			}
			return toolResult(call("POST", "/workspaces/"+ws(in.Workspace)+"/crons", nil, b))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "pause_cron", Description: "Pause a cron, or resume it (resume: true; it starts again from its next slot, missed ones aren't caught up).", Annotations: rw("Pause cron", false)},
		func(ctx context.Context, _ *mcp.CallToolRequest, in cronPauseArgs) (*mcp.CallToolResult, any, error) {
			act := "pause"
			if in.Resume {
				act = "resume"
			}
			return toolResult(call("POST", fmt.Sprintf("/crons/%d/%s", in.ID, act), nil, nil))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "run_cron_now", Description: "Run a cron once at its executor's next poll (about a minute), whatever its schedule.", Annotations: rw("Run cron now", false)},
		func(ctx context.Context, _ *mcp.CallToolRequest, in cronIDArg) (*mcp.CallToolResult, any, error) {
			return toolResult(call("POST", fmt.Sprintf("/crons/%d/run-now", in.ID), nil, nil))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "delete_cron", Description: "Delete a cron (its template and schedule stay).", Annotations: rw("Delete cron", true)},
		func(ctx context.Context, _ *mcp.CallToolRequest, in cronIDArg) (*mcp.CallToolResult, any, error) {
			return toolResult(call("DELETE", fmt.Sprintf("/crons/%d", in.ID), nil, nil))
		})
}
