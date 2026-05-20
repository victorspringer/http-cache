package cache

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Demonstrates the corrupted-cache silent-serve bug: when the adapter
// returns bytes that fail to decode (data drift, key collision, schema
// change), the middleware must fall through to the origin handler rather
// than serve a zero-value Response (empty body, 200 OK).
func TestMiddlewareTreatsCorruptedEntryAsMiss(t *testing.T) {
	const url = "http://x/corrupt"
	adapter := &adapterMock{
		store: map[uint64][]byte{
			generateKey(url): []byte("not a valid gob blob at all"),
		},
	}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}

	originCalls := 0
	handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originCalls++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("origin"))
	}))

	r := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if originCalls != 1 {
		t.Fatalf("origin handler called %d times, want 1 (corrupted entry must be treated as miss)", originCalls)
	}
	if got := w.Body.String(); got != "origin" {
		t.Fatalf("body = %q, want %q", got, "origin")
	}
}
