package proxy

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	logging "github.com/mnafshin/apix/internal/logging"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mnafshin/apix/pkg/plugins"
)

const (
	webSocketDirectionClient = "client"
	webSocketDirectionServer = "server"
)

func isWebSocketRequest(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	connection := r.Header.Values("Connection")
	for _, value := range connection {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), "upgrade") {
				return true
			}
		}
	}
	return false
}

func webSocketTargetURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	target := *u
	switch strings.ToLower(target.Scheme) {
	case "https":
		target.Scheme = "wss"
	case "http", "":
		target.Scheme = "ws"
	}
	return target.String()
}

func newWebSocketDialer(transport *http.Transport, req *plugins.ProxyRequest) *websocket.Dialer {
	dialer := &websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 15 * time.Second,
	}
	if transport != nil {
		dialer.NetDialContext = transport.DialContext
		dialer.TLSClientConfig = transport.TLSClientConfig
	}
	subprotocols := requestedSubprotocols(req.Headers)
	if len(subprotocols) > 0 {
		dialer.Subprotocols = subprotocols
	}
	return dialer
}

func requestedSubprotocols(headers http.Header) []string {
	values := headers.Values("Sec-WebSocket-Protocol")
	subprotocols := make([]string, 0)
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				subprotocols = append(subprotocols, part)
			}
		}
	}
	return subprotocols
}

// copyHeadersExcluding copies src headers, skipping any header whose canonical
// name matches one of the excluded names (case-insensitive).
func copyHeadersExcluding(src http.Header, exclude ...string) http.Header {
	skip := make(map[string]struct{}, len(exclude))
	for _, h := range exclude {
		skip[http.CanonicalHeaderKey(h)] = struct{}{}
	}
	out := make(http.Header)
	for key, values := range src {
		if _, ok := skip[key]; !ok {
			out[key] = append([]string(nil), values...)
		}
	}
	return out
}

func relayWebSocket(ctx context.Context, engine TransactionStore, transactionID string, clientConn, upstreamConn *websocket.Conn) {
	relayCtx, cancelRelay := context.WithCancel(ctx)
	defer cancelRelay()

	var forwardCloseOnce sync.Once
	var shutdownOnce sync.Once
	shutdown := func() {
		shutdownOnce.Do(func() {
			_ = clientConn.Close()
			_ = upstreamConn.Close()
		})
	}
	closePeer := func(peer *websocket.Conn, messageType int, payload []byte) error {
		deadline := time.Now().Add(5 * time.Second)
		return peer.WriteControl(messageType, payload, deadline)
	}

	configureControlHandlers := func(src, dst *websocket.Conn, direction string) {
		src.SetPingHandler(func(appData string) error {
			recordWebSocketFrame(ctx, engine, transactionID, direction, websocket.PingMessage, []byte(appData))
			return closePeer(dst, websocket.PingMessage, []byte(appData))
		})
		src.SetPongHandler(func(appData string) error {
			recordWebSocketFrame(ctx, engine, transactionID, direction, websocket.PongMessage, []byte(appData))
			return closePeer(dst, websocket.PongMessage, []byte(appData))
		})
		src.SetCloseHandler(func(code int, text string) error {
			payload := websocket.FormatCloseMessage(code, text)
			recordWebSocketFrame(ctx, engine, transactionID, direction, websocket.CloseMessage, payload)
			forwardCloseOnce.Do(func() {
				if err := closePeer(dst, websocket.CloseMessage, payload); err != nil {
					logging.Warnf(ctx, "websocket relay close forward: %v", err)
				}
			})
			return nil
		})
	}

	configureControlHandlers(clientConn, upstreamConn, webSocketDirectionClient)
	configureControlHandlers(upstreamConn, clientConn, webSocketDirectionServer)

	errCh := make(chan error, 1)
	var errOnce sync.Once
	reportErr := func(err error) {
		errOnce.Do(func() {
			errCh <- err
			cancelRelay()
		})
	}
	var wg sync.WaitGroup
	relay := func(src, dst *websocket.Conn, direction string) {
		defer wg.Done()
		for {
			if relayCtx.Err() != nil {
				return
			}
			messageType, payload, err := src.ReadMessage()
			if err != nil {
				reportErr(err)
				return
			}
			recordWebSocketFrame(ctx, engine, transactionID, direction, messageType, payload)
			if err := dst.WriteMessage(messageType, payload); err != nil {
				reportErr(err)
				return
			}
		}
	}

	wg.Add(2)
	go relay(clientConn, upstreamConn, webSocketDirectionClient)
	go relay(upstreamConn, clientConn, webSocketDirectionServer)

	select {
	case <-relayCtx.Done():
	case err := <-errCh:
		if !isExpectedWebSocketClose(err) {
			logging.Warnf(ctx, "websocket relay ended: %v", err)
		}
	}
	cancelRelay()
	shutdown()
	wg.Wait()
}

func isExpectedWebSocketClose(err error) bool {
	if err == nil {
		return true
	}
	if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) {
		return true
	}
	return strings.Contains(err.Error(), "use of closed network connection")
}

func recordWebSocketFrame(ctx context.Context, engine TransactionStore, transactionID, direction string, opcode int, payload []byte) {
	if engine == nil {
		return
	}
	frame := &WebSocketFrame{
		TransactionID: transactionID,
		Direction:     direction,
		Opcode:        opcode,
		Payload:       bytes.Clone(payload),
		Timestamp:     time.Now().UTC(),
	}
	if err := engine.StoreWebSocketFrame(frame); err != nil {
		logging.Warnf(ctx, "store websocket frame: %v", err)
	}
}

type hijackableResponseWriter struct {
	conn   net.Conn
	reader *bufio.Reader
	header http.Header
	ctx    context.Context
}

func newHijackableResponseWriter(ctx context.Context, conn net.Conn, reader *bufio.Reader) *hijackableResponseWriter {
	if ctx == nil {
		ctx = context.Background()
	}
	return &hijackableResponseWriter{
		conn:   conn,
		reader: reader,
		header: make(http.Header),
		ctx:    ctx,
	}
}

func (w *hijackableResponseWriter) Header() http.Header {
	return w.header
}

func (w *hijackableResponseWriter) Write(p []byte) (int, error) {
	return w.conn.Write(p)
}

func (w *hijackableResponseWriter) WriteHeader(statusCode int) {
	body := http.StatusText(statusCode)
	response := &http.Response{
		StatusCode:    statusCode,
		Status:        fmt.Sprintf("%d %s", statusCode, http.StatusText(statusCode)),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        w.header.Clone(),
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
	}
	if err := response.Write(w.conn); err != nil {
		logging.Errorf(w.ctx, "websocket response writer: %v", err)
	}
}

func (w *hijackableResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, bufio.NewReadWriter(w.reader, bufio.NewWriter(w.conn)), nil
}
