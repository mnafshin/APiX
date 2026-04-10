package engine

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	apix "github.com/mnafshin/apix/pkg/api/generated"
	"github.com/mnafshin/apix/internal/breakpoints"
	"github.com/mnafshin/apix/internal/pluginrt"
	"github.com/mnafshin/apix/internal/proxy"
	"github.com/mnafshin/apix/internal/storage"
)

// Engine is the central coordinator for APiX. It implements proxy.TrafficEngine
// and provides helpers for all gRPC handlers.
type Engine struct {
	mu          sync.Mutex
	db          *storage.DB
	bpManager   *breakpoints.Manager
	pluginRT    *pluginrt.Runtime
	subscribers []chan *apix.HttpRequest
}

// New creates a new Engine wiring together all sub-systems.
func New(db *storage.DB, bpManager *breakpoints.Manager, rt *pluginrt.Runtime) *Engine {
	return &Engine{
		db:        db,
		bpManager: bpManager,
		pluginRT:  rt,
	}
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

	req := tx.Request
	if req != nil {
		hdrs := make(map[string]string)
		for k, vv := range req.Headers {
			if len(vv) > 0 {
				hdrs[k] = vv[0]
			}
		}

		rec := &storage.RequestRecord{
			ID:         tx.ID,
			Method:     req.Method,
			URL:        req.URL.String(),
			Headers:    hdrs,
			Body:       tx.RequestBody,
			Timestamp:  time.Now(),
			DurationMs: tx.DurationMs,
		}
		if err := e.db.SaveRequest(rec); err != nil {
			log.Printf("engine: save request: %v", err)
		}

		// Publish to capture subscribers.
		protoReq := &apix.HttpRequest{
			Id:        tx.ID,
			Method:    req.Method,
			Url:       req.URL.String(),
			Headers:   hdrs,
			Timestamp: time.Now().UnixMilli(),
		}
		e.mu.Lock()
		subscribers := make([]chan *apix.HttpRequest, len(e.subscribers))
		copy(subscribers, e.subscribers)
		e.mu.Unlock()
		for _, sub := range subscribers {
			select {
			case sub <- protoReq:
			default:
			}
		}
	}

	resp := tx.Response
	if resp != nil {
		hdrs := make(map[string]string)
		for k, vv := range resp.Headers {
			if len(vv) > 0 {
				hdrs[k] = vv[0]
			}
		}
		rec := &storage.ResponseRecord{
			RequestID:  tx.ID,
			StatusCode: resp.StatusCode,
			StatusText: resp.Status,
			Headers:    hdrs,
			Body:       tx.ResponseBody,
		}
		if err := e.db.SaveResponse(rec); err != nil {
			log.Printf("engine: save response: %v", err)
		}
	}
	return nil
}

// PauseRequest holds a request at a breakpoint until resumed.
// It first evaluates whether any enabled rule matches; if none matches the
// request is forwarded immediately without blocking.
func (e *Engine) PauseRequest(tx *proxy.Transaction) (*proxy.Transaction, proxy.ResumeAction, error) {
	if tx.Request == nil || tx.Request.Raw == nil {
		return tx, proxy.ResumeForward, nil
	}

	bpID := e.bpManager.Evaluate(tx.Request.Method, tx.Request.URL.String())
	if bpID == "" {
		return tx, proxy.ResumeForward, nil
	}

	entry := breakpoints.NewPausedEntry(tx.ID, bpID, tx.Request.Raw)
	decision, err := e.bpManager.Pause(tx.Request.Raw.Context(), entry)
	if err != nil {
		return tx, proxy.ResumeForward, fmt.Errorf("pause request: %w", err)
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

// ----- gRPC support helpers -----

// Subscribe returns a channel that receives proto HttpRequest values on each
// captured transaction. The caller must call Unsubscribe when done.
func (e *Engine) Subscribe() chan *apix.HttpRequest {
	ch := make(chan *apix.HttpRequest, 32)
	e.mu.Lock()
	e.subscribers = append(e.subscribers, ch)
	e.mu.Unlock()
	return ch
}

// Unsubscribe removes and closes a subscriber channel.
func (e *Engine) Unsubscribe(ch chan *apix.HttpRequest) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, sub := range e.subscribers {
		if sub == ch {
			e.subscribers = append(e.subscribers[:i], e.subscribers[i+1:]...)
			close(ch)
			return
		}
	}
}

// DB returns the underlying storage.DB for direct access by gRPC handlers.
func (e *Engine) DB() *storage.DB { return e.db }

// BreakpointManager returns the breakpoints manager.
func (e *Engine) BreakpointManager() *breakpoints.Manager { return e.bpManager }

// PluginRuntime returns the plugin runtime.
func (e *Engine) PluginRuntime() *pluginrt.Runtime { return e.pluginRT }
