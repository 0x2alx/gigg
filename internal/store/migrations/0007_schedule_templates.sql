-- Schedules take their task content from a task template: a recurring task is a
-- template plus timing. A schedule made from the task form owns the template it created (deleted with
-- it when nothing else uses it); several schedules may share one template. The content columns on
-- recurrences stay as a copy written on save; gigg reads the template. The Go step after this SQL
-- gives every live schedule a template made from its own content.
ALTER TABLE recurrences ADD COLUMN template_id INTEGER REFERENCES task_templates(id);
ALTER TABLE recurrences ADD COLUMN owns_template INTEGER NOT NULL DEFAULT 0 CHECK (owns_template IN (0, 1));
CREATE INDEX recurrences_template ON recurrences(template_id) WHERE deleted_at IS NULL;
