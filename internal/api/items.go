package api

import (
	"context"
	"database/sql"

	"github.com/danielgtaylor/huma/v2"

	"github.com/0x2alx/gigg/internal/access"
	"github.com/0x2alx/gigg/internal/store"
)

// Relations, checklists, archive, custom fields, templates, saved filters and notifications.
// Order in every handler: visibility (404) -> role and key scope (403) -> capability (403).

type itemsOut[T any] struct {
	Body struct {
		Items []T `json:"items"`
	}
}

func items[T any](xs []T) *itemsOut[T] {
	out := &itemsOut[T]{}
	out.Body.Items = xs
	if out.Body.Items == nil {
		out.Body.Items = []T{}
	}
	return out
}

type relationIn struct {
	ID   string `path:"id" doc:"Gig id or key"`
	Body struct {
		Gig       string `json:"gig" doc:"The other gig (id or key, same workspace)"`
		Direction string `json:"direction" enum:"blocks,blocked_by,relates" doc:"blocks: this gig blocks the other; blocked_by: the other blocks this one"`
	}
}

type relationDelIn struct {
	ID        string `path:"id"`
	Gig       string `query:"gig" required:"true"`
	Direction string `query:"direction" enum:"blocks,blocked_by,relates" required:"true"`
}

type checklistItemPath struct {
	ID   string `path:"id"`
	Item int64  `path:"item"`
}

type templateBody struct {
	Project       *int64                 `json:"project_id,omitempty" doc:"Limit to one project (default: any project of the workspace)"`
	Name          string                 `json:"name" minLength:"1" maxLength:"80"`
	Title         string                 `json:"title" minLength:"1" maxLength:"250"`
	DescriptionMD string                 `json:"description_md,omitempty" maxLength:"65536"`
	Priority      int64                  `json:"priority,omitempty" minimum:"0" maximum:"4"`
	Color         *string                `json:"color,omitempty"`
	Labels        []string               `json:"labels,omitempty" maxItems:"20"`
	Assignees     []int64                `json:"assignees,omitempty" maxItems:"50"`
	Followers     []int64                `json:"followers,omitempty" maxItems:"50" doc:"Users who follow each gig made from it (they must see its project then; others are left out); on update, omit to keep, [] clears"`
	Checklist     []string               `json:"checklist,omitempty" maxItems:"200"`
	Subgigs       []store.TemplateSubgig `json:"subgigs,omitempty" maxItems:"50" doc:"Subgigs each gig made from it gets (title; blocks_parent = the gig waits for it)"`
	Custom        map[string]any         `json:"custom,omitempty"`
	Model         string                 `json:"model,omitempty" maxLength:"64" doc:"Model a runner should ask for (default: the agent's)"`
	Effort        string                 `json:"effort,omitempty" enum:"low,medium,high,xhigh,max," doc:"Effort level a runner should ask for (default: the agent's)"`
	AlwaysRun     bool                   `json:"always_run,omitempty" doc:"Gigs made from it run whatever the usage budget (needs the always_run permission)"`
	AllowedTools  []string               `json:"allowed_tools,omitempty" maxItems:"64" doc:"Tool allowlist its gigs get for the runner (permission rules); on update, omit to keep, [] clears. Needs the allowed_tools permission"`
	AutoArchive   *int64                 `json:"auto_archive_minutes,omitempty" minimum:"5" maximum:"527040" doc:"Gigs made from it archive themselves this many minutes after done or cancelled (omit: never)"`
	RunTimeout    *int64                 `json:"run_timeout_minutes,omitempty" minimum:"5" maximum:"300" doc:"Its gigs' run timeout in minutes (omit: the agent's default). Needs the run_timeout permission"`
	StartCategory string                 `json:"start_category,omitempty" enum:"backlog,todo," doc:"Where its gigs start: backlog or todo (the first status of that category in the gig's project); omit for the project's default status. A recurring gig made from it creates its gigs there, so backlog keeps them from the runner until someone moves them"`
}

func (b templateBody) apply(t *store.Template) {
	t.ProjectID, t.Name, t.Title, t.DescriptionMD, t.Priority, t.Color = b.Project, b.Name, b.Title, b.DescriptionMD, b.Priority, b.Color
	t.Labels, t.Assignees, t.Checklist, t.Custom, t.Subgigs = b.Labels, b.Assignees, b.Checklist, b.Custom, b.Subgigs
	t.Model, t.Effort, t.AlwaysRun, t.AutoArchive, t.RunTimeout, t.StartCategory = b.Model, b.Effort, b.AlwaysRun, b.AutoArchive, b.RunTimeout, b.StartCategory
	if b.AllowedTools != nil || t.ID == 0 {
		t.AllowedTools = b.AllowedTools
	}
	if b.Followers != nil || t.ID == 0 {
		t.Followers = b.Followers
	}
}

type fieldBody struct {
	Name    string   `json:"name" minLength:"1" maxLength:"60"`
	Kind    string   `json:"kind" enum:"text,number,date,select,checkbox,url"`
	Options []string `json:"options,omitempty" maxItems:"50"`
}

func (a *API) template(ctx context.Context, p *access.Principal, id int64) (*store.Template, error) {
	t, err := store.GetTemplate(ctx, a.st.R, id)
	if err != nil || (t.Shared && !seesShared(p)) || (!t.Shared && !p.SeesWorkspace(t.WorkspaceID)) ||
		(t.ProjectID != nil && !p.SeesProject(t.WorkspaceID, *t.ProjectID)) || (!t.Shared && t.ProjectID == nil && p.Narrowed(t.WorkspaceID)) {
		return nil, errNotFound()
	}
	return t, nil
}

// seesShared: the shared library (templates, schedules, errand templates) is there for anyone who
// sees at least one workspace whole (they can use it there), and for admins. Access to only some
// projects of a workspace doesn't show it: it is workspace-level.
func seesShared(p *access.Principal) bool {
	ws := p.Workspaces()
	if ws == nil {
		return true
	}
	for _, w := range ws {
		if p.Allowed(w, access.Viewer) {
			return true
		}
	}
	return false
}

// readIn checks a read of an item that is a workspace's (ws) or shared (ws 0): the workspace's viewer
// role and the scope; for a shared one, the scope only (the caller checked seesShared).
func readIn(p *access.Principal, ws int64, scope string) error {
	if ws == 0 {
		return need(p, scope)
	}
	return require(p, ws, access.Viewer, scope)
}

// sharedWrite: the shared library (gig templates, schedules, errand templates) is made, changed and
// deleted by holders of Shared library on the all-workspaces level (instance admins hold it), through a
// session or a key not limited to some places, with the library:write scope and the scope of the kind
// (gigs:write for gig templates and schedules, errands:write for errand templates): library:write widens
// where the key may act, never what it may do. The settings that have a permission of their own are
// checked on that level too (allowed(p, 0, ...)).
func sharedWrite(p *access.Principal, scope string) error {
	if p.Library() {
		return need(p, scope)
	}
	if p.Allowed(0, "shared_library") {
		return errForbidden("This key lacks the library:write scope.")
	}
	if p.Key != nil && p.Key.Bound {
		return errForbidden("A key limited to some places can't change the shared library.")
	}
	return allowed(p, 0, "shared_library")
}

func (a *API) field(ctx context.Context, p *access.Principal, id int64) (*store.CustomField, error) {
	f, err := store.GetCustomField(ctx, a.st.R, id)
	if err != nil || !p.SeesWorkspace(f.WorkspaceID) {
		return nil, errNotFound()
	}
	return f, nil
}

func (a *API) registerItems() {
	// ------------------------------------------------ relations
	huma.Register(a.h, huma.Operation{OperationID: "list-relations", Method: "GET", Path: "/gigs/{id}/relations", Tags: []string{"gigs"},
		Summary: "Gigs this one blocks, is blocked by, or relates to"}, func(ctx context.Context, in *gigPath) (*itemsOut[store.Relation], error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		t, err := a.gig(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if err := requireIn(p, t.WorkspaceID, t.ProjectID, access.Viewer, "gigs:read"); err != nil {
			return nil, err
		}
		rs, err := store.GigRelations(ctx, a.st.R, t.ID)
		if err != nil {
			return nil, err
		}
		// the other side may be in a project this key can't see: leave it out
		vis := rs[:0]
		for _, r := range rs {
			other := r.To
			if other == t.ID {
				other = r.From
			}
			if o, err := store.GetGig(ctx, a.st.R, other); err == nil && p.SeesProject(o.WorkspaceID, o.ProjectID) {
				vis = append(vis, r)
			}
		}
		return items(vis), nil
	})
	link := func(ctx context.Context, id, other, dir string, del bool) (*gigOut, error) {
		return a.withGigOp(ctx, id, "", access.Editor, "gigs:write", opGuarded, func(tx *sql.Tx, p *access.Principal, t *store.Gig, _ int64) (*store.Gig, error) {
			o, err := a.gig(ctx, p, other)
			if err != nil {
				return nil, store.Invalid("no such gig %q", other)
			}
			from, to, kind := t, o, "blocks"
			switch dir {
			case "blocked_by":
				from, to = o, t
			case "relates":
				kind = "relates"
				if o.ID < t.ID { // one row per pair, whichever side asks
					from, to = o, t
				}
			}
			if del {
				err = store.RemoveRelation(ctx, tx, p.Actor(), from, to, kind)
			} else {
				err = store.AddRelation(ctx, tx, p.Actor(), from, to, kind)
			}
			if err != nil {
				return nil, err
			}
			return store.GetGig(ctx, tx, t.ID)
		})
	}
	huma.Register(a.h, huma.Operation{OperationID: "add-relation", Method: "POST", Path: "/gigs/{id}/relations", Tags: []string{"gigs"},
		Summary: "Link two gigs: blocks, blocked_by or relates. A blocked gig can't be claimed until its blockers are done."},
		func(ctx context.Context, in *relationIn) (*gigOut, error) {
			return link(ctx, in.ID, in.Body.Gig, in.Body.Direction, false)
		})
	huma.Register(a.h, huma.Operation{OperationID: "remove-relation", Method: "DELETE", Path: "/gigs/{id}/relations", Tags: []string{"gigs"},
		Summary: "Unlink two gigs"}, func(ctx context.Context, in *relationDelIn) (*gigOut, error) {
		return link(ctx, in.ID, in.Gig, in.Direction, true)
	})

	// ------------------------------------------------ checklists
	huma.Register(a.h, huma.Operation{OperationID: "get-checklist", Method: "GET", Path: "/gigs/{id}/checklist", Tags: []string{"gigs"},
		Summary: "A gig's checklist"}, func(ctx context.Context, in *gigPath) (*itemsOut[store.ChecklistItem], error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		t, err := a.gig(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if err := requireIn(p, t.WorkspaceID, t.ProjectID, access.Viewer, "gigs:read"); err != nil {
			return nil, err
		}
		cl, err := store.Checklist(ctx, a.st.R, t.ID)
		if err != nil {
			return nil, err
		}
		return items(cl), nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "add-checklist-items", Method: "POST", Path: "/gigs/{id}/checklist", Tags: []string{"gigs"},
		Summary: "Append checklist items"}, func(ctx context.Context, in *struct {
		ID   string `path:"id"`
		Body struct {
			Items []string `json:"items" minItems:"1" maxItems:"200"`
		}
	}) (*gigOut, error) {
		return a.withGigOp(ctx, in.ID, "", access.Editor, "gigs:write", opGuarded, func(tx *sql.Tx, p *access.Principal, t *store.Gig, _ int64) (*store.Gig, error) {
			if err := store.AddChecklistItems(ctx, tx, p.Actor(), t, in.Body.Items); err != nil {
				return nil, err
			}
			return store.GetGig(ctx, tx, t.ID)
		})
	})
	huma.Register(a.h, huma.Operation{OperationID: "patch-checklist-item", Method: "PATCH", Path: "/gigs/{id}/checklist/{item}", Tags: []string{"gigs"},
		Summary: "Edit, tick/untick or move a checklist item"}, func(ctx context.Context, in *struct {
		ID   string `path:"id"`
		Item int64  `path:"item"`
		Body struct {
			Text  *string `json:"text,omitempty" maxLength:"500"`
			Done  *bool   `json:"done,omitempty"`
			After *int64  `json:"after,omitempty" doc:"Move after this item id (0 = top)"`
		}
	}) (*gigOut, error) {
		// ticking is a consumer's action (Work on gigs); text and order are a producer's (Edit gigs)
		edit := in.Body.Text != nil || in.Body.After != nil
		// (ticking an always-run gig or one with tools doesn't change how it runs: no guard)
		min, scope, op := access.Worker, "gigs:work", opEdit
		if edit {
			min, scope, op = access.Editor, "gigs:write", opGuarded
		}
		return a.withGigOp(ctx, in.ID, "", min, scope, op, func(tx *sql.Tx, p *access.Principal, t *store.Gig, _ int64) (*store.Gig, error) {
			if in.Body.Done != nil {
				if err := requireIn(p, t.WorkspaceID, t.ProjectID, access.Worker, "gigs:work"); err != nil {
					return nil, err
				}
			}
			if err := store.PatchChecklistItem(ctx, tx, p.Actor(), t, in.Item, store.ChecklistPatch{Text: in.Body.Text, Done: in.Body.Done, After: in.Body.After}); err != nil {
				return nil, err
			}
			return store.GetGig(ctx, tx, t.ID)
		})
	})
	huma.Register(a.h, huma.Operation{OperationID: "delete-checklist-item", Method: "DELETE", Path: "/gigs/{id}/checklist/{item}", Tags: []string{"gigs"},
		Summary: "Remove a checklist item"}, func(ctx context.Context, in *checklistItemPath) (*gigOut, error) {
		return a.withGigOp(ctx, in.ID, "", access.Editor, "gigs:write", opGuarded, func(tx *sql.Tx, p *access.Principal, t *store.Gig, _ int64) (*store.Gig, error) {
			if err := store.DeleteChecklistItem(ctx, tx, p.Actor(), t, in.Item); err != nil {
				return nil, err
			}
			return store.GetGig(ctx, tx, t.ID)
		})
	})

	// ------------------------------------------------ archive
	// archiving takes a gig out of the queue (like deleting); unarchiving puts a guarded one back, so that needs its permissions
	for _, arch := range []bool{true, false} {
		op, path, sum := "archive-gig", "/gigs/{id}/archive", "Archive a gig (off the board and lists; not trash)"
		if !arch {
			op, path, sum = "unarchive-gig", "/gigs/{id}/unarchive", "Bring an archived gig back"
		}
		huma.Register(a.h, huma.Operation{OperationID: op, Method: "POST", Path: path, Tags: []string{"gigs"}, Summary: sum},
			func(ctx context.Context, in *gigPath) (*gigOut, error) {
				return a.withGigOp(ctx, in.ID, "", access.Editor, "gigs:write", archiveOp(arch), func(tx *sql.Tx, p *access.Principal, t *store.Gig, _ int64) (*store.Gig, error) {
					if err := allowedIn(p, t.WorkspaceID, t.ProjectID, "archive_gigs"); err != nil {
						return nil, err
					}
					return store.ArchiveGig(ctx, tx, p.Actor(), t, arch)
				})
			})
	}
	huma.Register(a.h, huma.Operation{OperationID: "archive-done", Method: "POST", Path: "/projects/{p}/archive-done", Tags: []string{"gigs"},
		Summary: "Archive the project's done and cancelled gigs finished more than N days ago"}, func(ctx context.Context, in *struct {
		P    int64 `path:"p"`
		Body struct {
			OlderThanDays int64  `json:"older_than_days" minimum:"0" maximum:"3650"`
			Category      string `json:"category,omitempty" enum:"done,cancelled" doc:"Only this category; left out, done and cancelled both"`
		}
	}) (*struct {
		Body struct {
			Archived int `json:"archived"`
		}
	}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		pr, err := a.project(ctx, p, in.P)
		if err != nil {
			return nil, err
		}
		if err := requireIn(p, pr.WorkspaceID, pr.ID, access.Editor, "gigs:write"); err != nil {
			return nil, err
		}
		if err := allowedIn(p, pr.WorkspaceID, pr.ID, "archive_gigs"); err != nil {
			return nil, err
		}
		out := &struct {
			Body struct {
				Archived int `json:"archived"`
			}
		}{}
		err = a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			out.Body.Archived, err = store.ArchiveDone(ctx, tx, p.Actor(), pr, store.Now()-in.Body.OlderThanDays*86400000, in.Body.Category)
			return err
		})
		return out, err
	})

	// ------------------------------------------------ custom fields
	huma.Register(a.h, huma.Operation{OperationID: "list-fields", Method: "GET", Path: "/workspaces/{ws}/fields", Tags: []string{"fields"},
		Summary: "The workspace's custom gig fields"}, func(ctx context.Context, in *wsPath) (*itemsOut[*store.CustomField], error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		w, err := a.workspace(ctx, p, in.WS)
		if err != nil {
			return nil, err
		}
		if err := requireScope(p, "gigs:read"); err != nil { // the definitions: anyone who sees some of the workspace
			return nil, err
		}
		fs, err := store.CustomFields(ctx, a.st.R, w.ID)
		if err != nil {
			return nil, err
		}
		return items(fs), nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "create-field", Method: "POST", Path: "/workspaces/{ws}/fields", Tags: []string{"fields"},
		Summary: "Add a custom field (manager)", DefaultStatus: 201}, func(ctx context.Context, in *struct {
		WS   string `path:"ws"`
		Body fieldBody
	}) (*struct{ Body *store.CustomField }, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		w, err := a.workspace(ctx, p, in.WS)
		if err != nil {
			return nil, err
		}
		if err := require(p, w.ID, "custom_fields", "workspace:write"); err != nil {
			return nil, err
		}
		if err := allowed(p, w.ID, "custom_fields"); err != nil {
			return nil, err
		}
		f := &store.CustomField{WorkspaceID: w.ID, Name: in.Body.Name, Kind: in.Body.Kind, Options: in.Body.Options}
		err = a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			f, err = store.SaveCustomField(ctx, tx, p.Actor(), f, 0)
			return err
		})
		return &struct{ Body *store.CustomField }{f}, err
	})
	huma.Register(a.h, huma.Operation{OperationID: "reorder-fields", Method: "POST", Path: "/workspaces/{ws}/fields/reorder", Tags: []string{"fields"},
		Summary: "Set the order of the workspace's custom fields (manager)", DefaultStatus: 204}, func(ctx context.Context, in *struct {
		WS   string `path:"ws"`
		Body struct {
			IDs []int64 `json:"ids" maxItems:"100" doc:"Every field id of the workspace, in the new order"`
		}
	}) (*struct{}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		w, err := a.workspace(ctx, p, in.WS)
		if err != nil {
			return nil, err
		}
		if err := require(p, w.ID, "custom_fields", "workspace:write"); err != nil {
			return nil, err
		}
		if err := allowed(p, w.ID, "custom_fields"); err != nil {
			return nil, err
		}
		return nil, a.tx(ctx, func(tx *sql.Tx) error { return store.ReorderCustomFields(ctx, tx, p.Actor(), w.ID, in.Body.IDs) })
	})
	huma.Register(a.h, huma.Operation{OperationID: "update-field", Method: "PUT", Path: "/fields/{id}", Tags: []string{"fields"},
		Summary: "Rename a custom field or change its options (manager)"}, func(ctx context.Context, in *struct {
		ID      int64  `path:"id"`
		IfMatch string `header:"If-Match"`
		Body    fieldBody
	}) (*struct{ Body *store.CustomField }, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		f, err := a.field(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if err := require(p, f.WorkspaceID, "custom_fields", "workspace:write"); err != nil {
			return nil, err
		}
		if err := allowed(p, f.WorkspaceID, "custom_fields"); err != nil {
			return nil, err
		}
		v, err := ifMatch(in.IfMatch)
		if err != nil {
			return nil, err
		}
		f.Name, f.Kind, f.Options = in.Body.Name, in.Body.Kind, in.Body.Options
		err = a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			f, err = store.SaveCustomField(ctx, tx, p.Actor(), f, v)
			return err
		})
		return &struct{ Body *store.CustomField }{f}, err
	})
	huma.Register(a.h, huma.Operation{OperationID: "delete-field", Method: "DELETE", Path: "/fields/{id}", Tags: []string{"fields"},
		Summary: "Remove a custom field and its values (manager)", DefaultStatus: 204}, func(ctx context.Context, in *idPath) (*struct{}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		f, err := a.field(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if err := require(p, f.WorkspaceID, "custom_fields", "workspace:write"); err != nil {
			return nil, err
		}
		if err := allowed(p, f.WorkspaceID, "custom_fields"); err != nil {
			return nil, err
		}
		return nil, a.tx(ctx, func(tx *sql.Tx) error { return store.DeleteCustomField(ctx, tx, p.Actor(), f) })
	})
	huma.Register(a.h, huma.Operation{OperationID: "set-custom-values", Method: "PATCH", Path: "/gigs/{id}/custom", Tags: []string{"fields"},
		Summary: "Set or clear (null) a gig's custom field values, keyed by field id"}, func(ctx context.Context, in *struct {
		ID      string `path:"id"`
		IfMatch string `header:"If-Match"`
		Body    struct {
			Values map[string]any `json:"values"`
		}
	}) (*gigOut, error) {
		return a.withGigOp(ctx, in.ID, in.IfMatch, access.Editor, "gigs:write", opGuarded, func(tx *sql.Tx, p *access.Principal, t *store.Gig, v int64) (*store.Gig, error) {
			if v != 0 && v != t.Version {
				return nil, store.ErrVersion
			}
			if err := store.SetCustomValues(ctx, tx, p.Actor(), t, in.Body.Values); err != nil {
				return nil, err
			}
			return store.GetGig(ctx, tx, t.ID)
		})
	})

	// ------------------------------------------------ templates
	huma.Register(a.h, huma.Operation{OperationID: "list-templates", Method: "GET", Path: "/workspaces/{ws}/templates", Tags: []string{"templates"},
		Summary: "Gig templates of a workspace"}, func(ctx context.Context, in *struct {
		WS      string `path:"ws"`
		Project int64  `query:"project" doc:"Only those usable in this project"`
	}) (*itemsOut[*store.Template], error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		w, err := a.workspace(ctx, p, in.WS)
		if err != nil {
			return nil, err
		}
		if err := requireScope(p, "gigs:read"); err != nil {
			return nil, err
		}
		projects := p.ProjectFilter(w.ID)
		if in.Project != 0 {
			if !p.SeesProject(w.ID, in.Project) {
				return nil, errNotFound()
			}
			projects = []int64{in.Project}
		}
		ts, err := store.Templates(ctx, a.st.R, w.ID, projects)
		if err != nil {
			return nil, err
		}
		if p.Narrowed(w.ID) { // only some projects: only their templates (the workspace-wide ones are workspace-level)
			keep := ts[:0]
			for _, t := range ts {
				if t.ProjectID != nil {
					keep = append(keep, t)
				}
			}
			ts = keep
		}
		return items(ts), nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "create-template", Method: "POST", Path: "/workspaces/{ws}/templates", Tags: []string{"templates"},
		Summary: "Save a gig template", DefaultStatus: 201}, func(ctx context.Context, in *struct {
		WS   string `path:"ws"`
		Body templateBody
	}) (*struct{ Body *store.Template }, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		w, err := a.workspace(ctx, p, in.WS)
		if err != nil {
			return nil, err
		}
		if err := require(p, w.ID, access.Editor, "gigs:write"); err != nil {
			return nil, err
		}
		if in.Body.Project != nil && !p.SeesProject(w.ID, *in.Body.Project) {
			return nil, errNotFound()
		}
		if p.Narrowed(w.ID) && in.Body.Project == nil {
			return nil, errForbidden("Access to only some projects makes templates for one of those projects.")
		}
		if err := allowed(p, w.ID, "templates"); err != nil {
			return nil, err
		}
		if err := alwaysRunChange(p, w.ID, false, in.Body.AlwaysRun); err != nil {
			return nil, err
		}
		if err := timeoutChange(p, w.ID, nil, in.Body.RunTimeout); err != nil {
			return nil, err
		}
		if err := toolsChange(p, w.ID, nil, in.Body.AllowedTools); err != nil {
			return nil, err
		}
		t := &store.Template{WorkspaceID: w.ID}
		in.Body.apply(t)
		err = a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			var proj int64
			if t.ProjectID != nil {
				proj = *t.ProjectID
			}
			t.Assignees = store.OwnerDefault(ctx, tx, w.ID, proj, t.Assignees)
			t, err = store.SaveTemplate(ctx, tx, p.Actor(), t, 0)
			return err
		})
		return &struct{ Body *store.Template }{t}, err
	})
	huma.Register(a.h, huma.Operation{OperationID: "list-shared-templates", Method: "GET", Path: "/shared/templates", Tags: []string{"templates"},
		Summary: "The shared gig templates (every workspace can use them)"}, func(ctx context.Context, _ *struct{}) (*itemsOut[*store.Template], error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if !seesShared(p) {
			return items([]*store.Template{}), nil
		}
		if err := need(p, "gigs:read"); err != nil {
			return nil, err
		}
		ts, err := store.Templates(ctx, a.st.R, 0, nil)
		if err != nil {
			return nil, err
		}
		return items(ts), nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "create-shared-template", Method: "POST", Path: "/shared/templates", Tags: []string{"templates"},
		Summary: "Save a shared gig template (Shared library, on the all-workspaces level)", DefaultStatus: 201}, func(ctx context.Context, in *struct {
		Body templateBody
	}) (*struct{ Body *store.Template }, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := sharedWrite(p, "gigs:write"); err != nil {
			return nil, err
		}
		if err := alwaysRunChange(p, 0, false, in.Body.AlwaysRun); err != nil {
			return nil, err
		}
		if err := timeoutChange(p, 0, nil, in.Body.RunTimeout); err != nil {
			return nil, err
		}
		if err := toolsChange(p, 0, nil, in.Body.AllowedTools); err != nil {
			return nil, err
		}
		t := &store.Template{}
		in.Body.apply(t)
		err = a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			t, err = store.SaveTemplate(ctx, tx, p.Actor(), t, 0)
			return err
		})
		return &struct{ Body *store.Template }{t}, err
	})
	huma.Register(a.h, huma.Operation{OperationID: "get-template", Method: "GET", Path: "/templates/{id}", Tags: []string{"templates"},
		Summary: "A gig template"}, func(ctx context.Context, in *idPath) (*struct{ Body *store.Template }, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		t, err := a.template(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if err := readIn(p, t.WorkspaceID, "gigs:read"); err != nil {
			return nil, err
		}
		return &struct{ Body *store.Template }{t}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "update-template", Method: "PUT", Path: "/templates/{id}", Tags: []string{"templates"},
		Summary: "Replace a gig template"}, func(ctx context.Context, in *struct {
		ID      int64  `path:"id"`
		IfMatch string `header:"If-Match"`
		Body    templateBody
	}) (*struct{ Body *store.Template }, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		t, err := a.template(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if t.Shared {
			if err := sharedWrite(p, "gigs:write"); err != nil {
				return nil, err
			}
		} else {
			if err := require(p, t.WorkspaceID, access.Editor, "gigs:write"); err != nil {
				return nil, err
			}
			if in.Body.Project != nil && !p.SeesProject(t.WorkspaceID, *in.Body.Project) {
				return nil, errNotFound()
			}
			if p.Narrowed(t.WorkspaceID) && (in.Body.Project == nil || t.ProjectID == nil) {
				return nil, errForbidden("A key limited to some projects only changes templates of those projects.")
			}
			if err := allowed(p, t.WorkspaceID, "templates"); err != nil {
				return nil, err
			}
		}
		v, err := ifMatch(in.IfMatch)
		if err != nil {
			return nil, err
		}
		// a shared template's WorkspaceID is 0: the all-workspaces level
		if err := alwaysRunGuard(p, t.WorkspaceID, t.AlwaysRun || in.Body.AlwaysRun); err != nil {
			return nil, err
		}
		if err := toolsGuard(p, t.WorkspaceID, len(t.AllowedTools) > 0 || len(in.Body.AllowedTools) > 0); err != nil {
			return nil, err
		}
		if err := timeoutChange(p, t.WorkspaceID, t.RunTimeout, in.Body.RunTimeout); err != nil {
			return nil, err
		}
		in.Body.apply(t)
		err = a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			t, err = store.SaveTemplate(ctx, tx, p.Actor(), t, v)
			return err
		})
		return &struct{ Body *store.Template }{t}, err
	})
	huma.Register(a.h, huma.Operation{OperationID: "delete-template", Method: "DELETE", Path: "/templates/{id}", Tags: []string{"templates"},
		Summary: "Delete a gig template", DefaultStatus: 204}, func(ctx context.Context, in *idPath) (*struct{}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		t, err := a.template(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if t.Shared {
			if err := sharedWrite(p, "gigs:write"); err != nil {
				return nil, err
			}
			return nil, a.tx(ctx, func(tx *sql.Tx) error { return store.DeleteTemplate(ctx, tx, p.Actor(), t) })
		}
		if err := require(p, t.WorkspaceID, access.Editor, "gigs:write"); err != nil {
			return nil, err
		}
		if p.Narrowed(t.WorkspaceID) && t.ProjectID == nil {
			return nil, errForbidden("A key limited to some projects only changes templates of those projects.")
		}
		if err := allowed(p, t.WorkspaceID, "templates"); err != nil {
			return nil, err
		}
		return nil, a.tx(ctx, func(tx *sql.Tx) error { return store.DeleteTemplate(ctx, tx, p.Actor(), t) })
	})
	huma.Register(a.h, huma.Operation{OperationID: "use-template", Method: "POST", Path: "/templates/{id}/use", Tags: []string{"templates"},
		Summary: "Create a gig from a template", DefaultStatus: 201}, func(ctx context.Context, in *struct {
		ID   int64 `path:"id"`
		Body struct {
			Project int64   `json:"project" doc:"Project id"`
			Title   *string `json:"title,omitempty" maxLength:"250" doc:"Override the template's title"`
		}
	}) (*gigOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		tpl, err := a.template(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		pr, err := a.project(ctx, p, in.Body.Project)
		if err != nil {
			return nil, err
		}
		if err := requireIn(p, pr.WorkspaceID, pr.ID, access.Editor, "gigs:write"); err != nil {
			return nil, err
		}
		if err := allowedIn(p, pr.WorkspaceID, pr.ID, "create_gigs"); err != nil {
			return nil, err
		}
		if err := alwaysRunChange(p, pr.WorkspaceID, false, tpl.AlwaysRun); err != nil {
			return nil, err
		}
		if err := timeoutChange(p, pr.WorkspaceID, nil, tpl.RunTimeout); err != nil {
			return nil, err
		}
		if err := toolsChange(p, pr.WorkspaceID, nil, tpl.AllowedTools); err != nil {
			return nil, err
		}
		var via *int64
		if p.Key != nil {
			via = &p.Key.ID
		}
		var t *store.Gig
		err = a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			t, err = store.GigFromTemplate(ctx, tx, p.Actor(), tpl, pr, in.Body.Title, via)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &gigOut{ETag: etag(t.Version), Body: t}, nil
	})

	// ------------------------------------------------ saved filters (personal)
	huma.Register(a.h, huma.Operation{OperationID: "list-filters", Method: "GET", Path: "/me/filters", Tags: []string{"me"},
		Summary: "Your saved filters"}, func(ctx context.Context, _ *struct{}) (*itemsOut[*store.SavedFilter], error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := need(p, "gigs:read"); err != nil {
			return nil, err
		}
		fs, err := store.SavedFilters(ctx, a.st.R, p.User.ID)
		if err != nil {
			return nil, err
		}
		vis := fs[:0]
		for _, f := range fs {
			if filterInReach(p, f.WorkspaceID) {
				vis = append(vis, f)
			}
		}
		return items(vis), nil
	})
	saveFilter := func(ctx context.Context, id int64, b store.SavedFilter) (*struct{ Body *store.SavedFilter }, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := need(p, "gigs:read"); err != nil {
			return nil, err
		}
		if !filterInReach(p, b.WorkspaceID) {
			return nil, errNotFound()
		}
		if id != 0 {
			if err := a.ownFilterInReach(ctx, p, id); err != nil {
				return nil, err
			}
		}
		if err := allowedAnywhere(p, "saved_filters"); err != nil {
			return nil, err
		}
		b.ID = id
		var f *store.SavedFilter
		err = a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			f, err = store.SaveFilter(ctx, tx, p.User.ID, &b)
			return err
		})
		return &struct{ Body *store.SavedFilter }{f}, err
	}
	type filterBody struct {
		Workspace *int64            `json:"workspace_id,omitempty"`
		Name      string            `json:"name" minLength:"1" maxLength:"60"`
		Query     map[string]string `json:"query"`
	}
	huma.Register(a.h, huma.Operation{OperationID: "create-filter", Method: "POST", Path: "/me/filters", Tags: []string{"me"},
		Summary: "Save a filter", DefaultStatus: 201}, func(ctx context.Context, in *struct{ Body filterBody }) (*struct{ Body *store.SavedFilter }, error) {
		return saveFilter(ctx, 0, store.SavedFilter{WorkspaceID: in.Body.Workspace, Name: in.Body.Name, Query: in.Body.Query})
	})
	huma.Register(a.h, huma.Operation{OperationID: "update-filter", Method: "PUT", Path: "/me/filters/{id}", Tags: []string{"me"},
		Summary: "Replace a saved filter"}, func(ctx context.Context, in *struct {
		ID   int64 `path:"id"`
		Body filterBody
	}) (*struct{ Body *store.SavedFilter }, error) {
		return saveFilter(ctx, in.ID, store.SavedFilter{WorkspaceID: in.Body.Workspace, Name: in.Body.Name, Query: in.Body.Query})
	})
	huma.Register(a.h, huma.Operation{OperationID: "delete-filter", Method: "DELETE", Path: "/me/filters/{id}", Tags: []string{"me"},
		Summary: "Delete a saved filter", DefaultStatus: 204}, func(ctx context.Context, in *idPath) (*struct{}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := need(p, "gigs:read"); err != nil {
			return nil, err
		}
		if err := a.ownFilterInReach(ctx, p, in.ID); err != nil {
			return nil, err
		}
		return nil, storeErr(store.DeleteFilter(ctx, a.st.W, p.User.ID, in.ID))
	})

	// ------------------------------------------------ notifications (the bell)
	huma.Register(a.h, huma.Operation{OperationID: "list-notifications", Method: "GET", Path: "/me/notifications", Tags: []string{"me"},
		Summary: "Your notifications (mentions, assignments, questions, missed windows), newest first"}, func(ctx context.Context, in *struct {
		Unread bool `query:"unread" doc:"Only unread"`
		Limit  int  `query:"limit" minimum:"0" maximum:"200"`
	}) (*struct {
		Body struct {
			Items  []*store.Notification `json:"items"`
			Unread int                   `json:"unread"`
		}
	}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := need(p, "gigs:read"); err != nil {
			return nil, err
		}
		ns, unread, err := store.Notifications(ctx, a.st.R, p.User.ID, p.Workspaces(), in.Unread, in.Limit)
		if err != nil {
			return nil, err
		}
		// only entries about gigs in reach (project rules and limited keys narrow it)
		{
			vis := ns[:0]
			for _, n := range ns {
				if n.GigID != nil {
					if t, err := store.GetGig(ctx, a.st.R, *n.GigID); err == nil && p.SeesProject(t.WorkspaceID, t.ProjectID) {
						vis = append(vis, n)
					}
				}
			}
			ns = vis
			unread = 0
			for _, n := range ns {
				if n.ReadAt == nil {
					unread++
				}
			}
		}
		out := &struct {
			Body struct {
				Items  []*store.Notification `json:"items"`
				Unread int                   `json:"unread"`
			}
		}{}
		out.Body.Items, out.Body.Unread = ns, unread
		return out, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "read-notifications", Method: "POST", Path: "/me/notifications/read", Tags: []string{"me"},
		Summary: "Mark notifications read (ids, or all)"}, func(ctx context.Context, in *struct {
		Body struct {
			IDs []int64 `json:"ids,omitempty" maxItems:"500"`
			All bool    `json:"all,omitempty"`
		}
	}) (*struct {
		Body struct {
			Marked int64 `json:"marked"`
		}
	}, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := need(p, "gigs:read"); err != nil {
			return nil, err
		}
		ids := in.Body.IDs
		if in.Body.All {
			ids = nil
		} else if ids == nil {
			ids = []int64{}
		}
		out := &struct {
			Body struct {
				Marked int64 `json:"marked"`
			}
		}{}
		out.Body.Marked, err = store.MarkRead(ctx, a.st.W, p.User.ID, p.Workspaces(), ids)
		return out, err
	})
}

// filterInReach: a filter for a workspace the caller sees, or (for callers not bound to one
// workspace) a cross-workspace filter.
func filterInReach(p *access.Principal, ws *int64) bool {
	if ws == nil {
		return p.Key == nil || !p.Key.Bound
	}
	return p.SeesWorkspace(*ws)
}

// ownFilterInReach checks that an existing filter of the caller is in reach (404 otherwise).
func (a *API) ownFilterInReach(ctx context.Context, p *access.Principal, id int64) error {
	fs, err := store.SavedFilters(ctx, a.st.R, p.User.ID)
	if err != nil {
		return err
	}
	for _, f := range fs {
		if f.ID == id && filterInReach(p, f.WorkspaceID) {
			return nil
		}
	}
	return errNotFound()
}

func archiveOp(arch bool) gigOp {
	if arch {
		return opEdit
	}
	return opGuarded
}
