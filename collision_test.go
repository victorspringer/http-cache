package cache

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// fixedKeyAdapter ignores the supplied key and stores a single entry,
// simulating a worst-case 100% hash collision: every request reads the
// same blob unless the middleware can detect the collision some other
// way.
type fixedKeyAdapter struct {
	mu   sync.Mutex
	blob []byte
}

func (a *fixedKeyAdapter) Get(uint64) ([]byte, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.blob == nil {
		return nil, false
	}
	return a.blob, true
}
func (a *fixedKeyAdapter) Set(_ uint64, b []byte, _ time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.blob = append(a.blob[:0], b...)
}
func (a *fixedKeyAdapter) Release(uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.blob = nil
}

// FNV-64 collisions are rare but inevitable at scale. When two distinct
// requests hash to the same key, the middleware must not serve one
// user's cached response to the other. Verification of the stored
// canonical key fingerprint catches this.
func TestMiddlewareDetectsHashCollisionViaCanonicalKey(t *testing.T) {
	adapter := &fixedKeyAdapter{}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}

	handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("response for " + r.URL.Path))
	}))

	// Warm the cache for /user-a.
	handler.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "http://x/user-a", nil))

	// Request /user-b: distinct URL, so canonical fingerprint differs.
	// With the fixed-key adapter, Get returns /user-a's blob; the
	// middleware must detect the mismatch and serve a fresh response.
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://x/user-b", nil))
	if got, want := w.Body.String(), "response for /user-b"; got != want {
		t.Errorf("cross-user leak: got %q, want %q", got, want)
	}
}

// Pre-upgrade entries written without a canonical fingerprint must
// continue to be served (backward compatibility for entries already
// living in Redis or other persistent adapters).
func TestMiddlewareServesLegacyEntriesWithoutCanonicalKey(t *testing.T) {
	const url = "http://x/legacy"
	legacy := Response{
		Value:      []byte("legacy body"),
		Expiration: time.Now().Add(1 * time.Minute),
	}.Bytes()
	adapter := &adapterMock{store: map[uint64][]byte{
		generateKey(url): legacy,
	}}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}

	handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("fresh"))
	}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
	if got, want := w.Body.String(), "legacy body"; got != want {
		t.Errorf("legacy entry not served: got %q, want %q", got, want)
	}
}

// New writes must include a canonical fingerprint so future collisions
// can be detected.
func TestMiddlewareWritesCanonicalKeyOnStore(t *testing.T) {
	adapter := &adapterMock{store: map[uint64][]byte{}}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}

	handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("body"))
	}))

	const url = "http://x/canon"
	handler.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, url, nil))

	stored, ok := adapter.Get(generateKey(url))
	if !ok {
		t.Fatal("response was not cached")
	}
	r := BytesToResponse(stored)
	if len(r.CanonicalKey) == 0 {
		t.Fatal("CanonicalKey was not written on store")
	}
	// The fingerprint should be deterministic.
	handler.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, url, nil))
	stored2, _ := adapter.Get(generateKey(url))
	r2 := BytesToResponse(stored2)
	if !bytes.Equal(r.CanonicalKey, r2.CanonicalKey) {
		t.Errorf("canonical fingerprint is not deterministic: %x vs %x", r.CanonicalKey, r2.CanonicalKey)
	}
}
