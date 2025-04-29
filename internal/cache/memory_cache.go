package cache

import (
"context"
"sync"
"time"

"github.com/ZanzyTHEbar/errbuilder-go"
)

// InMemoryCache provides a simple thread-safe in-memory cache.
type InMemoryCache struct {
	store  map[string]cacheItem
	mutex  sync.RWMutex
	ttl    time.Duration
	logger Logger
}

type cacheItem struct {
	value      interface{}
	expiration int64
}

// NewInMemoryCache creates a new in-memory cache with a default TTL.
func NewInMemoryCache(defaultTTL time.Duration) *InMemoryCache {
	return NewInMemoryCacheWithLogger(defaultTTL, &StdLogger{})
}

// NewInMemoryCacheWithLogger creates a new in-memory cache with a default TTL and custom logger.
func NewInMemoryCacheWithLogger(defaultTTL time.Duration, logger Logger) *InMemoryCache {
	c := &InMemoryCache{
		store:  make(map[string]cacheItem),
		ttl:    defaultTTL,
		logger: logger,
	}
	// Start a background cleanup goroutine
	go c.cleanupLoop(10 * time.Minute)
	return c
}

// Get retrieves an item from the cache.
func (c *InMemoryCache) Get(ctx context.Context, key string) (interface{}, error) {
	// Check context cancellation first
	if err := errbuilder.WrapIfContextDone(ctx, nil); err != nil {
		return nil, err
	}

	c.mutex.RLock()
	defer c.mutex.RUnlock()

	item, found := c.store[key]
	if !found {
		return nil, errbuilder.NotFoundErr(errbuilder.GenericErr("cache item not found", nil))
	}

	if time.Now().UnixNano() > item.expiration {
		// Item expired (lazy cleanup)
		if c.logger != nil {
			c.logger.Info("Cache item expired", map[string]interface{}{"key": key})
		}
		return nil, errbuilder.NotFoundErr(errbuilder.GenericErr("cache item expired", nil))
	}

	return item.value, nil
}

// Set adds or updates an item in the cache.
func (c *InMemoryCache) Set(ctx context.Context, key string, value interface{}) error {
	// Check context cancellation first
	if err := errbuilder.WrapIfContextDone(ctx, nil); err != nil {
		return err
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	expiration := time.Now().Add(c.ttl).UnixNano()
	c.store[key] = cacheItem{
		value:      value,
		expiration: expiration,
	}
	if c.logger != nil {
		c.logger.Info("Cache item set", map[string]interface{}{"key": key})
	}
	return nil
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
