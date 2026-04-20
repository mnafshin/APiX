package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTemp writes content to a temp file in t.TempDir() and returns its path.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writeTemp: %v", err)
	}
	return path
}

func TestLoadConfig_Defaults(t *testing.T) {
	t.Parallel()
	cfg := LoadConfig("/nonexistent/path/config.yaml")

	if cfg.HTTPPort != "8080" {
		t.Errorf("HTTPPort: got %q want %q", cfg.HTTPPort, "8080")
	}
	if cfg.GRPCPort != "9090" {
		t.Errorf("GRPCPort: got %q want %q", cfg.GRPCPort, "9090")
	}
	if cfg.GRPCBindAddress != "127.0.0.1" {
		t.Errorf("GRPCBindAddress: got %q want 127.0.0.1", cfg.GRPCBindAddress)
	}
	if cfg.MaxBodySizeMB != 32 {
		t.Errorf("MaxBodySizeMB: got %d want 32", cfg.MaxBodySizeMB)
	}
	if cfg.HTTPIdleTimeout != 120 {
		t.Errorf("HTTPIdleTimeout: got %d want 120", cfg.HTTPIdleTimeout)
	}
	if cfg.MaxIdleConnsPerHost != 10 {
		t.Errorf("MaxIdleConnsPerHost: got %d want 10", cfg.MaxIdleConnsPerHost)
	}
	if cfg.MaxHeadersPerRequest != 200 {
		t.Errorf("MaxHeadersPerRequest: got %d want 200", cfg.MaxHeadersPerRequest)
	}
	if cfg.MaxHeaderValueBytes != 8192 {
		t.Errorf("MaxHeaderValueBytes: got %d want 8192", cfg.MaxHeaderValueBytes)
	}
	if cfg.MaxTotalHeaderBytes != 1048576 {
		t.Errorf("MaxTotalHeaderBytes: got %d want 1048576", cfg.MaxTotalHeaderBytes)
	}
	if cfg.MaxURLLength != 8192 {
		t.Errorf("MaxURLLength: got %d want 8192", cfg.MaxURLLength)
	}
	if cfg.TLSEnabled {
		t.Error("TLSEnabled: expected false by default")
	}
	if cfg.AuthToken != "" {
		t.Errorf("AuthToken: expected empty by default, got %q", cfg.AuthToken)
	}
	if cfg.MCPEnabled {
		t.Error("MCPEnabled: expected false by default")
	}
	if cfg.MCPPort != "9093" {
		t.Errorf("MCPPort: got %q want %q", cfg.MCPPort, "9093")
	}
	if cfg.MCPBindAddress != "127.0.0.1" {
		t.Errorf("MCPBindAddress: got %q want 127.0.0.1", cfg.MCPBindAddress)
	}
	if cfg.OTelEnabled {
		t.Error("OTelEnabled: expected false by default")
	}
	if cfg.OTelEndpoint != "localhost:4317" {
		t.Errorf("OTelEndpoint: got %q want localhost:4317", cfg.OTelEndpoint)
	}
	if cfg.OTelServiceName != "apix-proxy" {
		t.Errorf("OTelServiceName: got %q want apix-proxy", cfg.OTelServiceName)
	}
	if !cfg.OTelInsecure {
		t.Error("OTelInsecure: expected true by default")
	}
	if cfg.OTelSampleRate != 1.0 {
		t.Errorf("OTelSampleRate: got %v want 1.0", cfg.OTelSampleRate)
	}
	if cfg.AccessLogEnabled {
		t.Error("AccessLogEnabled: expected false by default")
	}
	if cfg.AccessLogFormat != "json" {
		t.Errorf("AccessLogFormat: got %q want json", cfg.AccessLogFormat)
	}
	if cfg.AccessLogPath != "stdout" {
		t.Errorf("AccessLogPath: got %q want stdout", cfg.AccessLogPath)
	}
}

func TestLoadConfig_YAMLOverride(t *testing.T) {
	t.Parallel()
	path := writeTemp(t, `
http_port: "9999"
grpc_port: "8888"
max_body_size_mb: 64
dial_timeout_sec: 5
max_headers_per_request: 150
max_header_value_bytes: 4096
max_total_header_bytes: 262144
max_url_length: 4096
mcp_enabled: true
mcp_port: "9100"
log_format: "json"
log_level: "debug"
otel_enabled: true
otel_endpoint: "otel-collector:4317"
otel_service_name: "apix-dev"
otel_insecure: false
otel_sample_rate: 0.25
access_log_enabled: true
access_log_format: "combined"
access_log_path: "/tmp/apix-access.log"
`)
	cfg := LoadConfig(path)

	if cfg.HTTPPort != "9999" {
		t.Errorf("HTTPPort: got %q want %q", cfg.HTTPPort, "9999")
	}
	if cfg.GRPCPort != "8888" {
		t.Errorf("GRPCPort: got %q want %q", cfg.GRPCPort, "8888")
	}
	if cfg.MaxBodySizeMB != 64 {
		t.Errorf("MaxBodySizeMB: got %d want 64", cfg.MaxBodySizeMB)
	}
	if cfg.DialTimeoutSec != 5 {
		t.Errorf("DialTimeoutSec: got %d want 5", cfg.DialTimeoutSec)
	}
	if cfg.MaxHeadersPerRequest != 150 {
		t.Errorf("MaxHeadersPerRequest: got %d want 150", cfg.MaxHeadersPerRequest)
	}
	if cfg.MaxHeaderValueBytes != 4096 {
		t.Errorf("MaxHeaderValueBytes: got %d want 4096", cfg.MaxHeaderValueBytes)
	}
	if cfg.MaxTotalHeaderBytes != 262144 {
		t.Errorf("MaxTotalHeaderBytes: got %d want 262144", cfg.MaxTotalHeaderBytes)
	}
	if cfg.MaxURLLength != 4096 {
		t.Errorf("MaxURLLength: got %d want 4096", cfg.MaxURLLength)
	}
	if !cfg.MCPEnabled {
		t.Error("MCPEnabled should be true from YAML override")
	}
	if cfg.MCPPort != "9100" {
		t.Errorf("MCPPort: got %q want %q", cfg.MCPPort, "9100")
	}
	if cfg.LogFormat != "json" {
		t.Errorf("LogFormat: got %q want json", cfg.LogFormat)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel: got %q want debug", cfg.LogLevel)
	}
	if !cfg.OTelEnabled {
		t.Error("OTelEnabled should be true from YAML override")
	}
	if cfg.OTelEndpoint != "otel-collector:4317" {
		t.Errorf("OTelEndpoint: got %q want otel-collector:4317", cfg.OTelEndpoint)
	}
	if cfg.OTelServiceName != "apix-dev" {
		t.Errorf("OTelServiceName: got %q want apix-dev", cfg.OTelServiceName)
	}
	if cfg.OTelInsecure {
		t.Error("OTelInsecure should be false from YAML override")
	}
	if cfg.OTelSampleRate != 0.25 {
		t.Errorf("OTelSampleRate: got %v want 0.25", cfg.OTelSampleRate)
	}
	if !cfg.AccessLogEnabled {
		t.Error("AccessLogEnabled should be true from YAML override")
	}
	if cfg.AccessLogFormat != "combined" {
		t.Errorf("AccessLogFormat: got %q want combined", cfg.AccessLogFormat)
	}
	if cfg.AccessLogPath != "/tmp/apix-access.log" {
		t.Errorf("AccessLogPath: got %q want /tmp/apix-access.log", cfg.AccessLogPath)
	}
	// Unspecified fields stay at defaults.
	if cfg.HTTPIdleTimeout != 120 {
		t.Errorf("HTTPIdleTimeout should stay at default 120, got %d", cfg.HTTPIdleTimeout)
	}
}

func TestLoadConfig_MCPBindAddress_DefaultsToLoopback(t *testing.T) {
	t.Parallel()
	path := writeTemp(t, `
mcp_enabled: true
mcp_bind_address: ""
`)
	cfg := LoadConfig(path)
	if cfg.MCPBindAddress != "127.0.0.1" {
		t.Errorf("MCPBindAddress: got %q want 127.0.0.1", cfg.MCPBindAddress)
	}
}

func TestLoadConfig_EnvTokenOverride(t *testing.T) {
	t.Setenv("APIX_AUTH_TOKEN", "env-secret-token")

	path := writeTemp(t, `auth_token: "file-token"`)
	cfg := LoadConfig(path)

	if cfg.AuthToken != "env-secret-token" {
		t.Errorf("AuthToken: env var should override file, got %q", cfg.AuthToken)
	}
}

func TestLoadConfig_EnvTokenWhenNoFile(t *testing.T) {
	t.Setenv("APIX_AUTH_TOKEN", "only-env-token")

	cfg := LoadConfig("/nonexistent/path.yaml")

	if cfg.AuthToken != "only-env-token" {
		t.Errorf("AuthToken: env var should be picked up with no file, got %q", cfg.AuthToken)
	}
}

func TestLoadConfig_GRPCBindAddress_DefaultsToLoopback(t *testing.T) {
	t.Parallel()
	// No TLS, no auth → must stay on 127.0.0.1 for security.
	path := writeTemp(t, `tls_enabled: false`)
	cfg := LoadConfig(path)

	if cfg.GRPCBindAddress != "127.0.0.1" {
		t.Errorf("GRPCBindAddress: got %q want 127.0.0.1 (no TLS/auth)", cfg.GRPCBindAddress)
	}
}

func TestLoadConfig_GRPCBindAddress_AllowsRemoteWhenTLSAndAuth(t *testing.T) {
	t.Setenv("APIX_AUTH_TOKEN", "secure-token")

	// Empty grpc_bind_address + TLS enabled + auth token → allow 0.0.0.0
	path := writeTemp(t, `
tls_enabled: true
grpc_bind_address: ""
`)
	cfg := LoadConfig(path)

	if cfg.GRPCBindAddress != "0.0.0.0" {
		t.Errorf("GRPCBindAddress: expected 0.0.0.0 with TLS+auth, got %q", cfg.GRPCBindAddress)
	}
}

func TestLoadConfig_GRPCBindAddress_ExplicitValueKept(t *testing.T) {
	t.Parallel()
	// If the user explicitly sets a bind address, honour it regardless.
	path := writeTemp(t, `grpc_bind_address: "192.168.1.10"`)
	cfg := LoadConfig(path)

	if cfg.GRPCBindAddress != "192.168.1.10" {
		t.Errorf("GRPCBindAddress: explicit value not preserved, got %q", cfg.GRPCBindAddress)
	}
}

func TestLoadConfig_MalformedYAML(t *testing.T) {
	t.Parallel()
	path := writeTemp(t, `{this is: [not: valid yaml`)
	// Should not panic; falls back to defaults.
	cfg := LoadConfig(path)

	if cfg.HTTPPort != "8080" {
		t.Errorf("HTTPPort: expected default after malformed YAML, got %q", cfg.HTTPPort)
	}
}

func TestLoadConfig_MapLocalRules(t *testing.T) {
	t.Parallel()
	path := writeTemp(t, `
map_local_rules:
  - url_pattern: "^https://example\\.com/mock$"
    file_path: "./mocks/mock.json"
    content_type: "application/json"
    status_code: 201
`)
	cfg := LoadConfig(path)
	if len(cfg.MapLocalRules) != 1 {
		t.Fatalf("MapLocalRules: got %d want 1", len(cfg.MapLocalRules))
	}
	rule := cfg.MapLocalRules[0]
	if rule.URLPattern != "^https://example\\.com/mock$" {
		t.Errorf("URLPattern: got %q", rule.URLPattern)
	}
	if rule.FilePath != "./mocks/mock.json" {
		t.Errorf("FilePath: got %q", rule.FilePath)
	}
	if rule.ContentType != "application/json" {
		t.Errorf("ContentType: got %q", rule.ContentType)
	}
	if rule.StatusCode != 201 {
		t.Errorf("StatusCode: got %d want 201", rule.StatusCode)
	}
}

func TestLoadConfig_MapLocalRulesLocalPathAlias(t *testing.T) {
	t.Parallel()
	path := writeTemp(t, `
map_local_rules:
  - url_pattern: "^https://example\\.com/mock$"
    local_path: "./mocks/mock.json"
`)
	cfg := LoadConfig(path)
	if len(cfg.MapLocalRules) != 1 {
		t.Fatalf("MapLocalRules: got %d want 1", len(cfg.MapLocalRules))
	}
	rule := cfg.MapLocalRules[0]
	if rule.FilePath != "" {
		t.Errorf("FilePath: got %q want empty when local_path used", rule.FilePath)
	}
	if rule.LocalPath != "./mocks/mock.json" {
		t.Errorf("LocalPath: got %q", rule.LocalPath)
	}
	if got := rule.EffectiveFilePath(); got != "./mocks/mock.json" {
		t.Errorf("EffectiveFilePath: got %q want ./mocks/mock.json", got)
	}
}

func TestDefaultPath_EnvVar(t *testing.T) {
	t.Setenv("APIX_CONFIG", "/custom/path/config.yaml")

	got := DefaultPath()
	if got != "/custom/path/config.yaml" {
		t.Errorf("DefaultPath: got %q want /custom/path/config.yaml", got)
	}
}
