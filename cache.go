package webdavds

import (
	"container/list"
	"sync"
)

// lru is a small, thread-safe, positive-only existence+size cache. It records
// "this key exists and has this size" to spare a PROPFIND on Has/GetSize.
//
// It deliberately does NOT cache absence: on a shared WebDAV backend another
// kubo instance may add a block at any time, so a negative cache could return
// stale "not found". kubo's blockstore bloom filter already shields the
// negative path; we only accelerate the positive one.
type lru struct {
	mu   sync.Mutex
	cap  int
	ll   *list.List
	m    map[string]*list.Element
}

type lruEntry struct {
	key  string
	size int
}

func newLRU(capacity int) *lru {
	if capacity <= 0 {
		capacity = 1 << 16
	}
	return &lru{cap: capacity, ll: list.New(), m: make(map[string]*list.Element, capacity)}
}

// get returns the cached size for key and whether it was present.
func (c *lru) get(key string) (int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.m[key]; ok {
		c.ll.MoveToFront(el)
		return el.Value.(*lruEntry).size, true
	}
	return 0, false
}

// set records key as present with the given size.
func (c *lru) set(key string, size int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.m[key]; ok {
		c.ll.MoveToFront(el)
		el.Value.(*lruEntry).size = size
		return
	}
	el := c.ll.PushFront(&lruEntry{key: key, size: size})
	c.m[key] = el
	for c.ll.Len() > c.cap {
		oldest := c.ll.Back()
		if oldest == nil {
			break
		}
		c.ll.Remove(oldest)
		delete(c.m, oldest.Value.(*lruEntry).key)
	}
}

// del forgets key (call after Delete).
func (c *lru) del(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.m[key]; ok {
		c.ll.Remove(el)
		delete(c.m, key)
	}
}
