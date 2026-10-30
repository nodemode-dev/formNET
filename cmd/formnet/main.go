// Command formnet records what programs on this computer actually send and
// receive: an intercepting proxy for the HTTP(S) a program is pointed through
// it, and a machine view of every connection the computer holds.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
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
	case "url":
		return printURL(stdout, stderr)
	case "open":
		return openBrowser(stdout, stderr)
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
  url              print the address of the running formnet, with this run's session token
  open             open that address in the browser
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
  FORMNET_INTERCEPT         auto (open HTTPS once the root is trusted), on, or off; default auto
  FORMNET_MACHINE_INTERVAL  seconds between machine snapshots; default 2
  FORMNET_REVERSE_DNS       1 to name remote addresses in the machine view (sends PTR queries); default off
  FORMNET_TOKEN             this run's session token; default a new one, kept in <data>/token

The web client answers only to a page opened with the session token: use the
address "formnet serve" prints, or run "formnet open". Scripts send the token
as "Authorization: Bearer <token>".

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

	token, err := sessionToken(cfg)
	if err != nil {
		log.Error("session token", "err", err)
		return 1
	}
	info := authority.Info()
	intercept := cfg.Intercept == "on" || (cfg.Intercept == "auto" && info.Trusted)

	store := flow.NewStore(cfg.FlowLimit)
	monitor := machine.NewMonitorWithOptions(machine.Options{Interval: cfg.MachineInterval, Log: log, ReverseDNS: cfg.ReverseDNS})
	go monitor.Run(ctx)

	px := proxy.New(proxy.Options{
		CA:          authority,
		Store:       store,
		BodyLimit:   cfg.BodyLimit,
		Passthrough: cfg.Passthrough,
		Intercept:   intercept,
		Owner:       monitor.Owner,
		Refuse:      []string{cfg.Addr, cfg.ProxyAddr},
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
		Handler:           server.Handler(server.Deps{Addr: cfg.Addr, Token: token, Store: store, Proxy: px, CA: authority, Machine: monitor, Dist: formnet.Dist(), Log: log}),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	apiErr := make(chan error, 1)
	go func() { apiErr <- api.Serve(ln) }()

	if err := waitForProxy(px, proxyErr); err != nil {
		log.Error("proxy", "addr", cfg.ProxyAddr, "err", err)
		return 1
	}
	log.Info("formnet", "version", app.Version, "open", sessionURL(cfg.Addr, token), "proxy", cfg.ProxyAddr, "data", cfg.Data, "ca_trusted", info.Trusted, "intercept", intercept)
	switch {
	case !info.Trusted && !intercept:
		log.Info("the root certificate is not trusted yet, so HTTPS is counted but not opened", "run", "formnet ca trust")
	case !info.Trusted && intercept:
		log.Info("HTTPS is opened although the root is not trusted; programs that verify certificates will refuse the proxy", "run", "formnet ca trust")
	}
	// The desktop app waits for this line before it opens its window.
	fmt.Fprintf(stdout, "formnet desktop ready http://%s/\n", cfg.Addr)

	code := 0
	select {
	case <-ctx.Done():
	case err := <-proxyErr:
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, http.ErrServerClosed) {
			log.Error("proxy stopped", "err", err)
			code = 1
		}
	case err := <-apiErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("api stopped", "err", err)
			code = 1
		}
	}
	stop()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = api.Shutdown(shutdown)
	return code
}

// waitForProxy returns once the proxy is listening, or with its error.
func waitForProxy(px *proxy.Proxy, errs <-chan error) error {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-errs:
			if err == nil {
				err = errors.New("stopped before it was ready")
			}
			return err
		default:
		}
		if px.Status().Running {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("not listening after 5 seconds")
}

const tokenFile = "token"

// sessionToken uses the configured token or makes one for this run, keeping
// it in the data folder (owner-readable only) for `formnet url` and `open`.
func sessionToken(cfg config.Config) (string, error) {
	token := cfg.Token
	if token == "" {
		var b [24]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		token = hex.EncodeToString(b[:])
	}
	path := filepath.Join(cfg.Data, tokenFile)
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", fmt.Errorf("chmod %s: %w", path, err)
	}
	return token, nil
}

func sessionURL(addr, token string) string {
	return "http://" + addr + "/?token=" + token
}

// currentURL reads the token the running formnet wrote.
func currentURL(stderr io.Writer) (string, bool) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(stderr, "formnet:", err)
		return "", false
	}
	raw, err := os.ReadFile(filepath.Join(cfg.Data, tokenFile))
	if err != nil {
		fmt.Fprintln(stderr, "formnet: no session token found; is formnet serve running with this data folder?")
		return "", false
	}
	return sessionURL(cfg.Addr, strings.TrimSpace(string(raw))), true
}

func printURL(stdout, stderr io.Writer) int {
	url, ok := currentURL(stderr)
	if !ok {
		return 1
	}
	fmt.Fprintln(stdout, url)
	return 0
}

func openBrowser(stdout, stderr io.Writer) int {
	url, ok := currentURL(stderr)
	if !ok {
		return 1
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("/usr/bin/open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(stderr, "formnet: could not open a browser (%v); open this yourself:\n%s\n", err, url)
		return 1
	}
	fmt.Fprintln(stdout, url)
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
