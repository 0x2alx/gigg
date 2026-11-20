package api

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/0x2alx/gigg/internal/store"
)

// Metrics: counts, times, cost and tokens per day, week or month. Read-only; what a caller sees is
// what the list endpoints show it: the workspaces and projects in its reach, narrowed by the
// filters. Errand runs belong to the workspace, so they count only where the caller sees the whole
// workspace and no project filter is given.

type metricsIn struct {
	From       string `query:"from" doc:"First day of the range, YYYY-MM-DD in tz (default: 29 days before to)"`
	To         string `query:"to" doc:"Last day of the range, YYYY-MM-DD in tz, included (default: today)"`
	Bucket     string `query:"bucket" enum:"day,week,month," doc:"Default: day. Weeks start on Monday"`
	TZ         string `query:"tz" doc:"IANA time zone that cuts the days (default: your time zone preference, else the instance default)"`
	Workspace  string `query:"workspace" doc:"Only this workspace (id or slug)"`
	Project    int64  `query:"project" doc:"Only this project (id)"`
	Assignee   string `query:"assignee" doc:"A user (id or username). Gigs: assigned to them. Runs and errand runs: done by them"`
	WorkerKind string `query:"worker_kind" enum:"human,agent," doc:"The same, for every user of this kind"`
}

type metricsOut struct {
	Body *store.MetricsResult
}

func (a *API) registerMetrics() {
	huma.Register(a.h, huma.Operation{OperationID: "get-metrics", Method: "GET", Path: "/metrics", Tags: []string{"metrics"},
		Summary: "Gigs created, done and cancelled, agent runs, cost, tokens, errand runs and recurring gigs on time, per day, week or month"},
		func(ctx context.Context, in *metricsIn) (*metricsOut, error) {
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			if err := requireScope(p, "gigs:read"); err != nil {
				return nil, err
			}
			tz := in.TZ
			if tz == "" {
				tz = userTZ(p)
			}
			if tz == "" {
				tz = store.DefaultTZ
			}
			loc, err := time.LoadLocation(tz)
			if err != nil {
				return nil, fail(422, "invalid", "tz is not an IANA time zone: "+tz)
			}
			day := func(s string) (time.Time, error) {
				t, err := time.ParseInLocation("2006-01-02", s, loc)
				if err != nil {
					return t, fail(422, "invalid", "dates are YYYY-MM-DD: "+s)
				}
				return t, nil
			}
			now := time.Now().In(loc)
			to := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
			if in.To != "" {
				if to, err = day(in.To); err != nil {
					return nil, err
				}
			}
			from := to.AddDate(0, 0, -29)
			if in.From != "" {
				if from, err = day(in.From); err != nil {
					return nil, err
				}
			}
			end := to.AddDate(0, 0, 1) // the last day is included
			if !end.After(from) {
				return nil, fail(422, "invalid", "from is after to")
			}
			if end.After(from.AddDate(0, 0, store.MetricsMaxDays)) {
				return nil, fail(422, "invalid", "the range is longer than "+strconv.Itoa(store.MetricsMaxDays)+" days")
			}
			bucket := in.Bucket
			if bucket == "" {
				bucket = "day"
			}
			q := store.MetricsQuery{From: from.UnixMilli(), To: end.UnixMilli(), Bucket: bucket, Loc: loc, WorkerKind: in.WorkerKind}
			if in.Assignee != "" {
				if q.Assignee, err = strconv.ParseInt(in.Assignee, 10, 64); err != nil {
					u, uerr := store.GetUserByName(ctx, a.st.R, strings.TrimPrefix(in.Assignee, "@"))
					if uerr != nil {
						return nil, fail(422, "invalid", "No such user: "+in.Assignee)
					}
					q.Assignee = u.ID
				}
			}
			// the scopes: the workspaces in reach, each with its projects in reach
			ws, err := store.ListWorkspaces(ctx, a.st.R, p.Workspaces())
			if err != nil {
				return nil, storeErr(err)
			}
			var only *store.Workspace
			if in.Workspace != "" {
				if only, err = a.workspace(ctx, p, in.Workspace); err != nil {
					return nil, err
				}
			}
			if in.Project != 0 {
				pr, err := a.project(ctx, p, in.Project)
				if err != nil {
					return nil, err
				}
				if only != nil && only.ID != pr.WorkspaceID {
					return nil, errNotFound()
				}
				q.Scopes = []store.MetricsScope{{WorkspaceID: pr.WorkspaceID, Projects: []int64{pr.ID}}}
			} else {
				canErrands := p.HasScope("errands:read")
				for _, w := range ws {
					if (only != nil && only.ID != w.ID) || !p.SeesWorkspace(w.ID) {
						continue
					}
					q.Scopes = append(q.Scopes, store.MetricsScope{WorkspaceID: w.ID, Projects: p.ProjectFilter(w.ID), Errands: canErrands && !p.Narrowed(w.ID)})
				}
			}
			m, err := store.Metrics(ctx, a.st.R, q)
			if err != nil {
				return nil, storeErr(err)
			}
			return &metricsOut{Body: m}, nil
		})
}
