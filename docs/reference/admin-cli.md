# Command line

```
gigg serve         run the server (the default when no command is given)
gigg healthcheck   exit 0 if the local server answers /healthz
gigg admin ...     first setup and recovery, directly on the data directory
gigg version       print the version (also --version, -v)
```

Any other command prints the usage and exits with status 2.

## gigg serve

Reads the configuration from the environment (see [configuration.md](configuration.md)), opens the
database in `GIGG_DATA_DIR` (applying pending migrations), and serves every configured listener until
SIGTERM or SIGINT. Exit status: 0 after a clean stop, 1 on a database or listener error, 2 on a
configuration error.

## gigg healthcheck

For container health checks. It loads the same configuration, picks a listener, and requests
`/healthz` on `127.0.0.1` at that listener's port with a 4-second timeout. Exit status 0 when the
answer is 200, 1 otherwise (the reason goes to stderr).

- It prefers an extra listener (the first by name, alphabetically) over the main one, since extra
  listeners are plain HTTP.
- If it uses a TLS main listener, it skips certificate verification (it talks to itself on
  loopback, and the certificate names the public host).
- Note: it always connects to `127.0.0.1`. A listener bound to a specific non-loopback address (for
  example `GIGG_LISTEN_INTERNAL=10.0.0.2:8080`) can't be reached that way and the check fails.

In a container (the image has no shell, so use the exec form):

```
HealthCmd=["/gigg","healthcheck"]
```

## gigg admin

`gigg admin` works directly on the database in `GIGG_DATA_DIR`, with or without the server running
(SQLite handles the concurrency). It needs the same environment as the server; with the container
image, run it inside the container:

```sh
podman exec -it gigg /gigg admin user-add admin -admin
```

General rules:

- Opening the database applies pending migrations, like `gigg serve` does.
- Messages go to stderr; only key tokens go to stdout.
- Exit status: 0 on success, 2 on a configuration error, 1 on anything else (including a wrong
  number of arguments, a bad flag or an unknown command). `gigg admin`, `gigg admin help` and
  `gigg admin -h` print the usage.
- Flags may be written before or after the positional arguments.
- Each command runs in one transaction and is recorded in the activity log with the listener `cli`
  (no acting user).
- Names: usernames are 2-32 characters of lowercase letters, digits, `-` and `_`, starting with a
  letter. Workspace and project slugs are 2-32 lowercase letters, digits and `-`, starting with a
  letter. Project keys are 2-5 capital letters or digits, starting with a letter.

### user-add

```
gigg admin user-add <username> [-agent] [-admin] [-name "Display Name"]
```

Creates a person (default) or, with `-agent`, an agent. `-admin` makes an instance admin. Agents have
no password and can't log in to the UI; they use API keys. A new person has no password until
`passwd` sets one. A new non-admin user sees nothing until given access (`member` or `access`).

```sh
gigg admin user-add admin -admin -name "Admin"
gigg admin user-add sam -name "Sam"
gigg admin user-add scout -agent -name "Scout"
```

### passwd

```
gigg admin passwd <username> [-file F]
```

Sets a person's password (12 to 256 characters) and ends all their sessions. The password is read:

- from the terminal, twice, without echo, when stdin is a terminal;
- otherwise as one line from stdin;
- with `-file F`, as the first line of that file.

Only people (`human` users) have passwords.

```sh
gigg admin passwd admin
printf '%s\n' "$NEW_PASSWORD" | gigg admin passwd sam
```

### workspace-add

```
gigg admin workspace-add <slug> <name>
```

Creates a workspace with the default statuses (Backlog, To-Do, Doing, Waiting, Done, Cancelled). If a
user with the username `<slug>` exists, they become the workspace's owner (display only).

```sh
gigg admin workspace-add acme "Acme"
```

### workspace-owner

```
gigg admin workspace-owner <workspace> <username|none>
```

Sets or clears the workspace's owner: the user whose picture marks the workspace on its chips (board
filters, task header, tables, dashboard). Ownership grants no access.

```sh
gigg admin workspace-owner acme sam
```

### color

```
gigg admin color user|workspace <username|workspace> <#RRGGBB|none>
```

Sets or clears the colour of a user or a workspace. The colour tints everything that stands for them:
workspace chips and badges (board filters, cards, the task header, tables, the dashboard), avatar rings
and the initials of a user without a picture.
`none` goes back to the default colour derived from the name.

```sh
gigg admin color workspace acme "#E5484D"
gigg admin color user sam none
```

### member

```
gigg admin member <workspace> <username> <viewer|commenter|member|manager|none>
```

Sets the user's preset in one workspace, keeping any per-permission overrides they already have
there. `none` removes the user's own rule for that workspace; access that comes from an
all-workspaces rule or from groups remains. See [access.md](access.md).

```sh
gigg admin member acme sam manager
gigg admin member acme scout member
```

### access

```
gigg admin access <username> <workspace|all> <preset|none> [permission=allow|deny ...]
```

Sets the user's access rule for one workspace, or for all workspaces (`all`, which also covers
workspaces created later). The rule is replaced as a whole: preset and overrides. `none` with no
overrides removes the rule. Permission names are listed in [access.md](access.md#permissions).

```sh
# member in acme, plus the runner-only permissions
gigg admin access scout acme member always_run=allow allowed_tools=allow
# read-only everywhere
gigg admin access sam all viewer
# no preset, just one override
gigg admin access sam acme none pause_runs=allow
```

### allow

```
gigg admin allow <username> <workspace|all> <permission> [...]
```

Adds permissions to the user's rule for one workspace (or their `all` rule) and keeps the rest of it
(preset and other overrides); `access` replaces the rule as a whole. The user needs that rule first
(`member` or `access`).

```sh
# let the agent scout manage its own crons in its workspace
gigg admin allow scout scout crons
```

Note: groups and group rules have no CLI command; manage them from an admin's browser session (UI or
API).

### project-add

```
gigg admin project-add <workspace> <slug> <name> <KEY>
```

Creates a project (board). `<KEY>` prefixes its task keys (`WEB-1`, `WEB-2`, ...).

```sh
gigg admin project-add acme web "Website" WEB
```

### key-add

```
gigg admin key-add -user U [-workspace W] -scopes a,b [-listeners x,y] [-cidrs c] [-name N] [-days 365] [-out F]
```

Creates an API key acting as user `U`.

| Flag | Default | Meaning |
|---|---|---|
| `-user` | (required) | The user the key acts as. |
| `-workspace` | none | Bind the key to this workspace (the user must be able to see it). Without it: an admin user's key is an admin key (may carry `admin:*` scopes); another user's key is unbound and reaches that user's workspaces. |
| `-scopes` | (required) | Comma-separated scopes (see [api.md](api.md#scopes)). |
| `-listeners` | any | Comma-separated listener names the key works on (`main`, `internal`, ...). |
| `-cidrs` | any | Comma-separated networks (CIDR) the key works from. |
| `-name` | `cli` | Key name (1-64 characters). |
| `-days` | `365` | Lifetime in days (keys live at most 3 years). |
| `-out` | stdout | Write the token to this file (mode `0600`, replaced atomically). |

Project narrowing (`project_ids`) is not available here; use `POST /api/v1/keys` or `/me/keys`.

The token is shown only once. Without `-out` it is printed on stdout, but `gigg admin` refuses to print
it on a terminal: redirect stdout into the file that needs it.

```sh
gigg admin key-add -user scout -workspace acme -name runner \
  -scopes workspace:read,tasks:read,tasks:write,comments:read,comments:write,runs:write \
  -listeners internal -out /run/secrets/scout.key
```

### key-rotate

```
gigg admin key-rotate <key-id> [-out F]
```

Replaces a key's secret (and prefix); the key keeps its id, scopes and restrictions. The old token stops
working at once. The new token is written like `key-add` does. Revoked keys can't be rotated.

```sh
gigg admin key-rotate 12 -out /run/secrets/scout.key
```

### key-revoke

```
gigg admin key-revoke <username> <key-name>
```

Revokes every active key of that user with that name and reports how many.

```sh
gigg admin key-revoke scout runner
```

### status-rename

```
gigg admin status-rename <workspace> <category> <new name>
```

Renames the status of a category (`backlog`, `todo`, `doing`, `waiting`, `done`, `cancelled`) in the
workspace's status template and in every project of the workspace.

```sh
gigg admin status-rename acme waiting "Needs input"
```

### batch

```
gigg admin batch <file>
```

Runs one admin command per line:

- blank lines and lines starting with `#` are skipped;
- words are separated by spaces; `"double quotes"` group words (there is no escape character, so a
  value can't contain a double quote);
- each line runs in its own transaction; the batch stops at the first failing line, reports its
  line number and exits with status 1. Lines before it stay applied.

Interactive input doesn't fit a batch: give `passwd` a `-file`, and `key-add` / `key-rotate` an `-out`.

Example first setup (`setup.txt`):

```
# people and agents
user-add admin -admin -name "Admin"
passwd admin -file /run/secrets/admin-password
user-add sam -name "Sam"
user-add scout -agent -name "Scout"

# a workspace with one board
workspace-add acme "Acme"
project-add acme web "Website" WEB
member acme sam member
access scout acme member always_run=allow

# the runner's key, usable only on the internal listener
key-add -user scout -workspace acme -name runner -scopes workspace:read,tasks:read,tasks:write,comments:read,comments:write,runs:write -listeners internal -out /run/secrets/scout.key
```

```sh
gigg admin batch setup.txt
```
