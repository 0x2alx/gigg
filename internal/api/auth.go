package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/0x2alx/gigg/internal/access"
	"github.com/0x2alx/gigg/internal/auth"
	"github.com/0x2alx/gigg/internal/store"
)

// MeBody describes the caller.
type MeBody struct {
	User       *store.User       `json:"user"`
	Admin      bool              `json:"admin" doc:"Instance admin through this credential"`
	Workspaces []WorkspaceAccess `json:"workspaces"`
	Key        *KeyInfo          `json:"key,omitempty"`
	Listener   string            `json:"listener"`
	// Capabilities: the permissions held in at least one workspace (see GET /permissions).
	Capabilities []string `json:"capabilities"`
}

// WorkspaceAccess is a workspace in reach and the caller's role there.
type WorkspaceAccess struct {
	ID   int64  `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
	// Color: the workspace's colour (#RRGGBB), or null for the UI's default from the slug.
	Color *string `json:"color"`
	// OwnerID: the user the workspace belongs to (display only), or null.
	OwnerID *int64 `json:"owner_id"`
	Role    string `json:"role" enum:"viewer,commenter,member,manager,admin" doc:"The closest preset to what the caller may do here (display only)"`
	// Permissions held here (see GET /permissions), after the key's limits.
	Permissions []string `json:"permissions"`
}

// KeyInfo is the key in use (never the secret).
type KeyInfo struct {
	ID               int64    `json:"id"`
	Name             string   `json:"name"`
	Prefix           string   `json:"prefix"`
	WorkspaceID      *int64   `json:"workspace_id"`
	ProjectIDs       []int64  `json:"project_ids"`
	Scopes           []string `json:"scopes"`
	AllowedListeners []string `json:"allowed_listeners"`
	AllowedCIDRs     []string `json:"allowed_cidrs"`
	ExpiresAt        int64    `json:"expires_at"`
}

func (a *API) meBody(ctx context.Context, p *access.Principal) (*MeBody, error) {
	ws, err := store.ListWorkspaces(ctx, a.st.R, p.Workspaces())
	if err != nil {
		return nil, err
	}
	b := &MeBody{User: p.User, Admin: p.IsInstanceAdmin(), Workspaces: []WorkspaceAccess{}, Listener: p.Req.Listener}
	for _, w := range ws {
		if !p.SeesWorkspace(w.ID) {
			continue
		}
		perms := []string{}
		for _, c := range store.Permissions {
			if p.Allowed(w.ID, c.Name) {
				perms = append(perms, c.Name)
			}
		}
		b.Workspaces = append(b.Workspaces, WorkspaceAccess{ID: w.ID, Slug: w.Slug, Name: w.Name, Color: w.Color, OwnerID: w.OwnerID, Role: store.RoleLabel(p.User.IsAdmin, perms), Permissions: perms})
	}
	if p.Key != nil {
		b.Key = keyInfo(p.Key)
	}
	b.Capabilities = []string{}
	for _, c := range store.Permissions {
		if p.AllowedAnywhere(c.Name) {
			b.Capabilities = append(b.Capabilities, c.Name)
		}
	}
	return b, nil
}

func keyInfo(k *store.APIKey) *KeyInfo {
	return &KeyInfo{ID: k.ID, Name: k.Name, Prefix: k.Prefix, WorkspaceID: k.WorkspaceID, ProjectIDs: k.ProjectIDs, Scopes: k.Scopes,
		AllowedListeners: k.AllowedListeners, AllowedCIDRs: k.AllowedCIDRs, ExpiresAt: k.ExpiresAt}
}

type meOut struct {
	Body *MeBody
}

type loginIn struct {
	Body struct {
		Username string `json:"username" maxLength:"64"`
		Password string `json:"password" maxLength:"256"`
	}
}

type loginOut struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
	Body      *MeBody
}

type logoutOut struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
}

type passwordIn struct {
	Body struct {
		Current string `json:"current" maxLength:"256"`
		New     string `json:"new" minLength:"12" maxLength:"256"`
	}
}

type sessionsOut struct {
	Body struct {
		Items []*store.Session `json:"items"`
	}
}

type idPath struct {
	ID int64 `path:"id"`
}

type mePatchIn struct {
	IfMatch string `header:"If-Match"`
	Body    struct {
		DisplayName Opt[string]          `json:"display_name,omitempty" maxLength:"64"`
		Description Opt[string]          `json:"description,omitempty" maxLength:"200"`
		Color       Opt[string]          `json:"color,omitempty" doc:"#RRGGBB or null"`
		Preferences Opt[json.RawMessage] `json:"preferences,omitempty" doc:"Display preferences (JSON object)"`
	}
}

type userOut struct {
	ETag string `header:"ETag"`
	Body *store.User
}

// KeyCreate is the body for creating a key.
type KeyCreate struct {
	Name             string   `json:"name" minLength:"1" maxLength:"64"`
	UserID           int64    `json:"user_id,omitempty" doc:"Acting user (admin /keys only; default: the caller)"`
	Workspace        string   `json:"workspace,omitempty" doc:"Workspace id or slug; empty = admin key (admins only)"`
	ProjectIDs       []int64  `json:"project_ids,omitempty" doc:"Narrow the key to these projects of its workspace"`
	Scopes           []string `json:"scopes" minItems:"1"`
	AllowedListeners []string `json:"allowed_listeners,omitempty" doc:"Usable only on these listeners (null = any)"`
	AllowedCIDRs     []string `json:"allowed_cidrs,omitempty" doc:"Usable only from these networks (null = any)"`
	ExpiresAt        int64    `json:"expires_at,omitempty" doc:"Unix ms; default one year from now"`
}

type keyCreateIn struct {
	Body KeyCreate
}

// KeyCreated carries the token, shown once.
type KeyCreated struct {
	Key   *store.APIKey `json:"key"`
	Token string        `json:"token" doc:"Shown once; store it now"`
}

type keyCreatedOut struct {
	Body *KeyCreated
}

type keysOut struct {
	Body struct {
		Items []*store.APIKey `json:"items"`
	}
}

func (a *API) registerAuth() {
	huma.Register(a.h, huma.Operation{OperationID: "login", Method: "POST", Path: "/auth/login", Tags: []string{"auth"},
		Summary: "Log in (browser session cookie)", Security: []map[string][]string{}}, a.login)
	huma.Register(a.h, huma.Operation{OperationID: "logout", Method: "POST", Path: "/auth/logout", Tags: []string{"auth"},
		Summary: "Log out", DefaultStatus: 204}, a.logout)
	huma.Register(a.h, huma.Operation{OperationID: "auth-me", Method: "GET", Path: "/auth/me", Tags: []string{"auth"},
		Summary: "Who am I: user, workspaces and roles, key scopes and restrictions"}, func(ctx context.Context, _ *struct{}) (*meOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		b, err := a.meBody(ctx, p)
		return &meOut{Body: b}, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "change-password", Method: "POST", Path: "/auth/password", Tags: []string{"auth"},
		Summary: "Change your password (logs out every session)", DefaultStatus: 204}, a.changePassword)
	huma.Register(a.h, huma.Operation{OperationID: "list-sessions", Method: "GET", Path: "/auth/sessions", Tags: []string{"auth"},
		Summary: "Your browser sessions"}, func(ctx context.Context, _ *struct{}) (*sessionsOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if p.Key != nil {
			return nil, errForbidden("Sessions are managed from a browser session.")
		}
		out := &sessionsOut{}
		out.Body.Items, err = store.ListSessions(ctx, a.st.R, p.User.ID)
		if out.Body.Items == nil {
			out.Body.Items = []*store.Session{}
		}
		return out, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "revoke-session", Method: "DELETE", Path: "/auth/sessions/{id}", Tags: []string{"auth"},
		Summary: "End one of your sessions", DefaultStatus: 204}, func(ctx context.Context, in *idPath) (*struct{}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if p.Key != nil {
			return nil, errForbidden("Sessions are managed from a browser session.")
		}
		return nil, storeErr(store.RevokeSession(ctx, a.st.W, in.ID, p.User.ID))
	})

	huma.Register(a.h, huma.Operation{OperationID: "get-me", Method: "GET", Path: "/me", Tags: []string{"profile"},
		Summary: "Your profile"}, func(ctx context.Context, _ *struct{}) (*userOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		return &userOut{ETag: etag(p.User.Version), Body: p.User}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "patch-me", Method: "PATCH", Path: "/me", Tags: []string{"profile"},
		Summary: "Change your profile and preferences"}, a.patchMe)
	huma.Register(a.h, huma.Operation{OperationID: "list-my-keys", Method: "GET", Path: "/me/keys", Tags: []string{"profile"},
		Summary: "Keys acting as you"}, func(ctx context.Context, _ *struct{}) (*keysOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := need(p, "keys:self"); err != nil {
			return nil, err
		}
		out := &keysOut{}
		out.Body.Items, err = store.ListKeys(ctx, a.st.R, p.User.ID)
		if out.Body.Items == nil {
			out.Body.Items = []*store.APIKey{}
		}
		return out, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "create-my-key", Method: "POST", Path: "/me/keys", Tags: []string{"profile"},
		Summary: "Create a key acting as you (never wider than the credential you use)", DefaultStatus: 201},
		func(ctx context.Context, in *keyCreateIn) (*keyCreatedOut, error) {
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			if err := need(p, "keys:self"); err != nil {
				return nil, err
			}
			in.Body.UserID = p.User.ID
			return a.createKey(ctx, p, in.Body)
		})
	huma.Register(a.h, huma.Operation{OperationID: "rotate-my-key", Method: "POST", Path: "/me/keys/{id}/rotate", Tags: []string{"profile"},
		Summary: "Replace one of your keys' secret"}, func(ctx context.Context, in *idPath) (*keyCreatedOut, error) {
		return a.rotateKey(ctx, in.ID, true)
	})
	huma.Register(a.h, huma.Operation{OperationID: "revoke-my-key", Method: "DELETE", Path: "/me/keys/{id}", Tags: []string{"profile"},
		Summary: "Revoke one of your keys", DefaultStatus: 204}, func(ctx context.Context, in *idPath) (*struct{}, error) {
		return nil, a.revokeKey(ctx, in.ID, true)
	})
}

func (a *API) login(ctx context.Context, in *loginIn) (*loginOut, error) {
	req := access.RequestFrom(ctx)
	if !a.listener(req.Listener).UI {
		return nil, errForbidden("Browser login is not available on this listener; use an API key.")
	}
	name := strings.ToLower(strings.TrimSpace(in.Body.Username))
	if !a.limiter.allow("login:"+req.IP+":"+name, 5) || !a.limiter.allow("login-user:"+name, 20) || !a.limiter.allow("login:"+req.IP, 30) {
		return nil, fail(429, "rate_limited", "Too many login attempts; wait a minute.")
	}
	// argon2id takes 64 MiB per check: at most two at a time.
	select {
	case a.hashSlots <- struct{}{}:
		defer func() { <-a.hashSlots }()
	case <-time.After(3 * time.Second):
		return nil, fail(429, "rate_limited", "Busy; try again in a moment.")
	}
	u, err := store.GetUserByName(ctx, a.st.R, name)
	if err != nil || u.PasswordHash == "" || u.Disabled || u.Kind != "human" {
		auth.DummyVerify(in.Body.Password)
		a.audit(ctx, "auth.login_failed", map[string]any{"username": name})
		return nil, fail(401, "bad_credentials", "Wrong username or password.")
	}
	if !auth.VerifyPassword(u.PasswordHash, in.Body.Password) {
		a.audit(ctx, "auth.login_failed", map[string]any{"username": name})
		return nil, fail(401, "bad_credentials", "Wrong username or password.")
	}
	tok, err := auth.NewSessionToken()
	if err != nil {
		return nil, err
	}
	ua := ""
	if hc, ok := ctx.Value(uaKey{}).(string); ok {
		ua = hc
	}
	var s *store.Session
	err = a.tx(ctx, func(tx *sql.Tx) error {
		var err error
		if s, err = store.CreateSession(ctx, tx, u.ID, auth.HashToken(tok), req.IP, ua); err != nil {
			return err
		}
		_, err = store.Activity(ctx, tx, store.Actor{UserID: u.ID, Listener: req.Listener, IP: req.IP, RequestID: req.RequestID},
			nil, nil, "auth.login", "user", u.ID, nil)
		return err
	})
	if err != nil {
		return nil, err
	}
	p, err := access.Load(ctx, a.st.R, u, nil, s.ID, req)
	if err != nil {
		return nil, err
	}
	b, err := a.meBody(ctx, p)
	if err != nil {
		return nil, err
	}
	return &loginOut{SetCookie: a.cookie(tok, int(store.SessionTTL/1000)), Body: b}, nil
}

type uaKey struct{}

// WithUserAgent passes the User-Agent down for session records.
func WithUserAgent(ctx context.Context, ua string) context.Context {
	return context.WithValue(ctx, uaKey{}, ua)
}

func (a *API) cookie(value string, maxAge int) http.Cookie {
	c := http.Cookie{Name: a.cookieName(), Value: value, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: maxAge}
	if a.cookieName() == cookieSecure {
		c.Secure = true
	}
	return c
}

func (a *API) logout(ctx context.Context, _ *struct{}) (*logoutOut, error) {
	p, err := me(ctx)
	if err != nil {
		return nil, err
	}
	if p.SessionID != 0 {
		_ = store.RevokeSession(ctx, a.st.W, p.SessionID, p.User.ID)
	}
	return &logoutOut{SetCookie: a.cookie("", -1)}, nil
}

func (a *API) changePassword(ctx context.Context, in *passwordIn) (*struct{}, error) {
	p, err := me(ctx)
	if err != nil {
		return nil, err
	}
	if p.Key != nil {
		return nil, errForbidden("Change passwords from a browser session.")
	}
	if !auth.VerifyPassword(p.User.PasswordHash, in.Body.Current) {
		return nil, fail(403, "bad_credentials", "The current password is wrong.")
	}
	h, err := auth.HashPassword(in.Body.New)
	if err != nil {
		return nil, fail(422, "invalid", err.Error())
	}
	return nil, a.tx(ctx, func(tx *sql.Tx) error { return store.SetPassword(ctx, tx, p.Actor(), p.User.ID, h) })
}

func (a *API) patchMe(ctx context.Context, in *mePatchIn) (*userOut, error) {
	p, err := me(ctx)
	if err != nil {
		return nil, err
	}
	if err := need(p, "profile:write"); err != nil {
		return nil, err
	}
	v, err := ifMatch(in.IfMatch)
	if err != nil {
		return nil, err
	}
	patch := store.UserPatch{DisplayName: in.Body.DisplayName.Val(), Description: in.Body.Description.Val(), Color: in.Body.Color.Ptr()}
	if in.Body.Preferences.Set && !in.Body.Preferences.Null {
		patch.Preferences = &in.Body.Preferences.Value
	}
	var u *store.User
	err = a.tx(ctx, func(tx *sql.Tx) error {
		var err error
		u, err = store.PatchUser(ctx, tx, p.Actor(), p.User.ID, v, patch)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &userOut{ETag: etag(u.Version), Body: u}, nil
}

// createKey enforces the no-escalation rule: a key can't create a key wider than itself.
func (a *API) createKey(ctx context.Context, p *access.Principal, in KeyCreate) (*keyCreatedOut, error) {
	n := store.NewKey{Name: in.Name, UserID: in.UserID, ProjectIDs: in.ProjectIDs, Scopes: in.Scopes,
		AllowedListeners: in.AllowedListeners, AllowedCIDRs: in.AllowedCIDRs, ExpiresAt: in.ExpiresAt, CreatedByUser: &p.User.ID}
	if n.ExpiresAt == 0 {
		n.ExpiresAt = store.Now() + int64(365*24*time.Hour/time.Millisecond)
	}
	if n.UserID == 0 {
		n.UserID = p.User.ID
	}
	target, err := store.GetUser(ctx, a.st.R, n.UserID)
	if err != nil {
		return nil, fail(422, "invalid", "No such user.")
	}
	if in.Workspace != "" {
		w, err := a.workspace(ctx, p, in.Workspace)
		if err != nil {
			return nil, err
		}
		n.WorkspaceID = &w.ID
		if !store.UserHas(ctx, a.st.R, target.ID, w.ID, "view") {
			return nil, fail(422, "invalid", "The key's user can't see that workspace.")
		}
	}
	if len(n.ProjectIDs) > 0 && n.WorkspaceID == nil {
		return nil, fail(422, "invalid", "Project narrowing needs a workspace.")
	}
	if k := p.Key; k != nil {
		n.CreatedByKey = &k.ID
		for _, s := range in.Scopes {
			if !slices.Contains(k.Scopes, s) {
				return nil, errForbidden("A key can't grant the " + s + " scope it doesn't have.")
			}
		}
		if k.WorkspaceID != nil && (n.WorkspaceID == nil || *n.WorkspaceID != *k.WorkspaceID) {
			return nil, errForbidden("A workspace key can only create keys for its own workspace.")
		}
		if len(k.ProjectIDs) > 0 {
			if len(n.ProjectIDs) == 0 {
				return nil, errForbidden("A narrowed key can only create keys narrowed to its projects.")
			}
			for _, pid := range n.ProjectIDs {
				if !slices.Contains(k.ProjectIDs, pid) {
					return nil, errForbidden("A narrowed key can only create keys narrowed to its projects.")
				}
			}
		}
		if k.AllowedListeners != nil {
			if n.AllowedListeners == nil {
				n.AllowedListeners = k.AllowedListeners
			}
			for _, l := range n.AllowedListeners {
				if !slices.Contains(k.AllowedListeners, l) {
					return nil, errForbidden("A key can't loosen its listener restriction.")
				}
			}
		}
		if k.AllowedCIDRs != nil {
			if n.AllowedCIDRs == nil {
				n.AllowedCIDRs = k.AllowedCIDRs
			}
			for _, c := range n.AllowedCIDRs {
				if !slices.Contains(k.AllowedCIDRs, c) {
					return nil, errForbidden("A key can't loosen its network restriction.")
				}
			}
		}
		if n.ExpiresAt > k.ExpiresAt {
			n.ExpiresAt = k.ExpiresAt
		}
	}
	tok, prefix, err := auth.NewKeyToken()
	if err != nil {
		return nil, err
	}
	n.Prefix, n.SecretHash = prefix, auth.HashToken(tok)
	var k *store.APIKey
	err = a.tx(ctx, func(tx *sql.Tx) error {
		var err error
		k, err = store.CreateKey(ctx, tx, p.Actor(), n)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &keyCreatedOut{Body: &KeyCreated{Key: k, Token: tok}}, nil
}

// rotateKey replaces a key's secret; own = only the caller's keys.
func (a *API) rotateKey(ctx context.Context, id int64, own bool) (*keyCreatedOut, error) {
	p, err := me(ctx)
	if err != nil {
		return nil, err
	}
	k, err := a.keyFor(ctx, p, id, own)
	if err != nil {
		return nil, err
	}
	if k.Revoked {
		return nil, fail(409, "conflict", "The key is revoked.")
	}
	// The caller receives the new secret, so a key may only rotate keys within its own reach.
	if p.Key != nil && k.ID != p.Key.ID {
		if err := within(p.Key, k); err != nil {
			return nil, err
		}
	}
	tok, prefix, err := auth.NewKeyToken()
	if err != nil {
		return nil, err
	}
	err = a.tx(ctx, func(tx *sql.Tx) error {
		var err error
		k, err = store.RotateKey(ctx, tx, p.Actor(), k, prefix, auth.HashToken(tok))
		return err
	})
	if err != nil {
		return nil, err
	}
	return &keyCreatedOut{Body: &KeyCreated{Key: k, Token: tok}}, nil
}

func (a *API) revokeKey(ctx context.Context, id int64, own bool) error {
	p, err := me(ctx)
	if err != nil {
		return err
	}
	k, err := a.keyFor(ctx, p, id, own)
	if err != nil {
		return err
	}
	return a.tx(ctx, func(tx *sql.Tx) error { return store.RevokeKey(ctx, tx, p.Actor(), k) })
}

// keyFor loads a key the caller may manage: its own keys (with keys:self; a key may only manage
// itself and the keys it created), or any key for admins (admin:keys).
func (a *API) keyFor(ctx context.Context, p *access.Principal, id int64, own bool) (*store.APIKey, error) {
	if !own {
		if err := adminWith(p, "admin:keys"); err != nil {
			return nil, err
		}
	}
	k, err := store.GetKey(ctx, a.st.R, id)
	if err != nil {
		return nil, errNotFound()
	}
	if own {
		if k.UserID != p.User.ID {
			return nil, errNotFound()
		}
		if err := need(p, "keys:self"); err != nil {
			return nil, err
		}
		if p.Key != nil && k.ID != p.Key.ID && (k.CreatedByKey == nil || *k.CreatedByKey != p.Key.ID) {
			return nil, errForbidden("A key can only manage itself and the keys it created.")
		}
		return k, nil
	}
	if err := adminWith(p, "admin:keys"); err != nil {
		return nil, err
	}
	return k, nil
}

// audit records an event outside any change (failed logins).
func (a *API) audit(ctx context.Context, action string, data map[string]any) {
	req := access.RequestFrom(ctx)
	_ = a.st.Tx(ctx, func(tx *sql.Tx) error {
		_, err := store.Activity(ctx, tx, store.Actor{Listener: req.Listener, IP: req.IP, RequestID: req.RequestID}, nil, nil, action, "", 0, data)
		return err
	})
}

// within reports whether target reaches no further than caller: scopes, workspace, projects,
// listener and network restrictions, expiry. Used where the caller would obtain target's secret.
func within(caller, target *store.APIKey) error {
	for _, sc := range target.Scopes {
		if !slices.Contains(caller.Scopes, sc) {
			return errForbidden("That key has the " + sc + " scope, which this key lacks.")
		}
	}
	if caller.WorkspaceID != nil && (target.WorkspaceID == nil || *target.WorkspaceID != *caller.WorkspaceID) {
		return errForbidden("That key reaches beyond this key's workspace.")
	}
	if len(caller.ProjectIDs) > 0 {
		if len(target.ProjectIDs) == 0 {
			return errForbidden("That key reaches beyond this key's projects.")
		}
		for _, pid := range target.ProjectIDs {
			if !slices.Contains(caller.ProjectIDs, pid) {
				return errForbidden("That key reaches beyond this key's projects.")
			}
		}
	}
	if caller.AllowedListeners != nil {
		if target.AllowedListeners == nil {
			return errForbidden("That key isn't restricted to this key's listeners.")
		}
		for _, l := range target.AllowedListeners {
			if !slices.Contains(caller.AllowedListeners, l) {
				return errForbidden("That key isn't restricted to this key's listeners.")
			}
		}
	}
	if caller.AllowedCIDRs != nil {
		if target.AllowedCIDRs == nil {
			return errForbidden("That key isn't restricted to this key's networks.")
		}
		for _, c := range target.AllowedCIDRs {
			if !slices.Contains(caller.AllowedCIDRs, c) {
				return errForbidden("That key isn't restricted to this key's networks.")
			}
		}
	}
	if target.ExpiresAt > caller.ExpiresAt {
		return errForbidden("That key outlives this key.")
	}
	return nil
}
