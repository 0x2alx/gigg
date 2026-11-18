# Access model

## Concepts

- **Users** are people (`human`) or agents (`agent`). People log in with a password; agents only use
  API keys. Any user can be disabled; a disabled user's sessions and keys stop working.
- **Instance admins** are users with the admin flag.
- **Workspaces** hold projects (boards), and projects hold gigs. Permissions are granted per
  workspace, and the gig permissions also per project. A workspace may name an **owner**, shown next to it in the sidebar; ownership grants
  nothing.
- **Groups** are named sets of users that share access rules.
- **Access rules** give a user or a group a **preset** and/or per-permission **overrides** in one
  project, in one workspace, or in all workspaces (which also covers workspaces created later).

## Permissions

Every permission is held per workspace. Every permission other than `view` also needs `view` in the
same workspace: without it, it counts as not held.

| Permission | Section | Allows |
|---|---|---|
| `view` | Basics | See the workspace, its projects, gigs, comments and runs. Every other permission needs it. |
| `comment` | Basics | Write comments and edit or delete your own. |
| `work_gigs` | Gigs | Work on gigs: move them between statuses of their project (and reorder), tick checklist items, ask for a resume; claim gigs assigned to you and report runs (agents). |
| `edit_gigs` | Gigs | Change and assign gigs (title, description, dates, labels, run settings...), add, edit, reorder and remove checklist items, add and remove dependencies (blocking/related links, subgigs that hold their parent), and move gigs to another project. |
| `create_gigs` | Gigs | Create one-off gigs, clone gigs, and create gigs from templates (with `edit_gigs`). |
| `delete_gigs` | Gigs | Move gigs to the trash and restore them (with `edit_gigs`). |
| `archive_gigs` | Gigs | Archive and unarchive gigs (with `edit_gigs`). |
| `always_run` | Runner | Mark gigs to run whatever the usage budget. The gig caps and pauses still apply. |
| `allowed_tools` | Runner | Give gigs (templates, recurring gigs) the list of tools the agent runner may use for them, and change gigs that have one. |
| `run_timeout` | Runner | Set, change or clear how long a runner lets one run of a gig (template, recurring gig) go, and give an errand a timeout other than the default or change it. |
| `pause_runs` | Runner | Pause and resume new agent gig runs and errand runs; with an all-workspaces rule, in every workspace at once. Changing the caps stays with admins. |
| `errands` | Runner | Make, change, pause, run and delete errands (in one project, with a project rule); in a whole workspace, its errand templates too. An agent's executor needs no permission to run the errands it is the worker of. |
| `errand_unsandboxed` | Runner | Let an errand run outside the agent's sandbox. An errand loses the flag when someone without this permission changes it, and its runs fail while the person who set it lacks the permission. |
| `recurring_gigs` | Planning | Create, change, pause and stop recurring gigs and schedules (recurring gigs also need `edit_gigs`, and `create_gigs` to make their gigs). |
| `templates` | Planning | Create, change and delete gig templates, with `edit_gigs` (using one only needs `create_gigs`). |
| `custom_fields` | Planning | Define the workspace's custom fields. |
| `saved_filters` | Planning | Save personal filtered views. |
| `manage_workspace` | Admin | Projects, statuses (columns), labels, workspace settings, and other people's comments. |

`GET /api/v1/permissions` returns this list with labels, the presets and the rule order.

### Producers and consumers

`work_gigs` and `edit_gigs` split working on gigs from writing them. A **consumer** (typically an
agent) holds `work_gigs` without `edit_gigs`: it reads its gigs, comments, moves them between the
statuses of their project, ticks checklist items, claims the gigs assigned to it, records runs and asks
for resumes, but can't change a title, description, prompt, model, tools, dates, labels, assignees,
custom fields or dependencies, add or remove checklist items, move a gig to another project, or
delete or archive it. The `consumer` preset is exactly this. A **producer** (a person, or an agent
trusted to plan work) also holds `edit_gigs` and the rest. The key scopes follow the same split: `gigs:work` and `gigs:write`.

### What `always_run` and `allowed_tools` guard

These two decide what an unattended agent does, so they also protect existing gigs:

- Setting or clearing `always_run` on a gig, template or recurring gig needs `always_run`. On a gig
  that has `always_run` set, these need `always_run` too: changing any field, its checklist (ticking an item excepted), custom
  field values or dependencies; reopening it (moving it into `todo` or `doing` from another category,
  by hand, by a move to another project, or by finishing a run with `move_to_category`); unarchiving
  or restoring it; requesting a resume.
- Setting, changing or clearing `allowed_tools` needs `allowed_tools`. The same rules apply to a gig
  that has a non-empty list.
- Changing a schedule that a recurring gig with either setting uses (directly, or by giving another recurring gig on it new timing) needs the matching permission,
  as editing that recurring gig would.
- Creating a gig from a template that carries either setting, and creating or changing a recurring
  gig whose content (or template) carries it, needs the matching permission.
- Cloning a gig copies neither setting.

`run_timeout` guards only the timeout itself: setting, changing or clearing `run_timeout_minutes` on
a gig, template or recurring gig, creating a gig or recurring gig from a template that has one, and
an errand `timeout_s` other than the default (300) on a new errand or a change of it. Other edits of
a gig with a timeout need nothing extra. The agent's host caps every timeout (the runner's
`timeout_max`, the errand executor's `errand_timeout_max`), whatever gigg stores.

## Presets

A preset is a named bundle of permissions. Roles shown in the UI are presets. Each one includes the
one before it:

- **viewer** sees the workspace and changes nothing;
- **consumer** works on what it's given: comments, moves gigs between statuses, ticks checklist items,
  claims and runs the gigs assigned to it, runs its own errands;
- **member** also makes and shapes gigs;
- **manager** also runs the workspace: planning, errands, the runner's settings.

| Permission | `viewer` | `consumer` | `member` | `manager` |
|---|:-:|:-:|:-:|:-:|
| `view` | x | x | x | x |
| `comment` | | x | x | x |
| `work_gigs` | | x | x | x |
| `edit_gigs` | | | x | x |
| `create_gigs` | | | x | x |
| `delete_gigs` | | | x | x |
| `archive_gigs` | | | x | x |
| `saved_filters` | | | x | x |
| `recurring_gigs` | | | | x |
| `templates` | | | | x |
| `custom_fields` | | | | x |
| `manage_workspace` | | | | x |
| `errands` | | | | x |
| `pause_runs` | | | | x |
| `always_run` | | | | x |
| `allowed_tools` | | | | x |
| `run_timeout` | | | | x |
| `errand_unsandboxed` | | | | |

`errand_unsandboxed` is in no preset: it is granted only by an override (or by being an instance
admin). A preset is a starting point: any rule can add or remove single permissions with overrides.

The role displayed for a user in a workspace (`role` in `/auth/me` and member lists) is the closest
preset to what they actually hold there: `admin`, else `manager` if they have `manage_workspace`,
`member` if `edit_gigs`, `consumer` if `work_gigs`, `viewer` if `view`. It is for display only.

## Access rules

A rule belongs to one user or one group, and to one project, one workspace or all workspaces. A user
or group has at most one rule per level. A rule has:

- `preset`: `viewer`, `consumer`, `member`, `manager`, or none;
- `overrides`: a map of permission to `true` (allow) or `false` (deny).

Inside one rule, an override beats the preset. A permission that the rule's preset doesn't include and
that has no override is "no opinion" (inherit), not a deny.

### Rules for one project

A project rule decides, in that project only, the gig permissions (`view`, `comment`, `work_gigs`,
`edit_gigs`, `create_gigs`, `delete_gigs`, `archive_gigs`) and `errands`. Its preset is `viewer`,
`consumer` or `member` (the others give workspace-level permissions); `errands` is in no preset there
either, and is given as an override. Everything else (templates, recurring gigs, schedules, custom
fields, errand templates, no-sandbox errands, run timeouts, runner settings, managing the workspace) is
decided for the whole workspace and needs `view` of the whole workspace.

Uses:

- **Give someone one project of a workspace**: a project rule alone. They see the workspace (its name,
  and its labels and members, which gigs show) and only the projects they have, with their gigs,
  comments, runs, activity and errands (and the errand templates and schedules those errands use);
  nothing else workspace-level. Their gigs' assignees, notifications and claims follow the same reach,
  and so do their own errands: an agent's executor runs the errands it is the worker of wherever it
  sees their project.
- **Let someone run errands in one project**: a project rule with `errands=allow`. They make, change,
  pause, run and delete that project's errands. Changing one's command never changes another errand
  that shares its template: the errand gets its own copy. Errand templates, no-sandbox errands and
  other timeouts than the default stay with whoever holds them for the whole workspace.
- **Lower or raise a member in one project**: a project rule with overrides (`edit_gigs=deny` in a
  project where they should only move gigs). A preset only adds permissions; an override can remove one.

A workspace's member list shows people with only some projects as such, with their projects.

### Resolution

For one user, one place and one permission, the first level with an opinion decides, most specific
first:

1. instance admin: allowed;
2. the user's rule for this project (gig permissions in a project);
3. the user's rule for this workspace;
4. the user's rule for all workspaces;
5. the rules of the user's groups for this project;
6. the rules of the user's groups for this workspace;
7. the rules of the user's groups for all workspaces;
8. otherwise: not allowed.

Between several groups on the same level, any allow beats a deny. Finally, a permission other than
`view` is not held unless `view` resolves to allowed in the same place (the project, for a gig
permission there; the whole workspace otherwise).

So the user's own rules beat group rules, and a workspace rule beats an all-workspaces rule. A user
deny at any level wins over every group allow below it.

Example: group `agents` has `member` for all workspaces. Agent `scout` is in `agents` and has a user
rule in `acme` with no preset and `always_run=allow, delete_gigs=deny`. In `acme`, `scout` has the
member permissions except `delete_gigs` (the user rule denies it), plus `always_run`. In other
workspaces `scout` is a plain member.

`GET /api/v1/users/{id}/access` (admin) shows a user's rules, groups and the resolved grid for every
workspace, and for each project with a rule of its own, with the level each answer came from (`admin`,
`user_project`, `user`, `user_all`, `group_project`, `group`, `group_all`, `needs_view`, `default`).
The row with `workspace_id` 0 is the all-workspaces level: what a new workspace starts with.

### Setting rules

| How | What |
|---|---|
| `gigg admin member <ws>[/<project>] <user> <preset\|none>` | Set a user's preset in one workspace or project, keeping their overrides there; `none` removes the rule. |
| `gigg admin access <user> <ws\|ws/project\|all> <preset\|none> [perm=allow\|deny ...]` | Replace a user's rule at one level. |
| `PUT /api/v1/users/{id}/access` | `{"workspace_id": 3 or null, "project_id": 12 (optional), "preset": "member", "overrides": {"always_run": true}}`; no preset and no overrides removes the rule. |
| `PUT /api/v1/groups/{id}/access` | The same for a group. |
| `PUT` / `DELETE /api/v1/workspaces/{ws}/members/{user}` | Set a user's preset in a workspace (`{"role": "member"}`, keeping their overrides), or remove their own rule there. |
| `PUT` / `DELETE /api/v1/projects/{p}/members/{user}` | The same for one project (`viewer`, `consumer` or `member`). |

The API routes need an instance admin in a browser session (keys are refused). Group membership is
changed with `PATCH /api/v1/groups/{id}`. When a change leaves a user unable to see a project, they
are removed as assignee from its gigs, at once.

## Instance admins

An admin user, through a browser session or an admin key:

- holds every permission in every workspace and sees every workspace;
- can create workspaces, archive them and set their owners, and change runner caps;
- can create users and agents, change profiles, disable users, end sessions, set agents' pictures;
- can manage every user's API keys, and read the instance-wide audit log;
- sees key, member and access events in activity, and edited or deleted comment text;
- can, from a browser session only: set access rules, manage groups and workspace members, make users
  admins, and set passwords.

An admin is not listed among a workspace's members unless they have their own rule there.

Admin keys (unbound keys of an admin user) are limited by their `admin:*` scopes for admin
operations: `admin:workspaces`, `admin:members`, `admin:users`, `admin:keys`, `admin:audit` (see
[api.md](api.md#scopes)). A limited key of an admin user holds every permission in its places but is
not an instance admin.

## API keys, scopes and permissions

An API request succeeds only if all of these hold:

1. **Key restrictions**: not revoked or expired; the listener and client network are allowed.
2. **Reach**: for a limited key, the object is in one of its places (a whole workspace, or that
   project); a workspace-level object needs a whole workspace among them. For any credential, the
   user sees the object's project (or, for a workspace-level object, the whole workspace). Otherwise
   the answer is 404.
3. **Permission**: the key's user holds the permission there (by the rules above).
4. **Scope**: the key carries the scope the operation needs.

Missing 3 or 4 gives 403 naming the permission or scope. Browser sessions carry every scope, so only 1-3
apply (1 only for the listener: sessions work on the main listener only).

Typical scope sets:

| Use | Scopes | Permissions (in the workspace) |
|---|---|---|
| Read-only dashboard | `workspace:read`, `gigs:read`, `comments:read` | `viewer` |
| Agent working gigs via MCP (a consumer) | `workspace:read`, `gigs:read`, `gigs:work`, `comments:read`, `comments:write`, `runs:write` | `consumer` |
| Unattended runner | the same | the same |
| Someone who also writes gigs through a key (a producer) | add `gigs:write` (and `gigs:delete`) | `member` |
| Admin automation | `admin:*` as needed, plus the workspace scopes | instance admin |

Work done in the background on a user's behalf (recurring gigs creating gigs, notifications,
assignees) checks that user's permissions at that time, and the user must exist and not be disabled.
