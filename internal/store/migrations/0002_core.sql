-- The core schema. Times are UTC unix milliseconds.

CREATE TABLE users (
  id                 INTEGER PRIMARY KEY,
  username           TEXT NOT NULL UNIQUE COLLATE NOCASE CHECK (username GLOB '[a-z]*' AND length(username) BETWEEN 2 AND 32),
  kind               TEXT NOT NULL CHECK (kind IN ('human', 'agent')),
  display_name       TEXT NOT NULL DEFAULT '' CHECK (length(display_name) <= 64),
  description        TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 200),
  color              TEXT CHECK (color IS NULL OR color GLOB '#[0-9A-F][0-9A-F][0-9A-F][0-9A-F][0-9A-F][0-9A-F]'),
  avatar_sha256      TEXT,
  password_hash      TEXT,
  is_admin           INTEGER NOT NULL DEFAULT 0 CHECK (is_admin IN (0, 1)),
  disabled_at        INTEGER,
  preferences        TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(preferences)),
  theme_selected_id  TEXT,
  theme_match_system INTEGER NOT NULL DEFAULT 0 CHECK (theme_match_system IN (0, 1)),
  last_login_at      INTEGER,
  created_at         INTEGER NOT NULL,
  version            INTEGER NOT NULL DEFAULT 1,
  CHECK (kind = 'human' OR password_hash IS NULL)
) STRICT;

CREATE TABLE user_themes (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  theme_id   TEXT NOT NULL,
  theme      TEXT NOT NULL CHECK (json_valid(theme)),
  updated_at INTEGER NOT NULL,
  UNIQUE (user_id, theme_id)
) STRICT;

CREATE TABLE sessions (
  id           INTEGER PRIMARY KEY,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash   BLOB NOT NULL UNIQUE,
  created_at   INTEGER NOT NULL,
  last_seen_at INTEGER NOT NULL,
  expires_at   INTEGER NOT NULL,
  ip           TEXT NOT NULL DEFAULT '',
  user_agent   TEXT NOT NULL DEFAULT '',
  revoked_at   INTEGER
) STRICT;
CREATE INDEX sessions_user ON sessions(user_id);

CREATE TABLE workspaces (
  id              INTEGER PRIMARY KEY,
  slug            TEXT NOT NULL UNIQUE CHECK (slug GLOB '[a-z]*' AND length(slug) BETWEEN 2 AND 32),
  name            TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 64),
  color           TEXT,
  description_md  TEXT NOT NULL DEFAULT '' CHECK (length(description_md) <= 65536),
  status_template TEXT NOT NULL CHECK (json_valid(status_template)),
  archived_at     INTEGER,
  created_at      INTEGER NOT NULL,
  version         INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE TABLE memberships (
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role         TEXT NOT NULL CHECK (role IN ('viewer', 'commenter', 'member', 'manager')),
  created_at   INTEGER NOT NULL,
  PRIMARY KEY (workspace_id, user_id)
) STRICT;
CREATE INDEX memberships_user ON memberships(user_id);

CREATE TABLE projects (
  id               INTEGER PRIMARY KEY,
  workspace_id     INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  slug             TEXT NOT NULL CHECK (slug GLOB '[a-z]*' AND length(slug) BETWEEN 2 AND 32),
  name             TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 64),
  key              TEXT NOT NULL CHECK (key GLOB '[A-Z]*' AND length(key) BETWEEN 2 AND 5),
  color            TEXT,
  description_md   TEXT NOT NULL DEFAULT '' CHECK (length(description_md) <= 65536),
  position         REAL NOT NULL DEFAULT 0,
  next_task_number INTEGER NOT NULL DEFAULT 1,
  archived_at      INTEGER,
  deleted_at       INTEGER,
  created_at       INTEGER NOT NULL,
  version          INTEGER NOT NULL DEFAULT 1,
  UNIQUE (workspace_id, slug),
  UNIQUE (workspace_id, key)
) STRICT;

CREATE TABLE api_keys (
  id                 INTEGER PRIMARY KEY,
  name               TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 64),
  prefix             TEXT NOT NULL UNIQUE,
  secret_hash        BLOB NOT NULL,
  user_id            INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  workspace_id       INTEGER REFERENCES workspaces(id) ON DELETE CASCADE, -- NULL = admin key
  scopes             TEXT NOT NULL CHECK (json_valid(scopes)),
  allowed_listeners  TEXT CHECK (allowed_listeners IS NULL OR json_valid(allowed_listeners)),
  allowed_cidrs      TEXT CHECK (allowed_cidrs IS NULL OR json_valid(allowed_cidrs)),
  expires_at         INTEGER NOT NULL,
  created_at         INTEGER NOT NULL,
  created_by_user    INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_by_key     INTEGER REFERENCES api_keys(id) ON DELETE SET NULL,
  last_used_at       INTEGER,
  last_used_listener TEXT,
  last_used_ip       TEXT,
  revoked_at         INTEGER,
  version            INTEGER NOT NULL DEFAULT 1
) STRICT;
CREATE INDEX api_keys_user ON api_keys(user_id);

CREATE TABLE api_key_projects (
  key_id     INTEGER NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  PRIMARY KEY (key_id, project_id)
) STRICT;

CREATE TABLE statuses (
  id           INTEGER PRIMARY KEY,
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  project_id   INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name         TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 40),
  category     TEXT NOT NULL CHECK (category IN ('backlog', 'todo', 'doing', 'waiting', 'done', 'cancelled')),
  color        TEXT,
  position     REAL NOT NULL,
  wip_limit    INTEGER,
  is_default   INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0, 1)),
  version      INTEGER NOT NULL DEFAULT 1
) STRICT;
CREATE INDEX statuses_project ON statuses(project_id, position);

CREATE TABLE tasks (
  id                  INTEGER PRIMARY KEY,
  workspace_id        INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  project_id          INTEGER NOT NULL REFERENCES projects(id),
  number              INTEGER NOT NULL,
  title               TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 250),
  description_md      TEXT NOT NULL DEFAULT '' CHECK (length(description_md) <= 65536),
  status_id           INTEGER NOT NULL REFERENCES statuses(id),
  position            REAL NOT NULL,
  priority            INTEGER NOT NULL DEFAULT 0 CHECK (priority BETWEEN 0 AND 4),
  color               TEXT,
  start_at            INTEGER,
  due_at              INTEGER,
  done_at             INTEGER,
  parent_id           INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  cloned_from_id      INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  created_by          INTEGER NOT NULL REFERENCES users(id),
  created_via_key_id  INTEGER REFERENCES api_keys(id) ON DELETE SET NULL,
  created_at          INTEGER NOT NULL,
  updated_at          INTEGER NOT NULL,
  session_id          TEXT,
  last_run_id         INTEGER,
  resume_requested_at INTEGER,
  cost_usd_total      REAL NOT NULL DEFAULT 0,
  comment_count       INTEGER NOT NULL DEFAULT 0,
  deleted_at          INTEGER,
  deleted_by          INTEGER REFERENCES users(id),
  version             INTEGER NOT NULL DEFAULT 1,
  UNIQUE (project_id, number)
) STRICT;
CREATE INDEX tasks_board ON tasks(project_id, status_id, position);
CREATE INDEX tasks_ws ON tasks(workspace_id, deleted_at);

CREATE TABLE task_aliases (
  task_id      INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  project_key  TEXT NOT NULL,
  number       INTEGER NOT NULL,
  PRIMARY KEY (workspace_id, project_key, number)
) STRICT;

CREATE TABLE task_assignees (
  task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  PRIMARY KEY (task_id, user_id)
) STRICT;

CREATE TABLE labels (
  id           INTEGER PRIMARY KEY,
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  name         TEXT NOT NULL COLLATE NOCASE CHECK (length(name) BETWEEN 1 AND 40),
  color        TEXT,
  description  TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 200),
  version      INTEGER NOT NULL DEFAULT 1,
  UNIQUE (workspace_id, name)
) STRICT;

CREATE TABLE task_labels (
  task_id  INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  label_id INTEGER NOT NULL REFERENCES labels(id) ON DELETE CASCADE,
  PRIMARY KEY (task_id, label_id)
) STRICT;

CREATE TABLE runs (
  id                 INTEGER PRIMARY KEY,
  workspace_id       INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  task_id            INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  worker_user_id     INTEGER NOT NULL REFERENCES users(id),
  key_id             INTEGER REFERENCES api_keys(id) ON DELETE SET NULL,
  session_id         TEXT,
  mode               TEXT NOT NULL DEFAULT '' CHECK (length(mode) <= 32),
  attempt            INTEGER NOT NULL DEFAULT 1,
  status             TEXT NOT NULL CHECK (status IN ('running', 'done', 'needs_human', 'error', 'timeout', 'interrupted')),
  started_at         INTEGER NOT NULL,
  heartbeat_at       INTEGER NOT NULL,
  ended_at           INTEGER,
  duration_ms        INTEGER,
  cost_usd           REAL,
  num_turns          INTEGER,
  tokens_in          INTEGER,
  tokens_out         INTEGER,
  tokens_cache_read  INTEGER,
  tokens_cache_write INTEGER,
  model              TEXT,
  exit_code          INTEGER,
  error_reason       TEXT,
  transcript_path    TEXT,
  log_path           TEXT,
  report_comment_id  INTEGER,
  progress_pct       INTEGER,
  progress_note      TEXT,
  version            INTEGER NOT NULL DEFAULT 1
) STRICT;
CREATE INDEX runs_task ON runs(task_id, id);
CREATE INDEX runs_ws ON runs(workspace_id, status, started_at);

CREATE TABLE run_events (
  id     INTEGER PRIMARY KEY,
  run_id INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  at     INTEGER NOT NULL,
  kind   TEXT NOT NULL CHECK (kind IN ('progress', 'note', 'artifact', 'state')),
  text   TEXT NOT NULL DEFAULT '' CHECK (length(text) <= 2048),
  pct    INTEGER
) STRICT;
CREATE INDEX run_events_run ON run_events(run_id, id);

CREATE TABLE comments (
  id           INTEGER PRIMARY KEY,
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  task_id      INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  author_id    INTEGER NOT NULL REFERENCES users(id),
  key_id       INTEGER REFERENCES api_keys(id) ON DELETE SET NULL,
  kind         TEXT NOT NULL CHECK (kind IN ('comment', 'report', 'question', 'resume_request', 'system')),
  body_md      TEXT NOT NULL CHECK (length(body_md) BETWEEN 1 AND 65536),
  run_id       INTEGER REFERENCES runs(id) ON DELETE SET NULL,
  created_at   INTEGER NOT NULL,
  edited_at    INTEGER,
  deleted_at   INTEGER,
  version      INTEGER NOT NULL DEFAULT 1
) STRICT;
CREATE INDEX comments_task ON comments(task_id, id);

CREATE TABLE blobs (
  sha256   TEXT PRIMARY KEY,
  size     INTEGER NOT NULL,
  refcount INTEGER NOT NULL DEFAULT 0
) STRICT;

CREATE TABLE artifacts (
  id           INTEGER PRIMARY KEY,
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  task_id      INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  run_id       INTEGER REFERENCES runs(id) ON DELETE SET NULL,
  kind         TEXT NOT NULL CHECK (kind IN ('file', 'path', 'commit', 'url')),
  label        TEXT NOT NULL DEFAULT '' CHECK (length(label) <= 200),
  value        TEXT NOT NULL DEFAULT '' CHECK (length(value) <= 2048),
  blob_sha256  TEXT REFERENCES blobs(sha256),
  filename     TEXT,
  mime         TEXT,
  size         INTEGER,
  created_by   INTEGER NOT NULL REFERENCES users(id),
  created_at   INTEGER NOT NULL,
  deleted_at   INTEGER
) STRICT;
CREATE INDEX artifacts_task ON artifacts(task_id);

-- The audit log, the per-task history and the event stream. Append-only.
CREATE TABLE activity (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  at            INTEGER NOT NULL,
  workspace_id  INTEGER,
  project_id    INTEGER,
  actor_user_id INTEGER,
  key_id        INTEGER,
  listener      TEXT NOT NULL DEFAULT '',
  ip            TEXT NOT NULL DEFAULT '',
  request_id    TEXT NOT NULL DEFAULT '',
  action        TEXT NOT NULL,
  entity_type   TEXT NOT NULL DEFAULT '',
  entity_id     INTEGER,
  data          TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(data))
) STRICT;
CREATE INDEX activity_ws ON activity(workspace_id, id);
CREATE INDEX activity_entity ON activity(entity_type, entity_id, id);
CREATE TRIGGER activity_no_update BEFORE UPDATE ON activity BEGIN SELECT RAISE(ABORT, 'activity is append-only'); END;
CREATE TRIGGER activity_no_delete BEFORE DELETE ON activity BEGIN SELECT RAISE(ABORT, 'activity is append-only'); END;

CREATE TABLE idempotency_keys (
  principal    TEXT NOT NULL,
  key          TEXT NOT NULL,
  request_hash TEXT NOT NULL,
  status       INTEGER NOT NULL,
  response     BLOB NOT NULL,
  created_at   INTEGER NOT NULL,
  PRIMARY KEY (principal, key)
) STRICT;
