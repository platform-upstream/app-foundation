package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func lowerSnapshotInterval(t *testing.T) {
	t.Helper()
	previous := minSnapshotInterval
	minSnapshotInterval = time.Millisecond
	t.Cleanup(func() { minSnapshotInterval = previous })
}

func snapshotFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sqlite") && !strings.HasPrefix(entry.Name(), ".") {
			names = append(names, entry.Name())
		}
	}
	return names
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestOpenWithoutSnapshotDirWritesNoSnapshots(t *testing.T) {
	t.Setenv(snapshotDirEnv, "")
	dir := t.TempDir()
	db, err := Open(context.Background(), filepath.Join(dir, "app.sqlite"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := os.Stat(filepath.Join(dir, ".snapshots")); !os.IsNotExist(err) {
		t.Fatalf("snapshot directory created without %s: %v", snapshotDirEnv, err)
	}
}

func TestOpenWritesSnapshotsAndKeepsTheNewest(t *testing.T) {
	lowerSnapshotInterval(t)
	dir := t.TempDir()
	snapshots := filepath.Join(dir, ".snapshots")
	t.Setenv(snapshotDirEnv, snapshots)
	t.Setenv(snapshotIntervalEnv, "30ms")
	t.Setenv(snapshotKeepEnv, "2")
	db, err := Open(context.Background(), filepath.Join(dir, "app.sqlite"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE notes (body TEXT); INSERT INTO notes VALUES ('kept')"); err != nil {
		t.Fatal(err)
	}
	// Several ticks: at least three snapshots were written, only two are kept.
	time.Sleep(300 * time.Millisecond)
	waitFor(t, "two snapshots", func() bool { return len(snapshotFiles(t, snapshots)) == 2 })
	names := snapshotFiles(t, snapshots)
	for _, name := range names {
		if !strings.HasPrefix(name, "app-") {
			t.Fatalf("snapshot %q is not named after the database", name)
		}
	}
	db.Close()

	copyDB, err := OpenReadOnly(context.Background(), filepath.Join(snapshots, names[len(names)-1]), Options{Immutable: true})
	if err != nil {
		t.Fatal(err)
	}
	defer copyDB.Close()
	var body string
	if err := copyDB.QueryRow("SELECT body FROM notes").Scan(&body); err != nil || body != "kept" {
		t.Fatalf("snapshot content = %q, %v", body, err)
	}
}

func TestSnapshotsStopWhenTheDatabaseCloses(t *testing.T) {
	lowerSnapshotInterval(t)
	dir := t.TempDir()
	snapshots := filepath.Join(dir, ".snapshots")
	t.Setenv(snapshotDirEnv, snapshots)
	t.Setenv(snapshotIntervalEnv, "20ms")
	db, err := Open(context.Background(), filepath.Join(dir, "app.sqlite"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a snapshot", func() bool { return len(snapshotFiles(t, snapshots)) > 0 })
	db.Close()
	time.Sleep(60 * time.Millisecond)
	before := snapshotFiles(t, snapshots)
	time.Sleep(150 * time.Millisecond)
	if after := snapshotFiles(t, snapshots); len(after) != len(before) || after[len(after)-1] != before[len(before)-1] {
		t.Fatalf("snapshots continued after Close: %v -> %v", before, after)
	}
}

func TestTwoDatabasesWithTheSameFileNameDoNotShareSnapshots(t *testing.T) {
	a, err := snapshotPrefix("/data/people/a/paper.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	b, err := snapshotPrefix("file:/data/people/b/paper.sqlite?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	if a == b || !strings.HasPrefix(a, "paper-") || !strings.HasPrefix(b, "paper-") {
		t.Fatalf("prefixes %q and %q", a, b)
	}
}

func TestWrongSnapshotSettingsFailOpen(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(snapshotDirEnv, filepath.Join(dir, ".snapshots"))
	for _, tc := range []struct{ env, value string }{
		{snapshotIntervalEnv, "hourly"},
		{snapshotIntervalEnv, "1s"},
		{snapshotKeepEnv, "0x"},
		{snapshotKeepEnv, "-1"},
	} {
		t.Run(tc.env+"="+tc.value, func(t *testing.T) {
			t.Setenv(snapshotIntervalEnv, "")
			t.Setenv(snapshotKeepEnv, "")
			t.Setenv(tc.env, tc.value)
			db, err := Open(context.Background(), filepath.Join(dir, "app.sqlite"), Options{})
			if err == nil {
				db.Close()
				t.Fatalf("Open accepted %s=%s", tc.env, tc.value)
			}
		})
	}
}

func TestReadOnlyAndMemoryDatabasesWriteNoSnapshots(t *testing.T) {
	lowerSnapshotInterval(t)
	dir := t.TempDir()
	snapshots := filepath.Join(dir, ".snapshots")
	t.Setenv(snapshotDirEnv, snapshots)
	t.Setenv(snapshotIntervalEnv, "20ms")
	t.Setenv(snapshotDirEnv, "")
	path := filepath.Join(dir, "app.sqlite")
	writable, err := Open(context.Background(), path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	writable.Close()
	t.Setenv(snapshotDirEnv, snapshots)
	readOnly, err := OpenReadOnly(context.Background(), path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer readOnly.Close()
	memory, err := Open(context.Background(), ":memory:", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	time.Sleep(120 * time.Millisecond)
	if _, err := os.Stat(snapshots); !os.IsNotExist(err) {
		t.Fatalf("read-only or in-memory database wrote snapshots: %v", err)
	}
}
