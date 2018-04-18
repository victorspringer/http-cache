package main

import (
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/allegro/bigcache"
	"github.com/coocood/freecache"
	cache "github.com/victorspringer/http-cache"
	"github.com/victorspringer/http-cache/adapter/memory"
)

const maxEntrySize = 256

func BenchmarkHTTPCacheMamoryAdapterSet(b *testing.B) {
	cache, expiration := initHTTPCacheMamoryAdapter(b.N)
	for i := 0; i < b.N; i++ {
		cache.Set(uint64(i), value(), expiration)
	}
}

func BenchmarkMapSet(b *testing.B) {
	m := make(map[string][]byte)
	for i := 0; i < b.N; i++ {
		m[string(i)] = value()
	}
}

func BenchmarkConcurrentMapSet(b *testing.B) {
	var m sync.Map
	for i := 0; i < b.N; i++ {
		m.Store(uint64(i), value())
	}
}

func BenchmarkFreeCacheSet(b *testing.B) {
	cache := freecache.NewCache(b.N * maxEntrySize)
	for i := 0; i < b.N; i++ {
		cache.Set([]byte([]byte(string(i))), value(), 0)
	}
}

func BenchmarkBigCacheSet(b *testing.B) {
	cache := initBigCache(b.N)
	for i := 0; i < b.N; i++ {
		cache.Set(string(i), value())
	}
}

func BenchmarkHTTPCacheMamoryAdapterGet(b *testing.B) {
	b.StopTimer()
	cache, expiration := initHTTPCacheMamoryAdapter(b.N)
	for i := 0; i < b.N; i++ {
		cache.Set(uint64(i), value(), expiration)
	}

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(uint64(i))
	}
}
func BenchmarkMapGet(b *testing.B) {
	b.StopTimer()
	m := make(map[string][]byte)
	for i := 0; i < b.N; i++ {
		m[string(i)] = value()
	}

	b.StartTimer()
	hitCount := 0
	for i := 0; i < b.N; i++ {
		if m[string(i)] != nil {
			hitCount++
		}
	}
}

func BenchmarkConcurrentMapGet(b *testing.B) {
	b.StopTimer()
	var m sync.Map
	for i := 0; i < b.N; i++ {
		m.Store(uint64(i), value())
	}

	b.StartTimer()
	hitCounter := 0
	for i := 0; i < b.N; i++ {
		_, ok := m.Load(uint64(i))
		if ok {
			hitCounter++
		}
	}
}

func BenchmarkFreeCacheGet(b *testing.B) {
	b.StopTimer()
	cache := freecache.NewCache(b.N * maxEntrySize)
	for i := 0; i < b.N; i++ {
		cache.Set([]byte(string(i)), value(), 0)
	}

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		cache.Get([]byte(string(i)))
	}
}

func BenchmarkBigCacheGet(b *testing.B) {
	b.StopTimer()
	cache := initBigCache(b.N)
	for i := 0; i < b.N; i++ {
		cache.Set(string(i), value())
	}

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(string(i))
	}
}

func BenchmarkHTTPCacheMamoryAdapterSetParallel(b *testing.B) {
	cache, expiration := initHTTPCacheMamoryAdapter(b.N)
	rand.Seed(time.Now().Unix())

	b.RunParallel(func(pb *testing.PB) {
		id := rand.Intn(1000)
		counter := 0
		for pb.Next() {
			cache.Set(parallelKey(id, counter), value(), expiration)
			counter = counter + 1
		}
	})
}

func BenchmarkBigCacheSetParallel(b *testing.B) {
	cache := initBigCache(b.N)
	rand.Seed(time.Now().Unix())

	b.RunParallel(func(pb *testing.PB) {
		id := rand.Intn(1000)
		counter := 0
		for pb.Next() {
			cache.Set(string(parallelKey(id, counter)), value())
			counter = counter + 1
		}
	})
}

func BenchmarkFreeCacheSetParallel(b *testing.B) {
	cache := freecache.NewCache(b.N * maxEntrySize)
	rand.Seed(time.Now().Unix())

	b.RunParallel(func(pb *testing.PB) {
		id := rand.Intn(1000)
		counter := 0
		for pb.Next() {
			cache.Set([]byte(string(parallelKey(id, counter))), value(), 0)
			counter = counter + 1
		}
	})
}

func BenchmarkConcurrentMapSetParallel(b *testing.B) {
	var m sync.Map

	b.RunParallel(func(pb *testing.PB) {
		id := rand.Intn(1000)
		for pb.Next() {
			m.Store(uint64(id), value())
		}
	})
}

func BenchmarkHTTPCacheMemoryAdapterGetParallel(b *testing.B) {
	b.StopTimer()
	cache, expiration := initHTTPCacheMamoryAdapter(b.N)
	for i := 0; i < b.N; i++ {
		cache.Set(uint64(i), value(), expiration)
	}

	b.StartTimer()
	b.RunParallel(func(pb *testing.PB) {
		counter := 0
		for pb.Next() {
			cache.Get(uint64(counter))
			counter = counter + 1
		}
	})
}

func BenchmarkBigCacheGetParallel(b *testing.B) {
	b.StopTimer()
	cache := initBigCache(b.N)
	for i := 0; i < b.N; i++ {
		cache.Set(string(i), value())
	}

	b.StartTimer()
	b.RunParallel(func(pb *testing.PB) {
		counter := 0
		for pb.Next() {
			cache.Get(string(counter))
			counter = counter + 1
		}
	})
}

func BenchmarkFreeCacheGetParallel(b *testing.B) {
	b.StopTimer()
	cache := freecache.NewCache(b.N * maxEntrySize)
	for i := 0; i < b.N; i++ {
		cache.Set([]byte(string(i)), value(), 0)
	}

	b.StartTimer()
	b.RunParallel(func(pb *testing.PB) {
		counter := 0
		for pb.Next() {
			cache.Get([]byte(string(counter)))
			counter = counter + 1
		}
	})
}

func BenchmarkConcurrentMapGetParallel(b *testing.B) {
	b.StopTimer()
	var m sync.Map
	for i := 0; i < b.N; i++ {
		m.Store(uint64(i), value())
	}

	b.StartTimer()
	hitCount := 0

	b.RunParallel(func(pb *testing.PB) {
		id := rand.Intn(1000)
		for pb.Next() {
			_, ok := m.Load(uint64(id))
			if ok {
				hitCount++
			}
		}
	})
}

func value() []byte {
	return make([]byte, 100)
}

func parallelKey(threadID int, counter int) uint64 {
	return uint64(threadID)
}

func initHTTPCacheMamoryAdapter(entries int) (cache.Adapter, time.Time) {
	if entries < 2 {
		entries = 2
	}
	adapter, _ := memory.NewAdapter(&memory.Config{
		Capacity:  entries,
		Algorithm: memory.LRU,
	})

	return adapter, time.Now().Add(1 * time.Minute)
}

func initBigCache(entriesInWindow int) *bigcache.BigCache {
	cache, _ := bigcache.NewBigCache(bigcache.Config{
		Shards:             256,
		LifeWindow:         10 * time.Minute,
		MaxEntriesInWindow: entriesInWindow,
		MaxEntrySize:       maxEntrySize,
		Verbose:            true,
	})

	return cache
}
