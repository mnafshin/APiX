package proxy

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CertAuthority manages a local self-signed CA and issues per-host leaf certs.
type CertAuthority struct {
	mu       sync.RWMutex
	caCert   *x509.Certificate
	caKey    *rsa.PrivateKey
	cache    map[string]*tls.Certificate
	certPath string
	keyPath  string
}

// NewCertAuthority loads or generates a CA at the given paths.
func NewCertAuthority(certPath, keyPath string) (*CertAuthority, error) {
	ca := &CertAuthority{
		cache:    make(map[string]*tls.Certificate),
		certPath: certPath,
		keyPath:  keyPath,
	}

	// Try to load existing CA from disk.
	if err := ca.load(); err == nil {
		return ca, nil
	}

	// Generate a new CA.
	if err := ca.generate(); err != nil {
		return nil, fmt.Errorf("generate CA: %w", err)
	}
	if err := ca.save(); err != nil {
		return nil, fmt.Errorf("save CA: %w", err)
	}
	return ca, nil
}

func (ca *CertAuthority) load() error {
	certPEM, err := os.ReadFile(ca.certPath)
	if err != nil {
		return err
	}
	keyPEM, err := os.ReadFile(ca.keyPath)
	if err != nil {
		return err
	}

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("parse CA key pair: %w", err)
	}
	x509Cert, err := x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		return fmt.Errorf("parse CA cert: %w", err)
	}
	rsaKey, ok := tlsCert.PrivateKey.(*rsa.PrivateKey)
	if !ok {
		return fmt.Errorf("CA key is not RSA")
	}
	ca.caCert = x509Cert
	ca.caKey = rsaKey
	return nil
}

func (ca *CertAuthority) generate() error {
	key, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return fmt.Errorf("generate RSA key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "APiX CA",
			Organization: []string{"APiX"},
		},
		NotBefore:             now,
		NotAfter:              now.Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("create CA cert: %w", err)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return err
	}
	ca.caCert = cert
	ca.caKey = key
	return nil
}

func (ca *CertAuthority) save() error {
	if err := os.MkdirAll(filepath.Dir(ca.certPath), 0o700); err != nil {
		return err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.caCert.Raw})
	if err := os.WriteFile(ca.certPath, certPEM, 0o600); err != nil {
		return err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(ca.caKey)
	if err != nil {
		return err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return os.WriteFile(ca.keyPath, keyPEM, 0o600)
}

// CertForHost returns (cached or newly generated) a TLS certificate for host.
func (ca *CertAuthority) CertForHost(host string) (*tls.Certificate, error) {
	ca.mu.RLock()
	if cert, ok := ca.cache[host]; ok {
		ca.mu.RUnlock()
		return cert, nil
	}
	ca.mu.RUnlock()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate leaf key for %s: %w", host, err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	// When host is an IP address, use IPAddresses SAN instead of DNSNames so
	// that TLS clients performing IP-based verification accept the certificate.
	if ip := net.ParseIP(host); ip != nil {
		tmpl.DNSNames = nil
		tmpl.IPAddresses = []net.IP{ip}
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, ca.caCert, &key.PublicKey, ca.caKey)
	if err != nil {
		return nil, fmt.Errorf("create leaf cert for %s: %w", host, err)
	}

	tlsCert := &tls.Certificate{
		Certificate: [][]byte{certDER, ca.caCert.Raw},
		PrivateKey:  key,
	}

	ca.mu.Lock()
	ca.cache[host] = tlsCert
	ca.mu.Unlock()
	return tlsCert, nil
}

// CACertPEM returns the PEM-encoded CA certificate for export to clients.
func (ca *CertAuthority) CACertPEM() ([]byte, error) {
	ca.mu.RLock()
	defer ca.mu.RUnlock()
	if ca.caCert == nil {
		return nil, fmt.Errorf("CA not initialised")
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.caCert.Raw}), nil
}
