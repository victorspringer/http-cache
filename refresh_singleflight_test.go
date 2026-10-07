package cache

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientWithSingleflightJoinsBackgroundRefresh(t *testing.T) {
	for _, tt := range []struct {
		name         string
		status       int
		cacheControl string
		write        bool
		stored       bool
	}{
		{name: "cacheable", status: http.StatusCreated, write: true, stored: true},
		{name: "filtered status", status: http.StatusServiceUnavailable, write: true},
		{name: "no store", status: http.StatusOK, cacheControl: "no-store", write: true},
		{name: "private", status: http.StatusOK, cacheControl: "private", write: true},
		{name: "no write", status: http.StatusOK},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const url = "http://x/shared-refresh"
			const followers = 8
			key := generateKey(url)
			adapter := &slowAdapter{store: map[uint64][]byte{
				key: Response{
					Value:      []byte("stale"),
					Expiration: time.Now().Add(-time.Second),
				}.Bytes(),
			}}
			misses := make(chan struct{}, followers)
			client, err := NewClient(
				ClientWithAdapter(adapter),
				ClientWithTTL(time.Minute),
				ClientWithStaleWhileRevalidate(time.Hour),
				ClientWithSingleflight(),
				ClientWithRespectCacheControl(),
				ClientWithObserver(func(event CacheEvent) {
					if event.Type == CacheEventMiss {
						misses <- struct{}{}
					}
				}),
			)
			if err != nil {
				t.Fatal(err)
			}

			started := make(chan struct{}, followers+1)
			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			var calls int64
			handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt64(&calls, 1)
				started <- struct{}{}
				<-release
				if tt.write {
					w.Header()["X-Values"] = []string{"one", "two"}
					if tt.cacheControl != "" {
						w.Header().Set("Cache-Control", tt.cacheControl)
					}
					w.WriteHeader(tt.status)
					fmt.Fprint(w, "fresh")
				}
			}))

			stale := httptest.NewRecorder()
			handler.ServeHTTP(stale, httptest.NewRequest(http.MethodGet, url, nil))
			if got := stale.Body.String(); got != "stale" {
				t.Fatalf("stale body = %q", got)
			}
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("background refresh did not start")
			}
			adapter.Release(key)

			type result struct {
				response *httptest.ResponseRecorder
				panic    interface{}
			}
			results := make(chan result, followers)
			for i := 0; i < followers; i++ {
				go func() {
					out := result{response: httptest.NewRecorder()}
					defer func() {
						out.panic = recover()
						results <- out
					}()
					handler.ServeHTTP(out.response, httptest.NewRequest(http.MethodGet, url, nil))
				}()
			}
			for i := 0; i < followers; i++ {
				select {
				case <-misses:
				case <-time.After(2 * time.Second):
					t.Fatal("foreground request did not reach the miss path")
				}
			}
			waitForSingleflightDups(t, &client.sf, key, followers)
			select {
			case <-results:
				t.Fatal("foreground request returned before the refresh")
			default:
			}
			unblock()

			for i := 0; i < followers; i++ {
				select {
				case out := <-results:
					if out.panic != nil {
						t.Errorf("foreground request panicked: %v", out.panic)
						continue
					}
					if out.response.Code != tt.status {
						t.Errorf("status = %d, want %d", out.response.Code, tt.status)
					}
					body := ""
					if tt.write {
						body = "fresh"
						if got := out.response.Header().Values("X-Values"); !reflect.DeepEqual(got, []string{"one", "two"}) {
							t.Errorf("headers = %v", got)
						}
						if got := out.response.Header().Get("Cache-Control"); got != tt.cacheControl {
							t.Errorf("Cache-Control = %q, want %q", got, tt.cacheControl)
						}
					}
					if got := out.response.Body.String(); got != body {
						t.Errorf("body = %q, want %q", got, body)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("foreground request remained blocked")
				}
			}
			if got := atomic.LoadInt64(&calls); got != 1 {
				t.Errorf("origin calls = %d, want 1", got)
			}
			if _, stored := adapter.Get(key); stored != tt.stored {
				t.Errorf("response stored = %v, want %v", stored, tt.stored)
			}
		})
	}
}

type refreshLookupAdapter struct {
	slowAdapter
	blockNextGet bool
	started      chan struct{}
	release      chan struct{}
}

func (a *refreshLookupAdapter) Get(key uint64) ([]byte, bool) {
	a.mu.Lock()
	block := a.blockNextGet
	a.blockNextGet = false
	a.mu.Unlock()
	if block {
		close(a.started)
		<-a.release
	}
	return a.slowAdapter.Get(key)
}

func TestClientWithSingleflightJoinsRefreshCacheLookup(t *testing.T) {
	for _, matching := range []bool{true, false} {
		t.Run(fmt.Sprintf("matching=%v", matching), func(t *testing.T) {
			const url = "http://x/refreshed-lookup"
			adapter := &refreshLookupAdapter{
				slowAdapter:  slowAdapter{store: map[uint64][]byte{}},
				blockNextGet: true,
				started:      make(chan struct{}),
				release:      make(chan struct{}),
			}
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(adapter.release) }) }
			t.Cleanup(unblock)
			miss := make(chan struct{}, 1)
			client, err := NewClient(
				ClientWithAdapter(adapter),
				ClientWithTTL(time.Minute),
				ClientWithSingleflight(),
				ClientWithMaxBodySize(1),
				ClientWithExpiresHeader(),
				ClientWithObserver(func(event CacheEvent) {
					if event.Type == CacheEventMiss {
						miss <- struct{}{}
					}
				}),
			)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodGet, url, nil)
			key, fingerprint, err := client.key(request)
			if err != nil {
				t.Fatal(err)
			}
			storedFingerprint := fingerprint
			if !matching {
				storedFingerprint = []byte("different request")
			}
			expires := time.Now().Add(time.Minute).Truncate(time.Second)
			var calls int64
			origin := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt64(&calls, 1)
				fmt.Fprint(w, "x")
			})
			client.scheduleRefresh(request, origin, key, fingerprint)
			select {
			case <-adapter.started:
			case <-time.After(2 * time.Second):
				t.Fatal("refresh lookup did not start")
			}
			response := httptest.NewRecorder()
			done := make(chan interface{}, 1)
			go func() {
				defer func() { done <- recover() }()
				client.Middleware(origin).ServeHTTP(response, request)
			}()
			select {
			case <-miss:
			case <-time.After(2 * time.Second):
				t.Fatal("foreground request did not reach the miss path")
			}
			waitForSingleflightDups(t, &client.sf, key, 1)
			select {
			case <-done:
				t.Fatal("foreground request returned before the refresh lookup")
			default:
			}
			adapter.Set(key, Response{
				Value: []byte("cached response"),
				Header: cacheHeader(http.Header{
					"X-Values": []string{"one", "two"},
				}, http.StatusCreated),
				Expiration:   expires,
				CanonicalKey: storedFingerprint,
			}.Bytes(), expires)
			unblock()
			select {
			case failure := <-done:
				if failure != nil {
					t.Fatalf("foreground request panicked: %v", failure)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("foreground request remained blocked")
			}
			if !matching {
				if response.Body.String() != "x" || response.Code != http.StatusOK || atomic.LoadInt64(&calls) != 1 {
					t.Fatalf("mismatched cache entry was served: status=%d body=%q calls=%d", response.Code, response.Body.String(), calls)
				}
				return
			}
			if response.Code != http.StatusCreated || response.Body.String() != "cached response" {
				t.Errorf("cached response = %d %q", response.Code, response.Body.String())
			}
			if got := response.Header().Values("X-Values"); !reflect.DeepEqual(got, []string{"one", "two"}) {
				t.Errorf("cached headers = %v", got)
			}
			if got := response.Header().Get("Expires"); got != expires.UTC().Format(http.TimeFormat) {
				t.Errorf("Expires = %q", got)
			}
			if got := response.Header().Get(cacheStatusCodeHeader); got != "" {
				t.Errorf("internal status header = %q", got)
			}
			if got := atomic.LoadInt64(&calls); got != 0 {
				t.Errorf("origin calls = %d, want 0", got)
			}
		})
	}
}
