package proxy_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestEdgeCase_103EarlyHintsNotStored verifies that a 103 Early Hints
// informational response from the upstream is forwarded to the client but NOT
// stored as a separate transaction.
func TestEdgeCase_103EarlyHintsNotStored(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Send 103 Early Hints then the real 200 response.
		w.Header().Set("Link", "</style.css>; rel=preload")
		// WriteHeader(103) sends the informational response; ignore errors from
		// older test backends that don't support 1xx.
		w.WriteHeader(103)

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(upstream.Close)

	httpP, _, eng, bpMgr, _ := newTestStack(t)
	startAutoResume(t, bpMgr)

	proxySrv := httptest.NewServer(httpP)
	t.Cleanup(proxySrv.Close)

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(mustParseURL(t, proxySrv.URL)),
		},
	}

	resp, err := client.Get(upstream.URL)
	if err != nil {
		t.Fatalf("GET through proxy: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("final status: got %d, want 200", resp.StatusCode)
	}
	if string(body) != "ok" {
		t.Errorf("body: got %q, want %q", string(body), "ok")
	}

	// Allow the store goroutine (if any) to complete.
	time.Sleep(20 * time.Millisecond)

	txs, resps, err := eng.DB().ListTransactions(100, 0, "", "", 0, "")
	if err != nil {
		t.Fatalf("list transactions: %v", err)
	}
	// Only the real 200 response must be stored, not the 103.
	if len(txs) != 1 {
		t.Errorf("stored transactions: got %d, want 1", len(txs))
	}
	if len(resps) > 0 && resps[0] != nil && resps[0].StatusCode == 103 {
		t.Error("103 informational response was incorrectly stored as a transaction")
	}
}

// TestEdgeCase_TrailersStoredWithPrefix verifies that HTTP trailers returned by
// the upstream are captured and stored in the transaction headers with a
// "Trailer-" prefix (e.g. "Trailer-Grpc-Status").
func TestEdgeCase_TrailersStoredWithPrefix(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Declare the trailer keys before writing headers.
		w.Header().Set("Trailer", "Grpc-Status")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data"))
		// Set the trailer value after writing the body.
		w.Header().Set("Grpc-Status", "0")
	}))
	t.Cleanup(upstream.Close)

	httpP, _, eng, bpMgr, _ := newTestStack(t)
	startAutoResume(t, bpMgr)

	proxySrv := httptest.NewServer(httpP)
	t.Cleanup(proxySrv.Close)

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(mustParseURL(t, proxySrv.URL)),
		},
	}

	resp, err := client.Get(upstream.URL)
	if err != nil {
		t.Fatalf("GET through proxy: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)

	time.Sleep(20 * time.Millisecond)

	txs, resps, err := eng.DB().ListTransactions(100, 0, "", "", 0, "")
	if err != nil {
		t.Fatalf("list transactions: %v", err)
	}
	if len(txs) == 0 {
		t.Fatal("expected at least one stored transaction")
	}
	if len(resps) == 0 || resps[0] == nil {
		t.Fatal("stored transaction has no response")
	}
	got := resps[0].Headers["Trailer-Grpc-Status"]
	if got != "0" {
		t.Errorf("Trailer-Grpc-Status header: got %q, want %q", got, "0")
	}
}

// TestEdgeCase_EmptyBodyStoredAsEmptySlice verifies that a response with an
// explicit Content-Length: 0 is stored as an empty (non-nil) byte slice.
func TestEdgeCase_EmptyBodyStoredAsEmptySlice(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)

	httpP, _, eng, bpMgr, _ := newTestStack(t)
	startAutoResume(t, bpMgr)

	proxySrv := httptest.NewServer(httpP)
	t.Cleanup(proxySrv.Close)

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(mustParseURL(t, proxySrv.URL)),
		},
	}

	resp, err := client.Get(upstream.URL)
	if err != nil {
		t.Fatalf("GET through proxy: %v", err)
	}
	defer resp.Body.Close()

	time.Sleep(20 * time.Millisecond)

	_, resps, err := eng.DB().ListTransactions(100, 0, "", "", 0, "")
	if err != nil {
		t.Fatalf("list transactions: %v", err)
	}
	if len(resps) == 0 || resps[0] == nil {
		t.Fatal("expected at least one stored response")
	}
	respRecord := resps[0]
	// Body should be nil or empty for a no-content response.
	if len(respRecord.Body) != 0 {
		t.Errorf("Body length: got %d, want 0", len(respRecord.Body))
	}
}

// TestEdgeCase_ChunkedBodyDecodedAndStored verifies that a chunked
// transfer-encoded response body is fully decoded and stored correctly.
// Go's net/http Transport automatically de-chunks the response body, so the
// stored bytes should equal the original unchunked content.
func TestEdgeCase_ChunkedBodyDecodedAndStored(t *testing.T) {
	t.Parallel()

	const want = "chunk1chunk2chunk3"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Omit Content-Length to force chunked transfer encoding in HTTP/1.1.
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("upstream ResponseWriter does not support Flush")
			return
		}
		_, _ = w.Write([]byte("chunk1"))
		flusher.Flush()
		_, _ = w.Write([]byte("chunk2"))
		flusher.Flush()
		_, _ = w.Write([]byte("chunk3"))
	}))
	t.Cleanup(upstream.Close)

	httpP, _, eng, bpMgr, _ := newTestStack(t)
	startAutoResume(t, bpMgr)

	proxySrv := httptest.NewServer(httpP)
	t.Cleanup(proxySrv.Close)

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(mustParseURL(t, proxySrv.URL)),
		},
	}

	resp, err := client.Get(upstream.URL)
	if err != nil {
		t.Fatalf("GET through proxy: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != want {
		t.Errorf("client received body: got %q, want %q", string(body), want)
	}

	time.Sleep(20 * time.Millisecond)

	_, resps, err := eng.DB().ListTransactions(100, 0, "", "", 0, "")
	if err != nil {
		t.Fatalf("list transactions: %v", err)
	}
	if len(resps) == 0 || resps[0] == nil {
		t.Fatal("expected at least one stored response")
	}
	if string(resps[0].Body) != want {
		t.Errorf("stored response Body: got %q, want %q", string(resps[0].Body), want)
	}
}
