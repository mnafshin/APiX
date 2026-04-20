package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type AuditLogConfig struct {
	Enabled bool
	Path    string
}

type AuditLogEntry struct {
	Timestamp time.Time
	Action    string
	Actor     string
	TargetID  string
	Details   map[string]any
}

func EmitAuditLog(_ context.Context, cfg AuditLogConfig, entry AuditLogEntry) error {
	if !cfg.Enabled {
		return nil
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}

	w, closeFn, err := accessLogWriter(cfg.Path)
	if err != nil {
		return err
	}
	if closeFn != nil {
		defer closeFn()
	}

	payload := map[string]any{
		"timestamp": entry.Timestamp.UTC().Format(time.RFC3339Nano),
		"action":    entry.Action,
		"actor":     entry.Actor,
		"target_id": entry.TargetID,
	}
	if len(entry.Details) > 0 {
		payload["details"] = entry.Details
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", b)
	return err
}
