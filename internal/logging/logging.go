// Package logging provides a structured logger backed by log/slog and helpers
// for request IDs.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync/atomic"

	"github.com/google/uuid"
)

// ctxKey is a private type for context keys in this package.
type ctxKey string

const ctxKeyRequestID ctxKey = "apix.request_id"

// RequestIDHeader is the header used to carry a request ID.
const RequestIDHeader = "X-Request-ID"

// global holds the active *slog.Logger with lock-free loads/stores.
var global atomic.Pointer[slog.Logger]
var levelVar slog.LevelVar

func getLogger() *slog.Logger {
	if l := global.Load(); l != nil {
		return l
	}
	return slog.Default()
}

func setLogger(l *slog.Logger) {
	global.Store(l)
}

// Init initializes the logger with the text handler writing to w.
// If w is nil, os.Stdout is used. Equivalent to InitWithFormat(w, "text").
func Init(w io.Writer) {
	InitWithFormatAndLevel(w, "text", "info")
}

// InitWithFormat initialises the global logger.
// format must be "json" or "text" (case-insensitive); anything else defaults to text.
// If w is nil, os.Stdout is used.
func InitWithFormat(w io.Writer, format string) {
	InitWithFormatAndLevel(w, format, "info")
}

// InitWithFormatAndLevel initialises the global logger with format and level.
// format: "json" or "text" (unknown values default to text)
// level: "debug", "info", "warn", "error" (unknown values default to info)
func InitWithFormatAndLevel(w io.Writer, format string, level string) {
	if w == nil {
		w = os.Stdout
	}
	levelVar.Set(parseLevel(level))
	opts := &slog.HandlerOptions{Level: &levelVar}
	var h slog.Handler
	if format == "json" {
		h = slog.NewJSONHandler(w, opts)
	} else {
		h = slog.NewTextHandler(w, opts)
	}
	setLogger(slog.New(h))
}

func parseLevel(level string) slog.Level {
	switch level {
	case "debug", "DEBUG":
		return slog.LevelDebug
	case "warn", "WARN", "warning", "WARNING":
		return slog.LevelWarn
	case "error", "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
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

// extraArgs returns the slog key/value pairs to add to every log record.
// Currently injects request_id when present in ctx.
func extraArgs(ctx context.Context) []any {
	if rid := RequestIDFromContext(ctx); rid != "" {
		return []any{"request_id", rid}
	}
	return nil
}

// Infof logs a formatted info-level message.
func Infof(ctx context.Context, format string, args ...interface{}) {
	getLogger().InfoContext(ctx, fmt.Sprintf(format, args...), extraArgs(ctx)...)
}

// Warnf logs a formatted warning-level message.
func Warnf(ctx context.Context, format string, args ...interface{}) {
	getLogger().WarnContext(ctx, fmt.Sprintf(format, args...), extraArgs(ctx)...)
}

// Errorf logs a formatted error-level message.
func Errorf(ctx context.Context, format string, args ...interface{}) {
	getLogger().ErrorContext(ctx, fmt.Sprintf(format, args...), extraArgs(ctx)...)
}

// Debugf logs a formatted debug-level message.
func Debugf(ctx context.Context, format string, args ...interface{}) {
	getLogger().DebugContext(ctx, fmt.Sprintf(format, args...), extraArgs(ctx)...)
}

// Fatalf logs a formatted fatal-level message and exits the process.
func Fatalf(ctx context.Context, format string, args ...interface{}) {
	getLogger().ErrorContext(ctx, fmt.Sprintf(format, args...), extraArgs(ctx)...)
	os.Exit(1)
}
