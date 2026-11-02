package config

import (
	"path/filepath"
	"testing"
	"time"
)

func env(values map[string]string) func(string) string {
	return func(k string) string { return values[k] }
}

func home() (string, error) { return "/home/tester", nil }

func TestDefaults(t *testing.T) {
	cfg, err := load(env(nil), home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Data != filepath.Join("/home/tester", "formnet") || cfg.Addr != DefaultAddr || cfg.ProxyAddr != DefaultProxyAddr {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.BodyLimit != DefaultBodyLimit || cfg.FlowLimit != DefaultFlowLimit || cfg.MachineInterval != DefaultMachineInterval {
		t.Fatalf("unexpected limits: %+v", cfg)
	}
	if cfg.Passthrough != nil || cfg.Desktop || cfg.ReverseDNS || cfg.Intercept != "auto" || cfg.Token != "" {
		t.Fatalf("unexpected passthrough, desktop, reverse dns, intercept or token: %+v", cfg)
	}
}

func TestInterceptTokenAndReverseDNS(t *testing.T) {
	cfg, err := load(env(map[string]string{"FORMNET_INTERCEPT": "ON", "FORMNET_REVERSE_DNS": "yes", "FORMNET_TOKEN": "0123456789abcdef"}), home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Intercept != "on" || !cfg.ReverseDNS || cfg.Token != "0123456789abcdef" {
		t.Fatalf("%+v", cfg)
	}
	if _, err := load(env(map[string]string{"FORMNET_INTERCEPT": "maybe"}), home); err == nil {
		t.Error("bad intercept accepted")
	}
	if _, err := load(env(map[string]string{"FORMNET_TOKEN": "short"}), home); err == nil {
		t.Error("short token accepted")
	}
}

func TestRefusesNetworkFacingAddress(t *testing.T) {
	for _, bad := range []string{"0.0.0.0:8760", "192.168.1.4:8760", "8760", ":8760"} {
		if _, err := load(env(map[string]string{"FORMNET_ADDR": bad}), home); err == nil {
			t.Errorf("FORMNET_ADDR=%q was accepted", bad)
		}
	}
	for _, bad := range []string{"0.0.0.0:8761", "[::]:8761"} {
		if _, err := load(env(map[string]string{"FORMNET_PROXY_ADDR": bad}), home); err == nil {
			t.Errorf("FORMNET_PROXY_ADDR=%q was accepted", bad)
		}
	}
}

func TestLocalhostAndIPv6Loopback(t *testing.T) {
	cfg, err := load(env(map[string]string{"FORMNET_ADDR": "localhost:9000", "FORMNET_PROXY_ADDR": "[::1]:9001"}), home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:9000" || cfg.ProxyAddr != "[::1]:9001" {
		t.Fatalf("unexpected addresses: %+v", cfg)
	}
}

func TestLimitsAndLists(t *testing.T) {
	cfg, err := load(env(map[string]string{
		"FORMNET_BODY_LIMIT":       "1024",
		"FORMNET_FLOW_LIMIT":       "10",
		"FORMNET_PASSTHROUGH":      " a.example.com, *.pinned.example ,",
		"FORMNET_MACHINE_INTERVAL": "5",
		"FORMNET_DESKTOP":          "1",
	}), home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BodyLimit != 1024 || cfg.FlowLimit != 10 || cfg.MachineInterval != 5*time.Second || !cfg.Desktop {
		t.Fatalf("unexpected: %+v", cfg)
	}
	if len(cfg.Passthrough) != 2 || cfg.Passthrough[0] != "a.example.com" || cfg.Passthrough[1] != "*.pinned.example" {
		t.Fatalf("unexpected passthrough: %q", cfg.Passthrough)
	}
	for _, bad := range []string{"0", "-5", "abc"} {
		if _, err := load(env(map[string]string{"FORMNET_FLOW_LIMIT": bad}), home); err == nil {
			t.Errorf("FORMNET_FLOW_LIMIT=%q was accepted", bad)
		}
	}
}
