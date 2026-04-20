package proxy

import (
	"fmt"
	"net/http"

	"github.com/mnafshin/apix/internal/config"
)

func validateInboundRequest(cfg *config.Config, req *http.Request) error {
	if cfg == nil || req == nil {
		return nil
	}

	urlLen := len(req.URL.String())
	if cfg.MaxURLLength > 0 && urlLen > cfg.MaxURLLength {
		return fmt.Errorf("url too long: %d bytes (max %d)", urlLen, cfg.MaxURLLength)
	}

	headerFields := 0
	totalHeaderBytes := 0
	for key, values := range req.Header {
		headerFields += len(values)
		for _, value := range values {
			if cfg.MaxHeaderValueBytes > 0 && len(value) > cfg.MaxHeaderValueBytes {
				return fmt.Errorf("header %q value too large: %d bytes (max %d)", key, len(value), cfg.MaxHeaderValueBytes)
			}
			totalHeaderBytes += len(key) + len(value)
			if cfg.MaxTotalHeaderBytes > 0 && totalHeaderBytes > cfg.MaxTotalHeaderBytes {
				return fmt.Errorf("total headers too large: %d bytes (max %d)", totalHeaderBytes, cfg.MaxTotalHeaderBytes)
			}
		}
	}
	if cfg.MaxHeadersPerRequest > 0 && headerFields > cfg.MaxHeadersPerRequest {
		return fmt.Errorf("too many header fields: %d (max %d)", headerFields, cfg.MaxHeadersPerRequest)
	}

	return nil
}
