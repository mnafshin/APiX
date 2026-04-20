package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type AccessLogConfig struct {
	Enabled bool
	Format  string
	Path    string
}

type AccessLogEntry struct {
	Timestamp    time.Time
	Method       string
	URL          string
	Status       int
	DurationMs   int64
	RequestID    string
	ClientIP     string
	RequestSize  int
	ResponseSize int
}

func EmitAccessLog(_ context.Context, cfg AccessLogConfig, entry AccessLogEntry) error {
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

	switch strings.ToLower(cfg.Format) {
	case "common":
		_, err = fmt.Fprintf(w, "%s - - [%s] \"%s %s HTTP/1.1\" %d %d request_id=%s duration_ms=%d request_size=%d\n",
			emptyDash(entry.ClientIP),
			entry.Timestamp.Format("02/Jan/2006:15:04:05 -0700"),
			entry.Method,
			entry.URL,
			entry.Status,
			entry.ResponseSize,
			emptyDash(entry.RequestID),
			entry.DurationMs,
			entry.RequestSize,
		)
		return err
	case "combined":
		_, err = fmt.Fprintf(w, "%s - - [%s] \"%s %s HTTP/1.1\" %d %d \"-\" \"-\" request_id=%s duration_ms=%d request_size=%d\n",
			emptyDash(entry.ClientIP),
			entry.Timestamp.Format("02/Jan/2006:15:04:05 -0700"),
			entry.Method,
			entry.URL,
			entry.Status,
			entry.ResponseSize,
			emptyDash(entry.RequestID),
			entry.DurationMs,
			entry.RequestSize,
		)
		return err
	default:
		payload := map[string]any{
			"ts":            entry.Timestamp.UTC().Format(time.RFC3339Nano),
			"method":        entry.Method,
			"url":           entry.URL,
			"status":        entry.Status,
			"duration_ms":   entry.DurationMs,
			"request_id":    entry.RequestID,
			"client_ip":     entry.ClientIP,
			"request_size":  entry.RequestSize,
			"response_size": entry.ResponseSize,
		}
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "%s\n", b)
		return err
	}
}

func accessLogWriter(path string) (io.Writer, func(), error) {
	switch strings.ToLower(strings.TrimSpace(path)) {
	case "", "stdout":
		return os.Stdout, nil, nil
	case "stderr":
		return os.Stderr, nil, nil
	default:
		// #nosec G304 -- access_log_path is an explicit operator-configured destination.
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, nil, err
		}
		return f, func() { _ = f.Close() }, nil
	}
}

func emptyDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "-"
	}
	return v
}
