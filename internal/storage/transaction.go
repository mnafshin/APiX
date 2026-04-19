package storage

// Transaction pairs a stored request with its optional response.
// It is a lightweight view type used by callers that need both records together.
type Transaction struct {
	Request  *RequestRecord  // always non-nil for a stored transaction
	Response *ResponseRecord // nil if no response has been stored yet
}
