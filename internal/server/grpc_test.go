package server_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
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
		UrlPattern: `.*example\.com.*`,
		Methods:    []string{"GET"},
		Enabled:    true,
		Label:      "test-bp",
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
