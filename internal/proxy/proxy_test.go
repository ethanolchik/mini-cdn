package proxy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethanolchik/mini-cdn/internal/cache"
)

type testBalancer struct {
	origins []string
	idx     uint64
}

func (b *testBalancer) GetOrigin() (string, error) {
	if len(b.origins) == 0 {
		return "", errors.New("no healthy origins available")
	}

	oldIdx := atomic.AddUint64(&b.idx, 1) - 1
	return b.origins[oldIdx%uint64(len(b.origins))], nil
}

func (b *testBalancer) RunHealthChecks(context.Context, time.Duration) {}

func (b *testBalancer) UpdateHealthyOrigins(map[string]bool) {}

// newTestProxy creates a proxy pointing at the given origin URL.
func newTestProxy(originURL string) *ReverseProxy {
	return New(&testBalancer{origins: []string{originURL}})
}

// newTestProxyWithTTL creates a proxy with a custom TTL for testing expiry.
func newTestProxyWithTTL(originURL string, ttl float64) *ReverseProxy {
	rp := newTestProxy(originURL)
	rp.cache = cache.New(100, 60)
	return rp
}

// TestCacheMissAndHit verifies that the first request is a cache miss and the second is a cache hit.
func TestCacheMissAndHit(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello from origin"))
	}))
	defer origin.Close()

	rp := newTestProxy(origin.URL)

	// First request — cache miss
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	rp.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("X-Cache"); got != "MISS" {
		t.Errorf("expected X-Cache: MISS, got %q", got)
	}
	if w.Body.String() != "hello from origin" {
		t.Errorf("unexpected body: %q", w.Body.String())
	}

	// Second request — cache hit
	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	w = httptest.NewRecorder()
	rp.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("X-Cache"); got != "HIT" {
		t.Errorf("expected X-Cache: HIT, got %q", got)
	}
	if w.Body.String() != "hello from origin" {
		t.Errorf("unexpected body: %q", w.Body.String())
	}
}

// TestCacheKeyIsolation verifies that different paths are cached independently.
func TestCacheKeyIsolation(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("path: " + r.URL.Path))
	}))
	defer origin.Close()

	rp := newTestProxy(origin.URL)

	for _, path := range []string{"/a", "/b", "/a"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		rp.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("path %s: expected 200, got %d", path, w.Code)
		}
	}

	// /a should now be a HIT, /b was only requested once so also HIT now
	for _, path := range []string{"/a", "/b"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		rp.ServeHTTP(w, req)

		if got := w.Header().Get("X-Cache"); got != "HIT" {
			t.Errorf("path %s: expected X-Cache: HIT, got %q", path, got)
		}
	}
}

// TestNotAvailableNoOrigins verifies that a proxy with no origins returns 503.
func TestNotAvailableNoOrigins(t *testing.T) {
	rp := New(&testBalancer{})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	rp.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 Service Unavailable, got %d", w.Code)
	}
}

// TestBadGatewayUnreachableOrigin verifies that an unreachable origin returns 502.
func TestBadGatewayUnreachableOrigin(t *testing.T) {
	rp := New(&testBalancer{origins: []string{"http://localhost:1"}}) // nothing listening here

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	rp.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502 Bad Gateway, got %d", w.Code)
	}
}

// TestTTLExpiry verifies that cache entries expire after the TTL.
func TestTTLExpiry(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=1")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello from origin"))
	}))
	defer origin.Close()

	rp := newTestProxyWithTTL(origin.URL, 1) // 1 second TTL

	// First request =  cache miss
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	rp.ServeHTTP(w, req)

	if got := w.Header().Get("X-Cache"); got != "MISS" {
		t.Fatalf("expected MISS, got %q", got)
	}

	// Second request before TTL = cache hit
	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	w = httptest.NewRecorder()
	rp.ServeHTTP(w, req)

	if got := w.Header().Get("X-Cache"); got != "HIT" {
		t.Fatalf("expected HIT before TTL expiry, got %q", got)
	}

	// Wait for TTL to expire
	time.Sleep(2 * time.Second)

	// Third request after TTL = cache miss again
	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	w = httptest.NewRecorder()
	rp.ServeHTTP(w, req)

	if got := w.Header().Get("X-Cache"); got != "MISS" {
		t.Errorf("expected MISS after TTL expiry, got %q", got)
	}
}

// TestHopByHopStripped verifies that hop-by-hop headers are stripped from the forwarded request.
func TestHopByHopStripped(t *testing.T) {
	hopByHop := []string{
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Te",
		"Trailers",
		"Transfer-Encoding",
		"Upgrade",
	}

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, h := range hopByHop {
			if r.Header.Get(h) != "" {
				t.Errorf("hop-by-hop header %q should have been stripped", h)
			}
		}
		// X-Custom-Header is listed in Connection so should also be stripped
		if r.Header.Get("X-Custom-Header") != "" {
			t.Error("X-Custom-Header should have been stripped by Connection header directive")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer origin.Close()

	rp := newTestProxy(origin.URL)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	for _, h := range hopByHop {
		req.Header.Set(h, "test-value")
	}
	req.Header.Set("Connection", "keep-alive, X-Custom-Header")
	req.Header.Set("X-Custom-Header", "should be stripped")

	w := httptest.NewRecorder()
	rp.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// TestXForwardedFor verifies that the X-Forwarded-For header is set on the forwarded request.
func TestXForwardedFor(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Forwarded-For") == "" {
			t.Error("expected X-Forwarded-For header to be set")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer origin.Close()

	rp := newTestProxy(origin.URL)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	rp.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// TestXForwardedForAppended verifies that existing X-Forwarded-For headers are appended to, not replaced.
func TestXForwardedForAppended(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		xff := r.Header.Get("X-Forwarded-For")
		if xff == "" {
			t.Error("expected X-Forwarded-For to be set")
		}
		// Should contain the original upstream IP and the new one
		if xff == "10.0.0.1" {
			t.Error("expected X-Forwarded-For to be appended, not replaced")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer origin.Close()

	rp := newTestProxy(origin.URL)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.1")

	w := httptest.NewRecorder()
	rp.ServeHTTP(w, req)
}

// TestSingleflight verifies that concurrent requests for the same resource only hit the origin once.
func TestSingleflight(t *testing.T) {
	var originHits atomic.Int32

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originHits.Add(1)
		time.Sleep(100 * time.Millisecond) // simulate slow origin
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello from origin"))
	}))
	defer origin.Close()

	rp := newTestProxy(origin.URL)

	numRequests := 10
	done := make(chan struct{}, numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			w := httptest.NewRecorder()
			rp.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("expected 200, got %d", w.Code)
			}
			done <- struct{}{}
		}()
	}

	for i := 0; i < numRequests; i++ {
		<-done
	}

	if hits := originHits.Load(); hits > 1 {
		t.Errorf("expected 1 origin hit due to singleflight, got %d", hits)
	}
}

// TestResponseBodyPreserved verifies that the response body is correctly forwarded to the client.
func TestResponseBodyPreserved(t *testing.T) {
	expected := "exact response body"

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(expected))
	}))
	defer origin.Close()

	rp := newTestProxy(origin.URL)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	rp.ServeHTTP(w, req)

	if w.Body.String() != expected {
		t.Errorf("expected body %q, got %q", expected, w.Body.String())
	}
}
