package config

import (
	"fmt"
	"strconv"
)

// Validate checks common configuration invariants and returns an error
// describing the first problem found. This helps catch misconfigurations
// early (and powers the --config-check CLI flag).
func (c *Config) Validate() error {
	if c.HTTPPort == "" {
		return fmt.Errorf("http_port must be set")
	}
	if _, err := strconv.Atoi(c.HTTPPort); err != nil {
		return fmt.Errorf("invalid http_port %q: %w", c.HTTPPort, err)
	}
	if c.GRPCPort == "" {
		return fmt.Errorf("grpc_port must be set")
	}
	if _, err := strconv.Atoi(c.GRPCPort); err != nil {
		return fmt.Errorf("invalid grpc_port %q: %w", c.GRPCPort, err)
	}
	if c.DBPath == "" {
		return fmt.Errorf("db_path must be set")
	}
	if c.MaxIdleConnsPerHost <= 0 {
		return fmt.Errorf("max_idle_conns_per_host must be > 0")
	}
	if c.MaxBodySizeMB < 0 {
		return fmt.Errorf("max_body_size_mb must be >= 0")
	}
	// If TLS is enabled for upstreams, require an auth token to avoid running
	// an insecure public-facing proxy by accident.
	if c.TLSEnabled && c.AuthToken == "" {
		return fmt.Errorf("tls_enabled is true but auth_token is empty; set auth_token for secure operation")
	}
	return nil
}
