package ca

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLoadCreatesAndReloadsTheSameRoot(t *testing.T) {
	dir := t.TempDir()
	first, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint() != second.Fingerprint() {
		t.Fatalf("reload changed the root: %s vs %s", first.Fingerprint(), second.Fingerprint())
	}
	if !strings.HasPrefix(string(first.PEM()), "-----BEGIN CERTIFICATE-----") || strings.Contains(string(first.PEM()), "PRIVATE") {
		t.Fatal("PEM must be the certificate only")
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(filepath.Join(dir, keyFile))
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("key mode %o, want 600", st.Mode().Perm())
		}
	}
	info := first.Info()
	if info.Path != filepath.Join(dir, certFile) || len(strings.Split(info.Fingerprint, ":")) != 32 {
		t.Fatalf("info: %+v", info)
	}
	if info.Trusted {
		t.Fatal("a fresh root in a temp dir must not be trusted")
	}
	if info.TrustCommand == "" || info.TrustHint == "" || !strings.Contains(info.TrustCommand, info.Path) {
		t.Fatalf("trust instructions: %+v", info)
	}
	if until := time.Until(info.NotAfter); until < 700*24*time.Hour || until > 731*24*time.Hour {
		t.Fatalf("root lifetime is not two years: %v", info.NotAfter)
	}
	if info.CommonName != first.CommonName() || !strings.HasPrefix(info.CommonName, "formnet root on ") {
		t.Fatalf("common name: %q", info.CommonName)
	}
	namesTheRoot := strings.Contains(info.UntrustCommand, info.CommonName) || strings.Contains(info.UntrustCommand, "formnet.crt")
	if info.UntrustCommand == "" || !namesTheRoot {
		t.Fatalf("untrust: %q", info.UntrustCommand)
	}
	if !strings.Contains(info.TrustCommand, "'"+info.Path+"'") {
		t.Fatalf("path must be single-quoted: %q", info.TrustCommand)
	}
	if first.cert.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth || len(first.cert.ExtKeyUsage) != 1 {
		t.Fatalf("root ext key usage: %v", first.cert.ExtKeyUsage)
	}
}

func TestLeafChainsToRootWithSANs(t *testing.T) {
	c, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := c.Leaf("API.Example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	if len(leaf.Leaf.DNSNames) != 1 || leaf.Leaf.DNSNames[0] != "api.example.com" || len(leaf.Leaf.IPAddresses) != 0 {
		t.Fatalf("SANs: %v %v", leaf.Leaf.DNSNames, leaf.Leaf.IPAddresses)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(c.PEM())
	if _, err := leaf.Leaf.Verify(x509.VerifyOptions{DNSName: "api.example.com", Roots: roots}); err != nil {
		t.Fatalf("leaf does not chain to the root: %v", err)
	}
	if time.Until(leaf.Leaf.NotAfter) > 31*24*time.Hour {
		t.Fatalf("leaf lifetime too long: %v", leaf.Leaf.NotAfter)
	}
	again, err := c.Leaf("api.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if again != leaf {
		t.Fatal("cache miss for the same host")
	}
	ipLeaf, err := c.Leaf("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if len(ipLeaf.Leaf.IPAddresses) != 1 || !ipLeaf.Leaf.IPAddresses[0].Equal(net.ParseIP("127.0.0.1")) || len(ipLeaf.Leaf.DNSNames) != 0 {
		t.Fatalf("IP SANs: %v %v", ipLeaf.Leaf.DNSNames, ipLeaf.Leaf.IPAddresses)
	}
	if _, err := c.Leaf(""); err == nil {
		t.Fatal("empty host accepted")
	}
}

func TestTLSConfigServesTheSNIHost(t *testing.T) {
	c, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(c.PEM())
	ln, err := tls.Listen("tcp", "127.0.0.1:0", c.TLSConfigFor("fallback.example"))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_ = conn.(*tls.Conn).Handshake()
				conn.Close()
			}()
		}
	}()
	for _, name := range []string{"one.example", "two.example"} {
		conn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{ServerName: name, RootCAs: roots, MinVersion: tls.VersionTLS12})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := conn.ConnectionState().PeerCertificates[0].DNSNames[0]; got != name {
			t.Fatalf("served %s for %s", got, name)
		}
		conn.Close()
	}
	// No SNI: the fallback host is served.
	ip := ln.Addr().(*net.TCPAddr).IP.String()
	conn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{ServerName: "", RootCAs: roots, MinVersion: tls.VersionTLS12, InsecureSkipVerify: true, VerifyConnection: func(cs tls.ConnectionState) error {
		if cs.PeerCertificates[0].DNSNames[0] != "fallback.example" {
			t.Errorf("fallback served %v", cs.PeerCertificates[0].DNSNames)
		}
		return nil
	}})
	_ = ip
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}

func TestTrustInstructionsPerOS(t *testing.T) {
	path := "/data dir/it's/ca.pem"
	want := `'/data dir/it'\''s/ca.pem'`
	for _, goos := range []string{"darwin", "linux", "windows"} {
		cmd, hint, untrust := trustInstructionsFor(goos, path, "formnet root on box")
		if hint == "" || untrust == "" {
			t.Errorf("%s: hint %q untrust %q", goos, hint, untrust)
		}
		if goos == "windows" {
			want = `"/data dir/it's/ca.pem"`
		}
		if !strings.Contains(cmd, want) {
			t.Errorf("%s: path not quoted in %q", goos, cmd)
		}
		if strings.Contains(cmd, "sudo") && goos != "linux" {
			t.Errorf("%s: trust must not need sudo: %q", goos, cmd)
		}
	}
	darwin, _, darwinUntrust := trustInstructionsFor("darwin", path, "formnet root on box")
	if !strings.Contains(darwin, "-p ssl") || !strings.Contains(darwin, "login.keychain-db") || !strings.Contains(darwinUntrust, "delete-certificate -c 'formnet root on box'") {
		t.Errorf("darwin: %q / %q", darwin, darwinUntrust)
	}
	if _, _, u := trustInstructionsFor("windows", path, "formnet root on box"); !strings.Contains(u, `-delstore -user Root "formnet root on box"`) {
		t.Errorf("windows untrust: %q", u)
	}
	if cmd, hint, _ := trustInstructionsFor("plan9", path, "x"); cmd != "" || hint == "" {
		t.Errorf("unknown os: %q %q", cmd, hint)
	}
}

func TestLeafCacheDropsTheOldest(t *testing.T) {
	old := leafCacheLimit
	leafCacheLimit = 2
	t.Cleanup(func() { leafCacheLimit = old })
	c, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, _ := c.Leaf("a.example")
	c.Leaf("b.example")
	c.Leaf("c.example")
	if len(c.leaves) != 2 || len(c.order) != 2 {
		t.Fatalf("cache size %d/%d", len(c.leaves), len(c.order))
	}
	again, _ := c.Leaf("a.example")
	if again == first {
		t.Fatal("the oldest leaf should have been dropped and minted again")
	}
	if _, ok := c.leaves["b.example"]; ok {
		t.Fatal("b should now be the oldest and gone")
	}
}

func TestLoadRefusesALooseKeyFileAndNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(dir); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(filepath.Join(dir, keyFile), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := Load(dir)
		if err == nil || !strings.Contains(err.Error(), "chmod 600") {
			t.Fatalf("loose key accepted: %v", err)
		}
		_ = os.Chmod(filepath.Join(dir, keyFile), 0o600)
	}
	// A stray certificate without a key must not be overwritten by a new root.
	stray := t.TempDir()
	if err := os.WriteFile(filepath.Join(stray, certFile), []byte("not ours"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(stray); err == nil {
		t.Fatal("Load replaced a certificate it did not make")
	}
	if b, _ := os.ReadFile(filepath.Join(stray, certFile)); string(b) != "not ours" {
		t.Fatal("the stray certificate was changed")
	}
}

func TestLoadRefusesMismatchedKey(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(dir); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	if _, err := Load(other); err != nil {
		t.Fatal(err)
	}
	key, _ := os.ReadFile(filepath.Join(other, keyFile))
	if err := os.WriteFile(filepath.Join(dir, keyFile), key, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("a key that does not match the certificate was accepted")
	}
}
