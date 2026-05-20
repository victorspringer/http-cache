package cache

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Without the opt-in, Cache-Control on the response is ignored: the
// historical behavior of always-cache continues so existing users see
// no surprises.
func TestMiddlewareIgnoresCacheControlByDefault(t *testing.T) {
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
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintf(w, "v=%d", calls)
	}))

	for i := 0; i < 2; i++ {
		handler.ServeHTTP(httptest.NewRecorder(),
			httptest.NewRequest(http.MethodGet, "http://x/cc-default", nil))
	}
	if calls != 1 {
		t.Fatalf("handler called %d times, want 1 (Cache-Control must be ignored without opt-in)", calls)
	}
}

// With the opt-in, a response with Cache-Control: no-store must not be
// stored, and subsequent requests must re-invoke the handler.
func TestMiddlewareRespectsResponseNoStore(t *testing.T) {
	adapter := &adapterMock{store: map[uint64][]byte{}}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
		ClientWithRespectCacheControl(),
	)
	if err != nil {
		t.Fatal(err)
	}

	calls := 0
	handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintf(w, "v=%d", calls)
	}))

	for i := 0; i < 2; i++ {
		handler.ServeHTTP(httptest.NewRecorder(),
			httptest.NewRequest(http.MethodGet, "http://x/cc-no-store", nil))
	}
	if calls != 2 {
		t.Fatalf("handler called %d times, want 2", calls)
	}
}

// Cache-Control: private targets a per-user cache; a shared HTTP cache
// like this one must not store it.
func TestMiddlewareRespectsResponsePrivate(t *testing.T) {
	adapter := &adapterMock{store: map[uint64][]byte{}}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
		ClientWithRespectCacheControl(),
	)
	if err != nil {
		t.Fatal(err)
	}

	calls := 0
	handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Cache-Control", "private, max-age=60")
		fmt.Fprintf(w, "v=%d", calls)
	}))

	for i := 0; i < 2; i++ {
		handler.ServeHTTP(httptest.NewRecorder(),
			httptest.NewRequest(http.MethodGet, "http://x/cc-private", nil))
	}
	if calls != 2 {
		t.Fatalf("handler called %d times, want 2 (private must not be stored)", calls)
	}
}

// max-age overrides the client TTL when the opt-in is enabled.
// s-maxage takes precedence for shared caches.
func TestMiddlewareRespectsResponseMaxAge(t *testing.T) {
	tests := []struct {
		name      string
		header    string
		wantSecs  int
	}{
		{"max-age sets TTL", "max-age=42", 42},
		{"s-maxage wins for shared caches", "max-age=10, s-maxage=99", 99},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const url = "http://x/cc-maxage"
			adapter := &adapterMock{store: map[uint64][]byte{}}
			client, err := NewClient(
				ClientWithAdapter(adapter),
				ClientWithTTL(1*time.Hour),
				ClientWithRespectCacheControl(),
			)
			if err != nil {
				t.Fatal(err)
			}

			handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Cache-Control", tt.header)
				w.Write([]byte("ok"))
			}))

			before := time.Now()
			handler.ServeHTTP(httptest.NewRecorder(),
				httptest.NewRequest(http.MethodGet, url, nil))
			after := time.Now()

			stored, ok := adapter.Get(generateKey(url))
			if !ok {
				t.Fatal("response was not cached")
			}
			resp := BytesToResponse(stored)
			gotTTL := resp.Expiration.Sub(before)
			wantTTL := time.Duration(tt.wantSecs) * time.Second
			elapsed := after.Sub(before)
			// Allow slack for time measurement noise.
			if gotTTL < wantTTL-elapsed || gotTTL > wantTTL+100*time.Millisecond {
				t.Errorf("TTL = %v, want approximately %v", gotTTL, wantTTL)
			}
		})
	}
}

// Cache-Control: no-store on the request bypasses the cache entirely:
// no lookup, no store.
func TestMiddlewareRespectsRequestNoStore(t *testing.T) {
	adapter := &adapterMock{
		store: map[uint64][]byte{
			generateKey("http://x/cc-req"): Response{
				Value:      []byte("cached"),
				Expiration: time.Now().Add(1 * time.Minute),
			}.Bytes(),
		},
	}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
		ClientWithRespectCacheControl(),
	)
	if err != nil {
		t.Fatal(err)
	}

	calls := 0
	handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte("fresh"))
	}))

	r := httptest.NewRequest(http.MethodGet, "http://x/cc-req", nil)
	r.Header.Set("Cache-Control", "no-store")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if got := w.Body.String(); got != "fresh" {
		t.Errorf("body = %q, want fresh", got)
	}
	if calls != 1 {
		t.Errorf("handler calls = %d, want 1", calls)
	}
}

// Cache-Control: no-cache on the request forces a revalidation (we
// treat that as a miss and serve fresh) but the response is still
// stored for subsequent callers without the no-cache header.
func TestMiddlewareRespectsRequestNoCache(t *testing.T) {
	const url = "http://x/cc-req-nc"
	adapter := &adapterMock{
		store: map[uint64][]byte{
			generateKey(url): Response{
				Value:      []byte("old"),
				Expiration: time.Now().Add(1 * time.Minute),
			}.Bytes(),
		},
	}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
		ClientWithRespectCacheControl(),
	)
	if err != nil {
		t.Fatal(err)
	}

	calls := 0
	handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte("fresh"))
	}))

	r := httptest.NewRequest(http.MethodGet, url, nil)
	r.Header.Set("Cache-Control", "no-cache")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if got := w.Body.String(); got != "fresh" {
		t.Errorf("body = %q, want fresh", got)
	}
	if calls != 1 {
		t.Errorf("handler calls = %d, want 1", calls)
	}

	// Second request without no-cache hits the freshly-stored response.
	r = httptest.NewRequest(http.MethodGet, url, nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if got := w.Body.String(); got != "fresh" {
		t.Errorf("second request body = %q, want fresh", got)
	}
	if calls != 1 {
		t.Errorf("handler calls = %d, want 1 (must hit cache)", calls)
	}
}
