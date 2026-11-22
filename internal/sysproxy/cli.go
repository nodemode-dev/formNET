package sysproxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nodemode-dev/formnet/internal/config"
	"github.com/nodemode-dev/formnet/internal/settings"
)

// Command is `formnet sysproxy [on|off|restore]`.
func Command(args []string, stdout, stderr io.Writer) int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(stderr, "formnet:", err)
		return 2
	}
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "status":
		st, running, err := ask(cfg.Data, http.MethodGet, nil)
		if err != nil {
			fmt.Fprintln(stderr, "formnet sysproxy:", err)
			return 1
		}
		if !running {
			st = inspect(cfg.Data)
			fmt.Fprintln(stdout, "FormNET is not running with this data folder; this is the system as it is now.")
		}
		printStatus(stdout, st)
		return 0
	case "on", "off":
		st, running, err := ask(cfg.Data, http.MethodPost, map[string]bool{"enabled": sub == "on"})
		if err != nil {
			fmt.Fprintln(stderr, "formnet sysproxy:", err)
			return 1
		}
		if !running {
			fmt.Fprintln(stderr, "formnet sysproxy: FormNET is not running with this data folder; start it first (formnet serve)")
			return 1
		}
		printStatus(stdout, st)
		if st.Error != "" {
			return 1
		}
		return 0
	case "restore":
		rec, found, err := readRecord(cfg.Data)
		if !found && err == nil {
			fmt.Fprintf(stdout, "Nothing to restore: %s does not exist.\n", recordPath(cfg.Data))
			return 0
		}
		if err == nil && rec.PID != os.Getpid() && alive(rec.API) {
			fmt.Fprintf(stderr, "formnet sysproxy: the FormNET that set the system proxy is still running (pid %d, %s); run formnet sysproxy off instead\n", rec.PID, rec.API)
			return 1
		}
		msg, err := Recover(cfg.Data)
		if err != nil {
			fmt.Fprintln(stderr, "formnet sysproxy:", err)
			return 1
		}
		if msg == "" {
			msg = "nothing was changed"
		}
		fmt.Fprintln(stdout, strings.ToUpper(msg[:1])+msg[1:]+".")
		return 0
	default:
		fmt.Fprintf(stderr, "formnet sysproxy: unknown subcommand %q (use on, off or restore)\n", sub)
		return 2
	}
}

// inspect reports the system without a running server.
func inspect(data string) Status {
	st := Status{Services: []Service{}}
	if f, _ := settings.Open(data); f != nil {
		st.Enabled = f.Current().SystemProxy
	}
	st.Supported, st.Reason = platformSupported()
	if !st.Supported {
		return st
	}
	cur, err := readSystem()
	if err != nil {
		st.Error = "reading the system proxy: " + err.Error()
	}
	st.Services = describe(cur)
	st.AutoConfig = autoConfig(cur)
	if rec, found, _ := readRecord(data); found {
		st.Address = rec.Address
		st.Active = pointsAt(cur, rec.Address)
	}
	return st
}

// ask calls the running server's /api/sysproxy. running is false when no
// FormNET answers with this data folder's endpoints and token.
func ask(data, method string, body any) (Status, bool, error) {
	var st Status
	raw, err := os.ReadFile(filepath.Join(data, "endpoints.json"))
	if err != nil {
		return st, false, nil
	}
	var ends struct {
		Product string `json:"product"`
		Addr    string `json:"addr"`
	}
	if json.Unmarshal(raw, &ends) != nil || ends.Product != "formnet" || ends.Addr == "" || !alive(ends.Addr) {
		return st, false, nil
	}
	token, err := os.ReadFile(filepath.Join(data, "token"))
	if err != nil {
		return st, true, errors.New("FormNET is running but its session token could not be read: " + err.Error())
	}
	var payload io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return st, true, err
		}
		payload = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, "http://"+ends.Addr+"/api/sysproxy", payload)
	if err != nil {
		return st, true, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 60 * time.Second, Transport: &http.Transport{Proxy: nil}}
	resp, err := client.Do(req)
	if err != nil {
		return st, true, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return st, true, fmt.Errorf("FormNET answered %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&st); err != nil {
		return st, true, err
	}
	return st, true, nil
}

func printStatus(w io.Writer, st Status) {
	fmt.Fprintf(w, "supported:   %t\n", st.Supported)
	fmt.Fprintf(w, "enabled:     %t\n", st.Enabled)
	fmt.Fprintf(w, "active:      %t\n", st.Active)
	if st.Address != "" {
		fmt.Fprintf(w, "address:     %s\n", st.Address)
	}
	if st.Supported {
		fmt.Fprintf(w, "auto config: %t\n", st.AutoConfig)
	}
	for _, s := range st.Services {
		fmt.Fprintf(w, "service:     %s  web %s  secure %s\n", s.Name, orOff(s.Web), orOff(s.Secure))
	}
	if st.Reason != "" {
		fmt.Fprintf(w, "note:        %s\n", st.Reason)
	}
	if st.Error != "" {
		fmt.Fprintf(w, "error:       %s\n", st.Error)
	}
}

func orOff(s string) string {
	if s == "" {
		return "off"
	}
	return s
}
