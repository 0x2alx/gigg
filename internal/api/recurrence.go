package api

import (
	"context"
	"database/sql"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/0x2alx/gigg/internal/access"
	"github.com/0x2alx/gigg/internal/store"
)

// RecurrenceOut is a recurring gig with its next occurrences (window starts, unix ms).
type RecurrenceOut struct {
	*store.Recurrence
	Upcoming []int64 `json:"upcoming"`
}

// RecurrenceBody creates or replaces a recurring gig: a template (template_id, or the gig content
// here) paired with a schedule (schedule_id, or the timing here), plus its own settings.
type RecurrenceBody struct {
	TemplateID    *int64                 `json:"template_id,omitempty" doc:"Make each gig from this template (this workspace's, for this project or any, or a shared one); the content fields are then ignored. On update, another id switches to that template; without it, content changes are written to the current template (every recurring gig using it follows)."`
	ScheduleID    *int64                 `json:"schedule_id,omitempty" doc:"Use this schedule (this workspace's or a shared one); the timing fields are then ignored. On update, another id switches to it; without it, timing changes are written to the current schedule (every recurring gig on it moves). On create without it, a schedule with exactly this timing is reused or made."`
	Title         string                 `json:"title,omitempty" maxLength:"250" doc:"Gig title (required unless template_id is given)"`
	Checklist     []string               `json:"checklist,omitempty" maxItems:"200" doc:"Checklist items each gig gets; omit to keep the template's, [] clears"`
	Subgigs       []store.TemplateSubgig `json:"subgigs,omitempty" maxItems:"50" doc:"Subgigs each gig gets (title; blocks_parent = the gig waits for it); omit to keep the template's, [] clears"`
	Custom        map[string]any         `json:"custom,omitempty" doc:"Custom field values (field id -> value) each gig gets; omit to keep the template's, {} clears"`
	DescriptionMD string                 `json:"description_md,omitempty" maxLength:"65536"`
	Priority      int64                  `json:"priority,omitempty" minimum:"0" maximum:"4"`
	Color         *string                `json:"color,omitempty"`
	Labels        []string               `json:"labels,omitempty" maxItems:"20"`
	Assignees     []int64                `json:"assignees,omitempty" maxItems:"50" doc:"Who its gigs go to. With a shared template (also when template_id names one), the recurring gig's own assignees"`
	Followers     []int64                `json:"followers,omitempty" maxItems:"50" doc:"Who follows each of its gigs (kept in its template); omit to keep, [] clears"`
	AutoArchive   *int64                 `json:"auto_archive_minutes,omitempty" minimum:"0" maximum:"527040" doc:"Its gigs archive themselves this many minutes after done or cancelled (kept in its template); omit to keep, 0 clears"`
	RunTimeout    *int64                 `json:"run_timeout_minutes,omitempty" minimum:"0" maximum:"300" doc:"Its gigs' run timeout in minutes (kept in its template); omit to keep, 0 clears (the agent's default). Needs the run_timeout permission"`
	Model         string                 `json:"model,omitempty" maxLength:"64" doc:"Model a runner should ask for (default: the agent's)"`
	Effort        string                 `json:"effort,omitempty" enum:"low,medium,high,xhigh,max," doc:"Effort level a runner should ask for (default: the agent's)"`
	AlwaysRun     bool                   `json:"always_run,omitempty" doc:"Its gigs run whatever the usage budget (needs the always_run permission)"`
	AllowedTools  []string               `json:"allowed_tools,omitempty" maxItems:"64" doc:"Tool allowlist its gigs get for the runner (permission rules); omit to keep the template's, [] clears. Needs the allowed_tools permission"`
	Freq          string                 `json:"freq,omitempty" enum:"daily,weekly,monthly," doc:"Required unless schedule_id is given"`
	Interval      int64                  `json:"interval,omitempty" minimum:"0" maximum:"365" doc:"Every N days/weeks/months (default 1)"`
	Weekdays      []int                  `json:"weekdays,omitempty" doc:"Weekly: 1=Mon .. 7=Sun"`
	MonthDay      *int64                 `json:"month_day,omitempty" doc:"Monthly: 1-31, -1 = last day (short months use their last day)"`
	TZ            string                 `json:"tz,omitempty" doc:"IANA time zone (default: the user's time zone preference, else the instance default GIGG_DEFAULT_TZ)"`
	WindowStart   string                 `json:"window_start,omitempty" pattern:"^[0-2][0-9]:[0-5][0-9]$" doc:"When the window opens, local HH:MM (default 00:00)"`
	WindowMinutes int64                  `json:"window_minutes,omitempty" minimum:"0" maximum:"44640" doc:"How long the window stays open (default 1440)"`
	LeadMinutes   int64                  `json:"lead_minutes,omitempty" minimum:"0" maximum:"10080" doc:"Create the gig this long before the window opens"`
	StartsOn      int64                  `json:"starts_on,omitempty" doc:"Unix ms (default now)"`
	EndsOn        *int64                 `json:"ends_on,omitempty" doc:"Unix ms"`
	MaxCount      *int64                 `json:"max_count,omitempty" minimum:"1"`
	Overlap       string                 `json:"overlap,omitempty" enum:"skip,create," doc:"When the previous occurrence is still open: skip (default) or create anyway"`
	Paused        bool                   `json:"paused,omitempty"`
}

// apply copies the body: content unless a template is named, timing unless a schedule is named, and
// always the recurring gig's own settings.
func (b RecurrenceBody) apply(r *store.Recurrence) {
	if b.TemplateID == nil {
		r.Title, r.DescriptionMD, r.Priority, r.Color = b.Title, b.DescriptionMD, b.Priority, b.Color
		r.Labels, r.Assignees, r.Model, r.Effort, r.AlwaysRun = b.Labels, b.Assignees, b.Model, b.Effort, b.AlwaysRun
		r.Checklist, r.Custom, r.Subgigs = b.Checklist, b.Custom, b.Subgigs
		if b.AllowedTools != nil {
			r.AllowedTools = b.AllowedTools
		}
		if b.Followers != nil {
			r.Followers = b.Followers
		}
		if b.AutoArchive != nil {
			r.AutoArchive = b.AutoArchive
			if *b.AutoArchive == 0 {
				r.AutoArchive = nil
			}
		}
		if b.RunTimeout != nil {
			r.RunTimeout = b.RunTimeout
			if *b.RunTimeout == 0 {
				r.RunTimeout = nil
			}
		}
	}
	r.OwnAssignees = b.Assignees
	if b.ScheduleID == nil {
		r.SetRule(store.Rule{Freq: b.Freq, Interval: b.Interval, Weekdays: b.Weekdays, MonthDay: b.MonthDay, TZ: b.TZ, WindowStart: b.WindowStart,
			WindowMinutes: b.WindowMinutes, LeadMinutes: b.LeadMinutes})
	}
	r.EndsOn, r.MaxCount, r.Overlap, r.Paused = b.EndsOn, b.MaxCount, b.Overlap, b.Paused
	if b.StartsOn != 0 {
		r.StartsOn = b.StartsOn
	}
}

type recurrenceOut struct {
	ETag string `header:"ETag"`
	Body *RecurrenceOut
}

func (a *API) recurrenceOut(r *store.Recurrence) *recurrenceOut {
	return &recurrenceOut{ETag: etag(r.Version), Body: &RecurrenceOut{Recurrence: r, Upcoming: r.Upcoming(store.Now(), 5)}}
}

func (a *API) recurrence(ctx context.Context, p *access.Principal, id int64) (*store.Recurrence, error) {
	r, err := store.GetRecurrence(ctx, a.st.R, id)
	if err != nil || !p.SeesProject(r.WorkspaceID, r.ProjectID) {
		return nil, errNotFound()
	}
	return r, nil
}

func (a *API) registerRecurrence() {
	huma.Register(a.h, huma.Operation{OperationID: "list-recurrences", Method: "GET", Path: "/workspaces/{ws}/recurrences", Tags: []string{"recurrence"},
		Summary: "Recurring gigs in a workspace (each pairs a template with a schedule)"}, func(ctx context.Context, in *struct {
		WS      string `path:"ws"`
		Project int64  `query:"project"`
	}) (*struct {
		Body struct {
			Items []*RecurrenceOut `json:"items"`
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
		if err := requireScope(p, "gigs:read"); err != nil {
			return nil, err
		}
		projects := p.ProjectFilter(w.ID)
		if in.Project != 0 {
			if !p.SeesProject(w.ID, in.Project) {
				return nil, errNotFound()
			}
			projects = []int64{in.Project}
		}
		rs, err := store.ListRecurrences(ctx, a.st.R, w.ID, projects)
		if err != nil {
			return nil, err
		}
		out := &struct {
			Body struct {
				Items []*RecurrenceOut `json:"items"`
			}
		}{}
		out.Body.Items = []*RecurrenceOut{}
		for _, r := range rs {
			out.Body.Items = append(out.Body.Items, &RecurrenceOut{Recurrence: r, Upcoming: r.Upcoming(store.Now(), 3)})
		}
		return out, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "create-recurrence", Method: "POST", Path: "/projects/{p}/recurrences", Tags: []string{"recurrence"},
		Summary: "Create a recurring gig: a template (or content) on a schedule (or timing); gigg creates a gig per occurrence, with its time window", DefaultStatus: 201},
		func(ctx context.Context, in *struct {
			P    int64 `path:"p"`
			Body RecurrenceBody
		}) (*recurrenceOut, error) {
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			pr, err := a.project(ctx, p, in.P)
			if err != nil {
				return nil, err
			}
			if err := requireIn(p, pr.WorkspaceID, pr.ID, access.Editor, "gigs:write"); err != nil {
				return nil, err
			}
			if err := allowed(p, pr.WorkspaceID, "recurring_gigs"); err != nil {
				return nil, err
			}
			r := &store.Recurrence{ProjectID: pr.ID}
			if in.Body.TZ == "" {
				in.Body.TZ = userTZ(p)
			}
			in.Body.apply(r)
			always, tools, timeout := in.Body.AlwaysRun, in.Body.AllowedTools, r.RunTimeout
			if in.Body.TemplateID != nil {
				tpl, err := store.RecurrenceTemplate(ctx, a.st.R, pr.WorkspaceID, pr.ID, *in.Body.TemplateID)
				if err != nil {
					return nil, storeErr(err)
				}
				r.TemplateID, always, tools, timeout = tpl.ID, tpl.AlwaysRun, tpl.AllowedTools, tpl.RunTimeout
			} else if strings.TrimSpace(in.Body.Title) == "" {
				return nil, fail(422, "invalid", "A recurring gig needs a title (or a template_id).")
			}
			if in.Body.ScheduleID != nil {
				r.ScheduleID = *in.Body.ScheduleID
			} else if in.Body.Freq == "" {
				return nil, fail(422, "invalid", "A recurring gig needs freq (or a schedule_id).")
			}
			if err := alwaysRunChange(p, pr.WorkspaceID, false, always); err != nil {
				return nil, err
			}
			if err := toolsChange(p, pr.WorkspaceID, nil, tools); err != nil {
				return nil, err
			}
			if err := timeoutChange(p, pr.WorkspaceID, nil, timeout); err != nil {
				return nil, err
			}
			err = a.tx(ctx, func(tx *sql.Tx) error {
				var err error
				r, err = store.CreateRecurrence(ctx, tx, p.Actor(), r)
				return err
			})
			if err != nil {
				return nil, err
			}
			// create the first occurrence right away if its window is already due
			if res, err := a.st.Housekeeping(ctx, store.Now()); err != nil {
				a.log.Error("housekeeping after create", "err", err)
			} else {
				for _, e := range res.Errors {
					a.log.Error("housekeeping after create", "err", e)
				}
			}
			if r, err = store.GetRecurrence(ctx, a.st.R, r.ID); err != nil {
				return nil, storeErr(err)
			}
			return a.recurrenceOut(r), nil
		})
	huma.Register(a.h, huma.Operation{OperationID: "get-recurrence", Method: "GET", Path: "/recurrences/{id}", Tags: []string{"recurrence"},
		Summary: "A recurring gig with its next five occurrences"}, func(ctx context.Context, in *idPath) (*recurrenceOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		r, err := a.recurrence(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if err := requireIn(p, r.WorkspaceID, r.ProjectID, access.Viewer, "gigs:read"); err != nil {
			return nil, err
		}
		return a.recurrenceOut(r), nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "update-recurrence", Method: "PUT", Path: "/recurrences/{id}", Tags: []string{"recurrence"},
		Summary: "Replace a recurring gig: its own settings, and its template and schedule (switch with template_id/schedule_id, or edit them in place); the next occurrence is recomputed from now"}, func(ctx context.Context, in *struct {
		ID      int64  `path:"id"`
		IfMatch string `header:"If-Match"`
		Body    RecurrenceBody
	}) (*recurrenceOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		r, err := a.recurrence(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if err := requireIn(p, r.WorkspaceID, r.ProjectID, access.Editor, "gigs:write"); err != nil {
			return nil, err
		}
		if err := allowed(p, r.WorkspaceID, "recurring_gigs"); err != nil {
			return nil, err
		}
		// an edit makes the editor its owner, whose permissions the scheduler then creates gigs with
		if err := allowedIn(p, r.WorkspaceID, r.ProjectID, "create_gigs"); err != nil {
			return nil, err
		}
		v, err := ifMatch(in.IfMatch)
		if err != nil {
			return nil, err
		}
		old := *r
		var ch store.RecurrenceChange
		always := r.AlwaysRun || in.Body.AlwaysRun
		tools := len(r.AllowedTools) > 0 || len(in.Body.AllowedTools) > 0
		if in.Body.TemplateID != nil && *in.Body.TemplateID != r.TemplateID {
			tpl, err := store.RecurrenceTemplate(ctx, a.st.R, r.WorkspaceID, r.ProjectID, *in.Body.TemplateID)
			if err != nil {
				return nil, storeErr(err)
			}
			ch.Template, always = tpl.ID, r.AlwaysRun || tpl.AlwaysRun
			tools = tools || len(tpl.AllowedTools) > 0
		}
		if in.Body.ScheduleID != nil && *in.Body.ScheduleID != r.ScheduleID {
			ch.Schedule = *in.Body.ScheduleID
		}
		if err := alwaysRunGuard(p, r.WorkspaceID, always); err != nil {
			return nil, err
		}
		if err := toolsGuard(p, r.WorkspaceID, tools); err != nil {
			return nil, err
		}
		if in.Body.TZ == "" {
			in.Body.TZ = r.TZ // an edit that names no zone keeps the current one
		}
		in.Body.apply(r)
		newTimeout := r.RunTimeout
		if ch.Template != 0 {
			tpl, err := store.GetTemplate(ctx, a.st.R, ch.Template)
			if err != nil {
				return nil, storeErr(err)
			}
			newTimeout = tpl.RunTimeout
		}
		if err := timeoutChange(p, r.WorkspaceID, old.RunTimeout, newTimeout); err != nil {
			return nil, err
		}
		if in.Body.TemplateID == nil {
			if strings.TrimSpace(r.Title) == "" {
				return nil, fail(422, "invalid", "A recurring gig needs a title.")
			}
			if !store.SameContent(&old, r) && !old.TemplateShared { // a shared one isn't changed: this recurring gig gets a copy
				// the content lives in a template: someone else's needs Gig templates to change, and a
				// key limited to some projects can't change one for every project
				if old.TemplateBy != p.User.ID {
					if err := allowed(p, r.WorkspaceID, "templates"); err != nil {
						return nil, err
					}
				}
				if p.Narrowed(r.WorkspaceID) {
					if t, err := store.GetTemplate(ctx, a.st.R, old.TemplateID); err != nil || t.ProjectID == nil {
						return nil, errForbidden("A key limited to some projects only changes templates of those projects.")
					}
				}
			}
		}
		if in.Body.ScheduleID == nil {
			if in.Body.Freq == "" {
				return nil, fail(422, "invalid", "A recurring gig needs freq (or a schedule_id).")
			}
			if rule := r.Rule(); rule.Validate() == nil && !rule.Same(old.Rule()) && !old.ScheduleShared { // shared: it moves to its own
				if p.Narrowed(r.WorkspaceID) && a.scheduleUsedOutside(ctx, p, r.WorkspaceID, old.ScheduleID) {
					return nil, errForbidden("This schedule is used outside the projects this key reaches; pick or make another one.")
				}
				// new timing retimes the shared schedule, so every recurring gig on it: the same guards as
				// PUT /schedules/{id} (an always-run or tool-allowlist one on it needs those permissions)
				always, tools, err := store.ScheduleGuards(ctx, a.st.R, old.ScheduleID)
				if err != nil {
					return nil, err
				}
				if err := alwaysRunGuard(p, r.WorkspaceID, always); err != nil {
					return nil, err
				}
				if err := toolsGuard(p, r.WorkspaceID, tools); err != nil {
					return nil, err
				}
			}
		}
		err = a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			r, err = store.UpdateRecurrence(ctx, tx, p.Actor(), r, v, ch)
			return err
		})
		if err != nil {
			return nil, err
		}
		return a.recurrenceOut(r), nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "delete-recurrence", Method: "DELETE", Path: "/recurrences/{id}", Tags: []string{"recurrence"},
		Summary: "Stop a recurring gig (its gigs stay; its template and schedule too, unless asked)", DefaultStatus: 204,
		Description: "`template=true` / `schedule=true` also delete its template / schedule, refused (409, nothing deleted) when another recurring gig uses it."},
		func(ctx context.Context, in *struct {
			ID       int64 `path:"id"`
			Template bool  `query:"template" doc:"Also delete its template (when no other recurring gig uses it)"`
			Schedule bool  `query:"schedule" doc:"Also delete its schedule (when no other recurring gig uses it)"`
		}) (*struct{}, error) {
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			r, err := a.recurrence(ctx, p, in.ID)
			if err != nil {
				return nil, err
			}
			if err := requireIn(p, r.WorkspaceID, r.ProjectID, access.Editor, "gigs:write"); err != nil {
				return nil, err
			}
			if err := allowed(p, r.WorkspaceID, "recurring_gigs"); err != nil {
				return nil, err
			}
			var tpl *store.Template
			// shared templates and schedules belong to the instance: deleting a recurring gig keeps them
			in.Template = in.Template && !r.TemplateShared
			in.Schedule = in.Schedule && !r.ScheduleShared
			if in.Template {
				// as for changing its content: someone else's template needs Gig templates, and a key
				// limited to some projects can't delete one for every project
				t, err := store.GetTemplate(ctx, a.st.R, r.TemplateID)
				if err != nil {
					return nil, storeErr(err)
				}
				if r.TemplateBy != p.User.ID {
					if err := allowed(p, r.WorkspaceID, "templates"); err != nil {
						return nil, err
					}
				}
				if p.Narrowed(r.WorkspaceID) && t.ProjectID == nil {
					return nil, errForbidden("A key limited to some projects only changes templates of those projects.")
				}
				tpl = t
			}
			return nil, a.tx(ctx, func(tx *sql.Tx) error {
				if err := store.DeleteRecurrence(ctx, tx, p.Actor(), r); err != nil {
					return err
				}
				// each refuses (and the whole delete rolls back) while another recurring gig uses it
				if tpl != nil {
					if err := store.DeleteTemplate(ctx, tx, p.Actor(), tpl); err != nil {
						return err
					}
				}
				if in.Schedule {
					s, err := store.GetSchedule(ctx, tx, r.ScheduleID)
					if err != nil {
						return err
					}
					if err := store.DeleteSchedule(ctx, tx, p.Actor(), s); err != nil {
						return err
					}
				}
				return nil
			})
		})
}
