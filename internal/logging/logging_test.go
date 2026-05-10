package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitWithFormatJSON(t *testing.T) {
	var buf bytes.Buffer
	InitWithFormat(&buf, "json")

	ctx := context.Background()
	Infof(ctx, "hello %s", "world")

	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("expected JSON log output, got empty string")
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatalf("expected valid JSON, got: %s — err: %v", line, err)
	}
	if entry["msg"] != "hello world" {
		t.Errorf("unexpected msg: %v", entry["msg"])
	}
}

func TestInitWithFormatText(t *testing.T) {
	var buf bytes.Buffer
	InitWithFormat(&buf, "text")

	ctx := context.Background()
	Infof(ctx, "text mode %d", 42)

	out := buf.String()
	if !strings.Contains(out, "text mode 42") {
		t.Errorf("expected log to contain message, got: %s", out)
	}
}

func TestDebugf(t *testing.T) {
	var buf bytes.Buffer
	InitWithFormatAndLevel(&buf, "json", "debug")

	ctx := WithRequestID(context.Background(), "rid-debug")
	Debugf(ctx, "debug value=%d", 7)

	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("expected debug log output, got empty string")
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatalf("expected valid JSON, got: %s — err: %v", line, err)
	}
	if entry["msg"] != "debug value=7" {
		t.Errorf("unexpected msg: %v", entry["msg"])
	}
	if entry["request_id"] != "rid-debug" {
		t.Errorf("unexpected request_id: %v", entry["request_id"])
	}
}

func TestDebugfSuppressedAtInfoLevel(t *testing.T) {
	var buf bytes.Buffer
	InitWithFormatAndLevel(&buf, "json", "info")
	Debugf(context.Background(), "hidden debug")
	if strings.TrimSpace(buf.String()) != "" {
		t.Fatalf("expected no debug output at info level, got: %s", buf.String())
	}
}

func TestInitNilWriterDefaultsToText(t *testing.T) {
	// Init(nil) should not panic; it falls back to os.Stdout
	Init(nil)
}

func TestRequestIDPropagation(t *testing.T) {
	var buf bytes.Buffer
	InitWithFormat(&buf, "json")

	ctx := WithRequestID(context.Background(), "test-rid-123")
	Warnf(ctx, "check rid")

	line := strings.TrimSpace(buf.String())
	var entry map[string]any
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatalf("invalid JSON: %s — %v", line, err)
	}
	if entry["request_id"] != "test-rid-123" {
		t.Errorf("expected request_id=test-rid-123, got: %v", entry["request_id"])
	}
}

func TestRequestIDFromContextEmpty(t *testing.T) {
	if rid := RequestIDFromContext(context.Background()); rid != "" {
		t.Errorf("expected empty, got %q", rid)
	}
	if rid := RequestIDFromContext(nil); rid != "" {
		t.Errorf("expected empty for nil ctx, got %q", rid)
	}
}

func TestEnsureRequestID(t *testing.T) {
	h := make(http.Header)
	rid1 := EnsureRequestID(h)
	if rid1 == "" {
		t.Fatal("expected non-empty request ID")
	}
	// Second call should return the same value.
	rid2 := EnsureRequestID(h)
	if rid1 != rid2 {
		t.Errorf("expected same ID on second call: %s != %s", rid1, rid2)
	}
}

func TestEnsureRequestIDNilHeader(t *testing.T) {
	rid := EnsureRequestID(nil)
	if rid == "" {
		t.Fatal("expected non-empty request ID for nil header")
	}
}

func TestOpenWriterStdStreams(t *testing.T) {
	writer, closer, err := OpenWriter("stdout", 0, 0, true)
	if err != nil {
		t.Fatalf("OpenWriter(stdout): %v", err)
	}
	if writer == nil {
		t.Fatal("expected non-nil stdout writer")
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("stdout closer.Close(): %v", err)
	}
}

func TestOpenWriterFileRotation(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "apix.log")
	writer, closer, err := OpenWriter(logPath, 1, 2, true)
	if err != nil {
		t.Fatalf("OpenWriter(file): %v", err)
	}
	if writer == nil {
		t.Fatal("expected non-nil file writer")
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("file closer.Close(): %v", err)
	}
}
