package config

import (
	"log"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	HTTPPort string `yaml:"http_port"`
	GRPCPort string `yaml:"grpc_port"`
}

// LoadConfig reads configuration from a YAML file.
// It falls back to default ports if the file doesn't exist.
func LoadConfig(path string) *Config {
	cfg := &Config{
		HTTPPort: "8080",
		GRPCPort: "9090",
	}

	file, err := os.ReadFile(path)
	if err != nil {
		log.Printf("Config file not found, using default ports: %v", err)
		return cfg
	}

	if err := yaml.Unmarshal(file, cfg); err != nil {
		log.Printf("Failed to parse config file, using default ports: %v", err)
		return cfg
	}

	return cfg
}