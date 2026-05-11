package balancer

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type Balancer interface {
	GetOrigin() (string, error)
	RunHealthChecks(ctx context.Context, interval time.Duration)
	UpdateHealthyOrigins(cstatus map[string]bool)
}

// A load balancer that implements a round-robin strategy.
type RoundRobinBalancer struct {
	origins        []string
	healthyOrigins atomic.Value // []string
	idx            uint64
}

func isOriginHealthy(client *http.Client, origin string) bool {
	// Only fetch the headers for efficiency
	resp, err := client.Head(origin)
	if err != nil {
		return false
	}

	// If we get 405 (Method Not Allowed), we will resort to using GET
	if resp.StatusCode == 405 {
		resp, err = client.Get(origin)
		if err != nil {
			return false
		}
	}

	defer resp.Body.Close()
	// Consider an origin healthy if it responds with a valid HTTP status code (e.g., 200 OK).
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// New creates a new RoundRobinBalancer with the given list of origins. Initially, all origins are considered healthy.
func New(origins []string) *RoundRobinBalancer {
	rrb := &RoundRobinBalancer{origins: origins}
	// Prefill healthy_origins so that GetOrigin never encounters nil on Load().
	rrb.healthyOrigins.Store(origins) // Initially, all origins are considered healthy.

	go rrb.RunHealthChecks(context.Background(), 10*time.Second)

	return rrb
}

// GetOrigin returns the next available origin in a round-robin fashion. It only considers origins that are currently 'up' (true).
func (rrb *RoundRobinBalancer) GetOrigin() (string, error) {
	healthy := rrb.healthyOrigins.Load().([]string)

	hLen := uint64(len(healthy))
	if hLen == 0 {
		return "", errors.New("no healthy origins available")
	}

	// idx is incremented atomically to ensure thread safety, and the old value is used to determine which origin to return.
	oldIdx := atomic.AddUint64(&rrb.idx, 1) - 1

	return healthy[oldIdx%hLen], nil
}

// Run a health check on all origins at a specified interval.
// A health check is performed by sending an HTTP GET request to each origin and checking if the response status code indicates a healthy state (e.g., 200 OK).
func (rrb *RoundRobinBalancer) RunHealthChecks(ctx context.Context, interval time.Duration) {
	client := http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	check := func() {
		var wg sync.WaitGroup
		var mu sync.Mutex // Protects the local status map

		status := make(map[string]bool, len(rrb.origins))

		for _, addr := range rrb.origins {
			wg.Add(1)

			// Launch goroutine for each origin probe
			go func(a string) {
				defer wg.Done()

				healthy := isOriginHealthy(&client, a)

				// Lock the map only for the write
				mu.Lock()
				status[a] = healthy
				mu.Unlock()
			}(addr)
		}

		// Wait for every single probe to return or timeout
		wg.Wait()

		// Update the shared atomic state in the balancer
		rrb.UpdateHealthyOrigins(status)
	}

	// Run an initial check immediately before starting the ticker to avoid waiting for the first tick.
	check()

	for {
		// Wait for the next tick or context cancellation.
		select {
		case <-ticker.C:
			check()
		case <-ctx.Done():
			return
		}
	}
}

// UpdateHealthyOrigins updates the list of healthy origins based on the provided status map.
// The status map contains the health status of each origin, where the key is the origin address
// and the value is a boolean indicating whether the origin is healthy (true) or not (false).
func (rrb *RoundRobinBalancer) UpdateHealthyOrigins(status map[string]bool) {
	newHealthy := make([]string, 0, len(rrb.origins))
	for _, addr := range rrb.origins {
		if status[addr] {
			newHealthy = append(newHealthy, addr)
		}
	}

	rrb.healthyOrigins.Store(newHealthy)
}
