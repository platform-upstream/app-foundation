package sqlite

import (
	"context"
	"database/sql"
	"testing"
	"testing/fstest"
	"time"
)

func TestOpenDefaults(t *testing.T) {
	db, err := Open(context.Background(), t.TempDir()+"/nested/app.sqlite", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if got := db.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("MaxOpenConnections = %d", got)
	}
	for pragma, want := range map[string]string{"journal_mode": "wal", "foreign_keys": "1", "busy_timeout": "5000", "cache_size": "-4096"} {
		var got string
		if err := db.QueryRow("PRAGMA " + pragma).Scan(&got); err != nil {
			t.Fatalf("PRAGMA %s: %v", pragma, err)
		}
		if got != want {
			t.Errorf("PRAGMA %s = %q, want %q", pragma, got, want)
		}
	}
}

func TestOpenPreservesExistingData(t *testing.T) {
	path := t.TempDir() + "/copied.sqlite"
	original, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := original.Exec(`CREATE TABLE items (id TEXT PRIMARY KEY, title TEXT NOT NULL); INSERT INTO items VALUES ('kept', 'Original');`); err != nil {
		t.Fatal(err)
	}
	if err := original.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := Open(context.Background(), path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var title string
	if err := db.QueryRow(`SELECT title FROM items WHERE id='kept'`).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "Original" {
		t.Fatalf("title = %q", title)
	}
}

func TestMigrateOrdersAndIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	migrations := fstest.MapFS{
		"migrations/002_second.sql": {Data: []byte(`INSERT INTO events VALUES ('second');`)},
		"migrations/001_first.sql":  {Data: []byte(`CREATE TABLE events (name TEXT NOT NULL); INSERT INTO events VALUES ('first');`)},
		"migrations/README.md":      {Data: []byte("ignored")},
	}
	for i := 0; i < 2; i++ {
		if err := Migrate(context.Background(), db, migrations, "migrations"); err != nil {
			t.Fatalf("Migrate pass %d: %v", i, err)
		}
	}
	rows, err := db.Query(`SELECT name FROM events ORDER BY rowid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		got = append(got, name)
	}
	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("events = %#v", got)
	}
}

func TestFailedMigrationRollsBack(t *testing.T) {
	db := openTestDB(t)
	err := ApplyMigrations(context.Background(), db, []Migration{{
		Version: 1,
		Name:    "broken",
		SQL:     `CREATE TABLE should_rollback (id INTEGER); INSERT INTO missing_table VALUES (1);`,
	}})
	if err == nil {
		t.Fatal("broken migration succeeded")
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='should_rollback'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("failed migration left its table behind")
	}
	if err := db.QueryRow(`SELECT count(*) FROM ` + migrationTable).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("failed migration was recorded")
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(context.Background(), t.TempDir()+"/test.sqlite", Options{BusyTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
