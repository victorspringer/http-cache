package cache

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type touchingAdapter struct {
	mu       sync.Mutex
	store    map[uint64][]byte
	setCalls int64
	touches  int64
}

func (a *touchingAdapter) Get(key uint64) ([]byte, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	v, ok := a.store[key]
	return v, ok
}
func (a *touchingAdapter) Set(key uint64, b []byte, _ time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	atomic.AddInt64(&a.setCalls, 1)
	a.store[key] = b
}
func (a *touchingAdapter) Release(key uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.store, key)
}
func (a *touchingAdapter) Touch(key uint64) {
	atomic.AddInt64(&a.touches, 1)
}

// When the adapter implements AdapterTouch, the middleware must record
// the access via Touch and skip the read-modify-write Set that otherwise
// races and loses updates under concurrency.
func TestMiddlewareHitCallsTouchAndSkipsSet(t *testing.T) {
	adapter := &touchingAdapter{store: map[uint64][]byte{}}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}

	handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))

	handler.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "http://x/touch", nil))
	setsAfterWarm := atomic.LoadInt64(&adapter.setCalls)
	if setsAfterWarm != 1 {
		t.Fatalf("setCalls after warm = %d, want 1 (store)", setsAfterWarm)
	}

	const N = 200
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			handler.ServeHTTP(httptest.NewRecorder(),
				httptest.NewRequest(http.MethodGet, "http://x/touch", nil))
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt64(&adapter.setCalls); got != setsAfterWarm {
		t.Errorf("Set was called %d time(s) on hit (want 0)", got-setsAfterWarm)
	}
	if got := atomic.LoadInt64(&adapter.touches); got != N {
		t.Errorf("Touch calls = %d, want %d", got, N)
	}
}

// Backward compatibility: adapters that do not implement AdapterTouch
// keep the legacy in-blob bookkeeping (Frequency++ via Set), so existing
// custom adapters that depended on LastAccess/Frequency keep working.
func TestMiddlewareHitFallsBackToSetWhenAdapterDoesNotTouch(t *testing.T) {
	adapter := &adapterMock{store: map[uint64][]byte{}}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}

	handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))

	r := httptest.NewRequest(http.MethodGet, "http://x/legacy", nil)
	handler.ServeHTTP(httptest.NewRecorder(), r)

	// Two hits to bump Frequency past 1.
	handler.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "http://x/legacy", nil))
	handler.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "http://x/legacy", nil))

	stored, _ := adapter.Get(generateKey("http://x/legacy"))
	resp := BytesToResponse(stored)
	if resp.Frequency < 3 {
		t.Errorf("legacy adapter Frequency = %d, want >= 3 (warm + 2 hits)", resp.Frequency)
	}
}
