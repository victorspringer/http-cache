package cache

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A handler that returns without calling Write or WriteHeader (an early
// error return, a redirect via header-only mutation, an aborted RPC)
// must not be cached. The previous implementation cached an empty 200
// OK response in that case, masking subsequent requests' behavior.
func TestMiddlewareDoesNotCacheWhenHandlerNeverWrites(t *testing.T) {
	adapter := &adapterMock{store: map[uint64][]byte{}}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}

	calls := 0
	handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		// no Write, no WriteHeader -- early-return path.
	}))

	for i := 0; i < 2; i++ {
		handler.ServeHTTP(httptest.NewRecorder(),
			httptest.NewRequest(http.MethodGet, "http://x/empty", nil))
	}
	if calls != 2 {
		t.Fatalf("handler called %d times, want 2 (silent return must not cache)", calls)
	}
	if _, ok := adapter.Get(generateKey("http://x/empty")); ok {
		t.Fatal("empty silent response was cached")
	}
}

// A handler that only sets headers but never writes a body or calls
// WriteHeader is equivalent to the silent-return case -- Go defaults to
// 200 OK, but the handler did not signal an intent to respond.
func TestMiddlewareDoesNotCacheHeaderOnlyResponse(t *testing.T) {
	adapter := &adapterMock{store: map[uint64][]byte{}}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}

	calls := 0
	handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("X-Some-Header", "value")
		// no Write, no WriteHeader.
	}))

	for i := 0; i < 2; i++ {
		handler.ServeHTTP(httptest.NewRecorder(),
			httptest.NewRequest(http.MethodGet, "http://x/headers-only", nil))
	}
	if calls != 2 {
		t.Fatalf("handler called %d times, want 2", calls)
	}
}

// A handler that calls WriteHeader explicitly (even for a body-less
// status like 204) signaled an intent to respond and the entry must
// still be cached when the status passes the filter.
func TestMiddlewareCachesExplicitWriteHeader(t *testing.T) {
	adapter := &adapterMock{store: map[uint64][]byte{}}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}

	calls := 0
	handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}))

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w,
			httptest.NewRequest(http.MethodGet, "http://x/no-content", nil))
		if w.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
		}
	}
	if calls != 1 {
		t.Fatalf("handler called %d times, want 1 (204 must be cached)", calls)
	}
}
