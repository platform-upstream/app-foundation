package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // the zone tests below need no zone files on the machine
)

func lowerSnapshotInterval(t *testing.T) {
	t.Helper()
	previous := minSnapshotInterval
	minSnapshotInterval = time.Millisecond
	t.Cleanup(func() { minSnapshotInterval = previous })
}

// setSnapshotClock makes the scheduling clock read now at this moment and run on from there.
func setSnapshotClock(t *testing.T, now time.Time) {
	t.Helper()
	previous := snapshotClock
	start := time.Now()
	snapshotClock = func() time.Time { return now.Add(time.Since(start)) }
	t.Cleanup(func() { snapshotClock = previous })
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
		{snapshotAtEnv, "2:15"},
		{snapshotAtEnv, "24:00"},
		{snapshotAtEnv, "02:60"},
		{snapshotAtEnv, "02:15:00"},
		{snapshotAtEnv, "0215"},
		{snapshotAtEnv, "+2:15"},
		{snapshotAtEnv, "two"},
		{snapshotKeepEnv, "0x"},
		{snapshotKeepEnv, "-1"},
	} {
		t.Run(tc.env+"="+tc.value, func(t *testing.T) {
			t.Setenv(snapshotIntervalEnv, "")
			t.Setenv(snapshotAtEnv, "")
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

func TestDailySnapshotComesAtItsTimeAndThenWaitsForTheNextDay(t *testing.T) {
	// The pod started at 02:14:59.5 local time; 02:15 is half a second away, the next a day later.
	setSnapshotClock(t, time.Date(2026, 6, 10, 2, 14, 59, 500_000_000, time.Local))
	dir := t.TempDir()
	snapshots := filepath.Join(dir, ".snapshots")
	t.Setenv(snapshotDirEnv, snapshots)
	t.Setenv(snapshotIntervalEnv, "")
	t.Setenv(snapshotAtEnv, "02:15")
	db, err := Open(context.Background(), filepath.Join(dir, "app.sqlite"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if names := snapshotFiles(t, snapshots); len(names) != 0 {
		t.Fatalf("snapshot before 02:15: %v", names)
	}
	waitFor(t, "the 02:15 snapshot", func() bool { return len(snapshotFiles(t, snapshots)) == 1 })
	time.Sleep(300 * time.Millisecond)
	if names := snapshotFiles(t, snapshots); len(names) != 1 {
		t.Fatalf("snapshots after 02:15 = %v, want the one until tomorrow", names)
	}
}

func TestNextDailySnapshotFollowsTheWallClock(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	at := func(year int, month time.Month, day, hour, minute int) time.Time {
		return time.Date(year, month, day, hour, minute, 0, 0, berlin)
	}
	for _, tc := range []struct {
		name         string
		now          time.Time
		hour, minute int
		want         time.Time
	}{
		{"later today", at(2026, 6, 10, 1, 0), 2, 15, at(2026, 6, 10, 2, 15)},
		{"right at the time: tomorrow", at(2026, 6, 10, 2, 15), 2, 15, at(2026, 6, 11, 2, 15)},
		{"passed: tomorrow", at(2026, 6, 10, 9, 30), 2, 15, at(2026, 6, 11, 2, 15)},
		{"midnight", at(2026, 6, 10, 23, 59), 0, 0, at(2026, 6, 11, 0, 0)},
		{"new year", at(2026, 12, 31, 23, 30), 0, 15, at(2027, 1, 1, 0, 15)},
		// 2026-03-29 has 23 hours and 2026-10-25 has 25: the time stays, the gap does not.
		{"over the spring change", at(2026, 3, 28, 4, 0), 4, 0, at(2026, 3, 29, 4, 0)},
		{"over the autumn change", at(2026, 10, 24, 4, 0), 4, 0, at(2026, 10, 25, 4, 0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextDailySnapshot(tc.now, tc.hour, tc.minute); !got.Equal(tc.want) {
				t.Fatalf("next after %s = %s, want %s", tc.now, got, tc.want)
			}
		})
	}
	if gap := at(2026, 3, 29, 4, 0).Sub(at(2026, 3, 28, 4, 0)); gap != 23*time.Hour {
		t.Fatalf("spring change day lasts %s", gap)
	}

	// 02:30 does not exist on 2026-03-29 and exists twice on 2026-10-25: once that day either way.
	for _, day := range []time.Time{at(2026, 3, 29, 0, 0), at(2026, 10, 25, 0, 0)} {
		first := nextDailySnapshot(day, 2, 30)
		if y, m, d := first.Date(); y != day.Year() || m != day.Month() || d != day.Day() {
			t.Fatalf("02:30 on %s came on %s", day.Format(time.DateOnly), first)
		}
		if second := nextDailySnapshot(first, 2, 30); !second.Equal(at(day.Year(), day.Month(), day.Day()+1, 2, 30)) {
			t.Fatalf("after %s came %s, want 02:30 the next day", first, second)
		}
	}
}

func TestSnapshotAtIsStrictHHMM(t *testing.T) {
	for value, want := range map[string][2]int{"00:00": {0, 0}, "02:15": {2, 15}, "23:59": {23, 59}} {
		hour, minute, err := parseSnapshotAt(value)
		if err != nil || hour != want[0] || minute != want[1] {
			t.Fatalf("parseSnapshotAt(%q) = %d, %d, %v", value, hour, minute, err)
		}
	}
	for _, value := range []string{"", "2:15", "02:5", "24:00", "23:60", "-1:00", "02.15", "02:15 ", "02:15:00"} {
		if _, _, err := parseSnapshotAt(value); err == nil {
			t.Fatalf("parseSnapshotAt accepted %q", value)
		}
	}
}

func TestSnapshotAtAndIntervalExcludeEachOther(t *testing.T) {
	t.Setenv(snapshotDirEnv, t.TempDir())
	t.Setenv(snapshotAtEnv, "02:15")
	t.Setenv(snapshotIntervalEnv, "24h")
	_, _, err := SnapshotOptionsFromEnv()
	if err == nil || !strings.Contains(err.Error(), snapshotAtEnv) || !strings.Contains(err.Error(), snapshotIntervalEnv) {
		t.Fatalf("both set: %v", err)
	}
	t.Setenv(snapshotIntervalEnv, "")
	options, ok, err := SnapshotOptionsFromEnv()
	if err != nil || !ok || options.At != "02:15" || options.Interval != 0 {
		t.Fatalf("AT alone = %+v, %v, %v", options, ok, err)
	}

	db, err := Open(context.Background(), ":memory:", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	options.Interval = time.Hour
	if err := StartSnapshots(context.Background(), db, filepath.Join(t.TempDir(), "app.sqlite"), options); err == nil {
		t.Fatal("StartSnapshots accepted At and Interval")
	}
}

func TestUnloadableTZFailsDailySnapshots(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		tz     string
		set    bool
		local  *time.Location
		failed bool
	}{
		{"", false, time.UTC, false},           // no TZ, no /etc/localtime: UTC is the local time
		{"", true, time.UTC, false},            // TZ= asks for UTC
		{"UTC", true, time.UTC, false},         // so does TZ=UTC
		{"Europe/Berlin", true, berlin, false}, // loaded
		{"Europe/Berlin", true, time.UTC, true},
		{":Europe/Berlin", true, time.UTC, true},
	} {
		if err := localZoneError(tc.tz, tc.set, tc.local); (err != nil) != tc.failed {
			t.Fatalf("TZ=%q (set %v) on %s: %v", tc.tz, tc.set, tc.local, err)
		}
	}
}
