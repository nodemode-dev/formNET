// Package config reads formnet's settings from the environment.
//
// Every value has a default so `formnet` runs with nothing set:
//
//	FORMNET_DATA         data folder (CA, exports); default ~/formnet
//	FORMNET_ADDR         loopback address of the API and the web client; default 127.0.0.1:8760
//	FORMNET_PROXY_ADDR   loopback address of the intercepting proxy; default 127.0.0.1:8761
//	FORMNET_BODY_LIMIT   bytes of each request and response body kept; default 262144
//	FORMNET_FLOW_LIMIT   flows kept in memory before the oldest are dropped; default 5000
//	FORMNET_PASSTHROUGH  comma-separated host patterns tunnelled without interception; when set, wins over <data>/settings.json
//	FORMNET_MACHINE_INTERVAL  seconds between machine snapshots; default 2
//	FORMNET_REVERSE_DNS  1 to look up names for remote addresses in the machine view; default off
//	FORMNET_INTERCEPT    auto (open HTTPS only while the root is trusted), on, or off; when set, wins over <data>/settings.json; default auto
//	FORMNET_TOKEN        this run's session token; default a new random one, written to <data>/token
//	FORMNET_SYSTEM_PROXY 1 or 0: point the system proxy at formnet for this run (macOS, Windows); when set, wins over <data>/settings.json
//	FORMNET_REMOTE_CAPTURE 1 or 0: also listen for the proxy (never the API) on this computer's network addresses; when set, wins over <data>/settings.json
//	FORMNET_REMOTE_ALLOW   comma-separated addresses or CIDR ranges allowed to use that listener; when set, wins over <data>/settings.json
//	FORMNET_DESKTOP      set by the desktop app; the server stops when its stdin closes
package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultAddr            = "127.0.0.1:8760"
	DefaultProxyAddr       = "127.0.0.1:8761"
	DefaultBodyLimit       = 256 << 10
	DefaultFlowLimit       = 5000
	DefaultMachineInterval = 2 * time.Second
)

// Config is the resolved configuration. It is built once and never changed.
type Config struct {
	Data            string
	Addr            string
	ProxyAddr       string
	BodyLimit       int64
	FlowLimit       int
	Passthrough     []string
	MachineInterval time.Duration
	ReverseDNS      bool
	Intercept       string // auto, on, off
	InterceptSet    bool   // FORMNET_INTERCEPT was set: it wins over <data>/settings.json for this run
	PassthroughSet  bool   // FORMNET_PASSTHROUGH was set: it wins over <data>/settings.json for this run
	Token           string // empty means make one
	Desktop         bool
	SystemProxy     bool // FORMNET_SYSTEM_PROXY's value
	SystemProxySet  bool // FORMNET_SYSTEM_PROXY was set: it wins over <data>/settings.json for this run

	RemoteCapture  bool     // FORMNET_REMOTE_CAPTURE's value
	RemoteSet      bool     // FORMNET_REMOTE_CAPTURE was set: it wins over <data>/settings.json for this run
	RemoteAllow    []string // FORMNET_REMOTE_ALLOW's entries, each an address or a CIDR range
	RemoteAllowSet bool     // FORMNET_REMOTE_ALLOW was set: it wins over <data>/settings.json for this run
}

// Load resolves the configuration from the environment and the home folder.
func Load() (Config, error) {
	return load(os.Getenv, os.UserHomeDir)
}

func load(getenv func(string) string, home func() (string, error)) (Config, error) {
	data := getenv("FORMNET_DATA")
	if data == "" {
		h, err := home()
		if err != nil {
			return Config{}, fmt.Errorf("no home folder and FORMNET_DATA is not set: %w", err)
		}
		data = filepath.Join(h, "formnet")
	}
	addr, err := loopback(getenv("FORMNET_ADDR"), DefaultAddr)
	if err != nil {
		return Config{}, fmt.Errorf("FORMNET_ADDR: %w", err)
	}
	proxyAddr, err := loopback(getenv("FORMNET_PROXY_ADDR"), DefaultProxyAddr)
	if err != nil {
		return Config{}, fmt.Errorf("FORMNET_PROXY_ADDR: %w", err)
	}
	bodyLimit, err := positive(getenv("FORMNET_BODY_LIMIT"), DefaultBodyLimit)
	if err != nil {
		return Config{}, fmt.Errorf("FORMNET_BODY_LIMIT: %w", err)
	}
	flowLimit, err := positive(getenv("FORMNET_FLOW_LIMIT"), DefaultFlowLimit)
	if err != nil {
		return Config{}, fmt.Errorf("FORMNET_FLOW_LIMIT: %w", err)
	}
	intervalSeconds, err := positive(getenv("FORMNET_MACHINE_INTERVAL"), int64(DefaultMachineInterval/time.Second))
	if err != nil {
		return Config{}, fmt.Errorf("FORMNET_MACHINE_INTERVAL: %w", err)
	}
	intercept := strings.ToLower(strings.TrimSpace(getenv("FORMNET_INTERCEPT")))
	switch intercept {
	case "":
		intercept = "auto"
	case "auto", "on", "off":
	default:
		return Config{}, errors.New("FORMNET_INTERCEPT: must be auto, on or off")
	}
	token := strings.TrimSpace(getenv("FORMNET_TOKEN"))
	if token != "" && len(token) < 16 {
		return Config{}, errors.New("FORMNET_TOKEN: must be at least 16 characters")
	}
	systemProxy, systemProxySet := false, false
	switch strings.ToLower(strings.TrimSpace(getenv("FORMNET_SYSTEM_PROXY"))) {
	case "":
	case "1", "true", "yes", "on":
		systemProxy, systemProxySet = true, true
	case "0", "false", "no", "off":
		systemProxySet = true
	default:
		return Config{}, errors.New("FORMNET_SYSTEM_PROXY: must be 1 or 0")
	}
	remote, remoteSet := false, false
	switch strings.ToLower(strings.TrimSpace(getenv("FORMNET_REMOTE_CAPTURE"))) {
	case "":
	case "1", "true", "yes", "on":
		remote, remoteSet = true, true
	case "0", "false", "no", "off":
		remoteSet = true
	default:
		return Config{}, errors.New("FORMNET_REMOTE_CAPTURE: must be 1 or 0")
	}
	remoteAllow := splitList(getenv("FORMNET_REMOTE_ALLOW"))
	for _, a := range remoteAllow {
		if _, err := netip.ParsePrefix(a); err != nil {
			if _, err := netip.ParseAddr(a); err != nil {
				return Config{}, fmt.Errorf("FORMNET_REMOTE_ALLOW: %q is not an address or a CIDR range", a)
			}
		}
	}
	return Config{
		RemoteCapture:   remote,
		RemoteSet:       remoteSet,
		RemoteAllow:     remoteAllow,
		RemoteAllowSet:  strings.TrimSpace(getenv("FORMNET_REMOTE_ALLOW")) != "",
		SystemProxy:     systemProxy,
		SystemProxySet:  systemProxySet,
		Data:            data,
		ReverseDNS:      truthy(getenv("FORMNET_REVERSE_DNS")),
		Intercept:       intercept,
		InterceptSet:    strings.TrimSpace(getenv("FORMNET_INTERCEPT")) != "",
		PassthroughSet:  strings.TrimSpace(getenv("FORMNET_PASSTHROUGH")) != "",
		Token:           token,
		Addr:            addr,
		ProxyAddr:       proxyAddr,
		BodyLimit:       bodyLimit,
		FlowLimit:       int(flowLimit),
		Passthrough:     splitList(getenv("FORMNET_PASSTHROUGH")),
		MachineInterval: time.Duration(intervalSeconds) * time.Second,
		Desktop:         getenv("FORMNET_DESKTOP") != "",
	}, nil
}

// loopback accepts only addresses that bind the loopback interface: formnet never
// listens on a network-facing address, because the proxy and the API carry the
// bodies of everything it sees.
func loopback(value, fallback string) (string, error) {
	if value == "" {
		return fallback, nil
	}
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return "", err
	}
	if host == "localhost" {
		host = "127.0.0.1"
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", errors.New("must be a loopback address such as 127.0.0.1:8760")
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return "", fmt.Errorf("port %q: %w", port, err)
	}
	return net.JoinHostPort(host, port), nil
}

func positive(value string, fallback int64) (int64, error) {
	if value == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, err
	}
	if n <= 0 {
		return 0, errors.New("must be greater than zero")
	}
	return n, nil
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func splitList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
