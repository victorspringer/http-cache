# http-cache
[![Build Status](https://travis-ci.org/victorspringer/http-cache.svg?branch=master)](https://travis-ci.org/victorspringer/http-cache) [![Coverage Status](https://coveralls.io/repos/github/victorspringer/http-cache/badge.svg?branch=master)](https://coveralls.io/github/victorspringer/http-cache?branch=master) [![](https://img.shields.io/badge/godoc-reference-5272B4.svg?style=flat)](https://godoc.org/github.com/victorspringer/http-cache)

This is a HTTP middleware for server-side application layer caching, ideal for Golang REST APIs.

It is simple, fast, thread safe and gives the possibility to choose the adapter (memory, Redis, DynamoDB etc).

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
        cache.Config{
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

## Roadmap
- Solve the GC overhead issue in memory adapter
- Develop DynamoDB adapter
- Develop MongoDB adapter

## Godoc Reference
- [http-cache](https://godoc.org/github.com/victorspringer/http-cache)
- [Memory adapter](https://godoc.org/github.com/victorspringer/http-cache/adapter/memory)
- [Redis adapter](https://godoc.org/github.com/victorspringer/http-cache/adapter/redis)