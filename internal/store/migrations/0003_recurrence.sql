-- Recurring tasks and time windows.
-- A recurrence is a schedule plus a template; gigg creates a normal task for each occurrence, with
-- start_at = window opens, not_after = window closes (also the due date). Occurrences whose window
-- closes before anyone started them are marked missed.

CREATE TABLE recurrences (
  id              INTEGER PRIMARY KEY,
  workspace_id    INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  project_id      INTEGER NOT NULL REFERENCES projects(id),
  created_by      INTEGER NOT NULL REFERENCES users(id),
  -- template
  title           TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 250),
  description_md  TEXT NOT NULL DEFAULT '' CHECK (length(description_md) <= 65536),
  priority        INTEGER NOT NULL DEFAULT 0 CHECK (priority BETWEEN 0 AND 4),
  color           TEXT,
  labels          TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(labels)),
  assignees       TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(assignees)),
  -- schedule
  freq            TEXT NOT NULL CHECK (freq IN ('daily', 'weekly', 'monthly')),
  interval_n      INTEGER NOT NULL DEFAULT 1 CHECK (interval_n BETWEEN 1 AND 365),
  weekdays        TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(weekdays)), -- weekly: 1=Mon .. 7=Sun
  month_day       INTEGER CHECK (month_day IS NULL OR month_day BETWEEN -1 AND 31 AND month_day != 0), -- -1 = last day
  tz              TEXT NOT NULL DEFAULT 'America/New_York',
  window_start    TEXT NOT NULL DEFAULT '00:00' CHECK (window_start GLOB '[0-2][0-9]:[0-5][0-9]'),
  window_minutes  INTEGER NOT NULL DEFAULT 1440 CHECK (window_minutes BETWEEN 5 AND 44640),
  lead_minutes    INTEGER NOT NULL DEFAULT 0 CHECK (lead_minutes BETWEEN 0 AND 10080), -- create this early
  starts_on       INTEGER NOT NULL,
  ends_on         INTEGER,
  max_count       INTEGER,
  overlap         TEXT NOT NULL DEFAULT 'skip' CHECK (overlap IN ('skip', 'create')),
  -- state
  next_at         INTEGER,          -- start of the next occurrence's window (null = finished)
  created_count   INTEGER NOT NULL DEFAULT 0,
  skipped_count   INTEGER NOT NULL DEFAULT 0,
  last_task_id    INTEGER,
  paused_at       INTEGER,
  deleted_at      INTEGER,
  created_at      INTEGER NOT NULL,
  version         INTEGER NOT NULL DEFAULT 1
) STRICT;
CREATE INDEX recurrences_due ON recurrences(next_at) WHERE deleted_at IS NULL AND paused_at IS NULL;

ALTER TABLE tasks ADD COLUMN recurrence_id INTEGER REFERENCES recurrences(id) ON DELETE SET NULL;
ALTER TABLE tasks ADD COLUMN occurrence_at INTEGER;
ALTER TABLE tasks ADD COLUMN not_after INTEGER;
ALTER TABLE tasks ADD COLUMN missed_at INTEGER;
CREATE INDEX tasks_recurrence ON tasks(recurrence_id, occurrence_at);
CREATE INDEX tasks_window ON tasks(not_after) WHERE missed_at IS NULL AND deleted_at IS NULL;
