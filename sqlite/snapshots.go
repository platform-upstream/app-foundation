package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Periodic snapshots for backups. A file-level backup (Velero's file system backup, rsync) of a
// live SQLite database can copy pages from different moments and restore a torn database. With
// APP_SNAPSHOT_DIR set, every database opened writable with Open also writes a consistent,
// integrity-checked copy (Snapshot) into that directory, at a fixed interval or once a day at a
// set time, and keeps the newest few. A backup of the volume then always holds a recent copy that
// opens cleanly. Restore: stop the app, copy the newest snapshot over the database file, delete
// its -wal and -shm, start.
//
//	APP_SNAPSHOT_DIR       directory on the same volume, e.g. /data/.snapshots (unset: off)
//	APP_SNAPSHOT_INTERVAL  Go duration between snapshots, default 1h, at least 1m outside tests
//	APP_SNAPSHOT_AT        HH:MM (24h): one snapshot a day at that local time, instead of INTERVAL
//	APP_SNAPSHOT_KEEP      snapshots kept per database, default 2
//
// An interval counts from the process start, so a pod that restarts more often than its interval
// never writes one. A daily snapshot comes at the next HH:MM on the wall clock however often the
// pod restarted: set it shortly before the volume backup. AT and INTERVAL exclude each other.
// Local time is the process's: TZ, else /etc/localtime, else UTC. A TZ the binary has no zone
// data for (no tzdata in the image, no time/tzdata import) fails Open instead of running on UTC.
const (
	snapshotDirEnv      = "APP_SNAPSHOT_DIR"
	snapshotIntervalEnv = "APP_SNAPSHOT_INTERVAL"
	snapshotAtEnv       = "APP_SNAPSHOT_AT"
	snapshotKeepEnv     = "APP_SNAPSHOT_KEEP"
)

// minSnapshotInterval guards against a typo that would snapshot continuously; tests lower it.
var minSnapshotInterval = time.Minute

// dailySnapshotTimeout bounds one daily snapshot, as the default interval bounds a periodic one.
const dailySnapshotTimeout = time.Hour

// snapshotClock is the wall clock daily snapshots are scheduled on; tests move it.
var snapshotClock = time.Now

// SnapshotOptions configures StartSnapshots.
type SnapshotOptions struct {
	// Dir receives the snapshots; required.
	Dir string
	// Interval between snapshots; zero means one hour unless At is set.
	Interval time.Duration
	// At, written "HH:MM" (24h), takes one snapshot a day at that local wall-clock time instead of
	// every Interval: the first at its next occurrence, then on each following day. Set At or
	// Interval, not both.
	At string
	// Keep is the number of snapshots kept for this database; zero means 2.
	Keep int
	// Logger receives one line per snapshot or failure; nil uses slog.Default().
	Logger *slog.Logger
}

// SnapshotOptionsFromEnv reads APP_SNAPSHOT_DIR, APP_SNAPSHOT_INTERVAL, APP_SNAPSHOT_AT and
// APP_SNAPSHOT_KEEP. ok is false when APP_SNAPSHOT_DIR is unset (snapshots off). Setting both
// APP_SNAPSHOT_AT and APP_SNAPSHOT_INTERVAL is an error.
func SnapshotOptionsFromEnv() (options SnapshotOptions, ok bool, err error) {
	options.Dir = strings.TrimSpace(os.Getenv(snapshotDirEnv))
	if options.Dir == "" {
		return SnapshotOptions{}, false, nil
	}
	interval := strings.TrimSpace(os.Getenv(snapshotIntervalEnv))
	if interval != "" {
		options.Interval, err = time.ParseDuration(interval)
		if err != nil {
			return SnapshotOptions{}, false, fmt.Errorf("%s: %w", snapshotIntervalEnv, err)
		}
	}
	if options.At = strings.TrimSpace(os.Getenv(snapshotAtEnv)); options.At != "" {
		if interval != "" {
			return SnapshotOptions{}, false, fmt.Errorf("%s and %s are both set: set one (%[1]s once a day at that time, %[2]s every interval)",
				snapshotAtEnv, snapshotIntervalEnv)
		}
		if _, _, err := parseSnapshotAt(options.At); err != nil {
			return SnapshotOptions{}, false, fmt.Errorf("%s: %w", snapshotAtEnv, err)
		}
	}
	if value := strings.TrimSpace(os.Getenv(snapshotKeepEnv)); value != "" {
		options.Keep, err = strconv.Atoi(value)
		if err != nil {
			return SnapshotOptions{}, false, fmt.Errorf("%s: %w", snapshotKeepEnv, err)
		}
	}
	return options, true, nil
}

// StartSnapshots writes a snapshot of db (opened from path) into Dir every Interval, or once a
// day at At, named <database name>-<path hash>-<UTC time>.sqlite, and removes all but the newest
// Keep of them. The first snapshot follows one Interval after the start (none during a rollout's
// start-up), or comes at the next At on the local wall clock. It stops when ctx ends or, noticed
// at the next run, when db is closed. Failures are logged and retried at the next run; the
// earlier snapshots stay.
func StartSnapshots(ctx context.Context, db *sql.DB, path string, options SnapshotOptions) error {
	if db == nil {
		return errors.New("SQLite database is nil")
	}
	if options.Dir == "" {
		return errors.New("snapshot directory is empty")
	}
	daily := options.At != ""
	var hour, minute int
	var timeout time.Duration
	if daily {
		if options.Interval != 0 {
			return errors.New("snapshot At and Interval exclude each other: set one")
		}
		var err error
		if hour, minute, err = parseSnapshotAt(options.At); err != nil {
			return err
		}
		tz, set := os.LookupEnv("TZ")
		if err := localZoneError(tz, set, time.Local); err != nil {
			return err
		}
		timeout = dailySnapshotTimeout
	} else {
		if options.Interval == 0 {
			options.Interval = time.Hour
		}
		if options.Interval < minSnapshotInterval {
			return fmt.Errorf("snapshot interval %s is below %s", options.Interval, minSnapshotInterval)
		}
		timeout = options.Interval
	}
	if options.Keep == 0 {
		options.Keep = 2
	}
	if options.Keep < 1 {
		return errors.New("snapshots to keep must be at least 1")
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	prefix, err := snapshotPrefix(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(options.Dir, 0o700); err != nil {
		return fmt.Errorf("create snapshot directory: %w", err)
	}
	now := snapshotClock
	go func() {
		var ticks <-chan time.Time
		if !daily {
			ticker := time.NewTicker(options.Interval)
			defer ticker.Stop()
			ticks = ticker.C
		}
		for {
			if daily {
				if !sleepUntil(ctx, now, nextDailySnapshot(now(), hour, minute)) {
					return
				}
			} else {
				select {
				case <-ctx.Done():
					return
				case <-ticks:
				}
			}
			if err := db.PingContext(ctx); err != nil {
				if errors.Is(err, sql.ErrConnDone) || strings.Contains(err.Error(), "database is closed") {
					return
				}
			}
			destination := filepath.Join(options.Dir, prefix+time.Now().UTC().Format("20060102T150405.000000000Z")+".sqlite")
			snapshotCtx, cancel := context.WithTimeout(ctx, timeout)
			err := Snapshot(snapshotCtx, db, destination)
			cancel()
			if err != nil {
				if strings.Contains(err.Error(), "database is closed") {
					return
				}
				logger.Warn("sqlite snapshot failed", "database", path, "error", err)
				continue
			}
			removed, err := pruneSnapshots(options.Dir, prefix, options.Keep)
			if err != nil {
				logger.Warn("sqlite snapshot prune failed", "dir", options.Dir, "error", err)
			}
			logger.Info("sqlite snapshot written", "database", path, "snapshot", destination, "removed", removed)
		}
	}()
	return nil
}

// parseSnapshotAt reads a 24h wall-clock time written exactly HH:MM, 00:00 to 23:59.
func parseSnapshotAt(value string) (hour, minute int, err error) {
	digits := func(s string) bool { return strings.Trim(s, "0123456789") == "" }
	if len(value) == 5 && value[2] == ':' && digits(value[:2]) && digits(value[3:]) {
		hour, _ = strconv.Atoi(value[:2])
		minute, _ = strconv.Atoi(value[3:])
		if hour < 24 && minute < 60 {
			return hour, minute, nil
		}
	}
	return 0, 0, fmt.Errorf("snapshot time %q is not HH:MM (24h, 00:00 to 23:59)", value)
}

// localZoneError reports a TZ the process could not load. Go then runs on UTC without a word, and
// a daily snapshot would come hours off its time (and after the backup it should precede).
func localZoneError(tz string, set bool, local *time.Location) error {
	tz = strings.TrimPrefix(tz, ":")
	if !set || tz == "" || tz == "UTC" || local.String() != "UTC" {
		return nil
	}
	return fmt.Errorf("TZ=%s is not a time zone this binary can load (a typo, or no tzdata in the image: install it or import time/tzdata), so daily snapshots would run on UTC", tz)
}

// nextDailySnapshot is the next hour:minute on the wall clock of now's location after now: today
// if still ahead, else tomorrow. Each run is computed afresh from the wall clock, so a DST change
// or a long wait shifts nothing; a time the change skips comes an hour later that day, and of a
// time it repeats only one counts.
func nextDailySnapshot(now time.Time, hour, minute int) time.Time {
	year, month, day := now.Date()
	next := time.Date(year, month, day, hour, minute, 0, 0, now.Location())
	if !next.After(now) {
		next = time.Date(year, month, day+1, hour, minute, 0, 0, now.Location())
	}
	return next
}

// sleepUntil waits until the wall clock reaches due; false when ctx ends first. A timer runs on the
// monotonic clock, so it checks the wall clock again on waking in case it was set back meanwhile.
func sleepUntil(ctx context.Context, now func() time.Time, due time.Time) bool {
	for {
		wait := due.Sub(now())
		if wait <= 0 {
			return true
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}

// snapshotPrefix names a database's snapshots: its file name without extension and a short hash of
// its absolute path, so two databases with the same file name (one per tenant) never share names.
func snapshotPrefix(path string) (string, error) {
	plain := strings.TrimPrefix(path, "file:")
	if i := strings.IndexAny(plain, "?#"); i >= 0 {
		plain = plain[:i]
	}
	if plain == "" || plain == ":memory:" {
		return "", errors.New("snapshots need a database file")
	}
	absolute, err := filepath.Abs(plain)
	if err != nil {
		return "", fmt.Errorf("resolve database path: %w", err)
	}
	sum := sha256.Sum256([]byte(absolute))
	base := strings.TrimSuffix(filepath.Base(absolute), filepath.Ext(absolute))
	return base + "-" + hex.EncodeToString(sum[:4]) + "-", nil
}

// pruneSnapshots removes all but the newest keep snapshots with prefix (names sort by time).
func pruneSnapshots(dir, prefix string, keep int) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() && strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".sqlite") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	removed := 0
	var errs []error
	for len(names) > keep {
		err := os.Remove(filepath.Join(dir, names[0]))
		if err == nil {
			removed++
		} else if !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
		names = names[1:]
	}
	return removed, errors.Join(errs...)
}

// startSnapshotsFromEnv is Open's hook: snapshots for every writable database file when
// APP_SNAPSHOT_DIR is set. A wrong setting is an error at Open, never a silent gap in backups.
func startSnapshotsFromEnv(db *sql.DB, path string) error {
	options, ok, err := SnapshotOptionsFromEnv()
	if err != nil || !ok {
		return err
	}
	if path == ":memory:" || strings.Contains(path, "mode=memory") {
		return nil
	}
	return StartSnapshots(context.Background(), db, path, options)
}
