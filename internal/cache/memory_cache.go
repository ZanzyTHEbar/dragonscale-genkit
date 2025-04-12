package cache

import (
	"context"
	"sync"
	"time"

	"github.com/firebase/genkit/go/genkit"
)

// InMemoryCache provides a simple thread-safe in-memory cache.
type InMemoryCache struct {
	store map[string]cacheItem
	mutex sync.RWMutex
	ttl   time.Duration
}

type cacheItem struct {
	value      interface{}
	expiration int64
}

// NewInMemoryCache creates a new in-memory cache with a default TTL.
func NewInMemoryCache(defaultTTL time.Duration) *InMemoryCache {
	c := &InMemoryCache{
		store: make(map[string]cacheItem),
		ttl:   defaultTTL,
	}
	// Optional: Start a background cleanup goroutine
	// go c.cleanupLoop(10 * time.Minute)
	return c
}

// Get retrieves an item from the cache.
func (c *InMemoryCache) Get(ctx context.Context, key string) (interface{}, bool) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	item, found := c.store[key]
	if !found {
		return nil, false
	}

	if time.Now().UnixNano() > item.expiration {
		// Item expired (lazy cleanup)
		// We could delete it here, but need write lock. For now, just report not found.
		// Potential enhancement: trigger cleanup or delete under RUnlock/Lock sequence.
		genkit.Logger(ctx).Debug("Cache item expired", "key", key)
		return nil, false
	}

	return item.value, true
}

// Set adds or updates an item in the cache.
func (c *InMemoryCache) Set(ctx context.Context, key string, value interface{}) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	expiration := time.Now().Add(c.ttl).UnixNano()
	c.store[key] = cacheItem{
		value:      value,
		expiration: expiration,
	}
	genkit.Logger(ctx).Debug("Cache item set", "key", key)
}

// cleanupLoop periodically removes expired items (optional).
func (c *InMemoryCache) cleanupLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		c.mutex.Lock()
		now := time.Now().UnixNano()
		for key, item := range c.store {
			if now > item.expiration {
				delete(c.store, key)
			}
		}
		c.mutex.Unlock()
	}
}
