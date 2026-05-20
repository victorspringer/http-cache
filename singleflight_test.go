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

type slowAdapter struct {
	mu    sync.Mutex
	store map[uint64][]byte
}

func (a *slowAdapter) Get(key uint64) ([]byte, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	v, ok := a.store[key]
	return v, ok
}
func (a *slowAdapter) Set(key uint64, b []byte, _ time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.store[key] = b
}
func (a *slowAdapter) Release(key uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.store, key)
}

// With ClientWithSingleflight, a stampede of identical concurrent
// requests must coalesce into a single origin call. All requests must
// still receive a correct response.
func TestClientWithSingleflightCoalescesConcurrentMisses(t *testing.T) {
	adapter := &slowAdapter{store: map[uint64][]byte{}}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
		ClientWithSingleflight(),
	)
	if err != nil {
		t.Fatal(err)
	}

	var calls int64
	release := make(chan struct{})
	handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&calls, 1)
		<-release
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "origin response")
	}))

	const N = 50
	bodies := make([]string, N)
	statuses := make([]int, N)
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://x/sf", nil))
			bodies[i] = w.Body.String()
			statuses[i] = w.Code
		}()
	}
	// Give all goroutines a chance to enter sf.Do before releasing.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := atomic.LoadInt64(&calls); got != 1 {
		t.Fatalf("origin handler called %d times under singleflight, want 1", got)
	}
	for i, body := range bodies {
		if body != "origin response" {
			t.Errorf("request %d body = %q, want %q", i, body, "origin response")
		}
		if statuses[i] != http.StatusOK {
			t.Errorf("request %d status = %d, want 200", i, statuses[i])
		}
	}
}

// After the singleflight wave completes, the response must be in the
// cache so the next request (in a new singleflight batch) does not need
// to re-run the handler.
func TestClientWithSingleflightStoresFirstResponse(t *testing.T) {
	adapter := &slowAdapter{store: map[uint64][]byte{}}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
		ClientWithSingleflight(),
	)
	if err != nil {
		t.Fatal(err)
	}

	calls := 0
	handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprintf(w, "v=%d", calls)
	}))

	handler.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "http://x/sf-store", nil))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w,
		httptest.NewRequest(http.MethodGet, "http://x/sf-store", nil))
	if got, want := w.Body.String(), "v=1"; got != want {
		t.Fatalf("second request body = %q, want %q (cache miss after singleflight)", got, want)
	}
	if calls != 1 {
		t.Fatalf("handler called %d times, want 1 (second must hit cache)", calls)
	}
}

// Without ClientWithSingleflight, behavior is unchanged: every miss
// invokes the handler.
func TestClientWithoutSingleflightDoesNotCoalesce(t *testing.T) {
	adapter := &slowAdapter{store: map[uint64][]byte{}}
	client, err := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}

	var calls int64
	release := make(chan struct{})
	handler := client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&calls, 1)
		<-release
		w.Write([]byte("ok"))
	}))

	const N = 10
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			handler.ServeHTTP(httptest.NewRecorder(),
				httptest.NewRequest(http.MethodGet, "http://x/no-sf", nil))
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := atomic.LoadInt64(&calls); got != N {
		t.Errorf("origin handler called %d times without singleflight, want %d", got, N)
	}
}
