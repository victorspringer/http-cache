/*
MIT License

Copyright (c) 2018 Victor Springer

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/

package cache

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Response is the cached response data structure.
type Response struct {
	// Value is the cached response value.
	Value []byte

	// Header is the cached response header.
	Header http.Header

	// Expiration is the cached response expiration date.
	Expiration time.Time

	// LastAccess is the last date a cached response was accessed.
	// Used by LRU and MRU algorithms.
	LastAccess time.Time

	// Frequency is the count of times a cached response is accessed.
	// Used for LFU and MFU algorithms.
	Frequency int
}

// Client data structure for HTTP cache middleware.
type Client struct {
	adapter                 Adapter
	ttl                     time.Duration
	refreshKey              string
	skipCacheResponseHeader string
	methods                 []string
	writeExpiresHeader      bool
}

// ClientOption is used to set Client settings.
type ClientOption func(c *Client) error

// Adapter interface for HTTP cache middleware client.
type Adapter interface {
	// Get retrieves the cached response by a given key. It also
	// returns true or false, whether it exists or not.
	Get(key uint64) ([]byte, bool)

	// Set caches a response for a given key until an expiration date.
	Set(key uint64, response []byte, expiration time.Time)

	// Release frees cache for a given key.
	Release(key uint64)
}

// Middleware is the HTTP cache middleware handler.
func (c *Client) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c.cacheableMethod(r.Method) {
			sortURLParams(r.URL)
			key := generateKey(r.URL.String())
			if r.Method == http.MethodPost && r.Body != nil {
				body, err := io.ReadAll(r.Body)
				defer r.Body.Close()
				if err != nil {
					next.ServeHTTP(w, r)
					return
				}
				reader := io.NopCloser(bytes.NewBuffer(body))
				key = generateKeyWithBody(r.URL.String(), body)
				r.Body = reader
			}

			params := r.URL.Query()
			if _, ok := params[c.refreshKey]; ok {
				delete(params, c.refreshKey)

				r.URL.RawQuery = params.Encode()
				key = generateKey(r.URL.String())

				c.adapter.Release(key)
			} else {
				b, ok := c.adapter.Get(key)
				response := BytesToResponse(b)
				if ok {
					if response.Expiration.After(time.Now()) {
						response.LastAccess = time.Now()
						response.Frequency++
						c.adapter.Set(key, response.Bytes(), response.Expiration)

						//w.WriteHeader(http.StatusNotModified)
						for k, v := range response.Header {
							w.Header().Set(k, strings.Join(v, ","))
						}
						if c.writeExpiresHeader {
							w.Header().Set("Expires", response.Expiration.UTC().Format(http.TimeFormat))
						}
						w.Write(response.Value)
						return
					}

					c.adapter.Release(key)
				}
			}

			rec := httptest.NewRecorder()
			next.ServeHTTP(rec, r)
			result := rec.Result()
			headers := w.Header()

			statusCode := result.StatusCode
			value := rec.Body.Bytes()

			skipCachingResponse := headers.Get(c.skipCacheResponseHeader) != ""

			if !skipCachingResponse {

				now := time.Now()
				expires := now.Add(c.ttl)
				if statusCode < 400 {
					response := Response{
						Value:      value,
						Header:     result.Header,
						Expiration: expires,
						LastAccess: now,
						Frequency:  1,
					}
					c.adapter.Set(key, response.Bytes(), response.Expiration)
				}
				if c.writeExpiresHeader {
					w.Header().Set("Expires", expires.UTC().Format(http.TimeFormat))
				}

			}

			for k, v := range result.Header {
				w.Header().Set(k, strings.Join(v, ","))
			}
			w.WriteHeader(statusCode)
			w.Write(value)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (c *Client) cacheableMethod(method string) bool {
	for _, m := range c.methods {
		if method == m {
			return true
		}
	}
	return false
}

// BytesToResponse converts bytes array into Response data structure.
func BytesToResponse(b []byte) Response {
	var r Response
	dec := gob.NewDecoder(bytes.NewReader(b))
	dec.Decode(&r)

	return r
}

// Bytes converts Response data structure into bytes array.
func (r Response) Bytes() []byte {
	var b bytes.Buffer
	enc := gob.NewEncoder(&b)
	enc.Encode(&r)

	return b.Bytes()
}

func sortURLParams(URL *url.URL) {
	params := URL.Query()
	for _, param := range params {
		sort.Slice(param, func(i, j int) bool {
			return param[i] < param[j]
		})
	}
	URL.RawQuery = params.Encode()
}

// KeyAsString can be used by adapters to convert the cache key from uint64 to string.
func KeyAsString(key uint64) string {
	return strconv.FormatUint(key, 36)
}

func generateKey(URL string) uint64 {
	hash := fnv.New64a()
	hash.Write([]byte(URL))

	return hash.Sum64()
}

func generateKeyWithBody(URL string, body []byte) uint64 {
	hash := fnv.New64a()
	body = append([]byte(URL), body...)
	hash.Write(body)

	return hash.Sum64()
}

// NewClient initializes the cache HTTP middleware client with the given
// options.
func NewClient(opts ...ClientOption) (*Client, error) {
	c := &Client{}

	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}

	if c.adapter == nil {
		return nil, errors.New("cache client adapter is not set")
	}
	if int64(c.ttl) < 1 {
		return nil, errors.New("cache client ttl is not set")
	}
	if c.methods == nil {
		c.methods = []string{http.MethodGet}
	}

	return c, nil
}

// ClientWithAdapter sets the adapter type for the HTTP cache
// middleware client.
func ClientWithAdapter(a Adapter) ClientOption {
	return func(c *Client) error {
		c.adapter = a
		return nil
	}
}

// ClientWithTTL sets how long each response is going to be cached.
func ClientWithTTL(ttl time.Duration) ClientOption {
	return func(c *Client) error {
		if int64(ttl) < 1 {
			return fmt.Errorf("cache client ttl %v is invalid", ttl)
		}

		c.ttl = ttl

		return nil
	}
}

// ClientWithRefreshKey sets the parameter key used to free a request
// cached response. Optional setting.
func ClientWithRefreshKey(refreshKey string) ClientOption {
	return func(c *Client) error {
		c.refreshKey = refreshKey
		return nil
	}
}

// ClientWithSkipCacheResponseHeader sets the name of the response header
// that will be used to ensure a response does not get cached.
// Optional setting.
func ClientWithSkipCacheResponseHeader(headerName string) ClientOption {
	return func(c *Client) error {
		c.skipCacheResponseHeader = headerName
		return nil
	}
}

// ClientWithMethods sets the acceptable HTTP methods to be cached.
// Optional setting. If not set, default is "GET".
func ClientWithMethods(methods []string) ClientOption {
	return func(c *Client) error {
		for _, method := range methods {
			if method != http.MethodGet && method != http.MethodPost {
				return fmt.Errorf("invalid method %s", method)
			}
		}
		c.methods = methods
		return nil
	}
}

// ClientWithExpiresHeader enables middleware to add an Expires header to responses.
// Optional setting. If not set, default is false.
func ClientWithExpiresHeader() ClientOption {
	return func(c *Client) error {
		c.writeExpiresHeader = true
		return nil
	}
}
