package identity_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/platform-upstream/app-foundation/identity"
	"github.com/platform-upstream/app-foundation/identity/identitytest"
)

func TestCacheReusesAndExpires(t *testing.T) {
	dir := identitytest.New(alice)
	clock := time.Unix(1_800_000_000, 0)
	c := identity.NewCacheWithClock(dir, identity.CacheOptions{Fresh: time.Minute, Stale: time.Hour, Unknown: 10 * time.Second}, func() time.Time { return clock })
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if p, err := c.ByEmail(ctx, "ALICE@example.org"); err != nil || p.ID != "alice" {
			t.Fatalf("lookup %d: %+v %v", i, p, err)
		}
	}
	// The e-mail lookup also filled the id.
	if _, err := c.ByID(ctx, "alice"); err != nil || dir.Calls() != 1 {
		t.Fatalf("calls %d, %v", dir.Calls(), err)
	}

	// A removed grant takes effect once the entry is no longer fresh.
	dir.Remove("alice")
	clock = clock.Add(61 * time.Second)
	if _, err := c.ByEmail(ctx, "alice@example.org"); !errors.Is(err, identity.ErrUnknown) {
		t.Fatalf("after removal: %v", err)
	}
	// Unknown answers are reused briefly, then asked again.
	calls := dir.Calls()
	if _, err := c.ByEmail(ctx, "alice@example.org"); !errors.Is(err, identity.ErrUnknown) || dir.Calls() != calls {
		t.Fatalf("negative cache: %v calls %d", err, dir.Calls())
	}
	dir.Set(alice)
	clock = clock.Add(11 * time.Second)
	if _, err := c.ByEmail(ctx, "alice@example.org"); err != nil {
		t.Fatalf("granted again: %v", err)
	}
}

func TestCacheServesStaleWhileDirectoryIsDown(t *testing.T) {
	dir := identitytest.New(alice)
	clock := time.Unix(1_800_000_000, 0)
	c := identity.NewCacheWithClock(dir, identity.CacheOptions{Fresh: time.Minute, Stale: time.Hour}, func() time.Time { return clock })
	ctx := context.Background()
	if _, err := c.ByID(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	dir.SetUnavailable(true)
	clock = clock.Add(30 * time.Minute)
	if p, err := c.ByID(ctx, "alice"); err != nil || p.ID != "alice" {
		t.Fatalf("stale within the window: %+v %v", p, err)
	}
	// Nobody new gets in while directory is down.
	if _, err := c.ByID(ctx, "bob"); !errors.Is(err, identity.ErrUnavailable) {
		t.Fatalf("unknown while down: %v", err)
	}
	clock = clock.Add(31 * time.Minute)
	if _, err := c.ByID(ctx, "alice"); !errors.Is(err, identity.ErrUnavailable) {
		t.Fatalf("past the stale window: %v", err)
	}
}

// blocking holds every lookup until release is closed and counts them.
type blocking struct {
	mu      sync.Mutex
	calls   int
	release chan struct{}
}

func (b *blocking) ByEmail(ctx context.Context, email string) (identity.Person, error) {
	return b.ByID(ctx, email)
}

func (b *blocking) ByID(ctx context.Context, _ string) (identity.Person, error) {
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()
	<-b.release
	if ctx.Err() != nil {
		return identity.Person{}, ctx.Err()
	}
	return alice, nil
}

func TestCacheSharesConcurrentLookupsAndOutlivesACancelledCaller(t *testing.T) {
	b := &blocking{release: make(chan struct{})}
	c := identity.NewCache(b, identity.CacheOptions{})
	cancelled, cancel := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, err := c.ByID(cancelled, "alice")
		firstDone <- err
	}()
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.ByID(context.Background(), "alice")
			errs <- err
		}()
	}
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled caller: %v", err)
	}
	close(b.release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("shared lookup: %v", err)
		}
	}
	if b.calls != 1 {
		t.Fatalf("calls = %d, want 1", b.calls)
	}
}

func TestCacheIsBounded(t *testing.T) {
	people := []identity.Person{}
	for _, id := range []string{"a", "b", "c", "d"} {
		people = append(people, identity.Person{ID: id, Role: "member"})
	}
	dir := identitytest.New(people...)
	clock := time.Unix(1_800_000_000, 0)
	c := identity.NewCacheWithClock(dir, identity.CacheOptions{MaxEntries: 2}, func() time.Time { return clock })
	ctx := context.Background()
	for _, p := range people {
		clock = clock.Add(time.Second)
		if _, err := c.ByID(ctx, p.ID); err != nil {
			t.Fatal(err)
		}
	}
	calls := dir.Calls()
	_, _ = c.ByID(ctx, "d") // newest stays
	_, _ = c.ByID(ctx, "a") // oldest was dropped
	if dir.Calls() != calls+1 {
		t.Fatalf("calls %d → %d", calls, dir.Calls())
	}
}
