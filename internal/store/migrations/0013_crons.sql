-- gigg:foreign-keys-off
-- Crons: commands an agent's executor runs on a schedule, with a run history and no tasks. A cron
-- template is the content (command, timeout, output, sandbox); a cron links one template, one
-- schedule and one worker (the agent whose executor runs it). Schedules gain minute and hour
-- frequencies (crons only; recurring tasks keep daily/weekly/monthly), so the table is rebuilt for
-- its CHECK constraint (12-step procedure; foreign keys are off for this migration and checked
-- before commit).
CREATE TABLE schedules_new (
  id              INTEGER PRIMARY KEY,
  workspace_id    INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  name            TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
  freq            TEXT NOT NULL CHECK (freq IN ('minutes', 'hours', 'daily', 'weekly', 'monthly')),
  interval_n      INTEGER NOT NULL DEFAULT 1 CHECK (interval_n BETWEEN 1 AND 365),
  weekdays        TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(weekdays)), -- weekly (or minutes/hours: optional filter): 1=Mon .. 7=Sun
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
INSERT INTO schedules_new (id, workspace_id, name, freq, interval_n, weekdays, month_day, tz, window_start, window_minutes, lead_minutes,
  created_by, created_at, deleted_at, version)
  SELECT id, workspace_id, name, freq, interval_n, weekdays, month_day, tz, window_start, window_minutes, lead_minutes,
  created_by, created_at, deleted_at, version FROM schedules;
DROP TABLE schedules;
ALTER TABLE schedules_new RENAME TO schedules;
CREATE INDEX schedules_ws ON schedules(workspace_id) WHERE deleted_at IS NULL;

CREATE TABLE cron_templates (
  id              INTEGER PRIMARY KEY,
  workspace_id    INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  name            TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
  description_md  TEXT NOT NULL DEFAULT '' CHECK (length(description_md) <= 65536),
  command         TEXT NOT NULL CHECK (length(command) BETWEEN 1 AND 16384),
  timeout_s       INTEGER NOT NULL DEFAULT 300 CHECK (timeout_s BETWEEN 10 AND 3600),
  output          TEXT NOT NULL DEFAULT 'gigg' CHECK (output IN ('gigg', 'host')), -- host: only exit code and timing reach gigg
  unsandboxed     INTEGER NOT NULL DEFAULT 0 CHECK (unsandboxed IN (0, 1)),
  command_set_by  INTEGER NOT NULL REFERENCES users(id), -- who last changed the command, timeout or sandbox flag
  command_set_at  INTEGER NOT NULL,
  created_by      INTEGER NOT NULL REFERENCES users(id),
  created_at      INTEGER NOT NULL,
  deleted_at      INTEGER,
  version         INTEGER NOT NULL DEFAULT 1
) STRICT;
CREATE INDEX cron_templates_ws ON cron_templates(workspace_id) WHERE deleted_at IS NULL;

CREATE TABLE crons (
  id              INTEGER PRIMARY KEY,
  workspace_id    INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  template_id     INTEGER REFERENCES cron_templates(id),
  schedule_id     INTEGER REFERENCES schedules(id),
  worker_id       INTEGER NOT NULL REFERENCES users(id),
  created_by      INTEGER NOT NULL REFERENCES users(id),
  on_failure_task INTEGER NOT NULL DEFAULT 0 CHECK (on_failure_task IN (0, 1)), -- the first failure opens a task for the worker
  next_at         INTEGER,          -- the next slot (null: none)
  run_now_at      INTEGER,          -- someone asked for a run now
  paused_at       INTEGER,
  state           TEXT NOT NULL DEFAULT 'never' CHECK (state IN ('never', 'ok', 'failing')),
  fail_streak     INTEGER NOT NULL DEFAULT 0,
  failing_since   INTEGER,
  last_run_id     INTEGER,
  deleted_at      INTEGER,
  created_at      INTEGER NOT NULL,
  version         INTEGER NOT NULL DEFAULT 1,
  CHECK (deleted_at IS NOT NULL OR (template_id IS NOT NULL AND schedule_id IS NOT NULL))
) STRICT;
CREATE INDEX crons_due ON crons(worker_id, next_at) WHERE deleted_at IS NULL AND paused_at IS NULL;
CREATE INDEX crons_ws ON crons(workspace_id) WHERE deleted_at IS NULL;
CREATE INDEX crons_template ON crons(template_id) WHERE deleted_at IS NULL;
CREATE INDEX crons_schedule ON crons(schedule_id) WHERE deleted_at IS NULL;

CREATE TABLE cron_runs (
  id              INTEGER PRIMARY KEY,
  cron_id         INTEGER NOT NULL REFERENCES crons(id) ON DELETE CASCADE,
  workspace_id    INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  worker_id       INTEGER NOT NULL REFERENCES users(id),
  scheduled_for   INTEGER,          -- the slot (null: run now)
  started_at      INTEGER,          -- null: never started (missed, skipped, refused)
  finished_at     INTEGER,
  status          TEXT NOT NULL CHECK (status IN ('running', 'ok', 'failed', 'timeout', 'missed', 'skipped')),
  exit_code       INTEGER,
  duration_ms     INTEGER,
  sandboxed       INTEGER NOT NULL DEFAULT 1 CHECK (sandboxed IN (0, 1)),
  output_tail     TEXT CHECK (output_tail IS NULL OR length(CAST(output_tail AS BLOB)) <= 65536),
  output_bytes    INTEGER,
  error           TEXT CHECK (error IS NULL OR length(error) <= 1000),
  created_at      INTEGER NOT NULL
) STRICT;
CREATE INDEX cron_runs_cron ON cron_runs(cron_id, id);
CREATE INDEX cron_runs_running ON cron_runs(started_at) WHERE status = 'running';

-- Two pause boxes: PAUSE stops new task runs and cron runs; each can be resumed alone. Crons pause
-- until resumed (no end time).
ALTER TABLE runner_settings ADD COLUMN crons_paused_at INTEGER;
ALTER TABLE runner_settings ADD COLUMN crons_paused_by INTEGER REFERENCES users(id) ON DELETE SET NULL;
