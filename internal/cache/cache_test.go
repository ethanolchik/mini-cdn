package cache

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func testEntry(body string) CacheEntry {
	return CacheEntry{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body:       []byte(body),
	}
}

func TestCacheKey(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		query  string
		want   string
	}{
		{
			name:   "without query",
			method: http.MethodGet,
			path:   "/assets/logo.png",
			want:   "GET /assets/logo.png",
		},
		{
			name:   "with query",
			method: http.MethodGet,
			path:   "/assets/logo.png",
			query:  "v=1",
			want:   "GET /assets/logo.png?v=1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CacheKey(tt.method, tt.path, tt.query); got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestNewPanicsWithNonPositiveCapacity(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()

	New(0, 60)
}

func TestPutAndGet(t *testing.T) {
	c := New(2, 60)

	c.Put("GET /a", testEntry("alpha"), time.Minute)

	got, err := c.Get("GET /a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, got.StatusCode)
	}
	if string(got.Body) != "alpha" {
		t.Fatalf("expected body %q, got %q", "alpha", string(got.Body))
	}
	if got.Header.Get("Content-Type") != "text/plain" {
		t.Fatalf("expected content type %q, got %q", "text/plain", got.Header.Get("Content-Type"))
	}
	if got.Timestamp.IsZero() {
		t.Fatal("expected timestamp to be set")
	}
	if c.Size() != 1 {
		t.Fatalf("expected size 1, got %d", c.Size())
	}
}

func TestGetMissingKeyReturnsCacheMissError(t *testing.T) {
	c := New(1, 60)

	_, err := c.Get("GET /missing")
	if err == nil {
		t.Fatal("expected error")
	}

	var miss *CacheMissError
	if !errors.As(err, &miss) {
		t.Fatalf("expected CacheMissError, got %T", err)
	}
}

func TestPutExistingKeyUpdatesValueAndRecency(t *testing.T) {
	c := New(2, 60)

	c.Put("GET /a", testEntry("alpha"), time.Minute)
	c.Put("GET /b", testEntry("bravo"), time.Minute)
	c.Put("GET /a", testEntry("alpha-updated"), time.Minute)
	c.Put("GET /c", testEntry("charlie"), time.Minute)

	if _, err := c.Get("GET /b"); err == nil {
		t.Fatal("expected GET /b to be evicted")
	}

	got, err := c.Get("GET /a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got.Body) != "alpha-updated" {
		t.Fatalf("expected updated body, got %q", string(got.Body))
	}
	if c.Size() != 2 {
		t.Fatalf("expected size 2, got %d", c.Size())
	}
}

func TestGetRefreshesRecency(t *testing.T) {
	c := New(2, 60)

	c.Put("GET /a", testEntry("alpha"), time.Minute)
	c.Put("GET /b", testEntry("bravo"), time.Minute)

	if _, err := c.Get("GET /a"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c.Put("GET /c", testEntry("charlie"), time.Minute)

	if _, err := c.Get("GET /b"); err == nil {
		t.Fatal("expected GET /b to be evicted")
	}
	if _, err := c.Get("GET /a"); err != nil {
		t.Fatalf("expected GET /a to remain cached: %v", err)
	}
}

func TestPutEvictsLeastRecentlyUsedWhenFull(t *testing.T) {
	c := New(2, 60)

	c.Put("GET /a", testEntry("alpha"), time.Minute)
	c.Put("GET /b", testEntry("bravo"), time.Minute)
	c.Put("GET /c", testEntry("charlie"), time.Minute)

	if _, err := c.Get("GET /a"); err == nil {
		t.Fatal("expected GET /a to be evicted")
	}
	if _, err := c.Get("GET /b"); err != nil {
		t.Fatalf("expected GET /b to remain cached: %v", err)
	}
	if _, err := c.Get("GET /c"); err != nil {
		t.Fatalf("expected GET /c to remain cached: %v", err)
	}
	if c.Size() != c.Capacity() {
		t.Fatalf("expected size to equal capacity %d, got %d", c.Capacity(), c.Size())
	}
}

func TestGetExpiredEntryReturnsCacheMissAndRemovesEntry(t *testing.T) {
	c := New(1, 60)

	c.Put("GET /a", testEntry("alpha"), time.Nanosecond)
	time.Sleep(time.Millisecond)

	if _, err := c.Get("GET /a"); err == nil {
		t.Fatal("expected expired entry to miss")
	}
	if c.Size() != 0 {
		t.Fatalf("expected expired entry to be removed, size is %d", c.Size())
	}
}

func TestGetMaxTTLSeconds(t *testing.T) {
	c := New(1, 1.5)

	if got := c.GetMaxTTLSeconds(); got != 1500*time.Millisecond {
		t.Fatalf("expected 1.5s, got %s", got)
	}
}
