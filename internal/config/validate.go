package config

import (
	"crypto/tls"
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
	return c.validate(false)
}

// ValidateRuntime checks the configuration for commands that inspect or operate
// against an already-running engine. It skips port-availability checks so the
// engine's own bound ports do not make `apix doctor` or `apix config show`
// report the active config as invalid.
func (c *Config) ValidateRuntime() error {
	return c.validate(true)
}

func (c *Config) validate(allowBoundPorts bool) error {
	var errs []error

	applyValidationDefaults(c)
	errs = append(errs, c.validatePorts(allowBoundPorts)...)
	errs = append(errs, c.validateDatabase()...)
	errs = append(errs, c.validateConnectionPool()...)
	errs = append(errs, c.validateTLSAndAuth()...)
	errs = append(errs, c.validatePluginPaths()...)
	errs = append(errs, c.validateURLPatterns()...)
	errs = append(errs, c.validateLogging()...)
	errs = append(errs, c.validateMapLocalRules()...)

	if len(errs) > 0 {
		return &ValidationError{Errs: errs}
	}
	return nil
}

func applyValidationDefaults(c *Config) {
	// Keep zero values on direct Config literals safe by restoring secure
	// defaults; explicit negative values remain invalid and are rejected below.
	if c.MaxHeadersPerRequest == 0 {
		c.MaxHeadersPerRequest = 200
	}
	if c.ProxyRateLimitPerSec == 0 {
		c.ProxyRateLimitPerSec = 1000
	}
	if c.ProxyMaxConcurrentConnections == 0 {
		c.ProxyMaxConcurrentConnections = 200
	}
	if c.MaxHeaderValueBytes == 0 {
		c.MaxHeaderValueBytes = 8 * 1024
	}
	if c.MaxTotalHeaderBytes == 0 {
		c.MaxTotalHeaderBytes = 1 * 1024 * 1024
	}
	if c.MaxURLLength == 0 {
		c.MaxURLLength = 8 * 1024
	}
}

func (c *Config) validatePorts(allowBoundPorts bool) []error {
	var errs []error
	httpPort := validateNamedPort(&errs, "http_port", c.HTTPPort, true)
	grpcPort := validateNamedPort(&errs, "grpc_port", c.GRPCPort, true)

	if c.MCPEnabled {
		validateNamedPort(&errs, "mcp_port", c.MCPPort, false)
	}

	if !allowBoundPorts && httpPort >= 1 && httpPort <= 65535 {
		if conflictErr := checkPortAvailable("tcp", c.HTTPPort); conflictErr != nil {
			errs = append(errs, fmt.Errorf("http_port %s is already in use: %w — stop the conflicting process or choose a different port", c.HTTPPort, conflictErr))
		}
	}
	if !allowBoundPorts && grpcPort >= 1 && grpcPort <= 65535 {
		if conflictErr := checkPortAvailable("tcp", c.GRPCPort); conflictErr != nil {
			errs = append(errs, fmt.Errorf("grpc_port %s is already in use: %w — stop the conflicting process or choose a different port", c.GRPCPort, conflictErr))
		}
	}
	if c.GRPCBindAddress != "" {
		if ip := net.ParseIP(c.GRPCBindAddress); ip == nil && c.GRPCBindAddress != "localhost" {
			errs = append(errs, fmt.Errorf("grpc_bind_address %q is invalid — use an IP address, localhost, 0.0.0.0, or ::1", c.GRPCBindAddress))
		}
	}
	if c.MCPEnabled && !allowBoundPorts && c.MCPPort != "" {
		if _, err := strconv.Atoi(c.MCPPort); err == nil {
			if conflictErr := checkPortAvailable("tcp", c.MCPPort); conflictErr != nil {
				errs = append(errs, fmt.Errorf("mcp_port %s is already in use: %w — stop the conflicting process or choose a different port", c.MCPPort, conflictErr))
			}
		}
	}

	return errs
}

func validateNamedPort(errs *[]error, field, value string, required bool) int {
	if value == "" {
		if required {
			*errs = append(*errs, fmt.Errorf("%s must be set", field))
		} else {
			*errs = append(*errs, fmt.Errorf("%s must be set when mcp_enabled is true", field))
		}
		return 0
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("invalid %s %q: %w — must be a number between 1-65535", field, value, err))
		return 0
	}
	if parsed < 1 || parsed > 65535 {
		*errs = append(*errs, fmt.Errorf("invalid %s %q: must be a number between 1-65535", field, value))
	}
	return parsed
}

func (c *Config) validateDatabase() []error {
	if c.DBPath == "" {
		return []error{fmt.Errorf("db_path must be set — e.g. db_path: apix.db")}
	}
	return nil
}

func (c *Config) validateConnectionPool() []error {
	var errs []error

	if c.MaxIdleConnsPerHost <= 0 {
		errs = append(errs, fmt.Errorf("max_idle_conns_per_host must be > 0 (got %d) — recommended value: 10", c.MaxIdleConnsPerHost))
	}
	if c.ProxyRateLimitPerSec <= 0 {
		errs = append(errs, fmt.Errorf("proxy_rate_limit_per_sec must be > 0 (got %d)", c.ProxyRateLimitPerSec))
	}
	if c.ProxyMaxConcurrentConnections <= 0 {
		errs = append(errs, fmt.Errorf("proxy_max_concurrent_connections must be > 0 (got %d)", c.ProxyMaxConcurrentConnections))
	}
	if c.MaxHeadersPerRequest <= 0 {
		errs = append(errs, fmt.Errorf("max_headers_per_request must be > 0 (got %d)", c.MaxHeadersPerRequest))
	}
	if c.MaxHeaderValueBytes <= 0 {
		errs = append(errs, fmt.Errorf("max_header_value_bytes must be > 0 (got %d)", c.MaxHeaderValueBytes))
	}
	if c.MaxTotalHeaderBytes <= 0 {
		errs = append(errs, fmt.Errorf("max_total_header_bytes must be > 0 (got %d)", c.MaxTotalHeaderBytes))
	}
	if c.MaxURLLength <= 0 {
		errs = append(errs, fmt.Errorf("max_url_length must be > 0 (got %d)", c.MaxURLLength))
	}
	if c.MaxBodySizeMB < 0 {
		errs = append(errs, fmt.Errorf("max_body_size_mb must be >= 0 (got %d) — use 0 to disable the limit", c.MaxBodySizeMB))
	}

	return errs
}

func (c *Config) validateTLSAndAuth() []error {
	var errs []error

	if strings.TrimSpace(c.AuthTokenFile) != "" {
		if _, err := os.Stat(c.AuthTokenFile); os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("auth_token_file %q does not exist", c.AuthTokenFile))
		}
	}
	if c.AuthTokenRequireStrictPerms && c.authTokenSetInConfigFile && c.configPermsTooOpen {
		errs = append(errs, fmt.Errorf("auth_token is set in %s but file mode is %04o (must be 0600 or stricter)", c.configPath, c.configFileMode))
	}
	if c.TLSEnabled && c.AuthToken == "" {
		errs = append(errs, fmt.Errorf("tls_enabled is true but auth_token is empty — set APIX_AUTH_TOKEN or auth_token in config for secure operation"))
	}
	if c.TLSEnabled {
		if c.GRPCCertPath == "" {
			errs = append(errs, fmt.Errorf("tls_enabled is true but grpc_cert_path is empty — provide the gRPC server TLS certificate path"))
		} else if _, err := os.Stat(c.GRPCCertPath); os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("grpc_cert_path %q does not exist — check the path or generate a server certificate", c.GRPCCertPath))
		}
		if c.GRPCKeyPath == "" {
			errs = append(errs, fmt.Errorf("tls_enabled is true but grpc_key_path is empty — provide the gRPC server TLS private key path"))
		} else if _, err := os.Stat(c.GRPCKeyPath); os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("grpc_key_path %q does not exist — check the path or generate a server key", c.GRPCKeyPath))
		}
		if c.GRPCCertPath != "" && c.GRPCKeyPath != "" {
			if _, err := tls.LoadX509KeyPair(c.GRPCCertPath, c.GRPCKeyPath); err != nil {
				errs = append(errs, fmt.Errorf("grpc_cert_path/grpc_key_path cannot be loaded as a TLS key pair: %w", err))
			}
		}
	}

	if grpcBindRequiresRemoteSecurity(c.GRPCBindAddress) {
		if !c.TLSEnabled {
			errs = append(errs, fmt.Errorf("grpc_bind_address %q exposes gRPC remotely but tls_enabled is false — enable TLS before binding outside loopback", c.GRPCBindAddress))
		}
		if c.AuthToken == "" {
			errs = append(errs, fmt.Errorf("grpc_bind_address %q exposes gRPC remotely but auth_token is empty — set APIX_AUTH_TOKEN or auth_token for secure remote access", c.GRPCBindAddress))
		}
	}
	if c.MCPAllowReplay && !c.MCPEnabled {
		errs = append(errs, fmt.Errorf("mcp_allow_replay requires mcp_enabled to be true"))
	}
	if c.MCPAllowCompose && !c.MCPEnabled {
		errs = append(errs, fmt.Errorf("mcp_allow_compose requires mcp_enabled to be true"))
	}
	if c.MCPEnabled && !isLoopbackHost(c.MCPBindAddress) {
		if !c.TLSEnabled {
			errs = append(errs, fmt.Errorf("mcp_bind_address %q is non-loopback; tls_enabled must be true for remote MCP deployments", c.MCPBindAddress))
		}
		if c.AuthToken == "" {
			errs = append(errs, fmt.Errorf("mcp_bind_address %q is non-loopback; set APIX_AUTH_TOKEN or auth_token for MCP authentication", c.MCPBindAddress))
		}
	}

	return errs
}

func (c *Config) validatePluginPaths() []error {
	var errs []error

	for i, p := range c.PluginPaths {
		if p == "" {
			errs = append(errs, fmt.Errorf("plugin_paths[%d] is empty — remove the entry or provide a valid path", i))
			continue
		}
		if _, err := os.Stat(p); os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("plugin_paths[%d] %q does not exist — check the path or remove the entry", i, p))
		}
	}

	return errs
}

func (c *Config) validateURLPatterns() []error {
	var errs []error

	for i, pat := range c.URLPatterns {
		if pat == "" {
			errs = append(errs, fmt.Errorf("url_patterns[%d] is empty — remove the entry or provide a valid regex", i))
			continue
		}
		if _, err := regexp.Compile(pat); err != nil {
			errs = append(errs, fmt.Errorf("url_patterns[%d] %q is not a valid Go regexp: %w — fix the pattern syntax", i, pat, err))
		}
	}

	return errs
}

func (c *Config) validateLogging() []error {
	var errs []error

	if c.LogFormat != "" && c.LogFormat != "text" && c.LogFormat != "json" {
		errs = append(errs, fmt.Errorf("log_format %q is invalid — use \"text\" or \"json\"", c.LogFormat))
	}
	if c.LogLevel != "" {
		switch strings.ToLower(c.LogLevel) {
		case "debug", "info", "warn", "error":
		default:
			errs = append(errs, fmt.Errorf("log_level %q is invalid — use debug|info|warn|error", c.LogLevel))
		}
	}
	if c.LogMaxSizeMB < 0 {
		errs = append(errs, fmt.Errorf("log_max_size_mb must be >= 0 (got %d)", c.LogMaxSizeMB))
	}
	if c.LogMaxFiles < 0 {
		errs = append(errs, fmt.Errorf("log_max_files must be >= 0 (got %d)", c.LogMaxFiles))
	}
	if c.AccessLogFormat != "" {
		switch strings.ToLower(c.AccessLogFormat) {
		case "json", "common", "combined":
		default:
			errs = append(errs, fmt.Errorf("access_log_format %q is invalid — use json|common|combined", c.AccessLogFormat))
		}
	}
	if c.AccessLogEnabled && strings.TrimSpace(c.AccessLogPath) == "" {
		errs = append(errs, fmt.Errorf("access_log_path must be set when access_log_enabled is true"))
	}
	if c.AuditLogEnabled && strings.TrimSpace(c.AuditLogPath) == "" {
		errs = append(errs, fmt.Errorf("audit_log_path must be set when audit_log_enabled is true"))
	}
	if c.OTelEnabled {
		if strings.TrimSpace(c.OTelEndpoint) == "" {
			errs = append(errs, fmt.Errorf("otel_endpoint must be set when otel_enabled is true"))
		}
		if strings.TrimSpace(c.OTelServiceName) == "" {
			errs = append(errs, fmt.Errorf("otel_service_name must be set when otel_enabled is true"))
		}
		if c.OTelSampleRate <= 0 || c.OTelSampleRate > 1 {
			errs = append(errs, fmt.Errorf("otel_sample_rate must be > 0 and <= 1 when otel_enabled is true (got %v)", c.OTelSampleRate))
		}
	}

	return errs
}

func (c *Config) validateMapLocalRules() []error {
	var errs []error

	for i, rule := range c.MapLocalRules {
		if rule.URLPattern == "" {
			errs = append(errs, fmt.Errorf("map_local_rules[%d].url_pattern is empty — provide a valid regex", i))
		} else if _, err := regexp.Compile(rule.URLPattern); err != nil {
			errs = append(errs, fmt.Errorf("map_local_rules[%d].url_pattern %q is not a valid Go regexp: %w", i, rule.URLPattern, err))
		}
		if rule.EffectiveFilePath() == "" {
			errs = append(errs, fmt.Errorf("map_local_rules[%d].file_path is empty — provide file_path (or local_path) with a readable local file path", i))
		}
		if rule.StatusCode != 0 && (rule.StatusCode < 100 || rule.StatusCode > 599) {
			errs = append(errs, fmt.Errorf("map_local_rules[%d].status_code must be 100-599 when set (got %d)", i, rule.StatusCode))
		}
	}

	return errs
}

func grpcBindRequiresRemoteSecurity(addr string) bool {
	if addr == "" || addr == "localhost" {
		return false
	}
	ip := net.ParseIP(addr)
	if ip == nil {
		return true
	}
	return !ip.IsLoopback()
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

func isLoopbackHost(host string) bool {
	if host == "" || host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
