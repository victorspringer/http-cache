package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/allegro/bigcache"
	"github.com/coocood/freecache"
	"github.com/victorspringer/http-cache/adapter/memory"
)

func gcPause() time.Duration {
	runtime.GC()
	var stats debug.GCStats
	debug.ReadGCStats(&stats)
	return stats.PauseTotal
}

const (
	entries   = 20000000
	valueSize = 100
)

func main() {
	debug.SetGCPercent(10)
	fmt.Println("Number of entries: ", entries)

	config := &memory.Config{
		Algorithm: memory.LRU,
		Capacity:  entries,
	}
	expiration := time.Now().Add(1 * time.Minute)

	cache, _ := memory.NewAdapter(config)
	for i := 0; i < entries; i++ {
		key, val := generateKeyValue(i, valueSize)
		cache.Set(uint64(key), val, expiration)
	}

	firstKey, _ := generateKeyValue(1, valueSize)
	checkFirstElementBool(cache.Get(uint64(firstKey)))

	fmt.Println("GC pause for http-cache memory adapter: ", gcPause())
	cache = nil
	gcPause()

	//------------------------------------------

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

	firstKey, _ = generateKeyValue(1, valueSize)
	checkFirstElement(bigcache.Get(string(firstKey)))

	fmt.Println("GC pause for bigcache: ", gcPause())
	bigcache = nil
	gcPause()

	//------------------------------------------

	freeCache := freecache.NewCache(entries * 200) //allocate entries * 200 bytes
	for i := 0; i < entries; i++ {
		key, val := generateKeyValue(i, valueSize)
		if err := freeCache.Set([]byte(string(key)), val, 0); err != nil {
			fmt.Println("Error in set: ", err.Error())
		}
	}

	firstKey, _ = generateKeyValue(1, valueSize)
	checkFirstElement(freeCache.Get([]byte(string(firstKey))))

	if freeCache.OverwriteCount() != 0 {
		fmt.Println("Overwritten: ", freeCache.OverwriteCount())
	}
	fmt.Println("GC pause for freecache: ", gcPause())
	freeCache = nil
	gcPause()

	//------------------------------------------

	mapCache := make(map[string][]byte)
	for i := 0; i < entries; i++ {
		key, val := generateKeyValue(i, valueSize)
		mapCache[string(key)] = val
	}
	fmt.Println("GC pause for map: ", gcPause())
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
