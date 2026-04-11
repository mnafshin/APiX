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
	requestsTotal     *prometheus.CounterVec
	requestDuration   *prometheus.HistogramVec
	activeConnections prometheus.Gauge
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

	activeConnections = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "apix",
		Name:      "active_connections",
		Help:      "Number of active proxied connections",
	})

	prometheus.MustRegister(requestsTotal, requestDuration, activeConnections)
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
}
