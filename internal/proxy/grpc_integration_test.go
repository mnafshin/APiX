package proxy_test

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/http2"
)

func grpcFrameBytes(flag byte, payload []byte) []byte {
	out := make([]byte, 5+len(payload))
	out[0] = flag
	binary.BigEndian.PutUint32(out[1:5], uint32(len(payload)))
	copy(out[5:], payload)
	return out
}

func TestHTTPSProxy_GRPCHTTP2MetadataStored(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := strings.ToLower(r.Header.Get("Content-Type")); !strings.HasPrefix(ct, "application/grpc") {
			t.Errorf("upstream content-type: got %q want grpc", ct)
		}
		w.Header().Set("Content-Type", "application/grpc+proto")
		w.Header().Set("Trailer", "Grpc-Status,Grpc-Message")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(grpcFrameBytes(0, []byte("response-msg")))
		w.Header().Set("Grpc-Status", "0")
		w.Header().Set("Grpc-Message", "")
	}))
	upstream.EnableHTTP2 = true
	upstream.StartTLS()
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

	tr := &http.Transport{
		Proxy: http.ProxyURL(mustParseURL(t, proxySrv.URL)),
		TLSClientConfig: &tls.Config{
			RootCAs: caPool,
		},
		ForceAttemptHTTP2: true,
	}
	if err := http2.ConfigureTransport(tr); err != nil {
		t.Fatalf("ConfigureTransport: %v", err)
	}
	client := &http.Client{Transport: tr}

	reqBody := grpcFrameBytes(0, []byte("request-msg"))
	req, err := http.NewRequest(http.MethodPost, upstream.URL+"/users.UserService/GetUser", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/grpc+proto")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("gRPC request through MITM proxy: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)
	if resp.ProtoMajor != 2 {
		t.Fatalf("client protocol: got HTTP/%d.%d, want HTTP/2", resp.ProtoMajor, resp.ProtoMinor)
	}

	reqs, resps, err := eng.DB().ListTransactions(10, 0, "/users.UserService/GetUser", http.MethodPost, 0, "")
	if err != nil {
		t.Fatalf("list transactions: %v", err)
	}
	if len(reqs) == 0 || len(resps) == 0 || resps[0] == nil {
		t.Fatal("expected stored gRPC request+response transaction")
	}

	if got := reqs[0].Protocol; got != "HTTP/2.0" {
		t.Fatalf("stored request protocol: got %q want %q", got, "HTTP/2.0")
	}
	if got := reqs[0].Headers["X-Apix-Grpc-Frame-Count"]; got != "1" {
		t.Fatalf("stored request gRPC frame count: got %q want %q", got, "1")
	}
	if got := resps[0].Headers["Trailer-Grpc-Status"]; got != "0" {
		t.Fatalf("stored response grpc status trailer: got %q want %q", got, "0")
	}
	if got := resps[0].Headers["X-Apix-Grpc-Frame-Count"]; got != "1" {
		t.Fatalf("stored response gRPC frame count: got %q want %q", got, "1")
	}
}
