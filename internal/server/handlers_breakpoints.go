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
		ID:          req.Id,
		URLPattern:  req.UrlPattern,
		Methods:     req.Methods,
		Enabled:     req.Enabled,
		Label:       req.Label,
		HeaderName:  req.HeaderName,
		HeaderValue: req.HeaderValue,
		BodyPattern: req.BodyPattern,
		StatusCodes: req.StatusCodes,
	}
	if rule.ID == "" {
		rule.ID = uuid.NewString()
	}
	added, err := s.breakpointMgr.AddRule(rule)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid breakpoint: %v", err)
	}
	// Persist to storage.
	if err := s.db.SaveBreakpoint(added.ID, added.URLPattern, added.Methods, added.Enabled, added.Label, added.HeaderName, added.HeaderValue, added.BodyPattern, added.StatusCodes); err != nil {
		logging.Errorf(ctx, "grpc: save breakpoint: %v", err)
	}
	s.auditLog(ctx, "set_breakpoint", added.ID, map[string]any{
		"url_pattern": added.URLPattern,
		"methods":     added.Methods,
		"enabled":     added.Enabled,
	})
	return &apix.BreakpointResponse{
		Breakpoint: &apix.BreakpointRule{
			Id:          added.ID,
			UrlPattern:  added.URLPattern,
			Methods:     added.Methods,
			Enabled:     added.Enabled,
			Label:       added.Label,
			HeaderName:  added.HeaderName,
			HeaderValue: added.HeaderValue,
			BodyPattern: added.BodyPattern,
			StatusCodes: added.StatusCodes,
		},
	}, nil
}

func (s *EngineServer) DeleteBreakpoint(ctx context.Context, req *apix.BreakpointID) (*apix.Empty, error) {
	if err := s.breakpointMgr.RemoveRule(req.Id); err != nil {
		return nil, status.Errorf(codes.NotFound, "breakpoint not found: %v", err)
	}
	if err := s.db.DeleteBreakpoint(req.Id); err != nil {
		logging.Errorf(ctx, "grpc: delete breakpoint from storage: %v", err)
	}
	s.auditLog(ctx, "delete_breakpoint", req.Id, nil)
	return &apix.Empty{}, nil
}

func (s *EngineServer) ListBreakpoints(ctx context.Context, _ *apix.Empty) (*apix.BreakpointList, error) {
	rules := s.breakpointMgr.ListRules()
	list := make([]*apix.BreakpointRule, 0, len(rules))
	for _, r := range rules {
		list = append(list, &apix.BreakpointRule{
			Id:          r.ID,
			UrlPattern:  r.URLPattern,
			Methods:     r.Methods,
			Enabled:     r.Enabled,
			Label:       r.Label,
			HeaderName:  r.HeaderName,
			HeaderValue: r.HeaderValue,
			BodyPattern: r.BodyPattern,
			StatusCodes: r.StatusCodes,
		})
	}
	return &apix.BreakpointList{Breakpoints: list}, nil
}

func (s *EngineServer) WatchPausedRequests(_ *apix.Empty, stream grpc.ServerStreamingServer[apix.PausedRequest]) error {
	ch := s.breakpointMgr.Subscribe()
	defer s.breakpointMgr.Unsubscribe(ch)

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

// protoToResumeAction maps proto ResumeAction enum values to the internal
// breakpoints.ResumeAction type. New action variants only need a map entry.
var protoToResumeAction = map[apix.ResumeAction_Action]breakpoints.ResumeAction{
	apix.ResumeAction_DROP:    breakpoints.ActionDrop,
	apix.ResumeAction_RESPOND: breakpoints.ActionRespond,
}

func (s *EngineServer) ResumeRequest(ctx context.Context, req *apix.ResumeAction) (*apix.Empty, error) {
	action, ok := protoToResumeAction[req.Action]
	if !ok {
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
			httputil.SetValidHeaders(ctx, httpReq.Header, mr.Headers, "grpc")
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
		httputil.SetValidHeaders(ctx, hdrs, mr.Headers, "grpc")
		decision.ModifiedResponse = &http.Response{
			StatusCode: int(mr.StatusCode),
			Status:     mr.StatusText,
			Header:     hdrs,
			Body:       io.NopCloser(bytes.NewReader(mr.Body)),
		}
	}

	if err := s.breakpointMgr.Resume(req.RequestId, decision); err != nil {
		return nil, status.Errorf(codes.NotFound, "resume request: %v", err)
	}
	s.auditLog(ctx, "resume_request", req.RequestId, map[string]any{
		"action": req.Action.String(),
	})
	return &apix.Empty{}, nil
}
