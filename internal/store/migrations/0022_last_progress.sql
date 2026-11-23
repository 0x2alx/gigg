-- 0022: last_progress_at on gigs. When a gig last moved forward: a comment (not a system note), a
-- checklist tick or untick, a status change, or a run finishing, whoever did it. Title, description,
-- colour, label and follower edits don't count. NULL until the first such event (readers use the
-- creation time then). Backfilled from the latest comment, the latest finished run and done_at.
ALTER TABLE gigs ADD COLUMN last_progress_at INTEGER;

UPDATE gigs SET last_progress_at = max(
  coalesce((SELECT max(c.created_at) FROM comments c WHERE c.gig_id = gigs.id AND c.kind IN ('comment','report','question')), 0),
  coalesce((SELECT max(r.ended_at) FROM runs r WHERE r.gig_id = gigs.id), 0),
  coalesce(done_at, 0));
UPDATE gigs SET last_progress_at = NULL WHERE last_progress_at = 0;
