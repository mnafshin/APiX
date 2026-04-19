package engine

import "net/http"

// firstNonEmptyHeader returns the first non-empty http.Header from the candidates.
func firstNonEmptyHeader(candidates ...http.Header) http.Header {
	for _, h := range candidates {
		if len(h) > 0 {
			return h
		}
	}
	return http.Header{}
}
