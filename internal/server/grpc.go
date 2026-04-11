package server

import (
	"bytes"
	"context"
	"fmt"
	logging "github.com/mnafshin/apix/internal/logging"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/mnafshin/apix/internal/breakpoints"
	"github.com/mnafshin/apix/internal/config"
	"github.com/mnafshin/apix/internal/engine"
	httputil "github.com/mnafshin/apix/internal/http"
	"github.com/mnafshin/apix/internal/replay"
	apix "github.com/mnafshin/apix/pkg/api/generated"
	"github.com/mnafshin/apix/pkg/version"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

// EngineServer implements the apix.EngineServer gRPC interface.
type EngineServer struct {
	apix.UnimplementedEngineServer
	engine       *engine.Engine
	replayEngine *replay.Engine
	cfg          *config.Config
}

// NewEngineServer wires the gRPC server to all sub-systems.
func NewEngineServer(eng *engine.Engine, re *replay.Engine, cfg *config.Config) *EngineServer {
	return &EngineServer{
		engine:       eng,
		replayEngine: re,
		cfg:          cfg,
	}
}

// ----- Health -----

func (s *EngineServer) GetStatus(ctx context.Context, _ *apix.StatusRequest) (*apix.StatusResponse, error) {
	httpPort, err := strconv.Atoi(s.cfg.HTTPPort)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid http_port %q: %v", s.cfg.HTTPPort, err)
	}
	grpcPort, err := strconv.Atoi(s.cfg.GRPCPort)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid grpc_port %q: %v", s.cfg.GRPCPort, err)
	}
	return &apix.StatusResponse{
		Status:     "OK",
		Version:    version.Version,
		ProxyPort:  int32(httpPort),
		GrpcPort:   int32(grpcPort),
		TlsEnabled: s.cfg.TLSEnabled,
	}, nil
}

// ----- Traffic capture -----

func (s *EngineServer) CaptureTraffic(_ *apix.CaptureRequest, stream grpc.ServerStreamingServer[apix.HttpRequest]) error {
	ch := s.engine.Subscribe()
	defer s.engine.Unsubscribe(ch)

	for {
		select {
		case req, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(req); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return nil
		}
	}
}

// ----- Plugins -----

func (s *EngineServer) ListPlugins(ctx context.Context, _ *apix.PluginListRequest) (*apix.PluginListResponse, error) {
	metas := s.engine.PluginRuntime().List()
	infos := make([]*apix.PluginInfo, 0, len(metas))
	for _, m := range metas {
		infos = append(infos, &apix.PluginInfo{
			Name:        m.Name,
			Version:     m.Version,
			Description: m.Description,
			Enabled:     m.Enabled,
		})
	}
	return &apix.PluginListResponse{Plugins: infos}, nil
}

// ----- Breakpoints -----

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
			hdrs := make(map[string]string)
			for k, vv := range entry.Request.Header {
				if len(vv) > 0 {
					hdrs[k] = vv[0]
				}
			}
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

// ----- Replay -----

func (s *EngineServer) ReplayRequest(ctx context.Context, req *apix.ReplaySpec) (*apix.HttpResponse, error) {
	rr := &replay.ReplayRequest{
		OverrideHeaders: req.OverrideHeaders,
		OverrideBody:    req.OverrideBody,
		FollowRedirects: req.FollowRedirects,
	}

	switch src := req.Source.(type) {
	case *apix.ReplaySpec_RequestId:
		rr.RequestID = src.RequestId
	case *apix.ReplaySpec_RawRequest:
		raw := src.RawRequest
		httpReq, err := http.NewRequestWithContext(ctx, raw.Method, raw.Url, nil)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid raw request: %v", err)
		}
		for k, v := range raw.Headers {
			httpReq.Header.Set(k, v)
		}
		rr.RawRequest = httpReq
	default:
		return nil, status.Error(codes.InvalidArgument, "source must be request_id or raw_request")
	}

	resp, err := s.replayEngine.ReplayRequest(ctx, rr)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "replay: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "read replay response body: %v", err)
	}
	hdrs := make(map[string]string)
	for k, vv := range resp.Header {
		if len(vv) > 0 {
			hdrs[k] = vv[0]
		}
	}
	return &apix.HttpResponse{
		StatusCode: int32(resp.StatusCode),
		StatusText: resp.Status,
		Headers:    hdrs,
		Body:       body,
	}, nil
}

// ----- History -----

func (s *EngineServer) GetHistory(req *apix.HistoryQuery, stream grpc.ServerStreamingServer[apix.HttpTransaction]) error {
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 100
	}
	reqs, resps, err := s.engine.DB().ListTransactions(
		limit, int(req.Offset),
		req.UrlFilter, req.MethodFilter, int(req.StatusFilter),
	)
	if err != nil {
		return status.Errorf(codes.Internal, "list transactions: %v", err)
	}

	for i, r := range reqs {
		// Filter by since_ms.
		if req.SinceMs > 0 && r.Timestamp.UnixMilli() < req.SinceMs {
			continue
		}

		hdrs := make(map[string]string)
		for k, v := range r.Headers {
			hdrs[k] = v
		}

		tx := &apix.HttpTransaction{
			Id:         r.ID,
			Timestamp:  r.Timestamp.UnixMilli(),
			DurationMs: r.DurationMs,
			Request: &apix.HttpRequest{
				Id:        r.ID,
				Method:    r.Method,
				Url:       r.URL,
				Headers:   hdrs,
				Body:      r.Body,
				Timestamp: r.Timestamp.UnixMilli(),
			},
		}

		if resps[i] != nil {
			resp := resps[i]
			respHdrs := make(map[string]string)
			for k, v := range resp.Headers {
				respHdrs[k] = v
			}
			tx.Response = &apix.HttpResponse{
				StatusCode: int32(resp.StatusCode),
				StatusText: resp.StatusText,
				Headers:    respHdrs,
				Body:       resp.Body,
			}
		}

		if err := stream.Send(tx); err != nil {
			return err
		}
	}
	return nil
}

func (s *EngineServer) ClearHistory(ctx context.Context, _ *apix.Empty) (*apix.Empty, error) {
	if err := s.engine.DB().DeleteAllTransactions(); err != nil {
		return nil, status.Errorf(codes.Internal, "clear history: %v", err)
	}
	return &apix.Empty{}, nil
}

// validateToken checks the incoming metadata for a valid Bearer token.
// It returns Unauthenticated if the metadata or Authorization header is missing,
// or if the token does not match expectedToken.
func validateToken(ctx context.Context, expectedToken string) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "missing metadata")
	}
	auth := md.Get("authorization")
	if len(auth) == 0 {
		return status.Error(codes.Unauthenticated, "missing authorization header")
	}
	if auth[0] != "Bearer "+expectedToken {
		return status.Error(codes.Unauthenticated, "invalid token")
	}
	return nil
}

// authUnaryInterceptor returns a gRPC unary interceptor that validates the
// Bearer token from incoming metadata. When token is empty, auth is skipped
// (local desktop mode).
func authUnaryInterceptor(token string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if token == "" {
			return handler(ctx, req)
		}
		if err := validateToken(ctx, token); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// authStreamInterceptor returns a gRPC streaming interceptor that validates
// the Bearer token from incoming metadata. When token is empty, auth is
// skipped (local desktop mode).
func authStreamInterceptor(token string) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if token == "" {
			return handler(srv, ss)
		}
		if err := validateToken(ss.Context(), token); err != nil {
			return err
		}
		return handler(srv, ss)
	}
}

// NewGRPCServer creates a new *grpc.Server configured with auth interceptors
// derived from cfg.AuthToken. When AuthToken is empty, calls are allowed
// without credentials (local desktop mode).
func NewGRPCServer(cfg *config.Config) *grpc.Server {
	return grpc.NewServer(
		grpc.ChainUnaryInterceptor(logging.UnaryServerInterceptor(), authUnaryInterceptor(cfg.AuthToken)),
		grpc.ChainStreamInterceptor(logging.StreamServerInterceptor(), authStreamInterceptor(cfg.AuthToken)),
	)
}

// StartGRPCServer starts the gRPC server and blocks until ctx is cancelled.
func StartGRPCServer(ctx context.Context, eng *engine.Engine, re *replay.Engine, cfg *config.Config) {
	addr := cfg.GRPCBindAddress + ":" + cfg.GRPCPort
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		logging.Fatalf(ctx, "gRPC listen on %s: %v", addr, err)
	}
	defer lis.Close()

	grpcServer := NewGRPCServer(cfg)
	apix.RegisterEngineServer(grpcServer, NewEngineServer(eng, re, cfg))

	// Enable reflection only in unauthenticated (local development) mode.
	// When AuthToken is set for remote/production deployments, reflection
	// is disabled to prevent API surface enumeration bypass of auth.
	if cfg.AuthToken == "" {
		reflection.Register(grpcServer)
	}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		stopped := make(chan struct{})
		go func() {
			grpcServer.GracefulStop()
			close(stopped)
		}()
		select {
		case <-stopped:
		case <-shutCtx.Done():
			grpcServer.Stop()
		}
	}()

	logging.Infof(ctx, "gRPC server listening on %s", addr)
	if err := grpcServer.Serve(lis); err != nil {
		logging.Errorf(ctx, "gRPC server error: %v", err)
	}
	logging.Infof(ctx, "gRPC server stopped")
}

// Suppress unused import warnings.
var _ = fmt.Sprintf
var _ = time.Now
