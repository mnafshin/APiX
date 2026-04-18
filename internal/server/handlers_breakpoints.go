package server

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/mnafshin/apix/internal/breakpoints"
	httputil "github.com/mnafshin/apix/internal/http"
	logging "github.com/mnafshin/apix/internal/logging"
	apix "github.com/mnafshin/apix/pkg/api/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *EngineServer) SetBreakpoint(ctx context.Context, req *apix.BreakpointRule) (*apix.BreakpointResponse, error) {
	rule := &breakpoints.BreakpointRule{
		ID:         req.Id,
		URLPattern: req.UrlPattern,
		Methods:    req.Methods,
		Enabled:    req.Enabled,
		Label:      req.Label,
	}
	if rule.ID == "" {
		rule.ID = uuid.NewString()
	}
	added, err := s.engine.BreakpointManager().AddRule(rule)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid breakpoint: %v", err)
	}
	// Persist to storage.
	if err := s.engine.DB().SaveBreakpoint(added.ID, added.URLPattern, added.Methods, added.Enabled, added.Label); err != nil {
		logging.Errorf(ctx, "grpc: save breakpoint: %v", err)
	}
	return &apix.BreakpointResponse{
		Breakpoint: &apix.BreakpointRule{
			Id:         added.ID,
			UrlPattern: added.URLPattern,
			Methods:    added.Methods,
			Enabled:    added.Enabled,
			Label:      added.Label,
		},
	}, nil
}

func (s *EngineServer) DeleteBreakpoint(ctx context.Context, req *apix.BreakpointID) (*apix.Empty, error) {
	if err := s.engine.BreakpointManager().RemoveRule(req.Id); err != nil {
		return nil, status.Errorf(codes.NotFound, "breakpoint not found: %v", err)
	}
	if err := s.engine.DB().DeleteBreakpoint(req.Id); err != nil {
		logging.Errorf(ctx, "grpc: delete breakpoint from storage: %v", err)
	}
	return &apix.Empty{}, nil
}

func (s *EngineServer) ListBreakpoints(ctx context.Context, _ *apix.Empty) (*apix.BreakpointList, error) {
	rules := s.engine.BreakpointManager().ListRules()
	list := make([]*apix.BreakpointRule, 0, len(rules))
	for _, r := range rules {
		list = append(list, &apix.BreakpointRule{
			Id:         r.ID,
			UrlPattern: r.URLPattern,
			Methods:    r.Methods,
			Enabled:    r.Enabled,
			Label:      r.Label,
		})
	}
	return &apix.BreakpointList{Breakpoints: list}, nil
}

func (s *EngineServer) WatchPausedRequests(_ *apix.Empty, stream grpc.ServerStreamingServer[apix.PausedRequest]) error {
	ch := s.engine.BreakpointManager().Subscribe()
	defer s.engine.BreakpointManager().Unsubscribe(ch)

	for {
		select {
		case entry, ok := <-ch:
			if !ok {
				return nil
			}
			hdrs := httputil.HeadersToMap(entry.Request.Header)
			if err := stream.Send(&apix.PausedRequest{
				RequestId:    entry.RequestID,
				BreakpointId: entry.BreakpointID,
				PausedAt:     entry.PausedAt.UnixMilli(),
				Request: &apix.HttpRequest{
					Id:      entry.RequestID,
					Method:  entry.Request.Method,
					Url:     entry.Request.URL.String(),
					Headers: hdrs,
				},
			}); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return nil
		}
	}
}

func (s *EngineServer) ResumeRequest(ctx context.Context, req *apix.ResumeAction) (*apix.Empty, error) {
	var action breakpoints.ResumeAction
	switch req.Action {
	case apix.ResumeAction_DROP:
		action = breakpoints.ActionDrop
	case apix.ResumeAction_RESPOND:
		action = breakpoints.ActionRespond
	default:
		action = breakpoints.ActionForward
	}

	decision := &breakpoints.ResumeDecision{Action: action}

	// Build a modified *http.Request from the proto message when the caller
	// supplied one (Forward action only).
	if action == breakpoints.ActionForward && req.ModifiedRequest != nil {
		mr := req.ModifiedRequest
		httpReq, err := http.NewRequestWithContext(ctx, mr.Method, mr.Url,
			io.NopCloser(bytes.NewReader(mr.Body)))
		if err == nil {
			for k, v := range mr.Headers {
				if cn, ok := httputil.CanonicalHeader(k); ok {
					if httputil.IsValidHeaderValue(v) {
						httpReq.Header.Set(cn, v)
					} else {
						logging.Warnf(ctx, "grpc: skipped invalid header value for %q", k)
					}
				} else {
					logging.Warnf(ctx, "grpc: skipped invalid header name %q", k)
				}
			}
			decision.ModifiedRequest = httpReq
		} else {
			logging.Errorf(ctx, "grpc: build modified request: %v", err)
		}
	}

	// Build a synthetic *http.Response when the caller wants a custom reply
	// (Respond action only).
	if action == breakpoints.ActionRespond && req.ModifiedResponse != nil {
		mr := req.ModifiedResponse
		hdrs := make(http.Header)
		for k, v := range mr.Headers {
			if cn, ok := httputil.CanonicalHeader(k); ok {
				if httputil.IsValidHeaderValue(v) {
					hdrs.Set(cn, v)
				} else {
					logging.Warnf(ctx, "grpc: skipped invalid response header value for %q", k)
				}
			} else {
				logging.Warnf(ctx, "grpc: skipped invalid response header name %q", k)
			}
		}
		decision.ModifiedResponse = &http.Response{
			StatusCode: int(mr.StatusCode),
			Status:     mr.StatusText,
			Header:     hdrs,
			Body:       io.NopCloser(bytes.NewReader(mr.Body)),
		}
	}

	if err := s.engine.BreakpointManager().Resume(req.RequestId, decision); err != nil {
		return nil, status.Errorf(codes.NotFound, "resume request: %v", err)
	}
	return &apix.Empty{}, nil
}
