# http-cache
[![Build Status](https://app.travis-ci.com/victorspringer/http-cache.svg?branch=master)](https://app.travis-ci.com/victorspringer/http-cache) [![Coverage Status](https://coveralls.io/repos/github/victorspringer/http-cache/badge.svg?branch=master)](https://coveralls.io/github/victorspringer/http-cache?branch=master) [![](https://img.shields.io/badge/godoc-reference-5272B4.svg?style=flat)](https://godoc.org/github.com/victorspringer/http-cache)

This is a high performance Golang HTTP middleware for server-side application layer caching, ideal for REST APIs.

It is simple, super fast, thread safe and gives the possibility to choose the adapter (memory, Redis, DynamoDB etc).

The memory adapter minimizes GC overhead to near zero and supports some options of caching algorithms (LRU, MRU, LFU, MFU). This way, it is able to store plenty of gigabytes of responses, keeping great performance and being free of leaks.

## Getting Started

### Installation
`go get github.com/victorspringer/http-cache`

### Usage
This is an example of use with the memory adapter:

```go
package main

import (
    "log"
    "net/http"
    "time"

    "github.com/victorspringer/http-cache"
    "github.com/victorspringer/http-cache/adapter/memory"
)

func example(w http.ResponseWriter, r *http.Request) {
    w.Write([]byte("Ok"))
}

func main() {
    memcached, err := memory.NewAdapter(
        memory.AdapterWithAlgorithm(memory.LRU),
        memory.AdapterWithCapacity(10000000),
    )
    if err != nil {
        log.Fatal(err)
    }

    cacheClient, err := cache.NewClient(
        cache.ClientWithAdapter(memcached),
        cache.ClientWithTTL(10 * time.Minute),
        cache.ClientWithRefreshKey("opn"),
    )
    if err != nil {
        log.Fatal(err)
    }

    handler := http.HandlerFunc(example)

    http.Handle("/", cacheClient.Middleware(handler))
    http.ListenAndServe(":8080", nil)
}
```

Example of Client initialization with Redis adapter:
```go
import (
    "github.com/victorspringer/http-cache"
    "github.com/victorspringer/http-cache/adapter/redis"
)

...

    ringOpt := &redis.RingOptions{
        Addrs: map[string]string{
            "server": ":6379",
        },
    }
    cacheClient, err := cache.NewClient(
        cache.ClientWithAdapter(redis.NewAdapter(ringOpt)),
        cache.ClientWithTTL(10 * time.Minute),
        cache.ClientWithRefreshKey("opn"),
    )

...
```

## Optional Features

### Programmatic invalidation
Use `Drop` to release the cached response that matches a request. The same cache key rules used by the middleware are applied, including normalized query params, POST bodies and configured vary headers.

```go
req, err := http.NewRequest(http.MethodGet, "https://example.com/products?page=1", nil)
if err != nil {
    log.Fatal(err)
}

if err := cacheClient.Drop(req); err != nil {
    log.Fatal(err)
}
```

### PURGE requests
`PURGE` support is opt-in so existing applications that already handle `PURGE` keep working as before. Authentication should be handled by your application or middleware stack.

```go
cacheClient, err := cache.NewClient(
    cache.ClientWithAdapter(memcached),
    cache.ClientWithTTL(10 * time.Minute),
    cache.ClientWithPurge(),
)
```

When enabled, a matching `PURGE` request releases the cached response and returns `204 No Content`.

### Observability
Use `ClientWithObserver` to receive cache middleware events. The event includes the request, cache key, event type and status code when available.

```go
cacheClient, err := cache.NewClient(
    cache.ClientWithAdapter(memcached),
    cache.ClientWithTTL(10 * time.Minute),
    cache.ClientWithObserver(func(event cache.CacheEvent) {
        log.Printf("cache event=%s key=%d status=%d path=%s",
            event.Type,
            event.Key,
            event.StatusCode,
            event.Request.URL.Path,
        )
    }),
)
```

Available event types are `hit`, `miss`, `stale`, `refresh`, `store` and `purge`.

### Cache key and storage options

- `ClientWithMethods` enables caching for `GET` and/or `POST` requests.
- `ClientWithVaryHeaders` includes selected request headers in the cache key.
- `ClientWithStatusCodeFilter` controls which response status codes can be cached.
- `ClientWithSkipCacheResponseHeader` skips storage when a response includes a configured header.
- `ClientWithSkipCacheURIPathRegex` skips lookup and storage for matching URL paths.
- `ClientWithExpiresHeader` writes the cached response expiration as an `Expires` header.

## Benchmarks
The benchmarks were based on [allegro/bigcache](https://github.com/allegro/bigcache) tests and used to compare it with the http-cache memory adapter.<br>
The tests were run using an Intel i5-2410M with 8GB RAM on Arch Linux 64bits.<br>
The results are shown below:

### Writes and Reads
```bash
cd adapter/memory/benchmark
go test -bench=. -benchtime=10s ./... -timeout 30m

BenchmarkHTTPCacheMamoryAdapterSet-4             5000000     343 ns/op    172 B/op    1 allocs/op
BenchmarkBigCacheSet-4                           3000000     507 ns/op    535 B/op    1 allocs/op
BenchmarkHTTPCacheMamoryAdapterGet-4            20000000     146 ns/op      0 B/op    0 allocs/op
BenchmarkBigCacheGet-4                           3000000     343 ns/op    120 B/op    3 allocs/op
BenchmarkHTTPCacheMamoryAdapterSetParallel-4    10000000     223 ns/op    172 B/op    1 allocs/op
BenchmarkBigCacheSetParallel-4                  10000000     291 ns/op    661 B/op    1 allocs/op
BenchmarkHTTPCacheMemoryAdapterGetParallel-4    50000000    56.1 ns/op      0 B/op    0 allocs/op
BenchmarkBigCacheGetParallel-4                  10000000     163 ns/op    120 B/op    3 allocs/op
```
http-cache writes are slightly faster and reads are much more faster.

### Garbage Collection Pause Time
```bash
cache=http-cache go run benchmark_gc_overhead.go

Number of entries:  20000000
GC pause for http-cache memory adapter:  2.445617ms

cache=bigcache go run benchmark_gc_overhead.go

Number of entries:  20000000
GC pause for bigcache:  7.43339ms
```
http-cache memory adapter takes way less GC pause time, that means smaller GC overhead.

## Roadmap
- Make it compliant with RFC7234
- Add more middleware configuration (cacheable status codes, paths etc)
- Develop gRPC middleware
- Develop Badger adapter
- Develop DynamoDB adapter
- Develop MongoDB adapter

## Godoc Reference
- [http-cache](https://godoc.org/github.com/victorspringer/http-cache)
- [Memory adapter](https://godoc.org/github.com/victorspringer/http-cache/adapter/memory)
- [Redis adapter](https://godoc.org/github.com/victorspringer/http-cache/adapter/redis)

## License
http-cache is released under the [MIT License](https://github.com/victorspringer/http-cache/blob/master/LICENSE).
