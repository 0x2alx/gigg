package api

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/0x2alx/gigg/internal/access"
	"github.com/0x2alx/gigg/internal/store"
)

// Make shared: a workspace's schedule, gig template or errand template becomes a shared one in one
// transaction. The identical copies in other workspaces are merged into it: their recurring gigs and
// errands move to the shared item and the source and the copies are deleted (see store/makeshared.go for
// what "identical" means). Needs Shared library (sharedWrite: the library:write scope and the scope of
// the kind, a key not limited to some places), and for the source and every copy what deleting it needs in
// its workspace, and for every recurring gig and errand that moves what editing it by hand would need in
// its project; a copy in a workspace where the caller lacks any of that is left alone and listed as
// skipped (the source's own users refuse the whole move). The settings that have a permission of their own
// (always-run, tool allowlists, run timeouts, no sandbox) are asked on the all-workspaces level, as for any
// shared item.

type makeSharedBody struct {
	DryRun       bool    `json:"dry_run,omitempty" doc:"Only return the plan: the shared item, the copies found (and skipped), the users that would move. Nothing changes"`
	ExpectCopies []int64 `json:"expect_copies,omitempty" doc:"The ids of the copies the reviewed plan listed. When given and the plan computed now lists others, nothing happens and the answer is 409 (an empty list expects no copies)"`
}

type sharePlanOut struct {
	Body *store.SharePlan
}

// shareDenied says why the caller may not remove a workspace's item (and move its users) in its
// workspace, or "" when they may: what deleting it by hand would need there.
func shareDenied(p *access.Principal, kind string, it store.ShareItem) string {
	ws := it.WorkspaceID
	if p.Narrowed(ws) {
		return "access to only some projects of " + it.Workspace
	}
	need := func(perm, scope string) string {
		if !p.Can(ws, perm, scope) {
			return "no " + permNames[perm] + " permission in " + it.Workspace
		}
		return ""
	}
	switch kind {
	case store.ShareSchedule:
		if r := need("recurring_gigs", "gigs:write"); r != "" {
			return r
		}
		if it.Errands > 0 {
			return need("errands", "errands:write")
		}
	case store.ShareTemplate:
		if r := need(access.Editor, "gigs:write"); r != "" {
			return r
		}
		if r := need("templates", "gigs:write"); r != "" {
			return r
		}
		if it.Recurring > 0 {
			return need("recurring_gigs", "gigs:write")
		}
	case store.ShareErrandTemplate:
		return need("errands", "errands:write")
	}
	return ""
}

// shareUserDenied says why the caller may not repoint a recurring gig or an errand in its project, or ""
// when they may: what editing it by hand would need there (mirroring update-recurrence and the errand
// guard), and seeing the project at all.
func shareUserDenied(p *access.Principal, u store.ShareUser) string {
	ws := u.WorkspaceID
	where := func(what string) string {
		return what + " in the project of " + strings.ReplaceAll(u.Kind, "_", " ") + " #" + fmt.Sprint(u.ID) + " (" + u.Workspace + ")"
	}
	if u.ProjectID == nil {
		if !p.Allowed(ws, access.Viewer) {
			return where("no access")
		}
	} else if !p.SeesProject(ws, *u.ProjectID) {
		return "a project of " + u.Workspace + " you can't see"
	}
	has := func(perm, scope string) bool {
		switch {
		case u.ProjectID == nil && scope == "":
			return p.Allowed(ws, perm)
		case u.ProjectID == nil:
			return p.Can(ws, perm, scope)
		case scope == "":
			return p.AllowedIn(ws, *u.ProjectID, perm)
		}
		return p.CanIn(ws, *u.ProjectID, perm, scope)
	}
	var checks []struct{ perm, scope string }
	switch u.Kind {
	case "recurring_gig":
		checks = []struct{ perm, scope string }{{access.Editor, "gigs:write"}, {"create_gigs", ""}, {"recurring_gigs", ""}}
	case "errand":
		checks = []struct{ perm, scope string }{{"errands", "errands:write"}}
	}
	for _, c := range checks {
		if !has(c.perm, c.scope) {
			return where("no " + permNames[c.perm] + " permission")
		}
	}
	return ""
}

// sharePlan plans (and checks) a Make shared of the source id, reading through q. It returns the plan and
// the ids of the copies to merge.
func (a *API) sharePlan(ctx context.Context, q store.Q, p *access.Principal, kind string, id int64) (*store.SharePlan, []int64, error) {
	src, cands, err := store.ShareFind(ctx, q, kind, id)
	if err != nil {
		return nil, nil, storeErr(err)
	}
	if why := shareDenied(p, kind, src); why != "" {
		return nil, nil, errForbidden("You can't remove this item: " + why + ".")
	}
	candIDs := []int64{id}
	for _, c := range cands {
		candIDs = append(candIDs, c.ID)
	}
	users, err := store.ShareUsers(ctx, q, kind, candIDs)
	if err != nil {
		return nil, nil, storeErr(err)
	}
	byItem := map[int64][]store.ShareUser{}
	for _, u := range users {
		byItem[u.From] = append(byItem[u.From], u)
	}
	for _, u := range byItem[id] {
		if why := shareUserDenied(p, u); why != "" {
			return nil, nil, errForbidden("You can't move what uses this item: " + why + ".")
		}
	}
	plan := &store.SharePlan{Kind: kind, Source: src, Copies: []store.ShareItem{}, Skipped: []store.ShareItem{}, NewName: src.Name}
	ids := []int64{id}
	var copyIDs []int64
	for _, c := range cands {
		why := shareDenied(p, kind, c)
		for _, u := range byItem[c.ID] {
			if why == "" {
				why = shareUserDenied(p, u)
			}
		}
		if why != "" {
			c.Reason = why
			plan.Skipped = append(plan.Skipped, c)
			continue
		}
		plan.Copies = append(plan.Copies, c)
		copyIDs = append(copyIDs, c.ID)
		ids = append(ids, c.ID)
	}
	if err := a.shareRisks(ctx, q, p, kind, id, ids); err != nil {
		return nil, nil, err
	}
	plan.Users = []store.ShareUser{}
	for _, u := range users {
		if slices.Contains(ids, u.From) {
			plan.Users = append(plan.Users, u)
		}
	}
	if err := store.ShareCheck(ctx, q, kind, ids); err != nil {
		return nil, nil, storeErr(err)
	}
	plan.Moved, plan.DeleteN = len(plan.Users), len(ids)
	return plan, copyIDs, nil
}

// shareRisks: the new shared item carries the settings the source (and for a schedule, every recurring
// gig and errand that moves onto it) has, so the caller needs the permissions those settings need on the
// all-workspaces level.
func (a *API) shareRisks(ctx context.Context, q store.Q, p *access.Principal, kind string, id int64, ids []int64) error {
	switch kind {
	case store.ShareSchedule:
		for _, sid := range ids {
			always, tools, err := store.ScheduleGuards(ctx, q, sid)
			if err != nil {
				return storeErr(err)
			}
			if err := alwaysRunGuard(p, 0, always); err != nil {
				return err
			}
			if err := toolsGuard(p, 0, tools); err != nil {
				return err
			}
			if n, err := store.UnsandboxedErrandUses(ctx, q, sid); err != nil {
				return storeErr(err)
			} else if n > 0 {
				if err := allowed(p, 0, "errand_unsandboxed"); err != nil {
					return err
				}
			}
		}
	case store.ShareTemplate:
		t, err := store.GetTemplate(ctx, q, id)
		if err != nil {
			return storeErr(err)
		}
		if err := alwaysRunChange(p, 0, false, t.AlwaysRun); err != nil {
			return err
		}
		if err := timeoutChange(p, 0, nil, t.RunTimeout); err != nil {
			return err
		}
		return toolsChange(p, 0, nil, t.AllowedTools)
	case store.ShareErrandTemplate:
		t, err := store.GetErrandTemplate(ctx, q, id)
		if err != nil {
			return storeErr(err)
		}
		if t.Unsandboxed {
			if err := allowed(p, 0, "errand_unsandboxed"); err != nil {
				return err
			}
		}
		if t.TimeoutS != store.ErrandDefaultTimeout {
			return allowed(p, 0, "run_timeout")
		}
	}
	return nil
}

// shareScope is the scope of the kind that sharedWrite asks next to library:write.
func shareScope(kind string) string {
	if kind == store.ShareErrandTemplate {
		return "errands:write"
	}
	return "gigs:write"
}

// makeShared runs the plan (dry run) or the move, in one transaction. A move that names expect_copies
// goes ahead only when the copies planned inside the transaction are exactly those.
func (a *API) makeShared(ctx context.Context, p *access.Principal, kind string, id int64, body makeSharedBody) (*sharePlanOut, error) {
	if err := sharedWrite(p, shareScope(kind)); err != nil {
		return nil, err
	}
	if body.DryRun {
		plan, _, err := a.sharePlan(ctx, a.st.R, p, kind, id)
		if err != nil {
			return nil, err
		}
		plan.DryRun = true
		return &sharePlanOut{plan}, nil
	}
	var plan *store.SharePlan
	err := a.tx(ctx, func(tx *sql.Tx) error { // planned inside the write transaction: what is checked is what moves
		var copyIDs []int64
		var err error
		if plan, copyIDs, err = a.sharePlan(ctx, tx, p, kind, id); err != nil {
			return err
		}
		if body.ExpectCopies != nil && !sameIDs(body.ExpectCopies, copyIDs) {
			return fail(409, "conflict", "The copies changed; review the plan again.")
		}
		plan.Shared, err = store.ShareApply(ctx, tx, p.Actor(), kind, id, copyIDs)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &sharePlanOut{plan}, nil
}

// sameIDs: the two lists hold the same ids, in any order.
func sameIDs(a, b []int64) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(slices.Compact(x), y)
}

func (a *API) registerMakeShared() {
	type in struct {
		ID   int64 `path:"id"`
		Body makeSharedBody
	}
	huma.Register(a.h, huma.Operation{OperationID: "make-schedule-shared", Method: "POST", Path: "/schedules/{id}/make-shared", Tags: []string{"recurrence"},
		Summary: "Make a workspace schedule shared, merging its identical copies (Shared library; dry_run returns the plan only)"},
		func(ctx context.Context, i *in) (*sharePlanOut, error) {
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			if _, err := a.schedule(ctx, p, i.ID); err != nil {
				return nil, err
			}
			return a.makeShared(ctx, p, store.ShareSchedule, i.ID, i.Body)
		})
	huma.Register(a.h, huma.Operation{OperationID: "make-template-shared", Method: "POST", Path: "/templates/{id}/make-shared", Tags: []string{"templates"},
		Summary: "Make a workspace gig template shared, merging its identical copies (Shared library; dry_run returns the plan only)"},
		func(ctx context.Context, i *in) (*sharePlanOut, error) {
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			if _, err := a.template(ctx, p, i.ID); err != nil {
				return nil, err
			}
			return a.makeShared(ctx, p, store.ShareTemplate, i.ID, i.Body)
		})
	huma.Register(a.h, huma.Operation{OperationID: "make-errand-template-shared", Method: "POST", Path: "/errand-templates/{id}/make-shared", Tags: []string{"errands"},
		Summary: "Make a workspace errand template shared, merging its identical copies (Shared library; dry_run returns the plan only)"},
		func(ctx context.Context, i *in) (*sharePlanOut, error) {
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			if _, err := a.errandTemplate(ctx, p, i.ID); err != nil {
				return nil, err
			}
			return a.makeShared(ctx, p, store.ShareErrandTemplate, i.ID, i.Body)
		})
}
