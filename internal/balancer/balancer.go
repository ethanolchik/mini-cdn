package balancer

import (
	"net/http"
	"sync/atomic"
	"time"
)

// A load balancer that implements a round-robin strategy.
type RoundRobinBalancer struct {
	origins        []string
	healthyOrigins atomic.Value // []string
	idx            uint64
}

// New creates a new RoundRobinBalancer with the given list of origins. Initially, all origins are considered healthy.
func New(origins []string) *RoundRobinBalancer {
	rrb := &RoundRobinBalancer{
		origins: origins,
	}
	// Prefill healthy_origins so that GetOrigin never encounters nil on Load().
	rrb.healthyOrigins.Store(origins) // Initially, all origins are considered healthy.
	return rrb
}

func isOriginHealthy(client *http.Client, origin string) bool {
	resp, err := client.Get(origin)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	// Consider an origin healthy if it responds with a valid HTTP status code (e.g., 200 OK).
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// GetOrigin returns the next available origin in a round-robin fashion. It only considers origins that are currently 'up' (true).
func (rrb *RoundRobinBalancer) GetOrigin() string {
	healthy := rrb.healthyOrigins.Load().([]string)

	hLen := uint64(len(healthy))
	if hLen == 0 {
		return ""
	}

	// idx is incremented atomically to ensure thread safety, and the old value is used to determine which origin to return.
	oldIdx := atomic.AddUint64(&rrb.idx, 1) - 1

	return healthy[oldIdx%hLen]
}

// Run a health check on all origins at a specified interval.
// A health check is performed by sending an HTTP GET request to each origin and checking if the response status code indicates a healthy state (e.g., 200 OK).
func (rrb *RoundRobinBalancer) RunHealthChecks(interval time.Duration) {
	client := http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(interval)

	for range ticker.C {
		status := make(map[string]bool, len(rrb.origins))
		for _, addr := range rrb.origins {
			status[addr] = isOriginHealthy(&client, addr)
		}

		rrb.UpdateHealthyOrigins(status)
	}
}

// UpdateHealthyOrigins updates the list of healthy origins based on the provided status map.
// The status map contains the health status of each origin, where the key is the origin address
// and the value is a boolean indicating whether the origin is healthy (true) or not (false).
func (rrb *RoundRobinBalancer) UpdateHealthyOrigins(status map[string]bool) {
	new_healthy := make([]string, 0, len(rrb.origins))
	for _, addr := range rrb.origins {
		if status[addr] {
			new_healthy = append(new_healthy, addr)
		}
	}

	rrb.healthyOrigins.Store(new_healthy)
}
