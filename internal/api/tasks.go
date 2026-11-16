package api

import (
	"context"
	"database/sql"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/0x2alx/gigg/internal/access"
	"github.com/0x2alx/gigg/internal/store"
)

// task resolves an id or a key (current or alias) within reach. Trashed tasks resolve too.
func (a *API) task(ctx context.Context, p *access.Principal, ref string) (*store.Task, error) {
	var id int64
	if n, err := strconv.ParseInt(ref, 10, 64); err == nil {
		id = n
	} else {
		ids, err := store.TaskIDsByKey(ctx, a.st.R, strings.ToUpper(ref), p.Workspaces())
		if err != nil || len(ids) == 0 {
			return nil, errNotFound()
		}
		var visible []int64
		for _, cand := range ids {
			if t, err := store.GetTask(ctx, a.st.R, cand); err == nil && p.SeesProject(t.WorkspaceID, t.ProjectID) {
				visible = append(visible, cand)
			}
		}
		if len(visible) == 0 {
			return nil, errNotFound()
		}
		if len(visible) > 1 {
			return nil, fail(409, "ambiguous_key", "That key exists in more than one of your workspaces; use the task id.")
		}
		id = visible[0]
	}
	t, err := store.GetTask(ctx, a.st.R, id)
	if err != nil || !p.SeesProject(t.WorkspaceID, t.ProjectID) {
		return nil, errNotFound()
	}
	return t, nil
}

type taskPath struct {
	ID string `path:"id" doc:"Task id or key (e.g. GIG-12)"`
}

type taskOut struct {
	ETag string `header:"ETag"`
	Body *store.Task
}

// TaskList is a page of tasks.
type TaskList struct {
	Items      []*store.Task `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

type taskListIn struct {
	Status         int64  `query:"status" doc:"Status id"`
	Category       string `query:"category" enum:"backlog,todo,doing,waiting,done,cancelled,"`
	Label          string `query:"label" doc:"Label name"`
	PriorityMin    int64  `query:"priority_min" minimum:"0" maximum:"4"`
	Assignee       int64  `query:"assignee" doc:"User id"`
	CreatedBy      int64  `query:"created_by" doc:"User id"`
	Q              string `query:"q" maxLength:"200" doc:"Text search in titles, descriptions and comments"`
	DueBefore      int64  `query:"due_before" doc:"Unix ms"`
	UpdatedSince   int64  `query:"updated_since" doc:"Unix ms"`
	IncludeDeleted bool   `query:"include_deleted"`
	Deleted        bool   `query:"deleted" doc:"Only trashed tasks"`
	Sort           string `query:"sort" doc:"board (default) or a comma list of priority, due, created, updated, number, title; - for descending"`
	Limit          int    `query:"limit" minimum:"0" maximum:"500"`
	Cursor         string `query:"cursor"`
	Archived       string `query:"archived" enum:"only,include," doc:"Archived tasks are hidden unless only or include"`
	Parent         int64  `query:"parent" doc:"Only subtasks of this task id"`
	Blocked        bool   `query:"blocked" doc:"Only tasks with an open blocker"`
}

func (in *taskListIn) filter(ws int64) (store.TaskFilter, error) {
	f := store.TaskFilter{WorkspaceID: ws, Category: in.Category, Label: in.Label, Q: in.Q, IncludeDeleted: in.IncludeDeleted,
		OnlyDeleted: in.Deleted, Sort: in.Sort, Limit: in.Limit, Archived: in.Archived, BlockedOnly: in.Blocked}
	if in.Parent != 0 {
		f.ParentID = &in.Parent
	}
	if f.Limit == 0 {
		f.Limit = 50
	}
	if in.Status != 0 {
		f.StatusID = &in.Status
	}
	if in.PriorityMin != 0 {
		f.PriorityMin = &in.PriorityMin
	}
	if in.Assignee != 0 {
		f.Assignee = &in.Assignee
	}
	if in.CreatedBy != 0 {
		f.CreatedBy = &in.CreatedBy
	}
	if in.DueBefore != 0 {
		f.DueBefore = &in.DueBefore
	}
	if in.UpdatedSince != 0 {
		f.UpdatedSince = &in.UpdatedSince
	}
	if in.Cursor != "" {
		off, err := strconv.Atoi(in.Cursor)
		if err != nil || off < 0 {
			return f, fail(400, "bad_request", "Bad cursor.")
		}
		f.Offset = off
	}
	return f, nil
}

func page(ts []*store.Task, more bool, f store.TaskFilter) *TaskList {
	l := &TaskList{Items: ts}
	if l.Items == nil {
		l.Items = []*store.Task{}
	}
	if more {
		l.NextCursor = strconv.Itoa(f.Offset + len(ts))
	}
	return l
}

type wsTasksIn struct {
	WS             string `path:"ws"`
	Project        int64  `query:"project" doc:"Project id"`
	Status         int64  `query:"status" doc:"Status id"`
	Category       string `query:"category" enum:"backlog,todo,doing,waiting,done,cancelled,"`
	Label          string `query:"label" doc:"Label name"`
	PriorityMin    int64  `query:"priority_min" minimum:"0" maximum:"4"`
	Assignee       int64  `query:"assignee" doc:"User id"`
	CreatedBy      int64  `query:"created_by" doc:"User id"`
	Q              string `query:"q" maxLength:"200" doc:"Text search in titles, descriptions and comments"`
	DueBefore      int64  `query:"due_before" doc:"Unix ms"`
	UpdatedSince   int64  `query:"updated_since" doc:"Unix ms"`
	IncludeDeleted bool   `query:"include_deleted"`
	Deleted        bool   `query:"deleted" doc:"Only trashed tasks"`
	Sort           string `query:"sort" doc:"board (default) or a comma list of priority, due, created, updated, number, title; - for descending"`
	Limit          int    `query:"limit" minimum:"0" maximum:"500"`
	Cursor         string `query:"cursor"`
	Archived       string `query:"archived" enum:"only,include," doc:"Archived tasks are hidden unless only or include"`
	Parent         int64  `query:"parent" doc:"Only subtasks of this task id"`
	Blocked        bool   `query:"blocked" doc:"Only tasks with an open blocker"`
}

type projectTasksIn struct {
	P              int64  `path:"p"`
	Status         int64  `query:"status" doc:"Status id"`
	Category       string `query:"category" enum:"backlog,todo,doing,waiting,done,cancelled,"`
	Label          string `query:"label" doc:"Label name"`
	PriorityMin    int64  `query:"priority_min" minimum:"0" maximum:"4"`
	Assignee       int64  `query:"assignee" doc:"User id"`
	CreatedBy      int64  `query:"created_by" doc:"User id"`
	Q              string `query:"q" maxLength:"200" doc:"Text search in titles, descriptions and comments"`
	DueBefore      int64  `query:"due_before" doc:"Unix ms"`
	UpdatedSince   int64  `query:"updated_since" doc:"Unix ms"`
	IncludeDeleted bool   `query:"include_deleted"`
	Deleted        bool   `query:"deleted" doc:"Only trashed tasks"`
	Sort           string `query:"sort" doc:"board (default) or a comma list of priority, due, created, updated, number, title; - for descending"`
	Limit          int    `query:"limit" minimum:"0" maximum:"500"`
	Cursor         string `query:"cursor"`
	Archived       string `query:"archived" enum:"only,include," doc:"Archived tasks are hidden unless only or include"`
	Parent         int64  `query:"parent" doc:"Only subtasks of this task id"`
	Blocked        bool   `query:"blocked" doc:"Only tasks with an open blocker"`
}

func (in *wsTasksIn) list() *taskListIn {
	return &taskListIn{Status: in.Status, Category: in.Category, Label: in.Label, PriorityMin: in.PriorityMin, Assignee: in.Assignee, CreatedBy: in.CreatedBy, Q: in.Q, DueBefore: in.DueBefore, UpdatedSince: in.UpdatedSince, IncludeDeleted: in.IncludeDeleted, Deleted: in.Deleted, Sort: in.Sort, Limit: in.Limit, Cursor: in.Cursor, Archived: in.Archived, Parent: in.Parent, Blocked: in.Blocked}
}

func (in *projectTasksIn) list() *taskListIn {
	return &taskListIn{Status: in.Status, Category: in.Category, Label: in.Label, PriorityMin: in.PriorityMin, Assignee: in.Assignee, CreatedBy: in.CreatedBy, Q: in.Q, DueBefore: in.DueBefore, UpdatedSince: in.UpdatedSince, IncludeDeleted: in.IncludeDeleted, Deleted: in.Deleted, Sort: in.Sort, Limit: in.Limit, Cursor: in.Cursor, Archived: in.Archived, Parent: in.Parent, Blocked: in.Blocked}
}

// RelationIn is a dependency a new task starts with.
type RelationIn struct {
	Task      string `json:"task" doc:"The other task, id or key"`
	Direction string `json:"direction" enum:"blocks,blocked_by,relates" doc:"blocks: the new task blocks the other; blocked_by: the other blocks the new task"`
}

// TaskCreate is the body for a new task.
type TaskCreate struct {
	Checklist     []string                `json:"checklist,omitempty" maxItems:"200" doc:"Checklist items to start with"`
	Subtasks      []store.TemplateSubtask `json:"subtasks,omitempty" maxItems:"50" doc:"Subtasks to create with it, made like it (project, priority, assignees, window, run settings); blocks_parent = it waits for that subtask"`
	Relations     []RelationIn            `json:"relations,omitempty" maxItems:"50" doc:"Dependencies to start with (needs the relations permission)"`
	Custom        map[string]any          `json:"custom,omitempty" doc:"Custom field values by field id"`
	Title         string                  `json:"title" minLength:"1" maxLength:"250"`
	DescriptionMD string                  `json:"description_md,omitempty" maxLength:"65536"`
	Status        int64                   `json:"status,omitempty" doc:"Status id (default: the project's default status)"`
	Category      string                  `json:"category,omitempty" enum:"backlog,todo,doing,waiting,done,cancelled," doc:"Alternative to status"`
	Priority      int64                   `json:"priority,omitempty" minimum:"0" maximum:"4"`
	Color         *string                 `json:"color,omitempty"`
	StartAt       *int64                  `json:"start_at,omitempty"`
	DueAt         *int64                  `json:"due_at,omitempty"`
	NotAfter      *int64                  `json:"not_after,omitempty" doc:"The window closes (unix ms): not claimable after this; untouched it becomes missed"`
	ParentID      *int64                  `json:"parent_id,omitempty"`
	BlocksParent  bool                    `json:"blocks_parent,omitempty" doc:"A subtask that holds its parent back while it is open (needs parent_id)"`
	Labels        []string                `json:"labels,omitempty" maxItems:"20" doc:"Label names; missing ones are created"`
	Assignees     []int64                 `json:"assignees,omitempty"`
	Model         string                  `json:"model,omitempty" maxLength:"64" doc:"Model for the agent's claude -p, e.g. opus, sonnet, claude-opus-5-5 (default: the agent's)"`
	Effort        string                  `json:"effort,omitempty" enum:"low,medium,high,xhigh,max," doc:"Effort level for claude -p (default: the agent's)"`
	AlwaysRun     bool                    `json:"always_run,omitempty" doc:"Work it whatever the usage budget, day or night (needs the always_run permission)"`
	AllowedTools  []string                `json:"allowed_tools,omitempty" maxItems:"64" doc:"Tools the runner's claude -p may use for it (Claude Code permission rules, one per entry, e.g. Read, Bash(git status *)); it then runs with --permission-mode dontAsk. Needs the allowed_tools permission"`
}

type taskCreateIn struct {
	P    int64 `path:"p"`
	Body TaskCreate
}

type taskPatchIn struct {
	ID      string `path:"id"`
	IfMatch string `header:"If-Match"`
	Body    struct {
		Title         Opt[string]   `json:"title,omitempty" maxLength:"250"`
		DescriptionMD Opt[string]   `json:"description_md,omitempty" maxLength:"65536"`
		Priority      Opt[int64]    `json:"priority,omitempty" minimum:"0" maximum:"4"`
		Color         Opt[string]   `json:"color,omitempty"`
		StartAt       Opt[int64]    `json:"start_at,omitempty"`
		DueAt         Opt[int64]    `json:"due_at,omitempty"`
		NotAfter      Opt[int64]    `json:"not_after,omitempty"`
		ParentID      Opt[int64]    `json:"parent_id,omitempty"`
		BlocksParent  Opt[bool]     `json:"blocks_parent,omitempty" doc:"A subtask that holds its parent back while it is open"`
		Labels        Opt[[]string] `json:"labels,omitempty" doc:"Replaces the task's labels (names)"`
		Assignees     Opt[[]int64]  `json:"assignees,omitempty" doc:"Replaces the task's assignees (user ids)"`
		Model         Opt[string]   `json:"model,omitempty" maxLength:"64" doc:"Model for claude -p; null or empty = the agent's default"`
		Effort        Opt[string]   `json:"effort,omitempty" enum:"low,medium,high,xhigh,max," doc:"Effort for claude -p; null or empty = the agent's default"`
		AlwaysRun     Opt[bool]     `json:"always_run,omitempty" doc:"Work it whatever the usage budget (needs the always_run permission)"`
		AllowedTools  Opt[[]string] `json:"allowed_tools,omitempty" maxItems:"64" doc:"Replaces the runner's tool allowlist; null or [] clears (needs the allowed_tools permission)"`
	}
}

type taskMoveIn struct {
	ID      string `path:"id"`
	IfMatch string `header:"If-Match"`
	Body    struct {
		Status   int64  `json:"status,omitempty" doc:"Target status id"`
		Category string `json:"category,omitempty" enum:"backlog,todo,doing,waiting,done,cancelled," doc:"Alternative: first status of this category"`
		Project  int64  `json:"project,omitempty" doc:"Another project of the same workspace (gets a new key; the old one stays an alias)"`
		Position string `json:"position,omitempty" enum:"top,bottom,before,after," doc:"Default: bottom (keep when the status doesn't change)"`
		Task     int64  `json:"task,omitempty" doc:"Reference task for before/after"`
	}
}

type taskCloneIn struct {
	ID   string `path:"id"`
	Body struct {
		Project int64   `json:"project,omitempty" doc:"Target project in the same workspace (default: the same)"`
		Title   *string `json:"title,omitempty" maxLength:"250"`
	}
}

type resumeIn struct {
	ID   string `path:"id"`
	Body struct {
		CommentMD string `json:"comment_md,omitempty" maxLength:"65536" doc:"Answer or instructions for the next run"`
	}
}

type commentsIn struct {
	ID      string `path:"id"`
	SinceID int64  `query:"since_id" doc:"Only comments after this id"`
	Limit   int    `query:"limit" minimum:"0" maximum:"500"`
}

type commentsOut struct {
	Body struct {
		Items []*store.Comment `json:"items"`
	}
}

type commentCreateIn struct {
	ID   string `path:"id"`
	Body struct {
		BodyMD string `json:"body_md" minLength:"1" maxLength:"65536"`
		Kind   string `json:"kind,omitempty" enum:"comment,report,question," doc:"Default: comment"`
		RunID  *int64 `json:"run_id,omitempty"`
	}
}

type commentOut struct {
	ETag string `header:"ETag"`
	Body *store.Comment
}

type commentPatchIn struct {
	ID      int64  `path:"id"`
	IfMatch string `header:"If-Match"`
	Body    struct {
		BodyMD string `json:"body_md" minLength:"1" maxLength:"65536"`
	}
}

// ActivityItem is one entry of the activity log.
type ActivityItem struct {
	ID         int64  `json:"id"`
	At         int64  `json:"at"`
	ActorID    *int64 `json:"actor_user_id"`
	KeyID      *int64 `json:"key_id"`
	Listener   string `json:"listener"`
	Action     string `json:"action"`
	EntityType string `json:"entity_type"`
	EntityID   *int64 `json:"entity_id"`
	ProjectID  *int64 `json:"project_id"`
	Data       any    `json:"data"`
	// RequestID: the request that made the entry, only on the event stream and the change feed and
	// only for the viewer's own entries.
	RequestID string `json:"request_id,omitempty"`
}

type activityOut struct {
	Body struct {
		Items []ActivityItem `json:"items"`
	}
}

func (a *API) registerTasks() {
	huma.Register(a.h, huma.Operation{OperationID: "list-workspace-tasks", Method: "GET", Path: "/workspaces/{ws}/tasks", Tags: []string{"tasks"},
		Summary: "Tasks across a workspace's projects (filters combine)"}, func(ctx context.Context, in *wsTasksIn) (*struct{ Body *TaskList }, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		w, err := a.workspace(ctx, p, in.WS)
		if err != nil {
			return nil, err
		}
		if err := require(p, w.ID, access.Viewer, "tasks:read"); err != nil {
			return nil, err
		}
		f, err := in.list().filter(w.ID)
		if err != nil {
			return nil, err
		}
		f.Projects = p.ProjectFilter()
		f.SearchComments = p.HasScope("comments:read")
		if in.Project != 0 {
			if !p.SeesProject(w.ID, in.Project) {
				return nil, errNotFound()
			}
			f.Projects = []int64{in.Project}
		}
		ts, more, err := store.ListTasks(ctx, a.st.R, f)
		if err == nil && p.HasScope("comments:read") {
			err = store.AttachQuestions(ctx, a.st.R, ts)
		}
		if err != nil {
			return nil, storeErr(err)
		}
		return &struct{ Body *TaskList }{Body: page(ts, more, f)}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "list-project-tasks", Method: "GET", Path: "/projects/{p}/tasks", Tags: []string{"tasks"},
		Summary: "A project's tasks (filters combine)"}, func(ctx context.Context, in *projectTasksIn) (*struct{ Body *TaskList }, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		pr, err := a.project(ctx, p, in.P)
		if err != nil {
			return nil, err
		}
		if err := require(p, pr.WorkspaceID, access.Viewer, "tasks:read"); err != nil {
			return nil, err
		}
		f, err := in.list().filter(pr.WorkspaceID)
		if err != nil {
			return nil, err
		}
		f.Projects = []int64{pr.ID}
		f.SearchComments = p.HasScope("comments:read")
		ts, more, err := store.ListTasks(ctx, a.st.R, f)
		if err == nil && p.HasScope("comments:read") {
			err = store.AttachQuestions(ctx, a.st.R, ts)
		}
		if err != nil {
			return nil, storeErr(err)
		}
		return &struct{ Body *TaskList }{Body: page(ts, more, f)}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "create-task", Method: "POST", Path: "/projects/{p}/tasks", Tags: []string{"tasks"},
		Summary: "Create a task", DefaultStatus: 201}, func(ctx context.Context, in *taskCreateIn) (*taskOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		pr, err := a.project(ctx, p, in.P)
		if err != nil {
			return nil, err
		}
		if err := require(p, pr.WorkspaceID, access.Member, "tasks:write"); err != nil {
			return nil, err
		}
		if err := allowed(p, pr.WorkspaceID, "create_tasks"); err != nil {
			return nil, err
		}
		if len(in.Body.Checklist) > 0 {
			if err := allowed(p, pr.WorkspaceID, "checklists"); err != nil {
				return nil, err
			}
		}
		// a subtask that blocks its parent holds another task back, like a dependency
		if len(in.Body.Relations) > 0 || in.Body.BlocksParent || anyGate(in.Body.Subtasks) {
			if err := allowed(p, pr.WorkspaceID, "relations"); err != nil {
				return nil, err
			}
		}
		b := in.Body
		if err := alwaysRunChange(p, pr.WorkspaceID, false, b.AlwaysRun); err != nil {
			return nil, err
		}
		if err := toolsChange(p, pr.WorkspaceID, nil, b.AllowedTools); err != nil {
			return nil, err
		}
		n := store.NewTask{ProjectID: pr.ID, Title: b.Title, DescriptionMD: b.DescriptionMD, Priority: b.Priority, Color: b.Color,
			StartAt: b.StartAt, DueAt: b.DueAt, NotAfter: b.NotAfter, ParentID: b.ParentID, Labels: b.Labels, Assignees: b.Assignees,
			Model: strings.TrimSpace(b.Model), Effort: b.Effort, AlwaysRun: b.AlwaysRun, AllowedTools: b.AllowedTools, BlocksParent: b.BlocksParent}
		if p.Key != nil {
			n.CreatedViaKey = &p.Key.ID
		}
		var t *store.Task
		err = a.tx(ctx, func(tx *sql.Tx) error {
			if b.Status != 0 {
				n.StatusID = &b.Status
			} else if b.Category != "" {
				s, err := store.FirstStatusOf(ctx, tx, pr.ID, b.Category)
				if err != nil {
					return store.Invalid("the project has no %s status", b.Category)
				}
				n.StatusID = &s.ID
			}
			if b.ParentID != nil {
				if par, err := store.GetTask(ctx, tx, *b.ParentID); err != nil || !p.SeesProject(par.WorkspaceID, par.ProjectID) {
					return store.Invalid("no such parent task")
				}
			}
			var err error
			if t, err = store.CreateTask(ctx, tx, p.Actor(), n); err != nil {
				return err
			}
			if len(b.Checklist) > 0 {
				if err := store.AddChecklistItems(ctx, tx, p.Actor(), t, b.Checklist); err != nil {
					return err
				}
			}
			if len(b.Custom) > 0 {
				if err := store.SetCustomValues(ctx, tx, p.Actor(), t, b.Custom); err != nil {
					return err
				}
			}
			if err := store.AddSubtasks(ctx, tx, p.Actor(), t, b.Subtasks); err != nil {
				return err
			}
			for _, r := range b.Relations {
				o, err := a.task(ctx, p, r.Task)
				if err != nil {
					return store.Invalid("no such task %q", r.Task)
				}
				from, to, kind := t, o, "blocks"
				switch r.Direction {
				case "blocked_by":
					from, to = o, t
				case "relates":
					kind = "relates"
					if o.ID < t.ID {
						from, to = o, t
					}
				}
				if err := store.AddRelation(ctx, tx, p.Actor(), from, to, kind); err != nil {
					return err
				}
			}
			if len(b.Checklist)+len(b.Custom)+len(b.Subtasks)+len(b.Relations) > 0 {
				t, err = store.GetTask(ctx, tx, t.ID)
			}
			return err
		})
		if err != nil {
			return nil, err
		}
		return &taskOut{ETag: etag(t.Version), Body: t}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "get-task", Method: "GET", Path: "/tasks/{id}", Tags: []string{"tasks"},
		Summary: "A task, by id or key"}, func(ctx context.Context, in *taskPath) (*taskOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		t, err := a.task(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if err := require(p, t.WorkspaceID, access.Viewer, "tasks:read"); err != nil {
			return nil, err
		}
		return &taskOut{ETag: etag(t.Version), Body: t}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "patch-task", Method: "PATCH", Path: "/tasks/{id}", Tags: []string{"tasks"},
		Summary: "Change a task's fields (null clears)"}, func(ctx context.Context, in *taskPatchIn) (*taskOut, error) {
		return a.withTask(ctx, in.ID, in.IfMatch, access.Member, "tasks:write", func(tx *sql.Tx, p *access.Principal, t *store.Task, v int64) (*store.Task, error) {
			b := in.Body
			if err := alwaysRunGuard(p, t.WorkspaceID, t.AlwaysRun); err != nil {
				return nil, err
			}
			if err := toolsGuard(p, t.WorkspaceID, len(t.AllowedTools) > 0); err != nil {
				return nil, err
			}
			if pid := b.ParentID.Val(); pid != nil {
				if par, err := store.GetTask(ctx, tx, *pid); err != nil || !p.SeesProject(par.WorkspaceID, par.ProjectID) {
					return nil, store.Invalid("no such parent task")
				}
			}
			patch := store.TaskPatch{Title: b.Title.Val(), DescriptionMD: b.DescriptionMD.Val(), Priority: b.Priority.Val(), Color: b.Color.Ptr(),
				StartAt: b.StartAt.Ptr(), DueAt: b.DueAt.Ptr(), NotAfter: b.NotAfter.Ptr(), ParentID: b.ParentID.Ptr()}
			if b.BlocksParent.Set {
				on := false
				if v := b.BlocksParent.Val(); v != nil {
					on = *v
				}
				patch.BlocksParent = &on
			}
			// switching a subtask's hold on its parent, or moving a holding subtask under another
			// parent, is a dependency change
			if (patch.BlocksParent != nil && *patch.BlocksParent != t.BlocksParent) || (b.ParentID.Set && t.BlocksParent) {
				if err := allowed(p, t.WorkspaceID, "relations"); err != nil {
					return nil, err
				}
			}
			if b.Model.Set {
				m := ""
				if v := b.Model.Val(); v != nil {
					m = *v
				}
				patch.Model = &m
			}
			if b.Effort.Set {
				e := ""
				if v := b.Effort.Val(); v != nil {
					e = *v
				}
				patch.Effort = &e
			}
			if b.AlwaysRun.Set {
				on := false
				if v := b.AlwaysRun.Val(); v != nil {
					on = *v
				}
				if err := alwaysRunChange(p, t.WorkspaceID, t.AlwaysRun, on); err != nil {
					return nil, err
				}
				patch.AlwaysRun = &on
			}
			if b.AllowedTools.Set {
				tools := []string{}
				if v := b.AllowedTools.Val(); v != nil && *v != nil {
					tools = *v
				}
				if err := toolsChange(p, t.WorkspaceID, t.AllowedTools, tools); err != nil {
					return nil, err
				}
				patch.AllowedTools = tools
			}
			if l := b.Labels.Val(); l != nil {
				patch.Labels = *l
				if patch.Labels == nil {
					patch.Labels = []string{}
				}
			} else if b.Labels.Null {
				patch.Labels = []string{}
			}
			if as := b.Assignees.Val(); as != nil {
				patch.Assignees = *as
				if patch.Assignees == nil {
					patch.Assignees = []int64{}
				}
			} else if b.Assignees.Null {
				patch.Assignees = []int64{}
			}
			return store.PatchTask(ctx, tx, p.Actor(), t, v, patch)
		})
	})
	huma.Register(a.h, huma.Operation{OperationID: "move-task", Method: "POST", Path: "/tasks/{id}/move", Tags: []string{"tasks"},
		Summary: "Move a task: status, position, or another project of the workspace"}, func(ctx context.Context, in *taskMoveIn) (*taskOut, error) {
		return a.withTask(ctx, in.ID, in.IfMatch, access.Member, "tasks:write", func(tx *sql.Tx, p *access.Principal, t *store.Task, v int64) (*store.Task, error) {
			b := in.Body
			m := store.Move{Category: b.Category, Place: store.Place{Where: b.Position, TaskID: b.Task}}
			if b.Status != 0 {
				m.StatusID = &b.Status
			}
			if b.Project != 0 && b.Project != t.ProjectID {
				if !p.SeesProject(t.WorkspaceID, b.Project) {
					return nil, store.Invalid("no such project in this workspace")
				}
				m.ProjectID = &b.Project
			}
			if (b.Position == "before" || b.Position == "after") && b.Task == 0 {
				return nil, store.Invalid("before/after need a reference task")
			}
			moved, err := store.MoveTask(ctx, tx, p.Actor(), t, v, m)
			if err != nil {
				return nil, err
			}
			// reopening an always-run task (or one with tools) runs it again: judged on where it actually
			// landed (a move to another project can fall back to that project's default status)
			if open := func(c string) bool { return c == "todo" || c == "doing" }; open(moved.StatusCategory) && !open(t.StatusCategory) {
				if err := guardTask(p, t); err != nil {
					return nil, err // the transaction rolls back
				}
			}
			return moved, nil
		})
	})
	huma.Register(a.h, huma.Operation{OperationID: "clone-task", Method: "POST", Path: "/tasks/{id}/clone", Tags: []string{"tasks"},
		Summary: "Copy a task (optionally into another project of the workspace)", DefaultStatus: 201}, func(ctx context.Context, in *taskCloneIn) (*taskOut, error) {
		return a.withTask(ctx, in.ID, "", access.Member, "tasks:write", func(tx *sql.Tx, p *access.Principal, t *store.Task, _ int64) (*store.Task, error) {
			if err := allowed(p, t.WorkspaceID, "create_tasks"); err != nil {
				return nil, err
			}
			var proj *int64
			if in.Body.Project != 0 {
				if !p.SeesProject(t.WorkspaceID, in.Body.Project) {
					return nil, store.Invalid("no such project in this workspace")
				}
				proj = &in.Body.Project
			}
			var via *int64
			if p.Key != nil {
				via = &p.Key.ID
			}
			return store.CloneTask(ctx, tx, p.Actor(), t, proj, in.Body.Title, via)
		})
	})
	huma.Register(a.h, huma.Operation{OperationID: "delete-task", Method: "DELETE", Path: "/tasks/{id}", Tags: []string{"tasks"},
		Summary: "Move a task to the trash (restore with /restore)"}, func(ctx context.Context, in *taskPath) (*taskOut, error) {
		return a.withTaskOp(ctx, in.ID, "", access.Member, "tasks:delete", opTrashed, func(tx *sql.Tx, p *access.Principal, t *store.Task, _ int64) (*store.Task, error) {
			if err := allowed(p, t.WorkspaceID, "delete_tasks"); err != nil {
				return nil, err
			}
			return store.TrashTask(ctx, tx, p.Actor(), t, false)
		})
	})
	huma.Register(a.h, huma.Operation{OperationID: "restore-task", Method: "POST", Path: "/tasks/{id}/restore", Tags: []string{"tasks"},
		Summary: "Restore a task from the trash"}, func(ctx context.Context, in *taskPath) (*taskOut, error) {
		return a.withTaskOp(ctx, in.ID, "", access.Member, "tasks:delete", opTrashed|opGuarded, func(tx *sql.Tx, p *access.Principal, t *store.Task, _ int64) (*store.Task, error) {
			if err := allowed(p, t.WorkspaceID, "delete_tasks"); err != nil {
				return nil, err
			}
			return store.TrashTask(ctx, tx, p.Actor(), t, true)
		})
	})
	huma.Register(a.h, huma.Operation{OperationID: "resume-task", Method: "POST", Path: "/tasks/{id}/resume", Tags: []string{"tasks"},
		Summary: "Ask for the task's agent session to be resumed: records the request and moves it to todo (gigg runs nothing)"},
		func(ctx context.Context, in *resumeIn) (*taskOut, error) {
			return a.withTask(ctx, in.ID, "", access.Member, "tasks:write", func(tx *sql.Tx, p *access.Principal, t *store.Task, _ int64) (*store.Task, error) {
				if in.Body.CommentMD != "" && !p.HasScope("comments:write") {
					return nil, errForbidden("This key lacks the comments:write scope.")
				}
				if err := alwaysRunGuard(p, t.WorkspaceID, t.AlwaysRun); err != nil {
					return nil, err
				}
				if err := toolsGuard(p, t.WorkspaceID, len(t.AllowedTools) > 0); err != nil {
					return nil, err
				}
				return store.ResumeTask(ctx, tx, p.Actor(), t, in.Body.CommentMD)
			})
		})
	huma.Register(a.h, huma.Operation{OperationID: "task-activity", Method: "GET", Path: "/tasks/{id}/activity", Tags: []string{"tasks"},
		Summary: "A task's history (old values included)"}, func(ctx context.Context, in *taskPath) (*activityOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		t, err := a.task(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if err := require(p, t.WorkspaceID, access.Viewer, "tasks:read"); err != nil {
			return nil, err
		}
		if err := need(p, "comments:read"); err != nil {
			return nil, err
		}
		items, err := a.activity(ctx, `(entity_type = 'task' AND entity_id = ?) OR (entity_type IN ('comment','run') AND action != 'run.progress' AND json_extract(data, '$.task_id') = ?)`, []any{t.ID, t.ID}, 0, 500)
		if !p.IsInstanceAdmin() {
			redactDeleted(items)
		}
		out := &activityOut{}
		out.Body.Items = items
		return out, storeErr(err)
	})

	huma.Register(a.h, huma.Operation{OperationID: "list-comments", Method: "GET", Path: "/tasks/{id}/comments", Tags: []string{"comments"},
		Summary: "A task's comments, oldest first, with author facts"}, func(ctx context.Context, in *commentsIn) (*commentsOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		t, err := a.task(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if err := require(p, t.WorkspaceID, access.Viewer, "comments:read"); err != nil {
			return nil, err
		}
		out := &commentsOut{}
		out.Body.Items, err = store.Comments(ctx, a.st.R, t.ID, in.SinceID, in.Limit)
		if out.Body.Items == nil {
			out.Body.Items = []*store.Comment{}
		}
		return out, storeErr(err)
	})
	huma.Register(a.h, huma.Operation{OperationID: "create-comment", Method: "POST", Path: "/tasks/{id}/comments", Tags: []string{"comments"},
		Summary: "Comment on a task (Markdown)", DefaultStatus: 201}, func(ctx context.Context, in *commentCreateIn) (*commentOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		t, err := a.task(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if t.Deleted {
			return nil, trashErr(t)
		}
		if err := require(p, t.WorkspaceID, access.Commenter, "comments:write"); err != nil {
			return nil, err
		}
		if !store.IsClientCommentKind(in.Body.Kind) {
			return nil, fail(422, "invalid", "kind is comment, report or question")
		}
		var c *store.Comment
		err = a.tx(ctx, func(tx *sql.Tx) error {
			if in.Body.RunID != nil {
				if r, err := store.GetRun(ctx, tx, *in.Body.RunID); err != nil || r.TaskID != t.ID {
					return store.Invalid("run %d is not a run of this task", *in.Body.RunID)
				}
			}
			var err error
			c, err = store.AddComment(ctx, tx, p.Actor(), t, in.Body.Kind, in.Body.BodyMD, in.Body.RunID)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &commentOut{ETag: etag(c.Version), Body: c}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "edit-comment", Method: "PATCH", Path: "/comments/{id}", Tags: []string{"comments"},
		Summary: "Edit your comment (the old text stays in the history)"}, func(ctx context.Context, in *commentPatchIn) (*commentOut, error) {
		p, c, err := a.comment(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		if c.AuthorID != p.User.ID {
			return nil, errForbidden("Only the author can edit a comment.")
		}
		if err := require(p, c.WorkspaceID, access.Commenter, "comments:write"); err != nil {
			return nil, err
		}
		v, err := ifMatch(in.IfMatch)
		if err != nil {
			return nil, err
		}
		err = a.tx(ctx, func(tx *sql.Tx) error {
			var err error
			c, err = store.EditComment(ctx, tx, p.Actor(), c, v, in.Body.BodyMD)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &commentOut{ETag: etag(c.Version), Body: c}, nil
	})
	huma.Register(a.h, huma.Operation{OperationID: "delete-comment", Method: "DELETE", Path: "/comments/{id}", Tags: []string{"comments"},
		Summary: "Delete a comment (yours, or any as a manager)", DefaultStatus: 204}, func(ctx context.Context, in *idPath) (*struct{}, error) {
		p, c, err := a.comment(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		min := access.Commenter
		if c.AuthorID != p.User.ID {
			min = access.Manager
		}
		if err := require(p, c.WorkspaceID, min, "comments:write"); err != nil {
			return nil, err
		}
		return nil, a.tx(ctx, func(tx *sql.Tx) error { return store.DeleteComment(ctx, tx, p.Actor(), c) })
	})
}

func (a *API) comment(ctx context.Context, id int64) (*access.Principal, *store.Comment, error) {
	p, err := me(ctx)
	if err != nil {
		return nil, nil, err
	}
	c, err := store.GetComment(ctx, a.st.R, id)
	if err != nil {
		return nil, nil, errNotFound()
	}
	t, err := store.GetTask(ctx, a.st.R, c.TaskID)
	if err != nil || !p.SeesProject(t.WorkspaceID, t.ProjectID) {
		return nil, nil, errNotFound()
	}
	return p, c, nil
}

// withTask resolves the task, checks role and scope, and runs fn in a transaction with the
// freshly read task.
// taskOp says what a change may do beyond the usual role and scope checks.
type taskOp int

const (
	opEdit    taskOp = 0         // a live task only
	opGuarded taskOp = 1 << iota // also needs always_run / allowed_tools when the task has them
	opTrashed                    // allowed on a task in the trash too (delete, restore)
)

// trashErr: a task in the trash can only be restored (or read).
func trashErr(t *store.Task) error {
	return fail(409, "in_trash", t.Key+" is in the trash: restore it first.")
}

// guardTask: changing what an always-run task (or one with a tool allowlist) does needs those
// permissions too, whichever endpoint the change comes through.
func guardTask(p *access.Principal, t *store.Task) error {
	if err := alwaysRunGuard(p, t.WorkspaceID, t.AlwaysRun); err != nil {
		return err
	}
	return toolsGuard(p, t.WorkspaceID, len(t.AllowedTools) > 0)
}

func (a *API) withTask(ctx context.Context, ref, ifm string, min, scope string,
	fn func(tx *sql.Tx, p *access.Principal, t *store.Task, v int64) (*store.Task, error)) (*taskOut, error) {
	return a.withTaskOp(ctx, ref, ifm, min, scope, opEdit, fn)
}

func (a *API) withTaskOp(ctx context.Context, ref, ifm string, min, scope string, op taskOp,
	fn func(tx *sql.Tx, p *access.Principal, t *store.Task, v int64) (*store.Task, error)) (*taskOut, error) {
	p, err := me(ctx)
	if err != nil {
		return nil, err
	}
	t, err := a.task(ctx, p, ref)
	if err != nil {
		return nil, err
	}
	if err := require(p, t.WorkspaceID, min, scope); err != nil {
		return nil, err
	}
	v, err := ifMatch(ifm)
	if err != nil {
		return nil, err
	}
	var out *store.Task
	err = a.tx(ctx, func(tx *sql.Tx) error {
		cur, err := store.GetTask(ctx, tx, t.ID)
		if err != nil {
			return err
		}
		if cur.Deleted && op&opTrashed == 0 {
			return trashErr(cur)
		}
		if op&opGuarded != 0 {
			if err := guardTask(p, cur); err != nil {
				return err
			}
		}
		out, err = fn(tx, p, cur, v)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &taskOut{ETag: etag(out.Version), Body: out}, nil
}

// anyGate: some of these subtasks would hold their parent back (that needs the relations permission).
func anyGate(subs []store.TemplateSubtask) bool {
	for _, s := range subs {
		if s.BlocksParent {
			return true
		}
	}
	return false
}
