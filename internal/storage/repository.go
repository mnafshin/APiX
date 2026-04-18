package storage

import apix "github.com/mnafshin/apix/pkg/api/generated"

// TransactionRepository is the persistence interface used by the engine core.
// Only methods actually called by internal/engine/engine.go are included here.
// The gRPC server layer uses *storage.DB directly for the full API.
type TransactionRepository interface {
	SaveRequest(r *RequestRecord) error
	SaveResponse(r *ResponseRecord) error
	SaveWebSocketFrame(frame *WebSocketFrameRecord) error
	ListRewriteRules() ([]*apix.RewriteRule, error)
}
