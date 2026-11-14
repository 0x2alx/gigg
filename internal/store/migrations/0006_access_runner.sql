-- One access model, and runner controls.
--
-- access_rules replaces memberships (roles per workspace), user_permissions and group_permissions
-- (global feature switches): a rule gives a user or a group a preset and/or overrides in one
-- workspace or in all of them (workspace_id NULL). The data is converted in Go right after this
-- script (store.migrateAccess), which then drops the three old tables; nobody gains or loses access.

CREATE TABLE access_rules (
  id            INTEGER PRIMARY KEY,
  user_id       INTEGER REFERENCES users(id) ON DELETE CASCADE,
  group_id      INTEGER REFERENCES groups(id) ON DELETE CASCADE,
  workspace_id  INTEGER REFERENCES workspaces(id) ON DELETE CASCADE, -- NULL: all workspaces, future ones too
  preset        TEXT NOT NULL DEFAULT '' CHECK (preset IN ('', 'viewer', 'commenter', 'member', 'manager')),
  overrides     TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(overrides) AND json_type(overrides) = 'object'),
  created_at    INTEGER NOT NULL,
  CHECK ((user_id IS NULL) != (group_id IS NULL))
) STRICT;
CREATE UNIQUE INDEX access_rules_key ON access_rules(IFNULL(user_id, 0), IFNULL(group_id, 0), IFNULL(workspace_id, 0));
CREATE INDEX access_rules_user ON access_rules(user_id);
CREATE INDEX access_rules_group ON access_rules(group_id);
CREATE INDEX access_rules_ws ON access_rules(workspace_id);

-- How an agent works a task: model and effort for claude -p ('' = the agent's default), and
-- always_run: worked whatever the usage budget (cron-like tasks), still within the task cap.
ALTER TABLE tasks ADD COLUMN model TEXT NOT NULL DEFAULT '' CHECK (length(model) <= 64);
ALTER TABLE tasks ADD COLUMN effort TEXT NOT NULL DEFAULT '' CHECK (effort IN ('', 'low', 'medium', 'high', 'xhigh', 'max'));
ALTER TABLE tasks ADD COLUMN always_run INTEGER NOT NULL DEFAULT 0 CHECK (always_run IN (0, 1));
ALTER TABLE recurrences ADD COLUMN model TEXT NOT NULL DEFAULT '' CHECK (length(model) <= 64);
ALTER TABLE recurrences ADD COLUMN effort TEXT NOT NULL DEFAULT '' CHECK (effort IN ('', 'low', 'medium', 'high', 'xhigh', 'max'));
ALTER TABLE recurrences ADD COLUMN always_run INTEGER NOT NULL DEFAULT 0 CHECK (always_run IN (0, 1));
ALTER TABLE task_templates ADD COLUMN model TEXT NOT NULL DEFAULT '' CHECK (length(model) <= 64);
ALTER TABLE task_templates ADD COLUMN effort TEXT NOT NULL DEFAULT '' CHECK (effort IN ('', 'low', 'medium', 'high', 'xhigh', 'max'));
ALTER TABLE task_templates ADD COLUMN always_run INTEGER NOT NULL DEFAULT 0 CHECK (always_run IN (0, 1));

-- What a run cost, in points of the account's weekly usage meter (the runner measures it; shared
-- meter, so approximate). NULL = not measured.
ALTER TABLE runs ADD COLUMN usage_pct REAL CHECK (usage_pct IS NULL OR usage_pct BETWEEN -100 AND 100);

-- Per-workspace runner limits and the pause switch (admins). No row = defaults, not paused.
CREATE TABLE runner_settings (
  workspace_id   INTEGER PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
  daily_cap      INTEGER NOT NULL DEFAULT 15 CHECK (daily_cap BETWEEN 0 AND 200),    -- distinct tasks per rolling 24 h
  night_cap      INTEGER NOT NULL DEFAULT 10 CHECK (night_cap BETWEEN 0 AND 200),    -- of which budget-dependent ones
  paused_until   INTEGER,                                                            -- ms; NULL = not paused
  paused_open    INTEGER NOT NULL DEFAULT 0 CHECK (paused_open IN (0, 1)),           -- 1 = until resumed
  paused_by      INTEGER REFERENCES users(id) ON DELETE SET NULL,
  paused_at      INTEGER,
  version        INTEGER NOT NULL DEFAULT 1
) STRICT;
