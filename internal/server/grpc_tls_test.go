package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mnafshin/apix/internal/config"
)

func TestGRPCServerOptionsFromConfig_TLSDisabled(t *testing.T) {
	opts, err := grpcServerOptionsFromConfig(&config.Config{TLSEnabled: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(opts) != 0 {
		t.Fatalf("expected no options, got %d", len(opts))
	}
}

func TestGRPCServerOptionsFromConfig_MissingCertPath(t *testing.T) {
	_, err := grpcServerOptionsFromConfig(&config.Config{TLSEnabled: true, GRPCKeyPath: "key.pem"})
	if err == nil {
		t.Fatal("expected error for missing grpc_cert_path")
	}
	if !strings.Contains(err.Error(), "grpc_cert_path") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGRPCServerOptionsFromConfig_MissingKeyPath(t *testing.T) {
	_, err := grpcServerOptionsFromConfig(&config.Config{TLSEnabled: true, GRPCCertPath: "cert.pem"})
	if err == nil {
		t.Fatal("expected error for missing grpc_key_path")
	}
	if !strings.Contains(err.Error(), "grpc_key_path") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGRPCServerOptionsFromConfig_InvalidCertPair(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "grpc-cert.pem")
	keyPath := filepath.Join(dir, "grpc-key.pem")
	if err := os.WriteFile(certPath, []byte("bad-cert"), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, []byte("bad-key"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	_, err := grpcServerOptionsFromConfig(&config.Config{TLSEnabled: true, GRPCCertPath: certPath, GRPCKeyPath: keyPath})
	if err == nil {
		t.Fatal("expected TLS key pair load error")
	}
	if !strings.Contains(err.Error(), "failed to load cert") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGRPCServerOptionsFromConfig_ValidPair(t *testing.T) {
	certPath, keyPath := writeTLSPair(t)
	opts, err := grpcServerOptionsFromConfig(&config.Config{TLSEnabled: true, GRPCCertPath: certPath, GRPCKeyPath: keyPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(opts) != 1 {
		t.Fatalf("expected one TLS server option, got %d", len(opts))
	}
}

func writeTLSPair(t *testing.T) (certPath, keyPath string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatalf("generate serial: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	dir := t.TempDir()
	certPath = filepath.Join(dir, "grpc-cert.pem")
	keyPath = filepath.Join(dir, "grpc-key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certPath, keyPath
}
