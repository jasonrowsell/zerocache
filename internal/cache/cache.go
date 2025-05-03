package cache

import (
	"hash/fnv"
	"sync"
)

const defaultShardCount = 256 // Must be power of 2 for bitwise AND

// Cache is a sharded key-value store.
type Cache struct {
	shards    []*Shard
	shardMask uint64
}

// Shard represents a single partition of a cache.
type Shard struct {
	items map[string][]byte
	mu    sync.RWMutex
}

// New creates a new Cache instance with the default number of shards.
func New() *Cache {
	return NewWithShardCount(defaultShardCount)
}

// NewWithShardCount creates a new Cache instance with a specific number of shards.
// shardCount must be a power of 2.
func NewWithShardCount(shardCount int) *Cache {
	if shardCount <= 0 || (shardCount&(shardCount-1)) != 0 {
		shardCount = defaultShardCount
	}
	c := &Cache{
		shards:    make([]*Shard, shardCount),
		shardMask: uint64(shardCount - 1), // Precompute mask
	}
	for i := 0; i < shardCount; i++ {
		c.shards[i] = &Shard{
			items: make(map[string][]byte),
		}
	}
	return c
}

// getShardIndex returns the index of a shard for a given key.
func (c *Cache) getShardIndex(key string) uint64 {
	hasher := fnv.New64a()
	hasher.Write([]byte(key))
	return hasher.Sum64() & c.shardMask // Use bitwise AND as modulo
}

// Get retrieves a value from the cache.
func (c *Cache) Get(key string) ([]byte, bool) {
	shardIndex := c.getShardIndex(key)
	shard := c.shards[shardIndex]

	shard.mu.RLock()

	value, found := shard.items[key]
	shard.mu.RUnlock()

	if !found {
		return nil, false
	}

	// Returns copy to prevent external modification of the cached slice
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)
	return valueCopy, true
}

// Set adds or updates a value in the cache.
func (c *Cache) Set(key string, value []byte) {
	shardIndex := c.getShardIndex(key)
	shard := c.shards[shardIndex]

	shard.mu.Lock()
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)
	shard.items[key] = valueCopy
	shard.mu.Unlock()
}

// Delete removes a value from the cache.
func (c *Cache) Delete(key string) {
	shardIndex := c.getShardIndex(key)
	shard := c.shards[shardIndex]

	shard.mu.Lock()
	delete(shard.items, key)
	shard.mu.Unlock()
}
