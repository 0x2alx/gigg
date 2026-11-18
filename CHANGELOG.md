# Changelog

## Unreleased

- **A top bar replaces the sidebar**: the logo, **Gigs** and **Errands** on the left; **+ New gig**,
  **+ New errand**, the live dot, Inbox, Notifications and your picture on the right. Your picture
  opens the account menu (on hover or click): Recurring, Schedules, Templates, Archive done, Trash,
  saved views, Dashboard, Admin (with its sections), Profile, Themes, Log out. The board's own header
  is gone: its count, help and the Board | Table and Columns | Rows switches end the filters row.
  The **My gigs** page is gone (`/my` opens the board filtered to "Assigned to me").
- Board: rows are 12% wider in the Rows layout; card text is two steps smaller; the gig form has more
  room between its rows.
- Pages use the window's full width (a thin edge on each side).
- The workspace and project chips follow the theme's shape (round with Pill, otherwise a step
  rounder than buttons) and glow in their workspace's colour, a project in its workspace's; so do
  the removable filters of the folded row. The view switches stay at the right end of the filters:
  on the chips row when the filters are open.
- A click outside the errand panel closes it, as it does the gig panel (one shared hook).
- The gig panel always opens beside the board again: the expand button widens that gig only, no
  longer every later one (the choice older versions kept per browser is cleared).
- Done cards get a one-click archive button (bottom left, before **Done …**). The message that
  follows has **Undo**.
- Errands table: the row borders line up again (two cells were flex boxes, which drop out of the
  row's height; the flex layouts now sit inside the cells).
- Recurring gigs: the cards are rebalanced. Title, place and priority (at the right) on top, the
  timing, the facts, then the actions at the bottom left and the assignees' pictures at the bottom
  right. A schedule named after its own timing no longer repeats it (dashes and "N min early"
  aside), and lead times read "16 h early" instead of "960 min early" (Schedules too).
- Errands table: the workspace (when you see several) and the project get columns of their own.
- The round help buttons draw their "?" as an icon, centred whatever the theme's font.
- The gig form dialog scrolls inside itself when it's taller than the window, so its footer sits
  on its bottom edge; before, the form showed through a strip below the footer.
- **Someone given only some projects of a workspace** no longer reads the workspace's description,
  the members who see none of those projects (nor others' project rows), labels not used in those
  projects, the runner's caps, usage and errand counts (only whether runs are paused), or errand
  templates (shared ones included) that no errand of those projects uses.
- A project's access rules apply only in its own workspace, whatever a caller asks.

- **Gigs and errands**: tasks are now **gigs** and crons are **errands**, everywhere: the UI, the
  API (`/gigs`, `/errands`, `gigs:read` ...), MCP tools (`create_gig`, `list_errands` ...),
  permissions (`create_gigs` ...), the database and the docs. Migration 0013 renames the tables and
  the names stored in data (key scopes, permission overrides, activity, allowed tool lists). Old
  `/board/p/<id>/t/<key>` and `/crons` links redirect. Clients using the old API paths or MCP tool
  names need updating.
- An agent's errand executor needs only view and `errands:run`: running your own errands isn't
  managing them, so an errand can be given to an agent without the Errands permission.
- MCP hides `create_gig`, `clone_gig` and `create_from_template` from a key whose user may not
  create gigs.
- `gigg admin deny`, and `each` as a target of `allow`/`deny` (every rule the user has); both
  refuse unknown permission names.
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
