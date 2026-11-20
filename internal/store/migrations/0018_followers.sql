-- gigg:foreign-keys-off
-- 0018: followers. Anyone who can see a gig can follow it; its bells (reports, questions, failed
-- runs, missed windows) go to its assignees and its followers, each filtered by the kinds that
-- person's bell settings allow. Existing gigs start with no followers.
CREATE TABLE gig_followers (
  gig_id  INTEGER NOT NULL REFERENCES gigs(id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  PRIMARY KEY (gig_id, user_id)
) STRICT;
CREATE INDEX gig_followers_user ON gig_followers(user_id);

-- A gig template names followers too: the gigs made from it (by hand or by a recurring gig) start
-- with them.
ALTER TABLE gig_templates ADD COLUMN followers TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(followers));

-- A new bell kind: failed (a run that ended in an error or timed out without asking a question).
CREATE TABLE notifications_new (
  id            INTEGER PRIMARY KEY,
  user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  workspace_id  INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  gig_id        INTEGER REFERENCES gigs(id) ON DELETE CASCADE,
  comment_id    INTEGER,
  kind          TEXT NOT NULL CHECK (kind IN ('mention', 'assigned', 'question', 'failed', 'missed', 'comment')),
  actor_user_id INTEGER REFERENCES users(id),
  created_at    INTEGER NOT NULL,
  read_at       INTEGER
) STRICT;
INSERT INTO notifications_new (id, user_id, workspace_id, gig_id, comment_id, kind, actor_user_id, created_at, read_at)
  SELECT id, user_id, workspace_id, gig_id, comment_id, kind, actor_user_id, created_at, read_at FROM notifications;
DROP TABLE notifications;
ALTER TABLE notifications_new RENAME TO notifications;
CREATE INDEX notifications_user ON notifications(user_id, read_at, id);
