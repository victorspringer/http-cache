package cache

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// An entry that expired within the stale window must be served from
// cache while a background goroutine repopulates it. The first request
// returns the stale body immediately; eventually the cache entry is
// replaced with a fresh response from the origin.
func TestClientWithStaleWhileRevalidateServesStaleAndRefreshes(t *testing.T) {
	const url = "http://x/swr"
	adapter := &adapterMock{
		store: map[uint64][]byte{
			generateKey(url): Response{
				Value:      []byte("stale"),
				Expiration: time.Now().Add(-10 * time.Millisecond),
			}.Bytes(),
		},
	}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
		ClientWithStaleWhileRevalidate(1*time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}

	var calls int64
	refreshed := make(chan struct{}, 1)
	handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&calls, 1)
		fmt.Fprint(w, "fresh")
		select {
		case refreshed <- struct{}{}:
		default:
		}
	}))

	// First request: served stale immediately.
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
	if got := w.Body.String(); got != "stale" {
		t.Fatalf("first request body = %q, want stale", got)
	}

	// Wait for the background refresh to finish.
	select {
	case <-refreshed:
	case <-time.After(2 * time.Second):
		t.Fatal("background refresh never ran")
	}
	// Give the goroutine a moment to write to the adapter.
	time.Sleep(20 * time.Millisecond)

	stored, ok := adapter.Get(generateKey(url))
	if !ok {
		t.Fatal("cache entry missing after refresh")
	}
	resp := BytesToResponse(stored)
	if string(resp.Value) != "fresh" {
		t.Fatalf("refreshed cache value = %q, want fresh", string(resp.Value))
	}
	if got := atomic.LoadInt64(&calls); got != 1 {
		t.Fatalf("origin called %d times, want 1", got)
	}
}

// An entry that expired beyond the stale window falls through to the
// existing miss path (release + run handler synchronously).
func TestClientWithStaleWhileRevalidateRejectsTooStale(t *testing.T) {
	const url = "http://x/too-stale"
	adapter := &adapterMock{
		store: map[uint64][]byte{
			generateKey(url): Response{
				Value:      []byte("very stale"),
				Expiration: time.Now().Add(-2 * time.Second),
			}.Bytes(),
		},
	}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
		ClientWithStaleWhileRevalidate(500*time.Millisecond),
	)
	if err != nil {
		t.Fatal(err)
	}

	handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "fresh")
	}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
	if got := w.Body.String(); got != "fresh" {
		t.Fatalf("body = %q, want fresh (entry was outside the stale window)", got)
	}
}

// Many concurrent stale hits must trigger at most one background
// refresh per stale key.
func TestClientWithStaleWhileRevalidateCoalescesRefresh(t *testing.T) {
	const url = "http://x/swr-coalesce"
	adapter := &adapterMock{
		store: map[uint64][]byte{
			generateKey(url): Response{
				Value:      []byte("stale"),
				Expiration: time.Now().Add(-10 * time.Millisecond),
			}.Bytes(),
		},
	}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
		ClientWithStaleWhileRevalidate(1*time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}

	var calls int64
	release := make(chan struct{})
	handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&calls, 1)
		<-release
		fmt.Fprint(w, "fresh")
	}))

	const N = 30
	doneCh := make(chan struct{}, N)
	for i := 0; i < N; i++ {
		go func() {
			handler.ServeHTTP(httptest.NewRecorder(),
				httptest.NewRequest(http.MethodGet, url, nil))
			doneCh <- struct{}{}
		}()
	}

	// All N requests should return immediately with stale body.
	for i := 0; i < N; i++ {
		select {
		case <-doneCh:
		case <-time.After(1 * time.Second):
			t.Fatalf("only %d/%d stale responses returned in time", i, N)
		}
	}

	// Now release the (single) background refresh.
	close(release)
	time.Sleep(50 * time.Millisecond)

	if got := atomic.LoadInt64(&calls); got != 1 {
		t.Errorf("background origin called %d times, want exactly 1", got)
	}
}
