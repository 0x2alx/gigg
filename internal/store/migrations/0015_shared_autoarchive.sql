-- gigg:foreign-keys-off
-- Shared templates and schedules, and auto-archive.
--
-- Shared: a gig template, an errand template or a schedule with no workspace (workspace_id NULL) belongs
-- to the whole instance: admins make and change it, every workspace sees it and pairs it with its own
-- (or other shared) parts in its recurring gigs and errands. A shared gig template is for any project
-- and holds no custom values (both are a workspace's own). The three tables are rebuilt to let
-- workspace_id be NULL (SQLite's 12-step procedure; foreign keys are off for this migration and checked
-- before commit).
--
-- A recurring gig gets its own assignees (NULL: its template's), so workspaces sharing a template each
-- hand its gigs to their own people and agents.
--
-- Auto-archive: a gig (and a template, for the gigs it makes) can archive itself this many minutes after
-- it was done or cancelled (housekeeping does it). NULL: never.
--
-- Run timeouts: a gig (and a template) can name how long an agent's runner lets a run go (5-300
-- minutes; NULL: the agent's default, 60). Below the 6 h after which the server closes a silent run.
-- Errand timeouts go up to 12 h. Setting either needs the run_timeout permission; the executors cap
-- both at their host's ceiling.

CREATE TABLE gig_templates_new (
  id              INTEGER PRIMARY KEY,
  workspace_id    INTEGER REFERENCES workspaces(id) ON DELETE CASCADE, -- NULL: shared by every workspace
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
  version         INTEGER NOT NULL DEFAULT 1,
  model           TEXT NOT NULL DEFAULT '' CHECK (length(model) <= 64),
  effort          TEXT NOT NULL DEFAULT '' CHECK (effort IN ('', 'low', 'medium', 'high', 'xhigh', 'max')),
  always_run      INTEGER NOT NULL DEFAULT 0 CHECK (always_run IN (0, 1)),
  allowed_tools   TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(allowed_tools)),
  subgigs         TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(subgigs)),
  auto_archive_minutes INTEGER CHECK (auto_archive_minutes IS NULL OR auto_archive_minutes BETWEEN 5 AND 527040),
  run_timeout_minutes INTEGER CHECK (run_timeout_minutes IS NULL OR run_timeout_minutes BETWEEN 5 AND 300),
  CHECK (workspace_id IS NOT NULL OR (project_id IS NULL AND custom = '{}'))
) STRICT;
INSERT INTO gig_templates_new (id, workspace_id, project_id, name, title, description_md, priority, color, labels, assignees, checklist, custom,
  created_by, created_at, deleted_at, version, model, effort, always_run, allowed_tools, subgigs)
  SELECT id, workspace_id, project_id, name, title, description_md, priority, color, labels, assignees, checklist, custom,
  created_by, created_at, deleted_at, version, model, effort, always_run, allowed_tools, subgigs FROM gig_templates;
DROP TABLE gig_templates;
ALTER TABLE gig_templates_new RENAME TO gig_templates;
CREATE INDEX gig_templates_ws ON gig_templates(workspace_id) WHERE deleted_at IS NULL;

CREATE TABLE errand_templates_new (
  id              INTEGER PRIMARY KEY,
  workspace_id    INTEGER REFERENCES workspaces(id) ON DELETE CASCADE, -- NULL: shared by every workspace
  name            TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
  description_md  TEXT NOT NULL DEFAULT '' CHECK (length(description_md) <= 65536),
  command         TEXT NOT NULL CHECK (length(command) BETWEEN 1 AND 16384),
  timeout_s       INTEGER NOT NULL DEFAULT 300 CHECK (timeout_s BETWEEN 10 AND 43200),
  output          TEXT NOT NULL DEFAULT 'gigg' CHECK (output IN ('gigg', 'host')),
  unsandboxed     INTEGER NOT NULL DEFAULT 0 CHECK (unsandboxed IN (0, 1)),
  command_set_by  INTEGER NOT NULL REFERENCES users(id),
  command_set_at  INTEGER NOT NULL,
  unsandboxed_set_by INTEGER REFERENCES users(id),
  created_by      INTEGER NOT NULL REFERENCES users(id),
  created_at      INTEGER NOT NULL,
  deleted_at      INTEGER,
  version         INTEGER NOT NULL DEFAULT 1
) STRICT;
INSERT INTO errand_templates_new (id, workspace_id, name, description_md, command, timeout_s, output, unsandboxed, command_set_by, command_set_at,
  unsandboxed_set_by, created_by, created_at, deleted_at, version)
  SELECT id, workspace_id, name, description_md, command, timeout_s, output, unsandboxed, command_set_by, command_set_at,
  unsandboxed_set_by, created_by, created_at, deleted_at, version FROM errand_templates;
DROP TABLE errand_templates;
ALTER TABLE errand_templates_new RENAME TO errand_templates;
CREATE INDEX errand_templates_ws ON errand_templates(workspace_id) WHERE deleted_at IS NULL;

CREATE TABLE schedules_new (
  id              INTEGER PRIMARY KEY,
  workspace_id    INTEGER REFERENCES workspaces(id) ON DELETE CASCADE, -- NULL: shared by every workspace
  name            TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
  freq            TEXT NOT NULL CHECK (freq IN ('minutes', 'hours', 'daily', 'weekly', 'monthly')),
  interval_n      INTEGER NOT NULL DEFAULT 1 CHECK (interval_n BETWEEN 1 AND 365),
  weekdays        TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(weekdays)),
  month_day       INTEGER CHECK (month_day IS NULL OR month_day BETWEEN -1 AND 31 AND month_day != 0),
  tz              TEXT NOT NULL DEFAULT 'UTC',
  window_start    TEXT NOT NULL DEFAULT '00:00' CHECK (window_start GLOB '[0-2][0-9]:[0-5][0-9]'),
  window_minutes  INTEGER NOT NULL DEFAULT 1440 CHECK (window_minutes BETWEEN 5 AND 44640),
  lead_minutes    INTEGER NOT NULL DEFAULT 0 CHECK (lead_minutes BETWEEN 0 AND 10080),
  created_by      INTEGER NOT NULL REFERENCES users(id),
  created_at      INTEGER NOT NULL,
  deleted_at      INTEGER,
  version         INTEGER NOT NULL DEFAULT 1
) STRICT;
INSERT INTO schedules_new (id, workspace_id, name, freq, interval_n, weekdays, month_day, tz, window_start, window_minutes, lead_minutes,
  created_by, created_at, deleted_at, version)
  SELECT id, workspace_id, name, freq, interval_n, weekdays, month_day, tz, window_start, window_minutes, lead_minutes,
  created_by, created_at, deleted_at, version FROM schedules;
DROP TABLE schedules;
ALTER TABLE schedules_new RENAME TO schedules;
CREATE INDEX schedules_ws ON schedules(workspace_id) WHERE deleted_at IS NULL;

ALTER TABLE recurrences ADD COLUMN assignees TEXT CHECK (assignees IS NULL OR json_valid(assignees)); -- NULL: the template's

ALTER TABLE gigs ADD COLUMN auto_archive_minutes INTEGER CHECK (auto_archive_minutes IS NULL OR auto_archive_minutes BETWEEN 5 AND 527040);
ALTER TABLE gigs ADD COLUMN run_timeout_minutes INTEGER CHECK (run_timeout_minutes IS NULL OR run_timeout_minutes BETWEEN 5 AND 300);
-- what housekeeping scans: done or cancelled gigs that archive themselves and aren't archived yet
CREATE INDEX gigs_auto_archive ON gigs(updated_at) WHERE auto_archive_minutes IS NOT NULL AND archived_at IS NULL AND deleted_at IS NULL;
