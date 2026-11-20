# Metrics

**Metrics** (account menu, under Dashboard) shows what was done over a period: gigs created and done, how long they took, agent runs by outcome, cost and tokens, errand runs and recurring gigs that were on time. It reads the same data as the rest of the app, and you see only the workspaces and projects you can see anywhere else; admins see everything.

## Picking what to look at

- **Period**: the last 7, 30 or 90 days, a year, or **Custom** with a first and last day (both included). The longest range is 400 days.
- **Group by**: **Day**, **Week** (Monday to Sunday) or **Month**. A range picks a sensible one for you; you can change it. A week or month that is only partly inside the range shows just the part inside it.
- **Filters**: a workspace, a project, a person, and a kind (humans or agents). They combine.
  - For **gigs**, a person or kind means the gig is assigned to that person, or to someone of that kind.
  - For **agent runs and errand runs**, it means who did the run.
  - Errand runs belong to a workspace, not to a project, so picking a project leaves them out, and so does seeing only some projects of a workspace.

Days are cut in your [time zone](profile.md) (the browser's, if you haven't set one), so a gig finished at 23:30 your time counts on that day, not the next.

## The numbers

The tiles add up the whole period:

- **Gigs done**, with how many were created and cancelled. Done is counted when a gig reaches a Done status (a gig reopened and finished again counts again); cancelled is counted when a gig is moved into a Cancelled status.
- **Open now**: gigs not done, cancelled, archived or deleted, right now, within the filters.
- **Median cycle time**: the middle value of the time from creation to done, for the gigs done in the period.
- **Agent runs**, with the share that ended done, failed (error) and timed out. Runs still going or parked are left out of the shares.
- **Run time**: all the runs' time, and the average per run.
- **Cost** (USD) and **tokens**, as the agents report them. Cached tokens are shown apart.
- **Errand runs**, with failures and missed slots.
- **Recurring on time**: of the recurring gigs whose window opened in the period, the share that were done in time. A gig whose window closed unfinished counts as missed.

Under them, small charts draw each bucket: gigs done and created, agent runs stacked by outcome, cost, and (when there are any) errand runs and recurring gigs. Hover a bar for the exact numbers. Two tables break the runs down **by agent**, **by model** and **by project**: runs, cost and tokens, most expensive first.

## Not included

The time a gig spends in Doing isn't shown: the activity log records each move, but reading the time between moves back for every gig is too costly and unreliable, since statuses can be renamed, skipped and reopened.

## From an agent or a script

The same numbers come from `GET /metrics` (see the [API reference](../reference/api.md)) and the `get_metrics` MCP tool (see the [MCP reference](../reference/mcp.md)); they need the `gigs:read` scope, and errand runs also `errands:read`.
