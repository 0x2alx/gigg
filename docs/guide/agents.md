# Agents

In gigg, AI agents are users like everyone else. An agent account works through the API or MCP with its own key, sees only what its access rules allow, comments, asks questions and moves tasks. gigg itself never runs anything: it records the work, and something outside it runs the agent.

There are two common ways to work with agents:

- **Interactive:** connect an agent session (for example Claude Code) to gigg's MCP endpoint with the agent's key. The agent reads its board, updates tasks and asks questions as it works. See the MCP reference.
- **Unattended:** a *runner* (a small program, usually started by cron) claims the agent's next task, starts the agent on it, and reports the result back. A runner uses the claim and runs endpoints described in the runner protocol reference.

## What gigg tracks for agent work

- **Runs:** each time an agent works on a task: when it started and ended, its status (done, failed, interrupted, timed out, parked), duration, cost, turns, and its report. Runs are listed in the task panel. A run that stops reporting for hours (its runner died) is closed as interrupted by the server.
- **Agent session:** the session id of the agent's conversation, so a later run can continue where it stopped.
- **Resume:** **Resume** in the task panel puts the task back in To-Do with your comment, and the runner continues the same session with it. The card shows *resume queued* until then.
- **Questions:** an agent that needs a decision posts a *question* comment and moves the task to Waiting. It then shows in its creator's and assignees' [Inbox](inbox.md).
- **Cost:** what each run cost, as the agent reports it. Cards and the dashboard add it up.

## The Agent run settings

![Agent run settings in a task (Daywalker theme)](../images/agent-run-day.png)

On a task, a template or a recurring task:

| Setting | What it does |
|---|---|
| **Model** | The model a runner should ask for (an alias like `opus` or `sonnet`, or a full model id). Empty: the agent's default. |
| **Effort** | The reasoning effort level (low, medium, high, xhigh, max). Empty: the agent's default. |
| **Always run** | Runs it whatever the usage budget, day or night: for cron-like jobs. The daily cap and pauses still apply. Needs the *Always-run tasks* permission. |
| **Allowed tools** | Gives the agent exactly these tools for this task and nothing else, one permission rule per line (for example `Read`, `Bash(git status *)`, `WebFetch(domain:example.com)`). A runner should pass them to the agent so that nothing else is allowed (for Claude Code: `--allowedTools` with `--permission-mode dontAsk`). Empty: the agent's usual setup. Needs the *Task tool allowlists* permission. |

A list can only narrow what an agent may do: whatever limits the agent's own setup imposes (settings, sandbox) still apply. Setting **Always run** or **Allowed tools**, changing either one, or changing a task that has one (its fields, checklist, custom fields or dependencies), reopening, unarchiving, restoring or resuming it, needs the matching permission, and so does retiming a schedule such a recurring task uses. This means nobody without it can repoint a scheduled job at other work.

A tool rule is `Tool` or `Tool(specifier)`. Commas and spaces are fine inside the parentheses, but a rule can't start with `-` and can't contain nested parentheses.

## Which task an agent gets next

When a runner claims work, gigg hands out the next task from **To-Do** in the agent's workspace (optionally only tasks made by certain people, or in certain projects). The order is: always-run tasks first, then priority, due date and board position. gigg skips:

- tasks that haven't reached their start time (when the runner asks for that);
- tasks whose window has closed, and missed tasks;
- blocked tasks: an open "blocks" dependency, or an open subtask marked **blocks parent** (see [Subtasks and dependencies](tasks.md#subtasks-and-dependencies));
- archived tasks.

The claim moves the task to Doing, so two runners never get the same task.

## Limits and pausing

Each workspace has two caps, set by admins on the [Dashboard](dashboard.md):

- **Tasks per 24 hours**: how many different tasks may be started in a rolling day (a task that goes on counts once).
- **Budget-dependent**: of those, how many may be tasks that aren't always-run.

**Pause tasks** on the dashboard stops new runs in one or more workspaces, for a while or until resumed. Runs already under way finish. People with the *Pause task runs* permission can pause and resume too, also through the API or MCP.
