# MCP

gigg serves the Model Context Protocol so agents can use the board as a set of tools.

## Connecting

| | |
|---|---|
| Endpoint | `/mcp` on any listener, e.g. `https://board.example.com/mcp` or `http://127.0.0.1:8080/mcp` |
| Transport | Streamable HTTP, stateless, JSON responses (no server-sent stream) |
| Authentication | `Authorization: Bearer gigg_<prefix>_<secret>` (an API key; browser sessions are refused) |
| Request body limit | 1 MiB |

Requests without a key get 401 with `WWW-Authenticate: Bearer realm="gigg"`. A request that sends an
`Origin` header is refused (403) unless it is the origin of `GIGG_PUBLIC_URL` or the very host and port
the request was sent to (when that host is an allowed host); without `GIGG_PUBLIC_URL` any `Origin` is
refused. Command-line MCP clients normally send no `Origin`. The key's listener and network restrictions, rate limit and scopes
apply as for the REST API, and the `Host` header must be an allowed host.

Claude Code:

```sh
claude mcp add --transport http gigg https://board.example.com/mcp \
  --header "Authorization: Bearer $GIGG_KEY"
```

Generic client configuration:

```json
{
  "mcpServers": {
    "gigg": {
      "type": "http",
      "url": "http://127.0.0.1:8080/mcp",
      "headers": { "Authorization": "Bearer gigg_abcd2345_..." }
    }
  }
}
```

A workspace-bound key is the usual choice: tools that take a `workspace` argument then default to the
key's workspace. With an unbound key, pass `workspace` (id or slug) explicitly.

## How tools work

Every tool is a call (or a few calls) to the REST API, made in-process with the caller's own key, so
MCP can never do more than the key can do through the API. Results are the API's JSON response as
text. API errors come back as a tool error whose text is `Error <status> <code>: <detail>`, followed by
validation details. A 204 answer is reported as `Nothing (204).`

The tool list depends on the key: a tool is listed only when the key has the scopes it needs, and some
also need a permission somewhere ("anywhere" below means in at least one workspace in reach). The
API still checks the permission in the task's own workspace on every call.

Times are Unix milliseconds. Tasks are referenced by id or key (`WEB-12`).

There are no MCP tools for claiming tasks or recording runs; runners use the REST API for that (see
[runner-protocol.md](runner-protocol.md)). An agent inside a run can use `report_progress`.

## Tools

### Reading

| Tool | Needs | Arguments | Does |
|---|---|---|---|
| `whoami` | `workspace:read` | none | `GET /auth/me`: identity, workspaces with permissions, key scopes. |
| `list_projects` | `workspace:read` | `workspace?` | Projects of a workspace, with ids and task key prefixes. |
| `summary` | `tasks:read` | `workspace?` | Plain-text board summary: per project, each status with up to 5 tasks. |
| `list_tasks` | `tasks:read` | `workspace?`, `project?`, `category?`, `label?`, `q?`, `priority_min?` (0-4), `sort?`, `limit?` (default 50, max 500), `cursor?`, `archived?` (`only`/`include`), `parent?`, `blocked?` | Finds tasks; filters combine; board order unless `sort` is given. |
| `get_task` | `tasks:read` | `task`, `comments?` | One task; with `comments: true` (and `comments:read`) returns `{"task": ..., "comments": ...}`. |
| `list_runs` | `tasks:read` | `task` | The task's runs (sessions, status, cost), newest first. |
| `list_comments` | `comments:read` | `task`, `since_id?` | Comments with author facts, oldest first. |
| `list_relations` | `tasks:read` | `task` | What blocks the task, what it blocks, related tasks. |
| `get_checklist` | `tasks:read` | `task` | Checklist items (ids, text, done). |
| `list_fields` | `tasks:read` | `workspace?` | Custom fields (ids, kinds, options). |
| `list_templates` | `tasks:read` | `workspace?`, `project?` | Templates you can create tasks from. |
| `list_recurring` | `tasks:read` | `workspace?`, `project?` | Recurring tasks with upcoming occurrences. |
| `list_schedules` | `tasks:read` | `workspace?` | Schedules and how many recurring tasks use each. |
| `changes` | `tasks:read` | `workspace?`, `after_id?` (id or `latest`), `limit?` (default 100, max 500) | Activity after an id, oldest first; pass `next_after_id` back as `after_id`. The API also requires `workspace:read` and `comments:read`. |
| `notifications` | `tasks:read` | `unread?`, `limit?` | Your notifications: mentions, assignments, questions, missed windows. |
| `runner_status` | `tasks:read` | none | Per workspace: task caps, pause state, last 24 hours of agent work. |

### Writing tasks

| Tool | Needs | Arguments | Does |
|---|---|---|---|
| `create_task` | `tasks:write` | `project`, `title`, `description_md?`, `category?`, `priority?` (0 none .. 4 urgent), `labels?` (created if missing), `color?`, `start_at?`, `due_at?`, `not_after?`, `parent_id?`, `blocks_parent?` (with `parent_id`: the parent waits for this subtask), `checklist?`, `custom?`, `model?`, `effort?`, `always_run?`, `allowed_tools?` | `POST /projects/{project}/tasks`. |
| `update_task` | `tasks:write` | `task`, `title?`, `description_md?`, `priority?`, `labels?` (replaces), `color?`, `start_at?`, `due_at?`, `not_after?`, `model?`, `effort?`, `always_run?`, `allowed_tools?` (`[]` clears), `blocks_parent?` (a subtask), `clear?` (any of `color`, `start_at`, `due_at`, `not_after`, `labels`) | `PATCH /tasks/{task}`. An empty `model`/`effort` resets to the agent's default. |
| `move_task` | `tasks:write` | `task`, `category?` or `status?`, `project?`, `position?` (`top`, `bottom`, `before`, `after`), `relative_to?` | `POST /tasks/{task}/move`. |
| `clone_task` | `tasks:write` | `task`, `project?`, `title?` | Copies a task: its content, labels, assignees, dates, custom field values and checklist (unticked). |
| `resume_task` | `tasks:write` | `task`, `comment_md?` | Asks for the task's agent session to be resumed (records the request, moves the task to todo; runs nothing). |
| `set_custom_fields` | `tasks:write` | `task`, `values` (field id -> value, `null` clears) | `PATCH /tasks/{task}/custom`. |
| `link_tasks` | `tasks:write`, `relations` anywhere | `task`, `other`, `direction` (`blocks`, `blocked_by`, `relates`) | Adds a dependency. A blocked task isn't claimed until its blockers are done. |
| `unlink_tasks` | `tasks:write`, `relations` anywhere | `task`, `other`, `direction` | Removes a dependency. |
| `update_checklist` | `tasks:write`, `checklists` anywhere | `task`, `add?`, `check?`, `uncheck?`, `remove?` (item ids) | Applies the changes in that order and returns the checklist. Stops at the first failing step. |
| `archive_task` | `tasks:write`, `archive_tasks` anywhere | `task`, `restore?` | Archives a task, or unarchives it with `restore: true`. |
| `create_from_template` | `tasks:write`, `create_tasks` anywhere | `template`, `project`, `title?` | Creates a task from a template. |
| `save_template` | `tasks:write`, `templates` anywhere | `workspace?`, `id?` (replace), `project_id?`, `name`, `title`, `description_md?`, `priority?`, `labels?`, `assignees?`, `checklist?`, `subtasks?` (`{title, blocks_parent?}` each), `custom?`, `model?`, `effort?`, `always_run?`, `allowed_tools?` | Creates a template, or replaces template `id`. Tasks made from it get its subtasks. |
| `delete_task` | `tasks:delete` | `task` | Moves a task to the trash. |
| `restore_task` | `tasks:delete` | `task` | Restores a task from the trash. |
| `add_comment` | `comments:write` | `task`, `body_md`, `kind?` (`comment`, `report`, `question`) | Comments on a task. |
| `mark_notifications_read` | `tasks:read` | `ids?` | Marks those notifications read, or all of them. |

### Recurring tasks and schedules

| Tool | Needs | Arguments | Does |
|---|---|---|---|
| `create_recurring` | `tasks:write`, `schedules` anywhere | `project`, content (`title`, `description_md`, `priority`, `labels`, `assignees`, `checklist`, `subtasks`, `model`, `effort`, `always_run`, `allowed_tools`) or `template_id`; timing (`freq` `daily`/`weekly`/`monthly`, `interval`, `weekdays` 1=Mon..7=Sun, `month_day` 1-31 or -1, `tz`, `window_start` HH:MM, `window_minutes`, `lead_minutes`) or `schedule_id`; `ends_on?`, `max_count?`, `overlap?` (`skip`/`create`), `paused?` | Creates a recurring task. Content given here is saved as a new template; timing reuses an identical schedule or makes one. |
| `update_recurring` | same | `id` plus any of the above | Changes a recurring task; omitted fields stay. `template_id`/`schedule_id` switch; content and timing fields edit the shared template/schedule in place. `ends_on`/`max_count` of 0 clear them. |
| `delete_recurring` | same | `id`, `also_template?`, `also_schedule?` | Stops a recurring task. Its tasks stay; its template and schedule too, unless asked (refused while another recurring task uses them). |
| `save_schedule` | `tasks:write`, `schedules` anywhere | `id?`, `workspace?`, `name?`, `freq`, `interval?`, `weekdays?`, `month_day?`, `tz?`, `window_start?`, `window_minutes?`, `lead_minutes?` | Makes a schedule, or changes schedule `id` (omitted fields stay); every recurring task on it follows. |
| `delete_schedule` | same | `id` | Deletes a schedule no recurring task uses. |

Defaults for timing: every 1 period, window from 00:00 for 1440 minutes, and the zone of your time zone preference (else the instance default, `GIGG_DEFAULT_TZ`).

### Runs and the runner

| Tool | Needs | Arguments | Does |
|---|---|---|---|
| `report_progress` | `runs:write`, `tasks:read` | `task`, `pct?` (0-100), `note?` | Finds the task's running run and posts a heartbeat/progress note to it; a tool error if no run is running. |
| `pause_runs` | admin key with `admin:workspaces`, or `runs:write` and `pause_runs` anywhere | `workspaces?` (ids or slugs), `minutes` (0 = until resumed) | Pauses new task runs in those workspaces, or in every workspace you may pause. |
| `resume_runs` | same | `workspaces?` | Lifts the pause. |

`model`, `effort`, `always_run` and `allowed_tools` are described in
[runner-protocol.md](runner-protocol.md#task-run-settings). Setting or changing `always_run` needs the
`always_run` permission, and `allowed_tools` the `allowed_tools` permission, in the task's workspace.
