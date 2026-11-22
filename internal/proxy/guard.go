package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"
)

// Resolver looks names up; tests substitute a fake.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// RefusedError says the proxy would not connect to a destination. Handlers
// answer it with 403 rather than 502.
type RefusedError struct {
	Target string
	Reason string
}

func (e *RefusedError) Error() string {
	return "formnet refused to connect to " + e.Target + ": " + e.Reason
}

// destinationGuard decides where the proxy may connect and performs every
// outbound dial, so the transport, tunnels and WebSocket dials share one
// resolver path and connect to the addresses that were checked.
type destinationGuard struct {
	refuse   map[string]struct{} // ip:port, loopback-normalised
	resolver Resolver
	dialer   *net.Dialer
	extra    atomic.Pointer[map[string]struct{}] // the remote listeners, while remote capture is on
}

func newGuard(refuse []string, resolver Resolver) *destinationGuard {
	g := &destinationGuard{refuse: map[string]struct{}{}, resolver: resolver, dialer: &net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}}
	if g.resolver == nil {
		g.resolver = net.DefaultResolver
	}
	for _, entry := range refuse {
		host, port, err := net.SplitHostPort(strings.TrimSpace(entry))
		if err != nil {
			continue
		}
		if host == "localhost" {
			host = "127.0.0.1"
		}
		if ip := net.ParseIP(host); ip != nil {
			g.refuse[net.JoinHostPort(normalise(ip).String(), port)] = struct{}{}
		}
	}
	return g
}

// normalise maps the unspecified addresses, which connect locally, onto loopback.
func normalise(ip net.IP) net.IP {
	if ip.IsUnspecified() {
		if ip.To4() != nil {
			return net.IPv4(127, 0, 0, 1)
		}
		return net.IPv6loopback
	}
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	return ip
}

func isLocal(ip net.IP) bool { return ip.IsLoopback() || ip.IsUnspecified() }

func isLocalName(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	return host == "localhost" || strings.HasSuffix(host, ".localhost")
}

// resolve returns the addresses a target may be dialled at, or why not.
func (g *destinationGuard) resolve(ctx context.Context, hostPort string) ([]string, error) {
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		return nil, &RefusedError{Target: hostPort, Reason: "not a host:port"}
	}
	host = strings.Trim(host, "[]")
	if ip := net.ParseIP(host); ip != nil {
		if g.refused(ip, port) {
			return nil, &RefusedError{Target: hostPort, Reason: "that is formnet's own address"}
		}
		return []string{net.JoinHostPort(ip.String(), port)}, nil
	}
	var ips []net.IPAddr
	if isLocalName(host) {
		ips = []net.IPAddr{{IP: net.IPv4(127, 0, 0, 1)}, {IP: net.IPv6loopback}}
	} else {
		ips, err = g.resolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("lookup %s: %w", host, err)
		}
	}
	out := make([]string, 0, len(ips))
	for _, a := range ips {
		if !isLocalName(host) && isLocal(a.IP) {
			return nil, &RefusedError{Target: hostPort, Reason: "the name resolves to this computer's loopback address"}
		}
		if g.refused(a.IP, port) {
			return nil, &RefusedError{Target: hostPort, Reason: "that is formnet's own address"}
		}
		out = append(out, net.JoinHostPort(a.IP.String(), port))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("lookup %s: no addresses", host)
	}
	return out, nil
}

func (g *destinationGuard) refused(ip net.IP, port string) bool {
	_, ok := g.refuse[net.JoinHostPort(normalise(ip).String(), port)]
	return ok || g.extraRefused(ip, port)
}

// DialContext checks the target and connects to the first address that answers.
func (g *destinationGuard) DialContext(ctx context.Context, network, hostPort string) (net.Conn, error) {
	if !strings.HasPrefix(network, "tcp") {
		return nil, &RefusedError{Target: hostPort, Reason: "only tcp is proxied"}
	}
	addrs, err := g.resolve(ctx, hostPort)
	if err != nil {
		return nil, err
	}
	var last error
	for _, addr := range addrs {
		conn, err := g.dialer.DialContext(ctx, network, addr)
		if err == nil {
			return conn, nil
		}
		last = err
	}
	return nil, last
}

// statusFor maps a dial error to the status the client gets.
func statusFor(err error) int {
	var refused *RefusedError
	if errors.As(err, &refused) {
		return 403
	}
	return 502
}
