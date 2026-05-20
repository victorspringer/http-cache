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
	"crypto/sha256"
	"encoding/gob"
	"errors"
	"fmt"
	"hash"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"time"
)

const cacheStatusCodeHeader = "Http-Cache-Status-Code"

const methodPurge = "PURGE"

// CacheEventType identifies an observed cache middleware event.
type CacheEventType string

const (
	// CacheEventHit means a valid cached response was served.
	CacheEventHit CacheEventType = "hit"

	// CacheEventMiss means no cached response was found.
	CacheEventMiss CacheEventType = "miss"

	// CacheEventStale means an expired cached response was found and released.
	CacheEventStale CacheEventType = "stale"

	// CacheEventRefresh means a cached response was explicitly released.
	CacheEventRefresh CacheEventType = "refresh"

	// CacheEventStore means a response was stored in cache.
	CacheEventStore CacheEventType = "store"

	// CacheEventPurge means a cached response was explicitly purged.
	CacheEventPurge CacheEventType = "purge"
)

// CacheEvent is passed to an observer when cache middleware events happen.
type CacheEvent struct {
	Type       CacheEventType
	Request    *http.Request
	Key        uint64
	StatusCode int
}

// Observer receives cache middleware events.
type Observer func(CacheEvent)

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

	// CanonicalKey is a fingerprint of the request inputs (URL, vary
	// headers and body) that produced this entry. The middleware
	// compares it against the incoming request on every hit so an
	// FNV-64 collision (or a manually corrupted entry) cannot serve
	// one client's response to another. Entries written by older
	// versions of this package have an empty CanonicalKey and bypass
	// verification for backward compatibility.
	CanonicalKey []byte
}

// Client data structure for HTTP cache middleware.
type Client struct {
	adapter            Adapter
	adapterTouch       AdapterTouch
	ttl                time.Duration
	ttlSet             bool
	refreshKey         string
	methods            []string
	skipCacheHeader    string
	skipCachePathRegex *regexp.Regexp
	varyHeaders        []string
	statusCodeFilter   func(int) bool
	writeExpiresHeader bool
	observer           Observer
	purgeEnabled       bool
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

// AdapterTouch is an optional Adapter extension. When an adapter
// implements it, the middleware records each cache hit via Touch
// instead of re-encoding the response and calling Set, eliminating the
// read-modify-write lost-update race and the per-hit gob round-trip.
// Adapters that do not implement Touch keep receiving the legacy Set
// call so existing custom adapters retain their behavior.
type AdapterTouch interface {
	Touch(key uint64)
}

// Middleware is the HTTP cache middleware handler.
func (c *Client) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c.purgeEnabled && r.Method == methodPurge && c.cacheableURIPath(r.URL) {
			key, _, err := c.key(r)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			c.adapter.Release(key)
			c.observe(CacheEventPurge, r, key, http.StatusNoContent)
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if c.cacheableMethod(r.Method) && c.cacheableURIPath(r.URL) {
			key, fingerprint, err := c.key(r)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			// Refresh detection is opt-in via ClientWithRefreshKey; an empty
			// refreshKey would otherwise match URLs containing a bare "?=x"
			// (empty query key, non-empty value) and let any caller wipe the
			// cache entry.
			refreshed := false
			if c.refreshKey != "" {
				params := r.URL.Query()
				if _, ok := params[c.refreshKey]; ok {
					delete(params, c.refreshKey)
					r.URL.RawQuery = params.Encode()
					key, fingerprint, err = c.key(r)
					if err != nil {
						next.ServeHTTP(w, r)
						return
					}

					c.adapter.Release(key)
					c.observe(CacheEventRefresh, r, key, 0)
					refreshed = true
				}
			}
			if !refreshed {
				b, ok := c.adapter.Get(key)
				switch {
				case !ok:
					c.observe(CacheEventMiss, r, key, 0)
				default:
					response, decodeErr := decodeResponse(b)
					switch {
					case decodeErr != nil:
						// Corrupted or version-skewed entry: drop it and
						// fall through to the origin as a miss.
						c.adapter.Release(key)
						c.observe(CacheEventMiss, r, key, 0)
					case !canonicalKeyMatches(response.CanonicalKey, fingerprint):
						// FNV-64 collision (or corrupted entry from a
						// different logical request): release the stored
						// blob and serve a fresh response.
						c.adapter.Release(key)
						c.observe(CacheEventMiss, r, key, 0)
					case response.Valid():
						if c.adapterTouch != nil {
							c.adapterTouch.Touch(key)
						} else {
							// Legacy in-blob bookkeeping for adapters that
							// don't implement AdapterTouch. Subject to the
							// lost-update race under concurrency, but
							// preserved for backward compatibility.
							response.LastAccess = time.Now()
							response.Frequency++
							c.adapter.Set(key, response.Bytes(), response.Expiration)
						}

						statusCode := cachedStatusCode(response.Header)
						c.observe(CacheEventHit, r, key, statusCode)
						writeHeader(w.Header(), response.Header)
						if c.writeExpiresHeader && !response.Expiration.IsZero() {
							w.Header().Set("Expires", response.Expiration.UTC().Format(http.TimeFormat))
						}
						if statusCode > 0 {
							w.WriteHeader(statusCode)
						}
						w.Write(response.Value)
						return
					default:
						c.adapter.Release(key)
						c.observe(CacheEventStale, r, key, 0)
					}
				}
			}

			rw := newResponseWriter(w)
			next.ServeHTTP(rw, r)

			statusCode := rw.statusCodeValue()
			value := rw.body.Bytes()
			now := time.Now()
			expires := time.Time{}
			if c.ttl > 0 {
				expires = now.Add(c.ttl)
			}
			if c.cacheableResponse(rw, statusCode) {
				response := Response{
					Value:        value,
					Header:       cacheHeader(rw.Header(), statusCode),
					Expiration:   expires,
					LastAccess:   now,
					Frequency:    1,
					CanonicalKey: fingerprint,
				}
				c.adapter.Set(key, response.Bytes(), response.Expiration)
				c.observe(CacheEventStore, r, key, statusCode)
			}

			return
		}

		next.ServeHTTP(w, r)
	})
}

// Drop releases the cache entry matching the given request.
func (c *Client) Drop(r *http.Request) error {
	key, _, err := c.key(r)
	if err != nil {
		return err
	}
	c.adapter.Release(key)
	return nil
}

func (c *Client) cacheableResponse(rw *responseWriter, statusCode int) bool {
	if !c.statusCodeFilter(statusCode) {
		return false
	}
	if c.skipCacheHeader == "" {
		return true
	}
	return rw.Header().Get(c.skipCacheHeader) == ""
}

func (c *Client) cacheableMethod(method string) bool {
	for _, m := range c.methods {
		if method == m {
			return true
		}
	}
	return false
}

func (c *Client) cacheableURIPath(URL *url.URL) bool {
	if c.skipCachePathRegex == nil {
		return true
	}
	return !c.skipCachePathRegex.MatchString(URL.Path)
}

func (c *Client) key(r *http.Request) (uint64, []byte, error) {
	sortURLParams(r.URL)
	if r.Method != http.MethodPost || r.Body == nil {
		urlStr := r.URL.String()
		return generateKeyWithHeaders(urlStr, r.Header, c.varyHeaders),
			canonicalFingerprint(urlStr, nil, r.Header, c.varyHeaders),
			nil
	}

	body, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		return 0, nil, err
	}

	r.Body = io.NopCloser(bytes.NewBuffer(body))
	urlStr := r.URL.String()
	return generateKeyWithBodyAndHeaders(urlStr, body, r.Header, c.varyHeaders),
		canonicalFingerprint(urlStr, body, r.Header, c.varyHeaders),
		nil
}

// canonicalKeyMatches returns true when the stored canonical key matches
// the incoming request's fingerprint. An empty stored key (entries
// written by older versions of this package) bypasses verification to
// preserve backward compatibility with persistent caches.
func canonicalKeyMatches(stored, fingerprint []byte) bool {
	if len(stored) == 0 {
		return true
	}
	return bytes.Equal(stored, fingerprint)
}

// canonicalFingerprint hashes the request inputs that go into the cache
// key with SHA-256. The middleware stores the fingerprint with each
// cached response and verifies it on every hit so an FNV-64 collision
// cannot serve one request's response to another.
func canonicalFingerprint(URL string, body []byte, headers http.Header, varyHeaders []string) []byte {
	h := sha256.New()
	h.Write([]byte(URL))
	for _, name := range varyHeaders {
		canonName := http.CanonicalHeaderKey(name)
		h.Write([]byte{0})
		h.Write([]byte(canonName))
		for _, v := range headers.Values(canonName) {
			h.Write([]byte{0})
			h.Write([]byte(v))
		}
	}
	if len(body) > 0 {
		h.Write([]byte{0})
		h.Write(body)
	}
	return h.Sum(nil)
}

func (c *Client) observe(eventType CacheEventType, r *http.Request, key uint64, statusCode int) {
	if c.observer == nil {
		return
	}

	c.observer(CacheEvent{
		Type:       eventType,
		Request:    r,
		Key:        key,
		StatusCode: statusCode,
	})
}

func cacheHeader(header http.Header, statusCode int) http.Header {
	cachedHeader := cloneHeader(header)
	cachedHeader.Del(cacheStatusCodeHeader)
	if statusCode != http.StatusOK {
		cachedHeader.Set(cacheStatusCodeHeader, strconv.Itoa(statusCode))
	}
	return cachedHeader
}

func cachedStatusCode(header http.Header) int {
	statusCode := header.Get(cacheStatusCodeHeader)
	if statusCode == "" {
		return http.StatusOK
	}
	code, err := strconv.Atoi(statusCode)
	if err != nil || code < 100 {
		return http.StatusOK
	}
	return code
}

func cloneHeader(header http.Header) http.Header {
	cloned := make(http.Header, len(header))
	for k, values := range header {
		cloned[k] = append([]string(nil), values...)
	}
	return cloned
}

func writeHeader(dst http.Header, src http.Header) {
	for k, values := range src {
		if k == cacheStatusCodeHeader {
			continue
		}
		// Use a fresh slice per key. Set-Cookie and other multi-valued
		// headers MUST be emitted as separate values; joining them with a
		// comma corrupts cookies whose Expires attribute contains a comma.
		dst[k] = append([]string(nil), values...)
	}
}

// BytesToResponse converts bytes array into Response data structure.
// Decoding errors are silently swallowed for backward compatibility;
// the middleware uses decodeResponse internally so it can detect
// corruption and treat the entry as a cache miss.
func BytesToResponse(b []byte) Response {
	r, _ := decodeResponse(b)
	return r
}

// decodeResponse returns a Response and any error from the gob decoder
// so callers can distinguish empty/corrupt entries from zero-valued ones.
func decodeResponse(b []byte) (Response, error) {
	var r Response
	if len(b) == 0 {
		return r, errors.New("cache: empty response payload")
	}
	dec := gob.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&r); err != nil {
		return Response{}, err
	}
	return r, nil
}

// Valid returns whether the response can still be served from cache.
func (r Response) Valid() bool {
	return r.Expiration.IsZero() || r.Expiration.After(time.Now())
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
	hash.Write([]byte(URL))
	hash.Write(body)

	return hash.Sum64()
}

func generateKeyWithHeaders(URL string, headers http.Header, varyHeaders []string) uint64 {
	if len(varyHeaders) == 0 {
		return generateKey(URL)
	}

	hash := fnv.New64a()
	writeKeyPart(hash, URL)
	writeHeaders(hash, headers, varyHeaders)

	return hash.Sum64()
}

func generateKeyWithBodyAndHeaders(URL string, body []byte, headers http.Header, varyHeaders []string) uint64 {
	if len(varyHeaders) == 0 {
		return generateKeyWithBody(URL, body)
	}

	hash := fnv.New64a()
	writeKeyPart(hash, URL)
	writeHeaders(hash, headers, varyHeaders)
	hash.Write(body)

	return hash.Sum64()
}

func writeHeaders(hash hash.Hash64, headers http.Header, varyHeaders []string) {
	for _, header := range varyHeaders {
		name := http.CanonicalHeaderKey(header)
		writeKeyPart(hash, name)
		for _, value := range headers.Values(name) {
			writeKeyPart(hash, value)
		}
	}
}

func writeKeyPart(hash hash.Hash64, value string) {
	hash.Write([]byte{0})
	hash.Write([]byte(value))
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
	if !c.ttlSet {
		return nil, errors.New("cache client ttl is not set")
	}
	if c.methods == nil {
		c.methods = []string{http.MethodGet}
	}
	if c.statusCodeFilter == nil {
		c.statusCodeFilter = func(statusCode int) bool {
			return statusCode < 400
		}
	}

	return c, nil
}

// ClientWithAdapter sets the adapter type for the HTTP cache
// middleware client.
func ClientWithAdapter(a Adapter) ClientOption {
	return func(c *Client) error {
		c.adapter = a
		if t, ok := a.(AdapterTouch); ok {
			c.adapterTouch = t
		} else {
			c.adapterTouch = nil
		}
		return nil
	}
}

// ClientWithTTL sets how long each response is going to be cached.
func ClientWithTTL(ttl time.Duration) ClientOption {
	return func(c *Client) error {
		if int64(ttl) < 0 {
			return fmt.Errorf("cache client ttl %v is invalid", ttl)
		}

		c.ttl = ttl
		c.ttlSet = true

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

// ClientWithStatusCodeFilter sets the response status codes that can be cached.
// Optional setting. If not set, responses below 400 are cached.
func ClientWithStatusCodeFilter(filter func(int) bool) ClientOption {
	return func(c *Client) error {
		if filter == nil {
			return errors.New("cache client status code filter is not set")
		}
		c.statusCodeFilter = filter
		return nil
	}
}

// ClientWithSkipCacheResponseHeader sets a response header that prevents
// successful responses from being stored.
func ClientWithSkipCacheResponseHeader(header string) ClientOption {
	return func(c *Client) error {
		c.skipCacheHeader = header
		return nil
	}
}

// ClientWithSkipCacheURIPathRegex skips cache lookup and storage for matching
// request URL paths.
func ClientWithSkipCacheURIPathRegex(pathRegex *regexp.Regexp) ClientOption {
	return func(c *Client) error {
		c.skipCachePathRegex = pathRegex
		return nil
	}
}

// ClientWithVaryHeaders includes selected request headers in cache keys.
func ClientWithVaryHeaders(headers []string) ClientOption {
	return func(c *Client) error {
		c.varyHeaders = headers
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

// ClientWithObserver sets a function that receives cache middleware events.
// Optional setting.
func ClientWithObserver(observer Observer) ClientOption {
	return func(c *Client) error {
		if observer == nil {
			return errors.New("cache client observer is not set")
		}
		c.observer = observer
		return nil
	}
}

// ClientWithPurge enables handling PURGE requests by releasing matching cache entries.
// Optional setting.
func ClientWithPurge() ClientOption {
	return func(c *Client) error {
		c.purgeEnabled = true
		return nil
	}
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
	body       bytes.Buffer
	header     http.Header
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{
		ResponseWriter: w,
		header:         make(http.Header),
	}
}

func (w *responseWriter) Header() http.Header {
	return w.header
}

func (w *responseWriter) WriteHeader(statusCode int) {
	if w.statusCode != 0 {
		return
	}
	w.statusCode = statusCode
	writeHeader(w.ResponseWriter.Header(), w.header)
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *responseWriter) Write(b []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	w.body.Write(b)
	writeHeader(w.ResponseWriter.Header(), w.header)
	return w.ResponseWriter.Write(b)
}

func (w *responseWriter) statusCodeValue() int {
	if w.statusCode == 0 {
		return http.StatusOK
	}
	return w.statusCode
}
