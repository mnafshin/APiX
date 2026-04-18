package httputil

import "net/http"

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
