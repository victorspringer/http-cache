# http-cache
[![Build Status](https://travis-ci.org/victorspringer/http-cache.svg?branch=master)](https://travis-ci.org/victorspringer/http-cache) [![Coverage Status](https://coveralls.io/repos/github/victorspringer/http-cache/badge.svg?branch=master)](https://coveralls.io/github/victorspringer/http-cache?branch=master) [![](https://img.shields.io/badge/godoc-reference-5272B4.svg?style=flat)](https://godoc.org/github.com/victorspringer/http-cache)

This is a HTTP middleware for server-side application layer caching, ideal for Golang REST APIs.

It is simple, super fast, thread safe and gives the possibility to choose the adapter (memory, Redis, DynamoDB etc).

## Getting Started

### Installation
`go get github.com/victorspringer/http-cache`

### Usage
This is an example of use with the memory adapter:

```go
package main

import (
    "fmt"
    "net/http"
    "os"
    "time"
    
    "github.com/victorspringer/http-cache"
    "github.com/victorspringer/http-cache/adapter/memory"
)

func example(w http.ResponseWriter, r *http.Request) {
    w.Write([]byte("Ok"))
}

func main() {
    memcached, err := memory.NewAdapter(
        &memory.Config{
            Algorithm: memory.LRU,
            Capacity:  10,
        },
    )
    if err != nil {
        fmt.Println(err)
        os.Exit(1)
    }

    cacheClient, err := cache.NewClient(
        &cache.Config{
            Adapter:    memcached,
            ReleaseKey: "opn",
            TTL:        10 * time.Minute,
        },
    )
    if err != nil {
        fmt.Println(err)
        os.Exit(1)
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
    cacheClient := cache.NewClient(
        &cache.Config{
            Adapter:    redis.NewAdapter(ringOpt),
            ReleaseKey: "opn",
            TTL:        10 * time.Minute,
        },
    )

...
```

## Benchmarks
The benchmarks were based on [allegro/bigache](https://github.com/allegro/bigcache) tests and used to compare it with the http-cache memory adapter.<br>
The tests were run using an Intel i5-2410M with 8GB RAM on Arch Linux 64bits.<br>
The results are shown below:

### Writes and Reads
```bash
cd benchmark
go test -bench=. -benchtime=10s ./... -timeout 30m

BenchmarkHTTPCacheMamoryAdapterSet-4             2000000               700 ns/op             242 B/op          1 allocs/op
BenchmarkBigCacheSet-4                           3000000	       550 ns/op	     535 B/op	       1 allocs/op
BenchmarkHTTPCacheMamoryAdapterGet-4            20000000	       158 ns/op	       0 B/op	       0 allocs/op
BenchmarkBigCacheGet-4                           3000000	       343 ns/op	     120 B/op	       3 allocs/op
BenchmarkHTTPCacheMamoryAdapterSetParallel-4     5000000	       277 ns/op	     112 B/op	       1 allocs/op
BenchmarkBigCacheSetParallel-4                  10000000	       267 ns/op	     533 B/op	       1 allocs/op
BenchmarkHTTPCacheMemoryAdapterGetParallel-4    50000000	       56.1 ns/op	       0 B/op	       0 allocs/op
BenchmarkBigCacheGetParallel-4                  10000000	       137 ns/op	     120 B/op	       3 allocs/op
```
Writes in http-cache are a little bit slower. Reads are much more faster than in bigcache.

### Garbage Collection Pause Time
```bash
cache=http_cache go run benchmark_gc_overhead.go

Number of entries:  20000000
GC pause for http-cache memory adapter:  12.395315ms

cache=bigcache go run benchmark_gc_overhead.go

Number of entries:  20000000
GC pause for bigcache:  11.643352ms
```
There is not much difference in GC pause time, as http-cache takes less than 1ms longer.

## Roadmap
- Develop DynamoDB adapter
- Develop MongoDB adapter

## Godoc Reference
- [http-cache](https://godoc.org/github.com/victorspringer/http-cache)
- [Memory adapter](https://godoc.org/github.com/victorspringer/http-cache/adapter/memory)
- [Redis adapter](https://godoc.org/github.com/victorspringer/http-cache/adapter/redis)