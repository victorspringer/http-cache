package cache

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sync"
	"testing"
	"time"
)

type adapterMock struct {
	sync.Mutex
	store map[string]Cache
}

func (a *adapterMock) Get(key string) (Cache, bool) {
	a.Lock()
	defer a.Unlock()
	if _, ok := a.store[key]; ok {
		return a.store[key], true
	}
	return Cache{}, false
}

func (a *adapterMock) Set(key string, cache Cache) {
	a.Lock()
	defer a.Unlock()
	a.store[key] = cache
}

func (a *adapterMock) Release(key string) {
	a.Lock()
	defer a.Unlock()
	delete(a.store, key)
}

func (a *adapterMock) Length() int {
	a.Lock()
	defer a.Unlock()
	return len(a.store)
}

func (a *adapterMock) Evict(algorithm Algorithm) {
	a.Lock()
	defer a.Unlock()

	lruKey := ""
	lruLastAccess := time.Now().Add(2 * time.Minute)

	for key, value := range a.store {
		if value.LastAccess.Before(lruLastAccess) {
			lruKey = key
			lruLastAccess = value.LastAccess
		}
	}

	go a.Release(lruKey)
}

func TestMiddleware(t *testing.T) {
	httpTestHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("new value"))
	})

	adapter := &adapterMock{
		store: map[string]Cache{
			"1e13f750b4d13e03a775f9d09032f87b": Cache{
				Value:      []byte("value 1"),
				Expiration: time.Now().Add(1 * time.Minute),
			},
			"48c169c22f6ae6351993050852982723": Cache{
				Value:      []byte("value 2"),
				Expiration: time.Now().Add(1 * time.Minute),
			},
			"e7bc18936aeeee6fa96bd9410a3970f4": Cache{
				Value:      []byte("value 3"),
				Expiration: time.Now().Add(-1 * time.Minute),
			},
		},
	}

	client, _ := NewClient(Config{
		Adapter:    adapter,
		TTL:        1 * time.Minute,
		Capacity:   4,
		Algorithm:  LRU,
		ReleaseKey: "rk",
	})

	handler := client.Middleware(httpTestHandler)

	tests := []struct {
		name     string
		url      string
		wantBody string
		wantCode int
	}{
		{
			"returns cached response",
			"http://foo.bar/test-1",
			"value 1",
			302,
		},
		{
			"returns cached response",
			"http://foo.bar/test-2",
			"value 2",
			302,
		},
		{
			"no cached response returns ok status",
			"http://foo.bar/test-3",
			"new value",
			200,
		},
		{
			"cache expired",
			"http://foo.bar/test-4",
			"new value",
			200,
		},
		{
			"releases cached response and returns ok status",
			"http://foo.bar/test-2?rk=true",
			"new value",
			200,
		},
		{
			"returns new cached response",
			"http://foo.bar/test-2",
			"new value",
			302,
		},
		{
			"returns new cached response",
			"http://foo.bar/test-5",
			"new value",
			200,
		},
		{
			"first cached response was evicted",
			"http://foo.bar/test-1",
			"new value",
			200,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := http.NewRequest("GET", tt.url, nil)
			if err != nil {
				t.Error(err)
				return
			}

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)

			if !reflect.DeepEqual(w.Code, tt.wantCode) {
				t.Errorf("*Client.Middleware() = %v, want %v", w.Code, tt.wantCode)
				return
			}
			if !reflect.DeepEqual(w.Body.String(), tt.wantBody) {
				t.Errorf("*Client.Middleware() = %v, want %v", w.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestNewClient(t *testing.T) {
	adapter := &adapterMock{}

	tests := []struct {
		name    string
		cfg     Config
		want    *Client
		wantErr bool
	}{
		{
			"return new client",
			Config{
				Adapter:   adapter,
				TTL:       1 * time.Millisecond,
				Algorithm: LRU,
				Capacity:  3,
			},
			&Client{
				adapter:    adapter,
				ttl:        1 * time.Millisecond,
				algorithm:  LRU,
				capacity:   3,
				releaseKey: "",
			},
			false,
		},
		{
			"return new client with release key",
			Config{
				Adapter:    adapter,
				TTL:        1 * time.Millisecond,
				Algorithm:  LRU,
				Capacity:   3,
				ReleaseKey: "rk",
			},
			&Client{
				adapter:    adapter,
				ttl:        1 * time.Millisecond,
				algorithm:  LRU,
				capacity:   3,
				releaseKey: "rk",
			},
			false,
		},
		{
			"return error",
			Config{
				Adapter: adapter,
			},
			nil,
			true,
		},
		{
			"return error",
			Config{
				TTL:        1 * time.Millisecond,
				ReleaseKey: "rk",
			},
			nil,
			true,
		},
		{
			"return error",
			Config{
				Adapter:    adapter,
				TTL:        1 * time.Millisecond,
				Algorithm:  LRU,
				ReleaseKey: "rk",
			},
			nil,
			true,
		},
		{
			"return error",
			Config{
				Adapter:    adapter,
				TTL:        1 * time.Millisecond,
				Capacity:   3,
				ReleaseKey: "rk",
			},
			nil,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewClient(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewClient() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSortURLParams(t *testing.T) {
	u, _ := url.Parse("http://test.com?zaz=bar&foo=zaz&boo=foo&boo=baz")
	tests := []struct {
		name string
		URL  *url.URL
		want string
	}{
		{
			"return url with ordered querystring params",
			u,
			"http://test.com?boo=baz&boo=foo&foo=zaz&zaz=bar",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sortURLParams(tt.URL)
			got := tt.URL.String()
			if got != tt.want {
				t.Errorf("sortURLParams() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGenerateKey(t *testing.T) {
	tests := []struct {
		name string
		URL  string
		want string
	}{
		{
			"get url checksum",
			"http://foo.bar/test-1",
			"1e13f750b4d13e03a775f9d09032f87b",
		},
		{
			"get url 2 checksum",
			"http://foo.bar/test-2",
			"48c169c22f6ae6351993050852982723",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := generateKey(tt.URL); got != tt.want {
				t.Errorf("generateKey() = %v, want %v", got, tt.want)
			}
		})
	}
}
