package api

import (
	"context"
	"database/sql"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/0x2alx/gigg/internal/access"
	"github.com/0x2alx/gigg/internal/store"
)

// Runner controls: per-workspace gig caps, the pause switch, and what agents spent. Reading is for
// anyone who sees the workspace (agents read their own before each pass); caps are for admins;
// pausing for admins and holders of the pause_runs permission.

// RunnerState is one workspace's runner view.
type RunnerState struct {
	WorkspaceID int64                 `json:"workspace_id"`
	Slug        string                `json:"slug"`
	Name        string                `json:"name"`
	Settings    *store.RunnerSettings `json:"settings"`
	Usage       *store.RunnerUsage    `json:"usage"`
	Errands     ErrandCounts          `json:"errands"`
	PauseOnly   bool                  `json:"pause_only,omitempty" doc:"You see only some projects of the workspace: only the pause state is filled in"`
}

// ErrandCounts: a workspace's live errands and how many of them are failing.
type ErrandCounts struct {
	Total   int64 `json:"total"`
	Failing int64 `json:"failing"`
}

type runnerList struct {
	Body struct {
		Items []RunnerState `json:"items"`
	}
}

func (a *API) runnerState(ctx context.Context, w *store.Workspace) (RunnerState, error) {
	st, err := store.GetRunnerSettings(ctx, a.st.R, w.ID)
	if err != nil {
		return RunnerState{}, err
	}
	u, err := store.RunnerUsageOf(ctx, a.st.R, w.ID, store.Now())
	if err != nil {
		return RunnerState{}, err
	}
	var cc ErrandCounts
	if cc.Total, cc.Failing, err = store.CountErrands(ctx, a.st.R, w.ID); err != nil {
		return RunnerState{}, err
	}
	return RunnerState{WorkspaceID: w.ID, Slug: w.Slug, Name: w.Name, Settings: st, Usage: u, Errands: cc}, nil
}

// PauseBody picks workspaces and a duration.
type PauseBody struct {
	Workspaces []int64 `json:"workspaces,omitempty" doc:"Workspace ids; omit for every workspace you may pause"`
	Minutes    int64   `json:"minutes,omitempty" minimum:"0" maximum:"525600" doc:"How long; 0 or omitted = until resumed (gig runs only: errands pause until resumed)"`
	Gigs       *bool   `json:"gigs,omitempty" doc:"The gig box (default true)"`
	Errands    *bool   `json:"errands,omitempty" doc:"The errand box (default true)"`
}

func (a *API) registerRunner() {
	huma.Register(a.h, huma.Operation{OperationID: "runner-status", Method: "GET", Path: "/runner", Tags: []string{"runs"},
		Summary: "Runner caps, pause state and the last 24 hours of agent work, per workspace in reach"}, func(ctx context.Context, _ *struct{}) (*runnerList, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := need(p, "gigs:read"); err != nil {
			return nil, err
		}
		ws, err := store.ListWorkspaces(ctx, a.st.R, p.Workspaces())
		if err != nil {
			return nil, err
		}
		out := &runnerList{}
		out.Body.Items = []RunnerState{}
		for _, w := range ws {
			if !p.SeesWorkspace(w.ID) {
				continue
			}
			s, err := a.runnerStateFor(ctx, p, w)
			if err != nil {
				return nil, err
			}
			out.Body.Items = append(out.Body.Items, s)
		}
		return out, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "workspace-runner", Method: "GET", Path: "/workspaces/{ws}/runner", Tags: []string{"runs"},
		Summary: "One workspace's runner caps, pause state and last 24 hours (runners read it before each pass)"}, func(ctx context.Context, in *wsPath) (*struct{ Body RunnerState }, error) {
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
		s, err := a.runnerStateFor(ctx, p, w)
		return &struct{ Body RunnerState }{s}, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "set-runner-caps", Method: "PATCH", Path: "/workspaces/{ws}/runner", Tags: []string{"runs"},
		Summary: "Change a workspace's gig caps (admin)"}, func(ctx context.Context, in *struct {
		WS   string `path:"ws"`
		Body struct {
			DailyCap *int64 `json:"daily_cap,omitempty" minimum:"0" maximum:"200" doc:"Distinct gigs per rolling 24 hours, any kind"`
			NightCap *int64 `json:"night_cap,omitempty" minimum:"0" maximum:"200" doc:"Of those, budget-dependent (not always-run) gigs"`
		}
	}) (*struct{ Body RunnerState }, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		w, err := a.workspace(ctx, p, in.WS)
		if err != nil {
			return nil, err
		}
		if err := adminWith(p, "admin:workspaces"); err != nil {
			return nil, err
		}
		if err := a.tx(ctx, func(tx *sql.Tx) error {
			_, err := store.SetRunnerCaps(ctx, tx, p.Actor(), w.ID, in.Body.DailyCap, in.Body.NightCap)
			return err
		}); err != nil {
			return nil, err
		}
		s, err := a.runnerStateFor(ctx, p, w)
		return &struct{ Body RunnerState }{s}, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "pause-runner", Method: "POST", Path: "/runner/pause", Tags: []string{"runs"},
		Summary: "Pause new gig runs in some or all workspaces, for a time or until resumed (admins, or the pause_runs permission)"}, func(ctx context.Context, in *struct{ Body PauseBody }) (*runnerList, error) {
		return a.pauseOrResume(ctx, in.Body, true)
	})
	huma.Register(a.h, huma.Operation{OperationID: "resume-runner", Method: "POST", Path: "/runner/resume", Tags: []string{"runs"},
		Summary: "Lift the pause in some or all workspaces (admins, or the pause_runs permission)"}, func(ctx context.Context, in *struct{ Body PauseBody }) (*runnerList, error) {
		return a.pauseOrResume(ctx, in.Body, false)
	})
	huma.Register(a.h, huma.Operation{OperationID: "run-usage", Method: "POST", Path: "/runs/{id}/usage", Tags: []string{"runs"},
		Summary: "Record what a finished run cost on the account's weekly usage meter (the runner measures it; once per run)"},
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body struct {
				UsagePct float64 `json:"usage_pct" minimum:"0" maximum:"100" doc:"Points of the weekly meter (after minus before)"`
			}
		}) (*struct{ Body *store.Run }, error) {
			p, r, project, err := a.run(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			if err := requireIn(p, r.WorkspaceID, project, access.Worker, "runs:write"); err != nil {
				return nil, err
			}
			if r.WorkerUserID != p.User.ID && !p.IsInstanceAdmin() {
				return nil, errForbidden("Only the run's worker records its usage.")
			}
			err = a.tx(ctx, func(tx *sql.Tx) error {
				var err error
				r, err = store.SetRunUsage(ctx, tx, r, in.Body.UsagePct)
				return err
			})
			return &struct{ Body *store.Run }{r}, err
		})
}

// mayPause: admins (with admin:workspaces), or the pause_runs permission in the workspace with a
// key that can write runs.
func mayPause(p *access.Principal, ws int64) bool {
	return p.AdminWith("admin:workspaces") || p.Can(ws, "pause_runs", "runs:write")
}

// pauseOrResume: no list means every workspace the caller may pause; a list must name only
// workspaces the caller sees (else 422, as if they didn't exist) and may pause (else 403).
func (a *API) pauseOrResume(ctx context.Context, b PauseBody, pause bool) (*runnerList, error) {
	p, err := me(ctx)
	if err != nil {
		return nil, err
	}
	ws, err := store.ListWorkspaces(ctx, a.st.R, p.Workspaces())
	if err != nil {
		return nil, err
	}
	all := len(b.Workspaces) == 0
	pick := map[int64]bool{}
	for _, id := range b.Workspaces {
		pick[id] = true
	}
	var chosen []*store.Workspace
	found := 0
	for _, w := range ws {
		if !p.SeesWorkspace(w.ID) && !p.IsInstanceAdmin() {
			continue
		}
		if !all && !pick[w.ID] {
			continue
		}
		found++
		if mayPause(p, w.ID) {
			chosen = append(chosen, w)
		} else if !all {
			return nil, noPauseErr(p)
		}
	}
	if !all && found != len(pick) {
		return nil, fail(422, "invalid", "No such workspace in the list.")
	}
	if len(chosen) == 0 {
		return nil, noPauseErr(p)
	}
	var until *int64
	if b.Minutes > 0 {
		u := store.Now() + b.Minutes*int64(time.Minute/time.Millisecond)
		until = &u
	}
	if err := a.tx(ctx, func(tx *sql.Tx) error {
		gigs, errands := b.Gigs == nil || *b.Gigs, b.Errands == nil || *b.Errands
		for _, w := range chosen {
			var err error
			switch {
			case pause && gigs:
				err = store.PauseRunner(ctx, tx, p.Actor(), w.ID, until)
			case !pause && gigs:
				err = store.ResumeRunner(ctx, tx, p.Actor(), w.ID)
			}
			if err != nil {
				return err
			}
			switch {
			case pause && errands:
				err = store.PauseErrands(ctx, tx, p.Actor(), w.ID)
			case !pause && errands:
				err = store.ResumeErrands(ctx, tx, p.Actor(), w.ID)
			}
			if err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	out := &runnerList{}
	out.Body.Items = []RunnerState{}
	for _, w := range chosen {
		s, err := a.runnerStateFor(ctx, p, w)
		if err != nil {
			return nil, err
		}
		out.Body.Items = append(out.Body.Items, s)
	}
	return out, nil
}

func noPauseErr(p *access.Principal) error {
	if !p.HasScope("runs:write") && !p.AdminWith("admin:workspaces") {
		return errForbidden("This key lacks the runs:write scope.")
	}
	return errNoPermission("pause_runs")
}

// runnerStateFor is runnerState as the caller may read it: someone who sees only some projects of the
// workspace gets whether gig and errand runs are paused (and until when, which runners read before
// each pass), nothing about its caps, usage or errands.
func (a *API) runnerStateFor(ctx context.Context, p *access.Principal, w *store.Workspace) (RunnerState, error) {
	s, err := a.runnerState(ctx, w)
	if err != nil || p.ProjectFilter(w.ID) == nil {
		return s, err
	}
	out := RunnerState{WorkspaceID: s.WorkspaceID, Slug: s.Slug, Name: s.Name, Usage: &store.RunnerUsage{}, PauseOnly: true}
	if s.Settings != nil {
		out.Settings = &store.RunnerSettings{WorkspaceID: s.Settings.WorkspaceID, PausedUntil: s.Settings.PausedUntil,
			PausedOpen: s.Settings.PausedOpen, Paused: s.Settings.Paused, ErrandsPaused: s.Settings.ErrandsPaused, Version: s.Settings.Version}
	}
	return out, nil
}
