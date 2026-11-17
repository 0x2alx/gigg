-- 0016: Work on gigs, a tidier permission list, and the workspace owner as the default assignee.
--
-- work_gigs (scope gigs:work) is a consumer's share of what edit_gigs (gigs:write) used to cover:
-- status moves inside the project, ticking checklist items, resume requests, claims and runs.
-- edit_gigs keeps the field edits. Every rule that says something about edit_gigs says the same
-- about work_gigs, and every key with gigs:write also gets gigs:work, so nothing changes for anyone
-- until a rule or a key is narrowed on purpose. (The member and manager presets include work_gigs.)
UPDATE access_rules
   SET overrides = json_set(overrides, '$.work_gigs',
         CASE json_type(overrides, '$.edit_gigs') WHEN 'true' THEN json('true') ELSE json('false') END)
 WHERE json_type(overrides, '$.edit_gigs') IN ('true', 'false')
   AND json_type(overrides, '$.work_gigs') IS NULL;

UPDATE api_keys
   SET scopes = json_insert(scopes, '$[#]', 'gigs:work')
 WHERE EXISTS (SELECT 1 FROM json_each(api_keys.scopes) WHERE value = 'gigs:write')
   AND NOT EXISTS (SELECT 1 FROM json_each(api_keys.scopes) WHERE value = 'gigs:work');

-- The permission list, tidied (what each rule allows stays the same, except as noted):
--   - the presets are viewer, consumer, member and manager: "commenter" is gone, and its rules become
--     viewer plus Comment (consumer is new; manager also gets errands, pause_runs and the run
--     settings, which widens it on purpose);
--   - "schedules" is called "recurring_gigs", which is what it covers;
--   - "checklists" and "relations" fold into edit_gigs: a rule that denied either now denies
--     edit_gigs (a merged permission is allowed only where all its parts were);
--   - the scopes no endpoint ever checked (artifacts:write, admin:transfer, admin:export,
--     admin:settings) are dropped from keys.
-- The table is rebuilt for the preset CHECK (migrations run with foreign keys off; nothing refers
-- to access_rules).
CREATE TABLE access_rules_new (
  id            INTEGER PRIMARY KEY,
  user_id       INTEGER REFERENCES users(id) ON DELETE CASCADE,
  group_id      INTEGER REFERENCES groups(id) ON DELETE CASCADE,
  workspace_id  INTEGER REFERENCES workspaces(id) ON DELETE CASCADE, -- NULL: all workspaces, future ones too
  preset        TEXT NOT NULL DEFAULT '' CHECK (preset IN ('', 'viewer', 'consumer', 'member', 'manager')),
  overrides     TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(overrides) AND json_type(overrides) = 'object'),
  created_at    INTEGER NOT NULL,
  CHECK ((user_id IS NULL) != (group_id IS NULL))
) STRICT;
INSERT INTO access_rules_new (id, user_id, group_id, workspace_id, preset, overrides, created_at)
SELECT id, user_id, group_id, workspace_id,
       CASE preset WHEN 'commenter' THEN 'viewer' ELSE preset END,
       CASE WHEN preset = 'commenter' AND json_type(overrides, '$.comment') IS NULL
            THEN json_set(overrides, '$.comment', json('true')) ELSE overrides END,
       created_at
  FROM access_rules;
DROP TABLE access_rules;
ALTER TABLE access_rules_new RENAME TO access_rules;
CREATE UNIQUE INDEX access_rules_key ON access_rules(IFNULL(user_id, 0), IFNULL(group_id, 0), IFNULL(workspace_id, 0));
CREATE INDEX access_rules_user ON access_rules(user_id);
CREATE INDEX access_rules_group ON access_rules(group_id);
CREATE INDEX access_rules_ws ON access_rules(workspace_id);

UPDATE access_rules
   SET overrides = json_remove(json_set(overrides, '$.recurring_gigs',
         CASE json_type(overrides, '$.schedules') WHEN 'true' THEN json('true') ELSE json('false') END), '$.schedules')
 WHERE json_type(overrides, '$.schedules') IN ('true', 'false');

UPDATE access_rules
   SET overrides = json_set(overrides, '$.edit_gigs', json('false'))
 WHERE json_type(overrides, '$.checklists') = 'false' OR json_type(overrides, '$.relations') = 'false';
UPDATE access_rules
   SET overrides = json_remove(overrides, '$.checklists', '$.relations')
 WHERE json_type(overrides, '$.checklists') IS NOT NULL OR json_type(overrides, '$.relations') IS NOT NULL;

UPDATE api_keys
   SET scopes = (SELECT json_group_array(value) FROM json_each(api_keys.scopes)
                  WHERE value NOT IN ('artifacts:write', 'admin:transfer', 'admin:export', 'admin:settings'))
 WHERE EXISTS (SELECT 1 FROM json_each(api_keys.scopes)
                WHERE value IN ('artifacts:write', 'admin:transfer', 'admin:export', 'admin:settings'));

-- A gig, workspace template or recurring gig created with nobody assigned now gets the workspace's
-- owner. The ones made before get it here, once: open gigs (not done, cancelled, trashed or archived)
-- with no assignee, workspace templates with none, and recurring gigs on a shared template with none
-- of their own. Later edits may unassign again.
--
-- The owner must be assignable there: active, and an admin or holding "see the workspace" through a
-- rule of theirs or of a group (in that workspace or all of them). This is a frozen, simpler reading
-- of the access rules than the code's (it ignores a deny that another rule would add), so it keeps
-- working whatever the access code becomes later.
CREATE TEMP TABLE m16_owner AS
SELECT w.id AS ws, w.owner_id AS uid
  FROM workspaces w JOIN users u ON u.id = w.owner_id
 WHERE u.disabled_at IS NULL
   AND (u.is_admin = 1 OR EXISTS (
         SELECT 1 FROM access_rules r
          WHERE (r.user_id = u.id OR r.group_id IN (SELECT group_id FROM group_members WHERE user_id = u.id))
            AND (r.workspace_id = w.id OR r.workspace_id IS NULL)
            AND COALESCE(json_extract(r.overrides, '$.view'), r.preset != '') = 1));

CREATE TEMP TABLE m16_gigs AS
SELECT t.id, t.workspace_id AS ws, t.project_id AS project, o.uid
  FROM gigs t JOIN statuses s ON s.id = t.status_id JOIN m16_owner o ON o.ws = t.workspace_id
 WHERE s.category NOT IN ('done', 'cancelled') AND t.deleted_at IS NULL AND t.archived_at IS NULL
   AND NOT EXISTS (SELECT 1 FROM gig_assignees a WHERE a.gig_id = t.id);

INSERT INTO gig_assignees (gig_id, user_id) SELECT id, uid FROM m16_gigs;
UPDATE gigs SET version = version + 1 WHERE id IN (SELECT id FROM m16_gigs);
INSERT INTO activity (at, workspace_id, project_id, listener, action, entity_type, entity_id, data)
SELECT CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER), ws, project, 'migration', 'gig.updated', 'gig', id,
       json_object('assignees', json_array(uid), 'note', 'the workspace owner, by default')
  FROM m16_gigs;

UPDATE gig_templates
   SET assignees = json_array((SELECT uid FROM m16_owner o WHERE o.ws = gig_templates.workspace_id)),
       version = version + 1
 WHERE workspace_id IN (SELECT ws FROM m16_owner) AND deleted_at IS NULL AND json_array_length(assignees) = 0;

UPDATE recurrences
   SET assignees = json_array((SELECT uid FROM m16_owner o WHERE o.ws = recurrences.workspace_id)),
       version = version + 1
 WHERE workspace_id IN (SELECT ws FROM m16_owner) AND deleted_at IS NULL
   AND (assignees IS NULL OR json_array_length(assignees) = 0)
   AND template_id IN (SELECT id FROM gig_templates WHERE workspace_id IS NULL);

DROP TABLE m16_gigs;
DROP TABLE m16_owner;
