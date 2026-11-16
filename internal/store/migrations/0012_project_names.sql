-- gigg:foreign-keys-off
-- A deleted project's slug and key are free again: they are unique among live projects only. (A
-- deleted project can't be restored, so nothing needs them back.) SQLite can't drop a table's UNIQUE
-- constraint, so the table is rebuilt (12-step procedure; foreign keys are off for this migration and
-- checked before commit) with partial unique indexes instead.
CREATE TABLE projects_new (
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
  version          INTEGER NOT NULL DEFAULT 1
) STRICT;
INSERT INTO projects_new SELECT * FROM projects;
DROP TABLE projects;
ALTER TABLE projects_new RENAME TO projects;
CREATE UNIQUE INDEX projects_slug ON projects(workspace_id, slug) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX projects_key ON projects(workspace_id, key) WHERE deleted_at IS NULL;
