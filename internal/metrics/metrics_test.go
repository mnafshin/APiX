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
	if requestDurationSummary != nil {
		prometheus.Unregister(requestDurationSummary)
	}
	if errorsTotal != nil {
		prometheus.Unregister(errorsTotal)
	}
	if breakpointsHitTotal != nil {
		prometheus.Unregister(breakpointsHitTotal)
	}
	if breakpointPauseDuration != nil {
		prometheus.Unregister(breakpointPauseDuration)
	}
	if pluginExecutionDuration != nil {
		prometheus.Unregister(pluginExecutionDuration)
	}
	if pluginErrorsTotal != nil {
		prometheus.Unregister(pluginErrorsTotal)
	}
	if webSocketFramesTotal != nil {
		prometheus.Unregister(webSocketFramesTotal)
	}
	if webSocketConnectionsActive != nil {
		prometheus.Unregister(webSocketConnectionsActive)
	}
	if activeConnections != nil {
		prometheus.Unregister(activeConnections)
	}
	requestsTotal = nil
	requestDuration = nil
	requestDurationSummary = nil
	errorsTotal = nil
	breakpointsHitTotal = nil
	breakpointPauseDuration = nil
	pluginExecutionDuration = nil
	pluginErrorsTotal = nil
	webSocketFramesTotal = nil
	webSocketConnectionsActive = nil
	activeConnections = nil

	// Initialize metrics and assert registration.
	Init(true)
	if atomic.LoadInt32(&enabled) != 1 {
		t.Fatalf("metrics.Init did not enable metrics")
	}

	// Exercise the vector metrics so they appear in the default registry (CounterVec/HistogramVec
	// do not expose metric families until a label combination is created).
	ObserveRequest("GET", 200, 0.123)
	ObserveRequest("POST", 500, 0.456)
	ObserveBreakpointPause(0.2)
	ObservePluginExecution("mock_response", "request", 0.03, false)
	ObservePluginExecution("mock_response", "response", 0.02, true)
	IncWebSocketFrame("client")
	IncWebSocketActive()
	DecWebSocketActive()

	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("prometheus gather failed: %v", err)
	}

	var foundReq, foundErr, foundDur, foundDurSummary, foundActive bool
	var foundBreakpointHits, foundBreakpointPause, foundPluginDur, foundPluginErr bool
	var foundWSFrames, foundWSActive bool
	for _, mf := range mfs {
		switch mf.GetName() {
		case "apix_requests_total":
			foundReq = true
		case "apix_errors_total":
			foundErr = true
		case "apix_request_duration_seconds":
			foundDur = true
		case "apix_request_duration_summary_seconds":
			foundDurSummary = true
		case "apix_breakpoints_hit_total":
			foundBreakpointHits = true
		case "apix_breakpoints_pause_duration_seconds":
			foundBreakpointPause = true
		case "apix_plugin_execution_duration_seconds":
			foundPluginDur = true
		case "apix_plugin_errors_total":
			foundPluginErr = true
		case "apix_websocket_frames_total":
			foundWSFrames = true
		case "apix_websocket_connections_active":
			foundWSActive = true
		case "apix_active_connections":
			foundActive = true
		}
	}

	if !foundReq || !foundErr || !foundDur || !foundDurSummary || !foundBreakpointHits || !foundBreakpointPause || !foundPluginDur || !foundPluginErr || !foundWSFrames || !foundWSActive || !foundActive {
		t.Fatalf("expected metrics registered: req=%v err=%v dur=%v summary=%v bpHits=%v bpPause=%v pluginDur=%v pluginErr=%v wsFrames=%v wsActive=%v active=%v",
			foundReq, foundErr, foundDur, foundDurSummary, foundBreakpointHits, foundBreakpointPause, foundPluginDur, foundPluginErr, foundWSFrames, foundWSActive, foundActive)
	}

	// Cleanup so other tests aren't affected.
	prometheus.Unregister(requestsTotal)
	prometheus.Unregister(errorsTotal)
	prometheus.Unregister(requestDuration)
	prometheus.Unregister(requestDurationSummary)
	prometheus.Unregister(breakpointsHitTotal)
	prometheus.Unregister(breakpointPauseDuration)
	prometheus.Unregister(pluginExecutionDuration)
	prometheus.Unregister(pluginErrorsTotal)
	prometheus.Unregister(webSocketFramesTotal)
	prometheus.Unregister(webSocketConnectionsActive)
	prometheus.Unregister(activeConnections)
	atomic.StoreInt32(&enabled, 0)
	requestsTotal = nil
	requestDuration = nil
	requestDurationSummary = nil
	errorsTotal = nil
	breakpointsHitTotal = nil
	breakpointPauseDuration = nil
	pluginExecutionDuration = nil
	pluginErrorsTotal = nil
	webSocketFramesTotal = nil
	webSocketConnectionsActive = nil
	activeConnections = nil
}
