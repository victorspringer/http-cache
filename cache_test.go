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
	store map[uint64]Cache
}

func (a *adapterMock) Get(key uint64) (Cache, bool) {
	a.Lock()
	defer a.Unlock()
	if _, ok := a.store[key]; ok {
		return a.store[key], true
	}
	return Cache{}, false
}

func (a *adapterMock) Set(key uint64, cache Cache) {
	a.Lock()
	defer a.Unlock()
	a.store[key] = cache
}

func (a *adapterMock) Release(key uint64) {
	a.Lock()
	defer a.Unlock()
	delete(a.store, key)
}

func TestMiddleware(t *testing.T) {
	httpTestHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("new value"))
	})

	adapter := &adapterMock{
		store: map[uint64]Cache{
			14974843192121052621: Cache{
				Value:      []byte("value 1"),
				Expiration: time.Now().Add(1 * time.Minute),
			},
			14974839893586167988: Cache{
				Value:      []byte("value 2"),
				Expiration: time.Now().Add(1 * time.Minute),
			},
			14974840993097796199: Cache{
				Value:      []byte("value 3"),
				Expiration: time.Now().Add(-1 * time.Minute),
			},
		},
	}

	client, _ := NewClient(&Config{
		Adapter:    adapter,
		TTL:        1 * time.Minute,
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
			"http://foo.bar/test-3?zaz=baz&baz=zaz",
			"new value",
			200,
		},
		{
			"returns cached response",
			"http://foo.bar/test-3?baz=zaz&zaz=baz",
			"new value",
			302,
		},
		{
			"cache expired",
			"http://foo.bar/test-3",
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

func TestSortURLParams(t *testing.T) {
	u, _ := url.Parse("http://test.com?zaz=bar&foo=zaz&boo=foo&boo=baz")
	tests := []struct {
		name string
		URL  *url.URL
		want string
	}{
		{
			"returns url with ordered querystring params",
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
		want uint64
	}{
		{
			"get url checksum",
			"http://foo.bar/test-1",
			14974843192121052621,
		},
		{
			"get url 2 checksum",
			"http://foo.bar/test-2",
			14974839893586167988,
		},
		{
			"get url 3 checksum",
			"http://foo.bar/test-3",
			14974840993097796199,
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

func TestNewClient(t *testing.T) {
	adapter := &adapterMock{}

	tests := []struct {
		name    string
		cfg     *Config
		want    *Client
		wantErr bool
	}{
		{
			"returns new client",
			&Config{
				Adapter: adapter,
				TTL:     1 * time.Millisecond,
			},
			&Client{
				adapter:    adapter,
				ttl:        1 * time.Millisecond,
				releaseKey: "",
			},
			false,
		},
		{
			"returns new client with release key",
			&Config{
				Adapter:    adapter,
				TTL:        1 * time.Millisecond,
				ReleaseKey: "rk",
			},
			&Client{
				adapter:    adapter,
				ttl:        1 * time.Millisecond,
				releaseKey: "rk",
			},
			false,
		},
		{
			"returns error",
			&Config{
				Adapter: adapter,
			},
			nil,
			true,
		},
		{
			"returns error",
			&Config{
				TTL:        1 * time.Millisecond,
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
