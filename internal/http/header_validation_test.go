package httputil

import "testing"

func TestHeaderNameValid(t *testing.T) {
	valid := []string{"X-Custom-Header", "Content-Type", "ETag", "X1"}
	for _, s := range valid {
		if !IsValidHeaderName(s) {
			t.Fatalf("expected valid header name %q to be valid", s)
		}
		if c, ok := CanonicalHeader(s); !ok || c == "" {
			t.Fatalf("expected canonical header for %q, got %q (ok=%v)", s, c, ok)
		}
	}
}

func TestHeaderNameInvalid(t *testing.T) {
	invalid := []string{"Bad(Header)", "Has\nNewline", "\x01", ""}
	for _, s := range invalid {
		if IsValidHeaderName(s) {
			t.Fatalf("expected invalid header name %q to be rejected", s)
		}
		if _, ok := CanonicalHeader(s); ok {
			t.Fatalf("expected CanonicalHeader to reject %q", s)
		}
	}
}

func TestHeaderValueValid(t *testing.T) {
	valid := []string{"text/plain", "gzip, deflate", "\tleading-tab", "quoted \"value\""}
	for _, v := range valid {
		if !IsValidHeaderValue(v) {
			t.Fatalf("expected valid header value %q", v)
		}
	}
}

func TestHeaderValueInvalid(t *testing.T) {
	invalid := []string{"bad\x00value", string(byte(0x7F)) + "bad"}
	for _, v := range invalid {
		if IsValidHeaderValue(v) {
			t.Fatalf("expected invalid header value %q", v)
		}
	}
}
