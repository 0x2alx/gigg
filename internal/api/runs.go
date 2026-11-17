package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/0x2alx/gigg/internal/access"
	"github.com/0x2alx/gigg/internal/store"
)

type runsOut struct {
	Body struct {
		Items []*store.Run `json:"items"`
	}
}

// RunDetail is a run with its timeline.
type RunDetail struct {
	*store.Run
	Events []store.RunEvent `json:"events"`
}

type runOut struct {
	Body *RunDetail
}

type runStartIn struct {
	ID   string `path:"id"`
	Body struct {
		SessionID *string `json:"session_id,omitempty" doc:"Lower-case UUID of the agent session"`
		Mode      string  `json:"mode,omitempty" maxLength:"32" doc:"Free label, e.g. new, resume, interrupted"`
		Attempt   int64   `json:"attempt,omitempty" minimum:"0"`
		Model     *string `json:"model,omitempty" maxLength:"100"`
	}
}

type runProgressIn struct {
	ID   int64 `path:"id"`
	Body struct {
		Pct  *int64 `json:"pct,omitempty" minimum:"0" maximum:"100"`
		Note string `json:"note,omitempty" maxLength:"2048"`
	}
}

// RunFinish is what a worker reports at the end of a run.
type RunFinish struct {
	Status         string   `json:"status" enum:"done,needs_human,error,timeout,interrupted,parked"`
	ReportMD       string   `json:"report_md,omitempty" maxLength:"65536" doc:"Stored as a comment on the gig"`
	CommentKind    string   `json:"comment_kind,omitempty" enum:"report,question,comment," doc:"Default: report"`
	MoveToCategory string   `json:"move_to_category,omitempty" enum:"backlog,todo,doing,waiting,done,cancelled," doc:"Where the gig goes; empty = stays"`
	CostUSD        *float64 `json:"cost_usd,omitempty"`
	NumTurns       *int64   `json:"num_turns,omitempty"`
	DurationMS     *int64   `json:"duration_ms,omitempty"`
	TokensIn       *int64   `json:"tokens_in,omitempty"`
	TokensOut      *int64   `json:"tokens_out,omitempty"`
	TokensCacheR   *int64   `json:"tokens_cache_read,omitempty"`
	TokensCacheW   *int64   `json:"tokens_cache_write,omitempty"`
	Model          *string  `json:"model,omitempty" maxLength:"100"`
	SessionID      *string  `json:"session_id,omitempty"`
	TranscriptPath *string  `json:"transcript_path,omitempty" maxLength:"1024"`
	LogPath        *string  `json:"log_path,omitempty" maxLength:"1024"`
	ExitCode       *int64   `json:"exit_code,omitempty"`
	ErrorReason    *string  `json:"error_reason,omitempty" maxLength:"1024"`
	CheckAt        *int64   `json:"check_at,omitempty" doc:"Only with status parked: the gig's start (ms) moves here, so a claim skips it until then (at most 7 days ahead)"`
}

type runFinishIn struct {
	ID   int64 `path:"id"`
	Body RunFinish
}

// FinishResult is the closed run and the gig after the move.
type FinishResult struct {
	Run *store.Run `json:"run"`
	Gig *store.Gig `json:"gig"`
}

// ClaimBody selects and moves the next gig.
type ClaimBody struct {
	FromCategory string   `json:"from_category,omitempty" enum:"backlog,todo,doing,waiting,done,cancelled," doc:"Default: todo"`
	ToCategory   string   `json:"to_category,omitempty" enum:"backlog,todo,doing,waiting,done,cancelled," doc:"Default: doing"`
	Projects     []int64  `json:"projects,omitempty"`
	CreatedBy    []string `json:"created_by,omitempty" doc:"Only gigs created by these users (ids or usernames)"`
	StartDue     bool     `json:"start_due,omitempty" doc:"Skip gigs whose start date is in the future"`
	AlwaysOnly   bool     `json:"always_only,omitempty" doc:"Only always-run gigs (runners send it by day, or when the usage budget says no)"`
	DryRun       bool     `json:"dry_run,omitempty"`
}

type claimOut struct {
	Status int
	ETag   string `header:"ETag"`
	Body   *store.Gig
}

type claimIn struct {
	WS   string `path:"ws"`
	Body ClaimBody
}

type wsRunsIn struct {
	WS           string `path:"ws"`
	Status       string `query:"status" enum:"running,done,needs_human,error,timeout,interrupted,parked,"`
	StartedSince int64  `query:"started_since" doc:"Unix ms"`
	Limit        int    `query:"limit" minimum:"0" maximum:"500"`
}

type statsIn struct {
	WS    string `path:"ws"`
	Since int64  `query:"since" doc:"Unix ms (default: 24 h ago)"`
}

type textOut struct {
	ContentType string `header:"Content-Type"`
	Body        []byte
}

type activityIn struct {
	WS       string `path:"ws"`
	BeforeID int64  `query:"before_id"`
	Limit    int    `query:"limit" minimum:"0" maximum:"500"`
}

type auditIn struct {
	BeforeID int64 `query:"before_id"`
	Limit    int   `query:"limit" minimum:"0" maximum:"500"`
}

// InboxItem is a gig waiting for a human, with the question.
type InboxItem struct {
	Gig       *store.Gig     `json:"gig"`
	Question  *store.Comment `json:"question"`
	Project   string         `json:"project"`
	Workspace string         `json:"workspace"`
}

// progressEventEvery: at most one run.progress event per run in this time (tests shorten it).
var progressEventEvery = 15 * time.Second

func (a *API) registerRuns() {
	huma.Register(a.h, huma.Operation{OperationID: "list-gig-runs", Method: "GET", Path: "/gigs/{id}/runs", Tags: []string{"runs"},
		Summary: "A gig's runs, newest first"}, func(ctx context.Context, in *gigPath) (*runsOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		t, err := a.gig(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if err := require(p, t.WorkspaceID, access.Viewer, "gigs:read"); err != nil {
			return nil, err
		}
		out := &runsOut{}
		out.Body.Items, err = store.ListRuns(ctx, a.st.R, store.RunFilter{GigID: &t.ID})
		if out.Body.Items == nil {
			out.Body.Items = []*store.Run{}
		}
		return out, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "start-run", Method: "POST", Path: "/gigs/{id}/runs", Tags: []string{"runs"},
		Summary: "Record that a worker started on the gig (gigg runs nothing)", DefaultStatus: 201}, func(ctx context.Context, in *runStartIn) (*runOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		t, err := a.gig(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if t.Deleted {
			return nil, trashErr(t)
		}
		if err := require(p, t.WorkspaceID, access.Worker, "runs:write"); err != nil {
			return nil, err
		}
		if !slices.Contains(t.Assignees, p.User.ID) {
			return nil, errForbidden("Only an assignee of the gig starts runs on it.")
		}
		var r *store.Run
		err = a.tx(ctx, func(tx *sql.Tx) error {
			if err := store.CheckRunStart(ctx, tx, t); err != nil {
				return err
			}
			var err error
			r, err = store.StartRun(ctx, tx, p.Actor(), t, in.Body.SessionID, in.Body.Mode, in.Body.Attempt, in.Body.Model)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &runOut{Body: &RunDetail{Run: r, Events: []store.RunEvent{}}}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "get-run", Method: "GET", Path: "/runs/{id}", Tags: []string{"runs"},
		Summary: "A run with its timeline"}, func(ctx context.Context, in *idPath) (*runOut, error) {
		p, r, err := a.run(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		if err := require(p, r.WorkspaceID, access.Viewer, "gigs:read"); err != nil {
			return nil, err
		}
		ev, err := store.RunEvents(ctx, a.st.R, r.ID)
		return &runOut{Body: &RunDetail{Run: r, Events: ev}}, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "run-progress", Method: "POST", Path: "/runs/{id}/progress", Tags: []string{"runs"},
		Summary: "Heartbeat and an optional progress note"}, func(ctx context.Context, in *runProgressIn) (*runOut, error) {
		p, r, err := a.run(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		if err := require(p, r.WorkspaceID, access.Worker, "runs:write"); err != nil {
			return nil, err
		}
		err = a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			r, err = store.RunProgress(ctx, tx, r, in.Body.Pct, in.Body.Note)
			if err != nil {
				return err
			}
			// open boards show the percentage and the note: tell them when either changed, at most once
			// per run every few seconds (a later call, even a bare heartbeat, sends what was held back)
			return store.RunProgressEvent(ctx, tx, p.Actor(), r, progressEventEvery)
		})
		if err != nil {
			return nil, err
		}
		return &runOut{Body: &RunDetail{Run: r, Events: []store.RunEvent{}}}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "finish-run", Method: "POST", Path: "/runs/{id}/finish", Tags: []string{"runs"},
		Summary: "Close a run with what the worker reports; store the report; move the gig if asked"}, func(ctx context.Context, in *runFinishIn) (*struct{ Body *FinishResult }, error) {
		p, r, err := a.run(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		if err := require(p, r.WorkspaceID, access.Worker, "runs:write"); err != nil {
			return nil, err
		}
		b := in.Body
		if b.ReportMD != "" && !p.HasScope("comments:write") {
			return nil, errForbidden("This key lacks the comments:write scope.")
		}
		if b.MoveToCategory != "" && !p.HasScope("gigs:work") {
			return nil, errForbidden("This key lacks the gigs:work scope.")
		}
		// moving the gig back to To-Do or Doing reopens it: the same check as move-gig (an always-run
		// gig, or one with a tool allowlist, only by people allowed to run those)
		if open := func(c string) bool { return c == "todo" || c == "doing" }; open(b.MoveToCategory) {
			cur, err := store.GetGig(ctx, a.st.R, r.GigID)
			if err != nil {
				return nil, storeErr(err)
			}
			if !open(cur.StatusCategory) {
				if err := alwaysRunGuard(p, cur.WorkspaceID, cur.AlwaysRun); err != nil {
					return nil, err
				}
				if err := toolsGuard(p, cur.WorkspaceID, len(cur.AllowedTools) > 0); err != nil {
					return nil, err
				}
			}
		}
		f := store.Finish{Status: b.Status, ReportMD: b.ReportMD, CommentKind: b.CommentKind, MoveToCategory: b.MoveToCategory, CostUSD: b.CostUSD,
			NumTurns: b.NumTurns, DurationMS: b.DurationMS, TokensIn: b.TokensIn, TokensOut: b.TokensOut, TokensCacheR: b.TokensCacheR,
			TokensCacheW: b.TokensCacheW, Model: b.Model, SessionID: b.SessionID, TranscriptPath: b.TranscriptPath, LogPath: b.LogPath,
			ExitCode: b.ExitCode, ErrorReason: b.ErrorReason, CheckAt: b.CheckAt}
		var t *store.Gig
		err = a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			r, t, err = store.FinishRun(ctx, tx, p.Actor(), r, f)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &struct{ Body *FinishResult }{Body: &FinishResult{Run: r, Gig: t}}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "claim-gig", Method: "POST", Path: "/workspaces/{ws}/gigs/claim", Tags: []string{"runs"},
		Summary: "Take the next matching gig and move it, atomically (204 when nothing matches)"}, func(ctx context.Context, in *claimIn) (*claimOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		w, err := a.workspace(ctx, p, in.WS)
		if err != nil {
			return nil, err
		}
		if err := require(p, w.ID, access.Worker, "gigs:work"); err != nil {
			return nil, err
		}
		c := store.Claim{FromCategory: in.Body.FromCategory, ToCategory: in.Body.ToCategory, StartDue: in.Body.StartDue, DryRun: in.Body.DryRun,
			AlwaysOnly: in.Body.AlwaysOnly}
		c.Projects = p.ProjectFilter()
		if in.Body.Projects != nil {
			for _, pid := range in.Body.Projects {
				if !p.SeesProject(w.ID, pid) {
					return nil, errNotFound()
				}
			}
			c.Projects = in.Body.Projects
		}
		for _, ref := range in.Body.CreatedBy {
			var u *store.User
			if id, e := strconv.ParseInt(ref, 10, 64); e == nil {
				u, err = store.GetUser(ctx, a.st.R, id)
			} else {
				u, err = store.GetUserByName(ctx, a.st.R, ref)
			}
			if err == nil {
				c.CreatedBy = append(c.CreatedBy, u.ID)
			}
		}
		if in.Body.CreatedBy != nil && c.CreatedBy == nil {
			return &claimOut{Status: 204}, nil // nobody matches: nothing to claim
		}
		var t *store.Gig
		err = a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			t, err = store.ClaimGig(ctx, tx, p.Actor(), w.ID, c)
			return err
		})
		var pr *Problem
		if errors.As(err, &pr) && pr.Code == "not_found" {
			return &claimOut{Status: 204}, nil
		}
		if err != nil {
			return nil, err
		}
		return &claimOut{Status: 200, ETag: etag(t.Version), Body: t}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "list-workspace-runs", Method: "GET", Path: "/workspaces/{ws}/runs", Tags: []string{"runs"},
		Summary: "Runs in a workspace, newest first"}, func(ctx context.Context, in *wsRunsIn) (*runsOut, error) {
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
		f := store.RunFilter{WorkspaceIDs: []int64{w.ID}, Projects: p.ProjectFilter(), Status: in.Status, Limit: in.Limit}
		if in.StartedSince != 0 {
			f.StartedSince = &in.StartedSince
		}
		out := &runsOut{}
		out.Body.Items, err = store.ListRuns(ctx, a.st.R, f)
		if out.Body.Items == nil {
			out.Body.Items = []*store.Run{}
		}
		return out, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "workspace-stats", Method: "GET", Path: "/workspaces/{ws}/stats", Tags: []string{"runs"},
		Summary: "Runs, cost and gigs since a time (what a runner needs for its own caps)"}, func(ctx context.Context, in *statsIn) (*struct{ Body *store.Stats }, error) {
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
		since := in.Since
		if since == 0 {
			since = store.Now() - 24*3600*1000
		}
		s, err := store.WorkspaceStats(ctx, a.st.R, w.ID, p.ProjectFilter(), since)
		return &struct{ Body *store.Stats }{Body: s}, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "workspace-summary", Method: "GET", Path: "/workspaces/{ws}/summary", Tags: []string{"workspaces"},
		Summary: "Plain-text board summary: per project, each status with up to 5 gigs"}, func(ctx context.Context, in *wsPath) (*textOut, error) {
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
		ps, err := store.ListProjects(ctx, a.st.R, w.ID, p.ProjectFilter())
		if err != nil {
			return nil, err
		}
		s, err := store.Summary(ctx, a.st.R, w.ID, ps, 5)
		return &textOut{ContentType: "text/plain; charset=utf-8", Body: []byte(s)}, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "workspace-activity", Method: "GET", Path: "/workspaces/{ws}/activity", Tags: []string{"workspaces"},
		Summary: "A workspace's activity, newest first"}, func(ctx context.Context, in *activityIn) (*activityOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		w, err := a.workspace(ctx, p, in.WS)
		if err != nil {
			return nil, err
		}
		if err := require(p, w.ID, access.Viewer, "workspace:read"); err != nil {
			return nil, err
		}
		if err := need(p, "gigs:read"); err != nil {
			return nil, err
		}
		if err := need(p, "comments:read"); err != nil {
			return nil, err
		}
		where, args := "workspace_id = ?", []any{w.ID}
		if !p.IsInstanceAdmin() {
			where += " AND action NOT LIKE 'key.%' AND action NOT LIKE 'member.%' AND action NOT LIKE '%.access'"
		}
		if pf := p.ProjectFilter(); pf != nil {
			where += " AND project_id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(pf)), ",") + ")"
			for _, x := range pf {
				args = append(args, x)
			}
		}
		out := &activityOut{}
		out.Body.Items, err = a.activity(ctx, where, args, in.BeforeID, in.Limit)
		if !p.IsInstanceAdmin() {
			redactDeleted(out.Body.Items)
		}
		return out, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "audit", Method: "GET", Path: "/audit", Tags: []string{"admin"},
		Summary: "The instance-wide audit log (admin)"}, func(ctx context.Context, in *auditIn) (*activityOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := adminWith(p, "admin:audit"); err != nil {
			return nil, err
		}
		out := &activityOut{}
		out.Body.Items, err = a.activity(ctx, "1=1", nil, in.BeforeID, in.Limit)
		return out, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "inbox", Method: "GET", Path: "/inbox", Tags: []string{"gigs"},
		Summary: "Gigs waiting for a human across your workspaces, with the latest question"}, a.inbox)
}

type inboxOut struct {
	Body struct {
		Items []InboxItem `json:"items"`
	}
}

func (a *API) inbox(ctx context.Context, _ *struct{}) (*inboxOut, error) {
	p, err := me(ctx)
	if err != nil {
		return nil, err
	}
	ws, err := store.ListWorkspaces(ctx, a.st.R, p.Workspaces())
	if err != nil {
		return nil, err
	}
	out := &inboxOut{}
	out.Body.Items = []InboxItem{}
	for _, w := range ws {
		if !p.Can(w.ID, access.Viewer, "gigs:read") || w.Archived {
			continue
		}
		ps, err := store.ListProjects(ctx, a.st.R, w.ID, p.ProjectFilter())
		if err != nil {
			return nil, err
		}
		names := map[int64]string{}
		for _, pr := range ps {
			names[pr.ID] = pr.Name
		}
		ts, _, err := store.ListGigs(ctx, a.st.R, store.GigFilter{WorkspaceID: w.ID, Projects: p.ProjectFilter(), Category: "waiting", Sort: "updated", Limit: 200})
		if err != nil {
			return nil, err
		}
		for _, t := range ts {
			item := InboxItem{Gig: t, Project: names[t.ProjectID], Workspace: w.Name}
			if p.HasScope("comments:read") {
				if item.Question, err = store.LatestQuestion(ctx, a.st.R, t.ID); err != nil {
					return nil, err
				}
			}
			out.Body.Items = append(out.Body.Items, item)
		}
	}
	return out, nil
}

func (a *API) run(ctx context.Context, id int64) (*access.Principal, *store.Run, error) {
	p, err := me(ctx)
	if err != nil {
		return nil, nil, err
	}
	r, err := store.GetRun(ctx, a.st.R, id)
	if err != nil {
		return nil, nil, errNotFound()
	}
	t, err := store.GetGig(ctx, a.st.R, r.GigID)
	if err != nil || !p.SeesProject(t.WorkspaceID, t.ProjectID) {
		return nil, nil, errNotFound()
	}
	return p, r, nil
}

// activity reads the log (newest first) with a WHERE clause built by the caller from constants.
func (a *API) activity(ctx context.Context, where string, args []any, beforeID int64, limit int) ([]ActivityItem, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if beforeID > 0 {
		where = "(" + where + ") AND id < ?"
		args = append(args, beforeID)
	}
	rows, err := a.st.R.QueryContext(ctx, `SELECT id, at, actor_user_id, key_id, listener, action, entity_type, entity_id, project_id, data
		FROM activity WHERE `+where+` ORDER BY id DESC LIMIT `+strconv.Itoa(limit), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ActivityItem{}
	for rows.Next() {
		var it ActivityItem
		var actor, key, ent, proj sql.NullInt64
		var data string
		if err := rows.Scan(&it.ID, &it.At, &actor, &key, &it.Listener, &it.Action, &it.EntityType, &ent, &proj, &data); err != nil {
			return nil, err
		}
		if actor.Valid {
			it.ActorID = &actor.Int64
		}
		if key.Valid {
			it.KeyID = &key.Int64
		}
		if ent.Valid {
			it.EntityID = &ent.Int64
		}
		if proj.Valid {
			it.ProjectID = &proj.Int64
		}
		_ = json.Unmarshal([]byte(data), &it.Data)
		out = append(out, it)
	}
	return out, rows.Err()
}

// redactDeleted removes the old text of deleted and edited comments from activity entries (admins see it).
func redactDeleted(items []ActivityItem) {
	for i := range items {
		if items[i].Action != "comment.deleted" && items[i].Action != "comment.edited" {
			continue
		}
		if m, ok := items[i].Data.(map[string]any); ok {
			delete(m, "old")
		}
	}
}
