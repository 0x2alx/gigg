-- The instance row (one per database). The real tables come in 0002.
CREATE TABLE instance (
  id         INTEGER PRIMARY KEY CHECK (id = 1),
  created_at INTEGER NOT NULL
) STRICT;
INSERT INTO instance (id, created_at) VALUES (1, CAST(unixepoch('subsec') * 1000 AS INTEGER));
