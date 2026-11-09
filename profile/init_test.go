package profile

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/platform-upstream/app-foundation/sqlite"
)

// The settings table is created on first use. A first request that fails (its client went away,
// the database was busy) must not leave the handler broken for every later request.
func TestSettingsTableIsCreatedAfterAFailedFirstUse(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "profile.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	h := New(db, nil, "/api/me")

	gone, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.Get(gone, "alice"); err == nil {
		t.Fatal("Get with a cancelled context succeeded")
	}
	if _, err := h.Get(context.Background(), "alice"); err != nil {
		t.Fatalf("Get after a failed first use: %v", err)
	}
}
