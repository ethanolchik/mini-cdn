# mini-cdn
A reverse proxy and caching layer in Go with LRU eviction, per-entry TTL driven by `Cache-Control` headers, singleflight-based stampede protection, and round-robin load balancing with concurrent health checks.

---

## Benchmarks
Benchmarked with `wrk` (-t4 -c100 -d30s) against a local origin with 20ms simulated latency:
| Scenario | Req/s | Avg Latency | Image |
|---|---|---|---|
| Cache MISS (unique URLs, always fetches from origin) | 4,627 | 27.92ms | <img src="assets/cache_miss_bench.png"> |
| Cache HIT (single primed URL, served from memory) | 172,068 | 5.56 |<img src="assets/cache_hit_bench.png">

**37x throughput improvement on cache hits.**

---

## Project Structure
 
```
mini-cdn/
├── cmd/
│   ├── main.go              # entry point — wires proxy, balancer, starts server
│   └── origin/
│       └── main.go          # test origin server with configurable latency
├── internal/
│   ├── proxy/
│   │   ├── proxy.go         # reverse proxy, caching, singleflight
│   │   └── proxy_test.go    # 88.2% statement coverage
│   ├── cache/
│   │   └── cache.go         # LRU cache with per-entry TTL
│   └── balancer/
│       └── balancer.go      # round-robin balancer with health checks
├── scripts/
│   └── random_path.lua      # wrk script for cache-miss benchmarking
├── assets/                  # contains images included in readme
├── go.mod
└── go.sum
```

---
 
## Running
 
```bash
# Start a test origin on :8080
go run cmd/origin/main.go
 
# Start the proxy on :8081
go run cmd/main.go
 
# Test
curl -v http://localhost:8081/
```

### Running Tests
 
```bash
go test -v ./internal/...
```

### Benchmarking
 
```bash
# Cache miss — unique URLs, always hits origin
wrk -t4 -c100 -d30s -s scripts/random_path.lua http://localhost:8081/
 
# Cache hit — prime the cache, then benchmark
curl http://localhost:8081/test
wrk -t4 -c100 -d30s http://localhost:8081/test
```
---