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
		sync.RWMutex{},
		2,
		LRU,
		map[uint64][]byte{
			14974843192121052621: cache.Response{
				Value:      []byte("value 1"),
				Expiration: time.Now(),
				LastAccess: time.Now(),
				Frequency:  1,
			}.Bytes(),
		},
	}

	tests := []struct {
		name string
		key  uint64
		want []byte
		ok   bool
	}{
		{
			"returns right response",
			14974843192121052621,
			[]byte("value 1"),
			true,
		},
		{
			"not found",
			123,
			nil,
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, ok := a.Get(tt.key)
			if ok != tt.ok {
				t.Errorf("memory.Get() ok = %v, tt.ok %v", ok, tt.ok)
				return
			}
			got := cache.BytesToResponse(b).Value
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("memory.Get() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSet(t *testing.T) {
	a := &Adapter{
		sync.RWMutex{},
		2,
		LRU,
		make(map[uint64][]byte),
	}

	tests := []struct {
		name     string
		key      uint64
		response cache.Response
	}{
		{
			"sets a response cache",
			1,
			cache.Response{
				Value:      []byte("value 1"),
				Expiration: time.Now().Add(1 * time.Minute),
			},
		},
		{
			"sets a response cache",
			2,
			cache.Response{
				Value:      []byte("value 2"),
				Expiration: time.Now().Add(1 * time.Minute),
			},
		},
		{
			"sets a response cache",
			3,
			cache.Response{
				Value:      []byte("value 3"),
				Expiration: time.Now().Add(1 * time.Minute),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a.Set(tt.key, tt.response.Bytes(), tt.response.Expiration)
			if cache.BytesToResponse(a.store[tt.key]).Value == nil {
				t.Errorf(
					"memory.Set() error = store[%v] response is not %s", tt.key, tt.response.Value,
				)
			}
		})
	}
}

func TestRelease(t *testing.T) {
	a := &Adapter{
		sync.RWMutex{},
		2,
		LRU,
		map[uint64][]byte{
			14974843192121052621: cache.Response{
				Expiration: time.Now().Add(1 * time.Minute),
				Value:      []byte("value 1"),
			}.Bytes(),
			14974839893586167988: cache.Response{
				Expiration: time.Now(),
				Value:      []byte("value 2"),
			}.Bytes(),
			14974840993097796199: cache.Response{
				Expiration: time.Now(),
				Value:      []byte("value 3"),
			}.Bytes(),
		},
	}

	tests := []struct {
		name        string
		key         uint64
		storeLength int
		wantErr     bool
	}{
		{
			"removes cached response from store",
			14974843192121052621,
			2,
			false,
		},
		{
			"removes cached response from store",
			14974839893586167988,
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
	k := make(chan uint64, 1)

	tests := []struct {
		name      string
		algorithm Algorithm
	}{
		{
			"lru removes third cached response",
			LRU,
		},
		{
			"mru removes first cached response",
			MRU,
		},
		{
			"lfu removes second cached response",
			LFU,
		},
		{
			"mfu removes third cached response",
			MFU,
		},
	}
	count := 0
	for _, tt := range tests {
		count++

		a := &Adapter{
			sync.RWMutex{},
			2,
			tt.algorithm,
			map[uint64][]byte{
				14974843192121052621: cache.Response{
					Value:      []byte("value 1"),
					Expiration: time.Now().Add(1 * time.Minute),
					LastAccess: time.Now().Add(-1 * time.Minute),
					Frequency:  2,
				}.Bytes(),
				14974839893586167988: cache.Response{
					Value:      []byte("value 2"),
					Expiration: time.Now().Add(1 * time.Minute),
					LastAccess: time.Now().Add(-2 * time.Minute),
					Frequency:  1,
				}.Bytes(),
				14974840993097796199: cache.Response{
					Value:      []byte("value 3"),
					Expiration: time.Now().Add(1 * time.Minute),
					LastAccess: time.Now().Add(-3 * time.Minute),
					Frequency:  3,
				}.Bytes(),
			},
		}
		t.Run(tt.name, func(t *testing.T) {
			a.evict(k)
			key := <-k

			if count == 1 {
				if key != 14974840993097796199 {
					t.Errorf("lru is not working properly")
					return
				}
			} else if count == 2 {
				if key != 14974843192121052621 {
					t.Errorf("mru is not working properly")
					return
				}
			} else if count == 3 {
				if key != 14974839893586167988 {
					t.Errorf("lfu is not working properly")
					return
				}
			} else {
				if count == 4 {
					if key != 14974840993097796199 {
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
		opts    []AdapterOptions
		want    cache.Adapter
		wantErr bool
	}{
		{
			"returns new Adapter",
			[]AdapterOptions{
				AdapterWithCapacity(4),
				AdapterWithAlgorithm(LRU),
			},
			&Adapter{
				sync.RWMutex{},
				4,
				LRU,
				make(map[uint64][]byte),
			},
			false,
		},
		{
			"returns error",
			[]AdapterOptions{
				AdapterWithAlgorithm(LRU),
			},
			nil,
			true,
		},
		{
			"returns error",
			[]AdapterOptions{
				AdapterWithCapacity(4),
			},
			nil,
			true,
		},
		{
			"returns error",
			[]AdapterOptions{
				AdapterWithCapacity(1),
			},
			nil,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewAdapter(tt.opts...)
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
