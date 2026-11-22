-- 0021: attachments. Files, links and commits on a gig. The never-used `artifacts` table (no API ever
-- wrote to it) is replaced by `attachments`: kinds file/link/commit (the old "path" meant nothing to
-- anyone but the host that wrote it), a thumbnail blob for images, and no soft delete (removing an
-- attachment deletes the row; the activity log keeps the record).
--
-- Files live in the existing content-addressed store (<data>/files/ab/cd/<sha256>, like user
-- pictures). blobs.refcount is recomputed by the nightly sweep from what still points at a blob, and
-- blobs.orphaned_at marks when it stopped being used: the file is removed only once that is older
-- than the snapshot window, so restoring a nightly snapshot never points at a missing file.
--
-- Workspaces get two limits (NULL = the server default): the largest file and the total of all its
-- attachments' sizes.
--
-- Keys that can work on gigs (gigs:work) get attachments:write: attaching is part of working on a
-- gig, so runner and agent keys keep doing what their rules allow without being reissued.
DROP INDEX artifacts_gig;
DROP TABLE artifacts;

CREATE TABLE attachments (
  id           INTEGER PRIMARY KEY,
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  gig_id       INTEGER NOT NULL REFERENCES gigs(id) ON DELETE CASCADE,
  run_id       INTEGER REFERENCES runs(id) ON DELETE SET NULL,
  kind         TEXT NOT NULL CHECK (kind IN ('file', 'link', 'commit')),
  label        TEXT NOT NULL DEFAULT '' CHECK (length(label) <= 200),
  value        TEXT NOT NULL DEFAULT '' CHECK (length(value) <= 2048),
  blob_sha256  TEXT REFERENCES blobs(sha256),
  thumb_sha256 TEXT REFERENCES blobs(sha256),
  filename     TEXT CHECK (filename IS NULL OR length(filename) BETWEEN 1 AND 255),
  mime         TEXT,
  size         INTEGER NOT NULL DEFAULT 0,
  width        INTEGER,
  height       INTEGER,
  created_by   INTEGER NOT NULL REFERENCES users(id),
  key_id       INTEGER REFERENCES api_keys(id) ON DELETE SET NULL,
  created_at   INTEGER NOT NULL,
  CHECK ((kind = 'file') = (blob_sha256 IS NOT NULL))
) STRICT;
CREATE INDEX attachments_gig ON attachments(gig_id, id);
CREATE INDEX attachments_ws ON attachments(workspace_id);
CREATE INDEX attachments_run ON attachments(run_id) WHERE run_id IS NOT NULL;

ALTER TABLE blobs ADD COLUMN orphaned_at INTEGER;

ALTER TABLE workspaces ADD COLUMN attach_file_max INTEGER CHECK (attach_file_max IS NULL OR attach_file_max > 0);
ALTER TABLE workspaces ADD COLUMN attach_quota INTEGER CHECK (attach_quota IS NULL OR attach_quota > 0);

UPDATE api_keys
   SET scopes = json_insert(scopes, '$[#]', 'attachments:read')
 WHERE EXISTS (SELECT 1 FROM json_each(api_keys.scopes) WHERE value = 'gigs:read')
   AND NOT EXISTS (SELECT 1 FROM json_each(api_keys.scopes) WHERE value = 'attachments:read');

UPDATE api_keys
   SET scopes = json_insert(scopes, '$[#]', 'attachments:write')
 WHERE EXISTS (SELECT 1 FROM json_each(api_keys.scopes) WHERE value = 'gigs:work')
   AND NOT EXISTS (SELECT 1 FROM json_each(api_keys.scopes) WHERE value = 'attachments:write');
