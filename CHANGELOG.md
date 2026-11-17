# Changelog

## Unreleased

- **Gigs and errands**: tasks are now **gigs** and crons are **errands**, everywhere: the UI, the
  API (`/gigs`, `/errands`, `gigs:read` ...), MCP tools (`create_gig`, `list_errands` ...),
  permissions (`create_gigs` ...), the database and the docs. Migration 0013 renames the tables and
  the names stored in data (key scopes, permission overrides, activity, allowed tool lists). Old
  `/board/p/<id>/t/<key>` and `/crons` links redirect. Clients using the old API paths or MCP tool
  names need updating.
- MCP `create_gig` and `update_gig` take `assignees` (usernames or user ids); `update_gig`
  clears them with `clear: ["assignees"]`.
- **Errands**: shell commands an agent runs on a schedule (every N minutes or hours, optionally on
  some weekdays between two times, or daily, weekly, monthly), in its sandbox, with a run history,
  each run's output and no gigs. An **Errands** page lists them, failing first, with a panel for
  runs and output and an editor. The permissions `errands` and `errand_unsandboxed`, the key scopes
  `errands:read`, `errands:output`, `errands:write` and `errands:run`, API endpoints and MCP tools. The
  executor that runs them is outside gigg (the server still never runs commands).
- **Pause runs** has two boxes, gig runs and errand runs; the Dashboard shows each workspace's errands
  and how many are failing.
- Schedules take `minutes` and `hours` timings (for errands only).
- `gigg admin allow <user> <workspace|all> <permission>...` adds permissions to a user's rule and keeps
  the rest (`access` replaces the rule).

- Dialogs opened from a table row (archive, make admin, revoke…) inherited the cell's right
  alignment and no-wrap, so titles sat on the right and text ran past the edge. Dialogs now render
  at the top of the page.
- Audit log: the API key badge shows the key's name (its id in the tooltip) and lines up with the
  actor.

## 0.1.0 (2026-11-16)

The first public release.

- **A board** for tasks across workspaces and projects, with filters, manual order, tables with bulk
  actions, saved views, live updates and a command palette.
- **Tasks**: Markdown with @mentions, checklists, subtasks, dependencies, custom fields, labels,
  dates and time windows, archive and trash.
- **Recurring tasks** built from reusable templates and schedules.
- **Agents** as ordinary users. They get runs with status, cost and reports, resumable sessions,
  questions in an inbox, and per-task model, effort, *always run* and tool allowlists. Each
  workspace has daily caps and a pause switch.
- **The same features three ways:** the web UI, a REST API with an OpenAPI document, and MCP.
- **Access control**: workspaces, roles, per-permission overrides for users and groups, and API
  keys limited by workspace, project, scope, listener, network and expiry.
- **One binary** with SQLite: HTTPS, an optional second listener for agents, nightly backups and
  snapshots before migrations.
- **Themes**, built-in help and an audit log.
