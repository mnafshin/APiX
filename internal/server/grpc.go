package server

import (
	"bytes"
	"context"
	"crypto/tls"
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
	"github.com/mnafshin/apix/internal/har"
	httputil "github.com/mnafshin/apix/internal/http"
	"github.com/mnafshin/apix/internal/replay"
	apix "github.com/mnafshin/apix/pkg/api/generated"
	"github.com/mnafshin/apix/pkg/version"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
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

func int32FromInt(v int, field string) (int32, error) {
	if v < -1<<31 || v > 1<<31-1 {
		return 0, status.Errorf(codes.InvalidArgument, "%s out of range for int32", field)
	}
	return int32(v), nil
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
	httpPort32, err := int32FromInt(httpPort, "http_port")
	if err != nil {
		return nil, err
	}
	grpcPort32, err := int32FromInt(grpcPort, "grpc_port")
	if err != nil {
		return nil, err
	}
	return &apix.StatusResponse{
		Status:     "OK",
		Version:    version.Version,
		ProxyPort:  httpPort32,
		GrpcPort:   grpcPort32,
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
	defer func() { _ = resp.Body.Close() }()

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
	statusCode, err := int32FromInt(resp.StatusCode, "replay status code")
	if err != nil {
		return nil, status.Errorf(codes.Internal, "replay: %v", err)
	}
	return &apix.HttpResponse{
		StatusCode: statusCode,
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
			statusCode, err := int32FromInt(resp.StatusCode, "response status code")
			if err != nil {
				return status.Errorf(codes.Internal, "list transactions: %v", err)
			}
			tx.Response = &apix.HttpResponse{
				StatusCode: statusCode,
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

func (s *EngineServer) GetWebSocketFrames(req *apix.GetWebSocketFramesRequest, stream grpc.ServerStreamingServer[apix.WebSocketFrame]) error {
	if req.TransactionId == "" {
		return status.Error(codes.InvalidArgument, "transaction_id is required")
	}

	frames, err := s.engine.DB().ListWebSocketFrames(req.TransactionId)
	if err != nil {
		return status.Errorf(codes.Internal, "list websocket frames: %v", err)
	}
	for _, frame := range frames {
		opcode, err := int32FromInt(frame.Opcode, "websocket opcode")
		if err != nil {
			return status.Errorf(codes.Internal, "list websocket frames: %v", err)
		}
		if err := stream.Send(&apix.WebSocketFrame{
			TransactionId: frame.TransactionID,
			Direction:     frame.Direction,
			Opcode:        opcode,
			Payload:       frame.Payload,
			TimestampMs:   frame.Timestamp.UnixMilli(),
		}); err != nil {
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

func (s *EngineServer) ExportHAR(_ context.Context, req *apix.ExportHARRequest) (*apix.ExportHARResponse, error) {
	requests, responses, err := s.engine.DB().ExportTransactions(req.TransactionIds)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "export HAR: %v", err)
	}
	harJSON, err := har.MarshalTransactions(requests, responses)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "export HAR: %v", err)
	}
	return &apix.ExportHARResponse{HarJson: harJSON}, nil
}

func (s *EngineServer) ImportHAR(ctx context.Context, req *apix.ImportHARRequest) (*apix.ImportHARResponse, error) {
	imported, err := har.ParseTransactions(req.HarJson)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "import HAR: %v", err)
	}

	transactionIDs := make([]string, 0, len(imported))
	for _, tx := range imported {
		select {
		case <-ctx.Done():
			return nil, status.Error(codes.Canceled, "import HAR canceled")
		default:
		}

		if err := s.engine.DB().SaveRequest(tx.Request); err != nil {
			return nil, status.Errorf(codes.Internal, "import HAR request: %v", err)
		}
		if tx.Response != nil {
			if err := s.engine.DB().SaveResponse(tx.Response); err != nil {
				return nil, status.Errorf(codes.Internal, "import HAR response: %v", err)
			}
		}
		transactionIDs = append(transactionIDs, tx.Request.ID)
	}

	return &apix.ImportHARResponse{TransactionIds: transactionIDs}, nil
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
// derived from cfg.AuthToken. When cfg.TLSEnabled is true the server is wrapped
// with TLS credentials loaded from cfg.GRPCCertPath and cfg.GRPCKeyPath.
// When AuthToken is empty, calls are allowed without credentials (local desktop mode).
// When cfg.GRPCRateLimitPerSec > 0, per-peer token-bucket rate limiting is applied.
func NewGRPCServer(cfg *config.Config, extraOpts ...grpc.ServerOption) *grpc.Server {
	opts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(
			logging.UnaryServerInterceptor(),
			rateLimitUnaryInterceptor(cfg.GRPCRateLimitPerSec),
			authUnaryInterceptor(cfg.AuthToken),
		),
		grpc.ChainStreamInterceptor(
			logging.StreamServerInterceptor(),
			rateLimitStreamInterceptor(cfg.GRPCRateLimitPerSec),
			authStreamInterceptor(cfg.AuthToken),
		),
	}
	opts = append(opts, extraOpts...)
	return grpc.NewServer(opts...)
}

// StartGRPCServer starts the gRPC server and blocks until ctx is cancelled.
func StartGRPCServer(ctx context.Context, eng *engine.Engine, re *replay.Engine, cfg *config.Config) {
	addr := cfg.GRPCBindAddress + ":" + cfg.GRPCPort
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		logging.Fatalf(ctx, "gRPC listen on %s: %v", addr, err)
	}
	defer func() { _ = lis.Close() }()

	var serverOpts []grpc.ServerOption
	if cfg.TLSEnabled {
		cert, err := tls.LoadX509KeyPair(cfg.GRPCCertPath, cfg.GRPCKeyPath)
		if err != nil {
			logging.Fatalf(ctx, "gRPC TLS: failed to load cert %q and key %q: %v — check grpc_cert_path and grpc_key_path in config", cfg.GRPCCertPath, cfg.GRPCKeyPath, err)
		}
		tlsCfg := &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
		serverOpts = append(serverOpts, grpc.Creds(credentials.NewTLS(tlsCfg)))
		logging.Infof(ctx, "gRPC TLS enabled (cert: %s)", cfg.GRPCCertPath)
	}

	grpcServer := NewGRPCServer(cfg, serverOpts...)
	apix.RegisterEngineServer(grpcServer, NewEngineServer(eng, re, cfg))

	// Enable reflection only in unauthenticated (local development) mode.
	// When AuthToken is set for remote/production deployments, reflection
	// is disabled to prevent API surface enumeration bypass of auth.
	if cfg.AuthToken == "" {
		reflection.Register(grpcServer)
	}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
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
