package replay

import (
	"net/http"
	"strings"
)

// RecordingFilter controls which requests get recorded and which header values
// are redacted before storage.
type RecordingFilter struct {
	// IncludePaths is a list of URL path prefixes to record.
	// An empty slice means "include everything" (before ExcludePaths is applied).
	IncludePaths []string

	// ExcludePaths is a list of URL path prefixes to skip. Takes priority over
	// IncludePaths when both match.
	ExcludePaths []string

	// IncludeHosts limits recording to requests whose Host matches one of the
	// entries. An empty slice means "include all hosts".
	IncludeHosts []string

	// ExcludeHeaders lists header names whose values are replaced with "[REDACTED]"
	// before the transaction is persisted (e.g. "Authorization", "Cookie").
	ExcludeHeaders []string
}

// ShouldRecord returns true when req passes the filter's include/exclude rules.
func (f *RecordingFilter) ShouldRecord(req *http.Request) bool {
	if f == nil {
		return true
	}

	host := req.Host
	if host == "" && req.URL != nil {
		host = req.URL.Hostname()
	}

	// Host filter: if any IncludeHosts are specified the request host must match
	// at least one of them.
	if len(f.IncludeHosts) > 0 {
		matched := false
		for _, h := range f.IncludeHosts {
			if strings.EqualFold(host, h) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	path := "/"
	if req.URL != nil {
		path = req.URL.Path
	}

	// ExcludePaths takes priority: if any prefix matches, skip the request.
	for _, p := range f.ExcludePaths {
		if strings.HasPrefix(path, p) {
			return false
		}
	}

	// IncludePaths: if specified, at least one prefix must match.
	if len(f.IncludePaths) > 0 {
		for _, p := range f.IncludePaths {
			if strings.HasPrefix(path, p) {
				return true
			}
		}
		return false
	}

	return true
}

// RedactHeaders returns a copy of headers with sensitive values replaced by
// "[REDACTED]". The original map is never modified.
func (f *RecordingFilter) RedactHeaders(headers map[string]string) map[string]string {
	if f == nil || len(f.ExcludeHeaders) == 0 {
		// Return a shallow copy so callers always get their own map.
		out := make(map[string]string, len(headers))
		for k, v := range headers {
			out[k] = v
		}
		return out
	}

	redactSet := make(map[string]struct{}, len(f.ExcludeHeaders))
	for _, h := range f.ExcludeHeaders {
		redactSet[strings.ToLower(h)] = struct{}{}
	}

	out := make(map[string]string, len(headers))
	for k, v := range headers {
		if _, redact := redactSet[strings.ToLower(k)]; redact {
			out[k] = "[REDACTED]"
		} else {
			out[k] = v
		}
	}
	return out
}
