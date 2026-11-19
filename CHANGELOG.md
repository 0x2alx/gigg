# Changelog

## Unreleased

- Cards: the gig's key stays visible on hover (the **⋯** has its own slot beside it, as on touch
  screens), and the **⋯** sits level with the key at every width, text size and border setting.
- Filters: **Recurring** on the board and in the table: every gig, **Hide recurring** (the gigs a
  recurring gig made) or **Only recurring**. It is kept in the page address, in the board's
  remembered filter and in saved views; the gig list API and the MCP `list_gigs` tool take
  `recurring=hide|only`.
- Gig panel: the **Checklist** section opens on its own when the gig has items, and stays folded
  when it has none; switching to another gig starts from that gig's checklist. The new-gig form and
  the template editor do the same, and picking a template with a checklist opens it.
- Cards: when a person handed a gig to an agent, hovering the person's picture, the **›** or the
  agent's picture reads it as one sentence ("Dana Rivera handed this gig to Scout").
- Docs: the README opens with a transparent logo (light and dark variants) and tells gigs
  (prompt-based, for agents to think through) from errands (commands run on a schedule); the themes
  guide covers **Make with AI**.
- Brand: the wordmark and the link preview's tagline are set in Anta (SIL OFL), bolder, with the
  wordmark centred on the ship; the banner, the link preview and the README header follow.

## 0.1.0 (2026-11-19)

The first public release.

- **A board** for gigs across workspaces and projects: columns or rows, filters kept in the URL,
  manual order and per-column sorting, archive and trash, live updates, a command palette (Ctrl+K),
  tables with bulk actions and saved views. It works on a phone too.
- **Gigs**: Markdown with @mentions, checklists, subgigs, dependencies, custom fields, labels,
  colours, start and due dates, time windows, assignees.
- **Recurring gigs** built from reusable templates and schedules, per workspace or shared.
- **Agents** as ordinary users. They get runs with status, cost and reports, resumable sessions,
  questions in an inbox, and per-gig model, effort, *always run* and tool allowlists. Each
  workspace has daily caps and a pause switch, and the dashboard shows their runs.
- **Errands**: shell commands an agent runs on a schedule in its sandbox, with a run history and
  each run's output. The executor that runs them is outside gigg: the server never runs commands.
- **The same features three ways:** the web UI, a REST API with an OpenAPI document, and MCP.
- **Access control**: workspaces, preset roles, per-permission overrides for users and groups (per
  workspace, per project or everywhere), and API keys limited by workspace, project, scope,
  listener, network and expiry.
- **One binary** with SQLite: HTTPS, an optional second listener for agents, nightly backups and
  snapshots before migrations.
- **Themes** (built-in and your own), built-in help and an audit log.
