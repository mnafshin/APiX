package httputil

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestReadLimitedBody_NilReader(t *testing.T) {
	body, err := ReadLimitedBody(nil, 10)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if body != nil {
		t.Fatalf("expected nil body, got %v", body)
	}
}

func TestReadLimitedBody_WithinLimit(t *testing.T) {
	body, err := ReadLimitedBody(io.NopCloser(strings.NewReader("hello")), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != "hello" {
		t.Fatalf("unexpected body: %q", string(body))
	}
}

func TestReadLimitedBody_ExceedsLimit(t *testing.T) {
	_, err := ReadLimitedBody(io.NopCloser(strings.NewReader("012345")), 5)
	if err == nil {
		t.Fatal("expected limit error, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("expected exceeds limit error, got %v", err)
	}
}

type errReadCloser struct{}

func (errReadCloser) Read(_ []byte) (int, error) { return 0, errors.New("read failed") }
func (errReadCloser) Close() error               { return nil }

func TestReadLimitedBody_ReadError(t *testing.T) {
	_, err := ReadLimitedBody(errReadCloser{}, 10)
	if err == nil {
		t.Fatal("expected read error, got nil")
	}
	if !strings.Contains(err.Error(), "read failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}
