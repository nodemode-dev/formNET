// Command formnet records what programs on this computer actually send and
// receive: an intercepting proxy for the HTTP(S) a program is pointed through
// it, and a machine view of every connection the computer holds.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	formnet "github.com/nodemode-dev/formnet"
	"github.com/nodemode-dev/formnet/internal/app"
	"github.com/nodemode-dev/formnet/internal/ca"
	"github.com/nodemode-dev/formnet/internal/config"
	"github.com/nodemode-dev/formnet/internal/flow"
	"github.com/nodemode-dev/formnet/internal/machine"
	"github.com/nodemode-dev/formnet/internal/proxy"
	"github.com/nodemode-dev/formnet/internal/server"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "serve":
		return serve(stdout, stderr)
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, app.Version)
		return 0
	case "ca":
		return caCommand(args[1:], stdout, stderr)
	case "snapshot":
		return snapshot(stdout, stderr)
	case "help", "--help", "-h":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "formnet: unknown command %q\n\n", cmd)
		usage(stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `usage: formnet [command]

  serve            start the proxy, the machine view and the web client (the default)
  ca               print the root certificate's path, fingerprint and trust state
  ca export PATH   write the root certificate (PEM) to PATH
  ca trust         print the command that makes this computer trust the root
  snapshot         print the machine's connection table once, as JSON
  version          print the version
  help             this text

Settings (environment):
  FORMNET_DATA              data folder (CA, exports); default ~/formnet
  FORMNET_ADDR              web client and API; default 127.0.0.1:8760 (loopback only)
  FORMNET_PROXY_ADDR        proxy; default 127.0.0.1:8761 (loopback only)
  FORMNET_BODY_LIMIT        bytes of each body kept; default 262144
  FORMNET_FLOW_LIMIT        flows kept in memory; default 5000
  FORMNET_PASSTHROUGH       hosts tunnelled without interception, comma-separated
  FORMNET_MACHINE_INTERVAL  seconds between machine snapshots; default 2

Point a program at the proxy, for example:
  curl -x http://127.0.0.1:8761 --cacert ~/formnet/ca.pem https://example.com/
  HTTPS_PROXY=http://127.0.0.1:8761 HTTP_PROXY=http://127.0.0.1:8761 some-program
`)
}

func logger(stderr io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func serve(stdout, stderr io.Writer) int {
	log := logger(stderr)
	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		return 2
	}
	if err := os.MkdirAll(cfg.Data, 0o700); err != nil {
		log.Error("data folder", "path", cfg.Data, "err", err)
		return 1
	}
	authority, err := ca.Load(cfg.Data)
	if err != nil {
		log.Error("certificate authority", "err", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if cfg.Desktop {
		go func() {
			_, _ = io.Copy(io.Discard, os.Stdin)
			log.Info("the desktop app closed its pipe; stopping")
			stop()
		}()
	}

	store := flow.NewStore(cfg.FlowLimit)
	monitor := machine.NewMonitor(cfg.MachineInterval, log)
	go monitor.Run(ctx)

	px := proxy.New(proxy.Options{
		CA:          authority,
		Store:       store,
		BodyLimit:   cfg.BodyLimit,
		Passthrough: cfg.Passthrough,
		Intercept:   true,
		Owner:       monitor.Owner,
		Log:         log,
	})
	proxyErr := make(chan error, 1)
	go func() { proxyErr <- px.ListenAndServe(ctx, cfg.ProxyAddr) }()

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		log.Error("listen", "addr", cfg.Addr, "err", err)
		return 1
	}
	api := &http.Server{
		Handler:           server.Handler(server.Deps{Addr: cfg.Addr, Store: store, Proxy: px, CA: authority, Machine: monitor, Dist: formnet.Dist(), Log: log}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	apiErr := make(chan error, 1)
	go func() { apiErr <- api.Serve(ln) }()

	info := authority.Info()
	log.Info("formnet", "version", app.Version, "ui", "http://"+cfg.Addr, "proxy", cfg.ProxyAddr, "data", cfg.Data, "ca_trusted", info.Trusted)
	if !info.Trusted {
		log.Info("the root certificate is not trusted yet; HTTPS is counted but not opened", "run", "formnet ca trust")
	}
	// The desktop app waits for this line before it opens its window.
	fmt.Fprintf(stdout, "formnet desktop ready http://%s/\n", cfg.Addr)

	select {
	case <-ctx.Done():
	case err := <-proxyErr:
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, http.ErrServerClosed) {
			log.Error("proxy stopped", "err", err)
			stop()
		}
	case err := <-apiErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("api stopped", "err", err)
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = api.Shutdown(shutdown)
	return 0
}

func caCommand(args []string, stdout, stderr io.Writer) int {
	log := logger(stderr)
	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		return 2
	}
	if err := os.MkdirAll(cfg.Data, 0o700); err != nil {
		log.Error("data folder", "path", cfg.Data, "err", err)
		return 1
	}
	authority, err := ca.Load(cfg.Data)
	if err != nil {
		log.Error("certificate authority", "err", err)
		return 1
	}
	info := authority.Info()
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "":
		fmt.Fprintf(stdout, "path:        %s\nfingerprint: %s\nexpires:     %s\ntrusted:     %t\n", info.Path, info.Fingerprint, info.NotAfter.Format(time.RFC3339), info.Trusted)
		if !info.Trusted {
			fmt.Fprintf(stdout, "\n%s\n  %s\n", info.TrustHint, info.TrustCommand)
		}
		return 0
	case "export":
		if len(args) < 2 {
			fmt.Fprintln(stderr, "formnet ca export needs a path")
			return 2
		}
		target := filepath.Clean(args[1])
		if err := os.WriteFile(target, authority.PEM(), 0o644); err != nil {
			log.Error("write", "path", target, "err", err)
			return 1
		}
		fmt.Fprintf(stdout, "wrote %s\n", target)
		return 0
	case "trust":
		fmt.Fprintf(stdout, "%s\n\n  %s\n", info.TrustHint, info.TrustCommand)
		if info.Trusted {
			fmt.Fprintln(stdout, "\nThis computer already trusts the root.")
		}
		return 0
	default:
		fmt.Fprintf(stderr, "formnet ca: unknown subcommand %q\n", sub)
		return 2
	}
}

func snapshot(stdout, stderr io.Writer) int {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	snap, err := machine.Take(ctx)
	if err != nil && !snap.Supported {
		fmt.Fprintln(stderr, "formnet snapshot:", err)
		return 1
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(snap)
	return 0
}
