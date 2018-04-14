/*
	Copyright 2018 Victor Springer

	The MIT License

	Permission is hereby granted, free of charge, to any person obtaining
	a copy of this software and associated documentation files (the
	"Software"), to deal in the Software without restriction, including
	without limitation the rights to use, copy, modify, merge, publish,
	distribute, sublicense, and/or sell copies of the Software, and to
	permit persons to whom the Software is furnished to do so, subject to
	the following conditions:

	The above copyright notice and this permission notice shall be
	included in all copies or substantial portions of the Software.

	THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
	EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
	MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
	NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE
	LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION
	OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION
	WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
*/

package cache

import (
	"crypto/md5"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"time"
)

// Algorithm is the string type for caching algorithms labels.
type Algorithm string

const (
	// LRU is the constant for Least Recently Used.
	LRU Algorithm = "LRU"

	// MRU is the constant for Most Recently Used.
	MRU Algorithm = "MRU"

	// LFU is the constant for Least Frequently Used.
	LFU Algorithm = "LFU"

	// MFU is the constant for Most Frequently Used.
	MFU Algorithm = "MFU"
)

// Cache is the cache data structure.
type Cache struct {
	// Value is the cached response value.
	Value []byte

	// Expiration is the cached response expiration date.
	Expiration time.Time

	// LastAccess is the last date a cached response was accessed.
	// Used by LRU and MRU algorithms.
	LastAccess time.Time

	// Frequency is the count of times a cached response is accessed.
	// Used for LFU and MFU algorithms.
	Frequency int
}

// Config contains the Client configuration parameters.
// ReleaseKey is optional setting.
type Config struct {
	// Adapter type for the HTTP cache middleware client.
	Adapter Adapter

	// TTL is how long a response is going to be cached.
	TTL time.Duration

	// Capacity is the maximum number of cached responses.
	Capacity int

	// Algorithm is the approach used to select a cached
	// response to be evicted when the capacity is reached.
	Algorithm Algorithm

	// ReleaseKey is the parameter key used to free a request cached
	// response. Optional setting.
	ReleaseKey string
}

// Client data structure for HTTP cache middleware.
type Client struct {
	adapter    Adapter
	ttl        time.Duration
	capacity   int
	algorithm  Algorithm
	releaseKey string
}

// Adapter interface for HTTP cache middleware client.
type Adapter interface {
	// Get retrieves the cached response by a given key. It also
	// returns true or false, whether it exists or not.
	Get(key string) (Cache, bool)

	// Set caches a response for a given key.
	Set(key string, cache Cache)

	// Release frees cache for a given key.
	Release(key string)

	// Length retrieves the total number of cached responses.
	Length() int

	// Evitct selects a cached response to be released based on a
	// given algorithm.
	Evict(algorithm Algorithm)
}

// Middleware is the HTTP cache middleware handler.
func (c *Client) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sortURLParams(r.URL)
		key := generateKey(r.URL.String())

		params := r.URL.Query()
		if _, ok := params[c.releaseKey]; ok {
			delete(params, c.releaseKey)

			r.URL.RawQuery = params.Encode()
			key = generateKey(r.URL.String())

			c.adapter.Release(key)
		} else {
			cache, ok := c.adapter.Get(key)
			if ok {
				if cache.Expiration.After(time.Now()) {
					cache.LastAccess = time.Now()
					cache.Frequency++
					c.adapter.Set(key, cache)

					w.WriteHeader(http.StatusFound)
					w.Write(cache.Value)
					return
				}

				c.adapter.Release(key)
			}
		}

		rec := httptest.NewRecorder()
		next.ServeHTTP(rec, r)

		statusCode := rec.Result().StatusCode
		if statusCode < 400 {
			if c.adapter.Length() == c.capacity {
				c.adapter.Evict(c.algorithm)
			}

			now := time.Now()
			value := rec.Body.Bytes()

			cache := Cache{
				Value:      value,
				Expiration: now.Add(c.ttl),
				LastAccess: now,
				Frequency:  1,
			}
			c.adapter.Set(key, cache)

			w.WriteHeader(statusCode)
			w.Write(value)
		}
	})
}

// NewClient initializes the cache HTTP middleware client with a given
// configuration.
func NewClient(cfg Config) (*Client, error) {
	if cfg.Adapter == nil {
		return nil, errors.New("cache client requires an adapter")
	}

	if int64(cfg.TTL) < 1 {
		return nil, errors.New("cache client requires a valid ttl")
	}

	if cfg.Capacity <= 1 {
		return nil, errors.New("cache client requires a capacity greater than one")
	}

	if cfg.Algorithm == "" {
		return nil, errors.New("cache client requires an algorithm")
	}

	c := &Client{
		adapter:    cfg.Adapter,
		ttl:        cfg.TTL,
		capacity:   cfg.Capacity,
		algorithm:  cfg.Algorithm,
		releaseKey: cfg.ReleaseKey,
	}

	return c, nil
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

func generateKey(URL string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(URL)))
}
