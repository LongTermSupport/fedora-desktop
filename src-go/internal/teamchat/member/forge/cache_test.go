package forge

import (
	"strconv"
	"testing"
	"time"
)

func TestSD5_3CacheLifeAndOrder(t *testing.T) {
	c := newCache[int](time.Hour)
	c.put("a", 1, 0)
	if v, ok := c.get("a", time.Hour-1); !ok || v != 1 {
		t.Fatalf("get before its life = %d, %t", v, ok)
	}
	if _, ok := c.get("a", time.Hour); ok {
		t.Fatal("an entry is kept at its life")
	}
	if _, ok := c.get("a", 0); ok {
		t.Fatal("an expired entry is kept after it was found expired")
	}
	forever := newCache[int](0)
	forever.put("k", 2, 0)
	if _, ok := forever.get("k", 1000*time.Hour); !ok {
		t.Fatal("a cache with no life lost its entry")
	}
}

func TestSD5_3CachePutAgainMovesToNewest(t *testing.T) {
	c := newCache[int](0)
	for i := range cacheEntries {
		c.put(strconv.Itoa(i), i, 0)
	}
	c.put("0", 10, 0)
	c.put("new", 0, 0)
	if v, ok := c.get("0", 0); !ok || v != 10 {
		t.Fatalf("an entry put again = %d, %t; want kept with its new value", v, ok)
	}
	if _, ok := c.get("1", 0); ok {
		t.Fatal("the oldest entry was not evicted")
	}
	if c.order.Len() != cacheEntries || len(c.byKey) != cacheEntries {
		t.Fatalf("%d entries, want %d", c.order.Len(), cacheEntries)
	}
}
