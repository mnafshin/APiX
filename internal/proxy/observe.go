package proxy

import (
	"context"
	"net"
	"time"

	"github.com/mnafshin/apix/internal/config"
	logging "github.com/mnafshin/apix/internal/logging"
	metrics "github.com/mnafshin/apix/internal/metrics"
)

// observeRequest records Prometheus metrics and emits a slowlog warning when the
// request duration exceeds the configured threshold.
func observeRequest(
	ctx context.Context,
	cfg *config.Config,
	method, displayURL string,
	status int,
	dur time.Duration,
	requestID, clientAddr string,
	requestSize, responseSize int,
) {
	metrics.ObserveRequest(method, status, dur.Seconds())
	if cfg != nil && cfg.SlowlogThresholdMs > 0 && dur.Milliseconds() > int64(cfg.SlowlogThresholdMs) {
		logging.Warnf(ctx, "slow request: method=%s url=%s status=%d duration_ms=%d",
			method, displayURL, status, dur.Milliseconds())
	}
	if cfg != nil {
		if err := logging.EmitAccessLog(ctx, logging.AccessLogConfig{
			Enabled: cfg.AccessLogEnabled,
			Format:  cfg.AccessLogFormat,
			Path:    cfg.AccessLogPath,
		}, logging.AccessLogEntry{
			Timestamp:    time.Now().UTC(),
			Method:       method,
			URL:          displayURL,
			Status:       status,
			DurationMs:   dur.Milliseconds(),
			RequestID:    requestID,
			ClientIP:     clientIP(clientAddr),
			RequestSize:  requestSize,
			ResponseSize: responseSize,
		}); err != nil {
			logging.Warnf(ctx, "access log write failed: %v", err)
		}
	}
}

func clientIP(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		return host
	}
	return addr
}
