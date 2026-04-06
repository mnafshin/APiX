package config

import (
	"log"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	HTTPPort   string `yaml:"http_port"`
	GRPCPort   string `yaml:"grpc_port"`
	DBPath     string `yaml:"db_path"`
	CACertPath string `yaml:"ca_cert_path"`
	CAKeyPath  string `yaml:"ca_key_path"`
	TLSEnabled bool   `yaml:"tls_enabled"`
	AuthToken  string `yaml:"auth_token"`
}

// LoadConfig reads configuration from a YAML file.
// It falls back to sane defaults if the file doesn't exist.
func LoadConfig(path string) *Config {
	home, _ := os.UserHomeDir()
	cfg := &Config{
		HTTPPort:   "8080",
		GRPCPort:   "9090",
		DBPath:     "apix.db",
		CACertPath: filepath.Join(home, ".apix", "ca.pem"),
		CAKeyPath:  filepath.Join(home, ".apix", "ca-key.pem"),
		TLSEnabled: false,
	}

	file, err := os.ReadFile(path)
	if err != nil {
		log.Printf("Config file not found (%s), using defaults: %v", path, err)
		return cfg
	}

	if err := yaml.Unmarshal(file, cfg); err != nil {
		log.Printf("Failed to parse config file, using defaults: %v", err)
		return cfg
	}

	return cfg
}