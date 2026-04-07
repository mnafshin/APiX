package config

import (
	"log"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	HTTPPort            string `yaml:"http_port"`
	GRPCPort            string `yaml:"grpc_port"`
	DBPath              string `yaml:"db_path"`
	CACertPath          string `yaml:"ca_cert_path"`
	CAKeyPath           string `yaml:"ca_key_path"`
	TLSEnabled          bool   `yaml:"tls_enabled"`
	AuthToken           string `yaml:"auth_token"`
	MaxIdleConnsPerHost int    `yaml:"max_idle_conns_per_host"`
	IdleConnTimeoutSec  int    `yaml:"idle_conn_timeout_sec"`
	DialTimeoutSec      int    `yaml:"dial_timeout_sec"`
}

// LoadConfig reads configuration from a YAML file.
// It falls back to sane defaults if the file doesn't exist.
func LoadConfig(path string) *Config {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("config: cannot determine home directory, falling back to .apix: %v", err)
		home = "."
	}
	cfg := &Config{
		HTTPPort:            "8080",
		GRPCPort:            "9090",
		DBPath:              "apix.db",
		CACertPath:          filepath.Join(home, ".apix", "ca.pem"),
		CAKeyPath:           filepath.Join(home, ".apix", "ca-key.pem"),
		TLSEnabled:          false,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeoutSec:  90,
		DialTimeoutSec:      10,
	}

	file, err := os.ReadFile(path)
	if err != nil {
		log.Printf("Config file not found (%s), using defaults: %v", path, err)
		if envToken := os.Getenv("APIX_AUTH_TOKEN"); envToken != "" {
			cfg.AuthToken = envToken
		}
		return cfg
	}

	if err := yaml.Unmarshal(file, cfg); err != nil {
		log.Printf("Failed to parse config file, using defaults: %v", err)
		return cfg
	}

	tokenFromFile := cfg.AuthToken != ""

	// APIX_AUTH_TOKEN env var takes precedence over the config file value.
	if envToken := os.Getenv("APIX_AUTH_TOKEN"); envToken != "" {
		cfg.AuthToken = envToken
	} else if tokenFromFile {
		log.Println("WARNING: auth_token is set in config.yaml. Consider using the APIX_AUTH_TOKEN environment variable instead to avoid storing secrets in plaintext.")
	}

	return cfg
}