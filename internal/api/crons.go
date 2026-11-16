package api

import (
	"context"
	"database/sql"
	"errors"

	"github.com/danielgtaylor/huma/v2"

	"github.com/0x2alx/gigg/internal/access"
	"github.com/0x2alx/gigg/internal/store"
)

// Crons: commands an agent's executor (gigg-cron) runs on a schedule, sandboxed, with a run history
// and no tasks. Reading needs view (crons:read; run output also crons:output); changing needs the
// Crons permission (crons:write); turning the sandbox off needs No-sandbox crons. The executor claims
// and reports runs with crons:run, for its own crons only.

// CronTemplateBody is a cron template's content.
type CronTemplateBody struct {
	Name          string `json:"name" minLength:"1" maxLength:"80"`
	DescriptionMD string `json:"description_md,omitempty" maxLength:"65536"`
	Command       string `json:"command" minLength:"1" maxLength:"16384" doc:"Run by bash, in the worker agent's sandbox unless unsandboxed"`
	TimeoutS      int64  `json:"timeout_s,omitempty" minimum:"0" maximum:"3600" doc:"Seconds (10-3600, default 300)"`
	Output        string `json:"output,omitempty" enum:"gigg,host" doc:"gigg (default): the output's last 64 KiB is kept with each run; host: only the exit code and timing reach gigg"`
	Unsandboxed   bool   `json:"unsandboxed,omitempty" doc:"Run outside the agent's sandbox (needs the No-sandbox crons permission)"`
}

func (b CronTemplateBody) template() *store.CronTemplate {
	return &store.CronTemplate{Name: b.Name, DescriptionMD: b.DescriptionMD, Command: b.Command, TimeoutS: b.TimeoutS, Output: b.Output,
		Unsandboxed: b.Unsandboxed}
}

// CronBody creates a cron: a template (template_id, or its content) and a schedule (schedule_id, or a rule).
type CronBody struct {
	TemplateID    int64         `json:"template_id,omitempty" doc:"An existing cron template; or give name, command, ... for a new one"`
	Name          string        `json:"name,omitempty" maxLength:"80"`
	DescriptionMD string        `json:"description_md,omitempty" maxLength:"65536"`
	Command       string        `json:"command,omitempty" maxLength:"16384"`
	TimeoutS      int64         `json:"timeout_s,omitempty" minimum:"0" maximum:"3600"`
	Output        string        `json:"output,omitempty" enum:"gigg,host"`
	Unsandboxed   bool          `json:"unsandboxed,omitempty"`
	ScheduleID    int64         `json:"schedule_id,omitempty" doc:"An existing schedule; or give a rule"`
	Rule          *ScheduleBody `json:"rule,omitempty" doc:"Timing (a live schedule with the same rule is reused, else one is made): freq minutes, hours, daily, weekly or monthly"`
	Worker        string        `json:"worker,omitempty" doc:"Username of the agent whose executor runs it (default: you). Someone else needs Manage the workspace"`
	OnFailureTask bool          `json:"on_failure_task,omitempty" doc:"The first failure opens a task for the worker"`
	Paused        bool          `json:"paused,omitempty" doc:"Create it paused"`
}

// CronPatch changes a cron; omitted fields stay. Content fields change its template (shared by the
// template's other crons).
type CronPatch struct {
	TemplateID    *int64        `json:"template_id,omitempty"`
	Name          *string       `json:"name,omitempty" maxLength:"80"`
	DescriptionMD *string       `json:"description_md,omitempty" maxLength:"65536"`
	Command       *string       `json:"command,omitempty" maxLength:"16384"`
	TimeoutS      *int64        `json:"timeout_s,omitempty" minimum:"0" maximum:"3600"`
	Output        *string       `json:"output,omitempty" enum:"gigg,host"`
	Unsandboxed   *bool         `json:"unsandboxed,omitempty"`
	ScheduleID    *int64        `json:"schedule_id,omitempty"`
	Rule          *ScheduleBody `json:"rule,omitempty"`
	Worker        *string       `json:"worker,omitempty"`
	OnFailureTask *bool         `json:"on_failure_task,omitempty"`
}

func (b *ScheduleBody) rule() *store.Rule {
	if b == nil {
		return nil
	}
	s := &store.Schedule{}
	b.apply(s)
	return &s.Rule
}

type cronOut struct {
	ETag string `header:"ETag"`
	Body *store.Cron
}

type cronTemplateOut struct {
	ETag string `header:"ETag"`
	Body *store.CronTemplate
}

func (a *API) cronEdit(p *access.Principal, ws int64) store.CronEdit {
	return store.CronEdit{CanUnsandbox: p.Can(ws, "cron_unsandboxed", "crons:write"), CanManage: allowed(p, ws, access.Manager) == nil}
}

func (a *API) cron(ctx context.Context, p *access.Principal, id int64) (*store.Cron, error) {
	c, err := store.GetCron(ctx, a.st.R, id)
	if err != nil || !p.SeesWorkspace(c.WorkspaceID) || p.Narrowed() {
		return nil, errNotFound()
	}
	return c, nil
}

func (a *API) cronTemplate(ctx context.Context, p *access.Principal, id int64) (*store.CronTemplate, error) {
	t, err := store.GetCronTemplate(ctx, a.st.R, id)
	if err != nil || !p.SeesWorkspace(t.WorkspaceID) || p.Narrowed() {
		return nil, errNotFound()
	}
	return t, nil
}

// cronWorker resolves the worker: yourself by default; another user only with Manage the workspace.
// The worker must be an agent holding the Crons permission there.
func (a *API) cronWorker(ctx context.Context, p *access.Principal, ws int64, name string) (int64, error) {
	if name == "" || name == p.User.Username {
		if p.User.Kind != "agent" {
			return 0, fail(422, "invalid", "A cron's worker is an agent: name one in worker.")
		}
		return p.User.ID, nil
	}
	if err := allowed(p, ws, access.Manager); err != nil {
		return 0, errForbidden("Only people with Manage the workspace can give a cron to another agent.")
	}
	u, err := store.GetUserByName(ctx, a.st.R, name)
	if err != nil {
		return 0, fail(422, "invalid", "No such user: "+name+".")
	}
	if u.Kind != "agent" {
		return 0, fail(422, "invalid", "A cron's worker is an agent.")
	}
	if !store.UserHas(ctx, a.st.R, u.ID, ws, "crons") {
		return 0, fail(422, "invalid", name+" doesn't have the Crons permission in this workspace.")
	}
	return u.ID, nil
}

// cronEditGuard: changing a cron needs Crons (crons:write); setting the sandbox off also needs
// No-sandbox crons (the store turns it off when someone without it changes anything).
func cronEditGuard(p *access.Principal, ws int64, unsandboxOn bool) error {
	if err := require(p, ws, "crons", "crons:write"); err != nil {
		return err
	}
	if unsandboxOn {
		return require(p, ws, "cron_unsandboxed", "crons:write")
	}
	return nil
}

func (a *API) registerCrons() {
	type listCrons struct {
		Body struct {
			Items []*store.Cron `json:"items"`
		}
	}
	huma.Register(a.h, huma.Operation{OperationID: "list-crons", Method: "GET", Path: "/workspaces/{ws}/crons", Tags: []string{"crons"},
		Summary: "A workspace's crons: failing first, then by name, each with its last run"}, func(ctx context.Context, in *wsPath) (*listCrons, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		w, err := a.workspace(ctx, p, in.WS)
		if err != nil {
			return nil, err
		}
		if err := require(p, w.ID, access.Viewer, "crons:read"); err != nil {
			return nil, err
		}
		out := &listCrons{}
		out.Body.Items, err = store.ListCrons(ctx, a.st.R, w.ID)
		return out, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "create-cron", Method: "POST", Path: "/workspaces/{ws}/crons", Tags: []string{"crons"},
		Summary: "Make a cron (a template and a schedule, existing or new; the worker is you unless you manage the workspace)", DefaultStatus: 201},
		func(ctx context.Context, in *struct {
			WS   string `path:"ws"`
			Body CronBody
		}) (*cronOut, error) {
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			w, err := a.workspace(ctx, p, in.WS)
			if err != nil {
				return nil, err
			}
			b := in.Body
			if err := cronEditGuard(p, w.ID, b.TemplateID == 0 && b.Unsandboxed); err != nil {
				return nil, err
			}
			worker, err := a.cronWorker(ctx, p, w.ID, b.Worker)
			if err != nil {
				return nil, err
			}
			n := store.NewCron{WorkspaceID: w.ID, WorkerID: worker, TemplateID: b.TemplateID, ScheduleID: b.ScheduleID, OnFailureTask: b.OnFailureTask,
				Paused: b.Paused}
			if b.TemplateID == 0 {
				n.Template = CronTemplateBody{Name: b.Name, DescriptionMD: b.DescriptionMD, Command: b.Command, TimeoutS: b.TimeoutS, Output: b.Output,
					Unsandboxed: b.Unsandboxed}.template()
			}
			if b.Rule != nil && b.Rule.TZ == "" {
				b.Rule.TZ = userTZ(p)
			}
			n.Rule = b.Rule.rule()
			var c *store.Cron
			if err := a.tx(ctx, func(tx *sql.Tx) error {
				var err error
				c, err = store.CreateCron(ctx, tx, p.Actor(), n, a.cronEdit(p, w.ID))
				return err
			}); err != nil {
				return nil, err
			}
			return &cronOut{ETag: etag(c.Version), Body: c}, nil
		})
	huma.Register(a.h, huma.Operation{OperationID: "get-cron", Method: "GET", Path: "/crons/{id}", Tags: []string{"crons"},
		Summary: "A cron"}, func(ctx context.Context, in *idPath) (*cronOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		c, err := a.cron(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if err := require(p, c.WorkspaceID, access.Viewer, "crons:read"); err != nil {
			return nil, err
		}
		return &cronOut{ETag: etag(c.Version), Body: c}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "update-cron", Method: "PATCH", Path: "/crons/{id}", Tags: []string{"crons"},
		Summary: "Change a cron (content fields change its template). Someone without No-sandbox crons changing anything turns its sandbox back on"},
		func(ctx context.Context, in *struct {
			ID      int64  `path:"id"`
			IfMatch string `header:"If-Match"`
			Body    CronPatch
		}) (*cronOut, error) {
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			c, err := a.cron(ctx, p, in.ID)
			if err != nil {
				return nil, err
			}
			b := in.Body
			if err := cronEditGuard(p, c.WorkspaceID, b.Unsandboxed != nil && *b.Unsandboxed && !c.Unsandboxed); err != nil {
				return nil, err
			}
			v, err := ifMatch(in.IfMatch)
			if err != nil {
				return nil, err
			}
			ch := store.CronChange{TemplateID: b.TemplateID, ScheduleID: b.ScheduleID, Name: b.Name, DescriptionMD: b.DescriptionMD,
				Command: b.Command, TimeoutS: b.TimeoutS, Output: b.Output, Unsandboxed: b.Unsandboxed}
			if b.Rule != nil {
				if b.Rule.TZ == "" {
					b.Rule.TZ = c.Rule.TZ
				}
				ch.Rule = b.Rule.rule()
			}
			if b.Worker != nil {
				id, err := a.cronWorker(ctx, p, c.WorkspaceID, *b.Worker)
				if err != nil {
					return nil, err
				}
				if id != c.WorkerID {
					// taking a cron over (even for yourself) is giving it to another agent's executor
					if err := allowed(p, c.WorkspaceID, access.Manager); err != nil {
						return nil, errForbidden("Only people with Manage the workspace can change a cron's worker.")
					}
				}
				ch.WorkerID = &id
			}
			if b.OnFailureTask != nil && *b.OnFailureTask != c.OnFailureTask && p.User.ID != c.WorkerID {
				// the failure task lands on the worker's board, made as the worker
				if err := allowed(p, c.WorkspaceID, access.Manager); err != nil {
					return nil, errForbidden("Only the cron's worker or someone with Manage the workspace can change its failure task.")
				}
			}
			ch.OnFailureTask = b.OnFailureTask
			if err := a.tx(ctx, func(tx *sql.Tx) error {
				var err error
				c, err = store.UpdateCron(ctx, tx, p.Actor(), c.ID, v, ch, a.cronEdit(p, c.WorkspaceID))
				return err
			}); err != nil {
				return nil, err
			}
			return &cronOut{ETag: etag(c.Version), Body: c}, nil
		})
	huma.Register(a.h, huma.Operation{OperationID: "delete-cron", Method: "DELETE", Path: "/crons/{id}", Tags: []string{"crons"},
		Summary: "Delete a cron (its template and schedule stay)", DefaultStatus: 204}, func(ctx context.Context, in *idPath) (*struct{}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		c, err := a.cron(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if err := cronEditGuard(p, c.WorkspaceID, false); err != nil {
			return nil, err
		}
		return nil, a.tx(ctx, func(tx *sql.Tx) error { return store.DeleteCron(ctx, tx, p.Actor(), c) })
	})
	for _, act := range []struct{ op, path, summary string }{
		{"pause-cron", "pause", "Pause a cron"},
		{"resume-cron", "resume", "Resume a cron (from its next slot; missed ones aren't caught up)"},
		{"run-cron-now", "run-now", "Run a cron once at its executor's next poll (about a minute), whatever its schedule"},
	} {
		huma.Register(a.h, huma.Operation{OperationID: act.op, Method: "POST", Path: "/crons/{id}/" + act.path, Tags: []string{"crons"},
			Summary: act.summary}, func(ctx context.Context, in *idPath) (*cronOut, error) {
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			c, err := a.cron(ctx, p, in.ID)
			if err != nil {
				return nil, err
			}
			if err := cronEditGuard(p, c.WorkspaceID, false); err != nil {
				return nil, err
			}
			if c.Unsandboxed && act.path != "pause" {
				// starting a no-sandbox command (or letting it start again) is for people who may set one
				if err := require(p, c.WorkspaceID, "cron_unsandboxed", "crons:write"); err != nil {
					return nil, errForbidden("This cron runs without the sandbox: resuming it or running it now needs No-sandbox crons.")
				}
			}
			if err := a.tx(ctx, func(tx *sql.Tx) error {
				var err error
				switch act.path {
				case "pause":
					c, err = store.SetCronPaused(ctx, tx, p.Actor(), c, true)
				case "resume":
					c, err = store.SetCronPaused(ctx, tx, p.Actor(), c, false)
				default:
					c, err = store.RunCronNow(ctx, tx, p.Actor(), c)
				}
				return err
			}); err != nil {
				return nil, err
			}
			return &cronOut{ETag: etag(c.Version), Body: c}, nil
		})
	}
	huma.Register(a.h, huma.Operation{OperationID: "list-cron-runs", Method: "GET", Path: "/crons/{id}/runs", Tags: []string{"crons"},
		Summary: "A cron's runs, newest first (without output; get-cron-run has it)"}, func(ctx context.Context, in *struct {
		ID     int64 `path:"id"`
		Before int64 `query:"before" doc:"Only runs with a smaller id (paging)"`
		Limit  int   `query:"limit" minimum:"0" maximum:"200" doc:"Default 50"`
	}) (*struct {
		Body struct {
			Items []*store.CronRun `json:"items"`
		}
	}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		c, err := a.cron(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if err := require(p, c.WorkspaceID, access.Viewer, "crons:read"); err != nil {
			return nil, err
		}
		out := &struct {
			Body struct {
				Items []*store.CronRun `json:"items"`
			}
		}{}
		out.Body.Items, err = store.ListCronRuns(ctx, a.st.R, c.ID, in.Before, in.Limit)
		return out, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "get-cron-run", Method: "GET", Path: "/cron-runs/{id}", Tags: []string{"crons"},
		Summary: "One cron run, with its output tail when the key has crons:output"}, func(ctx context.Context, in *idPath) (*struct{ Body *store.CronRun }, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		r, err := store.GetCronRun(ctx, a.st.R, in.ID, true)
		if err != nil || !p.SeesWorkspace(r.WorkspaceID) || p.Narrowed() {
			return nil, errNotFound()
		}
		if err := require(p, r.WorkspaceID, access.Viewer, "crons:read"); err != nil {
			return nil, err
		}
		if !p.HasScope("crons:output") {
			r.OutputTail = nil
		}
		return &struct{ Body *store.CronRun }{r}, nil
	})

	// the executor
	huma.Register(a.h, huma.Operation{OperationID: "claim-crons", Method: "POST", Path: "/workspaces/{ws}/crons/claim", Tags: []string{"crons"},
		Summary: "Start due runs of your own crons (the executor polls this every minute); paused: no items"}, func(ctx context.Context, in *struct {
		WS   string `path:"ws"`
		Body *struct {
			Limit int `json:"limit,omitempty" minimum:"0" maximum:"100" doc:"Start at most this many runs (the executor's free slots); the rest stay due. 0: all"`
		}
	}) (*struct {
		Body struct {
			Items  []*store.ClaimedCron `json:"items"`
			Paused bool                 `json:"paused" doc:"The workspace's crons are paused"`
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
		if err := require(p, w.ID, "crons", "crons:run"); err != nil {
			return nil, err
		}
		out := &struct {
			Body struct {
				Items  []*store.ClaimedCron `json:"items"`
				Paused bool                 `json:"paused" doc:"The workspace's crons are paused"`
			}
		}{}
		err = a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			limit := 0
			if in.Body != nil {
				limit = in.Body.Limit
			}
			out.Body.Items, err = store.ClaimCrons(ctx, tx, p.User.ID, w.ID, limit)
			return err
		})
		if errors.Is(err, store.ErrCronsPaused) {
			out.Body.Items, out.Body.Paused, err = []*store.ClaimedCron{}, true, nil
		}
		return out, err
	})
	huma.Register(a.h, huma.Operation{OperationID: "finish-cron-run", Method: "POST", Path: "/cron-runs/{id}/finish", Tags: []string{"crons"},
		Summary: "Report a run's end (the worker's executor only): status, exit code, duration, the output tail unless the cron keeps it on the host"},
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body struct {
				Status      string  `json:"status" enum:"ok,failed,timeout"`
				ExitCode    *int64  `json:"exit_code,omitempty"`
				DurationMS  int64   `json:"duration_ms" minimum:"0"`
				OutputTail  *string `json:"output_tail,omitempty" maxLength:"65536" doc:"The last 64 KiB (bytes) of stdout and stderr"`
				OutputBytes int64   `json:"output_bytes,omitempty" minimum:"0"`
				Error       string  `json:"error,omitempty" maxLength:"1000"`
			}
		}) (*struct{ Body *store.CronOutcome }, error) {
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			r, err := store.GetCronRun(ctx, a.st.R, in.ID, false)
			if err != nil || !p.SeesWorkspace(r.WorkspaceID) {
				return nil, errNotFound()
			}
			if err := require(p, r.WorkspaceID, "crons", "crons:run"); err != nil {
				return nil, err
			}
			if r.WorkerID != p.User.ID {
				return nil, errForbidden("Only the cron's worker reports its runs.")
			}
			b := in.Body
			c, err := store.GetCron(ctx, a.st.R, r.CronID)
			if err == nil && c.Output == "host" {
				b.OutputTail = nil // the cron keeps its output on the host: never store it, whatever was sent
			}
			var o *store.CronOutcome
			err = a.tx(ctx, func(tx *sql.Tx) error {
				var err error
				o, err = store.FinishCronRun(ctx, tx, p.User.ID, in.ID, store.CronFinish{Status: b.Status, ExitCode: b.ExitCode, DurationMS: b.DurationMS,
					OutputTail: b.OutputTail, OutputBytes: b.OutputBytes, Error: b.Error})
				return err
			})
			return &struct{ Body *store.CronOutcome }{o}, err
		})

	// templates
	huma.Register(a.h, huma.Operation{OperationID: "list-cron-templates", Method: "GET", Path: "/workspaces/{ws}/cron-templates", Tags: []string{"crons"},
		Summary: "A workspace's cron templates"}, func(ctx context.Context, in *wsPath) (*struct {
		Body struct {
			Items []*store.CronTemplate `json:"items"`
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
		if err := require(p, w.ID, access.Viewer, "crons:read"); err != nil {
			return nil, err
		}
		out := &struct {
			Body struct {
				Items []*store.CronTemplate `json:"items"`
			}
		}{}
		out.Body.Items, err = store.ListCronTemplates(ctx, a.st.R, w.ID)
		return out, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "create-cron-template", Method: "POST", Path: "/workspaces/{ws}/cron-templates", Tags: []string{"crons"},
		Summary: "Make a cron template", DefaultStatus: 201}, func(ctx context.Context, in *struct {
		WS   string `path:"ws"`
		Body CronTemplateBody
	}) (*cronTemplateOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		w, err := a.workspace(ctx, p, in.WS)
		if err != nil {
			return nil, err
		}
		if err := cronEditGuard(p, w.ID, in.Body.Unsandboxed); err != nil {
			return nil, err
		}
		t := in.Body.template()
		t.WorkspaceID = w.ID
		if err := a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			t, err = store.SaveCronTemplate(ctx, tx, p.Actor(), t, 0, a.cronEdit(p, w.ID))
			return err
		}); err != nil {
			return nil, err
		}
		return &cronTemplateOut{ETag: etag(t.Version), Body: t}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "get-cron-template", Method: "GET", Path: "/cron-templates/{id}", Tags: []string{"crons"},
		Summary: "A cron template"}, func(ctx context.Context, in *idPath) (*cronTemplateOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		t, err := a.cronTemplate(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if err := require(p, t.WorkspaceID, access.Viewer, "crons:read"); err != nil {
			return nil, err
		}
		return &cronTemplateOut{ETag: etag(t.Version), Body: t}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "update-cron-template", Method: "PUT", Path: "/cron-templates/{id}", Tags: []string{"crons"},
		Summary: "Replace a cron template (every cron using it changes). A no-sandbox template: only with No-sandbox crons (403 otherwise; edit a cron instead)"},
		func(ctx context.Context, in *struct {
			ID      int64  `path:"id"`
			IfMatch string `header:"If-Match"`
			Body    CronTemplateBody
		}) (*cronTemplateOut, error) {
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			cur, err := a.cronTemplate(ctx, p, in.ID)
			if err != nil {
				return nil, err
			}
			if err := cronEditGuard(p, cur.WorkspaceID, in.Body.Unsandboxed && !cur.Unsandboxed); err != nil {
				return nil, err
			}
			v, err := ifMatch(in.IfMatch)
			if err != nil {
				return nil, err
			}
			t := in.Body.template()
			t.ID, t.WorkspaceID = cur.ID, cur.WorkspaceID
			if err := a.tx(ctx, func(tx *sql.Tx) error {
				var err error
				t, err = store.SaveCronTemplate(ctx, tx, p.Actor(), t, v, a.cronEdit(p, cur.WorkspaceID))
				return err
			}); err != nil {
				return nil, err
			}
			return &cronTemplateOut{ETag: etag(t.Version), Body: t}, nil
		})
	huma.Register(a.h, huma.Operation{OperationID: "delete-cron-template", Method: "DELETE", Path: "/cron-templates/{id}", Tags: []string{"crons"},
		Summary: "Delete a cron template no cron uses", DefaultStatus: 204}, func(ctx context.Context, in *idPath) (*struct{}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		t, err := a.cronTemplate(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if err := cronEditGuard(p, t.WorkspaceID, false); err != nil {
			return nil, err
		}
		return nil, a.tx(ctx, func(tx *sql.Tx) error { return store.DeleteCronTemplate(ctx, tx, p.Actor(), t) })
	})
}
