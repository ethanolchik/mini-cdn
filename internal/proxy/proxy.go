package proxy

import (
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/ethanolchik/mini-cdn/internal/cache"
	"golang.org/x/sync/singleflight"
)

var hopByHopHeadersMap = map[string]struct{}{
	"Connection":          {},
	"Keep-Alive":          {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
	"Proxy-Authorization": {},
	"Proxy-Connection":    {},
	"Te":                  {},
	"Trailers":            {},
}

// Check if a header is in the hop-by-hop map.
func isHopByHop(header string) bool {
	_, ok := hopByHopHeadersMap[http.CanonicalHeaderKey(header)]
	return ok
}

// Copy a set of headers, removing all hop-by-hop headers.
func copyHeaders(dest, src http.Header) {
	for key, values := range src {
		if isHopByHop(key) {
			continue
		}

		for _, v := range values {
			dest.Add(key, v)
		}
	}
}

// ReverseProxy is a simple reverse proxy that forwards requests to the specified origins.
type ReverseProxy struct {
	origins []string
	client  *http.Client
	cache   *cache.Cache
	group   singleflight.Group
}

// New creates a new ReverseProxy with the specified origins and a default HTTP client with a 30 second timeout and no redirects.
func New(origins []string) *ReverseProxy {
	c := cache.New(100, 60) // TODO: Make these parameters configurable in the future
	return &ReverseProxy{
		origins: origins,
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		cache: &c,
	}
}

// Clone a request, removing all hop-by-hop headers.
func (rp *ReverseProxy) cloneRequest(r *http.Request, originURL string) (*http.Request, error) {
	target, err := url.Parse(originURL)
	if err != nil {
		return nil, err
	}

	// Preserve the path and query string from the original request
	target.Path = r.URL.Path
	target.RawQuery = r.URL.RawQuery

	// Use NewRequestWithContext to propagate cancellation from the client - if the client disconnects mid request,
	// the context is cancelled and the outbound request will be cancelled too.
	outbound, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), r.Body)
	if err != nil {
		return nil, err
	}

	// Copy headers across, skipping hop-by-hop
	copyHeaders(outbound.Header, r.Header)

	return outbound, nil
}

// Copies the request, strips any Connection headers and adds an X-Forwarded-For header with the client's IP address.
// Then it forwards the request to the first origin in the list and writes the response back to the client.
// TODO: In the future, we can add load balancing and health checks to distribute requests across multiple origins.
func (rp *ReverseProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// If we don't have any origins, return
	if len(rp.origins) == 0 {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}

	// Check if the request is in cache. If it is, return the cached response. If it's not, forward the request to the origin and cache the response for future requests.
	cacheKey := cache.CacheKey(r.Method, r.URL.Path, r.URL.RawQuery)
	if entry, err := rp.cache.Get(cacheKey); err == nil {
		for key, values := range entry.Header {
			for _, v := range values {
				w.Header().Add(key, v)
			}
		}
		w.Header().Set("X-Cache", "HIT")
		w.WriteHeader(entry.StatusCode)
		w.Write(entry.Body)
		return
	}

	// For now, we select the first origin. This will change when load balancing is introduced
	newReq, err := rp.cloneRequest(r, rp.origins[0])
	if err != nil {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}

	if _, ok := newReq.Header["X-Forwarded-For"]; ok {
		existing := newReq.Header.Get("X-Forwarded-For")
		newReq.Header.Set("X-Forwarded-For", existing+", "+ip)
	} else {
		newReq.Header.Set("X-Forwarded-For", ip)
	}

	result, err, _ := rp.group.Do(cacheKey, func() (interface{}, error) {
		resp, err := rp.client.Do(newReq)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		// TODO: For large responses, consider streaming the response body back to the client while also writing it to cache,
		// 		 instead of reading the entire response body into memory at once.
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}

		return &cache.CacheEntry{
			StatusCode: resp.StatusCode,
			Header:     resp.Header.Clone(),
			Body:       body,
		}, nil
	})

	if err != nil {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}

	// Copy the response headers and body back to the client, stripping any hop-by-hop headers.
	// TODO: We should also consider copying the trailers if the response has any.
	entry := result.(*cache.CacheEntry)

	// Add the response to cache before writing it back to the client.
	// This way, if writing the response back to the client fails, we still have it in cache for future requests.
	rp.cache.Put(cacheKey, *entry)

	// Write the response back to the client
	copyHeaders(w.Header(), entry.Header)
	w.Header().Set("X-Cache", "MISS")
	w.WriteHeader(entry.StatusCode)
	w.Write(entry.Body)
}
