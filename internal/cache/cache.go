package cache

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

type node struct {
	key  string
	val  CacheEntry
	next *node
	prev *node

	maxTTLSeconds time.Duration
}

type CacheEntry struct {
	// The status code of the cache entry
	StatusCode int
	// The headers of the cache entry
	Header http.Header
	// The contents cached
	Body []byte
	// The timestamp of when the entry was cached
	Timestamp time.Time
}

// LRU Cache
// Implemented as a doubly-linked list, where elements are inserted at the tail and removed from the head.
type Cache struct {
	capacity int              // The capacity of the cache
	size     int              // The number of elements currently in cache
	cache    map[string]*node // A hashmap where keys are http method + url path + query string, values are nodes
	head     *node
	tail     *node

	maxTTLSeconds float64
	mu            sync.Mutex
}

type CacheMissError struct {
	key string
}

func (cme *CacheMissError) Error() string {
	return fmt.Sprintf("Cache miss on key %s", cme.key)
}

func New(capacity int, maxTTLSeconds float64) *Cache {
	if capacity <= 0 {
		panic("Cache capacity must be greater than 0")
	}

	// Sentinel
	head := &node{
		next: nil,
		prev: nil,
	}
	tail := &node{
		next: nil,
		prev: nil,
	}

	head.next = tail
	tail.prev = head

	return &Cache{
		capacity:      capacity,
		size:          0,
		cache:         make(map[string]*node),
		head:          head,
		tail:          tail,
		maxTTLSeconds: maxTTLSeconds,
	}
}

func CacheKey(method, path, query string) string {
	if query == "" {
		return method + " " + path
	}
	return method + " " + path + "?" + query
}

// push_back moves the given entry to the tail of the list, which represents that it is the most recently used element in the cache.
// If the entry is not in the list, it will be added to the tail. If the entry is already in the list, it will be moved to the tail.
func (c *Cache) push_back(entry *node) {
	// If the entry is already in the list, we should remove it first before pushing it back to the tail.
	if entry.prev != nil && entry.next != nil {
		entry.prev.next = entry.next
		entry.next.prev = entry.prev
	}

	// Insert the entry at the tail of the list.
	c.tail.prev.next = entry
	entry.prev = c.tail.prev
	c.tail.prev = entry
	entry.next = c.tail

	c.cache[entry.key] = entry
}

// Remove an element from the cache by its key
func (c *Cache) remove(key string) error {
	if entry, ok := c.cache[key]; ok {
		entry.prev.next = entry.next
		entry.next.prev = entry.prev

		delete(c.cache, key)
		c.size--
		return nil
	} else {
		return &CacheMissError{key}
	}
}

// Evict the least recently used element from the cache, which is the one at the head of the list.
func (c *Cache) evict() {
	if c.Size() == 0 {
		return
	}

	// Remove the least recently used element, which is the one at the head of the list.
	lru := c.head.next
	c.head.next = lru.next
	lru.next.prev = c.head

	delete(c.cache, lru.key)
	c.size--
}

// Get a value in cache from its key.
// Key is in the form: HTTP Method + URL Path + Query String.
// Returns error on cache miss.
func (c *Cache) Get(key string) (CacheEntry, error) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.Size() == 0 {
		return CacheEntry{}, &CacheMissError{key: key}
	}

	if node, ok := c.cache[key]; ok {
		// If the cache entry has expired, we treat it as a cache miss
		if now.Sub(node.val.Timestamp) >= node.maxTTLSeconds {
			c.remove(key)
			return CacheEntry{}, &CacheMissError{key: key}
		}

		c.push_back(node)

		return node.val, nil
	}

	// If we didn't find it in the cache, we return cache miss
	return CacheEntry{}, &CacheMissError{key: key}
}

// Inserts the cache entry at the given key with the time to live provided.
// The timestamp at which the cache was inserted is placed into the value too.
func (c *Cache) Put(key string, value CacheEntry, ttl time.Duration) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	if n, ok := c.cache[key]; ok {
		// If we get a cache hit, we should update the value and timestamp
		n.val = value
		n.val.Timestamp = now
		n.maxTTLSeconds = ttl
	} else {
		if c.Size() == c.Capacity() {
			c.evict()
		}
		value.Timestamp = now

		c.cache[key] = &node{
			key:           key,
			val:           value,
			next:          nil,
			prev:          nil,
			maxTTLSeconds: ttl,
		}
		c.size++
	}

	c.push_back(c.cache[key])
}

// The current size of the cache, i.e. the number of elements currently in cache.
func (c *Cache) Size() int {
	return c.size
}

// The capacity of the cache, i.e. the maximum number of elements that can be in cache at any time.
func (c *Cache) Capacity() int {
	return c.capacity
}

// The maximum TTL in seconds for cache entries in this cache.
// This is used as the default TTL for cache entries if the origin server does not provide a Cache-Control header with max-age directive.
func (c *Cache) GetMaxTTLSeconds() time.Duration {
	return time.Duration(c.maxTTLSeconds * float64(time.Second))
}
