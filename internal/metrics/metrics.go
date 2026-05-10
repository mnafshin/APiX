package metrics

import (
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var enabled int32

var (
	requestsTotal              *prometheus.CounterVec
	errorsTotal                *prometheus.CounterVec
	requestDuration            *prometheus.HistogramVec
	requestDurationSummary     *prometheus.SummaryVec
	breakpointsHitTotal        prometheus.Counter
	breakpointPauseDuration    prometheus.Histogram
	pluginExecutionDuration    *prometheus.HistogramVec
	pluginErrorsTotal          *prometheus.CounterVec
	webSocketFramesTotal       *prometheus.CounterVec
	webSocketConnectionsActive prometheus.Gauge
	activeConnections          prometheus.Gauge
)

// Init enables metrics collection and registers Prometheus collectors when
// enabledFlag is true. Calling Init(false) disables metrics (no-op).
func Init(enabledFlag bool) {
	if !enabledFlag {
		atomic.StoreInt32(&enabled, 0)
		return
	}
	if atomic.LoadInt32(&enabled) == 1 {
		return
	}
	requestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "apix",
		Name:      "requests_total",
		Help:      "Total number of proxied requests",
	}, []string{"method", "status_class"})

	requestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "apix",
		Name:      "request_duration_seconds",
		Help:      "Request duration in seconds",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "status_class"})

	requestDurationSummary = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Namespace:  "apix",
		Name:       "request_duration_summary_seconds",
		Help:       "Request duration summary in seconds (quantiles: p50,p90,p95,p99)",
		Objectives: map[float64]float64{0.50: 0.01, 0.90: 0.01, 0.95: 0.005, 0.99: 0.001},
	}, []string{"method"})

	errorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "apix",
		Name:      "errors_total",
		Help:      "Total number of proxied requests resulting in 4xx/5xx",
	}, []string{"method", "status_class"})

	breakpointsHitTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "apix",
		Name:      "breakpoints_hit_total",
		Help:      "Total number of breakpoint pauses triggered",
	})

	breakpointPauseDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "apix",
		Name:      "breakpoints_pause_duration_seconds",
		Help:      "Time spent waiting at breakpoints in seconds",
		Buckets:   prometheus.DefBuckets,
	})

	pluginExecutionDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "apix",
		Name:      "plugin_execution_duration_seconds",
		Help:      "Plugin execution duration in seconds",
		Buckets:   prometheus.DefBuckets,
	}, []string{"plugin_name", "phase"})

	pluginErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "apix",
		Name:      "plugin_errors_total",
		Help:      "Total number of plugin execution errors",
	}, []string{"plugin_name", "phase"})

	webSocketFramesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "apix",
		Name:      "websocket_frames_total",
		Help:      "Total number of proxied websocket frames",
	}, []string{"direction"})

	webSocketConnectionsActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "apix",
		Name:      "websocket_connections_active",
		Help:      "Number of active proxied websocket connections",
	})

	activeConnections = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "apix",
		Name:      "active_connections",
		Help:      "Number of active proxied connections",
	})

	prometheus.MustRegister(
		requestsTotal,
		errorsTotal,
		requestDuration,
		requestDurationSummary,
		breakpointsHitTotal,
		breakpointPauseDuration,
		pluginExecutionDuration,
		pluginErrorsTotal,
		webSocketFramesTotal,
		webSocketConnectionsActive,
		activeConnections,
	)
	atomic.StoreInt32(&enabled, 1)
}

// Handler returns the Prometheus HTTP handler (promhttp).
func Handler() http.Handler { return promhttp.Handler() }

// IncActive increments the active connections gauge (no-op if metrics disabled).
func IncActive() {
	if atomic.LoadInt32(&enabled) == 1 {
		activeConnections.Inc()
	}
}

// DecActive decrements the active connections gauge (no-op if metrics disabled).
func DecActive() {
	if atomic.LoadInt32(&enabled) == 1 {
		activeConnections.Dec()
	}
}

// ObserveRequest records a single request metric (method, status class, duration).
func ObserveRequest(method string, statusCode int, durationSec float64) {
	if atomic.LoadInt32(&enabled) != 1 {
		return
	}
	statusClass := fmt.Sprintf("%dxx", statusCode/100)
	requestsTotal.WithLabelValues(method, statusClass).Inc()
	requestDuration.WithLabelValues(method, statusClass).Observe(durationSec)
	requestDurationSummary.WithLabelValues(method).Observe(durationSec)
	if statusCode >= 400 {
		errorsTotal.WithLabelValues(method, statusClass).Inc()
	}
}

// ObserveBreakpointPause records a breakpoint hit and pause duration.
func ObserveBreakpointPause(durationSec float64) {
	if atomic.LoadInt32(&enabled) != 1 {
		return
	}
	breakpointsHitTotal.Inc()
	breakpointPauseDuration.Observe(durationSec)
}

// ObservePluginExecution records plugin execution latency and optional errors.
func ObservePluginExecution(pluginName, phase string, durationSec float64, hadError bool) {
	if atomic.LoadInt32(&enabled) != 1 {
		return
	}
	pluginExecutionDuration.WithLabelValues(pluginName, phase).Observe(durationSec)
	if hadError {
		pluginErrorsTotal.WithLabelValues(pluginName, phase).Inc()
	}
}

// IncWebSocketFrame increments websocket frame counter by direction.
func IncWebSocketFrame(direction string) {
	if atomic.LoadInt32(&enabled) != 1 {
		return
	}
	webSocketFramesTotal.WithLabelValues(direction).Inc()
}

// IncWebSocketActive increments active websocket connection gauge.
func IncWebSocketActive() {
	if atomic.LoadInt32(&enabled) != 1 {
		return
	}
	webSocketConnectionsActive.Inc()
}

// DecWebSocketActive decrements active websocket connection gauge.
func DecWebSocketActive() {
	if atomic.LoadInt32(&enabled) != 1 {
		return
	}
	webSocketConnectionsActive.Dec()
}
