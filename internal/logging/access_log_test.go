package logging

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEmitAccessLogJSONToFile(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "access.log")
	err := EmitAccessLog(context.Background(), AccessLogConfig{
		Enabled: true,
		Format:  "json",
		Path:    file,
	}, AccessLogEntry{
		Timestamp:    time.Unix(1700000000, 0).UTC(),
		Method:       "GET",
		URL:          "https://api.example.com/users",
		Status:       200,
		DurationMs:   45,
		RequestID:    "rid-1",
		ClientIP:     "127.0.0.1",
		RequestSize:  12,
		ResponseSize: 128,
	})
	if err != nil {
		t.Fatalf("EmitAccessLog: %v", err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	out := string(data)
	if !strings.Contains(out, `"method":"GET"`) || !strings.Contains(out, `"request_id":"rid-1"`) {
		t.Fatalf("unexpected access log output: %s", out)
	}
}

func TestEmitAccessLogCommonToFile(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "access-common.log")
	err := EmitAccessLog(context.Background(), AccessLogConfig{
		Enabled: true,
		Format:  "common",
		Path:    file,
	}, AccessLogEntry{
		Timestamp:    time.Unix(1700000000, 0).UTC(),
		Method:       "POST",
		URL:          "https://api.example.com/orders",
		Status:       201,
		DurationMs:   32,
		RequestID:    "rid-2",
		ClientIP:     "10.0.0.1",
		RequestSize:  64,
		ResponseSize: 256,
	})
	if err != nil {
		t.Fatalf("EmitAccessLog: %v", err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	out := string(data)
	if !strings.Contains(out, `"POST https://api.example.com/orders HTTP/1.1"`) {
		t.Fatalf("missing request line in common log: %s", out)
	}
	if !strings.Contains(out, "request_id=rid-2") {
		t.Fatalf("missing request_id in common log: %s", out)
	}
}
