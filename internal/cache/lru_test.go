package cache

import (
	"fmt"
	"sync"
	"testing"
)

func TestEvictsLeastRecentlyUsedByCount(t *testing.T) {
	c := New(2, 0)
	c.Add("a", []byte("1"))
	c.Add("b", []byte("2"))
	if _, ok := c.Get("a"); !ok { // a is now most recent
		t.Fatal("a missing")
	}
	c.Add("c", []byte("3")) // evicts b
	if _, ok := c.Get("b"); ok {
		t.Fatal("b should have been evicted")
	}
	for _, k := range []string{"a", "c"} {
		if _, ok := c.Get(k); !ok {
			t.Fatalf("%s missing", k)
		}
	}
}

func TestEvictsByBytes(t *testing.T) {
	c := New(0, 10)
	c.Add("a", make([]byte, 4))
	c.Add("b", make([]byte, 4))
	c.Add("c", make([]byte, 4)) // 12 bytes > 10, evict a
	if _, ok := c.Get("a"); ok {
		t.Fatal("a should be evicted")
	}
	if s := c.Stats(); s.Bytes != 8 || s.Entries != 2 {
		t.Fatalf("stats %+v", s)
	}
	c.Add("huge", make([]byte, 11)) // larger than the whole cache: ignored
	if _, ok := c.Get("huge"); ok {
		t.Fatal("oversized value must not be cached")
	}
	if c.Len() != 2 {
		t.Fatalf("len %d", c.Len())
	}
}

func TestReplaceUpdatesSize(t *testing.T) {
	c := New(0, 100)
	c.Add("a", make([]byte, 10))
	c.Add("a", make([]byte, 30))
	if s := c.Stats(); s.Bytes != 30 || s.Entries != 1 {
		t.Fatalf("stats %+v", s)
	}
	c.Remove("a")
	if s := c.Stats(); s.Bytes != 0 || s.Entries != 0 {
		t.Fatalf("stats after remove %+v", s)
	}
}

func TestHitMissCounters(t *testing.T) {
	c := New(10, 0)
	c.Get("x")
	c.Add("x", []byte("v"))
	c.Get("x")
	c.Get("x")
	if s := c.Stats(); s.Hits != 2 || s.Misses != 1 {
		t.Fatalf("stats %+v", s)
	}
	c.Purge()
	if c.Len() != 0 || c.Stats().Bytes != 0 {
		t.Fatal("purge failed")
	}
}

func TestConcurrentAccess(t *testing.T) {
	c := New(50, 0)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				k := fmt.Sprintf("%d", (g*31+i)%120)
				c.Add(k, []byte(k))
				c.Get(k)
			}
		}(g)
	}
	wg.Wait()
	if c.Len() > 50 {
		t.Fatalf("len %d exceeds limit", c.Len())
	}
}
