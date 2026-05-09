package proxy

import (
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var hopByHopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Transfer-Encoding",
	"Upgrade",
	"Proxy-Authorization",
	"Proxy-Connection",
	"Te",
	"Trailers",
}

// Check if a header is in the hop-by-hop map.
func isHopByHop(header string) bool {
	for _, h := range hopByHopHeaders {
		if strings.EqualFold(h, header) {
			return true
		}
	}

	return false
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
}

// New creates a new ReverseProxy with the specified origins and a default HTTP client with a 30 second timeout and no redirects.
func New(origins []string) *ReverseProxy {
	return &ReverseProxy{
		origins: origins,
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
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
	if len(rp.origins) < 1 {
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

	resp, err := rp.client.Do(newReq)
<<<<<<< HEAD
=======
	defer resp.Body.Close()
>>>>>>> ae9665efaac5e353a16af20eadcc939384837f2e
	if err != nil {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
=======
>>>>>>> ae9665efaac5e353a16af20eadcc939384837f2e

	// Copy the response headers and body back to the client, stripping any hop-by-hop headers.
	// TODO: We should also consider copying the trailers if the response has any.
	// TODO: When caching is implemented, we should check for cacheable responses and store them in the cache before writing back to the client.
	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
