package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	logging "github.com/mnafshin/apix/internal/logging"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mnafshin/apix/internal/breakpoints"
	"github.com/mnafshin/apix/internal/config"
	httputil "github.com/mnafshin/apix/internal/http"
	"github.com/mnafshin/apix/internal/pluginrt"
	"github.com/mnafshin/apix/internal/proxy"
	"github.com/mnafshin/apix/internal/storage"
	apix "github.com/mnafshin/apix/pkg/api/generated"
)

// Engine is the central coordinator for APiX. It implements proxy.TrafficEngine
// and provides helpers for all gRPC handlers.
type Engine struct {
	mu              sync.RWMutex
	db              storage.TransactionRepository
	bpManager       BreakpointEvaluator
	pluginRT        PluginRuntime
	subscribers     map[chan *apix.HttpRequest]struct{}
	pauseTimeoutSec int // 0 = no timeout
}

type PluginRuntime interface {
	List() []pluginrt.PluginMeta
}

type StorageAccess interface {
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
	GetTransaction(requestID string) (*storage.RequestRecord, *storage.ResponseRecord, error)
	ListWebSocketFrames(transactionID string) ([]*storage.WebSocketFrameRecord, error)
}

type pairTransactionSaver interface {
	SaveTransaction(req *storage.RequestRecord, resp *storage.ResponseRecord) error
}

// New creates a new Engine wiring together all sub-systems.
func New(db storage.TransactionRepository, bpManager BreakpointEvaluator, rt PluginRuntime) *Engine {
	return &Engine{
		db:          db,
		bpManager:   bpManager,
		pluginRT:    rt,
		subscribers: make(map[chan *apix.HttpRequest]struct{}),
	}
}

// NewWithConfig creates a new Engine using per-configuration settings such as
// the breakpoint pause timeout.
func NewWithConfig(db storage.TransactionRepository, bpManager BreakpointEvaluator, rt PluginRuntime, cfg *config.Config) *Engine {
	e := New(db, bpManager, rt)
	if cfg != nil {
		e.pauseTimeoutSec = cfg.BreakpointPauseTimeoutSec
	}
	return e
}

// ----- proxy.TrafficEngine implementation -----

// StoreTransaction persists and publishes a completed HTTP transaction.
func (e *Engine) StoreTransaction(tx *proxy.Transaction) error {
	if tx == nil {
		return nil
	}
	if tx.ID == "" {
		tx.ID = uuid.NewString()
	}

	var (
		retErr  error
		reqRec  *storage.RequestRecord
		respRec *storage.ResponseRecord
	)

	req := tx.Request
	logCtx := context.Background()
	if req != nil && req.Raw != nil {
		logCtx = req.Raw.Context()
	}
	if tx.ID != "" {
		logCtx = logging.WithRequestID(logCtx, tx.ID)
	}
	if req != nil {
		// Prefer storing the original headers captured before plugins modified them.
		var rawHeader http.Header
		if req.Raw != nil {
			rawHeader = req.Raw.Header
		}
		hdrs := httputil.HeadersToMap(firstNonEmptyHeader(tx.OriginalRequestHeaders, rawHeader, req.Headers))

		reqRec = &storage.RequestRecord{
			ID:         tx.ID,
			Method:     req.Method,
			URL:        req.URL.String(),
			Headers:    hdrs,
			Body:       tx.RequestBody,
			Timestamp:  time.Now(),
			DurationMs: tx.DurationMs,
			Protocol:   req.Protocol,
		}

		// Publish to capture subscribers.
		protoReq := &apix.HttpRequest{
			Id:        tx.ID,
			Method:    req.Method,
			Url:       req.URL.String(),
			Headers:   hdrs,
			Body:      tx.RequestBody,
			Timestamp: time.Now().UnixMilli(),
			Protocol:  req.Protocol,
		}
		e.mu.RLock()
		subscribers := make([]chan *apix.HttpRequest, 0, len(e.subscribers))
		for ch := range e.subscribers {
			subscribers = append(subscribers, ch)
		}
		e.mu.RUnlock()
		var dropped int
		for _, sub := range subscribers {
			select {
			case sub <- protoReq:
			default:
				dropped++
			}
		}
		if dropped > 0 {
			logging.Warnf(logCtx,
				"dropped traffic event for %d/%d subscriber(s) — channel full",
				dropped, len(subscribers))
		}
	}

	resp := tx.Response
	if resp != nil {
		hdrs := httputil.HeadersToMap(resp.Headers)
		respRec = &storage.ResponseRecord{
			RequestID:  tx.ID,
			StatusCode: resp.StatusCode,
			StatusText: resp.Status,
			Headers:    hdrs,
			Body:       tx.ResponseBody,
		}
	}

	// Persist request/response. When both records exist and the repository
	// supports an atomic pair write, use it to avoid split auto-commit writes.
	if reqRec != nil && respRec != nil {
		if saver, ok := e.db.(pairTransactionSaver); ok {
			if err := saver.SaveTransaction(reqRec, respRec); err != nil {
				logging.Errorf(logCtx, "engine: save transaction: %v", err)
				retErr = err
			}
			return retErr
		}
	}

	if reqRec != nil {
		if err := e.db.SaveRequest(reqRec); err != nil {
			logging.Errorf(logCtx, "engine: save request: %v", err)
			retErr = err
		}
	}
	if respRec != nil {
		if err := e.db.SaveResponse(respRec); err != nil {
			logging.Errorf(logCtx, "engine: save response: %v", err)
			if retErr == nil {
				retErr = err
			}
		}
	}
	return retErr
}

// StoreWebSocketFrame persists a proxied WebSocket frame for later inspection.
func (e *Engine) StoreWebSocketFrame(frame *proxy.WebSocketFrame) error {
	if frame == nil {
		return nil
	}
	return e.db.SaveWebSocketFrame(&storage.WebSocketFrameRecord{
		TransactionID: frame.TransactionID,
		Direction:     frame.Direction,
		Opcode:        frame.Opcode,
		Payload:       frame.Payload,
		Timestamp:     frame.Timestamp,
	})
}

// PauseRequest holds a request at a breakpoint until resumed.
// It first evaluates whether any enabled rule matches; if none matches the
// request is forwarded immediately without blocking.
// When e.pauseTimeoutSec > 0, the pause is bounded by a deadline; on
// expiry the request is forwarded unchanged.
func (e *Engine) PauseRequest(tx *proxy.Transaction) (*proxy.Transaction, proxy.ResumeAction, error) {
	if tx.Request == nil || tx.Request.Raw == nil {
		return tx, proxy.ResumeForward, nil
	}

	bpID := e.bpManager.EvaluateRequest(tx.Request.Method, tx.Request.URL.String(), tx.Request.Headers, tx.RequestBody)
	if bpID == "" {
		return tx, proxy.ResumeForward, nil
	}

	pauseCtx := tx.Request.Raw.Context()
	var cancelPause context.CancelFunc
	if e.pauseTimeoutSec > 0 {
		pauseCtx, cancelPause = context.WithTimeout(pauseCtx, time.Duration(e.pauseTimeoutSec)*time.Second)
		defer cancelPause()
	}

	entry := breakpoints.NewPausedEntry(tx.ID, bpID, tx.Request.Raw)
	decision, err := e.bpManager.Pause(pauseCtx, entry)
	if err != nil {
		// Timeout or context cancellation: forward unchanged rather than dropping.
		return tx, proxy.ResumeForward, nil
	}
	if decision == nil {
		return tx, proxy.ResumeForward, nil
	}

	switch decision.Action {
	case breakpoints.ActionDrop:
		return tx, proxy.ResumeDrop, nil
	case breakpoints.ActionRespond:
		// Convert the *http.Response supplied by the gRPC handler into the
		// internal ProxyResponse so the proxy layer can write it to the client.
		if decision.ModifiedResponse != nil {
			r := decision.ModifiedResponse
			body, err := io.ReadAll(r.Body)
			if err != nil {
				return tx, proxy.ResumeRespond, fmt.Errorf("read synthetic response body: %w", err)
			}
			tx.Response = &proxy.ProxyResponse{
				StatusCode: r.StatusCode,
				Status:     r.Status,
				Headers:    r.Header.Clone(),
				Body:       io.NopCloser(bytes.NewReader(body)),
			}
		}
		return tx, proxy.ResumeRespond, nil
	default:
		// ActionForward — apply modified request fields if provided.
		if mr := decision.ModifiedRequest; mr != nil {
			tx.Request.Raw = mr
			tx.Request.Method = mr.Method
			tx.Request.URL = mr.URL
			tx.Request.Headers = mr.Header.Clone()
			tx.Request.Body = mr.Body
		}
		return tx, proxy.ResumeForward, nil
	}
}

// PauseResponse holds a response at a breakpoint when any response-phase rule matches.
func (e *Engine) PauseResponse(tx *proxy.Transaction, statusCode int, responseBody []byte) (*proxy.Transaction, proxy.ResumeAction, error) {
	if tx == nil || tx.Request == nil || tx.Request.Raw == nil {
		return tx, proxy.ResumeForward, nil
	}
	bpID := e.bpManager.EvaluateResponse(tx.Request.Method, tx.Request.URL.String(), tx.Request.Headers, responseBody, statusCode)
	if bpID == "" {
		return tx, proxy.ResumeForward, nil
	}

	pauseCtx := tx.Request.Raw.Context()
	var cancelPause context.CancelFunc
	if e.pauseTimeoutSec > 0 {
		pauseCtx, cancelPause = context.WithTimeout(pauseCtx, time.Duration(e.pauseTimeoutSec)*time.Second)
		defer cancelPause()
	}

	entry := breakpoints.NewPausedEntry(tx.ID, bpID, tx.Request.Raw)
	decision, err := e.bpManager.Pause(pauseCtx, entry)
	if err != nil || decision == nil {
		return tx, proxy.ResumeForward, nil
	}

	switch decision.Action {
	case breakpoints.ActionDrop:
		return tx, proxy.ResumeDrop, nil
	case breakpoints.ActionRespond:
		if decision.ModifiedResponse != nil {
			r := decision.ModifiedResponse
			body, readErr := io.ReadAll(r.Body)
			if readErr != nil {
				return tx, proxy.ResumeRespond, fmt.Errorf("read synthetic response body: %w", readErr)
			}
			tx.Response = &proxy.ProxyResponse{
				StatusCode: r.StatusCode,
				Status:     r.Status,
				Headers:    r.Header.Clone(),
				Body:       io.NopCloser(bytes.NewReader(body)),
			}
			tx.ResponseBody = body
		}
		return tx, proxy.ResumeRespond, nil
	default:
		return tx, proxy.ResumeForward, nil
	}
}

// ----- gRPC support helpers -----

// Subscribe returns a channel that receives proto HttpRequest values on each
// captured transaction. The caller must call Unsubscribe when done.
func (e *Engine) Subscribe() chan *apix.HttpRequest {
	ch := make(chan *apix.HttpRequest, 32)
	e.mu.Lock()
	e.subscribers[ch] = struct{}{}
	e.mu.Unlock()
	return ch
}

// Unsubscribe removes and closes a subscriber channel. O(1) via map lookup.
func (e *Engine) Unsubscribe(ch chan *apix.HttpRequest) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.subscribers[ch]; ok {
		delete(e.subscribers, ch)
		close(ch)
	}
}

// DB returns storage access through an interface instead of a concrete adapter.
func (e *Engine) DB() StorageAccess {
	db, _ := e.db.(StorageAccess)
	return db
}

// BreakpointManager returns the breakpoints manager (as BreakpointEvaluator).
func (e *Engine) BreakpointManager() BreakpointEvaluator { return e.bpManager }

// PluginRuntime returns the plugin runtime as an interface.
func (e *Engine) PluginRuntime() PluginRuntime { return e.pluginRT }

// RewriteRules loads the current list of rewrite rules from storage.
func (e *Engine) RewriteRules() ([]*proxy.RewriteRuleProto, error) {
	return e.db.ListRewriteRules()
}

func (e *Engine) SaveRequest(r *storage.RequestRecord) error {
	return e.db.SaveRequest(r)
}

func (e *Engine) SaveResponse(r *storage.ResponseRecord) error {
	return e.db.SaveResponse(r)
}

func (e *Engine) SaveRequestTemplate(tpl *storage.RequestTemplateRecord) error {
	db, err := e.storageAccess()
	if err != nil {
		return err
	}
	return db.SaveRequestTemplate(tpl)
}

func (e *Engine) ListRequestTemplates() ([]*storage.RequestTemplateRecord, error) {
	db, err := e.storageAccess()
	if err != nil {
		return nil, err
	}
	return db.ListRequestTemplates()
}

func (e *Engine) DeleteRequestTemplate(id string) error {
	db, err := e.storageAccess()
	if err != nil {
		return err
	}
	return db.DeleteRequestTemplate(id)
}

func (e *Engine) SaveBreakpoint(id, urlPattern string, methods []string, enabled bool, label, headerName, headerValue, bodyPattern string, statusCodes []int32) error {
	db, err := e.storageAccess()
	if err != nil {
		return err
	}
	return db.SaveBreakpoint(id, urlPattern, methods, enabled, label, headerName, headerValue, bodyPattern, statusCodes)
}

func (e *Engine) DeleteBreakpoint(id string) error {
	db, err := e.storageAccess()
	if err != nil {
		return err
	}
	return db.DeleteBreakpoint(id)
}

func (e *Engine) ListTransactions(limit, offset int, urlFilter, methodFilter string, statusFilter int, bodyFilter string) ([]*storage.RequestRecord, []*storage.ResponseRecord, error) {
	db, err := e.storageAccess()
	if err != nil {
		return nil, nil, err
	}
	return db.ListTransactions(limit, offset, urlFilter, methodFilter, statusFilter, bodyFilter)
}

func (e *Engine) ExportTransactions(transactionIDs []string) ([]*storage.RequestRecord, []*storage.ResponseRecord, error) {
	db, err := e.storageAccess()
	if err != nil {
		return nil, nil, err
	}
	return db.ExportTransactions(transactionIDs)
}

func (e *Engine) DeleteAllTransactions() error {
	db, err := e.storageAccess()
	if err != nil {
		return err
	}
	return db.DeleteAllTransactions()
}

func (e *Engine) AddRewriteRule(rule *apix.RewriteRule) error {
	db, err := e.storageAccess()
	if err != nil {
		return err
	}
	return db.AddRewriteRule(rule)
}

func (e *Engine) UpdateRewriteRule(rule *apix.RewriteRule) error {
	db, err := e.storageAccess()
	if err != nil {
		return err
	}
	return db.UpdateRewriteRule(rule)
}

func (e *Engine) DeleteRewriteRule(id string) error {
	db, err := e.storageAccess()
	if err != nil {
		return err
	}
	return db.DeleteRewriteRule(id)
}

func (e *Engine) GetRewriteRule(id string) (*apix.RewriteRule, error) {
	db, err := e.storageAccess()
	if err != nil {
		return nil, err
	}
	return db.GetRewriteRule(id)
}

func (e *Engine) ListRewriteRules() ([]*apix.RewriteRule, error) {
	db, err := e.storageAccess()
	if err != nil {
		return nil, err
	}
	return db.ListRewriteRules()
}

func (e *Engine) ListWebSocketFrames(transactionID string) ([]*storage.WebSocketFrameRecord, error) {
	db, err := e.storageAccess()
	if err != nil {
		return nil, err
	}
	return db.ListWebSocketFrames(transactionID)
}

func (e *Engine) storageAccess() (StorageAccess, error) {
	db := e.DB()
	if db == nil {
		return nil, errors.New("engine storage access is unavailable")
	}
	return db, nil
}
