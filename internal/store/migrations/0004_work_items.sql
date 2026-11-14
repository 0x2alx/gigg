-- Relations, checklists, custom fields, templates, archive, saved filters and notifications.

-- "from blocks to": `to` can't be claimed while `from` is open. 'relates' is informational.
CREATE TABLE task_relations (
  workspace_id  INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  from_task_id  INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  to_task_id    INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  kind          TEXT NOT NULL CHECK (kind IN ('blocks', 'relates')),
  created_by    INTEGER NOT NULL REFERENCES users(id),
  created_at    INTEGER NOT NULL,
  PRIMARY KEY (from_task_id, to_task_id, kind),
  CHECK (from_task_id != to_task_id)
) STRICT;
CREATE INDEX task_relations_to ON task_relations(to_task_id, kind);

CREATE TABLE checklist_items (
  id            INTEGER PRIMARY KEY,
  workspace_id  INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  task_id       INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  position      REAL NOT NULL,
  text          TEXT NOT NULL CHECK (length(text) BETWEEN 1 AND 500),
  done_at       INTEGER,
  done_by       INTEGER REFERENCES users(id),
  created_at    INTEGER NOT NULL
) STRICT;
CREATE INDEX checklist_task ON checklist_items(task_id, position);

-- Workspace-defined fields shown on every task of the workspace.
CREATE TABLE custom_fields (
  id            INTEGER PRIMARY KEY,
  workspace_id  INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  name          TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 60),
  kind          TEXT NOT NULL CHECK (kind IN ('text', 'number', 'date', 'select', 'checkbox', 'url')),
  options       TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(options)), -- select: the choices
  position      REAL NOT NULL,
  created_at    INTEGER NOT NULL,
  deleted_at    INTEGER,
  version       INTEGER NOT NULL DEFAULT 1
) STRICT;
CREATE UNIQUE INDEX custom_fields_name ON custom_fields(workspace_id, name) WHERE deleted_at IS NULL;

CREATE TABLE task_custom_values (
  task_id   INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  field_id  INTEGER NOT NULL REFERENCES custom_fields(id) ON DELETE CASCADE,
  value     TEXT NOT NULL CHECK (json_valid(value)),
  PRIMARY KEY (task_id, field_id)
) STRICT;

CREATE TABLE task_templates (
  id              INTEGER PRIMARY KEY,
  workspace_id    INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  project_id      INTEGER REFERENCES projects(id),  -- null = any project of the workspace
  name            TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
  title           TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 250),
  description_md  TEXT NOT NULL DEFAULT '' CHECK (length(description_md) <= 65536),
  priority        INTEGER NOT NULL DEFAULT 0 CHECK (priority BETWEEN 0 AND 4),
  color           TEXT,
  labels          TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(labels)),
  assignees       TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(assignees)),
  checklist       TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(checklist)),
  custom          TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(custom)),
  created_by      INTEGER NOT NULL REFERENCES users(id),
  created_at      INTEGER NOT NULL,
  deleted_at      INTEGER,
  version         INTEGER NOT NULL DEFAULT 1
) STRICT;
CREATE INDEX task_templates_ws ON task_templates(workspace_id) WHERE deleted_at IS NULL;

-- Archived tasks leave the board and lists (unless asked for) but are not trash.
ALTER TABLE tasks ADD COLUMN archived_at INTEGER;
CREATE INDEX tasks_archived ON tasks(project_id, archived_at);

-- Per-user saved views: a name plus the list query.
CREATE TABLE saved_filters (
  id            INTEGER PRIMARY KEY,
  user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  workspace_id  INTEGER REFERENCES workspaces(id) ON DELETE CASCADE, -- null = across workspaces
  name          TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 60),
  query         TEXT NOT NULL CHECK (json_valid(query)),
  position      REAL NOT NULL,
  created_at    INTEGER NOT NULL
) STRICT;
CREATE INDEX saved_filters_user ON saved_filters(user_id, position);

-- The bell: mentions, assignments, questions, missed windows.
CREATE TABLE notifications (
  id            INTEGER PRIMARY KEY,
  user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  workspace_id  INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  task_id       INTEGER REFERENCES tasks(id) ON DELETE CASCADE,
  comment_id    INTEGER,
  kind          TEXT NOT NULL CHECK (kind IN ('mention', 'assigned', 'question', 'missed', 'comment')),
  actor_user_id INTEGER REFERENCES users(id),
  created_at    INTEGER NOT NULL,
  read_at       INTEGER
) STRICT;
CREATE INDEX notifications_user ON notifications(user_id, read_at, id);

-- Groups and per-user/per-group capabilities (feature permissions on top of workspace roles and
-- key scopes). Resolution: the user's own setting, else allowed if any of their groups allows it,
-- else denied if a group denies it, else the default for their kind (human/agent). Admins: all.
CREATE TABLE groups (
  id           INTEGER PRIMARY KEY,
  name         TEXT NOT NULL UNIQUE CHECK (length(name) BETWEEN 1 AND 60),
  description  TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 500),
  created_at   INTEGER NOT NULL,
  version      INTEGER NOT NULL DEFAULT 1
) STRICT;
CREATE TABLE group_members (
  group_id  INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  PRIMARY KEY (group_id, user_id)
) STRICT;
CREATE INDEX group_members_user ON group_members(user_id);
CREATE TABLE group_permissions (
  group_id    INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  capability  TEXT NOT NULL,
  allowed     INTEGER NOT NULL CHECK (allowed IN (0, 1)),
  PRIMARY KEY (group_id, capability)
) STRICT;
CREATE TABLE user_permissions (
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  capability  TEXT NOT NULL,
  allowed     INTEGER NOT NULL CHECK (allowed IN (0, 1)),
  PRIMARY KEY (user_id, capability)
) STRICT;
