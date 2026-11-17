# Runner protocol

gigg starts no processes. Agents are run by a **runner**: any program that, in a loop, claims a gig
from gigg, runs an agent on it, and reports back. This page describes the API a runner uses. All paths
are relative to `/api/v1`; see [api.md](api.md) for authentication and conventions.

## Setup

A runner works for one agent user in one workspace:

1. An agent user (`gigg admin user-add scout -agent`).
2. Access in the workspace: `work_gigs` (*Work on gigs*: claiming, moving, recording runs), e.g. the
   `member` preset (`gigg admin member acme scout member`). A runner needs none of the producer's
   permissions (`edit_gigs`, `delete_gigs`, `archive_gigs`...): they can be denied.
3. A workspace key for that user with the scopes `workspace:read`, `gigs:read`, `gigs:work`,
   `comments:read`, `comments:write`, `runs:write` (no `gigs:write`). Restrict it to an internal listener and the
   runner's network if you can (`-listeners internal -cidrs 10.0.0.0/24`).

| Call | Permission | Scopes |
|---|---|---|
| Read runner state, stats, gigs, runs | `view` | `gigs:read` |
| Claim | `work_gigs` | `gigs:work` |
| Start run, progress, usage | `work_gigs` | `runs:write` |
| Finish run | `work_gigs` | `runs:write`; plus `comments:write` with a report, `gigs:work` with a move |

**A runner works only the gigs assigned to its user.** Claim hands out only gigs that list the caller
among their assignees, and starting a run on a gig needs the same (403 otherwise). A workspace can hold
gigs assigned to someone else; a gig assigned to a person *and* the agent is the agent's to work. New
gigs get the workspace's owner when nobody is assigned (see [gigs](../guide/gigs.md#assignees)), so an
agent's own workspace hands it everything by default.
| Pause / resume runs | `pause_runs` (or admin) | `runs:write` |

## Runner state: caps and pause

```
GET /workspaces/{ws}/runner
```

```json
{
  "workspace_id": 3, "slug": "acme", "name": "Acme",
  "settings": {
    "daily_cap": 15, "night_cap": 10,
    "paused": false, "paused_until": null, "paused_open": false, "paused_by": null, "paused_at": null
  },
  "usage": {
    "gigs_24h": 4, "always_24h": 1, "optional_24h": 3, "runs_24h": 6,
    "usage_pct_24h": 2.5, "usage_pct_7d": 11.0, "cost_usd_7d": 3.21, "unmeasured_7d": 0,
    "last_run_at": 1763000000000, "window": [1762913600000, 1763000000000]
  }
}
```

`GET /runner` returns the same for every workspace in reach. `GET /workspaces/{ws}/stats?since=<ms>`
gives runs started and running, cost, gigs created and done since a time (default 24 h ago).

The workspace's limits, enforced by the server on every claim and every run start:

| Limit | Default | Rule |
|---|---|---|
| `daily_cap` | 15 | Distinct gigs with a run started in the rolling last 24 hours. Once reached, only gigs already counted in the window can be claimed or started (resumes, parked checks, retries). |
| `night_cap` | 10 | Of those, gigs that are **not** `always_run` (budget-dependent gigs). Once reached, only `always_run` gigs and already counted gigs can be claimed or started. |
| Run cap | 4 x `daily_cap` | Runs of any gig started in the last 24 hours. Once reached, nothing can be claimed and run starts fail. |
| Pause | off | While paused, claims and run starts answer 423 `paused`. |

Caps are 0-200 and changed by admins: `PATCH /workspaces/{ws}/runner` with `{"daily_cap": 20, "night_cap": 12}`.

Pausing (admins, or users with `pause_runs` and a key with `runs:write`):

```
POST /runner/pause   {"workspaces": [3], "minutes": 60}   # omit workspaces = every one you may pause;
                                                        # minutes 0 or omitted = until resumed
POST /runner/resume  {"workspaces": [3]}
```

A pause stops new claims and run starts. It doesn't stop runs already going, and finishing,
progress and usage calls still work. Pausing again replaces the previous pause.

## Claiming

```
POST /workspaces/{ws}/gigs/claim
```

```json
{
  "from_category": "todo",
  "to_category": "doing",
  "projects": [7, 8],
  "created_by": ["sam", "12"],
  "start_due": true,
  "always_only": false,
  "dry_run": false
}
```

| Field | Default | Meaning |
|---|---|---|
| `from_category` | `todo` | Status category to take a gig from. |
| `to_category` | `doing` | Category to move it to (the first status of that category in its project). |
| `projects` | all in reach | Only these project ids. A project outside the key's reach answers 404. A narrowed key is limited to its projects anyway. |
| `created_by` | anyone | Only gigs created by these users (ids or usernames). Unknown users are ignored; if none is known, the answer is 204. |
| `start_due` | false | Skip gigs whose `start_at` is in the future. |
| `always_only` | false | Only `always_run` gigs (for example when the runner's own usage budget says no). |
| `dry_run` | false | Return the gig that would be claimed without moving it. |

Answers:

- **200** with the gig (and its `ETag`): it has been moved to `to_category` in the same transaction,
  so two runners can't claim the same gig.
- **204**: nothing matches.
- **423** `paused`: the workspace is paused.
- **404**: the workspace (or a listed project) is not in reach.

A gig is eligible when all of these hold:

- it is in the workspace, in a status of `from_category`, not in the trash, not archived;
- its project is not deleted or archived;
- it matches `projects`, `created_by` and `start_due`;
- if it has a time window (`not_after`): the window is open (`start_at` passed, `not_after` not yet),
  even without `start_due`; and it has not been marked missed;
- no open gig blocks it (a `blocks` relation from a gig whose category is not `done` or `cancelled`);
- the workspace limits above allow it.

Eligible gigs are ordered by: `always_run` first, then higher `priority`, then earliest `due_at` (gigs
without one last), then project position, status position, position in the column, and id. The first
one is claimed.

A dry run applies the same rules (including the pause) but reserves nothing: a later real claim may
return a different gig.

## Recording a run

A run is a record of one agent invocation on a gig. gigg stores what the runner reports; it decides
nothing.

### Start

```
POST /gigs/{id}/runs
{"session_id": "0b0f6a1e-8f5c-4c1e-9d55-2f1f1c3e7a10", "mode": "new", "attempt": 1, "model": "sonnet"}
```

| Field | Meaning |
|---|---|
| `session_id` | Optional. The agent session's id, a lower-case UUID. Stored on the run and on the gig. |
| `mode` | Free label up to 32 characters, e.g. `new`, `resume`, `interrupted`. |
| `attempt` | Attempt number (default 1). |
| `model` | Model actually used, for the record. |

Answers 201 with the run (`status: "running"`). The server checks the pause (423) and the caps (409
`conflict`) again, so a worker that skips the claim gets no more than one that claims. Starting a run
sets the gig's `last_run_id`, sets its `session_id` if one was given, and clears its
`resume_requested_at`.

### Progress

```
POST /runs/{id}/progress   {"pct": 40, "note": "tests pass, writing docs"}
```

Updates the run's heartbeat. `pct` (0-100) and `note` (up to 2 KiB) are optional; when either is given
it is added to the run's timeline (`GET /runs/{id}`). Both show on the gig's board card
(`last_run.progress_pct`, `last_run.progress_note`), and open boards hear of them through a
`run.progress` event: at most one per run every 15 seconds, and a later call (even a bare heartbeat)
sends what was held back. These events are pruned after a day and left out of the gig's history. Answers 409 once the run has ended. An agent
inside the run can report progress itself with the MCP tool `report_progress` (it finds the gig's
running run).

### Finish

```
POST /runs/{id}/finish
```

```json
{
  "status": "needs_human",
  "report_md": "Which database should the export use?",
  "comment_kind": "question",
  "move_to_category": "waiting",
  "session_id": "0b0f6a1e-8f5c-4c1e-9d55-2f1f1c3e7a10",
  "model": "claude-sonnet-4-5",
  "cost_usd": 0.42, "num_turns": 12, "duration_ms": 183000,
  "tokens_in": 52000, "tokens_out": 4100, "tokens_cache_read": 210000, "tokens_cache_write": 18000,
  "exit_code": 0, "error_reason": null,
  "transcript_path": "/var/lib/runner/transcripts/0b0f6a1e.jsonl", "log_path": "/var/log/runner/1234.log"
}
```

| Field | Meaning |
|---|---|
| `status` | Required: `done`, `needs_human`, `error`, `timeout`, `interrupted` or `parked`. |
| `report_md` | Stored as a comment on the gig, linked to the run (up to 64 KiB). Needs `comments:write`. |
| `comment_kind` | `report` (default), `question` or `comment`. Questions and reports notify the gig's creator and assignees; `@username` mentions notify those users. |
| `move_to_category` | Move the gig there (first status of that category); empty = leave it. Needs `gigs:work`. Moving a gig that is done, cancelled or waiting back to `todo` or `doing` reopens it: like a manual move, an always-run gig then needs the *Always-run gigs* permission and a gig with a tool allowlist the *Gig tool allowlists* permission (403 otherwise). |
| `check_at` | Only with `parked`: Unix ms, from now to 7 days ahead. The gig's `start_at` moves there, so a claim with `start_due` skips it until then. |
| `session_id` | The session id if it changed or wasn't known at start. Also stored on the gig. |
| `cost_usd` | Added to the run and to the gig's `cost_usd_total`. |
| `duration_ms` | Defaults to the time since the run started. |
| other fields | Stored on the run as reported (text fields up to 1024 characters). |

Answers 200 with `{"run": ..., "gig": ...}`; 409 if the run has already ended.

gigg attaches no meaning to the status beyond storing it; the runner decides where the gig goes. A
common convention:

| Outcome | `status` | `move_to_category` | Report |
|---|---|---|---|
| Finished | `done` | `done` | `report` |
| Needs an answer | `needs_human` | `waiting` | `question` (shows in the inbox, `GET /inbox`) |
| Failed | `error` / `timeout` | `waiting` or `todo` | `report` with the reason |
| Stopped by the runner | `interrupted` | `todo` | optional |
| Long job, check later | `parked` + `check_at` | `todo` | optional |

### Usage

```
POST /runs/{id}/usage   {"usage_pct": 1.5}
```

Records how many points of the agent account's usage meter the run used (0-100, measured by the
runner, e.g. the meter after minus before). Once per run, only after it has ended, only by the run's
worker or an admin. It feeds `usage_pct_24h` / `usage_pct_7d` in the runner state; gigg doesn't act on
it.

A run that sends no heartbeat (the start, or `POST /runs/{id}/progress`) for `GIGG_STALE_RUN_HOURS`
(default 6) is closed by the server as `interrupted`, with the reason in `error_reason`; its gig stays
where it is. A runner should still, on start-up, finish its own leftover `running` runs at once (for
example as `interrupted`; `GET /workspaces/{ws}/runs?status=running`), and send progress during long runs.

## Sessions and resume

Each gig remembers the last agent session (`session_id`) and run (`last_run_id`). When a person
answers a question:

```
POST /gigs/{id}/resume   {"comment_md": "Use the reporting replica."}
```

(or the MCP tool `resume_gig`). gigg stores the comment with kind `resume_request`, sets the gig's
`resume_requested_at`, and moves the gig to `todo` if it isn't there. It runs nothing.

On its next claim the runner gets the gig like any other. If `resume_requested_at` is set and the gig
has a `session_id`, the runner can resume that session instead of starting a new one, giving the agent
the comments since its last run (`GET /gigs/{id}/comments?since_id=<last seen comment id>`). Starting
the next run clears `resume_requested_at`.

Requesting a resume needs `work_gigs` and `gigs:work` (plus `comments:write` with a comment), and
`always_run` / `allowed_tools` if the gig carries those settings.

## Gig run settings

Gigs (and templates and recurring gigs) carry five fields for the runner. gigg validates and stores
them; applying them is the runner's job.

| Field | Values | Meaning |
|---|---|---|
| `model` | up to 64 characters: letters, digits and `. _ : - [ ]`, starting with a letter or digit; `""` = the agent's default | Model to run the agent with, passed to the agent CLI as one argument (e.g. Claude Code `--model sonnet`). |
| `effort` | `low`, `medium`, `high`, `xhigh`, `max`, or `""` = the agent's default | Effort level for the agent. Note: gigg only stores it; how it reaches the agent (flag, setting or environment) depends on the agent CLI and the runner. |
| `always_run` | boolean | Run the gig whatever the runner's usage budget, day or night. On the server side it ranks first in claims and counts against `daily_cap` but not `night_cap`. Pauses and caps still apply. |
| `allowed_tools` | up to 64 rules, each up to 500 characters; `[]` = none | Tools the agent may use for this gig, as Claude Code permission rules. |
| `run_timeout_minutes` | 5-300, or `null` = the agent's default | How long the runner lets one run go before it stops the agent. A runner should cap it at its own ceiling. It stays under the 6 h after which the server closes a run that sends no heartbeat. |

`allowed_tools` rules are one rule per entry: a tool name (`Read`, `Bash`, `WebFetch`,
`mcp__server__tool`) optionally followed by a specifier in parentheses (`Bash(git status *)`,
`WebFetch(domain:example.com)`). Rules can't contain control characters, can't start with `-`, and
can't contain parentheses inside the specifier; commas and spaces are allowed only inside the
specifier. gigg trims entries and drops empty and repeated ones, keeping the order.

A runner that uses Claude Code applies a non-empty list by running the agent with
`--permission-mode dontAsk` and the rules joined with commas into one argument,
`--allowedTools=<rule1>,<rule2>`: the agent may use exactly those tools, and anything else is refused
without a prompt. With an empty list the runner uses its usual mode. The agent's own settings and
sandbox still bound every run.

Who may change them:

- `model` and `effort`: anyone who may edit the gig (`edit_gigs`).
- `always_run`: setting or clearing it needs the `always_run` permission; so does editing, reopening or
  resuming a gig that has it.
- `allowed_tools`: setting, changing or clearing it needs the `allowed_tools` permission; so does
  editing, reopening or resuming a gig that has a list.
- `run_timeout_minutes`: setting, changing or clearing it needs the `run_timeout` permission.

These permissions come with the `manager` preset only (see [access.md](access.md)). Give them to the
people who decide what unattended agents may do, not to the agents themselves unless intended.

## A minimal runner

```text
key       = read token file
ws        = "current"                      # workspace key
on start:
    for run in GET /workspaces/{ws}/runs?status=running:
        if run.worker_username == me:
            POST /runs/{run.id}/finish {status: "interrupted"}

loop every N minutes:
    state = GET /workspaces/{ws}/runner
    if state.settings.paused: continue

    budget_ok = runner's own check (usage meter, time of day, ...)
    resp = POST /workspaces/{ws}/gigs/claim
               {start_due: true, always_only: not budget_ok}
    if resp.status in (204, 423): continue
    gig = resp.body

    resuming = gig.resume_requested_at != null and gig.session_id != null
    prompt   = build from gig.title, gig.description_md,
               and GET /gigs/{gig.id}/comments (since the last run when resuming)

    run = POST /gigs/{gig.id}/runs
              {mode: resuming ? "resume" : "new",
               session_id: resuming ? gig.session_id : new_uuid(),
               model: gig.model}
    if run.status is 409 or 423:            # cap reached or paused meanwhile
        POST /gigs/{gig.id}/move {category: "todo"}
        continue

    args = []
    if gig.model:  args += ["--model", gig.model]
    if gig.effort: args += effort setting
    if gig.allowed_tools:
        args += ["--permission-mode", "dontAsk",
                 "--allowedTools=" + join(gig.allowed_tools, ",")]
    result = run agent (resume session or new session) with args and prompt,
             posting POST /runs/{run.id}/progress as a heartbeat while it runs

    POST /runs/{run.id}/finish {
        status: result.status,               # done | needs_human | error | timeout | parked
        report_md: result.summary,
        comment_kind: result.asks_question ? "question" : "report",
        move_to_category: done -> "done", needs_human -> "waiting", parked -> "todo", ...,
        check_at: (parked only),
        session_id, cost_usd, num_turns, tokens..., exit_code, error_reason
    }
    if measured: POST /runs/{run.id}/usage {usage_pct: delta}
```

Use an `Idempotency-Key` on the claim, run start and finish calls if the runner retries them after
network errors, so a retry doesn't claim a second gig or close a run twice.
