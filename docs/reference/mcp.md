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

An agent's MCP key usually follows its user (no places of its own): it reaches whatever the agent is
given, and changing the agent's access needs no new key. Tools that take a `workspace` argument then
default to the only workspace in reach, or to the workspace the user owns; `list_gigs`, `summary` and
`list_projects` with no `workspace` cover every workspace in reach, as
`{"workspaces": [{"workspace", "slug", "result"}]}`. With a key limited to some places, the default is
its first place's workspace.

## How tools work

Every tool is a call (or a few calls) to the REST API, made in-process with the caller's own key, so
MCP can never do more than the key can do through the API. Results are the API's JSON response as
text. API errors come back as a tool error whose text is `Error <status> <code>: <detail>`, followed by
validation details. A 204 answer is reported as `Nothing (204).`

The tool list depends on the key: a tool is listed only when the key has the scopes it needs, and some
also need a permission somewhere ("anywhere" below means in at least one workspace in reach). The
API still checks the permission in the gig's own workspace on every call.

Times are Unix milliseconds. Gigs are referenced by id or key (`WEB-12`).

There are no MCP tools for claiming gigs or recording runs; runners use the REST API for that (see
[runner-protocol.md](runner-protocol.md)). An agent inside a run can use `report_progress`.

## Tools

### Reading

| Tool | Needs | Arguments | Does |
|---|---|---|---|
| `whoami` | `workspace:read` | none | `GET /auth/me`: identity, workspaces with permissions, key scopes. |
| `list_projects` | `workspace:read` | `workspace?` | Projects of a workspace (or of every workspace in reach), with ids, gig key prefixes and each project's description: what it is about, context for its gigs. |
| `summary` | `gigs:read` | `workspace?` | Plain-text board summary: per project, each status with up to 5 gigs (every workspace in reach without `workspace`). |
| `list_gigs` | `gigs:read` | `workspace?`, `project?`, `category?`, `label?`, `q?`, `priority_min?` (0-4), `sort?`, `limit?` (default 50, max 500), `cursor?`, `archived?` (`only`/`include`), `parent?`, `blocked?`, `recurring?` (`hide`/`only`) | Finds gigs; filters combine; board order unless `sort` is given. |
| `get_gig` | `gigs:read` | `gig`, `comments?` | One gig; with `comments: true` (and `comments:read`) returns `{"gig": ..., "comments": ...}`. |
| `list_runs` | `gigs:read` | `gig` | The gig's runs (sessions, status, cost), newest first. |
| `list_comments` | `comments:read` | `gig`, `since_id?` | Comments with author facts, oldest first. |
| `list_relations` | `gigs:read` | `gig` | What blocks the gig, what it blocks, related gigs. |
| `get_checklist` | `gigs:read` | `gig` | Checklist items (ids, text, done). |
| `list_fields` | `gigs:read` | `workspace?` | Custom fields (ids, kinds, options). |
| `list_templates` | `gigs:read` | `workspace?`, `project?` | Templates you can create gigs from. |
| `list_recurring` | `gigs:read` | `workspace?`, `project?` | Recurring gigs with upcoming occurrences. |
| `list_schedules` | `gigs:read` | `workspace?` | Schedules and how many recurring gigs use each. |
| `changes` | `gigs:read` | `workspace?`, `after_id?` (id or `latest`), `limit?` (default 100, max 500) | Activity after an id, oldest first; pass `next_after_id` back as `after_id`. The API also requires `workspace:read` and `comments:read`. |
| `notifications` | `gigs:read` | `unread?`, `limit?` | Your notifications: mentions, assignments, questions, missed windows. |
| `runner_status` | `gigs:read` | none | Per workspace: gig caps, pause state, last 24 hours of agent work. |

### Writing gigs

| Tool | Needs | Arguments | Does |
|---|---|---|---|
| `create_gig` | `gigs:write` | `project`, `title`, `description_md?`, `category?`, `priority?` (0 none .. 4 urgent), `labels?` (created if missing), `color?`, `start_at?`, `due_at?`, `not_after?`, `parent_id?`, `blocks_parent?` (with `parent_id`: the parent waits for this subgig), `checklist?`, `custom?`, `model?`, `effort?`, `always_run?`, `allowed_tools?` | `POST /projects/{project}/gigs`. |
| `update_gig` | `gigs:write`, `edit_gigs` anywhere | `gig`, `title?`, `description_md?`, `priority?`, `labels?` (replaces), `color?`, `start_at?`, `due_at?`, `not_after?`, `model?`, `effort?`, `always_run?`, `allowed_tools?` (`[]` clears), `blocks_parent?` (a subgig), `clear?` (any of `color`, `start_at`, `due_at`, `not_after`, `labels`) | `PATCH /gigs/{gig}`. An empty `model`/`effort` resets to the agent's default. |
| `move_gig` | `gigs:work` | `gig`, `category?` or `status?`, `project?`, `position?` (`top`, `bottom`, `before`, `after`), `relative_to?` | `POST /gigs/{gig}/move`. Another `project` also needs `gigs:write` and `edit_gigs`. |
| `clone_gig` | `gigs:write` | `gig`, `project?`, `title?` | Copies a gig: its content, labels, assignees, dates, custom field values and checklist (unticked). |
| `resume_gig` | `gigs:work` | `gig`, `comment_md?` | Asks for the gig's agent session to be resumed (records the request, moves the gig to todo; runs nothing). |
| `set_custom_fields` | `gigs:write` | `gig`, `values` (field id -> value, `null` clears) | `PATCH /gigs/{gig}/custom`. |
| `link_gigs` | `gigs:write`, `edit_gigs` anywhere | `gig`, `other`, `direction` (`blocks`, `blocked_by`, `relates`) | Adds a dependency. A blocked gig isn't claimed until its blockers are done. |
| `unlink_gigs` | `gigs:write`, `edit_gigs` anywhere | `gig`, `other`, `direction` | Removes a dependency. |
| `update_checklist` | `gigs:work` and `work_gigs` anywhere (ticking), or `gigs:write` and `edit_gigs` anywhere (adding, removing) | `gig`, `add?`, `check?`, `uncheck?`, `remove?` (item ids) | Applies the changes in that order and returns the checklist. Stops at the first failing step; each step needs its own permission. |
| `archive_gig` | `gigs:write`, `archive_gigs` anywhere | `gig`, `restore?` | Archives a gig, or unarchives it with `restore: true`. |
| `create_from_template` | `gigs:write`, `create_gigs` anywhere | `template`, `project`, `title?` | Creates a gig from a template. |
| `save_template` | `gigs:write`, `templates` anywhere | `workspace?`, `id?` (replace), `project_id?`, `name`, `title`, `description_md?`, `priority?`, `labels?`, `assignees?`, `checklist?`, `subgigs?` (`{title, blocks_parent?}` each), `custom?`, `model?`, `effort?`, `always_run?`, `allowed_tools?` | Creates a template, or replaces template `id`. Gigs made from it get its subgigs. |
| `delete_gig` | `gigs:delete` | `gig` | Moves a gig to the trash. |
| `restore_gig` | `gigs:delete` | `gig` | Restores a gig from the trash. |
| `add_comment` | `comments:write` | `gig`, `body_md`, `kind?` (`comment`, `report`, `question`) | Comments on a gig. |
| `mark_notifications_read` | `gigs:read` | `ids?` | Marks those notifications read, or all of them. |

### Recurring gigs and schedules

| Tool | Needs | Arguments | Does |
|---|---|---|---|
| `create_recurring` | `gigs:write`, `recurring_gigs` anywhere | `project`, content (`title`, `description_md`, `priority`, `labels`, `assignees`, `checklist`, `subgigs`, `model`, `effort`, `always_run`, `allowed_tools`) or `template_id`; timing (`freq` `daily`/`weekly`/`monthly`, `interval`, `weekdays` 1=Mon..7=Sun, `month_day` 1-31 or -1, `tz`, `window_start` HH:MM, `window_minutes`, `lead_minutes`) or `schedule_id`; `ends_on?`, `max_count?`, `overlap?` (`skip`/`create`), `paused?` | Creates a recurring gig. Content given here is saved as a new template; timing reuses an identical schedule or makes one. |
| `update_recurring` | same | `id` plus any of the above | Changes a recurring gig; omitted fields stay. `template_id`/`schedule_id` switch; content and timing fields edit the shared template/schedule in place. `ends_on`/`max_count` of 0 clear them. |
| `delete_recurring` | same | `id`, `also_template?`, `also_schedule?` | Stops a recurring gig. Its gigs stay; its template and schedule too, unless asked (refused while another recurring gig uses them). |
| `save_schedule` | `gigs:write`, `recurring_gigs` anywhere | `id?`, `workspace?`, `name?`, `freq`, `interval?`, `weekdays?`, `month_day?`, `tz?`, `window_start?`, `window_minutes?`, `lead_minutes?` | Makes a schedule, or changes schedule `id` (omitted fields stay); every recurring gig on it follows. |
| `delete_schedule` | same | `id` | Deletes a schedule no recurring gig uses. |

Defaults for timing: every 1 period, window from 00:00 for 1440 minutes, and the zone of your time zone preference (else the instance default, `GIGG_DEFAULT_TZ`).

### Runs and the runner

| Tool | Needs | Arguments | Does |
|---|---|---|---|
| `report_progress` | `runs:write`, `gigs:read` | `gig`, `pct?` (0-100), `note?` | Finds the gig's running run and posts a heartbeat/progress note to it; a tool error if no run is running. |
| `pause_runs` | admin key with `admin:workspaces`, or `runs:write` and `pause_runs` anywhere | `workspaces?` (ids or slugs), `minutes` (0 = until resumed), `gigs?`, `errands?` (default true) | Pauses new gig runs and errand runs in those workspaces, or in every workspace you may pause. Errands stay paused until resumed. |
| `resume_runs` | same | `workspaces?`, `gigs?`, `errands?` | Lifts the pause. |

### Errands

| Tool | Needs | Arguments | Does |
|---|---|---|---|
| `list_errands` | `errands:read` | `workspace?` | Errands, failing first: state, schedule in words, worker, last run, next slot. |
| `get_errand` | `errands:read` | `id` | One errand. |
| `list_errand_runs` | `errands:read` | `id`, `before?`, `limit?` | Its runs, newest first. |
| `get_errand_run` | `errands:read` | `id` | One run, with its output tail when the key has `errands:output`. |
| `list_errand_templates` | `errands:read` | `workspace?` | Errand templates. |
| `save_errand` | `errands:write`, `errands` anywhere | `id?` (change), `workspace?`, `template_id?` or `name`, `command`, `description_md?`, `timeout_s?`, `output?` (`gigg`/`host`), `unsandboxed?`, `project?` (the project it belongs to: who sees it sees the errand; its failure gigs land there; default the workspace's first); `schedule_id?` or `freq` (`minutes`/`hours`/`daily`/`weekly`/`monthly`), `interval?`, `weekdays?`, `month_day?`, `tz?`, `at?` (HH:MM), `for_minutes?` (minutes/hours: how long ticking lasts each day); `worker?`, `on_failure_gig?`, `paused?` | Makes an errand, or changes one (omitted fields stay). |
| `pause_errand` | same | `id`, `resume?` | Pauses or resumes an errand. |
| `run_errand_now` | same | `id` | Runs it once at the executor's next poll. |
| `delete_errand` | same | `id` | Deletes an errand. |

`model`, `effort`, `always_run` and `allowed_tools` are described in
[runner-protocol.md](runner-protocol.md#gig-run-settings). Setting or changing `always_run` needs the
`always_run` permission, and `allowed_tools` the `allowed_tools` permission, in the gig's workspace.
