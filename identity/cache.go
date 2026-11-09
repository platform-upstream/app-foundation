package identity

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// CacheOptions bound how long answers are reused. Zero values take the defaults.
type CacheOptions struct {
	// Fresh is how long a person is served without asking directory again (default 1m). A removed
	// grant takes effect within Fresh.
	Fresh time.Duration
	// Stale is how much longer a known person is still served while directory is unavailable
	// (default 1h, stale-if-error). Only ErrUnavailable uses it; ErrUnknown never does.
	Stale time.Duration
	// Unknown is how long an ErrUnknown answer is reused (default 10s).
	Unknown time.Duration
	// MaxEntries bounds the cache (default 1024).
	MaxEntries int
}

// Cache is a Resolver that remembers another one's answers. Concurrent lookups of the same key
// share one request; a lookup outlives a cancelled caller so the others still get the answer.
type Cache struct {
	next Resolver
	opts CacheOptions
	now  func() time.Time

	mu       sync.Mutex
	entries  map[string]entry
	inflight map[string]*call
}

type entry struct {
	person  Person
	unknown bool
	at      time.Time
}

type call struct {
	done   chan struct{}
	person Person
	err    error
}

// NewCache wraps next.
func NewCache(next Resolver, opts CacheOptions) *Cache {
	if opts.Fresh <= 0 {
		opts.Fresh = time.Minute
	}
	if opts.Stale <= 0 {
		opts.Stale = time.Hour
	}
	if opts.Unknown <= 0 {
		opts.Unknown = 10 * time.Second
	}
	if opts.MaxEntries <= 0 {
		opts.MaxEntries = 1024
	}
	return &Cache{next: next, opts: opts, now: time.Now, entries: map[string]entry{}, inflight: map[string]*call{}}
}

// ByEmail implements Resolver.
func (c *Cache) ByEmail(ctx context.Context, email string) (Person, error) {
	key := "e:" + strings.ToLower(strings.TrimSpace(email))
	return c.lookup(ctx, key, func(ctx context.Context) (Person, error) { return c.next.ByEmail(ctx, email) })
}

// ByID implements Resolver.
func (c *Cache) ByID(ctx context.Context, id string) (Person, error) {
	return c.lookup(ctx, "i:"+id, func(ctx context.Context) (Person, error) { return c.next.ByID(ctx, id) })
}

// Forget drops everything cached, for an app that learns of a change (tests, an admin action).
func (c *Cache) Forget() {
	c.mu.Lock()
	c.entries = map[string]entry{}
	c.mu.Unlock()
}

func (c *Cache) lookup(ctx context.Context, key string, fetch func(context.Context) (Person, error)) (Person, error) {
	c.mu.Lock()
	now := c.now()
	if e, ok := c.entries[key]; ok {
		if e.unknown && now.Sub(e.at) < c.opts.Unknown {
			c.mu.Unlock()
			return Person{}, ErrUnknown
		}
		if !e.unknown && now.Sub(e.at) < c.opts.Fresh {
			c.mu.Unlock()
			return clone(e.person), nil
		}
	}
	cl, running := c.inflight[key]
	if !running {
		cl = &call{done: make(chan struct{})}
		c.inflight[key] = cl
		go c.run(context.WithoutCancel(ctx), key, cl, fetch)
	}
	c.mu.Unlock()

	select {
	case <-cl.done:
		if cl.err != nil {
			return Person{}, cl.err
		}
		return clone(cl.person), nil
	case <-ctx.Done():
		return Person{}, ctx.Err()
	}
}

func (c *Cache) run(ctx context.Context, key string, cl *call, fetch func(context.Context) (Person, error)) {
	person, err := fetch(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()
	defer close(cl.done)
	delete(c.inflight, key)
	now := c.now()
	switch {
	case err == nil:
		c.store(key, entry{person: person, at: now})
		if strings.HasPrefix(key, "e:") {
			c.store("i:"+person.ID, entry{person: person, at: now})
		}
		cl.person = person
	case errors.Is(err, ErrUnknown):
		c.store(key, entry{unknown: true, at: now})
		cl.err = ErrUnknown
	default:
		// stale-if-error: a person known recently enough still passes while directory is down.
		if e, ok := c.entries[key]; ok && !e.unknown && errors.Is(err, ErrUnavailable) && now.Sub(e.at) < c.opts.Fresh+c.opts.Stale {
			cl.person = e.person
			return
		}
		cl.err = err
	}
}

// store adds e, first dropping expired entries and, when still full, the oldest one.
func (c *Cache) store(key string, e entry) {
	if _, exists := c.entries[key]; !exists && len(c.entries) >= c.opts.MaxEntries {
		now := c.now()
		oldestKey, oldest := "", now
		for k, v := range c.entries {
			if now.Sub(v.at) >= c.opts.Fresh+c.opts.Stale {
				delete(c.entries, k)
				continue
			}
			if v.at.Before(oldest) || oldestKey == "" {
				oldestKey, oldest = k, v.at
			}
		}
		if len(c.entries) >= c.opts.MaxEntries {
			delete(c.entries, oldestKey)
		}
	}
	c.entries[key] = e
}

// clone copies the slices of p, so a caller that changes its answer (sorts, appends, overwrites an
// element) never changes the cached person the next request gets.
func clone(p Person) Person {
	p.Emails = append([]string{}, p.Emails...)
	p.ActsFor = append([]string{}, p.ActsFor...)
	p.Spaces = append([]Space{}, p.Spaces...)
	return p
}
