package api

import (
	"context"
	"database/sql"

	"github.com/danielgtaylor/huma/v2"

	"github.com/0x2alx/gigg/internal/access"
	"github.com/0x2alx/gigg/internal/store"
)

// Groups and access rules. Reading needs admin:users; any change that alters what an account may do
// (rules, group membership) is made from an admin's browser session, never with a key.

type groupOut struct {
	ETag string `header:"ETag"`
	Body *store.Group
}

// RuleBody sets one rule: a preset and/or overrides at one level.
type RuleBody struct {
	WorkspaceID *int64          `json:"workspace_id" doc:"The workspace, or null for all workspaces (future ones too)"`
	Preset      string          `json:"preset,omitempty" enum:",viewer,consumer,member,manager" doc:"Fills in permissions; empty = none"`
	Overrides   map[string]bool `json:"overrides,omitempty" doc:"permission -> true (allow) or false (deny); absent = the preset decides, else inherit"`
}

// UserAccess is the resolved view of one account.
type UserAccess struct {
	UserID int64               `json:"user_id"`
	Groups []string            `json:"groups"`
	Rules  []*store.AccessRule `json:"rules" doc:"The user's own rules"`
	Grid   []store.AccessRow   `json:"grid" doc:"Every permission in every workspace (workspace_id 0 = what a new workspace starts with), with where each answer comes from"`
}

// PermissionsInfo describes the access model.
type PermissionsInfo struct {
	Permissions []store.Permission  `json:"permissions"`
	Presets     map[string][]string `json:"presets"`
	Roles       []string            `json:"roles" doc:"Presets in increasing order"`
}

func sessionOnly(p *access.Principal, what string) error {
	if p.Key != nil {
		return errForbidden(what + " are changed from an admin's browser session, not with a key.")
	}
	return nil
}

func (a *API) registerGroups() {
	huma.Register(a.h, huma.Operation{OperationID: "list-permissions", Method: "GET", Path: "/permissions", Tags: []string{"users"},
		Summary: "Every permission and preset (roles). Rule order: admin > the user's rule for the workspace > the user's rule for all workspaces > groups for the workspace > groups for all workspaces > off"},
		func(ctx context.Context, _ *struct{}) (*struct{ Body PermissionsInfo }, error) {
			if _, err := me(ctx); err != nil {
				return nil, err
			}
			return &struct{ Body PermissionsInfo }{PermissionsInfo{Permissions: store.Permissions, Presets: store.Presets, Roles: store.Roles}}, nil
		})
	huma.Register(a.h, huma.Operation{OperationID: "get-user-access", Method: "GET", Path: "/users/{id}/access", Tags: []string{"users"},
		Summary: "A user's rules and resolved permissions per workspace, with where each comes from (admin)"}, func(ctx context.Context, in *idPath) (*struct{ Body *UserAccess }, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := adminWith(p, "admin:users"); err != nil {
			return nil, err
		}
		return a.userAccess(ctx, in.ID)
	})
	huma.Register(a.h, huma.Operation{OperationID: "set-user-access", Method: "PUT", Path: "/users/{id}/access", Tags: []string{"users"},
		Summary: "Set (or, with no preset and no overrides, remove) one of a user's rules (admin, browser session)"},
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body RuleBody
		}) (*struct{ Body *UserAccess }, error) {
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			if err := adminWith(p, "admin:users"); err != nil {
				return nil, err
			}
			if err := sessionOnly(p, "Permissions"); err != nil {
				return nil, err
			}
			err = a.tx(ctx, func(tx *sql.Tx) error {
				if _, err := store.GetUser(ctx, tx, in.ID); err != nil {
					return err
				}
				return store.SetRule(ctx, tx, p.Actor(), "user", in.ID, in.Body.WorkspaceID, in.Body.Preset, in.Body.Overrides)
			})
			if err != nil {
				return nil, err
			}
			return a.userAccess(ctx, in.ID)
		})
	huma.Register(a.h, huma.Operation{OperationID: "set-group-access", Method: "PUT", Path: "/groups/{id}/access", Tags: []string{"users"},
		Summary: "Set (or, with no preset and no overrides, remove) one of a group's rules (admin, browser session)"},
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body RuleBody
		}) (*groupOut, error) {
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			if err := adminWith(p, "admin:users"); err != nil {
				return nil, err
			}
			if err := sessionOnly(p, "Permissions"); err != nil {
				return nil, err
			}
			var g *store.Group
			err = a.tx(ctx, func(tx *sql.Tx) error {
				if _, err := store.GetGroup(ctx, tx, in.ID); err != nil {
					return err
				}
				if err := store.SetRule(ctx, tx, p.Actor(), "group", in.ID, in.Body.WorkspaceID, in.Body.Preset, in.Body.Overrides); err != nil {
					return err
				}
				g, err = store.GetGroup(ctx, tx, in.ID)
				return err
			})
			if err != nil {
				return nil, err
			}
			return &groupOut{ETag: etag(g.Version), Body: g}, nil
		})
	huma.Register(a.h, huma.Operation{OperationID: "list-groups", Method: "GET", Path: "/groups", Tags: []string{"users"},
		Summary: "Groups with their members and access rules (admin)"}, func(ctx context.Context, _ *struct{}) (*struct {
		Body struct {
			Items []*store.Group `json:"items"`
		}
	}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := adminWith(p, "admin:users"); err != nil {
			return nil, err
		}
		out := &struct {
			Body struct {
				Items []*store.Group `json:"items"`
			}
		}{}
		out.Body.Items, err = store.ListGroups(ctx, a.st.R)
		return out, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "create-group", Method: "POST", Path: "/groups", Tags: []string{"users"},
		Summary: "Create a group (admin, browser session)", DefaultStatus: 201}, func(ctx context.Context, in *struct {
		Body struct {
			Name        string  `json:"name" minLength:"1" maxLength:"60"`
			Description string  `json:"description,omitempty" maxLength:"500"`
			Members     []int64 `json:"members,omitempty"`
		}
	}) (*groupOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := adminWith(p, "admin:users"); err != nil {
			return nil, err
		}
		if err := sessionOnly(p, "Groups"); err != nil {
			return nil, err
		}
		var g *store.Group
		err = a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			if g, err = store.CreateGroup(ctx, tx, p.Actor(), in.Body.Name, in.Body.Description); err != nil {
				return err
			}
			if in.Body.Members != nil {
				if g, err = store.UpdateGroup(ctx, tx, p.Actor(), g, 0, nil, nil, in.Body.Members); err != nil {
					return err
				}
			}
			g, err = store.GetGroup(ctx, tx, g.ID)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &groupOut{ETag: etag(g.Version), Body: g}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "update-group", Method: "PATCH", Path: "/groups/{id}", Tags: []string{"users"},
		Summary: "Rename a group and/or replace its members (admin, browser session)"},
		func(ctx context.Context, in *struct {
			ID      int64  `path:"id"`
			IfMatch string `header:"If-Match"`
			Body    struct {
				Name        *string `json:"name,omitempty" maxLength:"60"`
				Description *string `json:"description,omitempty" maxLength:"500"`
				Members     []int64 `json:"members,omitempty" doc:"Replaces the member list"`
			}
		}) (*groupOut, error) {
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			if err := adminWith(p, "admin:users"); err != nil {
				return nil, err
			}
			if err := sessionOnly(p, "Groups"); err != nil {
				return nil, err
			}
			v, err := ifMatch(in.IfMatch)
			if err != nil {
				return nil, err
			}
			var g *store.Group
			err = a.tx(ctx, func(tx *sql.Tx) error {
				cur, err := store.GetGroup(ctx, tx, in.ID)
				if err != nil {
					return err
				}
				if g, err = store.UpdateGroup(ctx, tx, p.Actor(), cur, v, in.Body.Name, in.Body.Description, in.Body.Members); err != nil {
					return err
				}
				g, err = store.GetGroup(ctx, tx, g.ID)
				return err
			})
			if err != nil {
				return nil, err
			}
			return &groupOut{ETag: etag(g.Version), Body: g}, nil
		})
	huma.Register(a.h, huma.Operation{OperationID: "delete-group", Method: "DELETE", Path: "/groups/{id}", Tags: []string{"users"},
		Summary: "Delete a group (admin, browser session)", DefaultStatus: 204}, func(ctx context.Context, in *idPath) (*struct{}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := adminWith(p, "admin:users"); err != nil {
			return nil, err
		}
		if err := sessionOnly(p, "Groups"); err != nil {
			return nil, err
		}
		return nil, a.tx(ctx, func(tx *sql.Tx) error {
			g, err := store.GetGroup(ctx, tx, in.ID)
			if err != nil {
				return err
			}
			return store.DeleteGroup(ctx, tx, p.Actor(), g)
		})
	})
}

func (a *API) userAccess(ctx context.Context, id int64) (*struct{ Body *UserAccess }, error) {
	_, g, err := store.UserGrants(ctx, a.st.R, id)
	if err != nil {
		return nil, storeErr(err)
	}
	rules, err := store.RulesOfUser(ctx, a.st.R, id)
	if err != nil {
		return nil, err
	}
	groups, err := store.GroupsOf(ctx, a.st.R, id)
	if err != nil {
		return nil, err
	}
	return &struct{ Body *UserAccess }{Body: &UserAccess{UserID: id, Groups: groups, Rules: rules, Grid: g.AccessGrid()}}, nil
}
