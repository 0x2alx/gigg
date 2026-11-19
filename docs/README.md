# gigg documentation

## User guide

How to use gigg in the web UI. The same pages open in the app from the **?** on each page.

- [The board](guide/board.md): every gig you can see in one place, with filters, sorting, layouts and the trash.
- [Gigs](guide/gigs.md): what a gig holds (description, checklist, subgigs, dependencies, fields, dates) and how to work with it.
- [Agents](guide/agents.md): agents as users, their runs, questions and reports, and the per-gig agent settings.
- [Recurring gigs, templates and schedules](guide/recurring.md): gigs that come back on a schedule.
- [Errands](guide/errands.md): shell commands an agent runs on a schedule, and their runs.
- [The dashboard](guide/dashboard.md): each workspace's agent runs, caps, usage and cost, and pausing runs.
- [Inbox and notifications](guide/inbox.md): agents' questions waiting for you, mentions and other news.
- [Tables, saved views and the command palette](guide/views.md): lists with bulk actions, views you keep, and Ctrl+K.
- [Themes](guide/themes.md): the built-in themes, your own, and importing and exporting them.
- [Your profile](guide/profile.md): your name, picture, password, time zone and preferences.
- [Administration](guide/admin.md): users, workspaces, projects, permissions, keys and the audit log.

## Reference

For people who run gigg or connect programs to it.

- [Configuration](reference/configuration.md): the environment variables, TLS, listeners, backups and running it as a service.
- [Command line](reference/admin-cli.md): `gigg serve` and the `gigg admin` commands.
- [REST API](reference/api.md): authentication, conventions and every endpoint.
- [MCP](reference/mcp.md): connecting an MCP client and the tools it gets.
- [Access model](reference/access.md): users, workspaces, presets, permissions and keys.
- [Runner protocol](reference/runner-protocol.md): how a runner claims gigs, records runs and reports back.
