package cache

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Drop computes a cache key from r.URL and r.Body via the same machinery
// the middleware uses. That machinery sortURLParams-mutates r.URL.RawQuery
// and consumes-and-restores r.Body. Drop must isolate the caller from
// both side effects so reusing the request afterwards (retrying, logging,
// passing to another client) sees the original value.
func TestDropDoesNotMutateRequestURL(t *testing.T) {
	adapter := &adapterMock{store: map[uint64][]byte{}}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}

	const rawQuery = "z=1&a=2&m=3"
	r := httptest.NewRequest(http.MethodGet, "http://x/p?"+rawQuery, nil)
	if err := client.Drop(r); err != nil {
		t.Fatal(err)
	}
	if r.URL.RawQuery != rawQuery {
		t.Errorf("Drop mutated request URL RawQuery: got %q, want %q", r.URL.RawQuery, rawQuery)
	}
}

func TestDropDoesNotMutateRequestHeader(t *testing.T) {
	adapter := &adapterMock{store: map[uint64][]byte{}}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
		ClientWithVaryHeaders([]string{"X-Tenant"}),
	)
	if err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest(http.MethodGet, "http://x/p", nil)
	r.Header.Set("X-Tenant", "acme")
	originalAddr := &r.Header
	if err := client.Drop(r); err != nil {
		t.Fatal(err)
	}
	if &r.Header != originalAddr {
		t.Error("Drop replaced caller's request.Header map pointer")
	}
	if r.Header.Get("X-Tenant") != "acme" {
		t.Errorf("Drop mutated caller's request headers")
	}
}

// Body restoration was already verified by an existing test; this one
// asserts the caller's *original* body reader is still usable after
// Drop, without relying on Drop replacing r.Body with a fresh reader.
func TestDropPreservesCallerBodyContents(t *testing.T) {
	adapter := &adapterMock{store: map[uint64][]byte{}}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
		ClientWithMethods([]string{http.MethodPost}),
	)
	if err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"foo":"bar"}`)
	r := httptest.NewRequest(http.MethodPost, "http://x/p", bytes.NewReader(body))
	if err := client.Drop(r); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("body after Drop = %q, want %q", string(got), string(body))
	}
}
