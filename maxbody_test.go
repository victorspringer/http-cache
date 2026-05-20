package cache

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// When the response body grows past the configured cap, the middleware
// must (a) keep streaming the full body to the client untouched and
// (b) not store the response in the cache. The buffered copy must also
// be released so a large response cannot run the server out of memory.
func TestClientWithMaxBodySizeSkipsCachingOversizedResponses(t *testing.T) {
	adapter := &adapterMock{store: map[uint64][]byte{}}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
		ClientWithMaxBodySize(64),
	)
	if err != nil {
		t.Fatal(err)
	}

	const big = 1024
	handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(strings.Repeat("a", big)))
	}))

	for i := 0; i < 2; i++ {
		r := httptest.NewRequest(http.MethodGet, "http://x/big", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Body.Len() != big {
			t.Fatalf("response truncated: got %d bytes, want %d", w.Body.Len(), big)
		}
	}

	if _, ok := adapter.Get(generateKey("http://x/big")); ok {
		t.Fatal("oversized response was cached")
	}
}

// Responses at or below the cap are cached as usual.
func TestClientWithMaxBodySizeCachesSmallResponses(t *testing.T) {
	adapter := &adapterMock{store: map[uint64][]byte{}}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
		ClientWithMaxBodySize(64),
	)
	if err != nil {
		t.Fatal(err)
	}

	calls := 0
	handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte("small"))
	}))

	for i := 0; i < 2; i++ {
		r := httptest.NewRequest(http.MethodGet, "http://x/small", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
	}
	if calls != 1 {
		t.Fatalf("handler called %d times, want 1 (second request must hit the cache)", calls)
	}
}

// Negative or zero caps are rejected; defaults preserve the historical
// "no limit" behavior.
func TestClientWithMaxBodySizeRejectsNonPositive(t *testing.T) {
	adapter := &adapterMock{store: map[uint64][]byte{}}
	if _, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
		ClientWithMaxBodySize(0),
	); err == nil {
		t.Fatal("ClientWithMaxBodySize(0) accepted, want error")
	}
	if _, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
		ClientWithMaxBodySize(-1),
	); err == nil {
		t.Fatal("ClientWithMaxBodySize(-1) accepted, want error")
	}
}
