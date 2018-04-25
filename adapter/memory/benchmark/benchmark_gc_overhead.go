package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/allegro/bigcache"
	"github.com/victorspringer/http-cache/adapter/memory"
)

const (
	entries   = 20000000
	valueSize = 100
)

func main() {
	debug.SetGCPercent(10)
	fmt.Println("Number of entries: ", entries)

	c := os.Getenv("cache")
	if c == "http-cache" {
		benchmarkHTTPCacheMemoryAdapter()
	} else if c == "bigcache" {
		benchmarkBigCache()
	} else {
		fmt.Println("invalid cache")
		os.Exit(1)
	}
}

func benchmarkHTTPCacheMemoryAdapter() {
	expiration := time.Now().Add(1 * time.Minute)

	cache, _ := memory.NewAdapter(
		memory.AdapterWithAlgorithm(memory.LRU),
		memory.AdapterWithCapacity(entries),
	)

	for i := 0; i < entries; i++ {
		key, val := generateKeyValue(i, valueSize)
		cache.Set(uint64(key), val, expiration)
	}

	firstKey, _ := generateKeyValue(1, valueSize)
	checkFirstElementBool(cache.Get(uint64(firstKey)))

	fmt.Println("GC pause for http-cache memory adapter: ", gcPause())

	os.Exit(0)
}

func benchmarkBigCache() {
	bgConfig := bigcache.Config{
		Shards:             256,
		LifeWindow:         100 * time.Minute,
		MaxEntriesInWindow: entries,
		MaxEntrySize:       200,
		Verbose:            true,
	}

	bigcache, _ := bigcache.NewBigCache(bgConfig)

	for i := 0; i < entries; i++ {
		key, val := generateKeyValue(i, valueSize)
		bigcache.Set(string(key), val)
	}

	firstKey, _ := generateKeyValue(1, valueSize)
	checkFirstElement(bigcache.Get(string(firstKey)))

	fmt.Println("GC pause for bigcache: ", gcPause())

	os.Exit(0)
}

func checkFirstElementBool(val []byte, ok bool) {
	_, expectedVal := generateKeyValue(1, valueSize)
	if !ok {
		fmt.Println("Get failed")
	} else if string(val) != string(expectedVal) {
		fmt.Println("Wrong first element: ", string(val))
	}
}

func checkFirstElement(val []byte, err error) {
	_, expectedVal := generateKeyValue(1, valueSize)
	if err != nil {
		fmt.Println("Error in get: ", err.Error())
	} else if string(val) != string(expectedVal) {
		fmt.Println("Wrong first element: ", string(val))
	}
}

func generateKeyValue(index int, valSize int) (int, []byte) {
	key := index
	fixedNumber := []byte(fmt.Sprintf("%010d", index))
	val := append(make([]byte, valSize-10), fixedNumber...)

	return key, val
}

func gcPause() time.Duration {
	runtime.GC()
	var stats debug.GCStats
	debug.ReadGCStats(&stats)
	return stats.PauseTotal
}
