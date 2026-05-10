package server

import (
	"crypto/x509"
	"fmt"
	"os"
)

// ClientAuthConfig holds mTLS client authentication configuration.
type ClientAuthConfig struct {
	Enabled   bool
	ClientCA  *x509.CertPool
	ClientCN  string // Expected client certificate CN (optional, "" = any)
}

// NewClientAuthConfig creates a ClientAuthConfig from CA cert file.
// If caPath is empty or file doesn't exist, returns disabled config.
func NewClientAuthConfig(caPath string) (*ClientAuthConfig, error) {
	if caPath == "" {
		return &ClientAuthConfig{Enabled: false}, nil
	}

	// Try to load the CA certificate
	caCertPEM, err := os.ReadFile(caPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &ClientAuthConfig{Enabled: false}, nil
		}
		return nil, fmt.Errorf("read client CA cert: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCertPEM) {
		return nil, fmt.Errorf("failed to parse client CA cert from %s", caPath)
	}

	return &ClientAuthConfig{
		Enabled:  true,
		ClientCA: caCertPool,
	}, nil
}

// ClientCertDN extracts the distinguished name from a client certificate.
// Returns CN, O, OU formatted as a readable identifier.
func ClientCertDN(cert *x509.Certificate) string {
	if cert == nil {
		return "unknown"
	}
	name := cert.Subject.CommonName
	if name == "" {
		name = cert.Subject.String()
	}
	return name
}
