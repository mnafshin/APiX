package proxy

import (
	"context"

	logging "github.com/mnafshin/apix/internal/logging"
)

func recoverProxyPanic(ctx context.Context, logTag string, onPanic func()) {
	if rec := recover(); rec != nil {
		logging.Errorf(ctx, "%s (recovered): %v", logTag, rec)
		if onPanic != nil {
			onPanic()
		}
	}
}
