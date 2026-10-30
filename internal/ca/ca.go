// Package ca is formnet's local certificate authority: one root made on this
// computer, kept in the data folder, trusted only when the person installs
// it, and used to mint a leaf certificate per host so the proxy can open TLS.
package ca

import (
	"crypto/tls"
	"errors"
	"time"
)

// Info describes the root for the status route and the setup screen.
type Info struct {
	Path         string    `json:"path"`
	Fingerprint  string    `json:"fingerprint"` // SHA-256 of the DER certificate, hex with colons
	NotAfter     time.Time `json:"not_after"`
	Trusted      bool      `json:"trusted"`       // the system trust store accepts leaves from it
	TrustCommand string    `json:"trust_command"` // the OS command that installs trust, for the person to run
	TrustHint    string    `json:"trust_hint"`    // one sentence on what that command does
}

// CA is the loaded root and its leaf cache.
type CA struct{}

var errNotImplemented = errors.New("ca: not implemented")

// Load opens the root in dir, creating it on first use.
func Load(dir string) (*CA, error) { return nil, errNotImplemented }

// PEM is the root certificate for download and install.
func (c *CA) PEM() []byte { return nil }

// Info describes the root.
func (c *CA) Info() Info { return Info{} }

// Leaf mints (or returns the cached) certificate for host, signed by the root.
func (c *CA) Leaf(host string) (*tls.Certificate, error) { return nil, errNotImplemented }

// TLSConfig serves a leaf for whatever server name a client asks for.
func (c *CA) TLSConfig() *tls.Config { return &tls.Config{} }
