package api

import (
	"context"
	"database/sql"

	"github.com/danielgtaylor/huma/v2"

	"github.com/0x2alx/gigg/internal/access"
	"github.com/0x2alx/gigg/internal/store"
)

// Schedules: named timing (rule, window, create-early) of a workspace. Recurring tasks pair one
// with a template. Reading needs view; changing needs the Recurring tasks permission.

// ScheduleBody creates or replaces a schedule.
type ScheduleBody struct {
	Name          string `json:"name,omitempty" maxLength:"80" doc:"Default: described from the rule"`
	Freq          string `json:"freq" enum:"daily,weekly,monthly"`
	Interval      int64  `json:"interval,omitempty" minimum:"0" maximum:"365" doc:"Every N days/weeks/months (default 1)"`
	Weekdays      []int  `json:"weekdays,omitempty" doc:"Weekly: 1=Mon .. 7=Sun"`
	MonthDay      *int64 `json:"month_day,omitempty" doc:"Monthly: 1-31, -1 = last day"`
	TZ            string `json:"tz,omitempty" doc:"IANA time zone (default: the user's time zone preference, else the instance default GIGG_DEFAULT_TZ)"`
	WindowStart   string `json:"window_start,omitempty" pattern:"^[0-2][0-9]:[0-5][0-9]$" doc:"When the window opens, local HH:MM (default 00:00)"`
	WindowMinutes int64  `json:"window_minutes,omitempty" minimum:"0" maximum:"44640" doc:"How long the window stays open (default 1440)"`
	LeadMinutes   int64  `json:"lead_minutes,omitempty" minimum:"0" maximum:"10080" doc:"Create the task this long before the window opens"`
}

func (b ScheduleBody) apply(s *store.Schedule) {
	s.Name = b.Name
	s.Rule = store.Rule{Freq: b.Freq, Interval: b.Interval, Weekdays: b.Weekdays, MonthDay: b.MonthDay, TZ: b.TZ, WindowStart: b.WindowStart,
		WindowMinutes: b.WindowMinutes, LeadMinutes: b.LeadMinutes}
}

type scheduleOut struct {
	ETag string `header:"ETag"`
	Body *store.Schedule
}

func (a *API) schedule(ctx context.Context, p *access.Principal, id int64) (*store.Schedule, error) {
	s, err := store.GetSchedule(ctx, a.st.R, id)
	if err != nil || !p.SeesWorkspace(s.WorkspaceID) {
		return nil, errNotFound()
	}
	return s, nil
}

// scheduleUsedOutside: a live recurring task outside the projects a narrowed key reaches uses it.
func (a *API) scheduleUsedOutside(ctx context.Context, p *access.Principal, id int64) bool {
	if !p.Narrowed() {
		return false
	}
	rows, err := a.st.R.QueryContext(ctx, `SELECT DISTINCT project_id FROM recurrences WHERE schedule_id = ? AND deleted_at IS NULL`, id)
	if err != nil {
		return true
	}
	defer rows.Close()
	for rows.Next() {
		var pid int64
		if rows.Scan(&pid) != nil {
			return true
		}
		reach := false
		for _, k := range p.ProjectFilter() {
			reach = reach || k == pid
		}
		if !reach {
			return true
		}
	}
	return false
}

func (a *API) registerSchedules() {
	huma.Register(a.h, huma.Operation{OperationID: "list-schedules", Method: "GET", Path: "/workspaces/{ws}/schedules", Tags: []string{"recurrence"},
		Summary: "A workspace's schedules (named timing that recurring tasks pair with templates)"}, func(ctx context.Context, in *wsPath) (*struct {
		Body struct {
			Items []*store.Schedule `json:"items"`
		}
	}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		w, err := a.workspace(ctx, p, in.WS)
		if err != nil {
			return nil, err
		}
		if err := require(p, w.ID, access.Viewer, "tasks:read"); err != nil {
			return nil, err
		}
		ss, err := store.ListSchedules(ctx, a.st.R, w.ID)
		out := &struct {
			Body struct {
				Items []*store.Schedule `json:"items"`
			}
		}{}
		out.Body.Items = ss
		return out, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "create-schedule", Method: "POST", Path: "/workspaces/{ws}/schedules", Tags: []string{"recurrence"},
		Summary: "Make a schedule", DefaultStatus: 201}, func(ctx context.Context, in *struct {
		WS   string `path:"ws"`
		Body ScheduleBody
	}) (*scheduleOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		w, err := a.workspace(ctx, p, in.WS)
		if err != nil {
			return nil, err
		}
		if err := require(p, w.ID, "schedules", "tasks:write"); err != nil {
			return nil, err
		}
		s := &store.Schedule{WorkspaceID: w.ID}
		if in.Body.TZ == "" {
			in.Body.TZ = userTZ(p)
		}
		in.Body.apply(s)
		if err := a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			s, err = store.SaveSchedule(ctx, tx, p.Actor(), s, 0)
			return err
		}); err != nil {
			return nil, err
		}
		return &scheduleOut{ETag: etag(s.Version), Body: s}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "get-schedule", Method: "GET", Path: "/schedules/{id}", Tags: []string{"recurrence"},
		Summary: "A schedule"}, func(ctx context.Context, in *idPath) (*scheduleOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		s, err := a.schedule(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if err := require(p, s.WorkspaceID, access.Viewer, "tasks:read"); err != nil {
			return nil, err
		}
		return &scheduleOut{ETag: etag(s.Version), Body: s}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "update-schedule", Method: "PUT", Path: "/schedules/{id}", Tags: []string{"recurrence"},
		Summary: "Replace a schedule (every recurring task on it moves to the new timing)"}, func(ctx context.Context, in *struct {
		ID      int64  `path:"id"`
		IfMatch string `header:"If-Match"`
		Body    ScheduleBody
	}) (*scheduleOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		s, err := a.schedule(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if err := require(p, s.WorkspaceID, "schedules", "tasks:write"); err != nil {
			return nil, err
		}
		if a.scheduleUsedOutside(ctx, p, s.ID) {
			return nil, errForbidden("This schedule is used outside the projects this key reaches.")
		}
		// retiming a schedule retimes every recurring task on it: an always-run one (or one with a tool
		// allowlist) needs those permissions, as editing that recurring task would
		always, tools, err := store.ScheduleGuards(ctx, a.st.R, s.ID)
		if err != nil {
			return nil, err
		}
		if err := alwaysRunGuard(p, s.WorkspaceID, always); err != nil {
			return nil, err
		}
		if err := toolsGuard(p, s.WorkspaceID, tools); err != nil {
			return nil, err
		}
		v, err := ifMatch(in.IfMatch)
		if err != nil {
			return nil, err
		}
		if in.Body.TZ == "" {
			in.Body.TZ = s.TZ // an edit that names no zone keeps the schedule's
		}
		in.Body.apply(s)
		if err := a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			s, err = store.SaveSchedule(ctx, tx, p.Actor(), s, v)
			return err
		}); err != nil {
			return nil, err
		}
		return &scheduleOut{ETag: etag(s.Version), Body: s}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "delete-schedule", Method: "DELETE", Path: "/schedules/{id}", Tags: []string{"recurrence"},
		Summary: "Delete a schedule no recurring task uses", DefaultStatus: 204}, func(ctx context.Context, in *idPath) (*struct{}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		s, err := a.schedule(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if err := require(p, s.WorkspaceID, "schedules", "tasks:write"); err != nil {
			return nil, err
		}
		return nil, a.tx(ctx, func(tx *sql.Tx) error { return store.DeleteSchedule(ctx, tx, p.Actor(), s) })
	})
}
