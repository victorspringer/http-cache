package memory

import (
	"bytes"
	"encoding/gob"
	"errors"
	"sync"
	"time"

	"github.com/victorspringer/http-cache"
)

// Algorithm is the string type for caching algorithms labels.
type Algorithm string

const (
	// LRU is the constant for Least Recently Used.
	LRU Algorithm = "LRU"

	// MRU is the constant for Most Recently Used.
	MRU Algorithm = "MRU"

	// LFU is the constant for Least Frequently Used.
	LFU Algorithm = "LFU"

	// MFU is the constant for Most Frequently Used.
	MFU Algorithm = "MFU"
)

// Config contains the memory adapter configuration parameters.
type Config struct {
	// Capacity is the maximum number of cached responses.
	Capacity int

	// Algorithm is the approach used to select a cached
	// response to be evicted when the capacity is reached.
	Algorithm Algorithm
}

// Adapter is the memory adapter data structure.
type Adapter struct {
	sync.Mutex
	capacity  int
	algorithm Algorithm
	store     map[uint64][]byte
}

// Get implements the cache Adapter interface Get method.
func (a *Adapter) Get(key uint64) (cache.Cache, bool) {
	a.Lock()
	defer a.Unlock()

	if b, ok := a.store[key]; ok {
		return bytesToCache(b), true
	}

	return cache.Cache{}, false
}

// Set implements the cache Adapter interface Set method.
func (a *Adapter) Set(key uint64, cache cache.Cache) {
	a.Lock()
	defer a.Unlock()

	if len(a.store) == a.capacity {
		k := make(chan uint64, 1)
		go a.evict(k)
		a.Release(<-k)
	}

	a.store[key] = cacheToBytes(cache)
}

// Release implements the Adapter interface Release method.
func (a *Adapter) Release(key uint64) {
	if _, ok := a.store[key]; ok {
		delete(a.store, key)
	}
}

func (a *Adapter) evict(key chan uint64) {
	selectedKey := uint64(0)
	lastAccess := time.Now()
	frequency := 9999999999999

	if a.algorithm == MRU {
		lastAccess = time.Time{}
	} else if a.algorithm == MFU {
		frequency = 0
	}

	for k, v := range a.store {
		c := bytesToCache(v)
		switch a.algorithm {
		case LRU:
			if c.LastAccess.Before(lastAccess) {
				selectedKey = k
				lastAccess = c.LastAccess
			}
		case MRU:
			if c.LastAccess.After(lastAccess) ||
				c.LastAccess.Equal(lastAccess) {
				selectedKey = k
				lastAccess = c.LastAccess
			}
		case LFU:
			if c.Frequency < frequency {
				selectedKey = k
				frequency = c.Frequency
			}
		case MFU:
			if c.Frequency >= frequency {
				selectedKey = k
				frequency = c.Frequency
			}
		}
	}

	key <- selectedKey
}

func bytesToCache(b []byte) cache.Cache {
	var c cache.Cache
	dec := gob.NewDecoder(bytes.NewReader(b))
	dec.Decode(&c)

	return c
}

func cacheToBytes(cache cache.Cache) []byte {
	var b bytes.Buffer
	enc := gob.NewEncoder(&b)
	enc.Encode(&cache)

	return b.Bytes()
}

// NewAdapter initializes memory adapter.
func NewAdapter(cfg *Config) (cache.Adapter, error) {
	if cfg.Capacity <= 1 {
		return nil, errors.New("memory adapter requires a capacity greater than one")
	}

	if cfg.Algorithm == "" {
		return nil, errors.New("memory adapter requires a caching algorithm")
	}

	return &Adapter{
		sync.Mutex{},
		cfg.Capacity,
		cfg.Algorithm,
		map[uint64][]byte{},
	}, nil
}
