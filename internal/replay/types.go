package replay

import "net/http"

// ReplayRequest describes a request to replay plus optional modifications.
type ReplayRequest struct {
	// Exactly one of RequestID or RawRequest must be set.
	RequestID  string        // replay from history (storage lookup)
	RawRequest *http.Request // replay an arbitrary request directly

	OverrideHeaders map[string]string
	OverrideBody    []byte
	OverrideMethod  string // if non-empty, replaces the request method
	FollowRedirects bool
}
