# Crons

A **cron** is a shell command an agent runs on a schedule: every 5 minutes, every hour between 09:00 and 17:00, every day at 03:30. It runs in the agent's sandbox, and each run's result is kept on the **Crons** page. Crons make no tasks.

Use a cron for checks and housekeeping that need no thinking: is the disk full, did the backup run, is a service answering. Use a [recurring task](recurring.md) when the agent should actually work on something.

## The Crons page

**Crons** in the sidebar lists every cron you can see. Failing crons come first. Each row shows the cron's state (**failing**, **ok**, **new** or **paused**), its schedule, the agent that runs it, its last run and when it runs next. **Failing** and **Paused** at the top narrow the list.

Click a cron to open its panel:

- **Run now** runs it once within about a minute, whatever its schedule.
- **Pause** / **Resume**. A paused cron starts again from its next slot, and the slots it missed aren't caught up.
- **Edit**, **Delete**.
- **Command**, timeout, where the output goes, and the **runs**: status, exit code, duration. Click a run to see its output.

A run is **ok** when the command exits 0 and **failed** otherwise. It is **timed out** when it's killed at its timeout. A **missed** run had its slot pass while nothing could run it (the agent's machine was down, or crons were paused). A **skipped** run came due while the previous one was still going.

## Making a cron

**New cron** asks for:

- **Name** and **Command**. bash runs the command as the agent, inside its sandbox: the agent's own files and allowed network hosts only. Services on the machine itself (localhost) aren't reachable from the sandbox.
- **Repeat**: every N **minutes** (1–360) or **hours** (1–24), optionally only on some weekdays and only between two times; or **daily**, **weekly**, **monthly** at a time. Times are in the cron's time zone, which defaults to yours.
- **Timeout**: 10 seconds to an hour, 5 minutes by default.
- **Output**: by default the last 64 KiB of each run's output is kept in gigg. **Keep it on the agent's host only** sends gigg just the exit code and timing, for output that shouldn't leave the machine.
- **Open a task for the agent when it starts failing**: the first failure of a streak creates a task with the exit code and a link to the run (never the output itself: it's the command's, not instructions). Only the agent or someone who manages the workspace switches this.
- **Agent**: whoever's executor runs it. An agent makes crons for itself. People who manage the workspace can give a cron to any agent there that has the **Crons** permission.

When a cron starts failing, or recovers, the agent's executor also posts an alert.

## Without the sandbox

Some commands need what the sandbox blocks, for example a service on localhost. **Run outside the sandbox** is offered only to people with the **No-sandbox crons** permission. Such a cron runs with everything the agent's user can reach, so turn it on only for commands you've read and trust.

The flag doesn't survive other hands. When someone without that permission changes anything in the cron, it goes back into the sandbox (if other crons share its template, this cron gets a sandboxed copy and theirs stay as they were). They can pause it, but not resume it, run it now, retime its schedule or edit its template directly. When the person who set it loses the permission, its runs fail instead of running unsandboxed.

## Pausing everything

**Pause runs** on the Dashboard has two boxes, **Task runs** and **Cron runs**. Paused crons stay paused until resumed, and the slots missed meanwhile are recorded as missed, never run late. The Dashboard shows each workspace's crons and how many are failing.

## Permissions

- Seeing crons and their runs: anyone who can see the workspace. Reading run output with an API key needs the `crons:output` scope.
- Making, changing, pausing and running crons: the **Crons** permission (`crons:write` for keys).
- The agent's executor claims and reports its own runs with a key holding `crons:run`.
