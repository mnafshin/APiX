package server

import (
	"context"
	"strconv"

	apix "github.com/mnafshin/apix/pkg/api/generated"
	"github.com/mnafshin/apix/pkg/version"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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

// GetVersion returns the engine's API version and compatibility information.
// Clients should call this on connect and compare api_version against their
// own expectation. The min_client_version field indicates the oldest client
// version still fully compatible with the running engine.
func (s *EngineServer) GetVersion(_ context.Context, _ *apix.VersionRequest) (*apix.VersionResponse, error) {
	return &apix.VersionResponse{
		EngineVersion:    version.Version,
		ApiVersion:       "1.0.0",
		MinClientVersion: "1.0.0",
	}, nil
}
