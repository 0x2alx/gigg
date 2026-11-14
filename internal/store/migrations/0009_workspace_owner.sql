-- A workspace can name the user it belongs to (an agent's own workspace, say); the sidebar shows
-- their picture next to it. Display only: ownership grants nothing, access stays with the rules.
-- Existing workspaces take the user whose username equals the slug, when there is one.
ALTER TABLE workspaces ADD COLUMN owner_id INTEGER REFERENCES users(id) ON DELETE SET NULL;

UPDATE workspaces SET owner_id = (SELECT u.id FROM users u WHERE u.username = workspaces.slug);
