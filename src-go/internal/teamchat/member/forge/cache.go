package forge

import (
	"container/list"
	"time"
)

// cache is one in-memory fact cache: at most cacheEntries, the oldest put evicted first, each
// kept for life on the boot clock (0 is for the link's life). Not safe for concurrent use:
// the Checker's lock guards it.
type cache[V any] struct {
	life  time.Duration
	order *list.List
	byKey map[string]*list.Element
}

type entry[V any] struct {
	key string
	at  time.Duration
	v   V
}

func newCache[V any](life time.Duration) *cache[V] {
	return &cache[V]{life: life, order: list.New(), byKey: map[string]*list.Element{}}
}

func (c *cache[V]) get(key string, now time.Duration) (V, bool) {
	var zero V
	el, ok := c.byKey[key]
	if !ok {
		return zero, false
	}
	e, ok := el.Value.(*entry[V])
	if !ok || c.life > 0 && now-e.at >= c.life {
		c.remove(el)
		return zero, false
	}
	return e.v, true
}

func (c *cache[V]) put(key string, v V, now time.Duration) {
	if el, ok := c.byKey[key]; ok {
		c.remove(el)
	}
	c.byKey[key] = c.order.PushBack(&entry[V]{key: key, at: now, v: v})
	if c.order.Len() > cacheEntries {
		c.remove(c.order.Front())
	}
}

func (c *cache[V]) remove(el *list.Element) {
	if e, ok := c.order.Remove(el).(*entry[V]); ok {
		delete(c.byKey, e.key)
	}
}

// lookup and keep reach one of c's caches under its lock, at the boot clock's reading.
func lookup[V any](c *Checker, k *cache[V], key string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return k.get(key, c.clock.Boot())
}

func keep[V any](c *Checker, k *cache[V], key string, v V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	k.put(key, v, c.clock.Boot())
}
