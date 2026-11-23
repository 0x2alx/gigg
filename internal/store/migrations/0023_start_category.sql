-- 0023: where a template's gigs start. '' = the project's default status (as before); 'backlog' or
-- 'todo' = the first status of that category in the gig's project (its default when it has none).
-- A category, not a status id, so a shared template means the same in every project. Recurring gigs
-- read it from their template, so a schedule can make gigs that wait in Backlog for a person (the
-- runner only claims To-Do gigs).
ALTER TABLE gig_templates ADD COLUMN start_category TEXT NOT NULL DEFAULT '' CHECK (start_category IN ('', 'backlog', 'todo'));
