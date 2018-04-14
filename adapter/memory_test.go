package adapter

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/victorspringer/http-cache"
)

func TestGet(t *testing.T) {
	m := &memory{
		sync.Mutex{},
		map[string]cache.Cache{
			"1e13f750b4d13e03a775f9d09032f87b": cache.Cache{
				Value:      []byte("value 1"),
				Expiration: time.Now(),
				LastAccess: time.Now(),
				Frequency:  1,
			},
		},
	}

	tests := []struct {
		name string
		key  string
		want []byte
		ok   bool
	}{
		{
			"returns right response",
			"1e13f750b4d13e03a775f9d09032f87b",
			[]byte("value 1"),
			true,
		},
		{
			"not found",
			"foo",
			nil,
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := m.Get(tt.key)
			if ok != tt.ok {
				t.Errorf("memory.Get() ok = %v, tt.ok %v", ok, tt.ok)
				return
			}
			if !reflect.DeepEqual(got.Value, tt.want) {
				t.Errorf("memory.Get() = %v, want %v", got.Value, tt.want)
			}
		})
	}
}

func TestSet(t *testing.T) {
	m := &memory{
		sync.Mutex{},
		map[string]cache.Cache{},
	}

	tests := []struct {
		name  string
		key   string
		cache cache.Cache
	}{
		{
			"sets a response cache",
			"1e13f750b4d13e03a775f9d09032f87b",
			cache.Cache{
				Value:      []byte("value 1"),
				Expiration: time.Now().Add(1 * time.Minute),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.Set(tt.key, tt.cache)
			if m.store[tt.key].Value == nil {
				t.Errorf("memory.Set() error = store[%s] response is not %s", tt.key, tt.cache.Value)
			}
		})
	}
}

func TestRelease(t *testing.T) {
	m := &memory{
		sync.Mutex{},
		map[string]cache.Cache{
			"1e13f750b4d13e03a775f9d09032f87b": cache.Cache{
				Expiration: time.Now().Add(1 * time.Minute),
				Value:      []byte("value 1"),
			},
			"48c169c22f6ae6351993050852982723": cache.Cache{
				Expiration: time.Now(),
				Value:      []byte("value 2"),
			},
			"e7bc18936aeeee6fa96bd9410a3970f4": cache.Cache{
				Expiration: time.Now(),
				Value:      []byte("value 3"),
			},
		},
	}

	tests := []struct {
		name        string
		key         string
		storeLength int
		wantErr     bool
	}{
		{
			"removes cached response from store",
			"1e13f750b4d13e03a775f9d09032f87b",
			2,
			false,
		},
		{
			"removes cached response from store",
			"48c169c22f6ae6351993050852982723",
			1,
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.Release(tt.key)
			if len(m.store) > tt.storeLength {
				t.Errorf("memory.Release() error; store length = %v, want 0", len(m.store))
			}
		})
	}
}

func TestLength(t *testing.T) {
	m := &memory{
		sync.Mutex{},
		map[string]cache.Cache{
			"1e13f750b4d13e03a775f9d09032f87b": cache.Cache{
				Expiration: time.Now().Add(1 * time.Minute),
				Value:      []byte("value 1"),
			},
			"48c169c22f6ae6351993050852982723": cache.Cache{
				Expiration: time.Now(),
				Value:      []byte("value 2"),
			},
			"e7bc18936aeeee6fa96bd9410a3970f4": cache.Cache{
				Expiration: time.Now(),
				Value:      []byte("value 3"),
			},
		},
	}

	tests := []struct {
		name string
		want int
	}{
		{
			"returns right store lentgh",
			3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := m.Length(); got != tt.want {
				t.Errorf("memory.Length() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvict(t *testing.T) {
	tests := []struct {
		name      string
		algorithm cache.Algorithm
	}{
		{
			"lru removes third cache",
			cache.LRU,
		},
		{
			"mru removes first cache",
			cache.MRU,
		},
		{
			"lfu removes second cache",
			cache.LFU,
		},
		{
			"mfu removes third cache",
			cache.MFU,
		},
	}
	count := 0
	for _, tt := range tests {
		count++
		m := &memory{
			sync.Mutex{},
			map[string]cache.Cache{
				"1e13f750b4d13e03a775f9d09032f87b": cache.Cache{
					Value:      []byte("value 1"),
					Expiration: time.Now().Add(1 * time.Minute),
					LastAccess: time.Now().Add(-1 * time.Minute),
					Frequency:  2,
				},
				"48c169c22f6ae6351993050852982723": cache.Cache{
					Value:      []byte("value 2"),
					Expiration: time.Now().Add(1 * time.Minute),
					LastAccess: time.Now().Add(-2 * time.Minute),
					Frequency:  1,
				},
				"e7bc18936aeeee6fa96bd9410a3970f4": cache.Cache{
					Value:      []byte("value 3"),
					Expiration: time.Now().Add(1 * time.Minute),
					LastAccess: time.Now().Add(-3 * time.Minute),
					Frequency:  3,
				},
			},
		}
		t.Run(tt.name, func(t *testing.T) {
			m.Evict(tt.algorithm)
			time.Sleep(5 * time.Millisecond)
			if count == 1 {
				_, ok := m.Get("e7bc18936aeeee6fa96bd9410a3970f4")
				if ok {
					t.Errorf("lru is not working properly")
					return
				}
			} else if count == 2 {
				_, ok := m.Get("1e13f750b4d13e03a775f9d09032f87b")
				if ok {
					t.Errorf("mru is not working properly")
					return
				}
			} else if count == 3 {
				_, ok := m.Get("48c169c22f6ae6351993050852982723")
				if ok {
					t.Errorf("lfu is not working properly")
					return
				}
			} else {
				if count == 4 {
					_, ok := m.Get("e7bc18936aeeee6fa96bd9410a3970f4")
					if ok {
						t.Errorf("mfu is not working properly")
					}
				}
			}
		})
	}
}
