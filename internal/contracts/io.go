package contracts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func LoadFile(path string) (*Contract, error) {
	cleanPath := filepath.Clean(path)
	data, err := os.ReadFile(cleanPath) // #nosec G304 -- operator-provided local file path.
	if err != nil {
		return nil, fmt.Errorf("read contract %q: %w", cleanPath, err)
	}
	var c Contract
	switch strings.ToLower(filepath.Ext(cleanPath)) {
	case ".json":
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("parse contract %q: %w", cleanPath, err)
		}
	default:
		if err := yaml.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("parse contract %q: %w", cleanPath, err)
		}
	}
	return &c, nil
}

func SaveYAML(path string, c *Contract) error {
	cleanPath := filepath.Clean(path)
	encoded, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode contract: %w", err)
	}
	if len(encoded) == 0 || encoded[len(encoded)-1] != '\n' {
		encoded = append(encoded, '\n')
	}
	if err := os.WriteFile(cleanPath, encoded, 0o644); err != nil {
		return fmt.Errorf("write contract %q: %w", cleanPath, err)
	}
	return nil
}

func ValidateFile(path string) (*Contract, *ValidationError, error) {
	c, err := LoadFile(path)
	if err != nil {
		return nil, nil, err
	}
	validation := Validate(c)
	if validation.HasErrors() {
		return c, validation, nil
	}
	return c, nil, nil
}

func LoadAll(paths []string) ([]*Contract, error) {
	contracts := make([]*Contract, 0, len(paths))
	for _, path := range paths {
		c, validation, err := ValidateFile(path)
		if err != nil {
			return nil, err
		}
		if validation != nil {
			return nil, fmt.Errorf("%s", validation.Error())
		}
		contracts = append(contracts, c)
	}
	return contracts, nil
}
