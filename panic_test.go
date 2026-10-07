package cache

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientWithSingleflightLeaderPanicReleasesFollowers(t *testing.T) {
	const url = "http://x/leader-panic"
	const followers = 4
	key := generateKey(url)
	adapter := &slowAdapter{store: map[uint64][]byte{}}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(time.Minute),
		ClientWithSingleflight(),
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
		if name == "leader" {
			close(started)
			<-release
			panic(http.ErrAbortHandler)
		}
		w.Header().Set("X-Request", name)
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, "response for "+name)
	}))
	type result struct {
		name     string
		response *httptest.ResponseRecorder
		panic    interface{}
	}
	results := make(chan result, followers+1)
	request := func(name string) {
		out := result{name: name, response: httptest.NewRecorder()}
		defer func() {
			out.panic = recover()
			results <- out
		}()
		r := httptest.NewRequest(http.MethodGet, url, nil)
		r.Header.Set("X-Request", name)
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
			if out.name == "leader" {
				if out.panic != http.ErrAbortHandler {
					t.Errorf("leader panic = %v, want ErrAbortHandler", out.panic)
				}
				continue
			}
			if out.panic != nil {
				t.Errorf("follower panicked: %v", out.panic)
				continue
			}
			if out.response.Code != http.StatusCreated || out.response.Body.String() != "response for "+out.name || out.response.Header().Get("X-Request") != out.name {
				t.Errorf("follower %q response = %d %q %v", out.name, out.response.Code, out.response.Body.String(), out.response.Header())
			}
		case <-time.After(2 * time.Second):
			t.Fatal("request remained blocked after leader panic")
		}
	}
	if _, ok := adapter.Get(key); !ok {
		t.Fatal("fallback response was not cached")
	}
	adapter.Release(key)
	go request("later")
	select {
	case out := <-results:
		if out.panic != nil || out.response.Code != http.StatusCreated || out.response.Body.String() != "response for later" {
			t.Fatalf("later request: panic=%v status=%d body=%q", out.panic, out.response.Code, out.response.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("later miss remained blocked after leader panic")
	}
	if got := atomic.LoadInt64(&calls); got != followers+2 {
		t.Errorf("origin calls = %d, want %d", got, followers+2)
	}
}

func TestClientWithStaleWhileRevalidateRecoversHandlerPanic(t *testing.T) {
	for _, tt := range []struct {
		name       string
		panicValue interface{}
		logged     bool
	}{
		{name: "panic", panicValue: "refresh failed", logged: true},
		{name: "abort handler", panicValue: http.ErrAbortHandler},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const url = "http://x/refresh-panic"
			adapter := &slowAdapter{store: map[uint64][]byte{}}
			client, err := NewClient(
				ClientWithAdapter(adapter),
				ClientWithTTL(time.Minute),
				ClientWithSingleflight(),
				ClientWithStaleWhileRevalidate(time.Hour),
			)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodGet, url, nil)
			key, fingerprint, err := client.key(r)
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			previousOutput := log.Writer()
			log.SetOutput(&output)
			t.Cleanup(func() { log.SetOutput(previousOutput) })
			started := make(chan struct{})
			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			var calls int64
			origin := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if atomic.AddInt64(&calls, 1) == 1 {
					close(started)
					<-release
					panic(tt.panicValue)
				}
				w.WriteHeader(http.StatusCreated)
				fmt.Fprint(w, "fresh")
			})
			refreshed := make(chan struct{})
			go func() {
				defer close(refreshed)
				client.refresh(r, origin, key, fingerprint)
			}()
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("background handler did not start")
			}
			unblock()
			select {
			case <-refreshed:
			case <-time.After(2 * time.Second):
				t.Fatal("background refresh remained blocked after panic")
			}
			logged := output.String()
			if tt.logged {
				if !strings.Contains(logged, "http-cache: panic in background refresh: refresh failed") || !strings.Contains(logged, "runtime/debug.Stack") || !strings.Contains(logged, "panic_test.go") {
					t.Errorf("panic log missing message or stack trace: %q", logged)
				}
			} else if logged != "" {
				t.Errorf("ErrAbortHandler was logged: %q", logged)
			}
			if _, ok := adapter.Get(key); ok {
				t.Fatal("panicking response was cached")
			}
			response := httptest.NewRecorder()
			done := make(chan interface{}, 1)
			go func() {
				defer func() { done <- recover() }()
				client.Middleware(origin).ServeHTTP(response, httptest.NewRequest(http.MethodGet, url, nil))
			}()
			select {
			case failure := <-done:
				if failure != nil {
					t.Fatalf("later request panicked: %v", failure)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("later miss remained blocked after background panic")
			}
			if response.Code != http.StatusCreated || response.Body.String() != "fresh" {
				t.Errorf("later response = %d %q", response.Code, response.Body.String())
			}
			if got := atomic.LoadInt64(&calls); got != 2 {
				t.Errorf("origin calls = %d, want 2", got)
			}
		})
	}
}
