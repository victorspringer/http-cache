package cache

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// PURGE for a POST-cached endpoint must include the same body so its
// cache key matches what was stored. Without body-aware PURGE keying
// the entry is never found and PURGE becomes silently inert.
func TestMiddlewarePurgeReleasesCachedPostByBody(t *testing.T) {
	adapter := &adapterMock{store: map[uint64][]byte{}}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
		ClientWithMethods([]string{http.MethodPost}),
		ClientWithPurge(),
	)
	if err != nil {
		t.Fatal(err)
	}

	calls := 0
	handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprintf(w, "v=%d", calls)
	}))

	body := []byte(`{"id":1}`)

	// Warm via POST.
	r := httptest.NewRequest(http.MethodPost, "http://x/users", bytes.NewReader(body))
	handler.ServeHTTP(httptest.NewRecorder(), r)

	// Confirm it's cached: second POST hits cache.
	r = httptest.NewRequest(http.MethodPost, "http://x/users", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if got := w.Body.String(); got != "v=1" {
		t.Fatalf("post hit body = %q, want v=1", got)
	}

	// PURGE with the same body must release the entry.
	r = httptest.NewRequest("PURGE", "http://x/users", bytes.NewReader(body))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("purge status = %d, want %d", w.Code, http.StatusNoContent)
	}

	// Next POST with the same body must miss and re-invoke the handler.
	r = httptest.NewRequest(http.MethodPost, "http://x/users", bytes.NewReader(body))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if got := w.Body.String(); got != "v=2" {
		t.Fatalf("post after purge body = %q, want v=2", got)
	}
}

// PURGE with a different body must not invalidate an unrelated cached
// entry. Each (URL, body) pair is its own cache key.
func TestMiddlewarePurgeDoesNotReleaseDifferentBody(t *testing.T) {
	adapter := &adapterMock{store: map[uint64][]byte{}}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
		ClientWithMethods([]string{http.MethodPost}),
		ClientWithPurge(),
	)
	if err != nil {
		t.Fatal(err)
	}

	calls := 0
	handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprintf(w, "v=%d", calls)
	}))

	bodyA := []byte(`{"id":1}`)
	bodyB := []byte(`{"id":2}`)

	handler.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "http://x/users", bytes.NewReader(bodyA)))

	// PURGE for a different body.
	handler.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest("PURGE", "http://x/users", bytes.NewReader(bodyB)))

	// Original (bodyA) entry must still be cached.
	w := httptest.NewRecorder()
	handler.ServeHTTP(w,
		httptest.NewRequest(http.MethodPost, "http://x/users", bytes.NewReader(bodyA)))
	if got := w.Body.String(); got != "v=1" {
		t.Fatalf("body = %q, want v=1 (unrelated PURGE must not invalidate)", got)
	}
}
