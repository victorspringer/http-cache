package redis

import (
	"reflect"
	"testing"
	"time"

	goredis "github.com/go-redis/redis"
	"github.com/victorspringer/http-cache"
)

var a cache.Adapter

// requireRedis probes the configured Redis instance by writing and
// reading back a unique key. If the round-trip fails, the test is
// skipped instead of erroring out — Redis is an integration dependency
// and the absence of a local server should not produce a failing test
// suite.
func requireRedis(t *testing.T, adapter cache.Adapter) {
	t.Helper()
	probe := uint64(time.Now().UnixNano())
	adapter.Set(probe, []byte("probe"), time.Now().Add(1*time.Minute))
	if _, ok := adapter.Get(probe); !ok {
		t.Skip("redis not reachable on :6379; skipping integration test")
	}
	adapter.Release(probe)
}

func TestSet(t *testing.T) {
	a = NewAdapter(&RingOptions{
		Addrs: map[string]string{
			"server": ":6379",
		},
	})
	requireRedis(t, a)

	tests := []struct {
		name     string
		key      uint64
		response []byte
	}{
		{
			"sets a response cache",
			1,
			cache.Response{
				Value:      []byte("value 1"),
				Expiration: time.Now().Add(1 * time.Minute),
			}.Bytes(),
		},
		{
			"sets a response cache",
			2,
			cache.Response{
				Value:      []byte("value 2"),
				Expiration: time.Now().Add(1 * time.Minute),
			}.Bytes(),
		},
		{
			"sets a response cache",
			3,
			cache.Response{
				Value:      []byte("value 3"),
				Expiration: time.Now().Add(1 * time.Minute),
			}.Bytes(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a.Set(tt.key, tt.response, time.Now().Add(1*time.Minute))
		})
	}
}

func TestGet(t *testing.T) {
	if a == nil {
		t.Skip("redis adapter was not initialized (TestSet skipped); skipping")
	}
	requireRedis(t, a)
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
			b, ok := a.Get(tt.key)
			if ok != tt.ok {
				t.Errorf("memory.Get() ok = %v, tt.ok %v", ok, tt.ok)
				return
			}
			got := cache.BytesToResponse(b).Value
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("memory.Get() = %v, want %v", string(got), string(tt.want))
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

func TestExpiration(t *testing.T) {
	a = NewAdapter(&RingOptions{
		Addrs: map[string]string{
			"server": ":6379",
		},
	})
	requireRedis(t, a)

	t.Run("sets response without expiration", func(t *testing.T) {
		key := uint64(10)
		response := cache.Response{
			Value: []byte("no expiration"),
		}.Bytes()

		a.Set(key, response, time.Time{})
		t.Cleanup(func() {
			a.Release(key)
		})

		b, ok := a.Get(key)
		if !ok {
			t.Fatal("response was not cached")
		}

		got := cache.BytesToResponse(b).Value
		if !reflect.DeepEqual(got, []byte("no expiration")) {
			t.Fatalf("memory.Get() = %v, want %v", string(got), "no expiration")
		}
	})
}

func TestNewAdapterWithClient(t *testing.T) {
	client := goredis.NewClient(&goredis.Options{
		Addr: ":6379",
	})
	defer client.Close()

	adapter := NewAdapterWithClient(client)
	requireRedis(t, adapter)
	key := uint64(20)
	response := cache.Response{
		Value: []byte("standalone client"),
	}.Bytes()

	adapter.Set(key, response, time.Now().Add(1*time.Minute))
	t.Cleanup(func() {
		adapter.Release(key)
	})

	b, ok := adapter.Get(key)
	if !ok {
		t.Fatal("response was not cached")
	}

	got := cache.BytesToResponse(b).Value
	if !reflect.DeepEqual(got, []byte("standalone client")) {
		t.Fatalf("memory.Get() = %v, want %v", string(got), "standalone client")
	}
}
