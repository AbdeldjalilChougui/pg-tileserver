// Package cache provides a small thread-safe LRU cache bounded by both the
// number of entries and the total size in bytes of the cached values.
package cache

import (
	"container/list"
	"sync"
)

type entry struct {
	key   string
	value []byte
}

// LRU is a least-recently-used byte cache. The zero value is not usable;
// create one with New.
type LRU struct {
	mu         sync.Mutex
	maxEntries int
	maxBytes   int64
	bytes      int64
	ll         *list.List
	items      map[string]*list.Element
	hits       uint64
	misses     uint64
}

// Stats is a snapshot of cache counters.
type Stats struct {
	Entries  int    `json:"entries"`
	Bytes    int64  `json:"bytes"`
	Hits     uint64 `json:"hits"`
	Misses   uint64 `json:"misses"`
	MaxBytes int64  `json:"max_bytes"`
}

// New creates a cache. A limit <= 0 disables that limit. If both limits are
// disabled the cache is unbounded.
func New(maxEntries int, maxBytes int64) *LRU {
	return &LRU{maxEntries: maxEntries, maxBytes: maxBytes, ll: list.New(), items: map[string]*list.Element{}}
}

// Get returns the cached value and marks it as recently used.
func (c *LRU) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.ll.MoveToFront(el)
		c.hits++
		return el.Value.(*entry).value, true
	}
	c.misses++
	return nil, false
}

// Add inserts or replaces a value. Values larger than maxBytes are not cached.
func (c *LRU) Add(key string, value []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	size := int64(len(value))
	if c.maxBytes > 0 && size > c.maxBytes {
		if el, ok := c.items[key]; ok {
			c.removeElement(el)
		}
		return
	}
	if el, ok := c.items[key]; ok {
		e := el.Value.(*entry)
		c.bytes += size - int64(len(e.value))
		e.value = value
		c.ll.MoveToFront(el)
	} else {
		c.items[key] = c.ll.PushFront(&entry{key, value})
		c.bytes += size
	}
	for c.overLimit() {
		c.removeElement(c.ll.Back())
	}
}

// Remove deletes a key if present.
func (c *LRU) Remove(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.removeElement(el)
	}
}

// Purge empties the cache.
func (c *LRU) Purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ll.Init()
	c.items = map[string]*list.Element{}
	c.bytes = 0
}

// Len returns the number of cached entries.
func (c *LRU) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}

// Stats returns a snapshot of the counters.
func (c *LRU) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Stats{Entries: c.ll.Len(), Bytes: c.bytes, Hits: c.hits, Misses: c.misses, MaxBytes: c.maxBytes}
}

func (c *LRU) overLimit() bool {
	if c.ll.Len() == 0 {
		return false
	}
	return (c.maxEntries > 0 && c.ll.Len() > c.maxEntries) || (c.maxBytes > 0 && c.bytes > c.maxBytes)
}

func (c *LRU) removeElement(el *list.Element) {
	e := c.ll.Remove(el).(*entry)
	delete(c.items, e.key)
	c.bytes -= int64(len(e.value))
}
