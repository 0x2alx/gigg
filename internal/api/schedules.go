package api

import (
	"context"
	"database/sql"

	"github.com/danielgtaylor/huma/v2"

	"github.com/0x2alx/gigg/internal/access"
	"github.com/0x2alx/gigg/internal/store"
)

// Schedules: named timing (rule, window, create-early) of a workspace, or shared by every workspace.
// Recurring gigs pair one with a template. Reading needs view; changing needs the Recurring gigs
// permission (a shared one: Shared library, on the all-workspaces level).

// ScheduleBody creates or replaces a schedule.
type ScheduleBody struct {
	Name          string `json:"name,omitempty" maxLength:"80" doc:"Default: described from the rule"`
	Freq          string `json:"freq" enum:"minutes,hours,daily,weekly,monthly" doc:"minutes and hours: for errands only"`
	Interval      int64  `json:"interval,omitempty" minimum:"1" maximum:"365" doc:"Every N minutes (1-360), hours (1-24), days, weeks or months (omit for 1)"`
	Weekdays      []int  `json:"weekdays,omitempty" doc:"Weekly: 1=Mon .. 7=Sun; minutes/hours: only on these days (empty: every day)"`
	MonthDay      *int64 `json:"month_day,omitempty" doc:"Monthly: 1-31, -1 = last day"`
	TZ            string `json:"tz,omitempty" doc:"IANA time zone (default: the user's time zone preference, else the instance default GIGG_DEFAULT_TZ)"`
	WindowStart   string `json:"window_start,omitempty" pattern:"^[0-2][0-9]:[0-5][0-9]$" doc:"When the window opens, local HH:MM (default 00:00)"`
	WindowMinutes int64  `json:"window_minutes,omitempty" minimum:"5" maximum:"44640" doc:"How long the window stays open, 5 to 44640 minutes (omit for 1440)"`
	LeadMinutes   int64  `json:"lead_minutes,omitempty" minimum:"0" maximum:"10080" doc:"Create the gig this long before the window opens"`
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
	if err != nil || (s.Shared && !seesShared(p)) || (!s.Shared && !p.Allowed(s.WorkspaceID, access.Viewer)) {
		return nil, errNotFound()
	}
	return s, nil
}

// sharedScheduleWrite: a shared schedule is made, changed and deleted with Shared library and gigs:write
// (sharedWrite).
func sharedScheduleWrite(p *access.Principal) error { return sharedWrite(p, "gigs:write") }

// retimeGuards: retiming a schedule retimes every recurring gig and errand on it, so an always-run
// recurring gig (or one with a tool allowlist) on it needs those permissions, as editing that recurring
// gig would, and a no-sandbox errand on it needs No-sandbox errands. ws 0 (a shared schedule): on the
// all-workspaces level. Any errand on it also needs Errands (and the errands:write scope) at that level.
func (a *API) retimeGuards(ctx context.Context, p *access.Principal, ws int64, s *store.Schedule) error {
	if s.ErrandUses > 0 { // retiming a schedule retimes its errands too
		if err := require(p, ws, "errands", "errands:write"); err != nil {
			return err
		}
	}
	always, tools, err := store.ScheduleGuards(ctx, a.st.R, s.ID)
	if err != nil {
		return err
	}
	if err := alwaysRunGuard(p, ws, always); err != nil {
		return err
	}
	if err := toolsGuard(p, ws, tools); err != nil {
		return err
	}
	if s.ErrandUses == 0 {
		return nil
	}
	if n, err := store.UnsandboxedErrandUses(ctx, a.st.R, s.ID); err != nil {
		return err
	} else if n > 0 && !p.Allowed(ws, "errand_unsandboxed") {
		return errForbidden("An errand without the sandbox uses this schedule: changing it needs No-sandbox errands.")
	}
	return nil
}

// scheduleUsedOutside: a live recurring gig outside the projects of ws the caller reaches uses it.
func (a *API) scheduleUsedOutside(ctx context.Context, p *access.Principal, ws, id int64) bool {
	if !p.Narrowed(ws) {
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
		for _, k := range p.ProjectFilter(ws) {
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
		Summary: "A workspace's schedules (named timing that recurring gigs pair with templates)"}, func(ctx context.Context, in *wsPath) (*struct {
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
		if err := require(p, w.ID, access.Viewer, "gigs:read"); err != nil {
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
		if err := require(p, w.ID, "recurring_gigs", "gigs:write"); err != nil {
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
	huma.Register(a.h, huma.Operation{OperationID: "list-shared-schedules", Method: "GET", Path: "/shared/schedules", Tags: []string{"recurrence"},
		Summary: "The shared schedules (every workspace can use them)"}, func(ctx context.Context, _ *struct{}) (*struct {
		Body struct {
			Items []*store.Schedule `json:"items"`
		}
	}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		out := &struct {
			Body struct {
				Items []*store.Schedule `json:"items"`
			}
		}{}
		out.Body.Items = []*store.Schedule{}
		if !seesShared(p) {
			return out, nil
		}
		if err := need(p, "gigs:read"); err != nil {
			return nil, err
		}
		ss, err := store.ListSchedules(ctx, a.st.R, 0)
		if ss != nil {
			out.Body.Items = ss
		}
		return out, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "create-shared-schedule", Method: "POST", Path: "/shared/schedules", Tags: []string{"recurrence"},
		Summary: "Make a shared schedule (Shared library, on the all-workspaces level)", DefaultStatus: 201}, func(ctx context.Context, in *struct {
		Body ScheduleBody
	}) (*scheduleOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := sharedScheduleWrite(p); err != nil {
			return nil, err
		}
		s := &store.Schedule{}
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
		if err := readIn(p, s.WorkspaceID, "gigs:read"); err != nil {
			return nil, err
		}
		return &scheduleOut{ETag: etag(s.Version), Body: s}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "update-schedule", Method: "PUT", Path: "/schedules/{id}", Tags: []string{"recurrence"},
		Summary: "Replace a schedule (every recurring gig on it moves to the new timing)"}, func(ctx context.Context, in *struct {
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
		if s.Shared {
			if err := sharedScheduleWrite(p); err != nil {
				return nil, err
			}
			if err := a.retimeGuards(ctx, p, 0, s); err != nil {
				return nil, err
			}
			v, err := ifMatch(in.IfMatch)
			if err != nil {
				return nil, err
			}
			if in.Body.TZ == "" {
				in.Body.TZ = s.TZ
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
		}
		if err := require(p, s.WorkspaceID, "recurring_gigs", "gigs:write"); err != nil {
			return nil, err
		}
		if a.scheduleUsedOutside(ctx, p, s.WorkspaceID, s.ID) {
			return nil, errForbidden("This schedule is used outside the projects this key reaches.")
		}
		if err := a.retimeGuards(ctx, p, s.WorkspaceID, s); err != nil {
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
		Summary: "Delete a schedule no recurring gig uses", DefaultStatus: 204}, func(ctx context.Context, in *idPath) (*struct{}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		s, err := a.schedule(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if s.Shared {
			if err := sharedScheduleWrite(p); err != nil {
				return nil, err
			}
		} else if err := require(p, s.WorkspaceID, "recurring_gigs", "gigs:write"); err != nil {
			return nil, err
		}
		return nil, a.tx(ctx, func(tx *sql.Tx) error { return store.DeleteSchedule(ctx, tx, p.Actor(), s) })
	})
}
