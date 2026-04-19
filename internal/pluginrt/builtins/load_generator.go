package builtins

import (
	"bytes"
	"context"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mnafshin/apix/pkg/plugins"
)

// LoadGeneratorConfig holds the full configuration for the LoadGenerator plugin.
type LoadGeneratorConfig struct {
	// MatchPath is a path substring to trigger on; empty matches all paths.
	MatchPath string
	// Concurrency is the number of concurrent workers (default 10).
	Concurrency int
	// TotalReqs is the total number of requests to fire; 0 means unlimited until Stop is called.
	TotalReqs int
	// RatePerSec is the target request rate per second; 0 means unlimited.
	RatePerSec float64
	// Passthrough, when true, also forwards the original request upstream.
	Passthrough bool
}

// LoadResult holds the outcome of a single load-generated request.
type LoadResult struct {
	StatusCode int
	LatencyMs  int64
	Error      string
}

// LoadGeneratorStats summarises a completed or in-progress load run.
type LoadGeneratorStats struct {
	Total    int64
	Success  int64
	Errors   int64
	AvgLatMs float64
	P99LatMs float64
}

// LoadGenerator is an APiX plugin that fires concurrent copies of a captured
// request to stress-test the upstream target.
type LoadGenerator struct {
	cfg     LoadGeneratorConfig
	mu      sync.Mutex
	results []LoadResult
	running bool
	cancel  context.CancelFunc
}

// NewLoadGenerator returns a LoadGenerator with the given config.
func NewLoadGenerator(cfg LoadGeneratorConfig) *LoadGenerator {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 10
	}
	return &LoadGenerator{cfg: cfg}
}

func (p *LoadGenerator) Name() string        { return "load-generator" }
func (p *LoadGenerator) Version() string     { return "1.0.0" }
func (p *LoadGenerator) Description() string { return "Concurrent load/stress test from captured requests." }

// Stats returns a snapshot of the current run's results.
func (p *LoadGenerator) Stats() LoadGeneratorStats {
	p.mu.Lock()
	snapshot := make([]LoadResult, len(p.results))
	copy(snapshot, p.results)
	p.mu.Unlock()

	var s LoadGeneratorStats
	s.Total = int64(len(snapshot))
	lats := make([]float64, 0, len(snapshot))
	var sumLat float64
	for _, r := range snapshot {
		if r.Error == "" {
			s.Success++
			lats = append(lats, float64(r.LatencyMs))
			sumLat += float64(r.LatencyMs)
		} else {
			s.Errors++
		}
	}
	if len(lats) > 0 {
		s.AvgLatMs = sumLat / float64(len(lats))
		sort.Float64s(lats)
		idx := int(math.Ceil(float64(len(lats))*0.99)) - 1
		if idx < 0 {
			idx = 0
		}
		s.P99LatMs = lats[idx]
	}
	return s
}

// Stop cancels an in-progress load run. Safe to call when not running.
func (p *LoadGenerator) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.running = false
}

// OnRequest triggers load generation when the path matches and req.Raw is set.
func (p *LoadGenerator) OnRequest(ctx context.Context, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
	if req.Raw == nil {
		return nil, nil
	}
	if p.cfg.MatchPath != "" && !strings.Contains(req.URL.Path, p.cfg.MatchPath) {
		return nil, nil
	}

	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return nil, nil
	}
	p.running = true
	runCtx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.results = nil
	p.mu.Unlock()

	// Snapshot the raw request body so workers can clone it.
	rawBody, _ := io.ReadAll(req.Raw.Body)
	req.Raw.Body = io.NopCloser(bytes.NewReader(rawBody))

	go p.runLoad(runCtx, req.Raw, rawBody)

	if p.cfg.Passthrough {
		return nil, nil
	}

	clone := req.Clone(req.Body)
	clone.MockedResponse = &plugins.ProxyResponse{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Headers:    make(http.Header),
		Body:       io.NopCloser(strings.NewReader("load test running")),
	}
	return clone, nil
}

// OnResponse is a no-op; all logic lives in OnRequest.
func (p *LoadGenerator) OnResponse(_ context.Context, _ *plugins.ProxyRequest, _ *plugins.ProxyResponse) (*plugins.ProxyResponse, error) {
	return nil, nil
}

// runLoad dispatches Concurrency workers that send clones of raw.
func (p *LoadGenerator) runLoad(ctx context.Context, raw *http.Request, body []byte) {
	defer func() {
		p.mu.Lock()
		p.running = false
		p.mu.Unlock()
	}()

	concurrency := p.cfg.Concurrency
	total := p.cfg.TotalReqs

	// counter shared across workers when TotalReqs > 0.
	var counter int64

	var ticker *time.Ticker
	var tickCh <-chan time.Time
	if p.cfg.RatePerSec > 0 {
		interval := time.Duration(float64(time.Second) / p.cfg.RatePerSec)
		ticker = time.NewTicker(interval)
		tickCh = ticker.C
		defer ticker.Stop()
	}

	// jobCh feeds tasks to workers.
	jobCh := make(chan struct{}, concurrency)

	var wg sync.WaitGroup
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			for range jobCh {
				p.doRequest(raw, body)
			}
		}()
	}

	for {
		select {
		case <-ctx.Done():
			close(jobCh)
			wg.Wait()
			return
		default:
		}

		if total > 0 {
			n := atomic.AddInt64(&counter, 1)
			if n > int64(total) {
				close(jobCh)
				wg.Wait()
				return
			}
		}

		if tickCh != nil {
			select {
			case <-ctx.Done():
				close(jobCh)
				wg.Wait()
				return
			case <-tickCh:
			}
		}

		select {
		case <-ctx.Done():
			close(jobCh)
			wg.Wait()
			return
		case jobCh <- struct{}{}:
		}
	}
}

// doRequest fires a single cloned HTTP request and records the result.
func (p *LoadGenerator) doRequest(raw *http.Request, body []byte) {
	clone := raw.Clone(context.Background())
	clone.Body = io.NopCloser(bytes.NewReader(body))

	start := time.Now()
	resp, err := http.DefaultClient.Do(clone) //nolint:gosec
	latMs := time.Since(start).Milliseconds()

	result := LoadResult{LatencyMs: latMs}
	if err != nil {
		result.Error = err.Error()
	} else {
		result.StatusCode = resp.StatusCode
		io.Copy(io.Discard, resp.Body) //nolint:errcheck
		_ = resp.Body.Close()
	}

	p.mu.Lock()
	p.results = append(p.results, result)
	p.mu.Unlock()
}
