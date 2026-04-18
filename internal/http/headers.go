package httputil

import (
	"context"
	"net/http"

	"github.com/mnafshin/apix/internal/logging"
)

// HeadersToMap converts an http.Header (multi-value) to a flat map[string]string,
// keeping only the first value for each header name.
func HeadersToMap(h http.Header) map[string]string {
	m := make(map[string]string, len(h))
	for k, vv := range h {
		if len(vv) > 0 {
			m[k] = vv[0]
		}
	}
	return m
}

// SetValidHeaders iterates over src (a map of header name → value), validates each
// key/value pair using CanonicalHeader and IsValidHeaderValue, and sets accepted
// headers on dst. Invalid entries are logged as warnings with the provided logTag.
func SetValidHeaders(ctx context.Context, dst http.Header, src map[string]string, logTag string) {
	for k, v := range src {
		if cn, ok := CanonicalHeader(k); ok {
			if IsValidHeaderValue(v) {
				dst.Set(cn, v)
			} else {
				logging.Warnf(ctx, "%s: skipped invalid header value for %q", logTag, k)
			}
		} else {
			logging.Warnf(ctx, "%s: skipped invalid header name %q", logTag, k)
		}
	}
}
