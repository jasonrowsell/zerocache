package cache

import (
	"container/list"
	"hash/fnv"
	"sync"
)

const (
	defaultShardCount       = 256 // Must be power of 2 for bitwise AND
	defaultMaxItemsPerShard = 1024
)

// cacheEntry holds the value and a pointer to its corresponding element in the LRU list.
type cacheEntry struct {
	value       []byte
	listElement *list.Element // Pointer to the node in the list.List
}

// Cache is a sharded key-value store.
type Cache struct {
	shards           []*Shard
	shardMask        uint64
	maxItemsPerShard int
}

// Shard represents a single partition of a cache.
type Shard struct {
	items    map[string]*cacheEntry
	lruList  *list.List
	mu       sync.RWMutex
	maxItems int
}

type Config struct {
	ShardCount       int
	MaxItemsPerShard int
}

// New creates a new Cache instance with the default number of shards.
func New() *Cache {
	return NewWithConfig(Config{
		ShardCount:       defaultShardCount,
		MaxItemsPerShard: defaultMaxItemsPerShard,
	})
}

// NewWithConfig creates a new Cache instance with specific configuration.
func NewWithConfig(config Config) *Cache {
	if config.ShardCount <= 0 || (config.ShardCount&(config.ShardCount-1)) != 0 {
		config.ShardCount = defaultShardCount
	}
	if config.MaxItemsPerShard < 0 {
		config.MaxItemsPerShard = 0 // Unlimited
	}
	c := &Cache{
		shards:           make([]*Shard, config.ShardCount),
		shardMask:        uint64(config.ShardCount - 1), // Precompute mask
		maxItemsPerShard: config.MaxItemsPerShard,
	}
	for i := 0; i < config.ShardCount; i++ {
		c.shards[i] = &Shard{
			items:    make(map[string]*cacheEntry),
			lruList:  list.New(),
			maxItems: config.MaxItemsPerShard,
			// mu implicity initialized
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
	shard := c.shards[c.getShardIndex(key)]

	shard.mu.Lock()
	entry, found := shard.items[key]
	if found {
		shard.lruList.MoveToFront(entry.listElement)
		valueCopy := make([]byte, len(entry.value))

		copy(valueCopy, entry.value)
		shard.mu.Unlock()
		return valueCopy, true
	}

	shard.mu.Unlock()
	return nil, false
}

// Set adds or updates a value in the cache.
func (c *Cache) Set(key string, value []byte) {
	shard := c.shards[c.getShardIndex(key)]

	shard.mu.Lock()
	defer shard.mu.Unlock()

	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)

	if entry, found := shard.items[key]; found {
		entry.value = valueCopy
		shard.lruList.MoveToFront(entry.listElement)
		return
	}

	listElement := shard.lruList.PushFront(key)
	newEntry := &cacheEntry{
		value:       valueCopy,
		listElement: listElement,
	}
	shard.items[key] = newEntry

	if shard.maxItems > 0 && shard.lruList.Len() > shard.maxItems {
		lruElement := shard.lruList.Back()
		if lruElement != nil {
			lruKey := lruElement.Value.(string)
			shard.lruList.Remove(lruElement)
			delete(shard.items, lruKey)
		}
	}
}

// Delete removes a value from the cache.
func (c *Cache) Delete(key string) {
	shard := c.shards[c.getShardIndex(key)]

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if entry, found := shard.items[key]; found {
		shard.lruList.Remove(entry.listElement)
		delete(shard.items, key)
	}
}

// Len returns the total number of items in the cache across all shards.
// Note: This requires locking all shards, potentially slow. Use for info/metrics only.
func (c *Cache) Len() int {
	totalLen := 0
	for _, shard := range c.shards {
		shard.mu.RLock()
		totalLen += shard.lruList.Len()
		shard.mu.RUnlock()
	}
	return totalLen
}
