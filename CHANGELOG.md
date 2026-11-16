# Changelog

## Unreleased

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
