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
// integrity-checked copy (Snapshot) into that directory at a fixed interval and keeps the newest
// few. A backup of the volume then always holds a recent copy that opens cleanly. Restore: stop
// the app, copy the newest snapshot over the database file, delete its -wal and -shm, start.
//
//	APP_SNAPSHOT_DIR       directory on the same volume, e.g. /data/.snapshots (unset: off)
//	APP_SNAPSHOT_INTERVAL  Go duration between snapshots, default 1h, at least 1m outside tests
//	APP_SNAPSHOT_KEEP      snapshots kept per database, default 2
const (
	snapshotDirEnv      = "APP_SNAPSHOT_DIR"
	snapshotIntervalEnv = "APP_SNAPSHOT_INTERVAL"
	snapshotKeepEnv     = "APP_SNAPSHOT_KEEP"
)

// minSnapshotInterval guards against a typo that would snapshot continuously; tests lower it.
var minSnapshotInterval = time.Minute

// SnapshotOptions configures StartSnapshots.
type SnapshotOptions struct {
	// Dir receives the snapshots; required.
	Dir string
	// Interval between snapshots; zero means one hour.
	Interval time.Duration
	// Keep is the number of snapshots kept for this database; zero means 2.
	Keep int
	// Logger receives one line per snapshot or failure; nil uses slog.Default().
	Logger *slog.Logger
}

// SnapshotOptionsFromEnv reads APP_SNAPSHOT_DIR, APP_SNAPSHOT_INTERVAL and APP_SNAPSHOT_KEEP.
// ok is false when APP_SNAPSHOT_DIR is unset (snapshots off).
func SnapshotOptionsFromEnv() (options SnapshotOptions, ok bool, err error) {
	options.Dir = strings.TrimSpace(os.Getenv(snapshotDirEnv))
	if options.Dir == "" {
		return SnapshotOptions{}, false, nil
	}
	if value := strings.TrimSpace(os.Getenv(snapshotIntervalEnv)); value != "" {
		options.Interval, err = time.ParseDuration(value)
		if err != nil {
			return SnapshotOptions{}, false, fmt.Errorf("%s: %w", snapshotIntervalEnv, err)
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

// StartSnapshots writes a snapshot of db (opened from path) every Interval into Dir, named
// <database name>-<path hash>-<UTC time>.sqlite, and removes all but the newest Keep of them. The
// first snapshot follows one Interval after the start (none during a rollout's start-up). It stops
// when ctx ends or db is closed. Failures are logged and retried at the next tick.
func StartSnapshots(ctx context.Context, db *sql.DB, path string, options SnapshotOptions) error {
	if db == nil {
		return errors.New("SQLite database is nil")
	}
	if options.Dir == "" {
		return errors.New("snapshot directory is empty")
	}
	if options.Interval == 0 {
		options.Interval = time.Hour
	}
	if options.Interval < minSnapshotInterval {
		return fmt.Errorf("snapshot interval %s is below %s", options.Interval, minSnapshotInterval)
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
	go func() {
		ticker := time.NewTicker(options.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if err := db.PingContext(ctx); err != nil {
				if errors.Is(err, sql.ErrConnDone) || strings.Contains(err.Error(), "database is closed") {
					return
				}
			}
			destination := filepath.Join(options.Dir, prefix+time.Now().UTC().Format("20060102T150405.000000000Z")+".sqlite")
			snapshotCtx, cancel := context.WithTimeout(ctx, options.Interval)
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
