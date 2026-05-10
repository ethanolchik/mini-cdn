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

	maxTTLSeconds float64
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
	Capacity int              // The capacity of the cache
	Size     int              // The number of elements currently in cache
	cache    map[string]*node // A hashmap where keys are http method + url path + query string, values are nodes
	head     *node
	tail     *node

	// The maximum time to live that the cache will default to when Cache-Control header is not provided
	MaxTTLSeconds float64
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
		Capacity:      capacity,
		Size:          0,
		cache:         make(map[string]*node),
		head:          head,
		tail:          tail,
		MaxTTLSeconds: maxTTLSeconds,
	}
}

func CacheKey(method, path, query string) string {
	if query == "" {
		return method + " " + path
	}
	return method + " " + path + "?" + query
}

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

	if _, ok := c.cache[entry.key]; !ok {
		c.cache[entry.key] = entry
	}
}

// Remove an element from the cache by its key
func (c *Cache) remove(key string) error {
	if entry, ok := c.cache[key]; ok {
		entry.prev.next = entry.next
		entry.next.prev = entry.prev

		delete(c.cache, key)
		c.Size--
		return nil
	} else {
		return &CacheMissError{key}
	}
}

// Evict the least recently used element from the cache, which is the one at the head of the list.
func (c *Cache) evict() {
	if c.Size == 0 {
		return
	}

	// Remove the least recently used element, which is the one at the head of the list.
	lru := c.head.next
	c.head.next = lru.next
	lru.next.prev = c.head

	delete(c.cache, lru.key)
	c.Size--
}

// Get a value in cache from its key.
// Key is in the form: HTTP Method + URL Path + Query String.
// Returns error on cache miss.
func (c *Cache) Get(key string) (CacheEntry, error) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.Size == 0 {
		return CacheEntry{}, &CacheMissError{key: key}
	}

	if node, ok := c.cache[key]; ok {
		// If the cache entry has expired, we treat it as a cache miss
		if now.Sub(node.val.Timestamp).Seconds() >= node.maxTTLSeconds {
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
func (c *Cache) Put(key string, value CacheEntry, ttl float64) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.cache[key]; ok {
		// If we get a cache hit, we should update the value and timestamp
		c.cache[key].val = value
		c.cache[key].val.Timestamp = now
		c.cache[key].maxTTLSeconds = ttl
	} else {
		if c.Size == c.Capacity {
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
		c.Size++
	}

	c.push_back(c.cache[key])
}
