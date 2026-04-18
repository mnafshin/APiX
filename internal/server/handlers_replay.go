package server

import (
	"context"
	"io"
	"net/http"

	httputil "github.com/mnafshin/apix/internal/http"
	"github.com/mnafshin/apix/internal/replay"
	apix "github.com/mnafshin/apix/pkg/api/generated"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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
	hdrs := httputil.HeadersToMap(resp.Header)
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
