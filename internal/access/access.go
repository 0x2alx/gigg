// Package access decides what a request may see and do. Every handler asks it before touching the
// store: permissions (per workspace, see store/access.go) AND key scopes AND the key's workspace
// and project binding. A
// resource outside the caller's reach is reported as not found, so ids can't be probed.
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

// Permissions a route asks for, named after the presets that first include them.
const (
	Viewer    = "view"
	Commenter = "comment"
	Worker    = "work_gigs" // consumer: status moves, ticks, claims, runs
	Member    = "edit_gigs" // producer: field edits
	Manager   = "manage_workspace"
)

// Allowed reports a permission in a workspace (a key never has more than its user, and a
// workspace-bound key nothing elsewhere).
func (p *Principal) Allowed(ws int64, perm string) bool {
	if p.Key != nil && p.Key.WorkspaceID != nil && *p.Key.WorkspaceID != ws {
		return false
	}
	return p.Grants != nil && p.Grants.Has(ws, perm)
}

// AllowedAnywhere reports a permission held in at least one workspace in reach.
func (p *Principal) AllowedAnywhere(perm string) bool {
	if p.Key != nil && p.Key.WorkspaceID != nil {
		return p.Allowed(*p.Key.WorkspaceID, perm)
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

// IsInstanceAdmin: an admin user through a session or an unbound key (no workspace binding).
// An unbound key of a non-admin acts like that user's session: it reaches the workspaces the
// user is a member of, at that role, limited by its scopes.
func (p *Principal) IsInstanceAdmin() bool {
	return p.User.IsAdmin && (p.Key == nil || p.Key.WorkspaceID == nil)
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

// SeesWorkspace reports whether the workspace exists for this caller.
func (p *Principal) SeesWorkspace(ws int64) bool { return p.Allowed(ws, Viewer) }

// Narrowed reports whether the caller's key is limited to some projects.
func (p *Principal) Narrowed() bool { return p.Key != nil && len(p.Key.ProjectIDs) > 0 }

// SeesProject reports whether a project (of workspace ws) is in reach.
func (p *Principal) SeesProject(ws, project int64) bool {
	if !p.SeesWorkspace(ws) {
		return false
	}
	return !p.Narrowed() || slices.Contains(p.Key.ProjectIDs, project)
}

// ProjectFilter is the project narrowing for queries (nil = every project of the workspace).
func (p *Principal) ProjectFilter() []int64 {
	if p.Narrowed() {
		return p.Key.ProjectIDs
	}
	return nil
}

// Workspaces lists the workspace ids in reach (nil = all, for instance admins).
func (p *Principal) Workspaces() []int64 {
	if p.Key != nil && p.Key.WorkspaceID != nil {
		if p.SeesWorkspace(*p.Key.WorkspaceID) {
			return []int64{*p.Key.WorkspaceID}
		}
		return []int64{}
	}
	if p.User.IsAdmin {
		return nil
	}
	if p.Grants == nil {
		return []int64{}
	}
	return p.Grants.Visible()
}

// Can: the permission in ws AND the scope (for keys). Callers check visibility first and answer
// 404 when the workspace or project is out of reach, 403 when Can is false.
func (p *Principal) Can(ws int64, perm, scope string) bool {
	return p.Allowed(ws, perm) && p.HasScope(scope)
}

// Load builds a principal for a user, reading their access rules.
func Load(ctx context.Context, q store.Q, u *store.User, key *store.APIKey, sessionID int64, req Request) (*Principal, error) {
	g, err := store.LoadGrants(ctx, q, u)
	if err != nil {
		return nil, err
	}
	return &Principal{User: u, Key: key, SessionID: sessionID, Grants: g, Req: req}, nil
}
