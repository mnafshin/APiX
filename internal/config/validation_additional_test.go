package config

import (
	"os"
	"testing"
)

func TestValidate_MissingFields(t *testing.T) {
	t.Parallel()
	cfg := &Config{}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation to fail for missing fields")
	}
}

func TestValidate_BadPorts(t *testing.T) {
	t.Parallel()
	cfg := &Config{HTTPPort: "notnum", GRPCPort: "also"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation to fail for bad ports")
	}
}

func TestValidate_OKWithTLSAndToken(t *testing.T) {
	t.Parallel()
	httpPort, grpcPort := mustFreePorts(t)

	// Create temp cert and key files so the existence check passes.
	certFile, err := os.CreateTemp(t.TempDir(), "grpc-cert-*.pem")
	if err != nil {
		t.Fatalf("create temp cert: %v", err)
	}
	certFile.Close()
	keyFile, err := os.CreateTemp(t.TempDir(), "grpc-key-*.pem")
	if err != nil {
		t.Fatalf("create temp key: %v", err)
	}
	keyFile.Close()

	cfg := &Config{
		HTTPPort:            httpPort,
		GRPCPort:            grpcPort,
		DBPath:              "db",
		TLSEnabled:          true,
		GRPCCertPath:        certFile.Name(),
		GRPCKeyPath:         keyFile.Name(),
		AuthToken:           "token",
		MaxIdleConnsPerHost: 1,
		MaxBodySizeMB:       0,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
}
