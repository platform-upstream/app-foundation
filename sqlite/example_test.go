package sqlite_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing/fstest"
	"time"

	"github.com/platform-upstream/app-foundation/sqlite"
)

// Open with the defaults, apply the embedded migrations, write in a transaction (BEGIN IMMEDIATE),
// read in a read-only one, and take a snapshot. In an app the migrations are an embed.FS
// (//go:embed *.sql) and the path comes from the environment.
func Example() {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "example")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	db, err := sqlite.Open(ctx, filepath.Join(dir, "app.sqlite"), sqlite.Options{})
	if err != nil {
		panic(err)
	}
	defer db.Close()

	migrations := fstest.MapFS{
		"001_initial.sql":  {Data: []byte(`CREATE TABLE notes (id INTEGER PRIMARY KEY, text TEXT NOT NULL);`)},
		"002_note_tag.sql": {Data: []byte(`ALTER TABLE notes ADD COLUMN tag TEXT NOT NULL DEFAULT '';`)}, // additive
	}
	if err := sqlite.Migrate(ctx, db, migrations, "."); err != nil {
		panic(err)
	}

	tx, err := db.BeginTx(ctx, nil) // takes the write lock up front
	if err != nil {
		panic(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO notes (text, tag) VALUES ('milk', 'shopping')`); err != nil {
		_ = tx.Rollback()
		panic(err)
	}
	if err := tx.Commit(); err != nil {
		panic(err)
	}

	read, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true}) // deferred: no write lock
	if err != nil {
		panic(err)
	}
	var count int
	if err := read.QueryRowContext(ctx, `SELECT count(*) FROM notes`).Scan(&count); err != nil {
		panic(err)
	}
	_ = read.Rollback()
	fmt.Println("notes:", count)

	// Snapshots need a deadline; the destination must not exist yet.
	snapshotCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	destination := filepath.Join(dir, "backups", "app-1.sqlite")
	if err := sqlite.Snapshot(snapshotCtx, db, destination); err != nil {
		panic(err)
	}
	snapshot, err := sqlite.OpenReadOnly(ctx, destination, sqlite.Options{Immutable: true})
	if err != nil {
		panic(err)
	}
	defer snapshot.Close()
	if err := snapshot.QueryRowContext(ctx, `SELECT count(*) FROM notes`).Scan(&count); err != nil {
		panic(err)
	}
	fmt.Println("in snapshot:", count)
	// Output:
	// notes: 1
	// in snapshot: 1
}
