package logging

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEmitAuditLog(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "audit.log")
	err := EmitAuditLog(context.Background(), AuditLogConfig{
		Enabled: true,
		Path:    path,
	}, AuditLogEntry{
		Timestamp: time.Unix(0, 0).UTC(),
		Action:    "replay_request",
		Actor:     "peer:127.0.0.1:1234",
		TargetID:  "req-1",
		Details: map[string]any{
			"method": "GET",
			"url":    "https://example.com",
		},
	})
	if err != nil {
		t.Fatalf("emit audit log: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	line := string(b)
	if !strings.Contains(line, `"action":"replay_request"`) {
		t.Fatalf("missing action field: %s", line)
	}
	if !strings.Contains(line, `"actor":"peer:127.0.0.1:1234"`) {
		t.Fatalf("missing actor field: %s", line)
	}
	if !strings.Contains(line, `"target_id":"req-1"`) {
		t.Fatalf("missing target_id field: %s", line)
	}
	if !strings.Contains(line, `"details":{"method":"GET","url":"https://example.com"}`) {
		t.Fatalf("missing details field: %s", line)
	}
}
