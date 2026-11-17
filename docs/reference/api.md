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
| `project_ids` | Optional narrowing to some projects of its workspace (needs a workspace). Other projects' gigs, runs and activity are invisible. |
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
| `workspace:read` | Reading workspaces, members, projects, statuses, labels; activity and the change feed (with `gigs:read` and `comments:read`). |
| `workspace:write` | Defining custom fields. |
| `projects:write` | Workspace settings, projects, statuses and labels (manager operations). |
| `gigs:read` | Reading gigs, runs, relations, checklists, custom fields, templates, recurring gigs, schedules, runner state, stats, summary, inbox, notifications and saved filters. |
| `gigs:work` | A consumer's actions: moving gigs between statuses of their project (and reordering), ticking checklist items, claiming, resume requests, a finish that moves the gig. |
| `gigs:write` | A producer's: creating, changing, cloning, archiving gigs; moving them to another project; relations, adding and removing checklist items, custom values; templates, recurring gigs, schedules. |
| `gigs:delete` | Moving gigs to the trash and restoring them. |
| `comments:read` | Reading comments (also needed for activity). |
| `comments:write` | Writing comments, run reports, resume comments. |
| `runs:write` | Recording runs (start, progress, finish, usage); pausing and resuming runs with the `pause_runs` permission. |
| `errands:read` | Reading errands, errand templates and errand runs (without output). |
| `errands:output` | Reading an errand run's output tail (with `errands:read`). |
| `errands:write` | Making, changing, pausing, running now and deleting errands and errand templates (with the `errands` permission). |
| `errands:run` | An agent's errand executor: claiming its own due runs and reporting them. |
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
- Gig references (`{id}` under `/gigs/`) are an id or a key such as `WEB-12` (case-insensitive). A
  gig moved to another project gets a new key; the old one keeps working as an alias. A key that
  matches gigs in more than one of your workspaces answers 409 `ambiguous_key`.
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
| 409 | `ambiguous_key` | Gig key matches several of your workspaces. |
| 409 | `idempotency_in_progress` | A request with this `Idempotency-Key` is still running. |
| 409 | `idempotency_unreplayable` | The request with this `Idempotency-Key` already ran, but its answer was too large to keep. |
| 412 | `version_conflict` | `If-Match` doesn't match the current version. |
| 413 | `too_large` | Request body too large. |
| 415 | `unsupported_media_type` | |
| 421 | `bad_host` | `Host` header not an allowed host. |
| 422 | `invalid` | Validation failed. |
| 422 | `idempotency_mismatch` | `Idempotency-Key` reused for a different request. |
| 423 | `paused` | Gig runs are paused for the workspace (claim, run start). |
| 429 | `rate_limited` | Rate limit hit. |
| 429 | `too_many_streams` | Too many open event streams. |
| 500 | `internal` | Server error. |
| 503 | `unavailable` | The database failed while checking the credential (not a logout). Comes with `Retry-After`; try again. |

### Versions, ETag and If-Match

Versioned objects (gigs, comments, workspaces, projects, statuses, labels, users, groups, templates,
schedules, recurring gigs, custom fields) have a `version` that increases with every change.
Responses that return a single versioned object (for example `GET /gigs/{id}`, gig writes, a
successful claim, `GET /me`) carry `ETag: "<version>"`; the `version` field in the body is the same number.

Write endpoints that accept `If-Match` (see the OpenAPI document; e.g. `PATCH /gigs/{id}`,
`POST /gigs/{id}/move`, `PATCH /comments/{id}`, `PATCH /me`, `PATCH /workspaces/{ws}`,
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
- Not stored: responses with status 500 or above (the request didn't complete, so a repeat runs it
  again) and anything beyond 2000 stored responses per principal. A response body over 256 KiB isn't
  stored either, but the key is: a repeat answers 409 `idempotency_unreplayable` instead of running
  the request a second time. Request bodies over 4 MiB are refused (413) when the header is set.
- Ignored on routes that carry credentials: paths under `/auth/`, `/keys`, `/me/keys`, `/users`,
  paths containing `/password` and paths ending in `/rotate`.

### Pagination

| Endpoint family | Parameters | Order |
|---|---|---|
| Gig lists (`/workspaces/{ws}/gigs`, `/projects/{p}/gigs`) | `limit` (default 50, max 500), `cursor` (pass back `next_cursor`; absent when there is no more) | board order, or `sort` |
| Activity (`/workspaces/{ws}/activity`, `/audit`) | `limit` (default 100, max 500), `before_id` | newest first |
| Change feed (`/workspaces/{ws}/changes`) | `after_id` (an id or `latest`), `limit` (default 100, max 500); response `next_after_id`, `more` | oldest first |
| Comments (`/gigs/{id}/comments`) | `since_id`, `limit` (max 500) | oldest first |
| Runs (`/workspaces/{ws}/runs`) | `limit` (default 200, max 500), `status`, `started_since` | newest first |
| Notifications | `limit` (max 200), `unread` | newest first |

Note: the gig `cursor` is an opaque string (currently an offset), so pages can shift if gigs change
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
`workspace:read`, `gigs:read`, `comments:read`):

- each entry is `event: activity`, `id: <activity id>`, `data: <activity item JSON>`; entries the
  caller made carry `request_id` (the `X-Request-Id` of the request behind them), so a client can
  recognise the echo of its own writes; other people's entries never do (the change feed works the same);
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
| GET | `/workspaces/{ws}/summary` | Plain-text board summary: per project, each status with up to 5 gigs. |

### labels

| Method | Path | Purpose |
|---|---|---|
| GET | `/workspaces/{ws}/labels` | A workspace's labels. |
| POST | `/workspaces/{ws}/labels` | Create a label (managers). |
| PATCH | `/labels/{id}` | Change a label (managers). |
| DELETE | `/labels/{id}` | Delete a label and remove it from every gig (managers). |

### projects

| Method | Path | Purpose |
|---|---|---|
| GET | `/workspaces/{ws}/projects` | Projects in reach. |
| POST | `/workspaces/{ws}/projects` | Create a project (managers). |
| GET | `/projects/{p}` | A project. |
| PATCH | `/projects/{p}` | Change a project (managers). |
| DELETE | `/projects/{p}` | Delete an empty project (managers). It can't be restored; its slug and key are free for a new project (the key stays taken while moved gigs still answer to old keys with it). |
| GET | `/projects/{p}/board` | The board: statuses with their gigs, and the workspace's labels. Up to 5000 unarchived gigs; past that, `truncated: true` (the rest are in gig lists). |
| GET | `/projects/{p}/statuses` | Statuses in board order. |
| POST | `/projects/{p}/statuses` | Add a status (managers). |
| POST | `/projects/{p}/statuses/reorder` | Set the order of statuses (managers). |
| PATCH | `/statuses/{id}` | Change a status (managers). |
| DELETE | `/statuses/{id}` | Delete an empty status (managers). |

### gigs

| Method | Path | Purpose |
|---|---|---|
| GET | `/workspaces/{ws}/gigs` | Gigs across a workspace (filters combine). |
| GET | `/projects/{p}/gigs` | A project's gigs (filters combine). |
| POST | `/projects/{p}/gigs` | Create a gig; `subgigs` (`{title, blocks_parent}`) and `relations` (`{gig, direction}`) are created with it, in one transaction. |
| GET | `/gigs/{id}` | A gig, by id or key. |
| PATCH | `/gigs/{id}` | Change a gig's fields (`null` clears). |
| POST | `/gigs/{id}/move` | Move a gig: status, position, or another project of the workspace. |
| POST | `/gigs/{id}/clone` | Copy a gig (optionally into another project): content, labels, assignees, dates, custom field values, and the checklist unticked (with the Checklists permission). |
| DELETE | `/gigs/{id}` | Move a gig to the trash. |
| POST | `/gigs/{id}/restore` | Restore a gig from the trash. |
| POST | `/gigs/{id}/archive` | Archive a gig (off the board and lists; not trash). |
| POST | `/gigs/{id}/unarchive` | Bring an archived gig back. |
| POST | `/projects/{p}/archive-done` | Archive done and cancelled gigs finished more than N days ago (`older_than_days`); `category` (`done` or `cancelled`) limits it to one. |
| POST | `/gigs/{id}/resume` | Ask for the gig's agent session to be resumed (records the request, moves the gig to todo). |
| GET | `/gigs/{id}/activity` | A gig's history (old values included). |
| GET | `/gigs/{id}/relations` | Gigs this one blocks, is blocked by, or relates to. |
| POST | `/gigs/{id}/relations` | Link two gigs (`blocks`, `blocked_by`, `relates`). |
| DELETE | `/gigs/{id}/relations` | Unlink two gigs (`?gig=...&direction=...`). |
| GET | `/gigs/{id}/checklist` | A gig's checklist. |
| POST | `/gigs/{id}/checklist` | Append checklist items. |
| PATCH | `/gigs/{id}/checklist/{item}` | Edit, tick/untick or move an item. |
| DELETE | `/gigs/{id}/checklist/{item}` | Remove an item. |
| GET | `/inbox` | Gigs waiting for a human (category `waiting`) across your workspaces, with the latest question. |

Gig list filters: `project` (workspace list only), `status`, `category`, `label`, `priority_min`,
`assignee`, `created_by`, `q` (text in titles, descriptions and comments), `due_before`,
`updated_since`, `include_deleted`, `deleted`, `archived` (`only` or `include`), `parent`, `blocked`,
`sort` (`board`, or a comma list of `priority`, `due`, `created`, `updated`, `number`, `title`, each
optionally prefixed with `-` for descending), `limit`, `cursor`.


Every gig carries `last_run` (absent when it never ran): its latest run in brief (`id`, `status`,
`worker_user_id`, `started_at`, `heartbeat_at`, `ended_at`, `model`, `progress_pct`, `progress_note`,
`error_reason`). In gig lists and on the board (`GET /projects/{p}/board`), a gig in a Waiting status
also carries `question` when the caller has `comments:read`: the gig's latest `question` or `report`
comment, until someone other than its author comments after it (an answer clears it; the author's own
follow-ups don't): `comment_id`, `author_id`, `created_at`, `kind` (`question` or `report`), `text` (its
Markdown cut to 280 characters).

### comments

| Method | Path | Purpose |
|---|---|---|
| GET | `/gigs/{id}/comments` | A gig's comments, oldest first, with author facts. |
| POST | `/gigs/{id}/comments` | Comment (Markdown; `kind` = `comment`, `report` or `question`). |
| PATCH | `/comments/{id}` | Edit your comment (the old text stays in the history). |
| DELETE | `/comments/{id}` | Delete a comment (yours, or any as a manager). |

### fields

| Method | Path | Purpose |
|---|---|---|
| GET | `/workspaces/{ws}/fields` | Custom gig fields. |
| POST | `/workspaces/{ws}/fields` | Add a custom field. |
| PUT | `/fields/{id}` | Rename a field or change its options. |
| DELETE | `/fields/{id}` | Remove a field and its values. |
| PATCH | `/gigs/{id}/custom` | Set or clear (`null`) a gig's custom values, keyed by field id. |

### templates

| Method | Path | Purpose |
|---|---|---|
| GET | `/workspaces/{ws}/templates` | Gig templates (`?project=` for those usable in a project). |
| POST | `/workspaces/{ws}/templates` | Save a template. |
| GET | `/templates/{id}` | A template. |
| PUT | `/templates/{id}` | Replace a template. |
| DELETE | `/templates/{id}` | Delete a template. |
| POST | `/templates/{id}/use` | Create a gig from a template. |

### recurrence

| Method | Path | Purpose |
|---|---|---|
| GET | `/workspaces/{ws}/recurrences` | Recurring gigs (each pairs a template with a schedule). |
| POST | `/projects/{p}/recurrences` | Create a recurring gig. |
| GET | `/recurrences/{id}` | A recurring gig with its next five occurrences. |
| PUT | `/recurrences/{id}` | Replace a recurring gig; the next occurrence is recomputed from now (an occurrence already made is never made again). The editor becomes its owner, so this needs `create_gigs` too; new timing changes the shared schedule and needs what `PUT /schedules/{id}` needs. |
| DELETE | `/recurrences/{id}` | Stop a recurring gig. Its gigs stay; `?template=true` / `?schedule=true` also delete its template / schedule (409 and nothing deleted while another recurring gig uses it). |
| GET | `/workspaces/{ws}/schedules` | Schedules (named timing; `minutes`/`hours` ones are for errands only). |
| POST | `/workspaces/{ws}/schedules` | Make a schedule. |
| GET | `/schedules/{id}` | A schedule. |
| PUT | `/schedules/{id}` | Replace a schedule (every recurring gig and errand on it follows; with errands on it, this also needs `errands`). |
| DELETE | `/schedules/{id}` | Delete a schedule no recurring gig or errand uses. |

### errands

Commands an agent's executor runs on a schedule, sandboxed, with a run history and no gigs (see
[the guide](../guide/errands.md)). Reading needs view and `errands:read`; changing needs the `errands`
permission and `errands:write`; `unsandboxed: true` also needs `errand_unsandboxed`. A change by someone
without `errand_unsandboxed` turns the flag off (on a template other errands share: on a sandboxed copy for
that errand); they can't resume or run-now a no-sandbox errand, retime its schedule, or `PUT` a no-sandbox
template directly (403: edit an errand that uses it instead).
`command_set_by` changes only with the command text; `unsandboxed_set_by` is who turned the sandbox
off. Sending a `host` errand's output to gigg needs the command's author or `manage_workspace`; changing
an errand's worker needs `manage_workspace`; its failure gig, the worker or `manage_workspace`.

| Method | Path | Purpose |
|---|---|---|
| GET | `/workspaces/{ws}/errands` | Errands, failing first, each with its schedule in words, next slot and last run. |
| POST | `/workspaces/{ws}/errands` | Make an errand: `template_id` or content (`name`, `command`, `timeout_s` 10-43200 (default 300; another value needs `run_timeout`), `output` `gigg`/`host`, `unsandboxed`), `schedule_id` or `rule` (`freq` `minutes`/`hours`/`daily`/`weekly`/`monthly`, `interval`, `weekdays`, `month_day`, `tz`, `window_start`, `window_minutes`), `worker` (default you; someone else needs `manage_workspace`), `on_failure_gig`, `paused`. |
| GET | `/errands/{id}` | An errand. |
| PATCH | `/errands/{id}` | Change an errand (`If-Match`); content fields change its template, shared by the template's other errands. |
| DELETE | `/errands/{id}` | Delete an errand and its runs (its template and schedule stay). |
| POST | `/errands/{id}/pause`, `/errands/{id}/resume` | Pause; resume from the next slot (no catch-up). |
| POST | `/errands/{id}/run-now` | Run once at the executor's next poll. |
| GET | `/errands/{id}/runs?before=&limit=` | Runs, newest first. |
| GET | `/errand-runs/{id}` | One run; `output_tail` only with `errands:output`. |
| GET, POST | `/workspaces/{ws}/errand-templates` | Errand templates. |
| GET, PUT, DELETE | `/errand-templates/{id}` | An errand template (delete: none uses it). |
| POST | `/workspaces/{ws}/errands/claim` | The executor (view and `errands:run`, own errands only; no Errands permission needed): start due runs, at most `limit` (optional body; its free slots), the rest stay due. Returns `items` (run id, command, timeout, output, unsandboxed, `command_set_by`, `unsandboxed_set_by`) and `paused`. Older missed slots are recorded as `missed`; a slot while the previous run is going as `skipped`. |
| POST | `/errand-runs/{id}/finish` | The executor reports a run: `status` `ok`/`failed`/`timeout`, `exit_code`, `duration_ms`, `output_tail` (last 64 KiB, cut on a character boundary if longer; dropped for `host` output), `output_bytes`, `error`. Returns whether the errand became failing or recovered. |

### runs

See [runner-protocol.md](runner-protocol.md) for how these fit together.

| Method | Path | Purpose |
|---|---|---|
| POST | `/workspaces/{ws}/gigs/claim` | Take the next matching gig and move it, atomically (204 when nothing matches). |
| GET | `/gigs/{id}/runs` | A gig's runs, newest first. |
| POST | `/gigs/{id}/runs` | Record that a worker started on the gig. |
| GET | `/runs/{id}` | A run with its timeline. |
| POST | `/runs/{id}/progress` | Heartbeat and an optional progress note. |
| POST | `/runs/{id}/finish` | Close a run, store the report, move the gig if asked. |
| POST | `/runs/{id}/usage` | Record a finished run's share of the account's usage meter (once). |
| GET | `/workspaces/{ws}/runs` | Runs in a workspace, newest first. |
| GET | `/workspaces/{ws}/stats` | Runs, cost and gigs since a time. |
| GET | `/runner` | Runner caps, pause state and the last 24 hours, per workspace in reach. |
| GET | `/workspaces/{ws}/runner` | The same for one workspace. |
| PATCH | `/workspaces/{ws}/runner` | Change a workspace's gig caps (admin). |
| POST | `/runner/pause` | Pause new gig runs and errand runs in some or all workspaces (admin, or `pause_runs`); `gigs`/`errands` (default true) pick the boxes. Errands stay paused until resumed. |
| POST | `/runner/resume` | Lift the pause (admin, or `pause_runs`); `gigs`/`errands` as above. |

### admin

| Method | Path | Purpose |
|---|---|---|
| GET | `/audit` | The instance-wide audit log (admin). |

### Outside `/api/v1`

| Method | Path | Purpose |
|---|---|---|
| GET | `/healthz` | Health and schema version (no authentication). |
| POST | `/mcp` | MCP endpoint, see [mcp.md](mcp.md). |
