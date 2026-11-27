package api

import (
	"context"
	"database/sql"
	"slices"

	"github.com/danielgtaylor/huma/v2"

	"github.com/0x2alx/gigg/internal/access"
	"github.com/0x2alx/gigg/internal/auth"
	"github.com/0x2alx/gigg/internal/store"
)

type usersOut struct {
	Body struct {
		Items []*store.User `json:"items"`
	}
}

type userCreateIn struct {
	Body struct {
		Username    string `json:"username" pattern:"^[a-z][a-z0-9_-]{1,31}$"`
		Kind        string `json:"kind" enum:"human,agent"`
		DisplayName string `json:"display_name,omitempty" maxLength:"64"`
		Password    string `json:"password,omitempty" minLength:"12" maxLength:"256" doc:"Humans only; without one the user can't log in until an admin sets it"`
		IsAdmin     bool   `json:"is_admin,omitempty"`
	}
}

type userPatchIn struct {
	ID      int64  `path:"id"`
	IfMatch string `header:"If-Match"`
	Body    struct {
		DisplayName Opt[string] `json:"display_name,omitempty" maxLength:"64"`
		Description Opt[string] `json:"description,omitempty" maxLength:"200"`
		Color       Opt[string] `json:"color,omitempty"`
		IsAdmin     Opt[bool]   `json:"is_admin,omitempty"`
		Disabled    Opt[bool]   `json:"disabled,omitempty"`
	}
}

type userPasswordIn struct {
	ID   int64 `path:"id"`
	Body struct {
		Password string `json:"password" minLength:"12" maxLength:"256"`
	}
}

type keyPatchIn struct {
	ID int64 `path:"id"`
}

func (a *API) registerUsers() {
	huma.Register(a.h, huma.Operation{OperationID: "list-users", Method: "GET", Path: "/users", Tags: []string{"users"},
		Summary: "All users (admin) or the members of your workspaces"}, func(ctx context.Context, _ *struct{}) (*usersOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		var ids []int64
		if !p.AdminWith("admin:users") {
			if ids, err = a.coMembers(ctx, p); err != nil {
				return nil, err
			}
		}
		out := &usersOut{}
		out.Body.Items, err = store.ListUsers(ctx, a.st.R, ids)
		if out.Body.Items == nil {
			out.Body.Items = []*store.User{}
		}
		for _, u := range out.Body.Items {
			if p.AdminWith("admin:users") {
				u.ShowPasswordState() // the admin's user list flags people who can't log in yet
			} else if u.ID != p.User.ID {
				u.Preferences = nil
			}
		}
		return out, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "get-user", Method: "GET", Path: "/users/{id}", Tags: []string{"users"},
		Summary: "A user's profile (members of your workspaces, or any for admins)"}, func(ctx context.Context, in *idPath) (*userOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if !p.AdminWith("admin:users") && in.ID != p.User.ID {
			ids, err := a.coMembers(ctx, p)
			if err != nil {
				return nil, err
			}
			if !slices.Contains(ids, in.ID) {
				return nil, errNotFound()
			}
		}
		u, err := store.GetUser(ctx, a.st.R, in.ID)
		if err != nil {
			return nil, storeErr(err)
		}
		if p.AdminWith("admin:users") {
			u.ShowPasswordState()
		} else if u.ID != p.User.ID {
			u.Preferences = nil
		}
		return &userOut{ETag: etag(u.Version), Body: u}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "create-user", Method: "POST", Path: "/users", Tags: []string{"users"},
		Summary: "Create a user or agent (admin)", DefaultStatus: 201}, func(ctx context.Context, in *userCreateIn) (*userOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := adminWith(p, "admin:users"); err != nil {
			return nil, err
		}
		if (in.Body.IsAdmin || in.Body.Password != "") && p.Key != nil {
			return nil, errForbidden("Admin rights and passwords are set from an admin's browser session, not with a key.")
		}
		n := store.NewUser{Username: in.Body.Username, Kind: in.Body.Kind, DisplayName: in.Body.DisplayName, IsAdmin: in.Body.IsAdmin}
		if in.Body.Password != "" {
			if in.Body.Kind != "human" {
				return nil, fail(422, "invalid", "Agents have no password.")
			}
			release, err := a.hashSlot()
			if err != nil {
				return nil, err
			}
			n.PasswordHash, err = auth.HashPassword(in.Body.Password)
			release()
			if err != nil {
				return nil, fail(422, "invalid", err.Error())
			}
		}
		var u *store.User
		err = a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			u, err = store.CreateUser(ctx, tx, p.Actor(), n)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &userOut{ETag: etag(u.Version), Body: u}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "patch-user", Method: "PATCH", Path: "/users/{id}", Tags: []string{"users"},
		Summary: "Change a user's profile, admin flag or disabled state (admin)"}, func(ctx context.Context, in *userPatchIn) (*userOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := adminWith(p, "admin:users"); err != nil {
			return nil, err
		}
		v, err := ifMatch(in.IfMatch)
		if err != nil {
			return nil, err
		}
		if in.Body.IsAdmin.Set && p.Key != nil {
			return nil, errForbidden("Admin rights are changed from an admin's browser session, not with a key.")
		}
		patch := store.UserPatch{DisplayName: in.Body.DisplayName.Val(), Description: in.Body.Description.Val(), Color: in.Body.Color.Ptr(),
			IsAdmin: in.Body.IsAdmin.Val(), Disabled: in.Body.Disabled.Val()}
		var u *store.User
		err = a.tx(ctx, func(tx *sql.Tx) error {
			cur, err := store.GetUser(ctx, tx, in.ID)
			if err != nil {
				return err
			}
			losing := (patch.IsAdmin != nil && !*patch.IsAdmin) || (patch.Disabled != nil && *patch.Disabled)
			if cur.IsAdmin && !cur.Disabled && losing {
				if n, err := store.AdminCount(ctx, tx); err != nil || n <= 1 {
					return store.Conflict("this is the last admin")
				}
			}
			u, err = store.PatchUser(ctx, tx, p.Actor(), in.ID, v, patch)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &userOut{ETag: etag(u.Version), Body: u}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "set-user-password", Method: "POST", Path: "/users/{id}/password", Tags: []string{"users"},
		Summary: "Set a person's password (admin; logs out their sessions)", DefaultStatus: 204}, func(ctx context.Context, in *userPasswordIn) (*struct{}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := adminWith(p, "admin:users"); err != nil {
			return nil, err
		}
		if p.Key != nil {
			return nil, errForbidden("Passwords are set from an admin's browser session, not with a key.")
		}
		release, err := a.hashSlot()
		if err != nil {
			return nil, err
		}
		h, err := auth.HashPassword(in.Body.Password)
		release()
		if err != nil {
			return nil, fail(422, "invalid", err.Error())
		}
		return nil, a.tx(ctx, func(tx *sql.Tx) error { return store.SetPassword(ctx, tx, p.Actor(), in.ID, h) })
	})
	huma.Register(a.h, huma.Operation{OperationID: "list-user-sessions", Method: "GET", Path: "/users/{id}/sessions", Tags: []string{"users"},
		Summary: "A user's sessions (admin)"}, func(ctx context.Context, in *idPath) (*sessionsOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := adminWith(p, "admin:users"); err != nil {
			return nil, err
		}
		out := &sessionsOut{}
		out.Body.Items, err = store.ListSessions(ctx, a.st.R, in.ID)
		if out.Body.Items == nil {
			out.Body.Items = []*store.Session{}
		}
		return out, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "revoke-any-session", Method: "DELETE", Path: "/sessions/{id}", Tags: []string{"users"},
		Summary: "End any session (admin)", DefaultStatus: 204}, func(ctx context.Context, in *idPath) (*struct{}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := adminWith(p, "admin:users"); err != nil {
			return nil, err
		}
		return nil, storeErr(store.RevokeSession(ctx, a.st.W, in.ID, 0))
	})

	// Admin key management.
	huma.Register(a.h, huma.Operation{OperationID: "list-keys", Method: "GET", Path: "/keys", Tags: []string{"keys"},
		Summary: "Every key (admin)"}, func(ctx context.Context, _ *struct{}) (*keysOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := adminWith(p, "admin:keys"); err != nil {
			return nil, err
		}
		out := &keysOut{}
		out.Body.Items, err = store.ListKeys(ctx, a.st.R, 0)
		if out.Body.Items == nil {
			out.Body.Items = []*store.APIKey{}
		}
		return out, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "create-key", Method: "POST", Path: "/keys", Tags: []string{"keys"},
		Summary: "Create a key for any user (admin)", DefaultStatus: 201}, func(ctx context.Context, in *keyCreateIn) (*keyCreatedOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := adminWith(p, "admin:keys"); err != nil {
			return nil, err
		}
		return a.createKey(ctx, p, in.Body)
	})
	huma.Register(a.h, huma.Operation{OperationID: "get-key", Method: "GET", Path: "/keys/{id}", Tags: []string{"keys"},
		Summary: "One key (admin)"}, func(ctx context.Context, in *keyPatchIn) (*struct{ Body *store.APIKey }, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		k, err := a.keyFor(ctx, p, in.ID, false)
		if err != nil {
			return nil, err
		}
		return &struct{ Body *store.APIKey }{Body: k}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "rotate-key", Method: "POST", Path: "/keys/{id}/rotate", Tags: []string{"keys"},
		Summary: "Replace any key's secret (admin)"}, func(ctx context.Context, in *idPath) (*keyCreatedOut, error) {
		return a.rotateKey(ctx, in.ID, false)
	})
	huma.Register(a.h, huma.Operation{OperationID: "revoke-key", Method: "DELETE", Path: "/keys/{id}", Tags: []string{"keys"},
		Summary: "Revoke any key (admin)", DefaultStatus: 204}, func(ctx context.Context, in *idPath) (*struct{}, error) {
		return nil, a.revokeKey(ctx, in.ID, false)
	})
}

// coMembers lists users who share a workspace with the caller (and the caller).
func (a *API) coMembers(ctx context.Context, p *access.Principal) ([]int64, error) {
	ws := p.Workspaces()
	ids := []int64{p.User.ID}
	if ws != nil && len(ws) == 0 {
		return ids, nil
	}
	if ws == nil { // every workspace (an admin whose key lacks admin:users): the members of all of them
		all, err := store.ListWorkspaces(ctx, a.st.R, nil)
		if err != nil {
			return nil, err
		}
		for _, w := range all {
			ws = append(ws, w.ID)
		}
	}
	for _, w := range ws {
		ms, err := store.Members(ctx, a.st.R, w)
		if err != nil {
			return nil, err
		}
		for _, m := range ms {
			if !slices.Contains(ids, m.UserID) {
				ids = append(ids, m.UserID)
			}
		}
	}
	// admins appear as authors everywhere; their profiles are visible to everyone in reach
	admins, err := a.adminIDs(ctx)
	if err != nil {
		return nil, err
	}
	for _, id := range admins {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (a *API) adminIDs(ctx context.Context) ([]int64, error) {
	rows, err := a.st.R.QueryContext(ctx, `SELECT id FROM users WHERE is_admin = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
