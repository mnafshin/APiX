package httputil

import (
	"context"
	"net/http"
	"testing"
)

func TestHeadersToMap_FirstValueOnly(t *testing.T) {
	h := http.Header{
		"X-One":   {"first", "second"},
		"X-Empty": {},
		"X-Two":   {"only"},
	}

	got := HeadersToMap(h)

	if got["X-One"] != "first" {
		t.Fatalf("expected first value for X-One, got %q", got["X-One"])
	}
	if _, ok := got["X-Empty"]; ok {
		t.Fatal("expected empty header values to be skipped")
	}
	if got["X-Two"] != "only" {
		t.Fatalf("expected value for X-Two, got %q", got["X-Two"])
	}
}

func TestSetValidHeaders_SetsOnlyValidEntries(t *testing.T) {
	dst := make(http.Header)
	src := map[string]string{
		"x-good":      "ok",
		"bad(header)": "ok",
		"X-Bad-Value": "bad\x00value",
	}

	SetValidHeaders(context.Background(), dst, src, "test")

	if got := dst.Get("X-Good"); got != "ok" {
		t.Fatalf("expected canonicalized valid header to be set, got %q", got)
	}
	if got := dst.Get("bad(header)"); got != "" {
		t.Fatalf("expected invalid header name to be skipped, got %q", got)
	}
	if got := dst.Get("X-Bad-Value"); got != "" {
		t.Fatalf("expected invalid header value to be skipped, got %q", got)
	}
}
