package server

import (
	"context"
	"strings"

	gql "github.com/mnafshin/apix/internal/graphql"
	"github.com/mnafshin/apix/internal/har"
	apix "github.com/mnafshin/apix/pkg/api/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *EngineServer) CaptureTraffic(_ *apix.CaptureRequest, stream grpc.ServerStreamingServer[apix.HttpRequest]) error {
	ch := s.traffic.Subscribe()
	defer s.traffic.Unsubscribe(ch)

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

func (s *EngineServer) GetHistory(req *apix.HistoryQuery, stream grpc.ServerStreamingServer[apix.HttpTransaction]) error {
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 100
	}
	reqs, resps, err := s.db.ListTransactions(
		limit, int(req.Offset),
		req.UrlFilter, req.MethodFilter, int(req.StatusFilter), req.BodyFilter,
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
			RequestId:  requestIDFromHeaders(r.ID, r.Headers),
			Request: &apix.HttpRequest{
				Id:        r.ID,
				Method:    r.Method,
				Url:       r.URL,
				Headers:   hdrs,
				Body:      r.Body,
				Timestamp: r.Timestamp.UnixMilli(),
				Protocol:  r.Protocol,
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
		tx.Graphql = toProtoGraphQLMetadata(gql.Extract(tx.Request.Headers, tx.Request.Body, respHeaders(tx.Response), respBody(tx.Response)))

		if err := stream.Send(tx); err != nil {
			return err
		}
	}
	return nil
}

func respHeaders(resp *apix.HttpResponse) map[string]string {
	if resp == nil {
		return nil
	}
	return resp.Headers
}

func respBody(resp *apix.HttpResponse) []byte {
	if resp == nil {
		return nil
	}
	return resp.Body
}

func requestIDFromHeaders(fallback string, headers map[string]string) string {
	for key, value := range headers {
		if strings.EqualFold(key, "X-Request-ID") && value != "" {
			return value
		}
	}
	return fallback
}

func (s *EngineServer) GetWebSocketFrames(req *apix.GetWebSocketFramesRequest, stream grpc.ServerStreamingServer[apix.WebSocketFrame]) error {
	if req.TransactionId == "" {
		return status.Error(codes.InvalidArgument, "transaction_id is required")
	}

	frames, err := s.db.ListWebSocketFrames(req.TransactionId)
	if err != nil {
		return status.Errorf(codes.Internal, "list websocket frames: %v", err)
	}

	// Apply offset/limit pagination matching the HistoryQuery pattern.
	offset := int(req.Offset)
	if offset < 0 {
		offset = 0
	}
	if offset > len(frames) {
		offset = len(frames)
	}
	frames = frames[offset:]
	if req.Limit > 0 && int(req.Limit) < len(frames) {
		frames = frames[:int(req.Limit)]
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
	if err := s.db.DeleteAllTransactions(); err != nil {
		return nil, status.Errorf(codes.Internal, "clear history: %v", err)
	}
	return &apix.Empty{}, nil
}

func (s *EngineServer) ExportHAR(_ context.Context, req *apix.ExportHARRequest) (*apix.ExportHARResponse, error) {
	requests, responses, err := s.db.ExportTransactions(req.TransactionIds)
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

		if err := s.db.SaveRequest(tx.Request); err != nil {
			return nil, status.Errorf(codes.Internal, "import HAR request: %v", err)
		}
		if tx.Response != nil {
			if err := s.db.SaveResponse(tx.Response); err != nil {
				return nil, status.Errorf(codes.Internal, "import HAR response: %v", err)
			}
		}
		transactionIDs = append(transactionIDs, tx.Request.ID)
	}

	return &apix.ImportHARResponse{TransactionIds: transactionIDs}, nil
}
