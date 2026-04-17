package proxy_test

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mnafshin/apix/internal/engine"
	"github.com/mnafshin/apix/internal/storage"
)

func TestHTTPProxy_WebSocketFramesStored(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade upstream websocket: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()

		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("read upstream websocket message: %v", err)
			return
		}
		if err := conn.WriteMessage(messageType, append([]byte("echo:"), payload...)); err != nil {
			t.Errorf("write upstream websocket echo: %v", err)
		}
	}))
	t.Cleanup(upstream.Close)

	httpP, _, eng, bpMgr, _ := newTestStack(t)
	startAutoResume(t, bpMgr)

	proxySrv := httptest.NewServer(httpP)
	t.Cleanup(proxySrv.Close)

	dialer := websocket.Dialer{Proxy: http.ProxyURL(mustParseURL(t, proxySrv.URL))}
	conn, _, err := dialer.Dial(strings.Replace(upstream.URL, "http://", "ws://", 1)+"/chat", nil)
	if err != nil {
		t.Fatalf("dial websocket through proxy: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if err := conn.WriteMessage(websocket.TextMessage, []byte("hello")); err != nil {
		t.Fatalf("write websocket message: %v", err)
	}
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read websocket echo: %v", err)
	}
	if string(payload) != "echo:hello" {
		t.Fatalf("unexpected websocket echo: %q", payload)
	}

	reqID := waitForWebSocketTransaction(t, eng)
	frames := waitForWebSocketFrames(t, eng, reqID, 2)
	var sawClient, sawServer bool
	for _, frame := range frames {
		switch frame.Direction {
		case "client":
			sawClient = sawClient || string(frame.Payload) == "hello"
		case "server":
			sawServer = sawServer || string(frame.Payload) == "echo:hello"
		}
	}
	if !sawClient || !sawServer {
		t.Fatalf("expected client and server websocket frames, got %+v", frames)
	}
}

func TestHTTPSProxy_WebSocketFramesStored(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade upstream secure websocket: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()

		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("read upstream secure websocket message: %v", err)
			return
		}
		if err := conn.WriteMessage(messageType, append([]byte("secure:"), payload...)); err != nil {
			t.Errorf("write upstream secure websocket echo: %v", err)
		}
	}))
	t.Cleanup(upstream.Close)

	httpP, tlsP, eng, bpMgr, _ := newTestStack(t)
	startAutoResume(t, bpMgr)
	tlsP.SetUpstreamTLSConfig(upstream.Client().Transport.(*http.Transport).TLSClientConfig)

	proxySrv := httptest.NewServer(httpP)
	t.Cleanup(proxySrv.Close)

	caPEM, err := tlsP.CACertPEM()
	if err != nil {
		t.Fatalf("CACertPEM: %v", err)
	}
	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(caPEM)

	dialer := websocket.Dialer{
		Proxy: http.ProxyURL(mustParseURL(t, proxySrv.URL)),
		TLSClientConfig: &tls.Config{
			RootCAs: caPool,
		},
	}
	conn, _, err := dialer.Dial(strings.Replace(upstream.URL, "https://", "wss://", 1)+"/secure", nil)
	if err != nil {
		t.Fatalf("dial secure websocket through proxy: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if err := conn.WriteMessage(websocket.BinaryMessage, []byte{0x01, 0x02, 0x03}); err != nil {
		t.Fatalf("write secure websocket message: %v", err)
	}
	messageType, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read secure websocket echo: %v", err)
	}
	if messageType != websocket.BinaryMessage {
		t.Fatalf("unexpected secure websocket message type: %d", messageType)
	}
	if string(payload) != string([]byte("secure:\x01\x02\x03")) {
		t.Fatalf("unexpected secure websocket echo payload: %v", payload)
	}

	reqID := waitForWebSocketTransaction(t, eng)
	frames := waitForWebSocketFrames(t, eng, reqID, 2)
	var sawClient, sawServer bool
	for _, frame := range frames {
		if frame.Opcode != websocket.BinaryMessage {
			t.Fatalf("expected binary opcode, got %+v", frame)
		}
		switch frame.Direction {
		case "client":
			sawClient = true
		case "server":
			sawServer = true
		}
	}
	if !sawClient || !sawServer {
		t.Fatalf("expected both client and server frames, got %+v", frames)
	}
}

func waitForWebSocketTransaction(t *testing.T, eng *engine.Engine) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		reqs, resps, err := eng.DB().ListTransactions(10, 0, "", "", 101, "")
		if err == nil {
			for i, req := range reqs {
				if req != nil && strings.EqualFold(req.Headers["Upgrade"], "websocket") && i < len(resps) && resps[i] != nil {
					return req.ID
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timed out waiting for websocket transaction")
	return ""
}

func waitForWebSocketFrames(t *testing.T, eng *engine.Engine, transactionID string, wantMin int) []*storage.WebSocketFrameRecord {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		frames, err := eng.DB().ListWebSocketFrames(transactionID)
		if err == nil && len(frames) >= wantMin {
			return frames
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d websocket frames for %s", wantMin, transactionID)
	return nil
}
