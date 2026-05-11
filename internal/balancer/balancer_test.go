package balancer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestRoundRobinBalancer(origins []string) *RoundRobinBalancer {
	rrb := &RoundRobinBalancer{origins: origins}
	rrb.healthyOrigins.Store(origins)
	return rrb
}

func TestGetOriginRoundRobin(t *testing.T) {
	rrb := newTestRoundRobinBalancer([]string{"origin-a", "origin-b", "origin-c"})

	expected := []string{
		"origin-a",
		"origin-b",
		"origin-c",
		"origin-a",
		"origin-b",
	}

	for i, want := range expected {
		got, err := rrb.GetOrigin()
		if err != nil {
			t.Fatalf("request %d: unexpected error: %v", i, err)
		}
		if got != want {
			t.Fatalf("request %d: expected %q, got %q", i, want, got)
		}
	}
}

func TestGetOriginNoHealthyOrigins(t *testing.T) {
	rrb := newTestRoundRobinBalancer([]string{})

	if got, err := rrb.GetOrigin(); err == nil {
		t.Fatalf("expected error, got origin %q", got)
	}
}

func TestUpdateHealthyOriginsFiltersAndPreservesOrder(t *testing.T) {
	rrb := newTestRoundRobinBalancer([]string{"origin-a", "origin-b", "origin-c"})

	rrb.UpdateHealthyOrigins(map[string]bool{
		"origin-a": false,
		"origin-b": true,
		"origin-c": true,
	})

	expected := []string{"origin-b", "origin-c", "origin-b"}
	for i, want := range expected {
		got, err := rrb.GetOrigin()
		if err != nil {
			t.Fatalf("request %d: unexpected error: %v", i, err)
		}
		if got != want {
			t.Fatalf("request %d: expected %q, got %q", i, want, got)
		}
	}
}

func TestUpdateHealthyOriginsWithNoHealthyOrigins(t *testing.T) {
	rrb := newTestRoundRobinBalancer([]string{"origin-a", "origin-b"})

	rrb.UpdateHealthyOrigins(map[string]bool{
		"origin-a": false,
		"origin-b": false,
	})

	if got, err := rrb.GetOrigin(); err == nil {
		t.Fatalf("expected error, got origin %q", got)
	}
}

func TestIsOriginHealthy(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    bool
	}{
		{
			name: "head success",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodHead {
					t.Errorf("expected HEAD request, got %s", r.Method)
				}
				w.WriteHeader(http.StatusNoContent)
			},
			want: true,
		},
		{
			name: "head server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			want: false,
		},
		{
			name: "head method not allowed falls back to get",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodHead {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				if r.Method != http.MethodGet {
					t.Errorf("expected GET fallback, got %s", r.Method)
				}
				w.WriteHeader(http.StatusOK)
			},
			want: true,
		},
	}

	client := &http.Client{Timeout: time.Second}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origin := httptest.NewServer(tt.handler)
			defer origin.Close()

			if got := isOriginHealthy(client, origin.URL); got != tt.want {
				t.Fatalf("expected healthy=%t, got %t", tt.want, got)
			}
		})
	}
}

func TestRunHealthChecksUpdatesHealthyOrigins(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthy.Close()

	unhealthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer unhealthy.Close()

	rrb := newTestRoundRobinBalancer([]string{healthy.URL, unhealthy.URL})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rrb.RunHealthChecks(ctx, time.Hour)

	got, err := rrb.GetOrigin()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != healthy.URL {
		t.Fatalf("expected only healthy origin %q, got %q", healthy.URL, got)
	}
}
