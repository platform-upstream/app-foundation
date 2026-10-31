-- The scaffold's example table. Migrations are forward-only: add 002_<name>.sql, never edit an
-- applied file.
CREATE TABLE notes (
  id         INTEGER PRIMARY KEY,
  text       TEXT    NOT NULL,
  created_at INTEGER NOT NULL
);
