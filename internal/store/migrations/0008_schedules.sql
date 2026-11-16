-- gigg:foreign-keys-off
-- Schedules become their own objects: a schedule is timing only (rule, window,
-- create-early) and belongs to the workspace; a template is the task content; a recurrence (a
-- recurring task) links one template and one schedule in a project, at most once per project, and
-- keeps its own start/end, max count, overlap, pause and state. Templates and schedules can be
-- paired freely. recurrences is rebuilt (its content and timing columns go; ids are kept, so tasks
-- stay linked). Every existing recurrence gets a schedule with the same id, named by the Go step.
CREATE TABLE schedules (
  id              INTEGER PRIMARY KEY,
  workspace_id    INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  name            TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
  freq            TEXT NOT NULL CHECK (freq IN ('daily', 'weekly', 'monthly')),
  interval_n      INTEGER NOT NULL DEFAULT 1 CHECK (interval_n BETWEEN 1 AND 365),
  weekdays        TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(weekdays)), -- weekly: 1=Mon .. 7=Sun
  month_day       INTEGER CHECK (month_day IS NULL OR month_day BETWEEN -1 AND 31 AND month_day != 0), -- -1 = last day
  tz              TEXT NOT NULL DEFAULT 'UTC',
  window_start    TEXT NOT NULL DEFAULT '00:00' CHECK (window_start GLOB '[0-2][0-9]:[0-5][0-9]'),
  window_minutes  INTEGER NOT NULL DEFAULT 1440 CHECK (window_minutes BETWEEN 5 AND 44640),
  lead_minutes    INTEGER NOT NULL DEFAULT 0 CHECK (lead_minutes BETWEEN 0 AND 10080),
  created_by      INTEGER NOT NULL REFERENCES users(id),
  created_at      INTEGER NOT NULL,
  deleted_at      INTEGER,
  version         INTEGER NOT NULL DEFAULT 1
) STRICT;
CREATE INDEX schedules_ws ON schedules(workspace_id) WHERE deleted_at IS NULL;

INSERT INTO schedules (id, workspace_id, name, freq, interval_n, weekdays, month_day, tz, window_start, window_minutes, lead_minutes,
  created_by, created_at, deleted_at)
  SELECT id, workspace_id, 'schedule ' || id, freq, interval_n, weekdays, month_day, tz, window_start, window_minutes, lead_minutes,
  created_by, created_at, deleted_at FROM recurrences;

CREATE TABLE recurrences_new (
  id              INTEGER PRIMARY KEY,
  workspace_id    INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  project_id      INTEGER NOT NULL REFERENCES projects(id),
  created_by      INTEGER NOT NULL REFERENCES users(id),
  template_id     INTEGER REFERENCES task_templates(id),
  schedule_id     INTEGER REFERENCES schedules(id),
  starts_on       INTEGER NOT NULL,
  ends_on         INTEGER,
  max_count       INTEGER,
  overlap         TEXT NOT NULL DEFAULT 'skip' CHECK (overlap IN ('skip', 'create')),
  next_at         INTEGER,          -- start of the next occurrence's window (null = finished)
  created_count   INTEGER NOT NULL DEFAULT 0,
  skipped_count   INTEGER NOT NULL DEFAULT 0,
  last_task_id    INTEGER,
  paused_at       INTEGER,
  deleted_at      INTEGER,
  created_at      INTEGER NOT NULL,
  version         INTEGER NOT NULL DEFAULT 1,
  CHECK (deleted_at IS NOT NULL OR (template_id IS NOT NULL AND schedule_id IS NOT NULL))
) STRICT;
INSERT INTO recurrences_new (id, workspace_id, project_id, created_by, template_id, schedule_id, starts_on, ends_on, max_count, overlap,
  next_at, created_count, skipped_count, last_task_id, paused_at, deleted_at, created_at, version)
  SELECT id, workspace_id, project_id, created_by, template_id, id, starts_on, ends_on, max_count, overlap,
  next_at, created_count, skipped_count, last_task_id, paused_at, deleted_at, created_at, version FROM recurrences;
DROP TABLE recurrences;
ALTER TABLE recurrences_new RENAME TO recurrences;
CREATE INDEX recurrences_due ON recurrences(next_at) WHERE deleted_at IS NULL AND paused_at IS NULL;
CREATE INDEX recurrences_template ON recurrences(template_id) WHERE deleted_at IS NULL;
CREATE INDEX recurrences_schedule ON recurrences(schedule_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX recurrences_pair ON recurrences(project_id, template_id, schedule_id) WHERE deleted_at IS NULL;
