package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/nodemode-dev/formnet/internal/app"
	"github.com/nodemode-dev/formnet/internal/config"
	"github.com/nodemode-dev/formnet/internal/machine"
	"github.com/nodemode-dev/formnet/internal/userfile"
)

const (
	endpointsFile = "endpoints.json"
	replaceWait   = 5 * time.Second
)

// endpoints is what other programs read to find the running formnet. It never
// holds the token or anything recorded.
type endpoints struct {
	Product   string    `json:"product"`
	Version   string    `json:"version"`
	PID       int       `json:"pid"`
	Addr      string    `json:"addr"`
	ProxyAddr string    `json:"proxy_addr"`
	CAPath    string    `json:"ca_path"`
	Started   time.Time `json:"started"`
}

func readEndpoints(data string) (endpoints, bool) {
	raw, err := os.ReadFile(filepath.Join(data, endpointsFile))
	if err != nil {
		return endpoints{}, false
	}
	var e endpoints
	if json.Unmarshal(raw, &e) != nil || e.Product != app.Product {
		return endpoints{}, false
	}
	return e, true
}

func writeEndpoints(data string, e endpoints) error {
	raw, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	return userfile.Write(filepath.Join(data, endpointsFile), append(raw, '\n'))
}

// removeEndpoints deletes the file only while it still describes this run, so
// a stopping formnet never removes the file of the one that replaced it.
func removeEndpoints(data string) {
	if e, ok := readEndpoints(data); ok && e.PID == os.Getpid() {
		_ = os.Remove(filepath.Join(data, endpointsFile))
	}
}

// previous is what the last run left in the data folder, read before this
// run overwrites the token.
type previous struct {
	token     string
	ends      endpoints
	endsFound bool
}

func readPrevious(data string) previous {
	var p previous
	if raw, err := os.ReadFile(filepath.Join(data, tokenFile)); err == nil {
		p.token = strings.TrimSpace(string(raw))
	}
	p.ends, p.endsFound = readEndpoints(data)
	return p
}

type health struct {
	Product   string `json:"product"`
	Version   string `json:"version"`
	ProxyAddr string `json:"proxy_addr"`
}

var localClient = &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}}

func probe(addr string) (health, bool) {
	resp, err := localClient.Get("http://" + addr + "/healthz")
	if err != nil {
		return health{}, false
	}
	defer resp.Body.Close()
	var h health
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&h) != nil || h.Product != app.Product {
		return health{}, false
	}
	return h, true
}

// claimed is the result of binding both listeners.
type claimed struct {
	api, proxy net.Listener
	notices    []string
}

// claim binds the API and proxy addresses. A leftover formnet on them is
// asked to stop first; anything else keeps its port and this run moves to
// free loopback ports, saying so.
func claim(cfg config.Config, prev previous, stderr io.Writer) (claimed, error) {
	var c claimed
	c.api, _ = net.Listen("tcp", cfg.Addr)
	c.proxy, _ = net.Listen("tcp", cfg.ProxyAddr)
	held := map[string]string{}
	if c.api == nil || c.proxy == nil {
		var candidates []string
		if c.api == nil {
			candidates = append(candidates, cfg.Addr)
		}
		if prev.endsFound && prev.ends.Addr != "" && prev.ends.Addr != cfg.Addr {
			candidates = append(candidates, prev.ends.Addr)
		}
		for _, addr := range candidates {
			h, ok := probe(addr)
			if !ok {
				continue
			}
			wanted := (addr == cfg.Addr && c.api == nil) || (c.proxy == nil && (h.ProxyAddr == cfg.ProxyAddr || (h.ProxyAddr == "" && addr == cfg.Addr)))
			if !wanted {
				continue
			}
			if replace(addr, prev, stderr) {
				waitFree(cfg, &c)
				break
			}
			note := "another FormNET " + h.Version + " that would not stop"
			if addr == cfg.Addr {
				held[cfg.Addr] = note
			}
			if h.ProxyAddr == cfg.ProxyAddr || h.ProxyAddr == "" {
				held[cfg.ProxyAddr] = note
			}
		}
	}
	var err error
	if c.api == nil {
		if c.api, err = moveAside(cfg.Addr, "the web client and API", held[cfg.Addr], &c, stderr); err != nil {
			return c, err
		}
	}
	if c.proxy == nil {
		if c.proxy, err = moveAside(cfg.ProxyAddr, "the proxy", held[cfg.ProxyAddr], &c, stderr); err != nil {
			_ = c.api.Close()
			return c, err
		}
	}
	return c, nil
}

func waitFree(cfg config.Config, c *claimed) {
	deadline := time.Now().Add(replaceWait)
	for time.Now().Before(deadline) {
		if c.api == nil {
			c.api, _ = net.Listen("tcp", cfg.Addr)
		}
		if c.proxy == nil {
			c.proxy, _ = net.Listen("tcp", cfg.ProxyAddr)
		}
		if c.api != nil && c.proxy != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// replace asks the formnet at addr to stop with the last run's token, and
// failing that stops the pid the endpoints file names, but only when that
// process holds addr and is a formnet program.
func replace(addr string, prev previous, stderr io.Writer) bool {
	if prev.token != "" {
		req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/api/quit", nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+prev.token)
			if resp, err := localClient.Do(req); err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusAccepted {
					fmt.Fprintf(stderr, "formnet: asked the FormNET already on %s to stop, to take its place\n", addr)
					return true
				}
			}
		}
	}
	if !prev.endsFound || prev.ends.Addr != addr || prev.ends.PID <= 0 || prev.ends.PID == os.Getpid() {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, port, _ := net.SplitHostPort(addr)
	holder, ok := machine.Listening(ctx, port)
	if !ok || holder.PID != prev.ends.PID || !formnetProgram(holder) {
		return false
	}
	p, err := os.FindProcess(holder.PID)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		err = p.Kill()
	} else {
		err = p.Signal(syscall.SIGTERM)
	}
	if err != nil {
		return false
	}
	fmt.Fprintf(stderr, "formnet: stopped the FormNET already on %s (pid %d), to take its place\n", addr, holder.PID)
	return true
}

func formnetProgram(c machine.Connection) bool {
	name := c.Process
	if c.Path != "" {
		name = filepath.Base(c.Path)
	}
	return strings.HasPrefix(strings.ToLower(name), app.Product)
}

// moveAside binds a free port on addr's loopback host and records why.
func moveAside(addr, what, holder string, c *claimed, stderr io.Writer) (net.Listener, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", addr, err)
	}
	if holder == "" {
		holder = holderOf(port)
	}
	notice := fmt.Sprintf("Port %s is held by %s, so %s moved to %s.", port, holder, what, ln.Addr().String())
	if what == "the proxy" {
		notice += " Point programs at the new address."
	}
	fmt.Fprintln(stderr, "formnet: "+notice)
	c.notices = append(c.notices, notice)
	return ln, nil
}

func holderOf(port string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if h, ok := machine.Listening(ctx, port); ok {
		name := h.Process
		if name == "" {
			name = "a program"
		}
		return fmt.Sprintf("%s (pid %d)", name, h.PID)
	}
	return "another program"
}
