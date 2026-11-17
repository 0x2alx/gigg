# Access model

## Concepts

- **Users** are people (`human`) or agents (`agent`). People log in with a password; agents only use
  API keys. Any user can be disabled; a disabled user's sessions and keys stop working.
- **Instance admins** are users with the admin flag.
- **Workspaces** hold projects (boards), and projects hold gigs. Permissions are granted per
  workspace. A workspace may name an **owner**, shown next to it in the sidebar; ownership grants
  nothing.
- **Groups** are named sets of users that share access rules.
- **Access rules** give a user or a group a **preset** and/or per-permission **overrides** in one
  workspace, or in all workspaces (which also covers workspaces created later).

## Permissions

Every permission is held per workspace. Every permission other than `view` also needs `view` in the
same workspace: without it, it counts as not held.

| Permission | Section | Allows |
|---|---|---|
| `view` | Basics | See the workspace, its projects, gigs, comments and runs. Every other permission needs it. |
| `comment` | Basics | Write comments and edit or delete your own. |
| `edit_gigs` | Gigs | Change, move and assign gigs; claim gigs and report runs (agents). |
| `create_gigs` | Gigs | Create one-off gigs, clone gigs, and create gigs from templates (with `edit_gigs`). |
| `delete_gigs` | Gigs | Move gigs to the trash and restore them (with `edit_gigs`). |
| `archive_gigs` | Gigs | Archive and unarchive gigs (with `edit_gigs`). |
| `relations` | Gigs | Add and remove blocking/related links between gigs (with `edit_gigs`). |
| `checklists` | Gigs | Add, tick and remove checklist items (with `edit_gigs`). An override alone doesn't give a viewer these: the admin page marks it "needs Edit gigs". |
| `always_run` | Runner | Mark gigs to run whatever the usage budget. The gig caps and pauses still apply. |
| `allowed_tools` | Runner | Give gigs (templates, recurring gigs) the list of tools the agent runner may use for them, and change gigs that have one. |
| `run_timeout` | Runner | Set, change or clear how long a runner lets one run of a gig (template, recurring gig) go, and give an errand a timeout other than the default or change it. |
| `pause_runs` | Runner | Pause and resume new agent gig runs and errand runs; with an all-workspaces rule, in every workspace at once. Changing the caps stays with admins. |
| `errands` | Runner | Make, change, pause, run and delete errands; an agent's executor needs it to run its errands. |
| `errand_unsandboxed` | Runner | Let an errand run outside the agent's sandbox. An errand loses the flag when someone without this permission changes it, and its runs fail while the person who set it lacks the permission. |
| `schedules` | Planning | Create, change, pause and stop recurring gigs and schedules (recurring gigs also need `edit_gigs`, and `create_gigs` to make their gigs). |
| `templates` | Planning | Create, change and delete gig templates, with `edit_gigs` (using one only needs `create_gigs`). |
| `custom_fields` | Planning | Define the workspace's custom fields. |
| `saved_filters` | Planning | Save personal filtered views. |
| `manage_workspace` | Admin | Projects, statuses (columns), labels, workspace settings, and other people's comments. |

`GET /api/v1/permissions` returns this list with labels, the presets and the rule order.

### What `always_run` and `allowed_tools` guard

These two decide what an unattended agent does, so they also protect existing gigs:

- Setting or clearing `always_run` on a gig, template or recurring gig needs `always_run`. On a gig
  that has `always_run` set, these need `always_run` too: changing any field, its checklist, custom
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

A preset is a named bundle of permissions. Roles shown in the UI are presets.

| Permission | `viewer` | `commenter` | `member` | `manager` |
|---|:-:|:-:|:-:|:-:|
| `view` | x | x | x | x |
| `comment` | | x | x | x |
| `edit_gigs` | | | x | x |
| `create_gigs` | | | x | x |
| `delete_gigs` | | | x | x |
| `archive_gigs` | | | x | x |
| `relations` | | | x | x |
| `checklists` | | | x | x |
| `saved_filters` | | | x | x |
| `schedules` | | | | x |
| `templates` | | | | x |
| `custom_fields` | | | | x |
| `manage_workspace` | | | | x |
| `always_run` | | | | |
| `allowed_tools` | | | | |
| `run_timeout` | | | | |
| `pause_runs` | | | | |

`always_run`, `allowed_tools`, `run_timeout` and `pause_runs` are in no preset: they are granted only by an override
(or by being an instance admin).

The role displayed for a user in a workspace (`role` in `/auth/me` and member lists) is the closest
preset to what they actually hold there: `admin`, else `manager` if they have `manage_workspace`,
`member` if `edit_gigs`, `commenter` if `comment`, `viewer` if `view`. It is for display only.

## Access rules

A rule belongs to one user or one group, and to one workspace or to all workspaces. A user or group has
at most one rule per level. A rule has:

- `preset`: `viewer`, `commenter`, `member`, `manager`, or none;
- `overrides`: a map of permission to `true` (allow) or `false` (deny).

Inside one rule, an override beats the preset. A permission that the rule's preset doesn't include and
that has no override is "no opinion" (inherit), not a deny.

### Resolution

For one user, one workspace and one permission, the first level with an opinion decides, most specific
first:

1. instance admin: allowed;
2. the user's rule for this workspace;
3. the user's rule for all workspaces;
4. the rules of the user's groups for this workspace;
5. the rules of the user's groups for all workspaces;
6. otherwise: not allowed.

Between several groups on the same level, any allow beats a deny. Finally, a permission other than
`view` is not held unless `view` resolves to allowed in the same workspace.

So the user's own rules beat group rules, and a workspace rule beats an all-workspaces rule. A user
deny at any level wins over every group allow below it.

Example: group `agents` has `member` for all workspaces. Agent `scout` is in `agents` and has a user
rule in `acme` with no preset and `always_run=allow, delete_gigs=deny`. In `acme`, `scout` has the
member permissions except `delete_gigs` (the user rule denies it), plus `always_run`. In other
workspaces `scout` is a plain member.

`GET /api/v1/users/{id}/access` (admin) shows a user's rules, groups and the resolved grid for every
workspace, with the level each answer came from (`admin`, `user`, `user_all`, `group`, `group_all`,
`needs_view`, `default`). The row with `workspace_id` 0 is the all-workspaces level: what a new
workspace starts with.

### Setting rules

| How | What |
|---|---|
| `gigg admin member <ws> <user> <preset\|none>` | Set a user's preset in one workspace, keeping their overrides there; `none` removes the rule. |
| `gigg admin access <user> <ws\|all> <preset\|none> [perm=allow\|deny ...]` | Replace a user's rule at one level. |
| `PUT /api/v1/users/{id}/access` | `{"workspace_id": 3 or null, "preset": "member", "overrides": {"always_run": true}}`; no preset and no overrides removes the rule. |
| `PUT /api/v1/groups/{id}/access` | The same for a group. |
| `PUT` / `DELETE /api/v1/workspaces/{ws}/members/{user}` | Set a user's preset in a workspace (`{"role": "member"}`, keeping their overrides), or remove their own rule there. |

The API routes need an instance admin in a browser session (keys are refused). Group membership is
changed with `PATCH /api/v1/groups/{id}`. When a change leaves a user unable to see a workspace, they
are removed as assignee from its gigs.

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
[api.md](api.md#scopes)). A workspace-bound key of an admin user holds every permission in its own
workspace but is not an instance admin.

## API keys, scopes and permissions

An API request succeeds only if all of these hold:

1. **Key restrictions**: not revoked or expired; the listener and client network are allowed.
2. **Reach**: the object's workspace is the key's workspace (for a bound key), and its project is
   among the key's projects (for a narrowed key). Otherwise the answer is 404.
3. **Permission**: the key's user holds the permission in that workspace (by the rules above).
4. **Scope**: the key carries the scope the operation needs.

Missing 3 or 4 gives 403 naming the permission or scope. Browser sessions carry every scope, so only 1-3
apply (1 only for the listener: sessions work on the main listener only).

Typical scope sets:

| Use | Scopes | Permissions (in the workspace) |
|---|---|---|
| Read-only dashboard | `workspace:read`, `gigs:read`, `comments:read` | `viewer` |
| Agent working gigs via MCP | `workspace:read`, `gigs:read`, `gigs:write`, `comments:read`, `comments:write`, `runs:write` | `member` |
| Unattended runner | the same | `member`, plus `always_run` / `allowed_tools` overrides only if the runner's agent should be able to set them |
| Admin automation | `admin:*` as needed, plus the workspace scopes | instance admin |

Work done in the background on a user's behalf (recurring gigs creating gigs, notifications,
assignees) checks that user's permissions at that time, and the user must exist and not be disabled.
