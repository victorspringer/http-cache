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
		})
	}
}

func TestGet(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want []byte
		ok   bool
	}{
		{
			"returns right response",
			"first",
			[]byte("value 1"),
			true,
		},
		{
			"returns right response",
			"second",
			[]byte("value 2"),
			true,
		},
		{
			"key does not exist",
			"fourth",
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
		key  string
	}{
		{
			"removes cached response from store",
			"first",
		},
		{
			"removes cached response from store",
			"second",
		},
		{
			"removes cached response from store",
			"third",
		},
		{
			"key does not exist",
			"fourth",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a.Release(tt.key)
			if _, ok := a.Get(tt.key); ok {
				t.Errorf("memory.Release() error; key %s should not be found", tt.key)
			}
		})
	}
}
