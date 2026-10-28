package reference

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"time"

	foundationsqlite "github.com/castillo-platform/app-foundation/sqlite"
)

type Item struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	State     string `json:"state"`
	CreatedAt string `json:"createdAt"`
}

func OpenStore(ctx context.Context, path string, migrationFiles fs.FS) (*sql.DB, error) {
	db, err := foundationsqlite.Open(ctx, path, foundationsqlite.Options{})
	if err != nil {
		return nil, err
	}
	if err := foundationsqlite.Migrate(ctx, db, migrationFiles, "."); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate reference database: %w", err)
	}
	return db, nil
}

func ListItems(ctx context.Context, db *sql.DB) ([]Item, error) {
	rows, err := db.QueryContext(ctx, "SELECT id,name,state,created_at FROM items ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Item, 0)
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.ID, &item.Name, &item.State, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func AddItem(ctx context.Context, db *sql.DB, name string) (Item, error) {
	createdAt := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := db.ExecContext(ctx, "INSERT INTO items(name,created_at) VALUES(?,?)", name, createdAt)
	if err != nil {
		return Item{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Item{}, err
	}
	return Item{ID: id, Name: name, State: "active", CreatedAt: createdAt}, nil
}
