package builtins

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mnafshin/apix/pkg/plugins"
)

// makeReqWithRaw builds a ProxyRequest that also carries a populated Raw field.
func makeReqWithRaw(method, rawURL, body string) *plugins.ProxyRequest {
	req := makeReq(method, rawURL, body)
	httpReq, _ := http.NewRequest(method, rawURL, strings.NewReader(body))
	req.Raw = httpReq
	return req
}

func TestLoadGenerator_StatsZeroInitially(t *testing.T) {
	t.Parallel()
	p := NewLoadGenerator(LoadGeneratorConfig{})
	s := p.Stats()
	if s.Total != 0 || s.Success != 0 || s.Errors != 0 {
		t.Errorf("expected zero stats, got %+v", s)
	}
}

func TestLoadGenerator_StopNotRunning(t *testing.T) {
	t.Parallel()
	p := NewLoadGenerator(LoadGeneratorConfig{})
	// Must not panic.
	p.Stop()
}

func TestLoadGenerator_PassthroughDoesNotSetMockedResponse(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewLoadGenerator(LoadGeneratorConfig{
		Passthrough: true,
		TotalReqs:   1,
		Concurrency: 1,
	})
	req := makeReqWithRaw("GET", srv.URL+"/test", "")
	result, err := p.OnRequest(context.Background(), req)
	p.Stop()
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	// Passthrough=true: plugin returns nil so original request is forwarded.
	if result != nil && result.MockedResponse != nil {
		t.Error("Passthrough=true must not set MockedResponse")
	}
}

func TestLoadGenerator_NonMatchingPathReturnsNil(t *testing.T) {
	t.Parallel()
	p := NewLoadGenerator(LoadGeneratorConfig{
		MatchPath: "/api/stress",
	})
	req := makeReqWithRaw("GET", "https://example.com/health", "")
	result, err := p.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result != nil {
		t.Error("non-matching path should return nil")
	}
}

func TestLoadGenerator_NilRawReturnsNil(t *testing.T) {
	t.Parallel()
	p := NewLoadGenerator(LoadGeneratorConfig{})
	req := makeReq("GET", "https://example.com/", "")
	// req.Raw is nil
	result, err := p.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result != nil {
		t.Error("nil Raw should return nil (no load fired)")
	}
}

func TestLoadGenerator_MockedResponseSetWhenNotPassthrough(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewLoadGenerator(LoadGeneratorConfig{
		Passthrough: false,
		TotalReqs:   1,
		Concurrency: 1,
	})
	req := makeReqWithRaw("GET", srv.URL+"/", "")
	result, err := p.OnRequest(context.Background(), req)
	p.Stop()
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result == nil {
		t.Fatal("expected modified request, got nil")
	}
	if result.MockedResponse == nil {
		t.Fatal("expected MockedResponse to be set when Passthrough=false")
	}
	if result.MockedResponse.StatusCode != http.StatusOK {
		t.Errorf("MockedResponse.StatusCode: got %d want 200", result.MockedResponse.StatusCode)
	}
	body, _ := io.ReadAll(result.MockedResponse.Body)
	if string(body) != "load test running" {
		t.Errorf("MockedResponse body: got %q want %q", string(body), "load test running")
	}
}

func TestLoadGenerator_FiresRequests(t *testing.T) {
	t.Parallel()
	var count int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	total := 5
	p := NewLoadGenerator(LoadGeneratorConfig{
		TotalReqs:   total,
		Concurrency: 2,
	})
	req := makeReqWithRaw("GET", srv.URL+"/load", "")
	_, _ = p.OnRequest(context.Background(), req)

	// Wait for the run to finish (up to 5s).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s := p.Stats()
		if s.Total >= int64(total) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	p.Stop()

	s := p.Stats()
	if s.Total < int64(total) {
		t.Errorf("expected at least %d requests fired, got %d", total, s.Total)
	}
	if s.Success == 0 {
		t.Error("expected at least one successful request")
	}
}

func TestLoadGenerator_OnResponsePassThrough(t *testing.T) {
	t.Parallel()
	p := NewLoadGenerator(LoadGeneratorConfig{})
	req := makeReq("GET", "https://example.com/", "")
	resp := makeResp(200, "ok")
	result, err := p.OnResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("OnResponse: %v", err)
	}
	if result != nil {
		t.Error("OnResponse should always return nil")
	}
}


