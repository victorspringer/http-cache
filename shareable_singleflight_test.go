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

// Followers reuse the leader's response unless the application marked it
// as uncacheable, since it may be specific to the leader's client. A
// response the cache refuses only because of its status is still shared,
// so a stampede against a failing origin collapses into one call.
func TestClientWithSingleflightSharingRules(t *testing.T) {
	for _, tt := range []struct {
		name         string
		status       int
		cacheControl string
		skipCache    bool
		shared       bool
	}{
		{name: "private", status: http.StatusOK, cacheControl: "private"},
		{name: "skip header", status: http.StatusOK, skipCache: true},
		{name: "filtered status", status: http.StatusServiceUnavailable, shared: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const url = "http://x/singleflight-sharing"
			const followers = 4
			key := generateKey(url)
			adapter := &slowAdapter{store: map[uint64][]byte{}}
			client, err := NewClient(
				ClientWithAdapter(adapter),
				ClientWithTTL(time.Minute),
				ClientWithSingleflight(),
				ClientWithRespectCacheControl(),
				ClientWithSkipCacheResponseHeader("X-Skip-Cache"),
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
				if atomic.AddInt64(&calls, 1) == 1 {
					close(started)
					<-release
				}
				if tt.cacheControl != "" {
					w.Header().Set("Cache-Control", tt.cacheControl)
				}
				if tt.skipCache {
					w.Header().Set("X-Skip-Cache", "1")
				}
				w.WriteHeader(tt.status)
				fmt.Fprint(w, "hello "+r.Header.Get("X-User"))
			}))
			type result struct {
				user     string
				response *httptest.ResponseRecorder
				panic    interface{}
			}
			results := make(chan result, followers+1)
			request := func(user string) {
				out := result{user: user, response: httptest.NewRecorder()}
				defer func() {
					out.panic = recover()
					results <- out
				}()
				r := httptest.NewRequest(http.MethodGet, url, nil)
				r.Header.Set("X-User", user)
				handler.ServeHTTP(out.response, r)
			}
			go request("leader")
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("leader did not start")
			}
			for i := 0; i < followers; i++ {
				go request(fmt.Sprintf("follower-%d", i))
			}
			waitForSingleflightDups(t, &client.sf, key, followers)
			unblock()
			for i := 0; i <= followers; i++ {
				select {
				case out := <-results:
					if out.panic != nil {
						t.Errorf("request for %q panicked: %v", out.user, out.panic)
						continue
					}
					want := "hello " + out.user
					if tt.shared {
						want = "hello leader"
					}
					if out.response.Code != tt.status || out.response.Body.String() != want {
						t.Errorf("response for %q = %d %q, want %d %q", out.user, out.response.Code, out.response.Body.String(), tt.status, want)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("request remained blocked after the leader finished")
				}
			}
			wantCalls := int64(1)
			if !tt.shared {
				wantCalls += followers
			}
			if got := atomic.LoadInt64(&calls); got != wantCalls {
				t.Errorf("origin calls = %d, want %d", got, wantCalls)
			}
			if _, ok := adapter.Get(key); ok {
				t.Fatal("uncacheable response was cached")
			}
		})
	}
}

func TestClientWithSingleflightDoesNotShareOversizedResponses(t *testing.T) {
	for _, background := range []bool{false, true} {
		t.Run(fmt.Sprintf("background=%v", background), func(t *testing.T) {
			const url = "http://x/oversized-singleflight"
			const followers = 4
			key := generateKey(url)
			adapter := &slowAdapter{store: map[uint64][]byte{}}
			client, err := NewClient(
				ClientWithAdapter(adapter),
				ClientWithTTL(time.Minute),
				ClientWithSingleflight(),
				ClientWithMaxBodySize(8),
				ClientWithStaleWhileRevalidate(time.Hour),
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
				if atomic.AddInt64(&calls, 1) == 1 {
					close(started)
					<-release
				}
				user := r.Header.Get("X-User")
				w.Header().Set("X-User", user)
				w.WriteHeader(http.StatusCreated)
				fmt.Fprint(w, "oversized response for "+user)
			}))
			type result struct {
				user     string
				response *httptest.ResponseRecorder
				panic    interface{}
			}
			results := make(chan result, followers+1)
			request := func(user string) {
				out := result{user: user, response: httptest.NewRecorder()}
				defer func() {
					out.panic = recover()
					results <- out
				}()
				r := httptest.NewRequest(http.MethodGet, url, nil)
				r.Header.Set("X-User", user)
				handler.ServeHTTP(out.response, r)
			}
			if background {
				expired := time.Now().Add(-time.Second)
				adapter.Set(key, Response{
					Value:      []byte("stale"),
					Expiration: expired,
				}.Bytes(), expired)
				r := httptest.NewRequest(http.MethodGet, url, nil)
				r.Header.Set("X-User", "leader")
				stale := httptest.NewRecorder()
				handler.ServeHTTP(stale, r)
				if got := stale.Body.String(); got != "stale" {
					t.Fatalf("stale body = %q, want stale", got)
				}
			} else {
				go request("leader")
			}
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("leader did not start")
			}
			if background {
				adapter.Release(key)
			}
			for i := 0; i < followers; i++ {
				go request(fmt.Sprintf("follower-%d", i))
			}
			waitForSingleflightDups(t, &client.sf, key, followers)
			unblock()
			responses := followers
			if !background {
				responses++
			}
			for i := 0; i < responses; i++ {
				select {
				case out := <-results:
					if out.panic != nil {
						t.Errorf("request for %q panicked: %v", out.user, out.panic)
						continue
					}
					if out.response.Code != http.StatusCreated || out.response.Header().Get("X-User") != out.user {
						t.Errorf("response for %q = %d %v", out.user, out.response.Code, out.response.Header())
					}
					if out.response.Body.String() != "oversized response for "+out.user {
						t.Errorf("response body for %q = %q", out.user, out.response.Body.String())
					}
				case <-time.After(2 * time.Second):
					t.Fatal("request remained blocked after oversized response")
				}
			}
			if got := atomic.LoadInt64(&calls); got != followers+1 {
				t.Errorf("origin calls = %d, want %d", got, followers+1)
			}
			if _, ok := adapter.Get(key); ok {
				t.Fatal("oversized response was cached")
			}
		})
	}
}
