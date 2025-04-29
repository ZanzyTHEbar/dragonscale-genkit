package cache

import (
"context"
"encoding/json"
"os"
"sync"
"time"
)

// FilePersistentCache provides a simple file-backed persistent cache.
type FilePersistentCache struct {
store     map[string]cacheItem
mutex     sync.RWMutex
ttl       time.Duration
filePath  string
logger    Logger
}

// NewFilePersistentCache creates a new persistent cache with a default TTL and file path.
func NewFilePersistentCache(defaultTTL time.Duration, filePath string, logger Logger) *FilePersistentCache {
c := &FilePersistentCache{
store:    make(map[string]cacheItem),
ttl:      defaultTTL,
filePath: filePath,
logger:   logger,
}
c.loadFromFile()
go c.cleanupLoop(10 * time.Minute)
return c
}

// loadFromFile loads cache items from the file.
func (c *FilePersistentCache) loadFromFile() {
c.mutex.Lock()
defer c.mutex.Unlock()
file, err := os.Open(c.filePath)
if err != nil {
return
}
defer file.Close()
decoder := json.NewDecoder(file)
_ = decoder.Decode(&c.store)
}

// saveToFile saves cache items to the file.
func (c *FilePersistentCache) saveToFile() {
c.mutex.RLock()
defer c.mutex.RUnlock()
file, err := os.Create(c.filePath)
if err != nil {
return
}
defer file.Close()
encoder := json.NewEncoder(file)
_ = encoder.Encode(c.store)
}

// Get retrieves an item from the cache.
func (c *FilePersistentCache) Get(ctx context.Context, key string) (interface{}, error) {
c.mutex.RLock()
item, found := c.store[key]
c.mutex.RUnlock()
if !found {
return nil, nil
}
if time.Now().UnixNano() > item.expiration {
if c.logger != nil {
c.logger.Info("Persistent cache item expired", map[string]interface{}{"key": key})
}
return nil, nil
}
return item.value, nil
}

// Set adds or updates an item in the cache.
func (c *FilePersistentCache) Set(ctx context.Context, key string, value interface{}) error {
c.mutex.Lock()
expiration := time.Now().Add(c.ttl).UnixNano()
c.store[key] = cacheItem{
value:      value,
expiration: expiration,
}
c.saveToFile()
c.mutex.Unlock()
if c.logger != nil {
c.logger.Info("Persistent cache item set", map[string]interface{}{"key": key})
}
return nil
}

// cleanupLoop periodically removes expired items and saves the file.
func (c *FilePersistentCache) cleanupLoop(interval time.Duration) {
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
c.saveToFile()
c.mutex.Unlock()
}
}
