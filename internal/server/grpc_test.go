package server_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/mnafshin/apix/internal/breakpoints"
	"github.com/mnafshin/apix/internal/config"
	"github.com/mnafshin/apix/internal/engine"
	"github.com/mnafshin/apix/internal/pluginrt"
	"github.com/mnafshin/apix/internal/proxy"
	"github.com/mnafshin/apix/internal/replay"
	"github.com/mnafshin/apix/internal/server"
	"github.com/mnafshin/apix/internal/storage"
	apix "github.com/mnafshin/apix/pkg/api/generated"
)

const bufSize = 1024 * 1024

// fixture bundles everything needed for a single test scenario.
type fixture struct {
	client apix.EngineClient
	db     *storage.DB
	bpm    *breakpoints.Manager
	eng    *engine.Engine
	stop   func()
}

// newFixture spins up an in-process gRPC server backed by a ":memory:" SQLite
// database and returns a connected client plus supporting objects.
func newFixture(t *testing.T) *fixture {
	t.Helper()

	lis := bufconn.Listen(bufSize)

	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}

	bpm := breakpoints.NewManager()
	prt := pluginrt.NewRuntime()
	eng := engine.New(db, bpm, prt)
	re := replay.NewEngine(db, nil)
	cfg := &config.Config{HTTPPort: "8080", GRPCPort: "9090"}

	grpcSrv := grpc.NewServer()
	apix.RegisterEngineServer(grpcSrv, server.NewEngineServer(eng, re, cfg))
	go grpcSrv.Serve(lis) //nolint:errcheck

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		grpcSrv.Stop()
		db.Close()
		t.Fatalf("grpc.NewClient: %v", err)
	}

	stop := func() {
		conn.Close()
		grpcSrv.Stop()
		db.Close()
	}

	return &fixture{
		client: apix.NewEngineClient(conn),
		db:     db,
		bpm:    bpm,
		eng:    eng,
		stop:   stop,
	}
}

// ── GetStatus ──────────────────────────────────────────────────────────────

func TestGetStatus(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.stop()

	ctx := context.Background()
	resp, err := f.client.GetStatus(ctx, &apix.StatusRequest{})
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if resp.Status != "OK" {
		t.Errorf("Status: got %q want %q", resp.Status, "OK")
	}
	if resp.ProxyPort != 8080 {
		t.Errorf("ProxyPort: got %d want 8080", resp.ProxyPort)
	}
	if resp.GrpcPort != 9090 {
		t.Errorf("GrpcPort: got %d want 9090", resp.GrpcPort)
	}
}

// ── ListPlugins ────────────────────────────────────────────────────────────

func TestListPlugins_Empty(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.stop()

	resp, err := f.client.ListPlugins(context.Background(), &apix.PluginListRequest{})
	if err != nil {
		t.Fatalf("ListPlugins: %v", err)
	}
	if len(resp.Plugins) != 0 {
		t.Errorf("expected 0 plugins, got %d", len(resp.Plugins))
	}
}

// ── Breakpoints ────────────────────────────────────────────────────────────

func TestSetAndListBreakpoints(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.stop()

	ctx := context.Background()

	setResp, err := f.client.SetBreakpoint(ctx, &apix.BreakpointRule{
		UrlPattern:  `.*example\.com.*`,
		Methods:     []string{"GET"},
		Enabled:     true,
		Label:       "test-bp",
		HeaderName:  "X-Debug",
		HeaderValue: "1",
		BodyPattern: "error",
		StatusCodes: []int32{500},
	})
	if err != nil {
		t.Fatalf("SetBreakpoint: %v", err)
	}
	if setResp.Breakpoint.Id == "" {
		t.Error("expected non-empty breakpoint ID")
	}
	if setResp.Breakpoint.Label != "test-bp" {
		t.Errorf("Label: got %q want %q", setResp.Breakpoint.Label, "test-bp")
	}

	listResp, err := f.client.ListBreakpoints(ctx, &apix.Empty{})
	if err != nil {
		t.Fatalf("ListBreakpoints: %v", err)
	}
	if len(listResp.Breakpoints) != 1 {
		t.Fatalf("expected 1 breakpoint, got %d", len(listResp.Breakpoints))
	}
	if listResp.Breakpoints[0].Id != setResp.Breakpoint.Id {
		t.Errorf("ID mismatch: got %q want %q", listResp.Breakpoints[0].Id, setResp.Breakpoint.Id)
	}
	if listResp.Breakpoints[0].HeaderName != "X-Debug" {
		t.Errorf("HeaderName: got %q want %q", listResp.Breakpoints[0].HeaderName, "X-Debug")
	}
	if len(listResp.Breakpoints[0].StatusCodes) != 1 || listResp.Breakpoints[0].StatusCodes[0] != 500 {
		t.Errorf("StatusCodes: got %v want [500]", listResp.Breakpoints[0].StatusCodes)
	}
}

func TestDeleteBreakpoint(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.stop()

	ctx := context.Background()

	setResp, err := f.client.SetBreakpoint(ctx, &apix.BreakpointRule{
		UrlPattern: `.*`,
		Enabled:    true,
		Label:      "to-delete",
	})
	if err != nil {
		t.Fatalf("SetBreakpoint: %v", err)
	}

	if _, err := f.client.DeleteBreakpoint(ctx, &apix.BreakpointID{Id: setResp.Breakpoint.Id}); err != nil {
		t.Fatalf("DeleteBreakpoint: %v", err)
	}

	listResp, err := f.client.ListBreakpoints(ctx, &apix.Empty{})
	if err != nil {
		t.Fatalf("ListBreakpoints after delete: %v", err)
	}
	if len(listResp.Breakpoints) != 0 {
		t.Errorf("expected 0 breakpoints after delete, got %d", len(listResp.Breakpoints))
	}
}

func TestDeleteBreakpoint_NotFound(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.stop()

	_, err := f.client.DeleteBreakpoint(context.Background(), &apix.BreakpointID{Id: "no-such-id"})
	if err == nil {
		t.Fatal("expected error for unknown breakpoint ID, got nil")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.NotFound {
		t.Errorf("expected codes.NotFound, got %v", err)
	}
}

func TestSetBreakpoint_InvalidRegex(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.stop()

	_, err := f.client.SetBreakpoint(context.Background(), &apix.BreakpointRule{
		UrlPattern: "[invalid-regex",
		Enabled:    true,
	})
	if err == nil {
		t.Fatal("expected error for invalid regex, got nil")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.InvalidArgument {
		t.Errorf("expected codes.InvalidArgument, got %v", err)
	}
}

// ── ResumeRequest ──────────────────────────────────────────────────────────

func TestResumeRequest_UnknownID(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.stop()

	_, err := f.client.ResumeRequest(context.Background(), &apix.ResumeAction{
		RequestId: "no-such-request",
		Action:    apix.ResumeAction_FORWARD,
	})
	if err == nil {
		t.Fatal("expected error for unknown request ID, got nil")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.NotFound {
		t.Errorf("expected codes.NotFound, got %v", err)
	}
}

// ── ClearHistory ───────────────────────────────────────────────────────────

func TestClearHistory(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.stop()

	ctx := context.Background()

	// Seed one request directly into storage.
	if err := f.db.SaveRequest(&storage.RequestRecord{
		ID:        "req-1",
		Method:    "GET",
		URL:       "https://example.com/",
		Headers:   map[string]string{},
		Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("SaveRequest: %v", err)
	}

	// Verify it is visible via GetHistory.
	streamBefore, err := f.client.GetHistory(ctx, &apix.HistoryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("GetHistory before clear: %v", err)
	}
	countBefore := drainHistory(t, streamBefore)
	if countBefore == 0 {
		t.Error("expected at least 1 transaction before clear")
	}

	if _, err := f.client.ClearHistory(ctx, &apix.Empty{}); err != nil {
		t.Fatalf("ClearHistory: %v", err)
	}

	streamAfter, err := f.client.GetHistory(ctx, &apix.HistoryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("GetHistory after clear: %v", err)
	}
	countAfter := drainHistory(t, streamAfter)
	if countAfter != 0 {
		t.Errorf("expected 0 transactions after clear, got %d", countAfter)
	}
}

func TestExportHAR(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.stop()

	now := time.Unix(1700000000, 0).UTC()
	if err := f.db.SaveRequest(&storage.RequestRecord{
		ID:         "har-1",
		Method:     "POST",
		URL:        "https://example.com/users",
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       []byte(`{"name":"alice"}`),
		Timestamp:  now,
		DurationMs: 42,
	}); err != nil {
		t.Fatalf("SaveRequest: %v", err)
	}
	if err := f.db.SaveResponse(&storage.ResponseRecord{
		RequestID:  "har-1",
		StatusCode: 201,
		StatusText: "Created",
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       []byte(`{"id":1}`),
	}); err != nil {
		t.Fatalf("SaveResponse: %v", err)
	}

	resp, err := f.client.ExportHAR(context.Background(), &apix.ExportHARRequest{
		TransactionIds: []string{"har-1"},
	})
	if err != nil {
		t.Fatalf("ExportHAR: %v", err)
	}
	for _, want := range []string{
		`"version": "1.2"`,
		`"url": "https://example.com/users"`,
		`"status": 201`,
		`"text": "{\"name\":\"alice\"}"`,
	} {
		if !strings.Contains(resp.HarJson, want) {
			t.Fatalf("expected HAR export to contain %q\n%s", want, resp.HarJson)
		}
	}
}

func TestImportHAR(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.stop()

	harJSON := `{
	  "log": {
	    "version": "1.2",
	    "creator": { "name": "tester", "version": "1" },
	    "entries": [{
	      "startedDateTime": "2024-01-01T00:00:00Z",
	      "time": 25,
	      "request": {
	        "method": "POST",
	        "url": "https://example.com/imported",
	        "httpVersion": "HTTP/1.1",
	        "headers": [{ "name": "Authorization", "value": "Bearer token" }],
	        "postData": {
	          "mimeType": "application/json",
	          "text": "{\"imported\":true}"
	        },
	        "headersSize": -1,
	        "bodySize": 17
	      },
	      "response": {
	        "status": 200,
	        "statusText": "OK",
	        "httpVersion": "HTTP/1.1",
	        "headers": [],
	        "content": {
	          "mimeType": "application/json",
	          "text": "{\"ok\":true}"
	        },
	        "redirectURL": "",
	        "headersSize": -1,
	        "bodySize": 11
	      },
	      "timings": { "send": 0, "wait": 25, "receive": 0 }
	    }]
	  }
	}`

	resp, err := f.client.ImportHAR(context.Background(), &apix.ImportHARRequest{HarJson: harJSON})
	if err != nil {
		t.Fatalf("ImportHAR: %v", err)
	}
	if len(resp.TransactionIds) != 1 {
		t.Fatalf("expected 1 imported transaction ID, got %d", len(resp.TransactionIds))
	}

	req, replayableResp, err := f.db.GetTransaction(resp.TransactionIds[0])
	if err != nil {
		t.Fatalf("GetTransaction: %v", err)
	}
	if req == nil {
		t.Fatal("expected imported request")
	}
	if req.URL != "https://example.com/imported" {
		t.Fatalf("request URL: got %q want %q", req.URL, "https://example.com/imported")
	}
	if string(req.Body) != `{"imported":true}` {
		t.Fatalf("request body: got %q", req.Body)
	}
	if replayableResp == nil {
		t.Fatal("expected imported response")
	}
	if replayableResp.StatusCode != 200 {
		t.Fatalf("response status: got %d want 200", replayableResp.StatusCode)
	}
}

// ── GetHistory ─────────────────────────────────────────────────────────────

func TestGetHistory(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.stop()

	ctx := context.Background()

	// Seed two requests.
	for _, rec := range []*storage.RequestRecord{
		{ID: "req-a", Method: "GET", URL: "https://a.example.com/", Headers: map[string]string{}, Timestamp: time.Now()},
		{ID: "req-b", Method: "POST", URL: "https://b.example.com/", Headers: map[string]string{}, Timestamp: time.Now()},
	} {
		if err := f.db.SaveRequest(rec); err != nil {
			t.Fatalf("SaveRequest %s: %v", rec.ID, err)
		}
	}

	stream, err := f.client.GetHistory(ctx, &apix.HistoryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	count := drainHistory(t, stream)
	if count != 2 {
		t.Errorf("expected 2 transactions, got %d", count)
	}
}

func TestGetWebSocketFrames(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.stop()

	req := &storage.RequestRecord{
		ID:        "ws-req-1",
		Method:    "GET",
		URL:       "wss://example.com/socket",
		Headers:   map[string]string{"Upgrade": "websocket"},
		Timestamp: time.Now(),
	}
	if err := f.db.SaveRequest(req); err != nil {
		t.Fatalf("SaveRequest: %v", err)
	}
	for _, frame := range []*storage.WebSocketFrameRecord{
		{TransactionID: "ws-req-1", Direction: "client", Opcode: 1, Payload: []byte("hello"), Timestamp: time.Unix(1700000000, 0).UTC()},
		{TransactionID: "ws-req-1", Direction: "server", Opcode: 2, Payload: []byte{0x01, 0x02}, Timestamp: time.Unix(1700000001, 0).UTC()},
	} {
		if err := f.db.SaveWebSocketFrame(frame); err != nil {
			t.Fatalf("SaveWebSocketFrame: %v", err)
		}
	}

	stream, err := f.client.GetWebSocketFrames(context.Background(), &apix.GetWebSocketFramesRequest{
		TransactionId: "ws-req-1",
	})
	if err != nil {
		t.Fatalf("GetWebSocketFrames: %v", err)
	}
	frames := drainWebSocketFrames(t, stream)
	if len(frames) != 2 {
		t.Fatalf("expected 2 websocket frames, got %d", len(frames))
	}
	if frames[0].Direction != "client" || string(frames[0].Payload) != "hello" {
		t.Fatalf("first websocket frame mismatch: %+v", frames[0])
	}
	if frames[1].Opcode != 2 {
		t.Fatalf("second websocket frame opcode mismatch: %+v", frames[1])
	}
}

func TestGetWebSocketFrames_InvalidRequest(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.stop()

	stream, err := f.client.GetWebSocketFrames(context.Background(), &apix.GetWebSocketFramesRequest{})
	if err != nil {
		t.Fatalf("GetWebSocketFrames: %v", err)
	}
	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error for empty transaction ID")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.InvalidArgument {
		t.Errorf("expected codes.InvalidArgument, got %v", err)
	}
}

// ── CaptureTraffic ─────────────────────────────────────────────────────────

func TestCaptureTraffic(t *testing.T) {
	f := newFixture(t)
	defer f.stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := f.client.CaptureTraffic(ctx, &apix.CaptureRequest{})
	if err != nil {
		t.Fatalf("CaptureTraffic: %v", err)
	}

	// Give the server goroutine a moment to subscribe before publishing.
	time.Sleep(20 * time.Millisecond)

	// Publish a transaction via the engine (mirrors what the proxy does).
	targetURL, _ := url.Parse("https://capture.example.com/test")
	err = f.eng.StoreTransaction(&proxy.Transaction{
		ID: "cap-1",
		Request: &proxy.ProxyRequest{
			ID:      "cap-1",
			Method:  "GET",
			URL:     targetURL,
			Headers: http.Header{},
			Raw:     &http.Request{Method: "GET", URL: targetURL, Header: http.Header{}},
		},
	})
	if err != nil {
		t.Fatalf("StoreTransaction: %v", err)
	}

	// Receive the published request from the stream.
	msg, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv: %v", err)
	}
	if msg.Method != "GET" {
		t.Errorf("Method: got %q want %q", msg.Method, "GET")
	}
	if msg.Url != "https://capture.example.com/test" {
		t.Errorf("URL: got %q want %q", msg.Url, "https://capture.example.com/test")
	}

	// Cancel the context to close the stream cleanly.
	cancel()
}

// ── WatchPausedRequests ────────────────────────────────────────────────────

func TestWatchPausedRequests(t *testing.T) {
	f := newFixture(t)
	defer f.stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := f.client.WatchPausedRequests(ctx, &apix.Empty{})
	if err != nil {
		t.Fatalf("WatchPausedRequests: %v", err)
	}

	// Give the server goroutine a moment to subscribe before pausing.
	time.Sleep(20 * time.Millisecond)

	pauseReqURL, _ := url.Parse("https://bp.example.com/api")
	rawReq, _ := http.NewRequestWithContext(ctx, "GET", "https://bp.example.com/api", nil)
	entry := breakpoints.NewPausedEntry("pause-req-1", "bp-1", rawReq)

	// Pause in a goroutine because Pause() blocks until resumed.
	pauseDone := make(chan error, 1)
	go func() {
		_, err := f.bpm.Pause(ctx, entry)
		pauseDone <- err
	}()

	// Receive the paused notification on the stream.
	msg, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv: %v", err)
	}
	if msg.RequestId != "pause-req-1" {
		t.Errorf("RequestId: got %q want %q", msg.RequestId, "pause-req-1")
	}
	if msg.Request.Url != pauseReqURL.String() {
		t.Errorf("URL: got %q want %q", msg.Request.Url, pauseReqURL.String())
	}

	// Resume the paused request so the goroutine unblocks.
	if err := f.bpm.Resume("pause-req-1", &breakpoints.ResumeDecision{Action: breakpoints.ActionForward}); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if err := <-pauseDone; err != nil {
		t.Errorf("Pause goroutine: %v", err)
	}

	cancel()
}

// ── ReplayRequest ──────────────────────────────────────────────────────────

func TestReplayRequest_RawRequest(t *testing.T) {
	t.Parallel()

	// Start a local HTTP echo server so the replay engine can reach it.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("pong")) //nolint:errcheck
	}))
	defer upstream.Close()

	f := newFixture(t)
	defer f.stop()

	ctx := context.Background()
	resp, err := f.client.ReplayRequest(ctx, &apix.ReplaySpec{
		Source: &apix.ReplaySpec_RawRequest{
			RawRequest: &apix.HttpRequest{
				Method: "GET",
				Url:    upstream.URL + "/ping",
			},
		},
		FollowRedirects: false,
	})
	if err != nil {
		t.Fatalf("ReplayRequest: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode: got %d want %d", resp.StatusCode, http.StatusOK)
	}
	if string(resp.Body) != "pong" {
		t.Errorf("Body: got %q want %q", string(resp.Body), "pong")
	}
}

func TestReplayRequest_NoSource(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.stop()

	// Sending a ReplaySpec with no source should return InvalidArgument.
	_, err := f.client.ReplayRequest(context.Background(), &apix.ReplaySpec{})
	if err == nil {
		t.Fatal("expected error for missing source, got nil")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.InvalidArgument {
		t.Errorf("expected codes.InvalidArgument, got %v", err)
	}
}

func TestReplayRequest_UnknownID(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.stop()

	_, err := f.client.ReplayRequest(context.Background(), &apix.ReplaySpec{
		Source: &apix.ReplaySpec_RequestId{RequestId: "no-such-id"},
	})
	if err == nil {
		t.Fatal("expected error for unknown request ID, got nil")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.Internal {
		t.Errorf("expected codes.Internal, got %v", err)
	}
}

// ── Auth interceptor ───────────────────────────────────────────────────────

func TestAuthInterceptor_NoToken_Passes(t *testing.T) {
	t.Parallel()
	// Empty token = auth disabled; any call should succeed without credentials.
	f := newFixtureWithToken(t, "")
	defer f.stop()

	_, err := f.client.GetStatus(context.Background(), &apix.StatusRequest{})
	if err != nil {
		t.Fatalf("GetStatus with no token configured: %v", err)
	}
}

func TestAuthInterceptor_ValidToken_Passes(t *testing.T) {
	t.Parallel()
	f := newFixtureWithToken(t, "secret")
	defer f.stop()

	_, err := f.client.GetStatus(ctxWithToken("secret"), &apix.StatusRequest{})
	if err != nil {
		t.Fatalf("GetStatus with valid token: %v", err)
	}
}

func TestAuthInterceptor_InvalidToken_Fails(t *testing.T) {
	t.Parallel()
	f := newFixtureWithToken(t, "secret")
	defer f.stop()

	_, err := f.client.GetStatus(ctxWithToken("wrong"), &apix.StatusRequest{})
	if err == nil {
		t.Fatal("expected Unauthenticated error for wrong token, got nil")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.Unauthenticated {
		t.Errorf("expected codes.Unauthenticated, got %v", err)
	}
}

func TestAuthInterceptor_MissingHeader_Fails(t *testing.T) {
	t.Parallel()
	f := newFixtureWithToken(t, "secret")
	defer f.stop()

	// Call without any Authorization header.
	_, err := f.client.GetStatus(context.Background(), &apix.StatusRequest{})
	if err == nil {
		t.Fatal("expected Unauthenticated error for missing header, got nil")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.Unauthenticated {
		t.Errorf("expected codes.Unauthenticated, got %v", err)
	}
}

// ── helpers ────────────────────────────────────────────────────────────────

// newFixtureWithToken is like newFixture but spins up the gRPC server with
// auth interceptors enabled for the given token.
func newFixtureWithToken(t *testing.T, token string) *fixture {
	t.Helper()

	lis := bufconn.Listen(bufSize)

	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}

	bpm := breakpoints.NewManager()
	prt := pluginrt.NewRuntime()
	eng := engine.New(db, bpm, prt)
	re := replay.NewEngine(db, nil)
	cfg := &config.Config{HTTPPort: "8080", GRPCPort: "9090", AuthToken: token}

	grpcSrv := server.NewGRPCServer(cfg)
	apix.RegisterEngineServer(grpcSrv, server.NewEngineServer(eng, re, cfg))
	go grpcSrv.Serve(lis) //nolint:errcheck

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		grpcSrv.Stop()
		db.Close()
		t.Fatalf("grpc.NewClient: %v", err)
	}

	stop := func() {
		conn.Close()
		grpcSrv.Stop()
		db.Close()
	}

	return &fixture{
		client: apix.NewEngineClient(conn),
		db:     db,
		bpm:    bpm,
		eng:    eng,
		stop:   stop,
	}
}

// ctxWithToken returns a context that carries an Authorization: Bearer <token>
// metadata entry for outgoing gRPC calls.
func ctxWithToken(token string) context.Context {
	md := metadata.Pairs("authorization", "Bearer "+token)
	return metadata.NewOutgoingContext(context.Background(), md)
}

// drainHistory reads all messages from a GetHistory stream until EOF and
// returns the count.
func drainHistory(t *testing.T, stream grpc.ServerStreamingClient[apix.HttpTransaction]) int {
	t.Helper()
	count := 0
	for {
		_, err := stream.Recv()
		if err != nil {
			break
		}
		count++
	}
	return count
}

func drainWebSocketFrames(t *testing.T, stream grpc.ServerStreamingClient[apix.WebSocketFrame]) []*apix.WebSocketFrame {
	t.Helper()
	frames := []*apix.WebSocketFrame{}
	for {
		frame, err := stream.Recv()
		if err != nil {
			break
		}
		frames = append(frames, frame)
	}
	return frames
}

// seedRequests inserts n request records into the fixture DB with sequential IDs.
func seedRequests(t *testing.T, f *fixture, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		rec := &storage.RequestRecord{
			ID:        fmt.Sprintf("req-%03d", i),
			Method:    "GET",
			URL:       "https://example.com/page",
			Headers:   map[string]string{},
			Timestamp: time.Now(),
		}
		if err := f.db.SaveRequest(rec); err != nil {
			t.Fatalf("SaveRequest %s: %v", rec.ID, err)
		}
	}
}

// ── GetHistory – pagination & filters ──────────────────────────────────────

func TestGetHistory_Pagination(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.stop()

	seedRequests(t, f, 25)

	ctx := context.Background()

	// First page: limit=10, offset=0 → expect 10 items.
	stream, err := f.client.GetHistory(ctx, &apix.HistoryQuery{Limit: 10, Offset: 0})
	if err != nil {
		t.Fatalf("GetHistory page 1: %v", err)
	}
	page1 := drainHistory(t, stream)
	if page1 != 10 {
		t.Errorf("page 1: got %d want 10", page1)
	}

	// Second page: limit=10, offset=10 → expect 10 items.
	stream2, err := f.client.GetHistory(ctx, &apix.HistoryQuery{Limit: 10, Offset: 10})
	if err != nil {
		t.Fatalf("GetHistory page 2: %v", err)
	}
	page2 := drainHistory(t, stream2)
	if page2 != 10 {
		t.Errorf("page 2: got %d want 10", page2)
	}

	// Third page: limit=10, offset=20 → expect 5 items (the last 5).
	stream3, err := f.client.GetHistory(ctx, &apix.HistoryQuery{Limit: 10, Offset: 20})
	if err != nil {
		t.Fatalf("GetHistory page 3: %v", err)
	}
	page3 := drainHistory(t, stream3)
	if page3 != 5 {
		t.Errorf("page 3: got %d want 5", page3)
	}
}

func TestGetHistory_URLFilter(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.stop()

	for _, rec := range []*storage.RequestRecord{
		{ID: "url-a", Method: "GET", URL: "https://api.example.com/users", Headers: map[string]string{}, Timestamp: time.Now()},
		{ID: "url-b", Method: "GET", URL: "https://api.example.com/orders", Headers: map[string]string{}, Timestamp: time.Now()},
		{ID: "url-c", Method: "GET", URL: "https://other.example.com/users", Headers: map[string]string{}, Timestamp: time.Now()},
	} {
		if err := f.db.SaveRequest(rec); err != nil {
			t.Fatalf("SaveRequest: %v", err)
		}
	}

	stream, err := f.client.GetHistory(context.Background(), &apix.HistoryQuery{
		Limit:     10,
		UrlFilter: "api.example.com",
	})
	if err != nil {
		t.Fatalf("GetHistory with URL filter: %v", err)
	}
	count := drainHistory(t, stream)
	if count != 2 {
		t.Errorf("URL filter: got %d want 2 (api.example.com)", count)
	}
}

func TestGetHistory_MethodFilter(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.stop()

	for _, rec := range []*storage.RequestRecord{
		{ID: "mf-get", Method: "GET", URL: "https://example.com/", Headers: map[string]string{}, Timestamp: time.Now()},
		{ID: "mf-post", Method: "POST", URL: "https://example.com/create", Headers: map[string]string{}, Timestamp: time.Now()},
		{ID: "mf-put", Method: "PUT", URL: "https://example.com/update", Headers: map[string]string{}, Timestamp: time.Now()},
	} {
		if err := f.db.SaveRequest(rec); err != nil {
			t.Fatalf("SaveRequest: %v", err)
		}
	}

	stream, err := f.client.GetHistory(context.Background(), &apix.HistoryQuery{
		Limit:        10,
		MethodFilter: "POST",
	})
	if err != nil {
		t.Fatalf("GetHistory with method filter: %v", err)
	}
	count := drainHistory(t, stream)
	if count != 1 {
		t.Errorf("method filter: got %d want 1 (POST only)", count)
	}
}

// ── ResumeRequest – modified request & response ────────────────────────────

func TestResumeRequest_WithModifiedRequest(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Pause a request via the breakpoints manager directly.
	targetURL, _ := url.Parse("http://example.com/original")
	rawReq, _ := http.NewRequestWithContext(ctx, "GET", targetURL.String(), nil)
	entry := breakpoints.NewPausedEntry("mod-req-1", "bp-1", rawReq)

	resumeDecision := make(chan struct{})
	go func() {
		defer close(resumeDecision)
		_, _ = f.bpm.Pause(ctx, entry)
	}()

	// Give the pause goroutine time to register.
	time.Sleep(20 * time.Millisecond)

	// Resume via gRPC with a modified URL — should succeed without error.
	_, err := f.client.ResumeRequest(ctx, &apix.ResumeAction{
		RequestId: "mod-req-1",
		Action:    apix.ResumeAction_FORWARD,
		ModifiedRequest: &apix.HttpRequest{
			Method: "GET",
			Url:    "http://example.com/modified",
		},
	})
	if err != nil {
		t.Fatalf("ResumeRequest with modified request: %v", err)
	}

	// Verify the pause goroutine unblocks.
	select {
	case <-resumeDecision:
	case <-time.After(3 * time.Second):
		t.Fatal("paused request never unblocked after ResumeRequest")
	}
}

// ── CaptureTraffic – multiple subscribers ──────────────────────────────────

func TestCaptureTraffic_MultipleSubscribers(t *testing.T) {
	f := newFixture(t)
	defer f.stop()

	ctx1, cancel1 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel1()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()

	stream1, err := f.client.CaptureTraffic(ctx1, &apix.CaptureRequest{})
	if err != nil {
		t.Fatalf("CaptureTraffic subscriber 1: %v", err)
	}
	stream2, err := f.client.CaptureTraffic(ctx2, &apix.CaptureRequest{})
	if err != nil {
		t.Fatalf("CaptureTraffic subscriber 2: %v", err)
	}

	// Let both server goroutines register before publishing.
	time.Sleep(30 * time.Millisecond)

	txURL, _ := url.Parse("https://multi.example.com/event")
	if err := f.eng.StoreTransaction(&proxy.Transaction{
		ID: "multi-cap-1",
		Request: &proxy.ProxyRequest{
			ID:      "multi-cap-1",
			Method:  "GET",
			URL:     txURL,
			Headers: http.Header{},
			Raw:     &http.Request{Method: "GET", URL: txURL, Header: http.Header{}},
		},
	}); err != nil {
		t.Fatalf("StoreTransaction: %v", err)
	}

	msg1, err := stream1.Recv()
	if err != nil {
		t.Fatalf("subscriber 1 Recv: %v", err)
	}
	msg2, err := stream2.Recv()
	if err != nil {
		t.Fatalf("subscriber 2 Recv: %v", err)
	}

	for i, msg := range []*apix.HttpRequest{msg1, msg2} {
		if msg.Id != "multi-cap-1" {
			t.Errorf("subscriber %d: ID got %q want multi-cap-1", i+1, msg.Id)
		}
	}

	cancel1()
	cancel2()
}
