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
}

func TestLoadConfig_YAMLOverride(t *testing.T) {
	t.Parallel()
	path := writeTemp(t, `
http_port: "9999"
grpc_port: "8888"
max_body_size_mb: 64
dial_timeout_sec: 5
mcp_enabled: true
mcp_port: "9100"
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
	if !cfg.MCPEnabled {
		t.Error("MCPEnabled should be true from YAML override")
	}
	if cfg.MCPPort != "9100" {
		t.Errorf("MCPPort: got %q want %q", cfg.MCPPort, "9100")
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

func TestDefaultPath_EnvVar(t *testing.T) {
	t.Setenv("APIX_CONFIG", "/custom/path/config.yaml")

	got := DefaultPath()
	if got != "/custom/path/config.yaml" {
		t.Errorf("DefaultPath: got %q want /custom/path/config.yaml", got)
	}
}
