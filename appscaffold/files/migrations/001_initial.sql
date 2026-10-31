-- The scaffold's example table. Migrations are forward-only: add 002_<name>.sql, never edit an
-- applied file. Every row belongs to a directory person (identity.Person.ID); queries filter by it.
CREATE TABLE notes (
  id         INTEGER PRIMARY KEY,
  person_id  TEXT    NOT NULL,
  text       TEXT    NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE INDEX notes_person ON notes (person_id, id);
