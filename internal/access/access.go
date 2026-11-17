// Package access decides what a request may see and do. Every handler asks it before touching the
// store: permissions (per workspace and per project, see store/access.go) AND key scopes AND the
// places a limited key reaches. A resource outside the caller's reach is reported as not found, so
// ids can't be probed.
package access

import (
	"context"
	"slices"

	"github.com/0x2alx/gigg/internal/store"
)

// Request facts set by the HTTP layer.
type Request struct {
	Listener  string
	IP        string
	RequestID string
}

type ctxKey int

const (
	keyRequest ctxKey = iota
	keyPrincipal
)

// WithRequest stores request facts.
func WithRequest(ctx context.Context, r Request) context.Context {
	return context.WithValue(ctx, keyRequest, r)
}

// RequestFrom returns request facts (zero value outside HTTP).
func RequestFrom(ctx context.Context) Request {
	r, _ := ctx.Value(keyRequest).(Request)
	return r
}

// WithPrincipal stores the authenticated caller.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, keyPrincipal, p)
}

// From returns the caller, or nil when the request is anonymous.
func From(ctx context.Context) *Principal {
	p, _ := ctx.Value(keyPrincipal).(*Principal)
	return p
}

// Principal is an authenticated caller: a user through a session, or a user through a key.
type Principal struct {
	User      *store.User
	Key       *store.APIKey // nil for a browser session
	SessionID int64
	Grants    *store.Grants // the user's access rules, resolved per request
	Req       Request
}

// The permissions routes ask for most.
const (
	Viewer    = "view"
	Commenter = "comment"
	Worker    = "work_gigs" // a consumer's: status moves, ticks, claims, runs
	Editor    = "edit_gigs" // a producer's: what a gig says and how it runs
	Manager   = "manage_workspace"
)

// Allowed reports a permission for a workspace as a whole: workspace-level things (errands,
// templates, settings...) and gig-level permissions asked without a project. A key limited to some
// of the workspace's projects has none of it.
func (p *Principal) Allowed(ws int64, perm string) bool {
	if p.Key != nil && !p.Key.WholeWorkspace(ws) {
		return false
	}
	return p.Grants != nil && p.Grants.Has(ws, perm)
}

// AllowedIn reports a permission in one project of a workspace (rules for that project, then the
// workspace's, then broader ones; a limited key must reach the project).
func (p *Principal) AllowedIn(ws, project int64, perm string) bool {
	if !store.IsProjectPermission(perm) {
		return p.Allowed(ws, perm) // about the workspace as a whole, wherever it's asked from
	}
	if p.Key != nil && !p.Key.ReachesProject(ws, project) {
		return false
	}
	return p.Grants != nil && p.Grants.HasIn(ws, project, perm)
}

// ProjectsWith lists the projects of ws (in sidebar order) where perm is held, within the key's reach.
func (p *Principal) ProjectsWith(ws int64, perm string) []int64 {
	if p.Grants == nil || (p.Key != nil && !p.Key.ReachesWorkspace(ws)) {
		return []int64{}
	}
	out := []int64{}
	for _, pid := range p.Grants.ProjectsWith(ws, perm) {
		if p.Key == nil || p.Key.ReachesProject(ws, pid) {
			out = append(out, pid)
		}
	}
	return out
}

// AllowedAnywhere reports a permission held in at least one workspace (or project) in reach.
func (p *Principal) AllowedAnywhere(perm string) bool {
	if p.Key != nil && p.Key.Bound {
		for _, pl := range p.Key.Reach {
			if pl.ProjectID == nil && p.Allowed(pl.WorkspaceID, perm) {
				return true
			}
			if store.IsProjectPermission(perm) && len(p.ProjectsWith(pl.WorkspaceID, perm)) > 0 {
				return true
			}
		}
		return false
	}
	return p.Grants != nil && p.Grants.Anywhere(perm)
}

// Actor is the store's view of who writes.
func (p *Principal) Actor() store.Actor {
	a := store.Actor{UserID: p.User.ID, Listener: p.Req.Listener, IP: p.Req.IP, RequestID: p.Req.RequestID}
	if p.Key != nil {
		a.KeyID = &p.Key.ID
	}
	return a
}

// IsInstanceAdmin: an admin user through a session or a key that follows them (not limited to
// some places). A non-admin's unlimited key acts like that user's session: it reaches what the
// user's rules give, limited by its scopes.
func (p *Principal) IsInstanceAdmin() bool {
	return p.User.IsAdmin && (p.Key == nil || !p.Key.Bound)
}

// HasScope: sessions carry every scope; keys only their own.
func (p *Principal) HasScope(scope string) bool {
	if p.Key == nil {
		return true
	}
	return slices.Contains(p.Key.Scopes, scope)
}

// AdminWith: an instance admin holding the given admin scope (always true for an admin session).
func (p *Principal) AdminWith(scope string) bool {
	return p.IsInstanceAdmin() && p.HasScope(scope)
}

// SeesWorkspace reports whether the workspace exists for this caller: whole, or some of its projects.
func (p *Principal) SeesWorkspace(ws int64) bool {
	return p.Allowed(ws, Viewer) || len(p.ProjectsWith(ws, Viewer)) > 0
}

// Narrowed reports whether the caller sees only some projects of ws: no whole-workspace view (by
// its rules, or because its key is limited to some projects), so the workspace-level things
// (errands outside those projects, templates, recurring timings, settings) stay out of reach.
func (p *Principal) Narrowed(ws int64) bool { return !p.Allowed(ws, Viewer) }

// SeesProject reports whether a project (of workspace ws) is in reach.
func (p *Principal) SeesProject(ws, project int64) bool { return p.AllowedIn(ws, project, Viewer) }

// ProjectFilter is the project narrowing of ws for queries: nil = every project of the workspace,
// otherwise the projects that can be seen (possibly none).
func (p *Principal) ProjectFilter(ws int64) []int64 {
	if p.Allowed(ws, Viewer) && (p.Grants == nil || p.Grants.ProjectFilter(ws) == nil) {
		return nil
	}
	return p.ProjectsWith(ws, Viewer)
}

// Current is the workspace "current" means: a limited key's first place (nil otherwise).
func (p *Principal) Current() *int64 {
	if p.Key == nil {
		return nil
	}
	return p.Key.Current()
}

// Workspaces lists the workspace ids in reach (nil = all, for instance admins).
func (p *Principal) Workspaces() []int64 {
	if p.Key != nil && p.Key.Bound {
		out := []int64{}
		for _, pl := range p.Key.Reach {
			if !slices.Contains(out, pl.WorkspaceID) && p.SeesWorkspace(pl.WorkspaceID) {
				out = append(out, pl.WorkspaceID)
			}
		}
		return out
	}
	if p.User.IsAdmin {
		return nil
	}
	if p.Grants == nil {
		return []int64{}
	}
	return p.Grants.Visible()
}

// Can: the permission for the workspace as a whole AND the scope (for keys). Callers check
// visibility first and answer 404 when the workspace or project is out of reach, 403 when Can is false.
func (p *Principal) Can(ws int64, perm, scope string) bool {
	return p.Allowed(ws, perm) && p.HasScope(scope)
}

// CanIn: the permission in one project AND the scope (for keys).
func (p *Principal) CanIn(ws, project int64, perm, scope string) bool {
	return p.AllowedIn(ws, project, perm) && p.HasScope(scope)
}

// Load builds a principal for a user, reading their access rules.
func Load(ctx context.Context, q store.Q, u *store.User, key *store.APIKey, sessionID int64, req Request) (*Principal, error) {
	g, err := store.LoadGrants(ctx, q, u)
	if err != nil {
		return nil, err
	}
	return &Principal{User: u, Key: key, SessionID: sessionID, Grants: g, Req: req}, nil
}
