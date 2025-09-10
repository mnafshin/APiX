package server

import (
	"context"
	"log"
	"net"

	apix "github.com/mnafshin/apix/pkg/api/generated"
	"github.com/mnafshin/apix/internal/engine"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type EngineServer struct {
	apix.UnimplementedEngineServer
	engine *engine.Engine
}

func NewEngineServer(eng *engine.Engine) *EngineServer {
	return &EngineServer{engine: eng}
}

func (s *EngineServer) GetStatus(ctx context.Context, req *apix.StatusRequest) (*apix.StatusResponse, error) {
	return &apix.StatusResponse{Status: "OK", Version: "1.0.0"}, nil
}

func (s *EngineServer) CaptureTraffic(req *apix.CaptureRequest, stream apix.Engine_CaptureTrafficServer) error {
	ch := s.engine.Subscribe()
	defer s.engine.Unsubscribe(ch)

	for {
		select {
		case reqInfo, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(reqInfo); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return nil
		}
	}
}

func (s *EngineServer) ListPlugins(ctx context.Context, req *apix.PluginListRequest) (*apix.PluginListResponse, error) {
	return &apix.PluginListResponse{
		Plugins: []*apix.PluginInfo{
			{Name: "PluginA", Description: "Dummy plugin A", Version: "0.1"},
			{Name: "PluginB", Description: "Dummy plugin B", Version: "0.2"},
		},
	}, nil
}

func StartGRPCServer(ctx context.Context, eng *engine.Engine, port string) {
	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("Failed to listen on :%s: %v", port, err)
	}
	grpcServer := grpc.NewServer()
	apix.RegisterEngineServer(grpcServer, NewEngineServer(eng))
	reflection.Register(grpcServer)

	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
	}()

	log.Printf("Starting gRPC server on :%s", port)
	if err := grpcServer.Serve(lis); err != nil {
		log.Printf("gRPC server error: %v", err)
	}
	log.Println("gRPC server stopped")
}