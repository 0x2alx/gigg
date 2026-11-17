package api

import (
	"context"
	"database/sql"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/0x2alx/gigg/internal/access"
	"github.com/0x2alx/gigg/internal/store"
)

// workspace resolves an id, a slug or "current" (a workspace key's own workspace) within reach.
func (a *API) workspace(ctx context.Context, p *access.Principal, ref string) (*store.Workspace, error) {
	var w *store.Workspace
	var err error
	switch {
	case ref == "current":
		if p.Key == nil || p.Key.WorkspaceID == nil {
			return nil, fail(422, "invalid", `"current" needs a workspace key.`)
		}
		w, err = store.GetWorkspace(ctx, a.st.R, *p.Key.WorkspaceID)
	default:
		if id, e := strconv.ParseInt(ref, 10, 64); e == nil {
			w, err = store.GetWorkspace(ctx, a.st.R, id)
		} else {
			w, err = store.GetWorkspaceBySlug(ctx, a.st.R, ref)
		}
	}
	if err != nil || !p.SeesWorkspace(w.ID) {
		return nil, errNotFound()
	}
	return w, nil
}

// project resolves a project id within reach (including the key's project narrowing).
func (a *API) project(ctx context.Context, p *access.Principal, id int64) (*store.Project, error) {
	pr, err := store.GetProject(ctx, a.st.R, id)
	if err != nil || pr.Deleted || !p.SeesProject(pr.WorkspaceID, pr.ID) {
		return nil, errNotFound()
	}
	return pr, nil
}

// require: visible already; now the permission and scope (403 when missing).
func require(p *access.Principal, ws int64, perm, scope string) error {
	if p.Can(ws, perm, scope) {
		return nil
	}
	if !p.HasScope(scope) {
		return errForbidden("This key lacks the " + scope + " scope.")
	}
	return errNoPermission(perm)
}

type wsPath struct {
	WS string `path:"ws" doc:"Workspace id, slug, or current"`
}

type workspaceOut struct {
	ETag string `header:"ETag"`
	Body *store.Workspace
}

type workspacesOut struct {
	Body struct {
		Items []*store.Workspace `json:"items"`
	}
}

type workspaceCreateIn struct {
	Body struct {
		Slug           string                 `json:"slug" pattern:"^[a-z][a-z0-9-]{1,31}$"`
		Name           string                 `json:"name" minLength:"1" maxLength:"64"`
		StatusTemplate []store.StatusTemplate `json:"status_template,omitempty"`
	}
}

type workspacePatchIn struct {
	WS      string `path:"ws"`
	IfMatch string `header:"If-Match"`
	Body    struct {
		Name           Opt[string]                 `json:"name,omitempty" maxLength:"64"`
		Color          Opt[string]                 `json:"color,omitempty"`
		DescriptionMD  Opt[string]                 `json:"description_md,omitempty"`
		StatusTemplate Opt[[]store.StatusTemplate] `json:"status_template,omitempty"`
		Archived       Opt[bool]                   `json:"archived,omitempty"`
		OwnerID        Opt[int64]                  `json:"owner_id,omitempty" doc:"The user the workspace belongs to (admins; null clears)"`
	}
}

type membersOut struct {
	Body struct {
		Items []store.Membership `json:"items"`
	}
}

type memberPutIn struct {
	WS   string `path:"ws"`
	User int64  `path:"user"`
	Body struct {
		Role string `json:"role" enum:"viewer,consumer,member,manager"`
	}
}

type memberDelIn struct {
	WS   string `path:"ws"`
	User int64  `path:"user"`
}

type labelsOut struct {
	Body struct {
		Items []*store.Label `json:"items"`
	}
}

type labelCreateIn struct {
	WS   string `path:"ws"`
	Body struct {
		Name        string  `json:"name" minLength:"1" maxLength:"40"`
		Color       *string `json:"color,omitempty"`
		Description string  `json:"description,omitempty" maxLength:"200"`
	}
}

type labelOut struct {
	ETag string `header:"ETag"`
	Body *store.Label
}

type labelPatchIn struct {
	ID      int64  `path:"id"`
	IfMatch string `header:"If-Match"`
	Body    struct {
		Name        Opt[string] `json:"name,omitempty" maxLength:"40"`
		Color       Opt[string] `json:"color,omitempty"`
		Description Opt[string] `json:"description,omitempty" maxLength:"200"`
	}
}

type projectsOut struct {
	Body struct {
		Items []*store.Project `json:"items"`
	}
}

type projectCreateIn struct {
	WS   string `path:"ws"`
	Body struct {
		Slug     string                 `json:"slug" pattern:"^[a-z][a-z0-9-]{1,31}$"`
		Name     string                 `json:"name" minLength:"1" maxLength:"64"`
		Key      string                 `json:"key" pattern:"^[A-Z][A-Z0-9]{1,4}$" doc:"Gig key prefix, e.g. GIG"`
		Statuses []store.StatusTemplate `json:"statuses,omitempty" doc:"Default: the workspace's status template"`
	}
}

type projectOut struct {
	ETag string `header:"ETag"`
	Body *store.Project
}

type projectPath struct {
	P int64 `path:"p"`
}

type projectPatchIn struct {
	P       int64  `path:"p"`
	IfMatch string `header:"If-Match"`
	Body    struct {
		Name          Opt[string] `json:"name,omitempty" maxLength:"64"`
		Color         Opt[string] `json:"color,omitempty"`
		DescriptionMD Opt[string] `json:"description_md,omitempty"`
		Archived      Opt[bool]   `json:"archived,omitempty"`
	}
}

type statusesOut struct {
	Body struct {
		Items []*store.Status `json:"items"`
	}
}

type statusCreateIn struct {
	P    int64 `path:"p"`
	Body struct {
		Name     string  `json:"name" minLength:"1" maxLength:"40"`
		Category string  `json:"category" enum:"backlog,todo,doing,waiting,done,cancelled"`
		Color    *string `json:"color,omitempty"`
	}
}

type statusOut struct {
	ETag string `header:"ETag"`
	Body *store.Status
}

type statusPatchIn struct {
	ID      int64  `path:"id"`
	IfMatch string `header:"If-Match"`
	Body    struct {
		Name      Opt[string] `json:"name,omitempty" maxLength:"40"`
		Category  Opt[string] `json:"category,omitempty" enum:"backlog,todo,doing,waiting,done,cancelled"`
		Color     Opt[string] `json:"color,omitempty"`
		WIPLimit  Opt[int64]  `json:"wip_limit,omitempty"`
		IsDefault Opt[bool]   `json:"is_default,omitempty"`
	}
}

type reorderIn struct {
	P    int64 `path:"p"`
	Body struct {
		IDs []int64 `json:"ids" doc:"Every status id of the project, in the new order"`
	}
}

// Board is a project with its statuses and gigs.
type Board struct {
	Project  *store.Project `json:"project"`
	Statuses []*BoardColumn `json:"statuses"`
	Labels   []*store.Label `json:"labels"`
	// Truncated: the project has more unarchived gigs than a board shows (boardMax); the rest are in lists
	Truncated bool `json:"truncated,omitempty"`
}

// boardMax bounds one board answer; past it the board says so (Truncated) instead of dropping cards silently.
const boardMax = 5000

// BoardColumn is a status with its gigs.
type BoardColumn struct {
	*store.Status
	Gigs []*store.Gig `json:"gigs"`
}

func (a *API) registerWorkspaces() {
	huma.Register(a.h, huma.Operation{OperationID: "list-workspaces", Method: "GET", Path: "/workspaces", Tags: []string{"workspaces"},
		Summary: "Workspaces in reach"}, func(ctx context.Context, _ *struct{}) (*workspacesOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		out := &workspacesOut{}
		out.Body.Items, err = store.ListWorkspaces(ctx, a.st.R, p.Workspaces())
		if out.Body.Items == nil {
			out.Body.Items = []*store.Workspace{}
		}
		return out, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "create-workspace", Method: "POST", Path: "/workspaces", Tags: []string{"workspaces"},
		Summary: "Create a workspace (admin)", DefaultStatus: 201}, func(ctx context.Context, in *workspaceCreateIn) (*workspaceOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := adminWith(p, "admin:workspaces"); err != nil {
			return nil, err
		}
		var w *store.Workspace
		err = a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			w, err = store.CreateWorkspace(ctx, tx, p.Actor(), in.Body.Slug, in.Body.Name, in.Body.StatusTemplate)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &workspaceOut{ETag: etag(w.Version), Body: w}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "get-workspace", Method: "GET", Path: "/workspaces/{ws}", Tags: []string{"workspaces"},
		Summary: "A workspace"}, func(ctx context.Context, in *wsPath) (*workspaceOut, error) {
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
		return &workspaceOut{ETag: etag(w.Version), Body: w}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "patch-workspace", Method: "PATCH", Path: "/workspaces/{ws}", Tags: []string{"workspaces"},
		Summary: "Change a workspace (managers: name, colour, description, template; admins: archive, owner)"}, func(ctx context.Context, in *workspacePatchIn) (*workspaceOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		w, err := a.workspace(ctx, p, in.WS)
		if err != nil {
			return nil, err
		}
		if err := require(p, w.ID, access.Manager, "projects:write"); err != nil {
			return nil, err
		}
		if in.Body.Archived.Set || in.Body.OwnerID.Set {
			if err := adminWith(p, "admin:workspaces"); err != nil {
				return nil, err
			}
		}
		if p.Narrowed() {
			return nil, errForbidden("A key narrowed to projects can't change the workspace.")
		}
		v, err := ifMatch(in.IfMatch)
		if err != nil {
			return nil, err
		}
		patch := store.WorkspacePatch{Name: in.Body.Name.Val(), Color: in.Body.Color.Ptr(), DescriptionMD: in.Body.DescriptionMD.Val(), Archived: in.Body.Archived.Val(), OwnerID: in.Body.OwnerID.Ptr()}
		if t := in.Body.StatusTemplate.Val(); t != nil {
			patch.StatusTemplate = *t
		}
		err = a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			w, err = store.PatchWorkspace(ctx, tx, p.Actor(), w.ID, v, patch)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &workspaceOut{ETag: etag(w.Version), Body: w}, nil
	})

	huma.Register(a.h, huma.Operation{OperationID: "list-members", Method: "GET", Path: "/workspaces/{ws}/members", Tags: []string{"workspaces"},
		Summary: "A workspace's members and roles"}, func(ctx context.Context, in *wsPath) (*membersOut, error) {
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
		out := &membersOut{}
		out.Body.Items, err = store.Members(ctx, a.st.R, w.ID)
		if out.Body.Items == nil {
			out.Body.Items = []store.Membership{}
		}
		if !p.AdminWith("admin:members") { // how others got their access is admin business
			for i := range out.Body.Items {
				out.Body.Items[i].Preset, out.Body.Items[i].Source = "", ""
			}
		}
		return out, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "set-member", Method: "PUT", Path: "/workspaces/{ws}/members/{user}", Tags: []string{"workspaces"},
		Summary: "Add a member or change their role (admin)", DefaultStatus: 204}, func(ctx context.Context, in *memberPutIn) (*struct{}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := adminWith(p, "admin:members"); err != nil {
			return nil, err
		}
		if err := sessionOnly(p, "Workspace members"); err != nil {
			return nil, err
		}
		w, err := a.workspace(ctx, p, in.WS)
		if err != nil {
			return nil, err
		}
		if _, err := store.GetUser(ctx, a.st.R, in.User); err != nil {
			return nil, errNotFound()
		}
		return nil, a.tx(ctx, func(tx *sql.Tx) error { return store.SetMembership(ctx, tx, p.Actor(), w.ID, in.User, in.Body.Role) })
	})
	huma.Register(a.h, huma.Operation{OperationID: "remove-member", Method: "DELETE", Path: "/workspaces/{ws}/members/{user}", Tags: []string{"workspaces"},
		Summary: "Remove a member (admin; also unassigns them)", DefaultStatus: 204}, func(ctx context.Context, in *memberDelIn) (*struct{}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := adminWith(p, "admin:members"); err != nil {
			return nil, err
		}
		if err := sessionOnly(p, "Workspace members"); err != nil {
			return nil, err
		}
		w, err := a.workspace(ctx, p, in.WS)
		if err != nil {
			return nil, err
		}
		return nil, a.tx(ctx, func(tx *sql.Tx) error { return store.SetMembership(ctx, tx, p.Actor(), w.ID, in.User, "") })
	})

	huma.Register(a.h, huma.Operation{OperationID: "list-labels", Method: "GET", Path: "/workspaces/{ws}/labels", Tags: []string{"labels"},
		Summary: "A workspace's labels"}, func(ctx context.Context, in *wsPath) (*labelsOut, error) {
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
		out := &labelsOut{}
		out.Body.Items, err = store.Labels(ctx, a.st.R, w.ID)
		if out.Body.Items == nil {
			out.Body.Items = []*store.Label{}
		}
		return out, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "create-label", Method: "POST", Path: "/workspaces/{ws}/labels", Tags: []string{"labels"},
		Summary: "Create a label (managers)", DefaultStatus: 201}, func(ctx context.Context, in *labelCreateIn) (*labelOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		w, err := a.workspace(ctx, p, in.WS)
		if err != nil {
			return nil, err
		}
		if err := require(p, w.ID, access.Manager, "projects:write"); err != nil {
			return nil, err
		}
		if p.Narrowed() {
			return nil, errForbidden("A key narrowed to projects can't change workspace labels.")
		}
		var l *store.Label
		err = a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			l, err = store.CreateLabel(ctx, tx, p.Actor(), w.ID, in.Body.Name, in.Body.Color, in.Body.Description)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &labelOut{ETag: etag(l.Version), Body: l}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "patch-label", Method: "PATCH", Path: "/labels/{id}", Tags: []string{"labels"},
		Summary: "Change a label (managers)"}, func(ctx context.Context, in *labelPatchIn) (*labelOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		l, err := store.GetLabel(ctx, a.st.R, in.ID)
		if err != nil || !p.SeesWorkspace(l.WorkspaceID) {
			return nil, errNotFound()
		}
		if err := require(p, l.WorkspaceID, access.Manager, "projects:write"); err != nil {
			return nil, err
		}
		if p.Narrowed() {
			return nil, errForbidden("A key narrowed to projects can't change workspace labels.")
		}
		v, err := ifMatch(in.IfMatch)
		if err != nil {
			return nil, err
		}
		err = a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			l, err = store.PatchLabel(ctx, tx, p.Actor(), l, v, store.LabelPatch{Name: in.Body.Name.Val(), Color: in.Body.Color.Ptr(), Description: in.Body.Description.Val()})
			return err
		})
		if err != nil {
			return nil, err
		}
		return &labelOut{ETag: etag(l.Version), Body: l}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "delete-label", Method: "DELETE", Path: "/labels/{id}", Tags: []string{"labels"},
		Summary: "Delete a label (managers; removes it from every gig)", DefaultStatus: 204}, func(ctx context.Context, in *idPath) (*struct{}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		l, err := store.GetLabel(ctx, a.st.R, in.ID)
		if err != nil || !p.SeesWorkspace(l.WorkspaceID) {
			return nil, errNotFound()
		}
		if err := require(p, l.WorkspaceID, access.Manager, "projects:write"); err != nil {
			return nil, err
		}
		if p.Narrowed() {
			return nil, errForbidden("A key narrowed to projects can't change workspace labels.")
		}
		return nil, a.tx(ctx, func(tx *sql.Tx) error { return store.DeleteLabel(ctx, tx, p.Actor(), l) })
	})

	huma.Register(a.h, huma.Operation{OperationID: "list-projects", Method: "GET", Path: "/workspaces/{ws}/projects", Tags: []string{"projects"},
		Summary: "A workspace's projects in reach"}, func(ctx context.Context, in *wsPath) (*projectsOut, error) {
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
		out := &projectsOut{}
		out.Body.Items, err = store.ListProjects(ctx, a.st.R, w.ID, p.ProjectFilter())
		if out.Body.Items == nil {
			out.Body.Items = []*store.Project{}
		}
		return out, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "create-project", Method: "POST", Path: "/workspaces/{ws}/projects", Tags: []string{"projects"},
		Summary: "Create a project (managers)", DefaultStatus: 201}, func(ctx context.Context, in *projectCreateIn) (*projectOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		w, err := a.workspace(ctx, p, in.WS)
		if err != nil {
			return nil, err
		}
		if err := require(p, w.ID, access.Manager, "projects:write"); err != nil {
			return nil, err
		}
		if p.Narrowed() {
			return nil, errForbidden("A key narrowed to projects can't create projects.")
		}
		var pr *store.Project
		err = a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			pr, err = store.CreateProject(ctx, tx, p.Actor(), w.ID, in.Body.Slug, in.Body.Name, in.Body.Key, in.Body.Statuses)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &projectOut{ETag: etag(pr.Version), Body: pr}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "get-project", Method: "GET", Path: "/projects/{p}", Tags: []string{"projects"},
		Summary: "A project"}, func(ctx context.Context, in *projectPath) (*projectOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		pr, err := a.project(ctx, p, in.P)
		if err != nil {
			return nil, err
		}
		if err := require(p, pr.WorkspaceID, access.Viewer, "workspace:read"); err != nil {
			return nil, err
		}
		return &projectOut{ETag: etag(pr.Version), Body: pr}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "patch-project", Method: "PATCH", Path: "/projects/{p}", Tags: []string{"projects"},
		Summary: "Change a project (managers)"}, func(ctx context.Context, in *projectPatchIn) (*projectOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		pr, err := a.project(ctx, p, in.P)
		if err != nil {
			return nil, err
		}
		if err := require(p, pr.WorkspaceID, access.Manager, "projects:write"); err != nil {
			return nil, err
		}
		v, err := ifMatch(in.IfMatch)
		if err != nil {
			return nil, err
		}
		err = a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			pr, err = store.PatchProject(ctx, tx, p.Actor(), pr.ID, v, store.ProjectPatch{Name: in.Body.Name.Val(), Color: in.Body.Color.Ptr(),
				DescriptionMD: in.Body.DescriptionMD.Val(), Archived: in.Body.Archived.Val()})
			return err
		})
		if err != nil {
			return nil, err
		}
		return &projectOut{ETag: etag(pr.Version), Body: pr}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "delete-project", Method: "DELETE", Path: "/projects/{p}", Tags: []string{"projects"},
		Summary: "Delete an empty project (managers)", DefaultStatus: 204}, func(ctx context.Context, in *projectPath) (*struct{}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		pr, err := a.project(ctx, p, in.P)
		if err != nil {
			return nil, err
		}
		if err := require(p, pr.WorkspaceID, access.Manager, "projects:write"); err != nil {
			return nil, err
		}
		return nil, a.tx(ctx, func(tx *sql.Tx) error { return store.DeleteProject(ctx, tx, p.Actor(), pr) })
	})
	huma.Register(a.h, huma.Operation{OperationID: "get-board", Method: "GET", Path: "/projects/{p}/board", Tags: []string{"projects"},
		Summary: "A project's board: statuses with their gigs, and the workspace's labels"}, func(ctx context.Context, in *projectPath) (*struct{ Body *Board }, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		pr, err := a.project(ctx, p, in.P)
		if err != nil {
			return nil, err
		}
		if err := require(p, pr.WorkspaceID, access.Viewer, "gigs:read"); err != nil {
			return nil, err
		}
		sts, err := store.Statuses(ctx, a.st.R, pr.ID)
		if err != nil {
			return nil, err
		}
		var ts []*store.Gig
		seen := map[int64]bool{} // pages are separate reads: a gig that moved between them comes once
		more, offset := true, 0
		for more && len(ts) < boardMax {
			var page []*store.Gig
			page, more, err = store.ListGigs(ctx, a.st.R, store.GigFilter{WorkspaceID: pr.WorkspaceID, Projects: []int64{pr.ID}, Limit: 500, Offset: offset})
			if err != nil {
				return nil, err
			}
			offset += len(page)
			for _, t := range page {
				if !seen[t.ID] {
					seen[t.ID] = true
					ts = append(ts, t)
				}
			}
		}
		if err == nil && p.HasScope("comments:read") {
			err = store.AttachQuestions(ctx, a.st.R, ts) // a waiting card shows the agent's question
		}
		if err != nil {
			return nil, err
		}
		b := &Board{Project: pr, Truncated: more}
		by := map[int64]*BoardColumn{}
		for _, s := range sts {
			c := &BoardColumn{Status: s, Gigs: []*store.Gig{}}
			by[s.ID] = c
			b.Statuses = append(b.Statuses, c)
		}
		for _, t := range ts {
			if c := by[t.StatusID]; c != nil {
				c.Gigs = append(c.Gigs, t)
			}
		}
		if b.Labels, err = store.Labels(ctx, a.st.R, pr.WorkspaceID); err != nil {
			return nil, err
		}
		if b.Labels == nil {
			b.Labels = []*store.Label{}
		}
		return &struct{ Body *Board }{Body: b}, nil
	})

	huma.Register(a.h, huma.Operation{OperationID: "list-statuses", Method: "GET", Path: "/projects/{p}/statuses", Tags: []string{"projects"},
		Summary: "A project's statuses in board order"}, func(ctx context.Context, in *projectPath) (*statusesOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		pr, err := a.project(ctx, p, in.P)
		if err != nil {
			return nil, err
		}
		if err := require(p, pr.WorkspaceID, access.Viewer, "workspace:read"); err != nil {
			return nil, err
		}
		out := &statusesOut{}
		out.Body.Items, err = store.Statuses(ctx, a.st.R, pr.ID)
		return out, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "create-status", Method: "POST", Path: "/projects/{p}/statuses", Tags: []string{"projects"},
		Summary: "Add a status (managers)", DefaultStatus: 201}, func(ctx context.Context, in *statusCreateIn) (*statusOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		pr, err := a.project(ctx, p, in.P)
		if err != nil {
			return nil, err
		}
		if err := require(p, pr.WorkspaceID, access.Manager, "projects:write"); err != nil {
			return nil, err
		}
		var s *store.Status
		err = a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			s, err = store.CreateStatus(ctx, tx, p.Actor(), pr, in.Body.Name, in.Body.Category, in.Body.Color)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &statusOut{ETag: etag(s.Version), Body: s}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "patch-status", Method: "PATCH", Path: "/statuses/{id}", Tags: []string{"projects"},
		Summary: "Change a status (managers)"}, func(ctx context.Context, in *statusPatchIn) (*statusOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		s, err := store.GetStatus(ctx, a.st.R, in.ID)
		if err != nil || !p.SeesProject(s.WorkspaceID, s.ProjectID) {
			return nil, errNotFound()
		}
		if err := require(p, s.WorkspaceID, access.Manager, "projects:write"); err != nil {
			return nil, err
		}
		v, err := ifMatch(in.IfMatch)
		if err != nil {
			return nil, err
		}
		err = a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			s, err = store.PatchStatus(ctx, tx, p.Actor(), s, v, store.StatusPatch{Name: in.Body.Name.Val(), Category: in.Body.Category.Val(),
				Color: in.Body.Color.Ptr(), WIPLimit: in.Body.WIPLimit.Ptr(), IsDefault: in.Body.IsDefault.Val()})
			return err
		})
		if err != nil {
			return nil, err
		}
		return &statusOut{ETag: etag(s.Version), Body: s}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "delete-status", Method: "DELETE", Path: "/statuses/{id}", Tags: []string{"projects"},
		Summary: "Delete an empty status (managers)", DefaultStatus: 204}, func(ctx context.Context, in *idPath) (*struct{}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		s, err := store.GetStatus(ctx, a.st.R, in.ID)
		if err != nil || !p.SeesProject(s.WorkspaceID, s.ProjectID) {
			return nil, errNotFound()
		}
		if err := require(p, s.WorkspaceID, access.Manager, "projects:write"); err != nil {
			return nil, err
		}
		return nil, a.tx(ctx, func(tx *sql.Tx) error { return store.DeleteStatus(ctx, tx, p.Actor(), s) })
	})
	huma.Register(a.h, huma.Operation{OperationID: "reorder-statuses", Method: "POST", Path: "/projects/{p}/statuses/reorder", Tags: []string{"projects"},
		Summary: "Set the order of a project's statuses (managers)", DefaultStatus: 204}, func(ctx context.Context, in *reorderIn) (*struct{}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		pr, err := a.project(ctx, p, in.P)
		if err != nil {
			return nil, err
		}
		if err := require(p, pr.WorkspaceID, access.Manager, "projects:write"); err != nil {
			return nil, err
		}
		return nil, a.tx(ctx, func(tx *sql.Tx) error { return store.ReorderStatuses(ctx, tx, p.Actor(), pr, in.Body.IDs) })
	})
}
