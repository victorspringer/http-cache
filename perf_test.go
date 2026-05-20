package cache

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A cached hit must not write to a client that has already
// disconnected. Detecting via r.Context().Err() avoids spending CPU
// and bandwidth on a connection that will not consume the bytes.
func TestMiddlewareSkipsCachedHitForCanceledContext(t *testing.T) {
	const url = "http://x/canceled"
	adapter := &adapterMock{
		store: map[uint64][]byte{
			generateKey(url): Response{
				Value:      []byte("cached body"),
				Expiration: time.Now().Add(1 * time.Minute),
			}.Bytes(),
		},
	}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}

	handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("origin handler should not be invoked on a cache hit")
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest(http.MethodGet, url, nil).WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Body.Len() != 0 {
		t.Errorf("middleware wrote %q to a canceled request", w.Body.String())
	}
}

// Verifies that the SHA-256 pool reuse path produces the same
// fingerprint as a fresh hasher (regression guard).
func TestCanonicalFingerprintDeterministicAcrossPoolReuse(t *testing.T) {
	headers := http.Header{}
	headers.Add("X-Tenant", "acme")

	first := canonicalFingerprint("http://x/path?a=1", []byte("body"), headers, []string{"X-Tenant"})
	for i := 0; i < 1000; i++ {
		got := canonicalFingerprint("http://x/path?a=1", []byte("body"), headers, []string{"X-Tenant"})
		if string(got) != string(first) {
			t.Fatalf("iteration %d: fingerprint diverged after pool reuse", i)
		}
		// Compute a different fingerprint to exercise pool reset.
		_ = canonicalFingerprint("http://x/other", nil, nil, nil)
	}
}
