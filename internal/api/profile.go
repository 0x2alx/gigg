package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/0x2alx/gigg/internal/access"
	"github.com/0x2alx/gigg/internal/avatar"
	"github.com/0x2alx/gigg/internal/store"
	"github.com/0x2alx/gigg/internal/theme"
)

type themesOut struct {
	Body *store.ThemeState
}

type themePutIn struct {
	ID   string `path:"theme_id" pattern:"^[A-Za-z0-9_-]{1,40}$"`
	Body struct {
		Theme json.RawMessage `json:"theme" doc:"A complete theme in the shared app format; stored after sanitising"`
	}
}

// ThemeSaved is the stored theme and whether older themes were dropped (50 max).
type ThemeSaved struct {
	ID      string          `json:"id"`
	Theme   json.RawMessage `json:"theme"`
	Dropped bool            `json:"dropped_oldest"`
}

type themeIDIn struct {
	ID string `path:"theme_id"`
}

type selectionIn struct {
	Body struct {
		SelectedID  *string `json:"selected_id,omitempty" maxLength:"40"`
		MatchSystem bool    `json:"match_system"`
	}
}

type avatarIn struct {
	ContentType string `header:"Content-Type"`
	RawBody     []byte `contentType:"image/png"`
}

type avatarGetIn struct {
	ID int64 `path:"id"`
}

type avatarOut struct {
	ContentType  string `header:"Content-Type"`
	CacheControl string `header:"Cache-Control"`
	ETag         string `header:"ETag"`
	CSP          string `header:"Content-Security-Policy"`
	Body         []byte
}

func (a *API) registerProfile() {
	huma.Register(a.h, huma.Operation{OperationID: "my-themes", Method: "GET", Path: "/me/themes", Tags: []string{"profile"},
		Summary: "Your custom themes (newest first) and selection"}, func(ctx context.Context, _ *struct{}) (*themesOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		st, err := store.Themes(ctx, a.st.R, p.User.ID)
		return &themesOut{Body: st}, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "put-my-theme", Method: "PUT", Path: "/me/themes/{theme_id}", Tags: []string{"profile"},
		Summary: "Save a custom theme (sanitised; at most 50, the oldest go)"}, func(ctx context.Context, in *themePutIn) (*struct{ Body *ThemeSaved }, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := need(p, "profile:write"); err != nil {
			return nil, err
		}
		if !theme.ValidID(in.ID) {
			return nil, fail(422, "invalid", "Theme ids are 1-40 letters, digits, - or _, and not a built-in's id.")
		}
		clean, err := theme.Sane(in.Body.Theme)
		if err != nil {
			return nil, fail(422, "invalid", err.Error())
		}
		var dropped bool
		err = a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			dropped, err = store.PutTheme(ctx, tx, p.Actor(), p.User.ID, in.ID, clean)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &struct{ Body *ThemeSaved }{Body: &ThemeSaved{ID: in.ID, Theme: clean, Dropped: dropped}}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "delete-my-theme", Method: "DELETE", Path: "/me/themes/{theme_id}", Tags: []string{"profile"},
		Summary: "Delete one of your custom themes", DefaultStatus: 204}, func(ctx context.Context, in *themeIDIn) (*struct{}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := need(p, "profile:write"); err != nil {
			return nil, err
		}
		return nil, storeErr(store.DeleteTheme(ctx, a.st.W, p.User.ID, in.ID))
	})
	huma.Register(a.h, huma.Operation{OperationID: "select-my-theme", Method: "PUT", Path: "/me/themes-selection", Tags: []string{"profile"},
		Summary: "Choose your theme (a built-in or custom id) and the match-the-system switch", DefaultStatus: 204}, func(ctx context.Context, in *selectionIn) (*struct{}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := need(p, "profile:write"); err != nil {
			return nil, err
		}
		if id := in.Body.SelectedID; id != nil {
			low := strings.ToLower(*id)
			if r := theme.Retired[low]; r != "" {
				low = r
			}
			if theme.Builtin[low] {
				*id = low
			} else {
				st, err := store.Themes(ctx, a.st.R, p.User.ID)
				if err != nil {
					return nil, storeErr(err)
				}
				if !slices.ContainsFunc(st.Custom, func(t store.UserTheme) bool { return t.ID == *id }) {
					return nil, fail(422, "invalid", "No theme with that id.")
				}
			}
		}
		return nil, storeErr(store.SelectTheme(ctx, a.st.W, p.User.ID, in.Body.SelectedID, in.Body.MatchSystem))
	})

	huma.Register(a.h, huma.Operation{OperationID: "put-my-avatar", Method: "PUT", Path: "/me/avatar", Tags: []string{"profile"},
		Summary: "Upload your picture (PNG, JPEG, WebP or GIF, up to 2 MB; stored as a 256 px PNG)", DefaultStatus: 204,
		MaxBodyBytes: avatar.MaxBytes + 1024}, func(ctx context.Context, in *avatarIn) (*struct{}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := need(p, "profile:write"); err != nil {
			return nil, err
		}
		return nil, a.setAvatar(ctx, p, p.User.ID, in.RawBody)
	})
	huma.Register(a.h, huma.Operation{OperationID: "delete-my-avatar", Method: "DELETE", Path: "/me/avatar", Tags: []string{"profile"},
		Summary: "Remove your picture", DefaultStatus: 204}, func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := need(p, "profile:write"); err != nil {
			return nil, err
		}
		return nil, a.tx(ctx, func(tx *sql.Tx) error { return store.SetAvatar(ctx, tx, p.Actor(), p.User.ID, "", 0) })
	})
	huma.Register(a.h, huma.Operation{OperationID: "put-user-avatar", Method: "PUT", Path: "/users/{id}/avatar", Tags: []string{"users"},
		Summary: "Set an agent's picture (admin; people set their own)", DefaultStatus: 204, MaxBodyBytes: avatar.MaxBytes + 1024},
		func(ctx context.Context, in *struct {
			ID          int64  `path:"id"`
			ContentType string `header:"Content-Type"`
			RawBody     []byte `contentType:"image/png"`
		}) (*struct{}, error) {
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			if err := adminWith(p, "admin:users"); err != nil {
				return nil, err
			}
			if err := a.agentTarget(ctx, p, in.ID); err != nil {
				return nil, err
			}
			return nil, a.setAvatar(ctx, p, in.ID, in.RawBody)
		})
	huma.Register(a.h, huma.Operation{OperationID: "delete-user-avatar", Method: "DELETE", Path: "/users/{id}/avatar", Tags: []string{"users"},
		Summary: "Remove an agent's picture (admin)", DefaultStatus: 204}, func(ctx context.Context, in *idPath) (*struct{}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := adminWith(p, "admin:users"); err != nil {
			return nil, err
		}
		if err := a.agentTarget(ctx, p, in.ID); err != nil {
			return nil, err
		}
		return nil, a.tx(ctx, func(tx *sql.Tx) error { return store.SetAvatar(ctx, tx, p.Actor(), in.ID, "", 0) })
	})
	huma.Register(a.h, huma.Operation{OperationID: "get-user-avatar", Method: "GET", Path: "/users/{id}/avatar", Tags: []string{"users"},
		Summary: "A user's picture (PNG)"}, func(ctx context.Context, in *avatarGetIn) (*avatarOut, error) {
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
		sha, err := store.AvatarSHA(ctx, a.st.R, in.ID)
		if err != nil || sha == "" {
			return nil, errNotFound()
		}
		data, err := os.ReadFile(a.st.BlobPath(sha))
		if err != nil {
			return nil, errNotFound()
		}
		return &avatarOut{ContentType: "image/png", CacheControl: "private, max-age=86400", ETag: `"` + sha[:16] + `"`,
			CSP: "default-src 'none'; sandbox", Body: data}, nil
	})
}

func (a *API) setAvatar(ctx context.Context, p *access.Principal, user int64, raw []byte) error {
	// decoding can take ~64 MiB for a 4096 px image: share the two heavy-work slots with login
	select {
	case a.hashSlots <- struct{}{}:
		defer func() { <-a.hashSlots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	png, err := avatar.Normalize(raw)
	if err != nil {
		return fail(422, "invalid", err.Error())
	}
	sha, err := a.st.WriteBlob(png)
	if err != nil {
		return err
	}
	return a.tx(ctx, func(tx *sql.Tx) error { return store.SetAvatar(ctx, tx, p.Actor(), user, sha, len(png)) })
}

// agentTarget: admins manage pictures of agents only; a person's picture is theirs to set.
func (a *API) agentTarget(ctx context.Context, p *access.Principal, id int64) error {
	u, err := store.GetUser(ctx, a.st.R, id)
	if err != nil {
		return errNotFound()
	}
	if u.Kind != "agent" && u.ID != p.User.ID {
		return errForbidden("People set their own picture (Profile); admins can set pictures for agents.")
	}
	return nil
}
