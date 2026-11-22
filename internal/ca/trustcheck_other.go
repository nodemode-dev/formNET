//go:build !linux

package ca

import "crypto/x509"

// storeAccepts asks the platform verifier, which reads the system store on every call on macOS and Windows.
func (c *CA) storeAccepts(leaf *x509.Certificate) bool {
	_, err := leaf.Verify(x509.VerifyOptions{DNSName: trustCheckHost, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
	return err == nil
}
