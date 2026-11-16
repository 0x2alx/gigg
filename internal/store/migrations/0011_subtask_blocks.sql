-- A subtask can hold its parent back: while a subtask with blocks_parent is open, the parent counts
-- as blocked (like a "blocks" dependency) and isn't handed out by claim. Only meaningful with a
-- parent_id. A template can carry subtasks (title + blocks_parent) that every task made from it gets.
ALTER TABLE tasks ADD COLUMN blocks_parent INTEGER NOT NULL DEFAULT 0 CHECK (blocks_parent IN (0, 1));
ALTER TABLE task_templates ADD COLUMN subtasks TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(subtasks));
-- the subtask lookups (counts, the blocked check) run per task: index the parent
CREATE INDEX tasks_parent ON tasks(parent_id) WHERE parent_id IS NOT NULL;
