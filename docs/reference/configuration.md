# Configuration

gigg is configured only through environment variables. There is no configuration file. The same
variables are read by `gigg serve`, `gigg healthcheck` and `gigg admin`. An invalid value makes
`gigg serve` and `gigg admin` exit with status 2, and `gigg healthcheck` with status 1.

## Environment variables

| Variable | Default | Meaning |
|---|---|---|
| `GIGG_DATA_DIR` | `/data` | Data directory (database, snapshots, uploaded files). Created with mode `0700` if missing. |
| `GIGG_DATABASE_URL` | (none) | A PostgreSQL connection URL (`postgres://user:password@host:5432/db?sslmode=require`). Unset: SQLite in the data directory. See [PostgreSQL](#postgresql). |
| `GIGG_DATABASE_URL_FILE` | (none) | A file holding that URL (a mounted secret), instead of `GIGG_DATABASE_URL`. Setting both is an error. |
| `GIGG_LISTEN` | `:8080` | Address of the **main** listener (`host:port` or `:port`). |
| `GIGG_TLS_CERT` | (none) | PEM certificate file for the main listener. Set both `GIGG_TLS_CERT` and `GIGG_TLS_KEY` for HTTPS, or neither for plain HTTP. With TLS, `GIGG_PUBLIC_URL` (if set) must start with `https://`. |
| `GIGG_TLS_KEY` | (none) | PEM private key file for the main listener. |
| `GIGG_LISTEN_<NAME>` | (none) | An extra, named listener (plain HTTP, API and MCP only). `<NAME>` is `[A-Z][A-Z0-9]{0,15}`; the listener's name is `<NAME>` in lower case (`GIGG_LISTEN_INTERNAL` makes the listener `internal`). `MAIN` is reserved. Any number may be set. |
| `GIGG_PUBLIC_URL` | (none) | The origin users reach the main listener at, `https://host[:port]` or `http://host[:port]`, no path. Its host name is added to the allowed hosts. It also picks the session cookie (see below) and is the `Origin` accepted for browser writes and for MCP requests that send an `Origin`, besides the exact host and port a request was sent to when that host is an allowed host (so the server can be reached by its IP and by a `.local` name in `GIGG_ALLOWED_HOSTS`). Without it, MCP requests that send an `Origin` are refused. Without it the server logs a warning and only `localhost`, `127.0.0.1`, `::1` and `GIGG_ALLOWED_HOSTS` are accepted. |
| `GIGG_ALLOWED_HOSTS` | (none) | Extra host names (comma- or space-separated, no ports) the server answers for. `localhost`, `127.0.0.1` and `::1` are always allowed. |
| `GIGG_TRUSTED_PROXIES` | (none) | Comma- or space-separated networks in CIDR notation (a single address is written `/32` or `/128`). When the TCP peer is in one of them, the client address is taken from `X-Forwarded-For` (see below), and the scheme the browser used from `X-Forwarded-Proto` (for the origin check on names other than the public URL). |
| `GIGG_BACKUP_AT` | `03:30` | Local time (`HH:MM`, 24 h) of the nightly database snapshot, or `off` to disable it. |
| `GIGG_DEFAULT_TZ` | `UTC` | IANA time zone for schedules and recurring gigs that name none and whose creator has no time zone preference (Profile). Also the default of `GIGG_BACKUP_TZ`. The zone database is built into the binary. |
| `GIGG_BACKUP_TZ` | `GIGG_DEFAULT_TZ` | IANA time zone `GIGG_BACKUP_AT` is read in. |
| `GIGG_STALE_RUN_HOURS` | `6` | A run that sends no heartbeat (start or progress) for this many hours (1-168) is closed as `interrupted` by the server's housekeeping. |
| `GIGG_BACKUP_KEEP` | `7` | How many nightly snapshots to keep (1-365). |
| `GIGG_REMOTE_FONTS` | on | The 20 fonts the theme editor offers are bundled, so pages never contact Google on their own; another Google Fonts family loads only after someone presses **Load from Google Fonts** on the Themes page (kept per browser). `off` removes Google Fonts (`fonts.googleapis.com`, `fonts.gstatic.com`) from the Content-Security-Policy and hides that button. Any other value leaves them allowed. |
| `GIGG_SOURCE_URL` | `https://github.com/0x2alx/gigg` | Where users get this server's source code, linked at the bottom of the profile page. gigg is under the AGPL-3.0: if you run a changed version for others, point this at your changed source. |

Time zones: a schedule always stores its own zone. When a request names none, gigg uses the creator's
time zone preference (set in Profile, stored as `timezone` in the user's preferences), else
`GIGG_DEFAULT_TZ`. Set `GIGG_DEFAULT_TZ` to your team's zone; the host's zone is not used.

## Listeners

gigg can serve several listeners from one process. Every listener serves the same REST API
(`/api/v1/...`), the MCP endpoint (`/mcp`) and `GET /healthz`.

| | Main listener | Extra listeners (`GIGG_LISTEN_<NAME>`) |
|---|---|---|
| Name | `main` | lower-cased `<NAME>` |
| Address | `GIGG_LISTEN` (default `:8080`) | the variable's value |
| TLS | yes if `GIGG_TLS_CERT`/`GIGG_TLS_KEY` are set | never (plain HTTP) |
| Web UI | yes | no (404 `not_found`) |
| Browser login and session cookies | yes | no: cookies are ignored, `POST /auth/login` answers 403 |
| Bearer API keys | yes | yes |
| MCP (`/mcp`, bearer keys only) | yes | yes |

A typical setup is HTTPS on the main listener for people, plus a plain-HTTP listener bound to
loopback (or a private network) for local agents and runners. API keys can be restricted to
listeners by name (`allowed_listeners`), so a runner's key can be made usable only on the internal
listener.

Each listener refuses requests whose `Host` header is not an allowed host (status 421, code
`bad_host`), which protects against DNS rebinding. Clients of an extra listener must therefore use a
host name in `GIGG_ALLOWED_HOSTS` (or `localhost` / `127.0.0.1`).

Server limits: request headers must arrive within 10 s and are capped at 64 KiB; idle keep-alive
connections close after 120 s; on SIGTERM/SIGINT the server stops accepting connections and waits up
to 10 s for requests to finish.

## TLS

- TLS 1.2 or newer, HTTP/2 and HTTP/1.1.
- The certificate files are checked for changes at most every 10 seconds (modification time and
  size) and reloaded without a restart. If a reload fails, the last good certificate stays in use.
  If the files can't be read at start, the server refuses to start.
- With TLS on the main listener, responses carry `Strict-Transport-Security: max-age=31536000`.
- Extra listeners never use TLS.

## Behind a reverse proxy

Run the main listener as plain HTTP and let the proxy terminate TLS:

```sh
GIGG_LISTEN=:8080
GIGG_PUBLIC_URL=https://board.example.com
GIGG_TRUSTED_PROXIES=10.0.0.5/32
```

- The proxy must forward a `Host` that gigg accepts (the public host name, or one listed in
  `GIGG_ALLOWED_HOSTS`) and must pass the browser's `Origin` header through.
- With an `https://` public URL the session cookie is `__Host-gigg_session` with the `Secure` flag,
  even though gigg itself speaks plain HTTP to the proxy. With no public URL it is the same when gigg
  terminates TLS itself. With an `http://` public URL, or none and no TLS, it is `gigg_session`
  without `Secure`.
- gigg sends HSTS only when it terminates TLS itself; configure HSTS on the proxy otherwise.
- Event streams (`GET /api/v1/workspaces/{ws}/events`) send `X-Accel-Buffering: no`; make sure the
  proxy doesn't buffer `text/event-stream` responses.

### Client address

The client address is used for API key network restrictions (`allowed_cidrs`), rate limits, session
records and the audit log. If the TCP peer is not in `GIGG_TRUSTED_PROXIES`, the peer address is used.
If it is, gigg reads every `X-Forwarded-For` header line, walks the hops from right to left and takes
the first address that is not a trusted proxy.

Behind a proxy that isn't listed, every request seems to come from the proxy, so all users share
one address's rate limits.

### Rate limits

Counted per minute, in memory (a restart resets them):

- **Sign-in:** 5 attempts per address and user name, 30 per address, and 20 per user name from any
  address. The last one means someone who knows a user name can keep that user from signing in for
  as long as they keep trying; that user's existing sessions and API keys keep working.
- **Change password:** 5 per user.
- **API keys:** 300 requests per key. An address that sends 30 invalid keys gets 429 for every key
  request for the rest of that minute, before any lookup.

## Security headers

Every response carries a strict Content-Security-Policy (`default-src 'none'`, scripts, styles,
images and connections from the same origin only; Google Fonts unless `GIGG_REMOTE_FONTS=off`;
`frame-ancestors 'none'`), plus `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`,
`Cross-Origin-Opener-Policy: same-origin` and `Cross-Origin-Resource-Policy: same-origin`. API
responses carry `Cache-Control: no-store`. Every response has an `X-Request-Id` header.

gigg registers no service worker. It still serves `/sw.js` and `/service-worker.js`: each removes
itself and the caches of whatever app registered it. A browser that kept another app's service worker
for the same address (for example a board that ran there before gigg) fetches that script when it
checks for updates, so it cleans itself up on its own.

## Logs

The server logs JSON lines to standard output: one line per request (listener, method, path,
status, bytes, duration, request id; never headers or query strings), startup, migrations, snapshots
and background work.

## Health check

`GET /healthz` on any listener, without authentication (the `Host` check still applies):

```json
{"status": "ok", "version": "1.2.3", "listener": "main", "schema": 12}
```

It answers 503 with `"status": "error"` when the database doesn't respond. `gigg healthcheck` calls it
for container health checks (see [admin-cli.md](admin-cli.md#gigg-healthcheck)).

## Data directory

```
<GIGG_DATA_DIR>/
  gigg.db              SQLite database (WAL mode)
  gigg.db-wal          write-ahead log (while running)
  gigg.db-shm          shared-memory index (while running)
  backups/
    nightly-YYYY-MM-DD.db   nightly snapshots (newest GIGG_BACKUP_KEEP kept)
    pre-NNNN.db             snapshot taken before migration NNNN (never pruned)
  files/
    ab/cd/<sha256>     uploaded files, content-addressed (user pictures, attachments and their previews)
```

All three directories are created with mode `0700` on start. The container image runs as UID/GID
`65532` and uses `/data`.

## PostgreSQL

SQLite is the default and needs nothing. To keep the data in PostgreSQL
instead (tested with PostgreSQL 18), set `GIGG_DATABASE_URL` (or `GIGG_DATABASE_URL_FILE`) to a `postgres://` URL.

- The database must exist; gigg creates its tables on first start. The role needs to create tables
  in its schema, and the `citext` extension must be available (it ships with PostgreSQL). If the
  role can't create extensions, run `CREATE EXTENSION citext;` once as a superuser first.
- Uploaded files still live in `<GIGG_DATA_DIR>/files/`: keep the data directory on a volume.
- gigg writes through a single connection and reads through four, as with SQLite.
- The nightly snapshot and the `pre-NNNN.db` snapshots are SQLite only. Back up PostgreSQL with its
  own tools (`pg_dump`, or your provider's backups) together with `files/`.
- A new database starts at the PostgreSQL baseline (a migration number); every later migration
  runs as with SQLite.
- There is no tool to move an existing SQLite board to PostgreSQL yet: choose before you start.
- The `starting` log line names the database in use (`sqlite` or `postgres`); the password is
  never logged.

## Background work

Once a minute the server:

- creates the gigs of recurring gigs that are due, and marks windowed gigs whose window closed
  untouched as missed;
- deletes stored idempotency responses older than 24 hours;
- closes runs that sent no heartbeat for `GIGG_STALE_RUN_HOURS` as `interrupted`;
- writes the nightly snapshot if it is due;
- once a day, removes stored files nothing has used for longer than `GIGG_BACKUP_KEEP` days plus one
  (removed attachments, replaced pictures), so every snapshot still kept finds its files. Files in
  `files/` that no row knows about (a crash between writing a file and saving its row) go after the
  same time.

The server starts no processes and opens no outbound connections.

## Backups

### Built-in nightly snapshots

Once the local time (in `GIGG_BACKUP_TZ`) has passed `GIGG_BACKUP_AT`, the server writes
`backups/nightly-YYYY-MM-DD.db` for that date if it doesn't exist yet. A server that was down at that
time catches up when it starts. The snapshot is a consistent copy made with SQLite `VACUUM INTO`,
written under a temporary name (`.nightly-YYYY-MM-DD.db.tmp`), synced and renamed when complete. After
each new snapshot the oldest `nightly-*.db` files beyond `GIGG_BACKUP_KEEP` are deleted.

### What to copy

- Copy `backups/` (the newest `nightly-*.db`, and `pre-*.db` if you want them) and `files/` with any
  file-level backup tool. Snapshot files are complete once they have their final name.
- Do not copy the live `gigg.db` (with its `-wal` and `-shm`) at file level while the server runs: it
  is not guaranteed to be consistent. Either stop the server first and copy the whole data directory,
  or rely on the snapshots.
- Note: there is no command for an on-demand snapshot. If you need one outside the nightly schedule,
  stop the server and copy the data directory.

### Restoring

1. Stop the server.
2. Replace `gigg.db` with the snapshot file, and delete `gigg.db-wal` and `gigg.db-shm` if present.
3. Restore `files/` from the same backup.
4. Start the server. If the snapshot has an older schema, pending migrations run (see below).

## Upgrades and migrations

The schema is versioned by numbered migrations (`internal/store/migrations/NNNN_name.sql`) embedded in
the binary, recorded in the `schema_migrations` table.

- Migrations run **automatically** whenever the database is opened: by `gigg serve` and also by every
  `gigg admin` command. Running `gigg admin` from a newer binary therefore upgrades the database.
- Before the first pending migration on a database that already has data, gigg writes a snapshot
  `backups/pre-NNNN.db` (NNNN = that migration's number) with `VACUUM INTO`. It is written once: a
  restart never overwrites it. These snapshots are not pruned; delete them yourself when no longer
  needed. A fresh, empty database gets no snapshot.
- Each migration runs in its own transaction. If one fails, it is rolled back and the server refuses
  to start, so it never runs on a half-migrated schema.
- If the database schema is newer than the binary, the binary refuses to start. To downgrade,
  restore the matching `pre-NNNN.db` snapshot.
- `GET /healthz` reports the current schema version.

Upgrade procedure: stop the old version, start the new one, check the log for `migration applied`
lines and `/healthz` for the new schema number.

## Container example

The image is built from `Containerfile`: a static binary on `scratch` (no shell), running as
`65532:65532`, with `ENTRYPOINT ["/gigg"]`, `CMD ["serve"]`, `GIGG_DATA_DIR=/data`, and empty files
`/run/gigg/tls.crt` and `/run/gigg/tls.key` as mount points (the root file system can be read-only).

```sh
podman run -d --name gigg \
  -e GIGG_LISTEN=:8443 \
  -e GIGG_TLS_CERT=/run/gigg/tls.crt -e GIGG_TLS_KEY=/run/gigg/tls.key \
  -e GIGG_PUBLIC_URL=https://board.example.com:8443 \
  -e GIGG_LISTEN_INTERNAL=:8080 -e GIGG_ALLOWED_HOSTS=gigg \
  -v ./tls.crt:/run/gigg/tls.crt:ro -v ./tls.key:/run/gigg/tls.key:ro \
  -v gigg:/data -p 8443:8443 -p 127.0.0.1:8080:8080 \
  --read-only --tmpfs /tmp --cap-drop ALL --security-opt no-new-privileges \
  --health-cmd '["/gigg","healthcheck"]' --health-interval 30s \
  localhost/gigg:latest
```

A Quadlet unit for rootless podman is in `deploy/gigg.container.example`. Health checks must use the
exec form (`["/gigg","healthcheck"]`): the image has no shell.
