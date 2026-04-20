package config

import (
	"context"
	logging "github.com/mnafshin/apix/internal/logging"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	HTTPPort        string `yaml:"http_port"`
	GRPCPort        string `yaml:"grpc_port"`
	GRPCBindAddress string `yaml:"grpc_bind_address"`
	DBPath          string `yaml:"db_path"`
	CACertPath      string `yaml:"ca_cert_path"`
	CAKeyPath       string `yaml:"ca_key_path"`
	TLSEnabled      bool   `yaml:"tls_enabled"`
	// GRPCCertPath and GRPCKeyPath are the TLS certificate and private key for
	// the gRPC server when tls_enabled is true. These are separate from the MITM
	// proxy CA cert/key (CACertPath/CAKeyPath). Required when tls_enabled is true.
	GRPCCertPath                     string `yaml:"grpc_cert_path"`
	GRPCKeyPath                      string `yaml:"grpc_key_path"`
	AuthToken                        string `yaml:"auth_token"`
	MaxIdleConnsPerHost              int    `yaml:"max_idle_conns_per_host"`
	IdleConnTimeoutSec               int    `yaml:"idle_conn_timeout_sec"`
	DialTimeoutSec                   int    `yaml:"dial_timeout_sec"`
	UpstreamTLSHandshakeTimeoutSec   int    `yaml:"upstream_tls_handshake_timeout_sec"`
	UpstreamResponseHeaderTimeoutSec int    `yaml:"upstream_response_header_timeout_sec"`
	UpstreamExpectContinueTimeoutSec int    `yaml:"upstream_expect_continue_timeout_sec"`
	HTTPReadHeaderTimeout            int    `yaml:"http_read_header_timeout_sec"`
	HTTPReadTimeout                  int    `yaml:"http_read_timeout_sec"`
	HTTPWriteTimeout                 int    `yaml:"http_write_timeout_sec"`
	HTTPIdleTimeout                  int    `yaml:"http_idle_timeout_sec"`
	MaxHeadersPerRequest             int    `yaml:"max_headers_per_request"`
	MaxHeaderValueBytes              int    `yaml:"max_header_value_bytes"`
	MaxTotalHeaderBytes              int    `yaml:"max_total_header_bytes"`
	MaxURLLength                     int    `yaml:"max_url_length"`
	MaxBodySizeMB                    int    `yaml:"max_body_size_mb"`
	ReplaySkipTLSVerify              bool   `yaml:"replay_skip_tls_verify"`
	// BreakpointPauseTimeoutSec is the maximum number of seconds a request will
	// be held at a breakpoint waiting for a resume decision. When the timeout
	// expires the request is forwarded unchanged. 0 means no timeout (wait
	// indefinitely — the request context deadline or a client disconnect will
	// still unblock it). Default: 120.
	BreakpointPauseTimeoutSec int `yaml:"breakpoint_pause_timeout_sec"`

	// Observability
	MetricsEnabled bool   `yaml:"metrics_enabled"`
	MetricsPort    string `yaml:"metrics_port"`
	// HealthPort is the TCP port for the lightweight HTTP health endpoint
	// that always serves GET /healthz → 200 {"status":"ok"}. Set to "" to
	// disable. Default: "9092".
	HealthPort string `yaml:"health_port"`
	// VacuumIntervalHours is how often (in hours) to run SQLite VACUUM to
	// reclaim free pages and defragment the database. 0 disables periodic
	// VACUUM. Default: 24 (once per day).
	VacuumIntervalHours int     `yaml:"vacuum_interval_hours"`
	SlowlogThresholdMs  int     `yaml:"slowlog_threshold_ms"`
	AccessLogEnabled    bool    `yaml:"access_log_enabled"`
	AccessLogFormat     string  `yaml:"access_log_format"`
	AccessLogPath       string  `yaml:"access_log_path"`
	AuditLogEnabled     bool    `yaml:"audit_log_enabled"`
	AuditLogPath        string  `yaml:"audit_log_path"`
	OTelEnabled         bool    `yaml:"otel_enabled"`
	OTelEndpoint        string  `yaml:"otel_endpoint"`
	OTelServiceName     string  `yaml:"otel_service_name"`
	OTelInsecure        bool    `yaml:"otel_insecure"`
	OTelSampleRate      float64 `yaml:"otel_sample_rate"`

	// History retention
	HistoryMaxAgeDays int `yaml:"history_max_age_days"` // 0 = keep forever
	HistoryMaxRows    int `yaml:"history_max_rows"`     // 0 = unlimited

	// Logging
	LogFormat string `yaml:"log_format"` // text|json
	LogLevel  string `yaml:"log_level"`  // debug|info|warn|error

	// GRPCRateLimitPerSec is the maximum number of gRPC unary calls allowed
	// per peer address per second. Stream RPCs count as 1 call on open.
	// 0 disables rate limiting (local desktop mode default).
	GRPCRateLimitPerSec int `yaml:"grpc_rate_limit_per_sec"`
	// MCP server settings for AI assistants (Copilot/Claude/Cursor).
	// Disabled by default and bound to loopback for local-only access.
	MCPEnabled      bool   `yaml:"mcp_enabled"`
	MCPPort         string `yaml:"mcp_port"`
	MCPBindAddress  string `yaml:"mcp_bind_address"`
	MCPAllowReplay  bool   `yaml:"mcp_allow_replay"`
	MCPAllowCompose bool   `yaml:"mcp_allow_compose"`

	// Plugin paths — each entry is a path to a plugin shared library or
	// script. Validated at startup via --config-check.
	PluginPaths []string `yaml:"plugin_paths"`

	// URLPatterns holds pre-configured URL regex patterns (e.g., allow/deny
	// lists). Each entry must be a valid Go regexp; validated at startup.
	URLPatterns []string `yaml:"url_patterns"`
	// MapLocalRules serves local files for matching request URLs.
	MapLocalRules []MapLocalRule `yaml:"map_local_rules"`
}

// MapLocalRule maps a URL regex pattern to a local file response.
type MapLocalRule struct {
	URLPattern string `yaml:"url_pattern"`
	FilePath   string `yaml:"file_path"`
	// LocalPath is a backwards-compatible alias for file_path.
	LocalPath   string `yaml:"local_path"`
	ContentType string `yaml:"content_type"`
	StatusCode  int    `yaml:"status_code"`
}

func (r MapLocalRule) EffectiveFilePath() string {
	if r.FilePath != "" {
		return r.FilePath
	}
	return r.LocalPath
}

// DefaultPath returns the config file path following these priorities:
// 1. APIX_CONFIG environment variable
// 2. ~/.apix/config.yaml
// 3. /etc/apix/config.yaml
// 4. ./config.yaml (for portable/dev installs)
func DefaultPath() string {
	// 1. Check APIX_CONFIG env var
	if path := os.Getenv("APIX_CONFIG"); path != "" {
		return path
	}

	// 2. Check ~/.apix/config.yaml
	if home, err := os.UserHomeDir(); err == nil {
		configPath := filepath.Join(home, ".apix", "config.yaml")
		if _, err := os.Stat(configPath); err == nil {
			return configPath
		}
	}

	// 3. Check /etc/apix/config.yaml
	if _, err := os.Stat("/etc/apix/config.yaml"); err == nil {
		return "/etc/apix/config.yaml"
	}

	// 4. Fall back to ./config.yaml for portable installs
	return "./config.yaml"
}

// LoadConfig reads configuration from a YAML file.
// It falls back to sane defaults if the file doesn't exist.
func LoadConfig(path string) *Config {
	home, err := os.UserHomeDir()
	if err != nil {
		logging.Warnf(context.Background(), "config: cannot determine home directory, falling back to .apix: %v", err)
		home = "."
	}
	cfg := &Config{
		HTTPPort:                         "8080",
		GRPCPort:                         "9090",
		GRPCBindAddress:                  "127.0.0.1",
		DBPath:                           "apix.db",
		CACertPath:                       filepath.Join(home, ".apix", "ca.pem"),
		CAKeyPath:                        filepath.Join(home, ".apix", "ca-key.pem"),
		TLSEnabled:                       false,
		MaxIdleConnsPerHost:              10,
		IdleConnTimeoutSec:               90,
		DialTimeoutSec:                   10,
		UpstreamTLSHandshakeTimeoutSec:   10,
		UpstreamResponseHeaderTimeoutSec: 30,
		UpstreamExpectContinueTimeoutSec: 1,
		HTTPReadHeaderTimeout:            10,
		HTTPReadTimeout:                  30,
		HTTPWriteTimeout:                 120,
		HTTPIdleTimeout:                  120,
		MaxHeadersPerRequest:             200,
		MaxHeaderValueBytes:              8 * 1024,
		MaxTotalHeaderBytes:              1 * 1024 * 1024,
		MaxURLLength:                     8 * 1024,
		MaxBodySizeMB:                    32,
		BreakpointPauseTimeoutSec:        120,
		// Observability defaults
		MetricsEnabled:      false,
		MetricsPort:         "9091",
		HealthPort:          "9092",
		VacuumIntervalHours: 24,
		GRPCRateLimitPerSec: 0, // 0 = disabled (local desktop); set to e.g. 100 for remote deployments
		SlowlogThresholdMs:  1000,
		AccessLogEnabled:    false,
		AccessLogFormat:     "json",
		AccessLogPath:       "stdout",
		AuditLogEnabled:     false,
		AuditLogPath:        "stdout",
		OTelEnabled:         false,
		OTelEndpoint:        "localhost:4317",
		OTelServiceName:     "apix-proxy",
		OTelInsecure:        true,
		OTelSampleRate:      1.0,
		LogFormat:           "text",
		LogLevel:            "info",
		MCPEnabled:          false,
		MCPPort:             "9093",
		MCPBindAddress:      "127.0.0.1",
		MCPAllowReplay:      false,
		MCPAllowCompose:     false,
	}

	// #nosec G304 -- APiX intentionally loads a user-selected config path.
	file, err := os.ReadFile(path)
	if err != nil {
		logging.Infof(context.Background(), "Config file not found (%s), using defaults: %v", path, err)
		if envToken := os.Getenv("APIX_AUTH_TOKEN"); envToken != "" {
			cfg.AuthToken = envToken
		}
		return cfg
	}

	if err := yaml.Unmarshal(file, cfg); err != nil {
		logging.Errorf(context.Background(), "Failed to parse config file, using defaults: %v", err)
		return cfg
	}

	tokenFromFile := cfg.AuthToken != ""

	// APIX_AUTH_TOKEN env var takes precedence over the config file value.
	if envToken := os.Getenv("APIX_AUTH_TOKEN"); envToken != "" {
		cfg.AuthToken = envToken
	} else if tokenFromFile {
		logging.Warnf(context.Background(), "auth_token is set in config.yaml. Consider using the APIX_AUTH_TOKEN environment variable instead to avoid storing secrets in plaintext.")
	}

	// Default to loopback only for security. Only allow 0.0.0.0 when TLS + auth are both configured for remote access
	if cfg.GRPCBindAddress == "" {
		cfg.GRPCBindAddress = "127.0.0.1"
		if cfg.TLSEnabled && cfg.AuthToken != "" {
			cfg.GRPCBindAddress = "0.0.0.0"
		}
	}
	if cfg.MCPBindAddress == "" {
		cfg.MCPBindAddress = "127.0.0.1"
	}

	return cfg
}
