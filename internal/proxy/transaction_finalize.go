package proxy

import (
	"context"
	"time"

	"github.com/mnafshin/apix/internal/config"
	logging "github.com/mnafshin/apix/internal/logging"
)

func storeAndObserve(ctx context.Context, cfg *config.Config, engine TrafficEngine, tx *Transaction, start time.Time, storeLogTag string) {
	if tx == nil || tx.Request == nil || tx.Response == nil {
		return
	}

	if engine != nil {
		tx.DurationMs = time.Since(start).Milliseconds()
		if err := engine.StoreTransaction(tx); err != nil {
			logging.Errorf(ctx, "%s: %v", storeLogTag, err)
		}
	}

	observeRequest(
		ctx,
		cfg,
		tx.Request.Method,
		tx.Request.URL.String(),
		tx.Response.StatusCode,
		time.Since(start),
		tx.ID,
		tx.Request.Raw.RemoteAddr,
		len(tx.RequestBody),
		len(tx.ResponseBody),
	)
}
