package identity

import "time"

// NewCacheWithClock is NewCache with a test clock.
func NewCacheWithClock(next Resolver, opts CacheOptions, now func() time.Time) *Cache {
	c := NewCache(next, opts)
	c.now = now
	return c
}
