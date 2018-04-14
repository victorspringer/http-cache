package adapter

import (
	"sync"
	"time"

	"github.com/victorspringer/http-cache"
)

type memory struct {
	sync.Mutex
	store map[string]cache.Cache
}

func (m *memory) Get(key string) (cache.Cache, bool) {
	m.Lock()
	defer m.Unlock()

	if cache, ok := m.store[key]; ok {
		return cache, true
	}

	return cache.Cache{}, false
}

func (m *memory) Set(key string, cache cache.Cache) {
	m.Lock()
	defer m.Unlock()

	m.store[key] = cache
}

func (m *memory) Release(key string) {
	m.Lock()
	defer m.Unlock()

	if _, ok := m.store[key]; ok {
		delete(m.store, key)
	}
}

func (m *memory) Length() int {
	m.Lock()
	defer m.Unlock()

	return len(m.store)
}

func (m *memory) Evict(algorithm cache.Algorithm) {
	switch algorithm {
	case "LRU":
		m.lru()
	case "MRU":
		m.mru()
	case "LFU":
		m.lfu()
	case "MFU":
		m.mfu()
	}
}

func (m *memory) lru() {
	m.Lock()
	defer m.Unlock()

	lruKey := ""
	lruLastAccess := time.Now()

	for key, value := range m.store {
		if value.LastAccess.Before(lruLastAccess) {
			lruKey = key
			lruLastAccess = value.LastAccess
		}
	}

	go m.Release(lruKey)
}

func (m *memory) mru() {
	m.Lock()
	defer m.Unlock()

	mruKey := ""
	mruLastAccess := time.Time{}

	for key, value := range m.store {
		if value.LastAccess.After(mruLastAccess) ||
			value.LastAccess.Equal(mruLastAccess) {
			mruKey = key
			mruLastAccess = value.LastAccess
		}
	}

	go m.Release(mruKey)
}

func (m *memory) lfu() {
	m.Lock()
	defer m.Unlock()

	lfuKey := ""
	lfuFrequency := 9999999999999

	for key, value := range m.store {
		if value.Frequency < lfuFrequency {
			lfuKey = key
			lfuFrequency = value.Frequency
		}
	}

	go m.Release(lfuKey)
}

func (m *memory) mfu() {
	m.Lock()
	defer m.Unlock()

	mfuKey := ""
	mfuFrequency := 0

	for key, value := range m.store {
		if value.Frequency >= mfuFrequency {
			mfuKey = key
			mfuFrequency = value.Frequency
		}
	}

	go m.Release(mfuKey)
}
