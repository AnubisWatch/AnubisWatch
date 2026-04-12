package cache

import (
	"container/list"
	"sync"
	"time"
)

// Cache is a thread-safe in-memory cache with TTL support
type Cache struct {
	mu         sync.RWMutex
	items      map[string]*cacheItem
	lruList    *list.List
	maxSize    int
	defaultTTL time.Duration
	stopCh     chan struct{}
}

// cacheItem represents a cached value with metadata
type cacheItem struct {
	key         string
	value       any
	expiresAt   time.Time
	element     *list.Element
	accessCount int
}

// New creates a new cache with the specified max size and default TTL
func New(maxSize int, defaultTTL time.Duration) *Cache {
	c := &Cache{
		items:      make(map[string]*cacheItem),
		lruList:    list.New(),
		maxSize:    maxSize,
		defaultTTL: defaultTTL,
	}

	// Start cleanup goroutine
	go c.cleanupLoop()

	return c
}

// Get retrieves a value from the cache
func (c *Cache) Get(key string) (any, bool) {
	c.mu.RLock()
	item, found := c.items[key]
	c.mu.RUnlock()

	if !found {
		return nil, false
	}

	// Check if expired
	if time.Now().After(item.expiresAt) {
		c.Delete(key)
		return nil, false
	}

	// Update access count and move to front
	c.mu.Lock()
	item.accessCount++
	c.lruList.MoveToFront(item.element)
	c.mu.Unlock()

	return item.value, true
}

// GetString retrieves a string value from the cache
func (c *Cache) GetString(key string) (string, bool) {
	value, found := c.Get(key)
	if !found {
		return "", false
	}
	if str, ok := value.(string); ok {
		return str, true
	}
	return "", false
}

// GetBytes retrieves a byte slice from the cache
func (c *Cache) GetBytes(key string) ([]byte, bool) {
	value, found := c.Get(key)
	if !found {
		return nil, false
	}
	if data, ok := value.([]byte); ok {
		return data, true
	}
	return nil, false
}

// Set adds or updates a value in the cache
func (c *Cache) Set(key string, value any, ttl time.Duration) {
	if ttl <= 0 {
		ttl = c.defaultTTL
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if item already exists
	if item, exists := c.items[key]; exists {
		// Update existing item
		item.value = value
		item.expiresAt = time.Now().Add(ttl)
		item.accessCount = 1
		c.lruList.MoveToFront(item.element)
		return
	}

	// Create new item
	item := &cacheItem{
		key:         key,
		value:       value,
		expiresAt:   time.Now().Add(ttl),
		accessCount: 1,
	}

	// Add to LRU list
	elem := c.lruList.PushFront(item)
	item.element = elem

	// Add to map
	c.items[key] = item

	// Evict if over capacity
	if c.maxSize > 0 && len(c.items) > c.maxSize {
		c.evictOldest()
	}
}

// SetString adds a string value to the cache
func (c *Cache) SetString(key string, value string, ttl time.Duration) {
	c.Set(key, value, ttl)
}

// SetBytes adds a byte slice to the cache
func (c *Cache) SetBytes(key string, value []byte, ttl time.Duration) {
	c.Set(key, value, ttl)
}

// Delete removes an item from the cache
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	item, exists := c.items[key]
	if !exists {
		return
	}

	c.lruList.Remove(item.element)
	delete(c.items, key)
}

// Exists checks if a key exists in the cache
func (c *Cache) Exists(key string) bool {
	c.mu.RLock()
	item, found := c.items[key]
	c.mu.RUnlock()

	if !found {
		return false
	}

	// Check if expired
	if time.Now().After(item.expiresAt) {
		c.Delete(key)
		return false
	}

	return true
}

// Size returns the number of items in the cache
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// Clear removes all items from the cache
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[string]*cacheItem)
	c.lruList = list.New()
}

// evictOldest removes the least recently used item
func (c *Cache) evictOldest() {
	elem := c.lruList.Back()
	if elem == nil {
		return
	}

	item := elem.Value.(*cacheItem)
	c.lruList.Remove(elem)
	delete(c.items, item.key)
}

// StartCleanup starts the periodic cleanup goroutine
func (c *Cache) StartCleanup() {
	c.stopCh = make(chan struct{})
	go c.cleanupLoop()
}

// StopCleanup stops the periodic cleanup goroutine
func (c *Cache) StopCleanup() {
	if c.stopCh != nil {
		close(c.stopCh)
	}
}

// cleanupLoop periodically removes expired items
func (c *Cache) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.cleanup()
		}
	}
}

// cleanup removes expired items
func (c *Cache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, item := range c.items {
		if now.After(item.expiresAt) {
			c.lruList.Remove(item.element)
			delete(c.items, key)
		}
	}
}

// Stats returns cache statistics
func (c *Cache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return CacheStats{
		Size:    len(c.items),
		MaxSize: c.maxSize,
		HitRate: 0, // Would need tracking
	}
}

// CacheStats holds cache statistics
type CacheStats struct {
	Size    int
	MaxSize int
	HitRate float64
}

// CacheWithLoader is a cache that can load missing values
type CacheWithLoader struct {
	*Cache
	loader func(key string) (any, error)
}

// NewWithLoader creates a cache with a loader function
func NewWithLoader(maxSize int, defaultTTL time.Duration, loader func(key string) (any, error)) *CacheWithLoader {
	return &CacheWithLoader{
		Cache:  New(maxSize, defaultTTL),
		loader: loader,
	}
}

// GetOrLoad retrieves a value, loading it if not cached
func (c *CacheWithLoader) GetOrLoad(key string) (any, error) {
	// Try cache first
	if value, found := c.Get(key); found {
		return value, nil
	}

	// Load value
	value, err := c.loader(key)
	if err != nil {
		return nil, err
	}

	// Store in cache
	c.Set(key, value, c.defaultTTL)

	return value, nil
}

// TypedCache is a type-safe wrapper around Cache
type TypedCache[T any] struct {
	*Cache
}

// NewTyped creates a new typed cache
func NewTyped[T any](maxSize int, defaultTTL time.Duration) *TypedCache[T] {
	return &TypedCache[T]{Cache: New(maxSize, defaultTTL)}
}

// Get retrieves a typed value from the cache
func (c *TypedCache[T]) Get(key string) (T, bool) {
	value, found := c.Cache.Get(key)
	if !found {
		var zero T
		return zero, false
	}

	if typed, ok := value.(T); ok {
		return typed, true
	}

	var zero T
	return zero, false
}

// Set adds or updates a typed value in the cache
func (c *TypedCache[T]) Set(key string, value T, ttl time.Duration) {
	c.Cache.Set(key, value, ttl)
}
