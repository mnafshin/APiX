package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"math"
	"net"
	"time"

	"github.com/mnafshin/apix/internal/config"
	"github.com/mnafshin/apix/internal/engine"
	logging "github.com/mnafshin/apix/internal/logging"
	"github.com/mnafshin/apix/internal/replay"
	apix "github.com/mnafshin/apix/pkg/api/generated"
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
	traffic       TrafficSubscriber
	breakpointMgr BreakpointManagerServer
	db            ServerRepository
	plugins       PluginLister
	replayEngine  *replay.Engine
	cfg           *config.Config
}

func int32FromInt(v int, field string) (int32, error) {
	if v < -1<<31 || v > 1<<31-1 {
		return 0, status.Errorf(codes.InvalidArgument, "%s out of range for int32", field)
	}
	return int32(v), nil
}

// NewEngineServer wires the gRPC server to all sub-systems.
// The concrete *engine.Engine is the composition root — it satisfies all
// narrow interfaces defined in interfaces.go.
func NewEngineServer(eng *engine.Engine, re *replay.Engine, cfg *config.Config) *EngineServer {
	return &EngineServer{
		traffic:       eng,
		breakpointMgr: eng.BreakpointManager(),
		db:            eng.DB(),
		plugins:       eng.PluginRuntime(),
		replayEngine:  re,
		cfg:           cfg,
	}
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
	if cfg.MaxTotalHeaderBytes > 0 {
		maxHeaderListSize := cfg.MaxTotalHeaderBytes
		if maxHeaderListSize > math.MaxUint32 {
			maxHeaderListSize = math.MaxUint32
		}
		opts = append(opts, grpc.MaxHeaderListSize(uint32(maxHeaderListSize)))
	}
	opts = append(opts, extraOpts...)
	return grpc.NewServer(opts...)
}

func grpcServerOptionsFromConfig(cfg *config.Config) ([]grpc.ServerOption, error) {
	if !cfg.TLSEnabled {
		return nil, nil
	}
	if cfg.GRPCCertPath == "" {
		return nil, fmt.Errorf("gRPC TLS: grpc_cert_path is required when tls_enabled is true")
	}
	if cfg.GRPCKeyPath == "" {
		return nil, fmt.Errorf("gRPC TLS: grpc_key_path is required when tls_enabled is true")
	}

	cert, err := tls.LoadX509KeyPair(cfg.GRPCCertPath, cfg.GRPCKeyPath)
	if err != nil {
		return nil, fmt.Errorf("gRPC TLS: failed to load cert %q and key %q: %w", cfg.GRPCCertPath, cfg.GRPCKeyPath, err)
	}

	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	return []grpc.ServerOption{grpc.Creds(credentials.NewTLS(tlsCfg))}, nil
}

// StartGRPCServer starts the gRPC server and blocks until ctx is cancelled.
func StartGRPCServer(ctx context.Context, eng *engine.Engine, re *replay.Engine, cfg *config.Config) {
	addr := cfg.GRPCBindAddress + ":" + cfg.GRPCPort
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		logging.Fatalf(ctx, "gRPC listen on %s: %v", addr, err)
	}
	defer func() { _ = lis.Close() }()

	serverOpts, err := grpcServerOptionsFromConfig(cfg)
	if err != nil {
		logging.Fatalf(ctx, "%v — check grpc_cert_path and grpc_key_path in config", err)
	}
	if cfg.TLSEnabled {
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
