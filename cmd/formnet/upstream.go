package main

import (
	"log/slog"

	"github.com/nodemode-dev/formnet/internal/config"
	"github.com/nodemode-dev/formnet/internal/proxy"
	"github.com/nodemode-dev/formnet/internal/settings"
)

// useUpstream gives the proxy the upstream proxy from the data folder, or
// from FORMNET_UPSTREAM_PROXY, which wins for this run and is never written.
func useUpstream(px *proxy.Proxy, cfg config.Config, saved *settings.File, log *slog.Logger) {
	var kept proxy.UpstreamSetting
	if u := saved.Current().Upstream; u != nil {
		kept = proxy.UpstreamSetting{Enabled: u.Enabled, URL: u.URL, Bypass: u.Bypass, Username: u.Username, Password: u.Password}
	}
	opts := proxy.UpstreamOptions{
		Saved: kept,
		Remember: func(s proxy.UpstreamSetting) error {
			return saved.SetUpstream(settings.Upstream{Enabled: s.Enabled, URL: s.URL, Bypass: s.Bypass, Username: s.Username, Password: s.Password})
		},
	}
	if cfg.UpstreamSet {
		env, err := proxy.UpstreamFromURL(cfg.UpstreamProxy, kept.Bypass)
		if err != nil {
			log.Warn("FORMNET_UPSTREAM_PROXY not used; no upstream proxy in this run", "err", err)
			env = proxy.UpstreamSetting{Bypass: kept.Bypass}
		}
		opts.Env = &env
	}
	if err := px.UseUpstream(opts); err != nil {
		log.Warn("upstream proxy not used until the next change", "err", err)
		return
	}
	if st := px.Upstream(); st.Active {
		log.Info("outgoing connections go through the upstream proxy", "upstream", st.URL, "bypass", len(st.Bypass), "from_env", st.FromEnv)
	}
}
