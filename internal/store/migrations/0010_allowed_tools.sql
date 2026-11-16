-- A task (and a template) can carry a tool allowlist: Claude Code permission rules the runner passes
-- to claude -p (--permission-mode dontAsk --allowedTools=...). Empty =
-- the runner's usual mode. Setting it, and changing a task or template that has one, needs the
-- allowed_tools permission (in no preset), checked by the API.
ALTER TABLE tasks ADD COLUMN allowed_tools TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(allowed_tools));
ALTER TABLE task_templates ADD COLUMN allowed_tools TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(allowed_tools));
