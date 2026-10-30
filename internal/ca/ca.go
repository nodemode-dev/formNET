// Package ca is formnet's local certificate authority: one root made on this
// computer, kept in the data folder, trusted only when the person installs
// it, and used to mint a leaf certificate per host so the proxy can open TLS.
package ca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	certFile       = "ca.pem"
	keyFile        = "ca-key.pem"
	rootLifetime   = 2 * 365 * 24 * time.Hour
	leafLifetime   = 30 * 24 * time.Hour
	trustCheckTTL  = 30 * time.Second
	trustCheckHost = "trust-check.formnet.invalid"
)

// leafCacheLimit caps the leaves kept; past it the oldest is dropped.
var leafCacheLimit = 1024

// Info describes the root for the status route and the setup screen.
type Info struct {
	Path           string    `json:"path"`
	CommonName     string    `json:"common_name"`
	Fingerprint    string    `json:"fingerprint"` // SHA-256 of the DER certificate, hex with colons
	NotAfter       time.Time `json:"not_after"`
	Trusted        bool      `json:"trusted"`         // the system trust store accepts leaves from it
	TrustCommand   string    `json:"trust_command"`   // the command that installs trust for this user, for the person to run
	TrustHint      string    `json:"trust_hint"`      // what that command does
	UntrustCommand string    `json:"untrust_command"` // the command that removes it again
}

// CA is the loaded root and its leaf cache.
type CA struct {
	path    string
	cert    *x509.Certificate
	certPEM []byte
	key     *ecdsa.PrivateKey

	mu     sync.Mutex
	leaves map[string]*tls.Certificate
	order  []string // hosts in minting order, oldest first

	trustMu      sync.Mutex
	trustChecked time.Time
	trusted      bool
}

// Load opens the root in dir, creating it on first use.
func Load(dir string) (*CA, error) {
	if dir == "" {
		return nil, errors.New("ca: the data folder is empty")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("ca: data folder: %w", err)
	}
	certPath := filepath.Join(dir, certFile)
	keyPath := filepath.Join(dir, keyFile)
	cert, certPEM, key, err := readRoot(certPath, keyPath)
	if errors.Is(err, os.ErrNotExist) {
		cert, certPEM, key, err = createRoot(certPath, keyPath)
	}
	if err != nil {
		return nil, err
	}
	return &CA{path: certPath, cert: cert, certPEM: certPEM, key: key, leaves: map[string]*tls.Certificate{}}, nil
}

func readRoot(certPath, keyPath string) (*x509.Certificate, []byte, *ecdsa.PrivateKey, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := checkKeyMode(keyPath); err != nil {
		return nil, nil, nil, err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, nil, err
	}
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, nil, nil, fmt.Errorf("ca: %s holds no certificate", certPath)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("ca: %s: %w", certPath, err)
	}
	kb, _ := pem.Decode(keyPEM)
	if kb == nil || kb.Type != "EC PRIVATE KEY" {
		return nil, nil, nil, fmt.Errorf("ca: %s holds no EC private key", keyPath)
	}
	key, err := x509.ParseECPrivateKey(kb.Bytes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("ca: %s: %w", keyPath, err)
	}
	if !key.PublicKey.Equal(cert.PublicKey) {
		return nil, nil, nil, errors.New("ca: the key file does not match the certificate")
	}
	return cert, certPEM, key, nil
}

func createRoot(certPath, keyPath string) (*x509.Certificate, []byte, *ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("ca: key: %w", err)
	}
	host, _ := os.Hostname()
	if host == "" {
		host = "this computer"
	}
	serial, err := serialNumber()
	if err != nil {
		return nil, nil, nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "formnet root on " + host, Organization: []string{"formnet"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(rootLifetime),
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLenZero:        true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("ca: root: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("ca: root: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("ca: key: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := writeNew(keyPath, keyPEM, 0o600); err != nil {
		return nil, nil, nil, fmt.Errorf("ca: write key: %w", err)
	}
	if err := writeNew(certPath, certPEM, 0o644); err != nil {
		return nil, nil, nil, fmt.Errorf("ca: write certificate: %w", err)
	}
	return cert, certPEM, key, nil
}

// writeNew creates path with mode and content, refusing to replace a file
// that already exists (or that something put there between the check and now).
func writeNew(path string, content []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func serialNumber() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	n, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("ca: serial: %w", err)
	}
	return n, nil
}

// PEM is the root certificate for download and install. The key is never returned.
func (c *CA) PEM() []byte {
	out := make([]byte, len(c.certPEM))
	copy(out, c.certPEM)
	return out
}

// Fingerprint is the SHA-256 of the root's DER bytes, upper hex with colons.
func (c *CA) Fingerprint() string {
	sum := sha256.Sum256(c.cert.Raw)
	h := strings.ToUpper(hex.EncodeToString(sum[:]))
	parts := make([]string, 0, len(h)/2)
	for i := 0; i < len(h); i += 2 {
		parts = append(parts, h[i:i+2])
	}
	return strings.Join(parts, ":")
}

// CommonName is the root's subject common name, as trust stores list it.
func (c *CA) CommonName() string { return c.cert.Subject.CommonName }

// Info describes the root.
func (c *CA) Info() Info {
	cmd, hint, untrust := trustInstructions(c.path, c.CommonName())
	return Info{
		Path:           c.path,
		CommonName:     c.CommonName(),
		Fingerprint:    c.Fingerprint(),
		NotAfter:       c.cert.NotAfter,
		Trusted:        c.Trusted(),
		TrustCommand:   cmd,
		TrustHint:      hint,
		UntrustCommand: untrust,
	}
}

// Leaf mints (or returns the cached) certificate for host, signed by the root.
func (c *CA) Leaf(host string) (*tls.Certificate, error) {
	host = normaliseHost(host)
	if host == "" {
		return nil, errors.New("ca: empty host")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if leaf, ok := c.leaves[host]; ok && time.Now().Before(leaf.Leaf.NotAfter.Add(-24*time.Hour)) {
		return leaf, nil
	}
	leaf, err := c.mint(host)
	if err != nil {
		return nil, err
	}
	if _, known := c.leaves[host]; !known {
		c.order = append(c.order, host)
	}
	c.leaves[host] = leaf
	for len(c.order) > leafCacheLimit {
		delete(c.leaves, c.order[0])
		c.order = c.order[1:]
	}
	return leaf, nil
}

func normaliseHost(host string) string {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.Trim(host, "[]")
}

func (c *CA) mint(host string) (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("ca: leaf key: %w", err)
	}
	serial, err := serialNumber()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host, Organization: []string{"formnet"}},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(leafLifetime),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(host); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.cert, &key.PublicKey, c.key)
	if err != nil {
		return nil, fmt.Errorf("ca: leaf for %s: %w", host, err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("ca: leaf for %s: %w", host, err)
	}
	return &tls.Certificate{Certificate: [][]byte{der, c.cert.Raw}, PrivateKey: key, Leaf: parsed}, nil
}

// TLSConfig serves a leaf for whatever server name a client asks for.
func (c *CA) TLSConfig() *tls.Config {
	return c.TLSConfigFor("")
}

// TLSConfigFor serves a leaf for the client's server name, or for fallback
// when the client sends none (a CONNECT host, usually).
func (c *CA) TLSConfigFor(fallback string) *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"http/1.1"},
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			host := hello.ServerName
			if host == "" {
				host = fallback
			}
			if host == "" {
				return nil, errors.New("ca: the client named no server and there is no fallback")
			}
			return c.Leaf(host)
		},
	}
}

// Trusted reports whether the system trust store accepts leaves from this
// root. The answer is cached for a short while because the check mints a
// certificate and consults the OS.
func (c *CA) Trusted() bool {
	c.trustMu.Lock()
	defer c.trustMu.Unlock()
	if !c.trustChecked.IsZero() && time.Since(c.trustChecked) < trustCheckTTL {
		return c.trusted
	}
	c.trusted = c.checkTrust()
	c.trustChecked = time.Now()
	return c.trusted
}

func (c *CA) checkTrust() bool {
	leaf, err := c.Leaf(trustCheckHost)
	if err != nil || leaf.Leaf == nil {
		return false
	}
	_, err = leaf.Leaf.Verify(x509.VerifyOptions{DNSName: trustCheckHost, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
	return err == nil
}
