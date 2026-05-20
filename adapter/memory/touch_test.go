package memory

import (
	"sync"
	"testing"
	"time"

	cache "github.com/victorspringer/http-cache"
)

// The memory adapter must satisfy the optional cache.AdapterTouch
// extension so the middleware can record hits atomically without
// re-serializing the response.
func TestAdapterImplementsAdapterTouch(t *testing.T) {
	a, err := NewAdapter(
		AdapterWithCapacity(4),
		AdapterWithAlgorithm(LFU),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(cache.AdapterTouch); !ok {
		t.Fatal("memory.Adapter does not implement cache.AdapterTouch")
	}
}

// Concurrent Touch calls must not lose frequency increments and the
// adapter must use those counters to drive LFU eviction. Previously,
// LFU/MFU eviction relied on the in-blob Response.Frequency counter,
// which was racy under concurrent middleware hits.
func TestAdapterTouchDrivesLFUEviction(t *testing.T) {
	a, err := NewAdapter(
		AdapterWithCapacity(3),
		AdapterWithAlgorithm(LFU),
	)
	if err != nil {
		t.Fatal(err)
	}

	resp := cache.Response{Value: []byte("v"), Expiration: time.Now().Add(1 * time.Hour)}
	a.Set(1, resp.Bytes(), resp.Expiration)
	a.Set(2, resp.Bytes(), resp.Expiration)
	a.Set(3, resp.Bytes(), resp.Expiration)

	tch, ok := a.(cache.AdapterTouch)
	if !ok {
		t.Fatal("memory.Adapter does not implement cache.AdapterTouch")
	}

	const N = 500
	var wg sync.WaitGroup
	wg.Add(2 * N)
	for i := 0; i < N; i++ {
		go func() { defer wg.Done(); tch.Touch(1) }()
		go func() { defer wg.Done(); tch.Touch(2) }()
	}
	wg.Wait()
	// key 3 stays at the baseline frequency from Set.

	// Insert a fourth entry: cache is full, LFU must evict key 3 (coldest).
	a.Set(4, resp.Bytes(), resp.Expiration)

	if _, ok := a.Get(3); ok {
		t.Error("LFU did not evict the least-frequent key 3")
	}
	if _, ok := a.Get(1); !ok {
		t.Error("LFU evicted the hottest key 1")
	}
	if _, ok := a.Get(2); !ok {
		t.Error("LFU evicted the second-hottest key 2")
	}
}

// Touch on a missing key must be a no-op (the entry may have been
// evicted between the middleware's Get and the Touch call).
func TestAdapterTouchMissingKeyIsNoop(t *testing.T) {
	a, err := NewAdapter(
		AdapterWithCapacity(2),
		AdapterWithAlgorithm(LRU),
	)
	if err != nil {
		t.Fatal(err)
	}
	tch := a.(cache.AdapterTouch)
	tch.Touch(99) // must not panic
}
