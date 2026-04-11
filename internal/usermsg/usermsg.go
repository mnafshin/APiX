package usermsg

import (
	"errors"
	"strings"
)

// UserMessage returns a concise, user-facing message for an internal error.
// It maps common low-level errors to friendly suggestions while preserving
// the original error for logging.
func UserMessage(err error) string {
	if err == nil {
		return ""
	}
	// Unwrap to find root cause
	root := err
	for {
		if un, ok := root.(interface{ Unwrap() error }); ok {
			if u := un.Unwrap(); u != nil {
				root = u
				continue
			}
		}
		break
	}
	s := root.Error()
	// Common cases with actionable messages
	switch {
	case strings.Contains(s, "address already in use") || strings.Contains(s, "bind: address"):
		return "Port already in use. Another process is listening on the configured port. Stop it or change the configured port in the config."
	case strings.Contains(s, "permission denied"):
		return "Permission denied. Try running with elevated privileges or use a different port/path."
	case strings.Contains(s, "no such file or directory"):
		return "Required file not found. Check configuration paths and file permissions."
	case strings.Contains(strings.ToLower(s), "database is locked") || strings.Contains(strings.ToLower(s), "database is busy") || strings.Contains(s, "SQLITE_BUSY"):
		return "Database is busy or locked. Ensure no other process is using the DB and try again."
	case strings.Contains(strings.ToLower(s), "invalid config") || strings.Contains(strings.ToLower(s), "validation failed"):
		return "Invalid configuration. Run 'apix --config-check' to see validation errors."
	}

	// Fallback to the original message for unknown errors.
	_ = errors.Unwrap(err) // no-op: keep for clarity and future extensibility
	return s
}
