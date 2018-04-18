comparison:
	go test -bench=. -benchtime=10s ./... -timeout 30m

gc_overhead_http_cache:
	cache=http_cache go run benchmark_gc_overhead.go

gc_overhead_bigcache:
	cache=bigcache go run benchmark_gc_overhead.go