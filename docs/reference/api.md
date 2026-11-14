# REST API

The API lives under `/api/v1` on every listener. The web UI uses the same API and nothing else.

The OpenAPI document is generated from the handlers and served without authentication at:

- `/api/v1/openapi.json` and `/api/v1/openapi.yaml` (OpenAPI 3.1)
- `/api/v1/openapi-3.0.json` and `/api/v1/openapi-3.0.yaml` (OpenAPI 3.0)

It is the authoritative reference for request and response bodies. This page covers authentication,
conventions and the list of endpoints.

## Authentication

A request is authenticated by an API key (`Authorization: Bearer ...`) or by a browser session cookie.
If an `Authorization` header is present it is used and any cookie is ignored. Requests without either
are anonymous: only login, the OpenAPI document and `/healthz` work for them; everything else answers
401 `unauthorized`.

### API keys

```
Authorization: Bearer gigg_<prefix>_<secret>
```

A token is `gigg_`, an 8-character lowercase prefix, `_`, and 43 base64url characters (256 random
bits). The prefix is stored in clear (for lookup and display); the token is stored only as a SHA-256
hash. Any other `Authorization` scheme, or a malformed, unknown, revoked or expired key, or a key used
where its restrictions don't allow it, gets the same 401 (the server logs the actual reason).

A key acts as one user and has these properties:

| Property | Meaning |
|---|---|
| `user_id` | The user the key acts as. A key never has more permissions than its user. |
| `workspace_id` | The workspace the key is bound to, or `null` (unbound). A bound key reaches nothing outside its workspace. |
| `project_ids` | Optional narrowing to some projects of its workspace (needs a workspace). Other projects' tasks, runs and activity are invisible. |
| `scopes` | What kinds of operations the key may perform (at least one; see below). |
| `allowed_listeners` | Listener names the key works on (`null` = any). |
| `allowed_cidrs` | Networks (CIDR) the key works from (`null` = any). The client address is resolved as described in [configuration.md](configuration.md#client-address). |
| `expires_at` | Unix ms. Default one year after creation; at most 3 years. |

Kinds of keys:

- **Workspace key** (bound to a workspace): reaches only that workspace. In it, it has its user's
  permissions, limited by its scopes. `current` can be used as the workspace reference
  (`/workspaces/current/...`).
- **Unbound key of a non-admin user**: reaches every workspace its user can see, at the user's
  permissions, limited by its scopes (like the user's session, without the browser-only operations).
- **Admin key** (unbound key of an instance admin): acts as an instance admin, limited by its scopes.
  Only admin keys may carry `admin:*` scopes. A workspace key of an admin user is not an admin key.

Keys are created with `gigg admin key-add`, `POST /me/keys` (keys acting as yourself, needs the
`keys:self` scope) or `POST /keys` (any user, admins with `admin:keys`). The token is returned once.
Creating a key with a key can't widen access: the new key's scopes must be a subset of the creating
key's, a workspace key can only create keys for its workspace, a narrowed key only keys narrowed to its
projects, listener and network restrictions are inherited and can't be loosened, and the expiry is
capped at the creating key's. With `keys:self`, a key may rotate or revoke only itself and the keys it
created; rotating another key requires that key to reach no further than the caller.

The key's last use (time, listener, address) is recorded and shown in key listings.

### Scopes

Sessions carry every scope. Keys carry only their own. An operation needs both the scope and the
permission in the workspace (see [access.md](access.md#api-keys-scopes-and-permissions)).

| Scope | Gates |
|---|---|
| `workspace:read` | Reading workspaces, members, projects, statuses, labels; activity and the change feed (with `tasks:read` and `comments:read`). |
| `workspace:write` | Defining custom fields. |
| `projects:write` | Workspace settings, projects, statuses and labels (manager operations). |
| `tasks:read` | Reading tasks, runs, relations, checklists, custom fields, templates, recurring tasks, schedules, runner state, stats, summary, inbox, notifications and saved filters. |
| `tasks:write` | Creating, changing, moving, cloning, archiving tasks; claiming; relations, checklists, custom values; templates, recurring tasks, schedules; resume requests. |
| `tasks:delete` | Moving tasks to the trash and restoring them. |
| `comments:read` | Reading comments (also needed for activity). |
| `comments:write` | Writing comments, run reports, resume comments. |
| `runs:write` | Recording runs (start, progress, finish, usage); pausing and resuming runs with the `pause_runs` permission. |
| `profile:write` | Your own profile, themes and picture. |
| `keys:self` | Managing keys that act as yourself. |
| `artifacts:write` | Accepted on keys. Note: not used by any endpoint yet. |
| `admin:workspaces` | Creating workspaces; archiving them and setting owners; runner caps; pausing any workspace. |
| `admin:members` | Seeing how members got their access (preset, source) in member lists. Changing members also needs a browser session. |
| `admin:users` | Users, agent pictures, groups and access rules (several of these also need a browser session, see below). |
| `admin:keys` | Every user's keys. |
| `admin:audit` | The instance-wide audit log. |
| `admin:transfer`, `admin:export`, `admin:settings` | Accepted on admin keys. Note: not used by any endpoint yet. |

Operations that work only from an admin's browser session, never with a key: setting access rules
(`PUT /users/{id}/access`, `PUT /groups/{id}/access`), creating, changing and deleting groups, adding and
removing workspace members, creating admins or setting passwords (`POST /users` with `is_admin` or
`password`, `PATCH /users/{id}` with `is_admin`, `POST /users/{id}/password`). Listing or ending your
own sessions and changing your own password also need a session.

### Browser sessions

`POST /api/v1/auth/login` with `{"username": "...", "password": "..."}` sets the session cookie and
returns the same body as `GET /auth/me`.

- Only on a listener that serves the UI (the main listener); elsewhere 403.
- Only people (`human` users with a password, not disabled). Agents can't log in.
- Cookie: `__Host-gigg_session` (with `Secure`) when `GIGG_PUBLIC_URL` is `https://`, else
  `gigg_session`. `HttpOnly`, `SameSite=Strict`, `Path=/`.
- Sessions last 30 days and slide forward with use. `POST /auth/logout` ends the current one. Changing
  a password (yours or by an admin) ends all of that user's sessions.
- Failed logins are recorded in the audit log (`auth.login_failed`). Login is rate-limited (below), and
  at most two password checks run at a time; a request that waits more than 3 s for one gets 429.

### CSRF rules for cookie requests

A cookie-authenticated request with a method other than GET, HEAD or OPTIONS must:

1. not carry `Sec-Fetch-Site` other than `same-origin`;
2. carry an `Origin` that is this server's: the public URL's origin, or an origin with exactly the host
   and port the request was sent to when that host is an allowed host (a server reached by both its IP and
   a `.local` name works under either). Without `GIGG_PUBLIC_URL`, an origin whose host is an allowed
   host;
3. have `Content-Type: application/json` or `application/merge-patch+json`, **or** carry an
   `X-Gigg-CSRF` header with any non-empty value (for uploads).

Otherwise it gets 403 with code `csrf`. Requests with a bearer key are not subject to these rules.

## Conventions

- JSON bodies; times are UTC Unix milliseconds; ids are integers.
- Workspace references in paths (`{ws}`) are an id, a slug, or `current` (workspace keys only).
- Task references (`{id}` under `/tasks/`) are an id or a key such as `WEB-12` (case-insensitive). A
  task moved to another project gets a new key; the old one keeps working as an alias. A key that
  matches tasks in more than one of your workspaces answers 409 `ambiguous_key`.
- Anything outside the caller's reach answers 404, never 403, so ids can't be probed. 403 means the
  object is visible but the caller lacks a permission or scope; the `detail` names which.
- `PATCH` bodies follow JSON Merge Patch: an absent field is unchanged, `null` clears it.
- Every API response has `Cache-Control: no-store` and an `X-Request-Id` header.

### Errors

Errors are RFC 9457 `application/problem+json` with a stable `code`:

```json
{
  "type": "about:blank",
  "title": "Unprocessable Entity",
  "status": 422,
  "code": "invalid",
  "detail": "effort is low, medium, high, xhigh, max or empty",
  "errors": [{"message": "...", "location": "body.effort"}]
}
```

`errors` lists validation details when there are any. Some errors also carry `request_id`; the
`X-Request-Id` response header is always present and matches the server log.

| Status | `code` | When |
|---|---|---|
| 400 | `bad_request` | Malformed request, bad `If-Match`, bad cursor, bad `Idempotency-Key`. |
| 401 | `unauthorized` | No or unusable credentials. |
| 401 | `bad_credentials` | Wrong username or password at login. |
| 403 | `forbidden` | Missing permission or scope, or an operation not allowed with this credential. |
| 403 | `bad_credentials` | Wrong current password when changing it. |
| 403 | `csrf` | Cookie request failing the CSRF rules. |
| 404 | `not_found` | No such object, or out of reach. |
| 405 | `method_not_allowed` | |
| 409 | `conflict` | State conflict (e.g. run already ended, cap reached, name taken). |
| 409 | `ambiguous_key` | Task key matches several of your workspaces. |
| 409 | `idempotency_in_progress` | A request with this `Idempotency-Key` is still running. |
| 412 | `version_conflict` | `If-Match` doesn't match the current version. |
| 413 | `too_large` | Request body too large. |
| 415 | `unsupported_media_type` | |
| 421 | `bad_host` | `Host` header not an allowed host. |
| 422 | `invalid` | Validation failed. |
| 422 | `idempotency_mismatch` | `Idempotency-Key` reused for a different request. |
| 423 | `paused` | Task runs are paused for the workspace (claim, run start). |
| 429 | `rate_limited` | Rate limit hit. |
| 429 | `too_many_streams` | Too many open event streams. |
| 500 | `internal` | Server error. |

### Versions, ETag and If-Match

Versioned objects (tasks, comments, workspaces, projects, statuses, labels, users, groups, templates,
schedules, recurring tasks, custom fields) have a `version` that increases with every change.
Responses that return a single versioned object (for example `GET /tasks/{id}`, task writes, a
successful claim, `GET /me`) carry `ETag: "<version>"`; the `version` field in the body is the same number.

Write endpoints that accept `If-Match` (see the OpenAPI document; e.g. `PATCH /tasks/{id}`,
`POST /tasks/{id}/move`, `PATCH /comments/{id}`, `PATCH /me`, `PATCH /workspaces/{ws}`,
`PUT /templates/{id}`) take `"N"`, `W/"N"` or `*`. Without the header (or with `*`) the write is
unconditional. If the version differs, the answer is 412 `version_conflict`: reload and retry.

### Idempotency

Any authenticated `POST` may carry `Idempotency-Key` (1-200 printable ASCII characters):

- A repeat with the same key by the same principal (the same key, or the same user's session) within
  24 hours gets the stored response (status, body, `Content-Type`, `ETag`, `Location`) with
  `Idempotent-Replayed: true`, and the request does not run again.
- The same key with a different request (method, path, query or body) answers 422
  `idempotency_mismatch`. A repeat while the first is still running answers 409
  `idempotency_in_progress`.
- Not stored: responses with status 500 or above, response bodies over 256 KiB, and anything beyond
  2000 stored responses per principal. Request bodies over 4 MiB are refused (413) when the header is
  set.
- Ignored on routes that carry credentials: paths under `/auth/`, `/keys`, `/me/keys`, `/users`,
  paths containing `/password` and paths ending in `/rotate`.

### Pagination

| Endpoint family | Parameters | Order |
|---|---|---|
| Task lists (`/workspaces/{ws}/tasks`, `/projects/{p}/tasks`) | `limit` (default 50, max 500), `cursor` (pass back `next_cursor`; absent when there is no more) | board order, or `sort` |
| Activity (`/workspaces/{ws}/activity`, `/audit`) | `limit` (default 100, max 500), `before_id` | newest first |
| Change feed (`/workspaces/{ws}/changes`) | `after_id` (an id or `latest`), `limit` (default 100, max 500); response `next_after_id`, `more` | oldest first |
| Comments (`/tasks/{id}/comments`) | `since_id`, `limit` (max 500) | oldest first |
| Runs (`/workspaces/{ws}/runs`) | `limit` (default 200, max 500), `status`, `started_since` | newest first |
| Notifications | `limit` (max 200), `unread` | newest first |

Note: the task `cursor` is an opaque string (currently an offset), so pages can shift if tasks change
between requests.

### Rate limits

Fixed one-minute windows, counted in memory per process:

| Who | Limit |
|---|---|
| Each API key | 300 requests per minute |
| Each browser session | 600 requests per minute |
| Anonymous requests, per client address | 120 per minute |
| Login, per address and username | 5 per minute |
| Login, per username | 20 per minute |
| Login, per address | 30 per minute |
| Open event streams, per user | 5 at a time |

Over the limit: 429 `rate_limited`. Note: an MCP tool call is one request to `/mcp` plus one or more
in-process API calls, each counted against the key.

### Live updates (Server-Sent Events)

`GET /api/v1/workspaces/{ws}/events` (not in the OpenAPI document) streams the workspace's activity
as `text/event-stream`, with the same visibility as the change feed (viewer, and the scopes
`workspace:read`, `tasks:read`, `comments:read`):

- each entry is `event: activity`, `id: <activity id>`, `data: <activity item JSON>`;
- it starts after `Last-Event-ID`, else after `?after_id=` (an id or `latest`), else from now;
- the stream begins with `retry: 3000`, sends a `: ping` comment every 25 s, and ends after 2 minutes;
  clients reconnect with `Last-Event-ID` (and are authenticated again);
- at most 5 open streams per user.

Instance admins see key, member and access events; others don't, and edited or deleted comment text is
removed from their entries.

## Endpoints

Paths are relative to `/api/v1`. "Admin" means an instance admin (with the matching `admin:*` scope
when using a key); "managers" means the `manage_workspace` permission. Other permission requirements are
in [access.md](access.md).

### auth

| Method | Path | Purpose |
|---|---|---|
| POST | `/auth/login` | Log in (sets the session cookie). |
| POST | `/auth/logout` | Log out. |
| GET | `/auth/me` | Who am I: user, workspaces with permissions, capabilities, key scopes and restrictions, listener. |
| POST | `/auth/password` | Change your password (session only; ends every session). |
| GET | `/auth/sessions` | Your browser sessions (session only). |
| DELETE | `/auth/sessions/{id}` | End one of your sessions (session only). |

### profile

| Method | Path | Purpose |
|---|---|---|
| GET | `/me` | Your profile. |
| PATCH | `/me` | Change your display name, description, colour, preferences. `preferences` is merged by top-level key: each key given replaces that preference, `null` removes it, the others stay (leaving a key out no longer removes it). |
| GET | `/me/keys` | Keys acting as you. |
| POST | `/me/keys` | Create a key acting as you (never wider than the credential used). |
| POST | `/me/keys/{id}/rotate` | Replace one of your keys' secret. |
| DELETE | `/me/keys/{id}` | Revoke one of your keys. |
| GET | `/me/themes` | Your custom themes and selection. |
| PUT | `/me/themes/{theme_id}` | Save a custom theme (at most 50; the oldest go). |
| DELETE | `/me/themes/{theme_id}` | Delete a custom theme. |
| PUT | `/me/themes-selection` | Choose your theme and the match-the-system switch. |
| PUT | `/me/avatar` | Upload your picture (PNG, JPEG, WebP or GIF, up to 2 MB; stored as a 256 px PNG). |
| DELETE | `/me/avatar` | Remove your picture. |

### me

| Method | Path | Purpose |
|---|---|---|
| GET | `/me/filters` | Your saved filters. |
| POST | `/me/filters` | Save a filter. |
| PUT | `/me/filters/{id}` | Replace a saved filter. |
| DELETE | `/me/filters/{id}` | Delete a saved filter. |
| GET | `/me/notifications` | Your notifications (mentions, assignments, questions, missed windows). |
| POST | `/me/notifications/read` | Mark notifications read (`ids`, or `all`). |

### users

| Method | Path | Purpose |
|---|---|---|
| GET | `/permissions` | Every permission, preset and role, and the rule order. |
| GET | `/users` | All users (admin), or the members of your workspaces. |
| GET | `/users/{id}` | A user's profile. |
| POST | `/users` | Create a person or agent (admin). |
| PATCH | `/users/{id}` | Change a user's profile, admin flag or disabled state (admin). |
| POST | `/users/{id}/password` | Set a person's password (admin, session only; ends their sessions). |
| GET | `/users/{id}/sessions` | A user's sessions (admin). |
| DELETE | `/sessions/{id}` | End any session (admin). |
| GET | `/users/{id}/access` | A user's rules and resolved permissions per workspace, with sources (admin). |
| PUT | `/users/{id}/access` | Set or remove one of a user's rules (admin, session only). |
| GET | `/groups` | Groups with members and rules (admin). |
| POST | `/groups` | Create a group (admin, session only). |
| PATCH | `/groups/{id}` | Rename a group and/or replace its members (admin, session only). |
| DELETE | `/groups/{id}` | Delete a group (admin, session only). |
| PUT | `/groups/{id}/access` | Set or remove one of a group's rules (admin, session only). |
| GET | `/users/{id}/avatar` | A user's picture (PNG). |
| PUT | `/users/{id}/avatar` | Set an agent's picture (admin). |
| DELETE | `/users/{id}/avatar` | Remove an agent's picture (admin). |

### keys

| Method | Path | Purpose |
|---|---|---|
| GET | `/keys` | Every key (admin). |
| POST | `/keys` | Create a key for any user (admin). |
| GET | `/keys/{id}` | One key (admin). |
| POST | `/keys/{id}/rotate` | Replace any key's secret (admin). |
| DELETE | `/keys/{id}` | Revoke any key (admin). |

### workspaces

| Method | Path | Purpose |
|---|---|---|
| GET | `/workspaces` | Workspaces in reach. |
| POST | `/workspaces` | Create a workspace (admin). |
| GET | `/workspaces/{ws}` | A workspace. |
| PATCH | `/workspaces/{ws}` | Change a workspace (managers: name, colour, description, status template; admins: archive, owner). |
| GET | `/workspaces/{ws}/members` | Members and roles. |
| PUT | `/workspaces/{ws}/members/{user}` | Add a member or change their preset (admin, session only). |
| DELETE | `/workspaces/{ws}/members/{user}` | Remove a member's own rule (admin, session only; also unassigns them if they lose access). |
| GET | `/workspaces/{ws}/activity` | Activity, newest first. |
| GET | `/workspaces/{ws}/changes` | Change feed: activity after an id, oldest first. |
| GET | `/workspaces/{ws}/events` | The same as a Server-Sent Events stream (see above; not in OpenAPI). |
| GET | `/workspaces/{ws}/summary` | Plain-text board summary: per project, each status with up to 5 tasks. |

### labels

| Method | Path | Purpose |
|---|---|---|
| GET | `/workspaces/{ws}/labels` | A workspace's labels. |
| POST | `/workspaces/{ws}/labels` | Create a label (managers). |
| PATCH | `/labels/{id}` | Change a label (managers). |
| DELETE | `/labels/{id}` | Delete a label and remove it from every task (managers). |

### projects

| Method | Path | Purpose |
|---|---|---|
| GET | `/workspaces/{ws}/projects` | Projects in reach. |
| POST | `/workspaces/{ws}/projects` | Create a project (managers). |
| GET | `/projects/{p}` | A project. |
| PATCH | `/projects/{p}` | Change a project (managers). |
| DELETE | `/projects/{p}` | Delete an empty project (managers). |
| GET | `/projects/{p}/board` | The board: statuses with their tasks, and the workspace's labels. |
| GET | `/projects/{p}/statuses` | Statuses in board order. |
| POST | `/projects/{p}/statuses` | Add a status (managers). |
| POST | `/projects/{p}/statuses/reorder` | Set the order of statuses (managers). |
| PATCH | `/statuses/{id}` | Change a status (managers). |
| DELETE | `/statuses/{id}` | Delete an empty status (managers). |

### tasks

| Method | Path | Purpose |
|---|---|---|
| GET | `/workspaces/{ws}/tasks` | Tasks across a workspace (filters combine). |
| GET | `/projects/{p}/tasks` | A project's tasks (filters combine). |
| POST | `/projects/{p}/tasks` | Create a task. |
| GET | `/tasks/{id}` | A task, by id or key. |
| PATCH | `/tasks/{id}` | Change a task's fields (`null` clears). |
| POST | `/tasks/{id}/move` | Move a task: status, position, or another project of the workspace. |
| POST | `/tasks/{id}/clone` | Copy a task (optionally into another project). |
| DELETE | `/tasks/{id}` | Move a task to the trash. |
| POST | `/tasks/{id}/restore` | Restore a task from the trash. |
| POST | `/tasks/{id}/archive` | Archive a task (off the board and lists; not trash). |
| POST | `/tasks/{id}/unarchive` | Bring an archived task back. |
| POST | `/projects/{p}/archive-done` | Archive done and cancelled tasks finished more than N days ago. |
| POST | `/tasks/{id}/resume` | Ask for the task's agent session to be resumed (records the request, moves the task to todo). |
| GET | `/tasks/{id}/activity` | A task's history (old values included). |
| GET | `/tasks/{id}/relations` | Tasks this one blocks, is blocked by, or relates to. |
| POST | `/tasks/{id}/relations` | Link two tasks (`blocks`, `blocked_by`, `relates`). |
| DELETE | `/tasks/{id}/relations` | Unlink two tasks (`?task=...&direction=...`). |
| GET | `/tasks/{id}/checklist` | A task's checklist. |
| POST | `/tasks/{id}/checklist` | Append checklist items. |
| PATCH | `/tasks/{id}/checklist/{item}` | Edit, tick/untick or move an item. |
| DELETE | `/tasks/{id}/checklist/{item}` | Remove an item. |
| GET | `/inbox` | Tasks waiting for a human (category `waiting`) across your workspaces, with the latest question. |

Task list filters: `project` (workspace list only), `status`, `category`, `label`, `priority_min`,
`assignee`, `created_by`, `q` (text in titles, descriptions and comments), `due_before`,
`updated_since`, `include_deleted`, `deleted`, `archived` (`only` or `include`), `parent`, `blocked`,
`sort` (`board`, or a comma list of `priority`, `due`, `created`, `updated`, `number`, `title`, each
optionally prefixed with `-` for descending), `limit`, `cursor`.


Every task carries `last_run` (absent when it never ran): its latest run in brief (`id`, `status`,
`worker_user_id`, `started_at`, `heartbeat_at`, `ended_at`, `model`, `progress_pct`, `progress_note`,
`error_reason`). In task lists and on the board (`GET /projects/{p}/board`), a task in a Waiting status
also carries `question` when the caller has `comments:read`: the task's latest `question` or `report`
comment, until someone other than its author comments after it (an answer clears it; the author's own
follow-ups don't): `comment_id`, `author_id`, `created_at`, `kind` (`question` or `report`), `text` (its
Markdown cut to 280 characters).

### comments

| Method | Path | Purpose |
|---|---|---|
| GET | `/tasks/{id}/comments` | A task's comments, oldest first, with author facts. |
| POST | `/tasks/{id}/comments` | Comment (Markdown; `kind` = `comment`, `report` or `question`). |
| PATCH | `/comments/{id}` | Edit your comment (the old text stays in the history). |
| DELETE | `/comments/{id}` | Delete a comment (yours, or any as a manager). |

### fields

| Method | Path | Purpose |
|---|---|---|
| GET | `/workspaces/{ws}/fields` | Custom task fields. |
| POST | `/workspaces/{ws}/fields` | Add a custom field. |
| PUT | `/fields/{id}` | Rename a field or change its options. |
| DELETE | `/fields/{id}` | Remove a field and its values. |
| PATCH | `/tasks/{id}/custom` | Set or clear (`null`) a task's custom values, keyed by field id. |

### templates

| Method | Path | Purpose |
|---|---|---|
| GET | `/workspaces/{ws}/templates` | Task templates (`?project=` for those usable in a project). |
| POST | `/workspaces/{ws}/templates` | Save a template. |
| GET | `/templates/{id}` | A template. |
| PUT | `/templates/{id}` | Replace a template. |
| DELETE | `/templates/{id}` | Delete a template. |
| POST | `/templates/{id}/use` | Create a task from a template. |

### recurrence

| Method | Path | Purpose |
|---|---|---|
| GET | `/workspaces/{ws}/recurrences` | Recurring tasks (each pairs a template with a schedule). |
| POST | `/projects/{p}/recurrences` | Create a recurring task. |
| GET | `/recurrences/{id}` | A recurring task with its next five occurrences. |
| PUT | `/recurrences/{id}` | Replace a recurring task; the next occurrence is recomputed from now. |
| DELETE | `/recurrences/{id}` | Stop a recurring task (its tasks, template and schedule stay). |
| GET | `/workspaces/{ws}/schedules` | Schedules (named timing). |
| POST | `/workspaces/{ws}/schedules` | Make a schedule. |
| GET | `/schedules/{id}` | A schedule. |
| PUT | `/schedules/{id}` | Replace a schedule (every recurring task on it follows). |
| DELETE | `/schedules/{id}` | Delete a schedule no recurring task uses. |

### runs

See [runner-protocol.md](runner-protocol.md) for how these fit together.

| Method | Path | Purpose |
|---|---|---|
| POST | `/workspaces/{ws}/tasks/claim` | Take the next matching task and move it, atomically (204 when nothing matches). |
| GET | `/tasks/{id}/runs` | A task's runs, newest first. |
| POST | `/tasks/{id}/runs` | Record that a worker started on the task. |
| GET | `/runs/{id}` | A run with its timeline. |
| POST | `/runs/{id}/progress` | Heartbeat and an optional progress note. |
| POST | `/runs/{id}/finish` | Close a run, store the report, move the task if asked. |
| POST | `/runs/{id}/usage` | Record a finished run's share of the account's usage meter (once). |
| GET | `/workspaces/{ws}/runs` | Runs in a workspace, newest first. |
| GET | `/workspaces/{ws}/stats` | Runs, cost and tasks since a time. |
| GET | `/runner` | Runner caps, pause state and the last 24 hours, per workspace in reach. |
| GET | `/workspaces/{ws}/runner` | The same for one workspace. |
| PATCH | `/workspaces/{ws}/runner` | Change a workspace's task caps (admin). |
| POST | `/runner/pause` | Pause new task runs in some or all workspaces (admin, or `pause_runs`). |
| POST | `/runner/resume` | Lift the pause (admin, or `pause_runs`). |

### admin

| Method | Path | Purpose |
|---|---|---|
| GET | `/audit` | The instance-wide audit log (admin). |

### Outside `/api/v1`

| Method | Path | Purpose |
|---|---|---|
| GET | `/healthz` | Health and schema version (no authentication). |
| POST | `/mcp` | MCP endpoint, see [mcp.md](mcp.md). |
