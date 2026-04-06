package proxy

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"path/filepath"
	"testing"
)

func TestNewCertAuthority(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	certPath := filepath.Join(dir, "ca.crt")
	keyPath := filepath.Join(dir, "ca.key")

	ca, err := NewCertAuthority(certPath, keyPath)
	if err != nil {
		t.Fatalf("NewCertAuthority: %v", err)
	}
	if ca == nil {
		t.Fatal("expected non-nil CertAuthority")
	}

	// Verify files were created.
	pemBytes, err := ca.CACertPEM()
	if err != nil {
		t.Fatalf("CACertPEM: %v", err)
	}
	if len(pemBytes) == 0 {
		t.Error("expected non-empty PEM")
	}
}

func TestLoadExistingCA(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	certPath := filepath.Join(dir, "ca.crt")
	keyPath := filepath.Join(dir, "ca.key")

	// Create CA.
	ca1, err := NewCertAuthority(certPath, keyPath)
	if err != nil {
		t.Fatalf("first NewCertAuthority: %v", err)
	}
	pem1, err := ca1.CACertPEM()
	if err != nil {
		t.Fatalf("CACertPEM (first): %v", err)
	}

	// Load from same paths.
	ca2, err := NewCertAuthority(certPath, keyPath)
	if err != nil {
		t.Fatalf("second NewCertAuthority: %v", err)
	}
	pem2, err := ca2.CACertPEM()
	if err != nil {
		t.Fatalf("CACertPEM (second): %v", err)
	}

	// Same cert bytes (not regenerated).
	if string(pem1) != string(pem2) {
		t.Error("expected same CA cert on reload, got different")
	}
}

func TestCertForHost(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ca, err := NewCertAuthority(
		filepath.Join(dir, "ca.crt"),
		filepath.Join(dir, "ca.key"),
	)
	if err != nil {
		t.Fatalf("NewCertAuthority: %v", err)
	}

	tlsCert, err := ca.CertForHost("example.com")
	if err != nil {
		t.Fatalf("CertForHost: %v", err)
	}
	if tlsCert == nil {
		t.Fatal("expected non-nil TLS cert")
	}

	// Parse the leaf cert.
	leaf, err := x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		t.Fatalf("parse leaf cert: %v", err)
	}

	if leaf.Subject.CommonName != "example.com" {
		t.Errorf("CN: got %q want %q", leaf.Subject.CommonName, "example.com")
	}

	// Check SAN.
	found := false
	for _, san := range leaf.DNSNames {
		if san == "example.com" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("SAN example.com not found in %v", leaf.DNSNames)
	}

	// Verify signed by our CA.
	caPEM, _ := ca.CACertPEM()
	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(caPEM)
	opts := x509.VerifyOptions{
		DNSName: "example.com",
		Roots:   caPool,
	}
	if _, err := leaf.Verify(opts); err != nil {
		t.Errorf("cert verification failed: %v", err)
	}
}

func TestCertForHostCaching(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ca, err := NewCertAuthority(
		filepath.Join(dir, "ca.crt"),
		filepath.Join(dir, "ca.key"),
	)
	if err != nil {
		t.Fatalf("NewCertAuthority: %v", err)
	}

	cert1, err := ca.CertForHost("cached.com")
	if err != nil {
		t.Fatalf("first CertForHost: %v", err)
	}
	cert2, err := ca.CertForHost("cached.com")
	if err != nil {
		t.Fatalf("second CertForHost: %v", err)
	}

	if cert1 != cert2 {
		t.Error("expected same *tls.Certificate pointer (cached), got different")
	}
}

func TestCACertPEM(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ca, err := NewCertAuthority(
		filepath.Join(dir, "ca.crt"),
		filepath.Join(dir, "ca.key"),
	)
	if err != nil {
		t.Fatalf("NewCertAuthority: %v", err)
	}

	pemBytes, err := ca.CACertPEM()
	if err != nil {
		t.Fatalf("CACertPEM: %v", err)
	}

	block, _ := pem.Decode(pemBytes)
	if block == nil {
		t.Fatal("failed to decode PEM block")
	}
	if block.Type != "CERTIFICATE" {
		t.Errorf("PEM type: got %q want CERTIFICATE", block.Type)
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	if !cert.IsCA {
		t.Error("expected IsCA to be true")
	}
	if cert.Subject.CommonName != "APiX CA" {
		t.Errorf("CN: got %q want %q", cert.Subject.CommonName, "APiX CA")
	}

	// Suppress unused import warning — tls is used in cert_test helpers.
	_ = tls.Certificate{}
}
