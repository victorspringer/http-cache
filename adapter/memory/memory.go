package memory

import (
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
	store     map[string]cache.Cache
}

// Get implements the cache Adapter interface Get method.
func (a *Adapter) Get(key string) (cache.Cache, bool) {
	a.Lock()
	defer a.Unlock()

	if cache, ok := a.store[key]; ok {
		return cache, true
	}

	return cache.Cache{}, false
}

// Set implements the cache Adapter interface Set method.
func (a *Adapter) Set(key string, cache cache.Cache) {
	a.Lock()
	defer a.Unlock()

	if len(a.store) == a.capacity {
		k := make(chan string, 1)
		go a.evict(k)
		a.Release(<-k)
	}

	a.store[key] = cache
}

// Release implements the Adapter interface Release method.
func (a *Adapter) Release(key string) {
	if _, ok := a.store[key]; ok {
		delete(a.store, key)
	}
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
		map[string]cache.Cache{},
	}, nil
}

func (a *Adapter) evict(key chan string) {
	switch a.algorithm {
	case "LRU":
		lruKey := ""
		lruLastAccess := time.Now()

		for key, value := range a.store {
			if value.LastAccess.Before(lruLastAccess) {
				lruKey = key
				lruLastAccess = value.LastAccess
			}
		}

		key <- lruKey
	case "MRU":
		mruKey := ""
		mruLastAccess := time.Time{}

		for key, value := range a.store {
			if value.LastAccess.After(mruLastAccess) ||
				value.LastAccess.Equal(mruLastAccess) {
				mruKey = key
				mruLastAccess = value.LastAccess
			}
		}

		key <- mruKey
	case "LFU":
		lfuKey := ""
		lfuFrequency := 9999999999999

		for key, value := range a.store {
			if value.Frequency < lfuFrequency {
				lfuKey = key
				lfuFrequency = value.Frequency
			}
		}

		key <- lfuKey
	case "MFU":
		mfuKey := ""
		mfuFrequency := 0

		for key, value := range a.store {
			if value.Frequency >= mfuFrequency {
				mfuKey = key
				mfuFrequency = value.Frequency
			}
		}

		key <- mfuKey
	}
}
