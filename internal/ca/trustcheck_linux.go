package ca

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// The places Go's crypto/x509 reads the system roots from on Linux, in its order.
var (
	linuxCertFiles = []string{
		"/etc/ssl/certs/ca-certificates.crt",
		"/etc/pki/tls/certs/ca-bundle.crt",
		"/etc/ssl/ca-bundle.pem",
		"/etc/pki/tls/cacert.pem",
		"/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem",
		"/etc/ssl/cert.pem",
	}
	linuxCertDirs = []string{"/etc/ssl/certs", "/etc/pki/tls/certs"}
)

var storeCache struct {
	sync.Mutex
	sig   string
	root  []byte
	found bool
}

// storeAccepts reads the system store from disk: Go loads the Linux roots
// once per process, so the cached pool would never see a root trusted or
// removed while formnet runs. The files are read again only when their
// size or modification time changes.
func (c *CA) storeAccepts(leaf *x509.Certificate) bool {
	files, dirs := linuxCertFiles, linuxCertDirs
	if f := os.Getenv("SSL_CERT_FILE"); f != "" {
		files = []string{f}
	}
	if d := os.Getenv("SSL_CERT_DIR"); d != "" {
		dirs = strings.Split(d, ":")
	}
	sig := storeSignature(files, dirs)
	storeCache.Lock()
	defer storeCache.Unlock()
	if sig != storeCache.sig || !bytes.Equal(storeCache.root, c.cert.Raw) {
		storeCache.sig, storeCache.root = sig, c.cert.Raw
		storeCache.found = storeHolds(files, dirs, c.cert.Raw)
	}
	if !storeCache.found {
		return false
	}
	roots := x509.NewCertPool()
	roots.AddCert(c.cert)
	_, err := leaf.Verify(x509.VerifyOptions{DNSName: trustCheckHost, Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
	return err == nil
}

func storeSignature(files, dirs []string) string {
	var b strings.Builder
	for _, p := range append(append([]string(nil), files...), dirs...) {
		if fi, err := os.Stat(p); err == nil {
			fmt.Fprintf(&b, "%s %d %d\n", p, fi.Size(), fi.ModTime().UnixNano())
		}
	}
	return b.String()
}

// storeHolds reports whether the first readable bundle file, or any file in
// the folders, holds the certificate with these DER bytes.
func storeHolds(files, dirs []string, raw []byte) bool {
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if pemHolds(data, raw) {
			return true
		}
		break
	}
	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			data, err := os.ReadFile(filepath.Join(d, e.Name()))
			if err == nil && pemHolds(data, raw) {
				return true
			}
		}
	}
	return false
}

func pemHolds(data, raw []byte) bool {
	for len(data) > 0 {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			return false
		}
		if block.Type == "CERTIFICATE" && bytes.Equal(block.Bytes, raw) {
			return true
		}
	}
	return false
}
