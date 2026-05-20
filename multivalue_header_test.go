package cache

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"
)

type releaseCountingAdapter struct {
	sync.Mutex
	store        map[uint64][]byte
	releaseCalls int
}

func (a *releaseCountingAdapter) Get(key uint64) ([]byte, bool) {
	a.Lock()
	defer a.Unlock()
	v, ok := a.store[key]
	return v, ok
}
func (a *releaseCountingAdapter) Set(key uint64, b []byte, _ time.Time) {
	a.Lock()
	defer a.Unlock()
	a.store[key] = b
}
func (a *releaseCountingAdapter) Release(key uint64) {
	a.Lock()
	defer a.Unlock()
	a.releaseCalls++
	delete(a.store, key)
}

func TestMiddlewareDoesNotRefreshWhenRefreshKeyEmpty(t *testing.T) {
	adapter := &releaseCountingAdapter{store: map[uint64][]byte{}}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
		// no ClientWithRefreshKey -> refreshKey == ""
	)
	if err != nil {
		t.Fatal(err)
	}

	calls := 0
	handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte("v"))
	}))

	// Warm the cache for /foo.
	handler.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "http://x/foo", nil))

	// Send /foo?=junk: with refreshKey == "", params[""] would match and
	// release the cache entry for /foo. After the fix, no release happens.
	handler.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "http://x/foo?=junk", nil))

	if adapter.releaseCalls != 0 {
		t.Fatalf("release was called %d time(s); empty refreshKey must never trigger refresh", adapter.releaseCalls)
	}
	_ = calls
}

func TestMiddlewarePreservesMultiValuedHeaders(t *testing.T) {
	adapter := &adapterMock{store: map[uint64][]byte{}}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}

	handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "a=1; Path=/")
		w.Header().Add("Set-Cookie", "b=2; Path=/")
		w.Header().Add("Link", "</one>; rel=preload")
		w.Header().Add("Link", "</two>; rel=preload")
		w.Write([]byte("ok"))
	}))

	wantCookies := []string{"a=1; Path=/", "b=2; Path=/"}
	wantLinks := []string{"</one>; rel=preload", "</two>; rel=preload"}

	for i, label := range []string{"first", "cached"} {
		r := httptest.NewRequest(http.MethodGet, "http://x/cookies", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		gotCookies := w.Result().Header.Values("Set-Cookie")
		if !reflect.DeepEqual(gotCookies, wantCookies) {
			t.Errorf("%s response Set-Cookie = %#v, want %#v", label, gotCookies, wantCookies)
		}
		gotLinks := w.Result().Header.Values("Link")
		if !reflect.DeepEqual(gotLinks, wantLinks) {
			t.Errorf("%s response Link = %#v, want %#v", label, gotLinks, wantLinks)
		}
		_ = i
	}
}
