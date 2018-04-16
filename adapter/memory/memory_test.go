package memory

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/victorspringer/http-cache"
)

func TestGet(t *testing.T) {
	a := &Adapter{
		sync.Mutex{},
		2,
		LRU,
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
			got, ok := a.Get(tt.key)
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
	a := &Adapter{
		sync.Mutex{},
		2,
		LRU,
		map[string]cache.Cache{},
	}

	tests := []struct {
		name  string
		key   string
		cache cache.Cache
	}{
		{
			"sets a response cache",
			"first",
			cache.Cache{
				Value:      []byte("value 1"),
				Expiration: time.Now().Add(1 * time.Minute),
			},
		},
		{
			"sets a response cache",
			"second",
			cache.Cache{
				Value:      []byte("value 2"),
				Expiration: time.Now().Add(1 * time.Minute),
			},
		},
		{
			"sets a response cache",
			"third",
			cache.Cache{
				Value:      []byte("value 3"),
				Expiration: time.Now().Add(1 * time.Minute),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a.Set(tt.key, tt.cache)
			if a.store[tt.key].Value == nil {
				t.Errorf("memory.Set() error = store[%s] response is not %s", tt.key, tt.cache.Value)
			}
		})
	}
}

func TestRelease(t *testing.T) {
	a := &Adapter{
		sync.Mutex{},
		2,
		LRU,
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
			a.Release(tt.key)
			if len(a.store) > tt.storeLength {
				t.Errorf("memory.Release() error; store length = %v, want 0", len(a.store))
			}
		})
	}
}

func TestEvict(t *testing.T) {
	done := make(chan string, 1)
	tests := []struct {
		name      string
		algorithm Algorithm
	}{
		{
			"lru removes third cache",
			LRU,
		},
		{
			"mru removes first cache",
			MRU,
		},
		{
			"lfu removes second cache",
			LFU,
		},
		{
			"mfu removes third cache",
			MFU,
		},
	}
	count := 0
	for _, tt := range tests {
		count++
		a := &Adapter{
			sync.Mutex{},
			2,
			tt.algorithm,
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
			a.evict(done)
			key := <-done
			if count == 1 {
				if key != "e7bc18936aeeee6fa96bd9410a3970f4" {
					t.Errorf("lru is not working properly")
					return
				}
			} else if count == 2 {
				if key != "1e13f750b4d13e03a775f9d09032f87b" {
					t.Errorf("mru is not working properly")
					return
				}
			} else if count == 3 {
				if key != "48c169c22f6ae6351993050852982723" {
					t.Errorf("lfu is not working properly")
					return
				}
			} else {
				if count == 4 {
					if key != "e7bc18936aeeee6fa96bd9410a3970f4" {
						t.Errorf("mfu is not working properly")
					}
				}
			}
		})
	}
}

func TestNewAdapter(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		want    cache.Adapter
		wantErr bool
	}{
		{
			"returns new Adapter",
			&Config{
				4,
				LRU,
			},
			&Adapter{
				sync.Mutex{},
				4,
				LRU,
				map[string]cache.Cache{},
			},
			false,
		},
		{
			"returns error",
			&Config{
				Algorithm: LRU,
			},
			nil,
			true,
		},
		{
			"returns error",
			&Config{
				Capacity: 4,
			},
			nil,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewAdapter(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewAdapter() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewAdapter() = %v, want %v", got, tt.want)
			}
		})
	}
}
