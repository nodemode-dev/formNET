package terminal

import (
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/nodemode-dev/formnet/internal/agentlookup"
)

var overrides = map[string][]string{"claude": {"FORMNET_CLAUDE_BIN"}, "codex": {"FORMNET_CODEX_BIN"}, "cursor": {"FORMNET_CURSOR_BIN"}, "antigravity": {"FORMNET_ANTIGRAVITY_BIN"}}

const otherwise = ""

var dropped = map[string]bool{"TERM": true, "COLORTERM": true, "FORMNET_TOKEN": true, "FORMNET_DESKTOP": true}

var captured = map[string]bool{"HTTP_PROXY": true, "HTTPS_PROXY": true, "http_proxy": true, "https_proxy": true, "NODE_USE_ENV_PROXY": true,
	"NODE_EXTRA_CA_CERTS": true, "SSL_CERT_FILE": true, "REQUESTS_CA_BUNDLE": true}

// Info is one program's answer to the dock's status call (clause 10).
type Info struct {
	Title string `json:"title"`
	agentlookup.Status
	Install  string `json:"install"`
	SignedIn *bool  `json:"signedIn"`
}

type agentCache struct {
	sync.Mutex
	at  time.Time
	out map[string]Info
}

func baseEnv(capture bool) []string {
	out := []string{}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if runtime.GOOS == "windows" && strings.EqualFold(k, "PATH") {
			kv, k = "PATH="+v, "PATH"
		}
		if dropped[k] || (capture && captured[k]) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func (m *Manager) childEnv(path string, capture bool) []string {
	extra := []string{"TERM=xterm-256color", "COLORTERM=truecolor", "FORMNET_TERMINAL=1"}
	if m.o.APIAddr != "" {
		extra = append(extra, "FORMNET_URL=http://"+m.o.APIAddr)
	}
	if capture {
		proxy := "http://" + m.o.ProxyAddr
		extra = append(extra, "HTTP_PROXY="+proxy, "HTTPS_PROXY="+proxy, "http_proxy="+proxy, "https_proxy="+proxy, "NODE_USE_ENV_PROXY=1",
			"NODE_EXTRA_CA_CERTS="+m.o.CAPath, "SSL_CERT_FILE="+m.o.CAPath, "REQUESTS_CA_BUNDLE="+m.o.CAPath)
	}
	return agentlookup.ChildEnv(agentlookup.Child{Path: path, Base: baseEnv(capture), ShellTimeout: 10 * time.Second, Extra: extra})
}

func (m *Manager) located(kind string, fresh bool) agentlookup.Found {
	found := agentlookup.Find(kind, agentlookup.Query{OverrideVars: overrides[kind], Env: baseEnv(false), ShellTimeout: 10 * time.Second, Fresh: fresh})
	return agentlookup.Check(found, m.childEnv(found.Path, false), 15*time.Second, fresh, "")
}

func (m *Manager) program(kind string) agentlookup.Found {
	found := m.located(kind, false)
	if found.State != "runs" {
		found = m.located(kind, true)
	}
	return found
}

func said(found agentlookup.Found) string {
	return agentlookup.Sentence(found, agentlookup.Install(found.Program, ""), otherwise)
}

func (m *Manager) status(fresh bool) map[string]Info {
	m.agents.Lock()
	defer m.agents.Unlock()
	if !fresh && m.agents.out != nil && time.Since(m.agents.at) < time.Minute {
		return m.agents.out
	}
	out := map[string]Info{}
	for _, kind := range agentlookup.Order {
		found := m.located(kind, fresh)
		install := agentlookup.Install(kind, "")
		a := Info{Title: found.Title, Status: agentlookup.Answer(found, install, otherwise)}
		if !a.Installed {
			a.Install = install
		}
		out[kind] = a
	}
	m.agents.at, m.agents.out = time.Now(), out
	return out
}

func loginShell() []string {
	if runtime.GOOS == "windows" {
		if c := os.Getenv("COMSPEC"); c != "" {
			return []string{c}
		}
		return []string{"cmd.exe"}
	}
	return []string{agentlookup.LoginShell(nil, ""), "-l"}
}
