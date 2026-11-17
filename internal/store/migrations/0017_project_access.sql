-- gigg:foreign-keys-off
-- 0017: projects as a place for access rules and keys, and errands that belong to a project.
--
-- access_rules.project_id: a rule for one project of its workspace (gig-level permissions only,
-- checked in Go). For one permission in one project the first rule with an opinion decides: the
-- user's project rule, workspace rule, all-workspaces rule, then the groups' project, workspace and
-- all-workspaces rules. Deleting a project removes its rules.
ALTER TABLE access_rules ADD COLUMN project_id INTEGER REFERENCES projects(id) ON DELETE CASCADE;
DROP INDEX access_rules_key;
CREATE UNIQUE INDEX access_rules_key ON access_rules(IFNULL(user_id, 0), IFNULL(group_id, 0), IFNULL(workspace_id, 0), IFNULL(project_id, 0));
CREATE INDEX access_rules_project ON access_rules(project_id) WHERE project_id IS NOT NULL;

-- Keys: a key either follows its user's rules (bound = 0, wherever they reach) or reaches only the
-- places listed in api_key_reach (bound = 1): whole workspaces and single projects, in order; the
-- first one's workspace is the key's "current". A bound key whose places are all gone reaches
-- nothing (it never falls back to following its user). Today's workspace keys become a key bound to
-- that workspace, or to its listed projects.
CREATE TABLE api_key_reach (
  key_id       INTEGER NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
  position     INTEGER NOT NULL,
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  project_id   INTEGER REFERENCES projects(id) ON DELETE CASCADE
) STRICT;
CREATE UNIQUE INDEX api_key_reach_place ON api_key_reach(key_id, workspace_id, IFNULL(project_id, 0));
INSERT INTO api_key_reach (key_id, position, workspace_id, project_id)
SELECT k.id, 0, k.workspace_id, NULL FROM api_keys k
 WHERE k.workspace_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM api_key_projects kp WHERE kp.key_id = k.id);
INSERT INTO api_key_reach (key_id, position, workspace_id, project_id)
SELECT k.id, row_number() OVER (PARTITION BY k.id ORDER BY kp.project_id) - 1, k.workspace_id, kp.project_id
  FROM api_keys k JOIN api_key_projects kp ON kp.key_id = k.id
 WHERE k.workspace_id IS NOT NULL;

CREATE TABLE api_keys_new (
  id                 INTEGER PRIMARY KEY,
  name               TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 64),
  prefix             TEXT NOT NULL UNIQUE,
  secret_hash        BLOB NOT NULL,
  user_id            INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  bound              INTEGER NOT NULL DEFAULT 0 CHECK (bound IN (0, 1)), -- 1: only the places in api_key_reach
  scopes             TEXT NOT NULL CHECK (json_valid(scopes)),
  allowed_listeners  TEXT CHECK (allowed_listeners IS NULL OR json_valid(allowed_listeners)),
  allowed_cidrs      TEXT CHECK (allowed_cidrs IS NULL OR json_valid(allowed_cidrs)),
  expires_at         INTEGER NOT NULL,
  created_at         INTEGER NOT NULL,
  created_by_user    INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_by_key     INTEGER REFERENCES api_keys(id) ON DELETE SET NULL,
  last_used_at       INTEGER,
  last_used_listener TEXT,
  last_used_ip       TEXT,
  revoked_at         INTEGER,
  version            INTEGER NOT NULL DEFAULT 1
) STRICT;
INSERT INTO api_keys_new (id, name, prefix, secret_hash, user_id, bound, scopes, allowed_listeners, allowed_cidrs, expires_at,
  created_at, created_by_user, created_by_key, last_used_at, last_used_listener, last_used_ip, revoked_at, version)
SELECT id, name, prefix, secret_hash, user_id, workspace_id IS NOT NULL, scopes, allowed_listeners, allowed_cidrs, expires_at,
  created_at, created_by_user, created_by_key, last_used_at, last_used_listener, last_used_ip, revoked_at, version
  FROM api_keys;
DROP TABLE api_key_projects;
DROP TABLE api_keys;
ALTER TABLE api_keys_new RENAME TO api_keys;
CREATE INDEX api_keys_user ON api_keys(user_id);

-- Errands belong to a project (the one whose work they do); existing ones go to their workspace's
-- first live project, the one a failure gig used to land in. A project that is deleted for good
-- leaves its errands without one (only whole-workspace readers see those).
ALTER TABLE errands ADD COLUMN project_id INTEGER REFERENCES projects(id) ON DELETE SET NULL;
UPDATE errands SET project_id = (SELECT p.id FROM projects p WHERE p.workspace_id = errands.workspace_id AND p.deleted_at IS NULL
  ORDER BY p.archived_at IS NOT NULL, p.position, p.id LIMIT 1);
CREATE INDEX errands_project ON errands(project_id) WHERE deleted_at IS NULL;
