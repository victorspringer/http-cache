package cache

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientWithSingleflightBypassesExplicitRefresh(t *testing.T) {
	const url = "http://x/bypass-refresh"
	for _, tt := range []struct {
		name         string
		url          string
		cacheControl string
	}{
		{name: "no-cache", url: url, cacheControl: "no-cache"},
		{name: "refresh key", url: url + "?refresh=1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			key := generateKey(url)
			adapter := &slowAdapter{store: map[uint64][]byte{
				key: Response{
					Value:      []byte("cached"),
					Expiration: time.Now().Add(-time.Second),
				}.Bytes(),
			}}
			refreshed := make(chan struct{})
			client, err := NewClient(
				ClientWithAdapter(adapter),
				ClientWithTTL(time.Minute),
				ClientWithSingleflight(),
				ClientWithStaleWhileRevalidate(time.Hour),
				ClientWithRespectCacheControl(),
				ClientWithRefreshKey("refresh"),
				ClientWithObserver(func(event CacheEvent) {
					if event.Type == CacheEventStore && event.Request.Header.Get("X-Request") == "background" {
						close(refreshed)
					}
				}),
			)
			if err != nil {
				t.Fatal(err)
			}
			started := make(chan struct{})
			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			var calls int64
			handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt64(&calls, 1)
				name := r.Header.Get("X-Request")
				if name == "background" {
					close(started)
					<-release
				}
				w.Header().Set("X-Origin", name)
				w.WriteHeader(http.StatusCreated)
				fmt.Fprint(w, "origin response for "+name)
			}))
			background := httptest.NewRequest(http.MethodGet, url, nil)
			background.Header.Set("X-Request", "background")
			stale := httptest.NewRecorder()
			handler.ServeHTTP(stale, background)
			if got := stale.Body.String(); got != "cached" {
				t.Fatalf("stale body = %q, want cached", got)
			}
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("background refresh did not start")
			}
			request := httptest.NewRequest(http.MethodGet, tt.url, nil)
			request.Header.Set("X-Request", "foreground")
			if tt.cacheControl != "" {
				request.Header.Set("Cache-Control", tt.cacheControl)
			}
			response := httptest.NewRecorder()
			done := make(chan interface{}, 1)
			go func() {
				defer func() { done <- recover() }()
				handler.ServeHTTP(response, request)
			}()
			select {
			case failure := <-done:
				if failure != nil {
					t.Fatalf("foreground request panicked: %v", failure)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("foreground request waited for the background refresh")
			}
			if response.Code != http.StatusCreated || response.Body.String() != "origin response for foreground" || response.Header().Get("X-Origin") != "foreground" {
				t.Errorf("foreground response = %d %q %v", response.Code, response.Body.String(), response.Header())
			}
			if got := atomic.LoadInt64(&calls); got != 2 {
				t.Errorf("origin calls = %d, want 2", got)
			}
			stored, ok := adapter.Get(key)
			if !ok || string(BytesToResponse(stored).Value) != "origin response for foreground" {
				t.Fatal("foreground response was not cached")
			}
			unblock()
			select {
			case <-refreshed:
			case <-time.After(2 * time.Second):
				t.Fatal("background refresh did not finish")
			}
		})
	}
}
