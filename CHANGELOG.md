# Changelog

## Unreleased

- Followers: anyone who can see a gig can follow it (**Follow** at the top of the gig panel, or named in
  its **Followers** in the gig form, the API and MCP), and gets its bells as its assignees do. A gig's
  bells go to its assignees and followers, no longer to its creator as such: you follow the gigs you
  create unless you turn off **Follow the gigs I create** (Profile → Bell). Templates carry followers,
  so gigs made from one, recurring ones included, start with them. API: `followers` on gigs,
  templates and recurring gigs, `GET/PUT/DELETE /gigs/{id}/follow`; MCP: `follow_gig`. Existing gigs
  start with no followers.
- Bell settings (Profile → Bell): pick which events ring you: needs a person (questions, and the new
  **failed** kind: a run that ended in an error or timed out), finished (reports; off by default),
  assigned, mentioned, missed window. A timed-out run used to ring nobody.
- Audit log: every object records the same thing. An update records each field that changed with its old
  and new value (schedules, templates, recurring gigs, errands and errand templates recorded only a few
  fields before; workspaces, projects and statuses only the new values), a create every initial value, a
  delete the last values. One store helper diffs the object as read before and after the write, so new
  fields are audited without more code. Errand commands are recorded old and new, with secrets masked.
  **Admin → Audit** shows a change as `field: old → new`. In workspace activity, the change feed and live
  events, errand entries carry their content only for readers with `errands:read`.
- Cards: a gig a recurring gig made stands out: a glowing purple **Recurring** badge on its top
  line, and a purple-tinted card and border. The hue is the same in every theme, the strength follows
  the theme, and the badge text reads at 4.5:1 in every built-in theme. The small ↻ pill in the card's
  foot is gone (the gig panel keeps it).
- Cards: the gig's key sits in the card's top right corner and always stays visible. The card's
  menu is a **gear** at the start of the foot, always shown (no longer only on hover), level with the
  foot's row; a done card's archive button follows it.
- Filters: one **More filters** button on the board and in the table holds the pickers that used to
  fill the toolbar (assignee, label, priority, recurring, and archived on the board; status, label,
  assignee, priority, recurring and include done in the table), with a count of those that are on.
- Filters: **Always run** on the board and in the table: every gig, **Hide always run** or **Only
  always run**. Kept like the recurring filter (address, remembered board filter, saved views); the
  gig list API and MCP `list_gigs` take `always_run=hide|only`.
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
