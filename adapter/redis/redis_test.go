package redis

import (
	"reflect"
	"testing"
	"time"

	"github.com/victorspringer/http-cache"
)

var a cache.Adapter

func TestSet(t *testing.T) {
	a = NewAdapter()

	tests := []struct {
		name  string
		key   uint64
		cache cache.Cache
	}{
		{
			"sets a response cache",
			1,
			cache.Cache{
				Value:      []byte("value 1"),
				Expiration: time.Now().Add(1 * time.Minute),
			},
		},
		{
			"sets a response cache",
			2,
			cache.Cache{
				Value:      []byte("value 2"),
				Expiration: time.Now().Add(1 * time.Minute),
			},
		},
		{
			"sets a response cache",
			3,
			cache.Cache{
				Value:      []byte("value 3"),
				Expiration: time.Now().Add(1 * time.Minute),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a.Set(tt.key, tt.cache)
		})
	}
}

func TestGet(t *testing.T) {
	tests := []struct {
		name string
		key  uint64
		want []byte
		ok   bool
	}{
		{
			"returns right response",
			1,
			[]byte("value 1"),
			true,
		},
		{
			"returns right response",
			2,
			[]byte("value 2"),
			true,
		},
		{
			"key does not exist",
			4,
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
				t.Errorf("memory.Get() = %v, want %v", string(got.Value), string(tt.want))
			}
		})
	}
}

func TestRelease(t *testing.T) {
	tests := []struct {
		name string
		key  uint64
	}{
		{
			"removes cached response from store",
			1,
		},
		{
			"removes cached response from store",
			2,
		},
		{
			"removes cached response from store",
			3,
		},
		{
			"key does not exist",
			4,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a.Release(tt.key)
			if _, ok := a.Get(tt.key); ok {
				t.Errorf("memory.Release() error; key %v should not be found", tt.key)
			}
		})
	}
}
