package builtins

import (
	"bytes"
	"container/list"
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/mnafshin/apix/pkg/plugins"
)

// CachingConfig holds the configuration for the CachingPlugin.
type CachingConfig struct {
	// Capacity is the maximum number of entries held in the cache (default: 1000).
	Capacity int
	// TTL is how long a cached entry remains valid (default: 5 minutes).
	TTL time.Duration
	// CacheMethods lists the HTTP methods whose responses are cached (default: GET, HEAD).
	CacheMethods []string
}

type cachedResponse struct {
	StatusCode int
	Status     string
	Headers    http.Header
	Body       []byte
}

type cacheEntry struct {
	key      string
	response *cachedResponse
	expiry   time.Time
}

// CachingPlugin is an in-memory LRU cache with TTL support.
// On a cache hit, OnRequest sets MockedResponse so the proxy skips the upstream.
// On a cache miss, OnResponse stores the 2xx response for future requests.
type CachingPlugin struct {
	cfg     CachingConfig
	mu      sync.Mutex
	entries map[string]*list.Element // cache key → LRU element
	lru     *list.List               // front = most-recently used
}

// NewCachingPlugin creates a CachingPlugin with the given configuration.
func NewCachingPlugin(cfg CachingConfig) *CachingPlugin {
	return &CachingPlugin{cfg: cfg}
}

func (p *CachingPlugin) Name() string        { return "caching" }
func (p *CachingPlugin) Version() string     { return "1.0.0" }
func (p *CachingPlugin) Description() string { return "In-memory LRU cache with TTL for proxied responses." }

func (p *CachingPlugin) capacity() int {
	if p.cfg.Capacity <= 0 {
		return 1000
	}
	return p.cfg.Capacity
}

func (p *CachingPlugin) ttl() time.Duration {
	if p.cfg.TTL <= 0 {
		return 5 * time.Minute
	}
	return p.cfg.TTL
}

func (p *CachingPlugin) cacheMethods() []string {
	if len(p.cfg.CacheMethods) == 0 {
		return []string{"GET", "HEAD"}
	}
	return p.cfg.CacheMethods
}

func (p *CachingPlugin) methodAllowed(method string) bool {
	for _, m := range p.cacheMethods() {
		if m == method {
			return true
		}
	}
	return false
}

func (p *CachingPlugin) init() {
	if p.entries == nil {
		p.entries = make(map[string]*list.Element)
		p.lru = list.New()
	}
}

func cacheKey(req *plugins.ProxyRequest) string {
	return req.Method + ":" + req.URL.String()
}

func (p *CachingPlugin) OnRequest(ctx context.Context, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
	if !p.methodAllowed(req.Method) {
		return nil, nil
	}

	key := cacheKey(req)

	p.mu.Lock()
	p.init()
	elem, ok := p.entries[key]
	if ok {
		entry := elem.Value.(*cacheEntry)
		if time.Now().After(entry.expiry) {
			// Expired — evict and let the request through.
			p.lru.Remove(elem)
			delete(p.entries, key)
			p.mu.Unlock()
			return nil, nil
		}
		// Cache hit — move to front and serve the cached response.
		p.lru.MoveToFront(elem)
		cached := entry.response
		p.mu.Unlock()

		clone := req.Clone(req.Body)
		clone.MockedResponse = &plugins.ProxyResponse{
			StatusCode: cached.StatusCode,
			Status:     cached.Status,
			Headers:    cached.Headers.Clone(),
			Body:       io.NopCloser(bytes.NewReader(cached.Body)),
		}
		return clone, nil
	}
	p.mu.Unlock()
	return nil, nil
}

func (p *CachingPlugin) OnResponse(ctx context.Context, req *plugins.ProxyRequest, resp *plugins.ProxyResponse) (*plugins.ProxyResponse, error) {
	if !p.methodAllowed(req.Method) {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	key := cacheKey(req)
	entry := &cacheEntry{
		key: key,
		response: &cachedResponse{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Headers:    resp.Headers.Clone(),
			Body:       body,
		},
		expiry: time.Now().Add(p.ttl()),
	}

	p.mu.Lock()
	p.init()
	if elem, ok := p.entries[key]; ok {
		// Update existing entry.
		p.lru.MoveToFront(elem)
		elem.Value = entry
	} else {
		// Evict LRU if at capacity.
		if p.lru.Len() >= p.capacity() {
			oldest := p.lru.Back()
			if oldest != nil {
				p.lru.Remove(oldest)
				delete(p.entries, oldest.Value.(*cacheEntry).key)
			}
		}
		elem := p.lru.PushFront(entry)
		p.entries[key] = elem
	}
	p.mu.Unlock()

	// Return response with re-readable body.
	clone := resp.Clone(io.NopCloser(bytes.NewReader(body)))
	return clone, nil
}
