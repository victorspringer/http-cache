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
func (a *Adapter) Get(key uint64) (cache.Cache, bool) {
	a.Lock()
	defer a.Unlock()

	var c cache.Cache
	if err := a.store.Get(string(key), &c); err == nil {
		return c, true
	}

	return cache.Cache{}, false
}

// Set implements the cache Adapter interface Set method.
func (a *Adapter) Set(key uint64, cache cache.Cache) {
	a.Lock()
	defer a.Unlock()
	fmt.Println(cache.Expiration.Sub(time.Now()))
	a.store.Set(&redisCache.Item{
		Key:        string(key),
		Object:     cache,
		Expiration: cache.Expiration.Sub(time.Now()),
	})
}

// Release implements the cache Adapter interface Release method.
func (a *Adapter) Release(key uint64) {
	a.Lock()
	defer a.Unlock()

	a.store.Delete(string(key))
}

// NewAdapter initializes Redis adapter.
func NewAdapter(opt *redis.RingOptions) cache.Adapter {
	return &Adapter{
		sync.Mutex{},
		&redisCache.Codec{
			Redis: redis.NewRing(opt),
			Marshal: func(v interface{}) ([]byte, error) {
				return msgpack.Marshal(v)

			},
			Unmarshal: func(b []byte, v interface{}) error {
				return msgpack.Unmarshal(b, v)
			},
		},
	}
}
