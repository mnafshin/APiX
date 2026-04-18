package httputil

import (
	"fmt"
	"io"
)

// ReadLimitedBody reads r up to maxBytes. If r is nil, returns nil, nil.
// If the body exceeds maxBytes, returns an error.
func ReadLimitedBody(r io.ReadCloser, maxBytes int64) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	// Read up to maxBytes+1 to detect an oversize body without reading it all.
	lr := io.LimitReader(r, maxBytes+1)
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("body exceeds limit of %d bytes", maxBytes)
	}
	return data, nil
}
