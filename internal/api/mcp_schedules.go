package api

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0x2alx/gigg/internal/access"
)

// Schedule tools: named timing that recurring tasks pair with templates.

type (
	scheduleArgs struct {
		ID            int64  `json:"id,omitempty" jsonschema:"An existing schedule to change (omit to make one); fields you omit stay as they are"`
		Workspace     string `json:"workspace,omitempty" jsonschema:"New schedule: workspace id or slug; omit to use your key's workspace"`
		Name          string `json:"name,omitempty" jsonschema:"Default: described from the rule"`
		Freq          string `json:"freq,omitempty" jsonschema:"daily, weekly or monthly (required for a new schedule)"`
		Interval      int64  `json:"interval,omitempty" jsonschema:"Every N days/weeks/months (default 1)"`
		Weekdays      []int  `json:"weekdays,omitempty" jsonschema:"Weekly: 1=Mon .. 7=Sun"`
		MonthDay      *int64 `json:"month_day,omitempty" jsonschema:"Monthly: 1-31, -1 = last day"`
		TZ            string `json:"tz,omitempty" jsonschema:"IANA zone; default: your time zone preference, else the instance default"`
		WindowStart   string `json:"window_start,omitempty" jsonschema:"Local HH:MM when the window opens (default 00:00)"`
		WindowMinutes int64  `json:"window_minutes,omitempty" jsonschema:"How long the window stays open, minutes (default 1440)"`
		LeadMinutes   *int64 `json:"lead_minutes,omitempty" jsonschema:"Create the task this many minutes before the window opens"`
	}
	scheduleIDArg struct {
		ID int64 `json:"id" jsonschema:"Schedule id"`
	}
)

func addScheduleTools(s *mcp.Server, p *access.Principal, call apiCall, ws func(string) string) {
	if p.HasScope("tasks:read") {
		mcp.AddTool(s, &mcp.Tool{Name: "list_schedules", Description: "The workspace's schedules: named timing (daily/weekly/monthly, a time window, create-early) that recurring tasks pair with templates, with how many use each.", Annotations: ro("List schedules")},
			func(ctx context.Context, _ *mcp.CallToolRequest, in wsArg) (*mcp.CallToolResult, any, error) {
				return toolResult(call("GET", "/workspaces/"+ws(in.Workspace)+"/schedules", nil, nil))
			})
	}
	if !p.HasScope("tasks:write") || !p.AllowedAnywhere("schedules") {
		return
	}
	mcp.AddTool(s, &mcp.Tool{Name: "save_schedule", Description: "Make a schedule, or change one (id): every recurring task on it moves to the new timing.", Annotations: rw("Save schedule", false)},
		func(ctx context.Context, _ *mcp.CallToolRequest, in scheduleArgs) (*mcp.CallToolResult, any, error) {
			b := map[string]any{}
			if in.ID != 0 {
				code, body := call("GET", fmt.Sprintf("/schedules/%d", in.ID), nil, nil)
				if code != 200 {
					return toolResult(code, body)
				}
				var cur map[string]any
				_ = json.Unmarshal(body, &cur)
				for _, k := range []string{"name", "freq", "interval", "weekdays", "month_day", "tz", "window_start", "window_minutes", "lead_minutes"} {
					if v, ok := cur[k]; ok && v != nil {
						b[k] = v
					}
				}
			}
			setIf(b, "name", in.Name)
			setIf(b, "freq", in.Freq)
			setIf(b, "tz", in.TZ)
			setIf(b, "window_start", in.WindowStart)
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
			if in.ID != 0 {
				return toolResult(call("PUT", fmt.Sprintf("/schedules/%d", in.ID), nil, b))
			}
			return toolResult(call("POST", "/workspaces/"+ws(in.Workspace)+"/schedules", nil, b))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "delete_schedule", Description: "Delete a schedule no recurring task uses.", Annotations: rw("Delete schedule", true)},
		func(ctx context.Context, _ *mcp.CallToolRequest, in scheduleIDArg) (*mcp.CallToolResult, any, error) {
			return toolResult(call("DELETE", fmt.Sprintf("/schedules/%d", in.ID), nil, nil))
		})
}
