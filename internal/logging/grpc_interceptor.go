package logging

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// wrappedServerStream wraps grpc.ServerStream to override the Context() method.
type wrappedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedServerStream) Context() context.Context { return w.ctx }

// UnaryServerInterceptor attaches or generates a request ID, injects it into
// the context, and emits start/finish log lines for the gRPC unary call.
func UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		var rid string
		if md != nil {
			if v := md.Get(strings.ToLower(RequestIDHeader)); len(v) > 0 {
				rid = v[0]
			}
		}
		if rid == "" {
			rid = uuid.NewString()
		}
		ctx = WithRequestID(ctx, rid)
		Infof(ctx, "grpc unary start: %s", info.FullMethod)
		res, err := handler(ctx, req)
		if err != nil {
			Errorf(ctx, "grpc unary error: %v", err)
		} else {
			Infof(ctx, "grpc unary done: %s", info.FullMethod)
		}
		return res, err
	}
}

// StreamServerInterceptor attaches or generates a request ID for streaming
// RPCs and logs lifecycle events.
func StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := ss.Context()
		md, _ := metadata.FromIncomingContext(ctx)
		var rid string
		if md != nil {
			if v := md.Get(strings.ToLower(RequestIDHeader)); len(v) > 0 {
				rid = v[0]
			}
		}
		if rid == "" {
			rid = uuid.NewString()
		}
		ctx = WithRequestID(ctx, rid)
		wrapped := &wrappedServerStream{ServerStream: ss, ctx: ctx}
		Infof(ctx, "grpc stream start: %s", info.FullMethod)
		err := handler(srv, wrapped)
		if err != nil {
			Errorf(ctx, "grpc stream error: %v", err)
		} else {
			Infof(ctx, "grpc stream done: %s", info.FullMethod)
		}
		return err
	}
}
