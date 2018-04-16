package redis

import (
	"fmt"
	"sync"
	"time"

	redisCache "github.com/go-redis/cache"
	redis "github.com/go-redis/redis"
	cache "github.com/victorspringer/http-cache"
	"github.com/vmihailenco/msgpack"
)

// Adapter is the memory adapter data structure.
type Adapter struct {
	sync.Mutex
	store *redisCache.Codec
}

// Get implements the cache Adapter interface Get method.
func (a *Adapter) Get(key string) (cache.Cache, bool) {
	a.Lock()
	defer a.Unlock()

	var c cache.Cache
	if err := a.store.Get(key, &c); err == nil {
		return c, true
	}

	return cache.Cache{}, false
}

// Set implements the cache Adapter interface Set method.
func (a *Adapter) Set(key string, cache cache.Cache) {
	a.Lock()
	defer a.Unlock()
	fmt.Println(cache.Expiration.Sub(time.Now()))
	a.store.Set(&redisCache.Item{
		Key:        key,
		Object:     cache,
		Expiration: cache.Expiration.Sub(time.Now()),
	})
}

// Release implements the cache Adapter interface Release method.
func (a *Adapter) Release(key string) {
	a.Lock()
	defer a.Unlock()

	a.store.Delete(key)
}

// NewAdapter initializes Redis adapter.
func NewAdapter() cache.Adapter {
	ring := redis.NewRing(&redis.RingOptions{
		Addrs: map[string]string{
			"server": ":6379",
		},
	})

	codec := &redisCache.Codec{
		Redis: ring,
		Marshal: func(v interface{}) ([]byte, error) {
			return msgpack.Marshal(v)

		},
		Unmarshal: func(b []byte, v interface{}) error {
			return msgpack.Unmarshal(b, v)
		},
	}

	return &Adapter{
		sync.Mutex{},
		codec,
	}
}
