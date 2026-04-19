package server

import (
	"github.com/mnafshin/apix/internal/breakpoints"
	"github.com/mnafshin/apix/internal/pluginrt"
	"github.com/mnafshin/apix/internal/storage"
	apix "github.com/mnafshin/apix/pkg/api/generated"
)

// TrafficSubscriber provides pub/sub for captured HTTP traffic.
type TrafficSubscriber interface {
	Subscribe() chan *apix.HttpRequest
	Unsubscribe(ch chan *apix.HttpRequest)
}

// BreakpointManagerServer is the interface EngineServer uses for breakpoint management.
// *breakpoints.Manager satisfies this interface.
type BreakpointManagerServer interface {
	AddRule(rule *breakpoints.BreakpointRule) (*breakpoints.BreakpointRule, error)
	RemoveRule(id string) error
	ListRules() []*breakpoints.BreakpointRule
	Subscribe() chan *breakpoints.PausedEntry
	Unsubscribe(ch chan *breakpoints.PausedEntry)
	Resume(requestID string, decision *breakpoints.ResumeDecision) error
}

// ServerRepository is the storage interface used by all gRPC handlers.
// *storage.DB satisfies this interface.
type ServerRepository interface {
	SaveRequest(r *storage.RequestRecord) error
	SaveResponse(r *storage.ResponseRecord) error
	SaveRequestTemplate(tpl *storage.RequestTemplateRecord) error
	ListRequestTemplates() ([]*storage.RequestTemplateRecord, error)
	DeleteRequestTemplate(id string) error
	SaveBreakpoint(id, urlPattern string, methods []string, enabled bool, label, headerName, headerValue, bodyPattern string, statusCodes []int32) error
	DeleteBreakpoint(id string) error
	ListTransactions(limit, offset int, urlFilter, methodFilter string, statusFilter int, bodyFilter string) ([]*storage.RequestRecord, []*storage.ResponseRecord, error)
	ExportTransactions(transactionIDs []string) ([]*storage.RequestRecord, []*storage.ResponseRecord, error)
	DeleteAllTransactions() error
	AddRewriteRule(rule *apix.RewriteRule) error
	UpdateRewriteRule(rule *apix.RewriteRule) error
	DeleteRewriteRule(id string) error
	GetRewriteRule(id string) (*apix.RewriteRule, error)
	ListRewriteRules() ([]*apix.RewriteRule, error)
	ListWebSocketFrames(transactionID string) ([]*storage.WebSocketFrameRecord, error)
}

// PluginLister lists installed plugins.
type PluginLister interface {
	List() []pluginrt.PluginMeta
}
