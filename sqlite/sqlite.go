// Package sqlite opens bounded pure-Go SQLite databases and applies embedded migrations.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const migrationTable = "app_schema_migrations"

var migrationName = regexp.MustCompile(`^([0-9]+)_([a-z0-9][a-z0-9_-]*)\.sql$`)

// Options bounds SQLite resources. Zero values select the platform defaults.
type Options struct {
	BusyTimeout  time.Duration
	CacheSizeKiB int
	MaxOpenConns int
	MaxIdleConns int
}

// Migration is one ordered forward-only schema change.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// Open creates the parent directory, opens SQLite with explicit pragmas, verifies connectivity,
// and bounds the connection pool. The caller owns Close.
func Open(ctx context.Context, path string, options Options) (*sql.DB, error) {
	if path == "" {
		return nil, errors.New("SQLite path is empty")
	}
	if err := applyDefaults(&options); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(path, "file:") && path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("create SQLite directory: %w", err)
		}
	}
	dsn := pragmaDSN(path, options)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open SQLite: %w", err)
	}
	db.SetMaxOpenConns(options.MaxOpenConns)
	db.SetMaxIdleConns(options.MaxIdleConns)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping SQLite: %w", err)
	}
	var foreignKeys int
	if err := db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		db.Close()
		if err != nil {
			return nil, fmt.Errorf("verify SQLite foreign keys: %w", err)
		}
		return nil, errors.New("SQLite foreign keys are disabled")
	}
	return db, nil
}

func applyDefaults(options *Options) error {
	if options.BusyTimeout == 0 {
		options.BusyTimeout = 5 * time.Second
	}
	if options.CacheSizeKiB == 0 {
		options.CacheSizeKiB = 4 * 1024
	}
	if options.MaxOpenConns == 0 {
		options.MaxOpenConns = 1
	}
	if options.MaxIdleConns == 0 {
		options.MaxIdleConns = options.MaxOpenConns
	}
	if options.BusyTimeout < time.Millisecond || options.CacheSizeKiB < 1 || options.MaxOpenConns < 1 || options.MaxIdleConns < 0 || options.MaxIdleConns > options.MaxOpenConns {
		return errors.New("invalid SQLite options")
	}
	return nil
}

func pragmaDSN(path string, options Options) string {
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	busyMilliseconds := options.BusyTimeout.Milliseconds()
	return path + separator +
		"_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=busy_timeout(" + strconv.FormatInt(busyMilliseconds, 10) + ")" +
		"&_pragma=cache_size(-" + strconv.Itoa(options.CacheSizeKiB) + ")"
}

// LoadMigrations reads files named <version>_<name>.sql from an embedded or ordinary filesystem.
func LoadMigrations(fsys fs.FS, directory string) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, directory)
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	migrations := make([]Migration, 0, len(entries))
	versions := map[int]bool{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		matches := migrationName.FindStringSubmatch(entry.Name())
		if matches == nil {
			return nil, fmt.Errorf("invalid migration filename %q", entry.Name())
		}
		version, err := strconv.Atoi(matches[1])
		if err != nil || version < 1 || versions[version] {
			return nil, fmt.Errorf("invalid or duplicate migration version in %q", entry.Name())
		}
		body, err := fs.ReadFile(fsys, filepath.ToSlash(filepath.Join(directory, entry.Name())))
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", entry.Name(), err)
		}
		if strings.TrimSpace(string(body)) == "" {
			return nil, fmt.Errorf("migration %q is empty", entry.Name())
		}
		versions[version] = true
		migrations = append(migrations, Migration{Version: version, Name: matches[2], SQL: string(body)})
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Version < migrations[j].Version })
	return migrations, nil
}

// Migrate loads ordered migrations and applies each pending change transactionally.
func Migrate(ctx context.Context, db *sql.DB, fsys fs.FS, directory string) error {
	migrations, err := LoadMigrations(fsys, directory)
	if err != nil {
		return err
	}
	return ApplyMigrations(ctx, db, migrations)
}

// ApplyMigrations applies an already-loaded ordered migration set. Applied versions are
// idempotent; changing the name attached to an applied version is rejected.
func ApplyMigrations(ctx context.Context, db *sql.DB, migrations []Migration) error {
	if db == nil {
		return errors.New("SQLite database is nil")
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS `+migrationTable+` (
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	applied := map[int]string{}
	rows, err := db.QueryContext(ctx, `SELECT version, name FROM `+migrationTable)
	if err != nil {
		return fmt.Errorf("read migration state: %w", err)
	}
	for rows.Next() {
		var version int
		var name string
		if err := rows.Scan(&version, &name); err != nil {
			rows.Close()
			return fmt.Errorf("scan migration state: %w", err)
		}
		applied[version] = name
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate migration state: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close migration state: %w", err)
	}

	lastVersion := 0
	seen := map[int]bool{}
	for _, migration := range migrations {
		if migration.Version < 1 || migration.Name == "" || strings.TrimSpace(migration.SQL) == "" || seen[migration.Version] || migration.Version < lastVersion {
			return fmt.Errorf("invalid or unordered migration %d_%s", migration.Version, migration.Name)
		}
		seen[migration.Version] = true
		lastVersion = migration.Version
		if oldName, ok := applied[migration.Version]; ok {
			if oldName != migration.Name {
				return fmt.Errorf("migration %d was applied as %q, not %q", migration.Version, oldName, migration.Name)
			}
			continue
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d_%s: %w", migration.Version, migration.Name, err)
		}
		if _, err := tx.ExecContext(ctx, migration.SQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %d_%s: %w", migration.Version, migration.Name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO `+migrationTable+` (version, name, applied_at) VALUES (?, ?, ?)`, migration.Version, migration.Name, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d_%s: %w", migration.Version, migration.Name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d_%s: %w", migration.Version, migration.Name, err)
		}
	}
	return nil
}
