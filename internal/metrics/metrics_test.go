package metrics

import (
	"sync/atomic"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestInitRegistersCollectors(t *testing.T) {
	// Ensure a clean state for the default registry.
	atomic.StoreInt32(&enabled, 0)
	if requestsTotal != nil {
		prometheus.Unregister(requestsTotal)
	}
	if requestDuration != nil {
		prometheus.Unregister(requestDuration)
	}
	if activeConnections != nil {
		prometheus.Unregister(activeConnections)
	}
	requestsTotal = nil
	requestDuration = nil
	activeConnections = nil

	// Initialize metrics and assert registration.
	Init(true)
	if atomic.LoadInt32(&enabled) != 1 {
		t.Fatalf("metrics.Init did not enable metrics")
	}

	// Exercise the vector metrics so they appear in the default registry (CounterVec/HistogramVec
	// do not expose metric families until a label combination is created).
	ObserveRequest("GET", 200, 0.123)

	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("prometheus gather failed: %v", err)
	}

	var foundReq, foundDur, foundActive bool
	for _, mf := range mfs {
		switch mf.GetName() {
		case "apix_requests_total":
			foundReq = true
		case "apix_request_duration_seconds":
			foundDur = true
		case "apix_active_connections":
			foundActive = true
		}
	}

	if !foundReq || !foundDur || !foundActive {
		t.Fatalf("expected metrics registered: requests=%v duration=%v active=%v", foundReq, foundDur, foundActive)
	}

	// Cleanup so other tests aren't affected.
	prometheus.Unregister(requestsTotal)
	prometheus.Unregister(requestDuration)
	prometheus.Unregister(activeConnections)
	atomic.StoreInt32(&enabled, 0)
	requestsTotal = nil
	requestDuration = nil
	activeConnections = nil
}
