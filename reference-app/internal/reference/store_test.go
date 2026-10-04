package reference

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/niiku-platform/app-foundation/reference-app/migrations"
	foundationsqlite "github.com/niiku-platform/app-foundation/sqlite"
)

func TestFreshAndUpgradeMigrations(t *testing.T) {
	ctx := context.Background()
	t.Run("fresh", func(t *testing.T) {
		db, err := OpenStore(ctx, filepath.Join(t.TempDir(), "fresh.sqlite"), migrations.Files)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM app_schema_migrations").Scan(&count); err != nil || count != 2 {
			t.Fatalf("migration count = %d, %v", count, err)
		}
		if _, err := AddItem(ctx, db, "fresh item"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("upgrade", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "upgrade.sqlite")
		db, err := foundationsqlite.Open(ctx, path, foundationsqlite.Options{})
		if err != nil {
			t.Fatal(err)
		}
		first, err := fs.ReadFile(migrations.Files, "001_initial.sql")
		if err != nil {
			t.Fatal(err)
		}
		if err := foundationsqlite.ApplyMigrations(ctx, db, []foundationsqlite.Migration{{Version: 1, Name: "initial", SQL: string(first)}}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("INSERT INTO items(name,created_at) VALUES('kept','2026-10-04T00:00:00Z')"); err != nil {
			t.Fatal(err)
		}
		if err := foundationsqlite.Migrate(ctx, db, migrations.Files, "."); err != nil {
			t.Fatal(err)
		}
		var state string
		if err := db.QueryRow("SELECT state FROM items WHERE name='kept'").Scan(&state); err != nil || state != "active" {
			t.Fatalf("upgraded state = %q, %v", state, err)
		}
		_ = db.Close()
	})
}

func TestFailedMigrationRollsBackAndCloses(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "failed.sqlite")
	bad := fstest.MapFS{
		"001_initial.sql":     {Data: []byte("CREATE TABLE stable(id INTEGER PRIMARY KEY);")},
		"002_broken_step.sql": {Data: []byte("CREATE TABLE rolled_back(id INTEGER); THIS IS NOT SQL;")},
	}
	if db, err := OpenStore(ctx, path, bad); err == nil {
		db.Close()
		t.Fatal("broken migration succeeded")
	}
	db, err := foundationsqlite.Open(ctx, path, foundationsqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var migrationsApplied int
	if err := db.QueryRow("SELECT COUNT(*) FROM app_schema_migrations").Scan(&migrationsApplied); err != nil || migrationsApplied != 1 {
		t.Fatalf("applied migrations = %d, %v", migrationsApplied, err)
	}
	var rolledBack int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='rolled_back'").Scan(&rolledBack); err != nil || rolledBack != 0 {
		t.Fatalf("rolled-back table count = %d, %v", rolledBack, err)
	}
}

func TestReadOnlySnapshotAndRestore(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.sqlite")
	db, err := OpenStore(ctx, sourcePath, migrations.Files)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AddItem(ctx, db, "snapshot item"); err != nil {
		t.Fatal(err)
	}
	snapshotPath := filepath.Join(directory, "snapshot.sqlite")
	if err := foundationsqlite.Snapshot(ctx, db, snapshotPath); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	readOnly, err := foundationsqlite.OpenReadOnly(ctx, snapshotPath, foundationsqlite.Options{Immutable: true})
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := readOnly.QueryRow("SELECT COUNT(*) FROM items").Scan(&count); err != nil || count != 1 {
		t.Fatalf("snapshot items = %d, %v", count, err)
	}
	if _, err := readOnly.Exec("INSERT INTO items(name,created_at) VALUES('forbidden','now')"); err == nil {
		t.Fatal("read-only database accepted a write")
	}
	_ = readOnly.Close()

	raw, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	restorePath := filepath.Join(directory, "restore.sqlite")
	if err := os.WriteFile(restorePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	restored, err := OpenStore(ctx, restorePath, migrations.Files)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var integrity string
	if err := restored.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("restored integrity = %q, %v", integrity, err)
	}
	items, err := ListItems(ctx, restored)
	if err != nil || len(items) != 1 || items[0].Name != "snapshot item" {
		t.Fatalf("restored items = %#v, %v", items, err)
	}
}
