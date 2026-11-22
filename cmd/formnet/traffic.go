package main

import (
	"log/slog"
	"strings"
	"time"

	"github.com/nodemode-dev/formnet/internal/config"
	"github.com/nodemode-dev/formnet/internal/proxy"
	"github.com/nodemode-dev/formnet/internal/rules"
	"github.com/nodemode-dev/formnet/internal/settings"
	"github.com/nodemode-dev/formnet/internal/throttle"
)

// useTraffic gives the proxy its rules, throttle and breakpoints from the
// data folder, and returns the step that applies capture from other devices
// once the proxy listens. FORMNET_REMOTE_CAPTURE and FORMNET_REMOTE_ALLOW win
// for this run and are never written to settings.json.
func useTraffic(px *proxy.Proxy, cfg config.Config, saved *settings.File, log *slog.Logger) func() {
	kept := saved.Current()
	engine, err := rules.Open(cfg.Data)
	if err != nil {
		log.Warn("rules file not read; no rules apply until the next change replaces it", "err", err)
	}
	tc := throttle.Defaults()
	if t := kept.Throttle; t != nil {
		tc = throttle.Config{Enabled: t.Enabled, Preset: t.Preset, DownKbps: t.DownKbps, UpKbps: t.UpKbps, LatencyMS: t.LatencyMS, Hosts: t.Hosts}
	}
	shaper, err := throttle.New(tc)
	if err != nil {
		log.Warn("throttle setting not read; it is off until the next change", "err", err)
	}
	var timeout time.Duration
	maxPaused := 0
	if b := kept.Breakpoints; b != nil {
		timeout, maxPaused = time.Duration(b.TimeoutSeconds)*time.Second, b.MaxPaused
	}
	px.UseTraffic(proxy.TrafficOptions{
		Rules:             engine,
		Throttle:          shaper,
		BreakpointTimeout: timeout,
		MaxPaused:         maxPaused,
		RemoteFromEnv:     cfg.RemoteSet || cfg.RemoteAllowSet,
		RememberThrottle: func(c throttle.Config) error {
			return saved.SetThrottle(settings.Throttle{Enabled: c.Enabled, Preset: c.Preset, DownKbps: c.DownKbps, UpKbps: c.UpKbps, LatencyMS: c.LatencyMS, Hosts: c.Hosts})
		},
		RememberRemote:      saved.SetRemote,
		RememberBreakpoints: saved.SetBreakpoints,
	})
	enabled, allow := false, []string{}
	if r := kept.Remote; r != nil {
		enabled, allow = r.Enabled, r.Allow
	}
	if cfg.RemoteSet {
		enabled = cfg.RemoteCapture
	}
	if cfg.RemoteAllowSet {
		allow = cfg.RemoteAllow
	}
	return func() {
		st, err := px.StartRemote(enabled, allow)
		switch {
		case err != nil && enabled:
			log.Warn("capture from other devices not turned on", "err", err)
			if _, err := px.StartRemote(false, allow); err != nil {
				log.Warn("remote allow list not read", "err", err)
			}
		case err != nil:
			log.Warn("remote allow list not read", "err", err)
		case st.Enabled:
			log.Info("capture from other devices: set the proxy on the other device to one of these", "addresses", strings.Join(st.Addresses, " "), "port", st.Port, "certificate", st.CertURL)
		}
	}
}
