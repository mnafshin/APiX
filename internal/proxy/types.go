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

// TrafficEngine is the interface the proxy uses to store and publish traffic.
// Implemented by internal/engine.Engine.
type TrafficEngine interface {
	// StoreTransaction persists and publishes a completed HTTP transaction.
	StoreTransaction(tx *Transaction) error
	// StoreWebSocketFrame persists a relayed WebSocket frame.
	StoreWebSocketFrame(frame *WebSocketFrame) error
	// PauseRequest holds a request at a breakpoint and blocks until resumed.
	// Returns the (possibly modified) request and the resume action.
	PauseRequest(tx *Transaction) (*Transaction, ResumeAction, error)
	// RewriteRules loads the current list of enabled rewrite rules from storage.
	RewriteRules() ([]*RewriteRuleProto, error)
}

// PluginChain is an ordered list of plugins applied to each request/response.
type PluginChain interface {
	// RunRequest applies all plugins' OnRequest hooks in order.
	RunRequest(ctx context.Context, req *ProxyRequest) (*ProxyRequest, error)
	// RunResponse applies all plugins' OnResponse hooks in order.
	RunResponse(ctx context.Context, req *ProxyRequest, resp *ProxyResponse) (*ProxyResponse, error)
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
