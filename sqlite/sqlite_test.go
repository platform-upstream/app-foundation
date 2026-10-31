package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
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

func TestOpenReadOnlyRejectsWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.sqlite")
	writable, err := Open(context.Background(), path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writable.Exec(`CREATE TABLE items (id TEXT PRIMARY KEY); INSERT INTO items VALUES ('kept')`); err != nil {
		t.Fatal(err)
	}
	if err := writable.Close(); err != nil {
		t.Fatal(err)
	}

	readOnly, err := OpenReadOnly(context.Background(), path, Options{Immutable: true})
	if err != nil {
		t.Fatal(err)
	}
	defer readOnly.Close()
	var count int
	if err := readOnly.QueryRow(`SELECT count(*) FROM items`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("read count = %d, error = %v", count, err)
	}
	if _, err := readOnly.Exec(`INSERT INTO items VALUES ('blocked')`); err == nil {
		t.Fatal("read-only database accepted a write")
	}
	var queryOnly int
	if err := readOnly.QueryRow(`PRAGMA query_only`).Scan(&queryOnly); err != nil || queryOnly != 1 {
		t.Fatalf("query_only = %d, error = %v", queryOnly, err)
	}
}

func TestOpenReadOnlyRejectsAmbiguousPaths(t *testing.T) {
	for _, path := range []string{"file:app.sqlite", "app?.sqlite", "app#.sqlite", ":memory:"} {
		if _, err := OpenReadOnly(context.Background(), path, Options{}); err == nil {
			t.Errorf("OpenReadOnly accepted %q", path)
		}
	}
}

func TestOpenReadOnlyDoesNotCreateMissingPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "app.sqlite")
	if _, err := OpenReadOnly(context.Background(), path, Options{}); err == nil {
		t.Fatal("OpenReadOnly created a missing database")
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("parent directory exists or stat failed unexpectedly: %v", err)
	}
}

func TestSnapshotIsConsistentAndNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(context.Background(), filepath.Join(dir, "live.sqlite"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE items (id TEXT PRIMARY KEY); INSERT INTO items VALUES ('deleted'), ('in-wal'); DELETE FROM items WHERE id='deleted'`); err != nil {
		t.Fatal(err)
	}
	var sourceRowID int64
	if err := db.QueryRow(`SELECT rowid FROM items WHERE id='in-wal'`).Scan(&sourceRowID); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(dir, "snapshots", "snapshot.sqlite")
	if err := Snapshot(context.Background(), db, destination); err != nil {
		t.Fatal(err)
	}
	snapshot, err := OpenReadOnly(context.Background(), destination, Options{Immutable: true})
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	var id string
	if err := snapshot.QueryRow(`SELECT id FROM items`).Scan(&id); err != nil || id != "in-wal" {
		t.Fatalf("snapshot id = %q, error = %v", id, err)
	}
	var snapshotRowID int64
	if err := snapshot.QueryRow(`SELECT rowid FROM items WHERE id='in-wal'`).Scan(&snapshotRowID); err != nil || snapshotRowID != sourceRowID {
		t.Fatalf("snapshot rowid = %d, source = %d, error = %v", snapshotRowID, sourceRowID, err)
	}
	if err := Snapshot(context.Background(), db, destination); err == nil {
		t.Fatal("Snapshot overwrote an existing destination")
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

func TestApplyMigrationsUpgradesExistingSchema(t *testing.T) {
	db := openTestDB(t)
	first := Migration{Version: 1, Name: "initial", SQL: `CREATE TABLE items (id TEXT PRIMARY KEY);`}
	second := Migration{Version: 2, Name: "add_title", SQL: `ALTER TABLE items ADD COLUMN title TEXT NOT NULL DEFAULT '';`}
	if err := ApplyMigrations(context.Background(), db, []Migration{first}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO items (id) VALUES ('kept')`); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(context.Background(), db, []Migration{first, second}); err != nil {
		t.Fatal(err)
	}
	var title string
	if err := db.QueryRow(`SELECT title FROM items WHERE id='kept'`).Scan(&title); err != nil || title != "" {
		t.Fatalf("upgraded title = %q, error = %v", title, err)
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

func TestMigrateRejectsRemovedOrRenamedAppliedMigration(t *testing.T) {
	db := openTestDB(t)
	original := []Migration{{Version: 1, Name: "initial", SQL: `CREATE TABLE items (id TEXT PRIMARY KEY);`}}
	if err := ApplyMigrations(context.Background(), db, original); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(context.Background(), db, nil); err == nil {
		t.Fatal("removed applied migration was accepted")
	}
	renamed := []Migration{{Version: 1, Name: "renamed", SQL: original[0].SQL}}
	if err := ApplyMigrations(context.Background(), db, renamed); err == nil {
		t.Fatal("renamed applied migration was accepted")
	}
}

func TestInvalidMigrationSetDoesNotCreateStateTable(t *testing.T) {
	db := openTestDB(t)
	err := ApplyMigrations(context.Background(), db, []Migration{{Version: 1, Name: "Invalid Name", SQL: "SELECT 1"}})
	if err == nil {
		t.Fatal("invalid migration set was accepted")
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, migrationTable).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("invalid migration set mutated the database")
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

// Two handles on one file stand in for the old and the new pod during a rolling update. A
// transaction that reads and then writes must not fail with SQLITE_BUSY_SNAPSHOT when the other
// process commits in between: with BEGIN IMMEDIATE the other writer waits instead.
func TestOverlappingProcessesDoNotFailWithBusySnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.sqlite")
	ctx := context.Background()
	oldPod, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer oldPod.Close()
	newPod, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer newPod.Close()
	if _, err := oldPod.Exec(`CREATE TABLE counter (n INTEGER NOT NULL); INSERT INTO counter VALUES (0)`); err != nil {
		t.Fatal(err)
	}

	tx, err := oldPod.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := tx.QueryRow(`SELECT n FROM counter`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	other := make(chan error, 1)
	go func() {
		_, err := newPod.Exec(`UPDATE counter SET n = n + 10`)
		other <- err
	}()
	select {
	case err := <-other:
		t.Fatalf("the other process wrote during an immediate transaction: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	if _, err := tx.Exec(`UPDATE counter SET n = ?`, n+1); err != nil {
		t.Fatalf("write after read: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-other; err != nil {
		t.Fatalf("the waiting writer failed: %v", err)
	}
	if err := oldPod.QueryRow(`SELECT n FROM counter`).Scan(&n); err != nil || n != 11 {
		t.Fatalf("n = %d, error = %v", n, err)
	}

	// Read-only transactions stay deferred and do not take the write lock.
	readTx, err := oldPod.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer readTx.Rollback()
	if err := readTx.QueryRow(`SELECT n FROM counter`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if _, err := newPod.Exec(`UPDATE counter SET n = 0`); err != nil {
		t.Fatalf("a read-only transaction blocked a writer: %v", err)
	}
}

// The failure the default avoids: a deferred transaction whose snapshot went stale cannot write,
// and busy_timeout does not help.
func TestDeferredTransactionFailsAfterConcurrentCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.sqlite")
	ctx := context.Background()
	oldPod, err := Open(ctx, "file:"+filepath.ToSlash(path)+"?_txlock=deferred", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer oldPod.Close()
	newPod, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer newPod.Close()
	if _, err := newPod.Exec(`CREATE TABLE counter (n INTEGER NOT NULL); INSERT INTO counter VALUES (0)`); err != nil {
		t.Fatal(err)
	}
	tx, err := oldPod.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(`SELECT n FROM counter`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if _, err := newPod.Exec(`UPDATE counter SET n = n + 10`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE counter SET n = ?`, n+1); err == nil {
		t.Fatal("a stale deferred transaction wrote; the immediate default would be unnecessary")
	}
}

func TestSnapshotWithoutHardLinks(t *testing.T) {
	original := link
	link = func(string, string) error {
		return &os.LinkError{Op: "link", Err: errors.New("operation not supported")}
	}
	defer func() { link = original }()

	dir := t.TempDir()
	db, err := Open(context.Background(), filepath.Join(dir, "live.sqlite"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE items (id TEXT PRIMARY KEY); INSERT INTO items VALUES ('kept')`); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(dir, "snapshots", "snapshot.sqlite")
	if err := Snapshot(context.Background(), db, destination); err != nil {
		t.Fatal(err)
	}
	snapshot, err := OpenReadOnly(context.Background(), destination, Options{Immutable: true})
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	var id string
	if err := snapshot.QueryRow(`SELECT id FROM items`).Scan(&id); err != nil || id != "kept" {
		t.Fatalf("snapshot id = %q, error = %v", id, err)
	}
	entries, err := os.ReadDir(filepath.Dir(destination))
	if err != nil || len(entries) != 1 {
		t.Fatalf("snapshot directory holds %v (%v), want only the snapshot", entries, err)
	}
	if err := Snapshot(context.Background(), db, destination); err == nil {
		t.Fatal("Snapshot overwrote an existing destination")
	}
}
