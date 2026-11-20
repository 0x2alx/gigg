-- 0020: indexes for the metrics page (GET /metrics). Every metric counts rows of one workspace in a
-- time range, so each gets a (workspace_id, time) index; the partial ones skip the rows that never
-- carry the time. activity is append-only; a new index doesn't touch its guard triggers.
CREATE INDEX gigs_created ON gigs(workspace_id, created_at);
CREATE INDEX gigs_done ON gigs(workspace_id, done_at) WHERE done_at IS NOT NULL;
CREATE INDEX gigs_archived_at ON gigs(workspace_id, archived_at) WHERE archived_at IS NOT NULL;
CREATE INDEX gigs_occurrence ON gigs(workspace_id, occurrence_at) WHERE recurrence_id IS NOT NULL;
CREATE INDEX runs_started ON runs(workspace_id, started_at);
CREATE INDEX errand_runs_created ON errand_runs(workspace_id, created_at);
CREATE INDEX activity_moved ON activity(workspace_id, at) WHERE action = 'gig.moved';
