package cache

import (
	"bytes"
	"errors"
	"fmt"
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
	store map[uint64][]byte
}

type errReader int

func (a *adapterMock) Get(key uint64) ([]byte, bool) {
	a.Lock()
	defer a.Unlock()
	if _, ok := a.store[key]; ok {
		return a.store[key], true
	}
	return nil, false
}

func (a *adapterMock) Set(key uint64, response []byte, expiration time.Time) {
	a.Lock()
	defer a.Unlock()
	a.store[key] = response
}

func (a *adapterMock) Release(key uint64) {
	a.Lock()
	defer a.Unlock()
	delete(a.store, key)
}

func (errReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("readAll error")
}

func TestMiddleware(t *testing.T) {
	counter := 0
	httpTestHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(fmt.Sprintf("new value %v", counter)))
	})

	adapter := &adapterMock{
		store: map[uint64][]byte{
			14974843192121052621: Response{
				Value:      []byte("value 1"),
				Expiration: time.Now().Add(1 * time.Minute),
			}.Bytes(),
			14974839893586167988: Response{
				Value:      []byte("value 2"),
				Expiration: time.Now().Add(1 * time.Minute),
			}.Bytes(),
			14974840993097796199: Response{
				Value:      []byte("value 3"),
				Expiration: time.Now().Add(-1 * time.Minute),
			}.Bytes(),
			10956846073361780255: Response{
				Value:      []byte("value 4"),
				Expiration: time.Now().Add(-1 * time.Minute),
			}.Bytes(),
		},
	}

	client, _ := NewClient(
		ClientWithAdapter(adapter),
		ClientWithTTL(1*time.Minute),
		ClientWithRefreshKey("rk"),
		ClientWithMethods([]string{http.MethodGet, http.MethodPost}),
	)

	handler := client.Middleware(httpTestHandler)

	tests := []struct {
		name     string
		url      string
		method   string
		body     []byte
		wantBody string
		wantCode int
	}{
		{
			"returns cached response",
			"http://foo.bar/test-1",
			"GET",
			nil,
			"value 1",
			200,
		},
		{
			"returns new response",
			"http://foo.bar/test-2",
			"PUT",
			nil,
			"new value 2",
			200,
		},
		{
			"returns cached response",
			"http://foo.bar/test-2",
			"GET",
			nil,
			"value 2",
			200,
		},
		{
			"returns new response",
			"http://foo.bar/test-3?zaz=baz&baz=zaz",
			"GET",
			nil,
			"new value 4",
			200,
		},
		{
			"returns cached response",
			"http://foo.bar/test-3?baz=zaz&zaz=baz",
			"GET",
			nil,
			"new value 4",
			200,
		},
		{
			"cache expired",
			"http://foo.bar/test-3",
			"GET",
			nil,
			"new value 6",
			200,
		},
		{
			"releases cached response and returns new response",
			"http://foo.bar/test-2?rk=true",
			"GET",
			nil,
			"new value 7",
			200,
		},
		{
			"returns new cached response",
			"http://foo.bar/test-2",
			"GET",
			nil,
			"new value 7",
			200,
		},
		{
			"returns new cached response",
			"http://foo.bar/test-2",
			"POST",
			[]byte(`{"foo": "bar"}`),
			"new value 9",
			200,
		},
		{
			"returns new cached response",
			"http://foo.bar/test-2",
			"POST",
			[]byte(`{"foo": "bar"}`),
			"new value 9",
			200,
		},
		{
			"ignores request body",
			"http://foo.bar/test-2",
			"GET",
			[]byte(`{"foo": "bar"}`),
			"new value 7",
			200,
		},
		{
			"returns new response",
			"http://foo.bar/test-2",
			"POST",
			[]byte(`{"foo": "bar"}`),
			"new value 12",
			200,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counter++
			var r *http.Request
			var err error

			if counter != 12 {
				reader := bytes.NewReader(tt.body)
				r, err = http.NewRequest(tt.method, tt.url, reader)
				if err != nil {
					t.Error(err)
					return
				}
			} else {
				r, err = http.NewRequest(tt.method, tt.url, errReader(0))
				if err != nil {
					t.Error(err)
					return
				}
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

func TestBytesToResponse(t *testing.T) {
	r := Response{
		Value:      []byte("value 1"),
		Expiration: time.Time{},
		Frequency:  0,
		LastAccess: time.Time{},
	}

	tests := []struct {
		name      string
		b         []byte
		wantValue string
	}{

		{
			"convert bytes array to response",
			r.Bytes(),
			"value 1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BytesToResponse(tt.b)
			if string(got.Value) != tt.wantValue {
				t.Errorf("BytesToResponse() Value = %v, want %v", got, tt.wantValue)
				return
			}
		})
	}
}

func TestResponseToBytes(t *testing.T) {
	r := Response{
		Value:      nil,
		Expiration: time.Time{},
		Frequency:  0,
		LastAccess: time.Time{},
	}

	tests := []struct {
		name     string
		response Response
	}{
		{
			"convert response to bytes array",
			r,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := tt.response.Bytes()
			if b == nil || len(b) == 0 {
				t.Error("Bytes() failed to convert")
				return
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

func TestGenerateKeyString(t *testing.T) {
	urls := []string{
		"http://localhost:8080/category",
		"http://localhost:8080/category/morisco",
		"http://localhost:8080/category/mourisquinho",
	}

	keys := make(map[string]string, len(urls))
	for _, u := range urls {
		rawKey := generateKey(u)
		key := KeyAsString(rawKey)

		if otherURL, found := keys[key]; found {
			t.Fatalf("URLs %s and %s share the same key %s", u, otherURL, key)
		}
		keys[key] = u
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

func TestGenerateKeyWithBody(t *testing.T) {
	tests := []struct {
		name string
		URL  string
		body []byte
		want uint64
	}{
		{
			"get POST checksum",
			"http://foo.bar/test-1",
			[]byte(`{"foo": "bar"}`),
			16224051135567554746,
		},
		{
			"get POST 2 checksum",
			"http://foo.bar/test-1",
			[]byte(`{"bar": "foo"}`),
			3604153880186288164,
		},
		{
			"get POST 3 checksum",
			"http://foo.bar/test-2",
			[]byte(`{"foo": "bar"}`),
			10956846073361780255,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := generateKeyWithBody(tt.URL, tt.body); got != tt.want {
				t.Errorf("generateKeyWithBody() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewClient(t *testing.T) {
	adapter := &adapterMock{}

	tests := []struct {
		name    string
		opts    []ClientOption
		want    *Client
		wantErr bool
	}{
		{
			"returns new client",
			[]ClientOption{
				ClientWithAdapter(adapter),
				ClientWithTTL(1 * time.Millisecond),
				ClientWithMethods([]string{http.MethodGet, http.MethodPost}),
			},
			&Client{
				adapter:    adapter,
				ttl:        1 * time.Millisecond,
				refreshKey: "",
				methods:    []string{http.MethodGet, http.MethodPost},
			},
			false,
		},
		{
			"returns new client with refresh key",
			[]ClientOption{
				ClientWithAdapter(adapter),
				ClientWithTTL(1 * time.Millisecond),
				ClientWithRefreshKey("rk"),
			},
			&Client{
				adapter:    adapter,
				ttl:        1 * time.Millisecond,
				refreshKey: "rk",
				methods:    []string{http.MethodGet},
			},
			false,
		},
		{
			"returns error",
			[]ClientOption{
				ClientWithAdapter(adapter),
			},
			nil,
			true,
		},
		{
			"returns error",
			[]ClientOption{
				ClientWithTTL(1 * time.Millisecond),
				ClientWithRefreshKey("rk"),
			},
			nil,
			true,
		},
		{
			"returns error",
			[]ClientOption{
				ClientWithAdapter(adapter),
				ClientWithTTL(0),
				ClientWithRefreshKey("rk"),
			},
			nil,
			true,
		},
		{
			"returns error",
			[]ClientOption{
				ClientWithAdapter(adapter),
				ClientWithTTL(1 * time.Millisecond),
				ClientWithMethods([]string{http.MethodGet, http.MethodPut}),
			},
			nil,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewClient(tt.opts...)
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
