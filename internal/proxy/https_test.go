package proxy_test

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHTTPSProxy_CONNECTTunnel starts a TLS mock upstream and verifies that an
// HTTP client can retrieve content through the MITM proxy via CONNECT tunnelling.
func TestHTTPSProxy_CONNECTTunnel(t *testing.T) {
	t.Parallel()

	const wantBody = "tls hello"
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(wantBody))
	}))
	t.Cleanup(upstream.Close)

	httpP, tlsP, _, bpMgr, _ := newTestStack(t)
	startAutoResume(t, bpMgr)

	// Allow the MITM proxy to trust the test TLS server's self-signed cert when
	// connecting to the upstream.
	tlsP.SetUpstreamTLSConfig(upstream.Client().Transport.(*http.Transport).TLSClientConfig)

	proxySrv := httptest.NewServer(httpP)
	t.Cleanup(proxySrv.Close)

	// Build a cert pool that trusts the proxy's CA so the client accepts the
	// MITM certificate the proxy presents during the TLS handshake.
	caPEM, err := tlsP.CACertPEM()
	if err != nil {
		t.Fatalf("CACertPEM: %v", err)
	}
	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(caPEM)

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(mustParseURL(t, proxySrv.URL)),
			TLSClientConfig: &tls.Config{
				RootCAs: caPool,
			},
		},
	}

	resp, err := client.Get(upstream.URL)
	if err != nil {
		t.Fatalf("HTTPS GET through MITM proxy: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != wantBody {
		t.Errorf("body: got %q, want %q", body, wantBody)
	}
}

// TestHTTPSProxy_MITMCertGenerated verifies that the TLS certificate presented
// by the proxy to the client is a dynamically generated MITM cert signed by the
// proxy's own CA — not the original upstream server certificate.
func TestHTTPSProxy_MITMCertGenerated(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	httpP, tlsP, _, bpMgr, _ := newTestStack(t)
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

	// Capture the leaf certificate the proxy presents during the TLS handshake.
	var peerCert *x509.Certificate
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(mustParseURL(t, proxySrv.URL)),
			TLSClientConfig: &tls.Config{
				RootCAs: caPool,
				// VerifyConnection is called after the normal chain verification
				// succeeds, giving us access to the negotiated state.
				VerifyConnection: func(cs tls.ConnectionState) error {
					if len(cs.PeerCertificates) > 0 {
						peerCert = cs.PeerCertificates[0]
					}
					return nil
				},
			},
		},
	}

	resp, err := client.Get(upstream.URL)
	if err != nil {
		t.Fatalf("HTTPS GET: %v", err)
	}
	resp.Body.Close()

	if peerCert == nil {
		t.Fatal("no peer certificate was captured during TLS handshake")
	}

	// The MITM cert must be issued by the proxy CA ("APiX CA"), not by the
	// original upstream test CA.
	if got := peerCert.Issuer.CommonName; got != "APiX CA" {
		t.Errorf("cert issuer: got %q, want %q — proxy did not present a MITM cert", got, "APiX CA")
	}

	// The cert must verify against the proxy CA pool.
	if _, err := peerCert.Verify(x509.VerifyOptions{Roots: caPool}); err != nil {
		t.Errorf("MITM cert not verifiable by proxy CA: %v", err)
	}

	// The cert must NOT verify against the upstream's original CA pool.
	upstreamPool := upstream.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	if _, err := peerCert.Verify(x509.VerifyOptions{Roots: upstreamPool}); err == nil {
		t.Error("MITM cert should not be verifiable by the upstream's original CA")
	}
}
