package config

import "testing"

func TestValidate_ValidConfig(t *testing.T) {
	cfg := &Config{
		HTTPPort:            "8080",
		GRPCPort:            "9090",
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
