-- gigg:foreign-keys-off
-- Runs gain the status 'parked': the agent started a long job and handed the task back to be
-- checked on a later pass. SQLite can't change a CHECK constraint in place, so the table is
-- rebuilt (12-step procedure; foreign keys are off for this migration and checked before commit).
CREATE TABLE runs_new (
  id                 INTEGER PRIMARY KEY,
  workspace_id       INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  task_id            INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  worker_user_id     INTEGER NOT NULL REFERENCES users(id),
  key_id             INTEGER REFERENCES api_keys(id) ON DELETE SET NULL,
  session_id         TEXT,
  mode               TEXT NOT NULL DEFAULT '' CHECK (length(mode) <= 32),
  attempt            INTEGER NOT NULL DEFAULT 1,
  status             TEXT NOT NULL CHECK (status IN ('running', 'done', 'needs_human', 'error', 'timeout', 'interrupted', 'parked')),
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
INSERT INTO runs_new SELECT * FROM runs;
DROP TABLE runs;
ALTER TABLE runs_new RENAME TO runs;
CREATE INDEX runs_task ON runs(task_id, id);
CREATE INDEX runs_ws ON runs(workspace_id, status, started_at);
