package config

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidate_ValidConfig(t *testing.T) {
	httpPort, grpcPort := mustFreePorts(t)
	cfg := &Config{
		HTTPPort:            httpPort,
		GRPCPort:            grpcPort,
		DBPath:              "apix.db",
		MaxIdleConnsPerHost: 10,
		MaxBodySizeMB:       32,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
}

func TestValidate_InvalidHTTPPort(t *testing.T) {
	cfg := &Config{
		HTTPPort:            "notaport",
		GRPCPort:            "9090",
		DBPath:              "apix.db",
		MaxIdleConnsPerHost: 10,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected invalid http port to error")
	}
}

func TestValidate_TLSWithoutToken(t *testing.T) {
	cfg := &Config{
		HTTPPort:            "8080",
		GRPCPort:            "9090",
		DBPath:              "apix.db",
		TLSEnabled:          true,
		AuthToken:           "",
		MaxIdleConnsPerHost: 10,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation to fail when TLS enabled without auth token")
	}
}

func TestValidate_NegativeBodySize(t *testing.T) {
	cfg := &Config{
		HTTPPort:            "8080",
		GRPCPort:            "9090",
		DBPath:              "apix.db",
		MaxIdleConnsPerHost: 10,
		MaxBodySizeMB:       -5,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected negative max body size to fail validation")
	}
}

// TestValidate_AggregatesAllErrors ensures that multiple config problems are
// reported at once rather than stopping at the first error.
func TestValidate_AggregatesAllErrors(t *testing.T) {
	t.Parallel()
	// Deliberately broken: empty ports, empty db_path, bad idle conns, negative body.
	cfg := &Config{
		HTTPPort:            "",
		GRPCPort:            "",
		DBPath:              "",
		MaxIdleConnsPerHost: 0,
		MaxBodySizeMB:       -1,
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation to fail")
	}
	if !IsValidationError(err) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	ve := err.(*ValidationError)
	if len(ve.Errs) < 4 {
		t.Fatalf("expected at least 4 errors, got %d: %v", len(ve.Errs), err)
	}
}

// TestValidate_PortConflict checks that Validate reports an error when a port
// is already in use.
func TestValidate_PortConflict(t *testing.T) {
	t.Parallel()
	// Bind a port on all interfaces so our checkPortAvailable (which also uses
	// 0.0.0.0) correctly detects the conflict.
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("could not bind test listener: %v", err)
	}
	defer ln.Close()
	addrParts := strings.Split(ln.Addr().String(), ":")
	port := addrParts[len(addrParts)-1]

	cfg := &Config{
		HTTPPort:            port,
		GRPCPort:            mustFreePort(t),
		DBPath:              "apix.db",
		MaxIdleConnsPerHost: 10,
	}
	err = cfg.Validate()
	if err == nil {
		t.Fatal("expected port-conflict error")
	}
	if !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("expected 'already in use' message, got: %v", err)
	}
}

func TestValidateRuntime_AllowsBoundPorts(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not bind test listener: %v", err)
	}
	defer func() { _ = ln.Close() }()

	addrParts := strings.Split(ln.Addr().String(), ":")
	port := addrParts[len(addrParts)-1]

	cfg := &Config{
		HTTPPort:            "18080",
		GRPCPort:            port,
		DBPath:              "apix.db",
		MaxIdleConnsPerHost: 10,
	}
	if err := cfg.ValidateRuntime(); err != nil {
		t.Fatalf("expected runtime validation to allow bound grpc port, got: %v", err)
	}
}

// TestValidate_PluginPaths checks that missing plugin paths are caught.
func TestValidate_PluginPaths(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	existing := filepath.Join(tmp, "plugin.so")
	if err := os.WriteFile(existing, []byte{}, 0o600); err != nil {
		t.Fatalf("create temp plugin: %v", err)
	}

	httpPort, grpcPort := mustFreePorts(t)
	cfg := &Config{
		HTTPPort:            httpPort,
		GRPCPort:            grpcPort,
		DBPath:              "apix.db",
		MaxIdleConnsPerHost: 10,
		PluginPaths:         []string{existing, "/nonexistent/plugin.so"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for missing plugin path")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("expected 'does not exist' message, got: %v", err)
	}
	// Existing path should not produce an error, so exactly one plugin error.
	ve := err.(*ValidationError)
	pluginErrs := 0
	for _, e := range ve.Errs {
		if strings.Contains(e.Error(), "plugin_paths") {
			pluginErrs++
		}
	}
	if pluginErrs != 1 {
		t.Fatalf("expected 1 plugin path error, got %d", pluginErrs)
	}
}

// TestValidate_URLPatterns checks that invalid regex patterns are caught.
func TestValidate_URLPatterns(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		HTTPPort:            "8080",
		GRPCPort:            "9090",
		DBPath:              "apix.db",
		MaxIdleConnsPerHost: 10,
		URLPatterns:         []string{"valid.*pattern", "[invalid"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for invalid regex")
	}
	if !strings.Contains(err.Error(), "valid Go regexp") {
		t.Fatalf("expected regexp error, got: %v", err)
	}
}

// TestValidate_ValidURLPatterns ensures well-formed patterns pass.
func TestValidate_ValidURLPatterns(t *testing.T) {
	t.Parallel()
	httpPort, grpcPort := mustFreePorts(t)
	cfg := &Config{
		HTTPPort:            httpPort,
		GRPCPort:            grpcPort,
		DBPath:              "apix.db",
		MaxIdleConnsPerHost: 10,
		URLPatterns:         []string{"https://example\\.com/.*", "^/api/v[0-9]+/"},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config with URL patterns, got: %v", err)
	}
}

// TestIsValidationError verifies the helper function.
func TestIsValidationError(t *testing.T) {
	t.Parallel()
	ve := &ValidationError{Errs: []error{}}
	if !IsValidationError(ve) {
		t.Fatal("expected IsValidationError to return true for *ValidationError")
	}
}

func mustFreePorts(t *testing.T) (string, string) {
	t.Helper()
	first, closeFirst := mustReservePort(t)
	second, closeSecond := mustReservePort(t)
	closeFirst()
	closeSecond()
	return first, second
}

func mustFreePort(t *testing.T) string {
	t.Helper()
	port, closePort := mustReservePort(t)
	closePort()
	return port
}

func mustReservePort(t *testing.T) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on ephemeral port: %v", err)
	}

	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		_ = ln.Close()
		t.Fatalf("split host/port: %v", err)
	}
	return port, func() {
		_ = ln.Close()
	}
}
