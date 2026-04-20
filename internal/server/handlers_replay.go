package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	httputil "github.com/mnafshin/apix/internal/http"
	"github.com/mnafshin/apix/internal/replay"
	"github.com/mnafshin/apix/internal/storage"
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
		httpReq, err := protoRequestToHTTP(ctx, src.RawRequest)
		if err != nil {
			return nil, err
		}
		rr.RawRequest = httpReq
	default:
		return nil, status.Error(codes.InvalidArgument, "source must be request_id or raw_request")
	}

	resp, err := s.replayEngine.ReplayRequest(ctx, rr)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "replay: %v", err)
	}
	details := map[string]any{
		"follow_redirects": rr.FollowRedirects,
	}
	if rr.RequestID != "" {
		details["source"] = "request_id"
		details["request_id"] = rr.RequestID
	} else if rr.RawRequest != nil {
		details["source"] = "raw_request"
		details["method"] = rr.RawRequest.Method
		details["url"] = rr.RawRequest.URL.String()
	}
	s.auditLog(ctx, "replay_request", rr.RequestID, details)
	return httpResponseToProto(resp)
}

func (s *EngineServer) ComposeRequest(ctx context.Context, req *apix.ComposeSpec) (*apix.HttpResponse, error) {
	httpReq, err := protoRequestToHTTP(ctx, req.GetRequest())
	if err != nil {
		return nil, err
	}
	resp, err := s.replayEngine.ReplayRequest(ctx, &replay.ReplayRequest{
		RawRequest:      httpReq,
		FollowRedirects: req.FollowRedirects,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "compose request: %v", err)
	}
	s.auditLog(ctx, "compose_request", "", map[string]any{
		"method":           httpReq.Method,
		"url":              httpReq.URL.String(),
		"follow_redirects": req.FollowRedirects,
	})
	return httpResponseToProto(resp)
}

func (s *EngineServer) SaveRequestTemplate(ctx context.Context, req *apix.RequestTemplate) (*apix.RequestTemplate, error) {
	if req.GetRequest() == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.GetRequest().Url == "" {
		return nil, status.Error(codes.InvalidArgument, "request.url is required")
	}
	if req.GetRequest().Method == "" {
		return nil, status.Error(codes.InvalidArgument, "request.method is required")
	}
	id := req.GetId()
	if id == "" {
		id = uuid.NewString()
	}
	record := &storage.RequestTemplateRecord{
		ID:        id,
		Name:      req.GetName(),
		Method:    req.GetRequest().Method,
		URL:       req.GetRequest().Url,
		Headers:   req.GetRequest().Headers,
		Body:      req.GetRequest().Body,
		UpdatedAt: time.Now(),
	}
	if err := s.engine.SaveRequestTemplate(record); err != nil {
		return nil, status.Errorf(codes.Internal, "save request template: %v", err)
	}
	s.auditLog(ctx, "save_request_template", id, map[string]any{
		"name":   record.Name,
		"method": record.Method,
		"url":    record.URL,
	})
	return requestTemplateRecordToProto(record), nil
}

func (s *EngineServer) ListRequestTemplates(_ context.Context, _ *apix.Empty) (*apix.RequestTemplateList, error) {
	records, err := s.engine.ListRequestTemplates()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list request templates: %v", err)
	}
	templates := make([]*apix.RequestTemplate, 0, len(records))
	for _, record := range records {
		templates = append(templates, requestTemplateRecordToProto(record))
	}
	return &apix.RequestTemplateList{Templates: templates}, nil
}

func (s *EngineServer) DeleteRequestTemplate(ctx context.Context, req *apix.RequestTemplateID) (*apix.Empty, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	if err := s.engine.DeleteRequestTemplate(req.GetId()); err != nil {
		return nil, status.Errorf(codes.Internal, "delete request template: %v", err)
	}
	s.auditLog(ctx, "delete_request_template", req.GetId(), nil)
	return &apix.Empty{}, nil
}

func protoRequestToHTTP(ctx context.Context, raw *apix.HttpRequest) (*http.Request, error) {
	if raw == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	body := bytes.NewReader(raw.Body)
	httpReq, err := http.NewRequestWithContext(ctx, raw.Method, raw.Url, body)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid request: %v", err)
	}
	for k, v := range raw.Headers {
		httpReq.Header.Set(k, v)
	}
	return httpReq, nil
}

func httpResponseToProto(resp *http.Response) (*apix.HttpResponse, error) {
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

func requestTemplateRecordToProto(record *storage.RequestTemplateRecord) *apix.RequestTemplate {
	return &apix.RequestTemplate{
		Id:   record.ID,
		Name: record.Name,
		Request: &apix.HttpRequest{
			Method:  record.Method,
			Url:     record.URL,
			Headers: record.Headers,
			Body:    record.Body,
		},
		UpdatedAt: record.UpdatedAt.UnixMilli(),
	}
}
