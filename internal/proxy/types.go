package proxy

import (
	"context"

	"github.com/mnafshin/apix/internal/breakpoints"
	"github.com/mnafshin/apix/pkg/plugins"
)

// ProxyRequest is an alias for the plugins-layer request type.
type ProxyRequest = plugins.ProxyRequest

// ProxyResponse is an alias for the plugins-layer response type.
type ProxyResponse = plugins.ProxyResponse

// TrafficEngine is the interface the proxy uses to store and publish traffic.
// Implemented by internal/engine.Engine.
type TrafficEngine interface {
	// StoreTransaction persists and publishes a completed HTTP transaction.
	StoreTransaction(tx *Transaction) error
	// PauseRequest holds a request at a breakpoint and blocks until resumed.
	// Returns the (possibly modified) request and the resume action.
	PauseRequest(tx *Transaction) (*Transaction, ResumeAction, error)
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
	ID           string
	Request      *ProxyRequest
	RequestBody  []byte // buffered body bytes captured before forwarding
	Response     *ProxyResponse
	ResponseBody []byte // buffered response body bytes captured after forwarding
	DurationMs   int64
}
