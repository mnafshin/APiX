package config

import "testing"

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
	certPath, keyPath := writeTLSPair(t)

	cfg := &Config{
		HTTPPort:            httpPort,
		GRPCPort:            grpcPort,
		DBPath:              "db",
		TLSEnabled:          true,
		GRPCCertPath:        certPath,
		GRPCKeyPath:         keyPath,
		AuthToken:           "token",
		MaxIdleConnsPerHost: 1,
		MaxBodySizeMB:       0,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
}
