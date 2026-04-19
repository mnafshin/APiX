package proxy

import (
	"context"
	"net/http"
	"time"

	"github.com/mnafshin/apix/internal/breakpoints"
	apix "github.com/mnafshin/apix/pkg/api/generated"
	"github.com/mnafshin/apix/pkg/plugins"
)

// RewriteRuleProto is a type alias so proxy callers don't need to import the generated package directly.
type RewriteRuleProto = apix.RewriteRule

// ProxyRequest is an alias for the plugins-layer request type.
type ProxyRequest = plugins.ProxyRequest

// ProxyResponse is an alias for the plugins-layer response type.
type ProxyResponse = plugins.ProxyResponse

// TransactionStore is the narrow interface for persisting and publishing traffic.
// Implemented by internal/engine.Engine.
type TransactionStore interface {
	// StoreTransaction persists and publishes a completed HTTP transaction.
	StoreTransaction(tx *Transaction) error
	// StoreWebSocketFrame persists a relayed WebSocket frame.
	StoreWebSocketFrame(frame *WebSocketFrame) error
	// RewriteRules loads the current list of enabled rewrite rules from storage.
	RewriteRules() ([]*RewriteRuleProto, error)
}

// RequestPauser is the narrow interface for breakpoint pause/resume.
// Implemented by internal/engine.Engine.
type RequestPauser interface {
	// PauseRequest holds a request at a breakpoint and blocks until resumed.
	// Returns the (possibly modified) request and the resume action.
	PauseRequest(tx *Transaction) (*Transaction, ResumeAction, error)
}

// TrafficEngine composes all proxy interfaces.
// Implemented by internal/engine.Engine.
type TrafficEngine interface {
	TransactionStore
	RequestPauser
}

// RequestPlugin is implemented by chains that process outbound requests.
// Pass a RequestPlugin to SetPlugins when only request interception is needed.
type RequestPlugin interface {
	// RunRequest applies all request hooks in order.
	RunRequest(ctx context.Context, req *ProxyRequest) (*ProxyRequest, error)
}

// ResponsePlugin is implemented by chains that process responses.
// Pass a ResponsePlugin to SetPlugins when only response interception is needed.
type ResponsePlugin interface {
	// RunResponse applies all response hooks in order.
	RunResponse(ctx context.Context, req *ProxyRequest, resp *ProxyResponse) (*ProxyResponse, error)
}

// PluginChain combines both optional interfaces. Implementations may satisfy
// only RequestPlugin, only ResponsePlugin, or both.
// Kept for backward compatibility — existing runtimes that implement both
// RunRequest and RunResponse continue to satisfy this interface.
type PluginChain interface {
	RequestPlugin
	ResponsePlugin
}

// ResumeAction mirrors the proto enum for the proxy layer.
// It is an alias of breakpoints.ResumeAction — defined once, used everywhere.
type ResumeAction = breakpoints.ResumeAction

const (
	ResumeForward = breakpoints.ActionForward
	ResumeDrop    = breakpoints.ActionDrop
	ResumeRespond = breakpoints.ActionRespond
)

// Transaction groups a request and its (eventual) response.
type Transaction struct {
	ID                     string
	Request                *ProxyRequest
	RequestBody            []byte      // buffered body bytes captured before forwarding
	OriginalRequestHeaders http.Header // original headers before plugins modified them
	Response               *ProxyResponse
	ResponseBody           []byte // buffered response body bytes captured after forwarding
	DurationMs             int64
}

// WebSocketFrame represents a proxied WebSocket frame/message.
type WebSocketFrame struct {
	TransactionID string
	Direction     string
	Opcode        int
	Payload       []byte
	Timestamp     time.Time
}
