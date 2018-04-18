package main

import (
	"math/rand"
	"testing"
	"time"

	"github.com/allegro/bigcache"
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
