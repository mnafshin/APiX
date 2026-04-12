package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// ValidationError groups all config problems found during validation so users
// see the full list of issues in one pass rather than having to fix-and-retry.
type ValidationError struct {
	Errs []error
}

func (ve *ValidationError) Error() string {
	msgs := make([]string, len(ve.Errs))
	for i, e := range ve.Errs {
		msgs[i] = "  • " + e.Error()
	}
	return "config validation failed:\n" + strings.Join(msgs, "\n")
}

func (ve *ValidationError) Unwrap() []error { return ve.Errs }

// Validate checks all configuration invariants and returns a *ValidationError
// that aggregates every problem found. This powers the --config-check flag so
// operators see all issues at once instead of one-at-a-time.
func (c *Config) Validate() error {
	var errs []error

	// ── Port validation ────────────────────────────────────────────────────
	if c.HTTPPort == "" {
		errs = append(errs, fmt.Errorf("http_port must be set"))
	} else if _, err := strconv.Atoi(c.HTTPPort); err != nil {
		errs = append(errs, fmt.Errorf("invalid http_port %q: %w — must be a number between 1-65535", c.HTTPPort, err))
	}

	if c.GRPCPort == "" {
		errs = append(errs, fmt.Errorf("grpc_port must be set"))
	} else if _, err := strconv.Atoi(c.GRPCPort); err != nil {
		errs = append(errs, fmt.Errorf("invalid grpc_port %q: %w — must be a number between 1-65535", c.GRPCPort, err))
	}

	// ── Port conflict checks ───────────────────────────────────────────────
	// Only check when port values look valid.
	if c.HTTPPort != "" {
		if _, err := strconv.Atoi(c.HTTPPort); err == nil {
			if conflictErr := checkPortAvailable("tcp", c.HTTPPort); conflictErr != nil {
				errs = append(errs, fmt.Errorf("http_port %s is already in use: %w — stop the conflicting process or choose a different port", c.HTTPPort, conflictErr))
			}
		}
	}
	if c.GRPCPort != "" {
		if _, err := strconv.Atoi(c.GRPCPort); err == nil {
			if conflictErr := checkPortAvailable("tcp", c.GRPCPort); conflictErr != nil {
				errs = append(errs, fmt.Errorf("grpc_port %s is already in use: %w — stop the conflicting process or choose a different port", c.GRPCPort, conflictErr))
			}
		}
	}

	// ── Database path ──────────────────────────────────────────────────────
	if c.DBPath == "" {
		errs = append(errs, fmt.Errorf("db_path must be set — e.g. db_path: apix.db"))
	}

	// ── Connection pool ────────────────────────────────────────────────────
	if c.MaxIdleConnsPerHost <= 0 {
		errs = append(errs, fmt.Errorf("max_idle_conns_per_host must be > 0 (got %d) — recommended value: 10", c.MaxIdleConnsPerHost))
	}
	if c.MaxBodySizeMB < 0 {
		errs = append(errs, fmt.Errorf("max_body_size_mb must be >= 0 (got %d) — use 0 to disable the limit", c.MaxBodySizeMB))
	}

	// ── TLS / auth ─────────────────────────────────────────────────────────
	if c.TLSEnabled && c.AuthToken == "" {
		errs = append(errs, fmt.Errorf("tls_enabled is true but auth_token is empty — set APIX_AUTH_TOKEN or auth_token in config for secure operation"))
	}

	// ── Plugin paths ───────────────────────────────────────────────────────
	for i, p := range c.PluginPaths {
		if p == "" {
			errs = append(errs, fmt.Errorf("plugin_paths[%d] is empty — remove the entry or provide a valid path", i))
			continue
		}
		if _, err := os.Stat(p); os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("plugin_paths[%d] %q does not exist — check the path or remove the entry", i, p))
		}
	}

	// ── URL pattern regex validation ───────────────────────────────────────
	for i, pat := range c.URLPatterns {
		if pat == "" {
			errs = append(errs, fmt.Errorf("url_patterns[%d] is empty — remove the entry or provide a valid regex", i))
			continue
		}
		if _, err := regexp.Compile(pat); err != nil {
			errs = append(errs, fmt.Errorf("url_patterns[%d] %q is not a valid Go regexp: %w — fix the pattern syntax", i, pat, err))
		}
	}

	if len(errs) > 0 {
		return &ValidationError{Errs: errs}
	}
	return nil
}

// checkPortAvailable probes whether addr is free by attempting a temporary
// listen. It returns nil if the port is available, or an error describing the
// conflict. The listener is closed immediately after the check.
func checkPortAvailable(network, port string) error {
	ln, err := net.Listen(network, net.JoinHostPort("", port))
	if err != nil {
		return err
	}
	_ = ln.Close()
	return nil
}

// IsValidationError reports whether err (or any error in its chain) is a
// *ValidationError so callers can distinguish config problems from I/O errors.
func IsValidationError(err error) bool {
	var ve *ValidationError
	return errors.As(err, &ve)
}
