// Package logging provides a small structured JSON logger and helpers for request IDs.
package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
)

// ctxKey is a private type for context keys in this package.
type ctxKey string

const ctxKeyRequestID ctxKey = "apix.request_id"

// RequestIDHeader is the header used to carry a request ID.
const RequestIDHeader = "X-Request-ID"

var logger *log.Logger

// Init initializes the logger to write JSON lines to the provided writer.
// If w is nil, os.Stdout is used.
func Init(w io.Writer) {
	if w == nil {
		w = os.Stdout
	}
	logger = log.New(w, "", 0)
}

// WithRequestID returns a new context with the provided request ID attached.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyRequestID, id)
}

// RequestIDFromContext extracts the request ID from ctx or returns empty string.
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v := ctx.Value(ctxKeyRequestID); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// EnsureRequestID returns the request ID found in header (X-Request-ID). If missing,
// a new UUID is generated, set on the header, and returned.
func EnsureRequestID(h http.Header) string {
	if h == nil {
		return uuid.NewString()
	}
	id := h.Get(RequestIDHeader)
	if id == "" {
		id = uuid.NewString()
		h.Set(RequestIDHeader, id)
	}
	return id
}

// logWithLevel emits a single JSON log line with level, timestamp, message and optional request_id.
func logWithLevel(ctx context.Context, level string, format string, args ...interface{}) {
	if logger == nil {
		Init(os.Stdout)
	}
	msg := fmt.Sprintf(format, args...)
	entry := map[string]interface{}{
		"level": level,
		"ts":    time.Now().UTC().Format(time.RFC3339Nano),
		"msg":   msg,
	}
	if rid := RequestIDFromContext(ctx); rid != "" {
		entry["request_id"] = rid
	}
	if b, err := json.Marshal(entry); err == nil {
		logger.Println(string(b))
	} else {
		// Fallback to plain text if JSON encoding fails
		logger.Printf("%s: %s", level, msg)
	}
	if level == "fatal" {
		os.Exit(1)
	}
}

// Infof logs an info-level message.
func Infof(ctx context.Context, format string, args ...interface{}) {
	logWithLevel(ctx, "info", format, args...)
}

// Warnf logs a warning-level message.
func Warnf(ctx context.Context, format string, args ...interface{}) {
	logWithLevel(ctx, "warn", format, args...)
}

// Errorf logs an error-level message.
func Errorf(ctx context.Context, format string, args ...interface{}) {
	logWithLevel(ctx, "error", format, args...)
}

// Fatalf logs a fatal message and exits the process.
func Fatalf(ctx context.Context, format string, args ...interface{}) {
	logWithLevel(ctx, "fatal", format, args...)
}
