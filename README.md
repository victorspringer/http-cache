# http-cache
[![Build Status](https://travis-ci.org/victorspringer/http-cache.svg?branch=master)](https://travis-ci.org/victorspringer/http-cache) [![Coverage Status](https://coveralls.io/repos/victorspringer/http-cache/badge.svg)](https://coveralls.io/r/victorspringer/http-cache) [![](https://img.shields.io/badge/godoc-reference-5272B4.svg?style=flat)](https://godoc.org/github.com/victorspringer/http-cache)

This is a HTTP middleware for server-side application layer caching, ideal for Golang REST APIs.

It is simple, fast, thread safe and gives the possibility to choose the adapter (memory, Redis, DynamoDB etc), algorithm (LRU, MRU, LFU, MFU) and to set up the capacity, TTL, release key and much more.

## Getting Started

### Installation
`go get github.com/victorspringer/http-cache`

### Usage
```go
package main

import (
    "fmt"
    "net/http"
    "os"
    "time"

    "github.com/victorspringer/http-cache"
    "github.com/victorspringer/http-cache/adapter"
)

func example(w http.ResponseWriter, r *http.Request) {
    w.Write([]byte("Ok"))
}

func main() {
    cfg := cache.Config{
        Adapter:    adapter.Memory(),
        ReleaseKey: "opn",
        TTL:        10 * time.Minute,
        Capacity:   1000,
        Algorithm:  cache.LFU,
    }
    cacheClient, err := cache.NewClient(cfg)
    if err != nil {
        fmt.Println(err)
        os.Exit(1)
    }

    handler := http.HandlerFunc(example)

    http.Handle("/", cacheClient.Middleware(handler))
    http.ListenAndServe(":8080", nil)
}
```

## Documentation
[https://godoc.org/github.com/victorspringer/http-cache](https://godoc.org/github.com/victorspringer/http-cache)