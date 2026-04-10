// Package integration_test tests gRPC auth interceptors over real TCP connections.
package integration_test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/mnafshin/apix/internal/breakpoints"
	"github.com/mnafshin/apix/internal/config"
	"github.com/mnafshin/apix/internal/engine"
	"github.com/mnafshin/apix/internal/pluginrt"
	"github.com/mnafshin/apix/internal/replay"
	"github.com/mnafshin/apix/internal/server"
	"github.com/mnafshin/apix/internal/storage"
	apix "github.com/mnafshin/apix/pkg/api/generated"
)

// grpcStack bundles a gRPC server + client for integration testing.
type grpcStack struct {
	server   *grpc.Server
	listener net.Listener
	client   apix.EngineClient
	conn     *grpc.ClientConn
	stopCh   chan struct{}
}

// newGRPCStackWithAuth starts a gRPC server on a random TCP port with the
// given auth token (empty means no auth). Returns a connected client and
// supporting cleanup.
func newGRPCStackWithAuth(t *testing.T, authToken string) *grpcStack {
	t.Helper()

	// Create in-memory database and engine.
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	bpMgr := breakpoints.NewManager()
	rt := pluginrt.NewRuntime()
	eng := engine.New(db, bpMgr, rt)
	re := replay.NewEngine(db, nil)

	// Create config with the specified auth token.
	cfg := &config.Config{
		HTTPPort:  "8080",
		GRPCPort:  "9090",
		AuthToken: authToken,
	}

	// Create gRPC server with auth interceptors.
	grpcSrv := server.NewGRPCServer(cfg)
	apix.RegisterEngineServer(grpcSrv, server.NewEngineServer(eng, re, cfg))

	// Start server on a random TCP port.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	grpcPort := lis.Addr().(*net.TCPAddr).Port
	go grpcSrv.Serve(lis) //nolint:errcheck

	// Give the server time to start.
	time.Sleep(50 * time.Millisecond)

	// Create a client connection without credentials.
	conn, err := grpc.NewClient(
		fmt.Sprintf("127.0.0.1:%d", grpcPort),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		grpcSrv.Stop()
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	client := apix.NewEngineClient(conn)

	return &grpcStack{
		server:   grpcSrv,
		listener: lis,
		client:   client,
		conn:     conn,
		stopCh:   make(chan struct{}),
	}
}

// ctxWithToken returns a context carrying an "authorization: Bearer <token>"
// metadata header.
func ctxWithToken(token string) context.Context {
	return metadata.AppendToOutgoingContext(
		context.Background(),
		"authorization", "Bearer "+token,
	)
}

// ── Test 1: Server with no auth token allows unauthenticated requests ──────

func TestIntegration_AuthToken_EmptyTokenSkipsAuth(t *testing.T) {
	t.Parallel()

	stack := newGRPCStackWithAuth(t, "")
	defer stack.server.Stop()

	// Client calls with no credentials should succeed when AuthToken is empty
	// (local desktop mode).
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := stack.client.GetStatus(ctx, &apix.StatusRequest{})
	if err != nil {
		t.Errorf("GetStatus with no credentials should succeed in empty-token mode, got: %v", err)
	}
}

// ── Test 2: Server with auth token rejects requests with no credentials ────

func TestIntegration_AuthToken_RejectsNoCredentials(t *testing.T) {
	t.Parallel()

	stack := newGRPCStackWithAuth(t, "secret-token")
	defer stack.server.Stop()

	// Client calls with no credentials should fail.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := stack.client.GetStatus(ctx, &apix.StatusRequest{})
	if err == nil {
		t.Error("GetStatus with no credentials should fail when AuthToken is set")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got: %v", err)
	}
	if st.Code() != codes.Unauthenticated {
		t.Errorf("error code: got %v want %v", st.Code(), codes.Unauthenticated)
	}
}

// ── Test 3: Server with auth token rejects requests with wrong token ──────

func TestIntegration_AuthToken_RejectsWrongToken(t *testing.T) {
	t.Parallel()

	stack := newGRPCStackWithAuth(t, "correct-token")
	defer stack.server.Stop()

	// Client calls with a wrong token should fail.
	ctx := ctxWithToken("wrong-token")
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := stack.client.GetStatus(ctx, &apix.StatusRequest{})
	if err == nil {
		t.Error("GetStatus with wrong token should fail")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got: %v", err)
	}
	if st.Code() != codes.Unauthenticated {
		t.Errorf("error code: got %v want %v", st.Code(), codes.Unauthenticated)
	}
}

// ── Test 4: Server with auth token accepts requests with correct token ────

func TestIntegration_AuthToken_AllowsCorrectToken(t *testing.T) {
	t.Parallel()

	stack := newGRPCStackWithAuth(t, "correct-token")
	defer stack.server.Stop()

	// Client calls with the correct token should succeed.
	ctx := ctxWithToken("correct-token")
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := stack.client.GetStatus(ctx, &apix.StatusRequest{})
	if err != nil {
		t.Errorf("GetStatus with correct token should succeed, got: %v", err)
	}
}

// ── Test 5: Streaming calls also enforce auth (no credentials) ────────────

func TestIntegration_AuthToken_StreamRejectsNoCredentials(t *testing.T) {
	t.Parallel()

	stack := newGRPCStackWithAuth(t, "secret-token")
	defer stack.server.Stop()

	// Streaming call with no credentials should fail.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := stack.client.GetHistory(ctx, &apix.HistoryQuery{Limit: 10})
	if err == nil {
		// First Recv should fail.
		_, err = stream.Recv()
	}

	if err == nil {
		t.Error("streaming GetHistory with no credentials should fail when AuthToken is set")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got: %v", err)
	}
	if st.Code() != codes.Unauthenticated {
		t.Errorf("error code: got %v want %v", st.Code(), codes.Unauthenticated)
	}
}

// ── Test 6: Streaming calls accept correct auth token ────────────────────

func TestIntegration_AuthToken_StreamAllowsCorrectToken(t *testing.T) {
	t.Parallel()

	stack := newGRPCStackWithAuth(t, "secret-token")
	defer stack.server.Stop()

	// Streaming call with correct token should succeed (even if empty result).
	ctx := ctxWithToken("secret-token")
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	stream, err := stack.client.GetHistory(ctx, &apix.HistoryQuery{Limit: 10})
	if err != nil {
		t.Errorf("GetHistory with correct token should succeed, got: %v", err)
	}

	// Recv should block until EOF (no transactions in DB).
	if stream != nil {
		_, err := stream.Recv()
		// Error is expected (EOF or io.EOF), but not Unauthenticated.
		if err != nil && status.Code(err) == codes.Unauthenticated {
			t.Errorf("streaming with correct token should not return Unauthenticated: %v", err)
		}
	}
}

// ── Test 7: Verify Bearer token format is enforced ───────────────────────

func TestIntegration_AuthToken_BearerFormatEnforced(t *testing.T) {
	t.Parallel()

	stack := newGRPCStackWithAuth(t, "secret-token")
	defer stack.server.Stop()

	// Metadata with incorrect format (no "Bearer " prefix) should fail.
	ctx := metadata.AppendToOutgoingContext(
		context.Background(),
		"authorization", "secret-token", // Missing "Bearer " prefix
	)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := stack.client.GetStatus(ctx, &apix.StatusRequest{})
	if err == nil {
		t.Error("GetStatus with malformed auth header should fail")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got: %v", err)
	}
	if st.Code() != codes.Unauthenticated {
		t.Errorf("error code: got %v want %v", st.Code(), codes.Unauthenticated)
	}
}
