package api

import (
	"context"
	"database/sql"
	"errors"

	"github.com/danielgtaylor/huma/v2"

	"github.com/0x2alx/gigg/internal/access"
	"github.com/0x2alx/gigg/internal/store"
)

// Errands: commands an agent's executor (gigg-errand) runs on a schedule, sandboxed, with a run history
// and no gigs. Reading needs view (errands:read; run output also errands:output); changing needs the
// Errands permission (errands:write); turning the sandbox off needs No-sandbox errands. The executor claims
// and reports runs with errands:run, for its own errands only.

// ErrandTemplateBody is an errand template's content.
type ErrandTemplateBody struct {
	Name          string `json:"name" minLength:"1" maxLength:"80"`
	DescriptionMD string `json:"description_md,omitempty" maxLength:"65536"`
	Command       string `json:"command" minLength:"1" maxLength:"16384" doc:"Run by bash, in the worker agent's sandbox unless unsandboxed"`
	TimeoutS      int64  `json:"timeout_s,omitempty" minimum:"0" maximum:"3600" doc:"Seconds (10-3600, default 300)"`
	Output        string `json:"output,omitempty" enum:"gigg,host" doc:"gigg (default): the output's last 64 KiB is kept with each run; host: only the exit code and timing reach gigg"`
	Unsandboxed   bool   `json:"unsandboxed,omitempty" doc:"Run outside the agent's sandbox (needs the No-sandbox errands permission)"`
}

func (b ErrandTemplateBody) template() *store.ErrandTemplate {
	return &store.ErrandTemplate{Name: b.Name, DescriptionMD: b.DescriptionMD, Command: b.Command, TimeoutS: b.TimeoutS, Output: b.Output,
		Unsandboxed: b.Unsandboxed}
}

// ErrandBody creates an errand: a template (template_id, or its content) and a schedule (schedule_id, or a rule).
type ErrandBody struct {
	TemplateID    int64         `json:"template_id,omitempty" doc:"An existing errand template; or give name, command, ... for a new one"`
	Name          string        `json:"name,omitempty" maxLength:"80"`
	DescriptionMD string        `json:"description_md,omitempty" maxLength:"65536"`
	Command       string        `json:"command,omitempty" maxLength:"16384"`
	TimeoutS      int64         `json:"timeout_s,omitempty" minimum:"0" maximum:"3600"`
	Output        string        `json:"output,omitempty" enum:"gigg,host"`
	Unsandboxed   bool          `json:"unsandboxed,omitempty"`
	ScheduleID    int64         `json:"schedule_id,omitempty" doc:"An existing schedule; or give a rule"`
	Rule          *ScheduleBody `json:"rule,omitempty" doc:"Timing (a live schedule with the same rule is reused, else one is made): freq minutes, hours, daily, weekly or monthly"`
	Worker        string        `json:"worker,omitempty" doc:"Username of the agent whose executor runs it (default: you). Someone else needs Manage the workspace"`
	OnFailureGig  bool          `json:"on_failure_gig,omitempty" doc:"The first failure opens a gig for the worker"`
	Paused        bool          `json:"paused,omitempty" doc:"Create it paused"`
}

// ErrandPatch changes an errand; omitted fields stay. Content fields change its template (shared by the
// template's other errands).
type ErrandPatch struct {
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
	OnFailureGig  *bool         `json:"on_failure_gig,omitempty"`
}

func (b *ScheduleBody) rule() *store.Rule {
	if b == nil {
		return nil
	}
	s := &store.Schedule{}
	b.apply(s)
	return &s.Rule
}

type errandOut struct {
	ETag string `header:"ETag"`
	Body *store.Errand
}

type errandTemplateOut struct {
	ETag string `header:"ETag"`
	Body *store.ErrandTemplate
}

func (a *API) errandEdit(p *access.Principal, ws int64) store.ErrandEdit {
	return store.ErrandEdit{CanUnsandbox: p.Can(ws, "errand_unsandboxed", "errands:write"), CanManage: allowed(p, ws, access.Manager) == nil}
}

func (a *API) errand(ctx context.Context, p *access.Principal, id int64) (*store.Errand, error) {
	c, err := store.GetErrand(ctx, a.st.R, id)
	if err != nil || !p.SeesWorkspace(c.WorkspaceID) || p.Narrowed() {
		return nil, errNotFound()
	}
	return c, nil
}

func (a *API) errandTemplate(ctx context.Context, p *access.Principal, id int64) (*store.ErrandTemplate, error) {
	t, err := store.GetErrandTemplate(ctx, a.st.R, id)
	if err != nil || (t.Shared && !seesShared(p)) || (!t.Shared && !p.SeesWorkspace(t.WorkspaceID)) || p.Narrowed() {
		return nil, errNotFound()
	}
	return t, nil
}

// sharedErrandEdit: a shared errand template is saved by an instance admin (who holds every permission,
// No-sandbox errands included) whose key has errands:write.
func sharedErrandEdit(p *access.Principal) (store.ErrandEdit, error) {
	if err := sharedWrite(p, "errands:write"); err != nil {
		return store.ErrandEdit{}, err
	}
	return store.ErrandEdit{CanUnsandbox: true, CanManage: true}, nil
}

// errandWorker resolves the worker: yourself by default; another user only with Manage the workspace.
// The worker must be an agent that can see the workspace (running errands needs no Errands permission).
func (a *API) errandWorker(ctx context.Context, p *access.Principal, ws int64, name string) (int64, error) {
	if name == "" || name == p.User.Username {
		if p.User.Kind != "agent" {
			return 0, fail(422, "invalid", "An errand's worker is an agent: name one in worker.")
		}
		return p.User.ID, nil
	}
	if err := allowed(p, ws, access.Manager); err != nil {
		return 0, errForbidden("Only people with Manage the workspace can give an errand to another agent.")
	}
	u, err := store.GetUserByName(ctx, a.st.R, name)
	if err != nil {
		return 0, fail(422, "invalid", "No such user: "+name+".")
	}
	if u.Kind != "agent" {
		return 0, fail(422, "invalid", "An errand's worker is an agent.")
	}
	if !store.UserHas(ctx, a.st.R, u.ID, ws, "view") { // it runs them, it needn't manage them
		return 0, fail(422, "invalid", name+" can't see this workspace.")
	}
	return u.ID, nil
}

// errandEditGuard: changing an errand needs Errands (errands:write); setting the sandbox off also needs
// No-sandbox errands (the store turns it off when someone without it changes anything).
func errandEditGuard(p *access.Principal, ws int64, unsandboxOn bool) error {
	if err := require(p, ws, "errands", "errands:write"); err != nil {
		return err
	}
	if unsandboxOn {
		return require(p, ws, "errand_unsandboxed", "errands:write")
	}
	return nil
}

func (a *API) registerErrands() {
	type listErrands struct {
		Body struct {
			Items []*store.Errand `json:"items"`
		}
	}
	huma.Register(a.h, huma.Operation{OperationID: "list-errands", Method: "GET", Path: "/workspaces/{ws}/errands", Tags: []string{"errands"},
		Summary: "A workspace's errands: failing first, then by name, each with its last run"}, func(ctx context.Context, in *wsPath) (*listErrands, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		w, err := a.workspace(ctx, p, in.WS)
		if err != nil {
			return nil, err
		}
		if err := require(p, w.ID, access.Viewer, "errands:read"); err != nil {
			return nil, err
		}
		out := &listErrands{}
		out.Body.Items, err = store.ListErrands(ctx, a.st.R, w.ID)
		return out, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "create-errand", Method: "POST", Path: "/workspaces/{ws}/errands", Tags: []string{"errands"},
		Summary: "Make an errand (a template and a schedule, existing or new; the worker is you unless you manage the workspace)", DefaultStatus: 201},
		func(ctx context.Context, in *struct {
			WS   string `path:"ws"`
			Body ErrandBody
		}) (*errandOut, error) {
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			w, err := a.workspace(ctx, p, in.WS)
			if err != nil {
				return nil, err
			}
			b := in.Body
			if err := errandEditGuard(p, w.ID, b.TemplateID == 0 && b.Unsandboxed); err != nil {
				return nil, err
			}
			worker, err := a.errandWorker(ctx, p, w.ID, b.Worker)
			if err != nil {
				return nil, err
			}
			n := store.NewErrand{WorkspaceID: w.ID, WorkerID: worker, TemplateID: b.TemplateID, ScheduleID: b.ScheduleID, OnFailureGig: b.OnFailureGig,
				Paused: b.Paused}
			if b.TemplateID == 0 {
				n.Template = ErrandTemplateBody{Name: b.Name, DescriptionMD: b.DescriptionMD, Command: b.Command, TimeoutS: b.TimeoutS, Output: b.Output,
					Unsandboxed: b.Unsandboxed}.template()
			}
			if b.Rule != nil && b.Rule.TZ == "" {
				b.Rule.TZ = userTZ(p)
			}
			n.Rule = b.Rule.rule()
			var c *store.Errand
			if err := a.tx(ctx, func(tx *sql.Tx) error {
				var err error
				c, err = store.CreateErrand(ctx, tx, p.Actor(), n, a.errandEdit(p, w.ID))
				return err
			}); err != nil {
				return nil, err
			}
			return &errandOut{ETag: etag(c.Version), Body: c}, nil
		})
	huma.Register(a.h, huma.Operation{OperationID: "get-errand", Method: "GET", Path: "/errands/{id}", Tags: []string{"errands"},
		Summary: "An errand"}, func(ctx context.Context, in *idPath) (*errandOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		c, err := a.errand(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if err := require(p, c.WorkspaceID, access.Viewer, "errands:read"); err != nil {
			return nil, err
		}
		return &errandOut{ETag: etag(c.Version), Body: c}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "update-errand", Method: "PATCH", Path: "/errands/{id}", Tags: []string{"errands"},
		Summary: "Change an errand (content fields change its template). Someone without No-sandbox errands changing anything turns its sandbox back on"},
		func(ctx context.Context, in *struct {
			ID      int64  `path:"id"`
			IfMatch string `header:"If-Match"`
			Body    ErrandPatch
		}) (*errandOut, error) {
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			c, err := a.errand(ctx, p, in.ID)
			if err != nil {
				return nil, err
			}
			b := in.Body
			if err := errandEditGuard(p, c.WorkspaceID, b.Unsandboxed != nil && *b.Unsandboxed && !c.Unsandboxed); err != nil {
				return nil, err
			}
			v, err := ifMatch(in.IfMatch)
			if err != nil {
				return nil, err
			}
			ch := store.ErrandChange{TemplateID: b.TemplateID, ScheduleID: b.ScheduleID, Name: b.Name, DescriptionMD: b.DescriptionMD,
				Command: b.Command, TimeoutS: b.TimeoutS, Output: b.Output, Unsandboxed: b.Unsandboxed}
			if b.Rule != nil {
				if b.Rule.TZ == "" {
					b.Rule.TZ = c.Rule.TZ
				}
				ch.Rule = b.Rule.rule()
			}
			if b.Worker != nil {
				id, err := a.errandWorker(ctx, p, c.WorkspaceID, *b.Worker)
				if err != nil {
					return nil, err
				}
				if id != c.WorkerID {
					// taking an errand over (even for yourself) is giving it to another agent's executor
					if err := allowed(p, c.WorkspaceID, access.Manager); err != nil {
						return nil, errForbidden("Only people with Manage the workspace can change an errand's worker.")
					}
				}
				ch.WorkerID = &id
			}
			if b.OnFailureGig != nil && *b.OnFailureGig != c.OnFailureGig && p.User.ID != c.WorkerID {
				// the failure gig lands on the worker's board, made as the worker
				if err := allowed(p, c.WorkspaceID, access.Manager); err != nil {
					return nil, errForbidden("Only the errand's worker or someone with Manage the workspace can change its failure gig.")
				}
			}
			ch.OnFailureGig = b.OnFailureGig
			if err := a.tx(ctx, func(tx *sql.Tx) error {
				var err error
				c, err = store.UpdateErrand(ctx, tx, p.Actor(), c.ID, v, ch, a.errandEdit(p, c.WorkspaceID))
				return err
			}); err != nil {
				return nil, err
			}
			return &errandOut{ETag: etag(c.Version), Body: c}, nil
		})
	huma.Register(a.h, huma.Operation{OperationID: "delete-errand", Method: "DELETE", Path: "/errands/{id}", Tags: []string{"errands"},
		Summary: "Delete an errand (its template and schedule stay)", DefaultStatus: 204}, func(ctx context.Context, in *idPath) (*struct{}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		c, err := a.errand(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if err := errandEditGuard(p, c.WorkspaceID, false); err != nil {
			return nil, err
		}
		return nil, a.tx(ctx, func(tx *sql.Tx) error { return store.DeleteErrand(ctx, tx, p.Actor(), c) })
	})
	for _, act := range []struct{ op, path, summary string }{
		{"pause-errand", "pause", "Pause an errand"},
		{"resume-errand", "resume", "Resume an errand (from its next slot; missed ones aren't caught up)"},
		{"run-errand-now", "run-now", "Run an errand once at its executor's next poll (about a minute), whatever its schedule"},
	} {
		huma.Register(a.h, huma.Operation{OperationID: act.op, Method: "POST", Path: "/errands/{id}/" + act.path, Tags: []string{"errands"},
			Summary: act.summary}, func(ctx context.Context, in *idPath) (*errandOut, error) {
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			c, err := a.errand(ctx, p, in.ID)
			if err != nil {
				return nil, err
			}
			if err := errandEditGuard(p, c.WorkspaceID, false); err != nil {
				return nil, err
			}
			if c.Unsandboxed && act.path != "pause" {
				// starting a no-sandbox command (or letting it start again) is for people who may set one
				if err := require(p, c.WorkspaceID, "errand_unsandboxed", "errands:write"); err != nil {
					return nil, errForbidden("This errand runs without the sandbox: resuming it or running it now needs No-sandbox errands.")
				}
			}
			if err := a.tx(ctx, func(tx *sql.Tx) error {
				var err error
				switch act.path {
				case "pause":
					c, err = store.SetErrandPaused(ctx, tx, p.Actor(), c, true)
				case "resume":
					c, err = store.SetErrandPaused(ctx, tx, p.Actor(), c, false)
				default:
					c, err = store.RunErrandNow(ctx, tx, p.Actor(), c)
				}
				return err
			}); err != nil {
				return nil, err
			}
			return &errandOut{ETag: etag(c.Version), Body: c}, nil
		})
	}
	huma.Register(a.h, huma.Operation{OperationID: "list-errand-runs", Method: "GET", Path: "/errands/{id}/runs", Tags: []string{"errands"},
		Summary: "An errand's runs, newest first (without output; get-errand-run has it)"}, func(ctx context.Context, in *struct {
		ID     int64 `path:"id"`
		Before int64 `query:"before" doc:"Only runs with a smaller id (paging)"`
		Limit  int   `query:"limit" minimum:"0" maximum:"200" doc:"Default 50"`
	}) (*struct {
		Body struct {
			Items []*store.ErrandRun `json:"items"`
		}
	}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		c, err := a.errand(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if err := require(p, c.WorkspaceID, access.Viewer, "errands:read"); err != nil {
			return nil, err
		}
		out := &struct {
			Body struct {
				Items []*store.ErrandRun `json:"items"`
			}
		}{}
		out.Body.Items, err = store.ListErrandRuns(ctx, a.st.R, c.ID, in.Before, in.Limit)
		return out, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "get-errand-run", Method: "GET", Path: "/errand-runs/{id}", Tags: []string{"errands"},
		Summary: "One errand run, with its output tail when the key has errands:output"}, func(ctx context.Context, in *idPath) (*struct{ Body *store.ErrandRun }, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		r, err := store.GetErrandRun(ctx, a.st.R, in.ID, true)
		if err != nil || !p.SeesWorkspace(r.WorkspaceID) || p.Narrowed() {
			return nil, errNotFound()
		}
		if err := require(p, r.WorkspaceID, access.Viewer, "errands:read"); err != nil {
			return nil, err
		}
		if !p.HasScope("errands:output") {
			r.OutputTail = nil
		}
		return &struct{ Body *store.ErrandRun }{r}, nil
	})

	// the executor
	huma.Register(a.h, huma.Operation{OperationID: "claim-errands", Method: "POST", Path: "/workspaces/{ws}/errands/claim", Tags: []string{"errands"},
		Summary: "Start due runs of your own errands (the executor polls this every minute); paused: no items"}, func(ctx context.Context, in *struct {
		WS   string `path:"ws"`
		Body *struct {
			Limit int `json:"limit,omitempty" minimum:"0" maximum:"100" doc:"Start at most this many runs (the executor's free slots); the rest stay due. 0: all"`
		}
	}) (*struct {
		Body struct {
			Items  []*store.ClaimedErrand `json:"items"`
			Paused bool                   `json:"paused" doc:"The workspace's errands are paused"`
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
		if err := require(p, w.ID, access.Viewer, "errands:run"); err != nil { // running your own errands isn't managing them: a worker without the Errands permission still runs its own
			return nil, err
		}
		out := &struct {
			Body struct {
				Items  []*store.ClaimedErrand `json:"items"`
				Paused bool                   `json:"paused" doc:"The workspace's errands are paused"`
			}
		}{}
		err = a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			limit := 0
			if in.Body != nil {
				limit = in.Body.Limit
			}
			out.Body.Items, err = store.ClaimErrands(ctx, tx, p.User.ID, w.ID, limit)
			return err
		})
		if errors.Is(err, store.ErrErrandsPaused) {
			out.Body.Items, out.Body.Paused, err = []*store.ClaimedErrand{}, true, nil
		}
		return out, err
	})
	huma.Register(a.h, huma.Operation{OperationID: "finish-errand-run", Method: "POST", Path: "/errand-runs/{id}/finish", Tags: []string{"errands"},
		Summary: "Report a run's end (the worker's executor only): status, exit code, duration, the output tail unless the errand keeps it on the host"},
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
		}) (*struct{ Body *store.ErrandOutcome }, error) {
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			r, err := store.GetErrandRun(ctx, a.st.R, in.ID, false)
			if err != nil || !p.SeesWorkspace(r.WorkspaceID) {
				return nil, errNotFound()
			}
			if err := require(p, r.WorkspaceID, access.Viewer, "errands:run"); err != nil {
				return nil, err
			}
			if r.WorkerID != p.User.ID {
				return nil, errForbidden("Only the errand's worker reports its runs.")
			}
			b := in.Body
			c, err := store.GetErrand(ctx, a.st.R, r.ErrandID)
			if err == nil && c.Output == "host" {
				b.OutputTail = nil // the errand keeps its output on the host: never store it, whatever was sent
			}
			var o *store.ErrandOutcome
			err = a.tx(ctx, func(tx *sql.Tx) error {
				var err error
				o, err = store.FinishErrandRun(ctx, tx, p.User.ID, in.ID, store.ErrandFinish{Status: b.Status, ExitCode: b.ExitCode, DurationMS: b.DurationMS,
					OutputTail: b.OutputTail, OutputBytes: b.OutputBytes, Error: b.Error})
				return err
			})
			return &struct{ Body *store.ErrandOutcome }{o}, err
		})

	// templates
	huma.Register(a.h, huma.Operation{OperationID: "list-errand-templates", Method: "GET", Path: "/workspaces/{ws}/errand-templates", Tags: []string{"errands"},
		Summary: "A workspace's errand templates"}, func(ctx context.Context, in *wsPath) (*struct {
		Body struct {
			Items []*store.ErrandTemplate `json:"items"`
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
		if err := require(p, w.ID, access.Viewer, "errands:read"); err != nil {
			return nil, err
		}
		out := &struct {
			Body struct {
				Items []*store.ErrandTemplate `json:"items"`
			}
		}{}
		out.Body.Items, err = store.ListErrandTemplates(ctx, a.st.R, w.ID)
		return out, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "create-errand-template", Method: "POST", Path: "/workspaces/{ws}/errand-templates", Tags: []string{"errands"},
		Summary: "Make an errand template", DefaultStatus: 201}, func(ctx context.Context, in *struct {
		WS   string `path:"ws"`
		Body ErrandTemplateBody
	}) (*errandTemplateOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		w, err := a.workspace(ctx, p, in.WS)
		if err != nil {
			return nil, err
		}
		if err := errandEditGuard(p, w.ID, in.Body.Unsandboxed); err != nil {
			return nil, err
		}
		t := in.Body.template()
		t.WorkspaceID = w.ID
		if err := a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			t, err = store.SaveErrandTemplate(ctx, tx, p.Actor(), t, 0, a.errandEdit(p, w.ID))
			return err
		}); err != nil {
			return nil, err
		}
		return &errandTemplateOut{ETag: etag(t.Version), Body: t}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "list-shared-errand-templates", Method: "GET", Path: "/shared/errand-templates", Tags: []string{"errands"},
		Summary: "The shared errand templates (every workspace can use them)"}, func(ctx context.Context, _ *struct{}) (*struct {
		Body struct {
			Items []*store.ErrandTemplate `json:"items"`
		}
	}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		out := &struct {
			Body struct {
				Items []*store.ErrandTemplate `json:"items"`
			}
		}{}
		out.Body.Items = []*store.ErrandTemplate{}
		if !seesShared(p) || p.Narrowed() {
			return out, nil
		}
		if err := need(p, "errands:read"); err != nil {
			return nil, err
		}
		ts, err := store.ListErrandTemplates(ctx, a.st.R, 0)
		if ts != nil {
			out.Body.Items = ts
		}
		return out, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "create-shared-errand-template", Method: "POST", Path: "/shared/errand-templates", Tags: []string{"errands"},
		Summary: "Make a shared errand template (instance admins)", DefaultStatus: 201}, func(ctx context.Context, in *struct {
		Body ErrandTemplateBody
	}) (*errandTemplateOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		e, err := sharedErrandEdit(p)
		if err != nil {
			return nil, err
		}
		t := in.Body.template()
		t.WorkspaceID = 0
		if err := a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			t, err = store.SaveErrandTemplate(ctx, tx, p.Actor(), t, 0, e)
			return err
		}); err != nil {
			return nil, err
		}
		return &errandTemplateOut{ETag: etag(t.Version), Body: t}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "get-errand-template", Method: "GET", Path: "/errand-templates/{id}", Tags: []string{"errands"},
		Summary: "An errand template"}, func(ctx context.Context, in *idPath) (*errandTemplateOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		t, err := a.errandTemplate(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if err := readIn(p, t.WorkspaceID, "errands:read"); err != nil {
			return nil, err
		}
		return &errandTemplateOut{ETag: etag(t.Version), Body: t}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "update-errand-template", Method: "PUT", Path: "/errand-templates/{id}", Tags: []string{"errands"},
		Summary: "Replace an errand template (every errand using it changes). A no-sandbox template: only with No-sandbox errands (403 otherwise; edit an errand instead)"},
		func(ctx context.Context, in *struct {
			ID      int64  `path:"id"`
			IfMatch string `header:"If-Match"`
			Body    ErrandTemplateBody
		}) (*errandTemplateOut, error) {
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			cur, err := a.errandTemplate(ctx, p, in.ID)
			if err != nil {
				return nil, err
			}
			var e store.ErrandEdit
			if cur.Shared {
				if e, err = sharedErrandEdit(p); err != nil {
					return nil, err
				}
			} else {
				if err := errandEditGuard(p, cur.WorkspaceID, in.Body.Unsandboxed && !cur.Unsandboxed); err != nil {
					return nil, err
				}
				e = a.errandEdit(p, cur.WorkspaceID)
			}
			v, err := ifMatch(in.IfMatch)
			if err != nil {
				return nil, err
			}
			t := in.Body.template()
			t.ID, t.WorkspaceID = cur.ID, cur.WorkspaceID
			if err := a.tx(ctx, func(tx *sql.Tx) error {
				var err error
				t, err = store.SaveErrandTemplate(ctx, tx, p.Actor(), t, v, e)
				return err
			}); err != nil {
				return nil, err
			}
			return &errandTemplateOut{ETag: etag(t.Version), Body: t}, nil
		})
	huma.Register(a.h, huma.Operation{OperationID: "delete-errand-template", Method: "DELETE", Path: "/errand-templates/{id}", Tags: []string{"errands"},
		Summary: "Delete an errand template no errand uses", DefaultStatus: 204}, func(ctx context.Context, in *idPath) (*struct{}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		t, err := a.errandTemplate(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if t.Shared {
			if _, err := sharedErrandEdit(p); err != nil {
				return nil, err
			}
		} else if err := errandEditGuard(p, t.WorkspaceID, false); err != nil {
			return nil, err
		}
		return nil, a.tx(ctx, func(tx *sql.Tx) error { return store.DeleteErrandTemplate(ctx, tx, p.Actor(), t) })
	})
}
