-- 0019: the shared library gets a permission of its own (shared_library, held only on an
-- all-workspaces rule) and a key scope (library:write). Until now only instance admins changed it,
-- with gigs:write (templates, schedules) or errands:write (errand templates): their unlimited keys that
-- carry either keep working by getting library:write. No rule changes: nobody but admins holds the
-- permission until it is granted.
UPDATE api_keys
   SET scopes = json_insert(scopes, '$[#]', 'library:write')
 WHERE bound = 0
   AND user_id IN (SELECT id FROM users WHERE is_admin = 1)
   AND EXISTS (SELECT 1 FROM json_each(api_keys.scopes) WHERE value IN ('gigs:write', 'errands:write'))
   AND NOT EXISTS (SELECT 1 FROM json_each(api_keys.scopes) WHERE value = 'library:write');
