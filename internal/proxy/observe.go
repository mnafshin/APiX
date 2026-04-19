package proxy

import (
	"context"
	"time"

	"github.com/mnafshin/apix/internal/config"
	logging "github.com/mnafshin/apix/internal/logging"
	metrics "github.com/mnafshin/apix/internal/metrics"
)

// observeRequest records Prometheus metrics and emits a slowlog warning when the
// request duration exceeds the configured threshold.
func observeRequest(ctx context.Context, cfg *config.Config, method, displayURL string, status int, dur time.Duration) {
	metrics.ObserveRequest(method, status, dur.Seconds())
	if cfg != nil && cfg.SlowlogThresholdMs > 0 && dur.Milliseconds() > int64(cfg.SlowlogThresholdMs) {
		logging.Warnf(ctx, "slow request: method=%s url=%s status=%d duration_ms=%d",
			method, displayURL, status, dur.Milliseconds())
	}
}
