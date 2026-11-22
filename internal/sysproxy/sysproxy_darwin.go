//go:build darwin

package sysproxy

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const networksetup = "/usr/sbin/networksetup"

func platformSupported() (bool, string) { return true, "" }

// run calls networksetup. It reports a failure both from the exit status and
// from an "** Error" line, which it prints with a zero exit for some refusals.
func run(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, networksetup, args...).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err == nil && strings.Contains(text, "** Error") {
		err = errors.New("networksetup reported an error")
	}
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return text, &setupError{args: args, said: text}
	}
	return text, nil
}

type setupError struct {
	args []string
	said string
}

func (e *setupError) Error() string {
	return "networksetup " + strings.Join(e.args, " ") + ": " + e.said
}

func (e *setupError) rights() bool {
	s := strings.ToLower(e.said)
	for _, w := range []string{"admin", "privilege", "root", "authoriz", "permission", "not permitted"} {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// services lists the enabled network services; a line starting with an
// asterisk is a disabled one, and the first line explains that.
func services() ([]string, error) {
	out, err := run("-listallnetworkservices")
	if err != nil {
		return nil, err
	}
	var names []string
	for i, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if i == 0 && strings.HasPrefix(line, "An asterisk") {
			continue
		}
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "*") {
			continue
		}
		names = append(names, line)
	}
	return names, nil
}

// fields reads "Name: value" lines.
func fields(out string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		name, value, ok := strings.Cut(line, ":")
		if ok {
			m[strings.TrimSpace(name)] = strings.TrimSpace(value)
		}
	}
	return m
}

func readEndpoint(flag, service string) (endpoint, error) {
	out, err := run(flag, service)
	if err != nil {
		return endpoint{}, err
	}
	f := fields(out)
	port, _ := strconv.Atoi(f["Port"])
	return endpoint{
		Enabled: strings.EqualFold(f["Enabled"], "Yes"),
		Host:    f["Server"],
		Port:    port,
		Auth:    f["Authenticated Proxy Enabled"] == "1",
	}, nil
}

func readBypass(service string) ([]string, error) {
	out, err := run("-getproxybypassdomains", service)
	if err != nil {
		return nil, err
	}
	list := []string{}
	if strings.HasPrefix(out, "There aren't any") {
		return list, nil
	}
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			list = append(list, line)
		}
	}
	return list, nil
}

func readService(name string) (serviceState, error) {
	s := serviceState{Name: name}
	var err error
	if s.Web, err = readEndpoint("-getwebproxy", name); err != nil {
		return s, err
	}
	if s.Secure, err = readEndpoint("-getsecurewebproxy", name); err != nil {
		return s, err
	}
	if s.Bypass, err = readBypass(name); err != nil {
		return s, err
	}
	if out, err := run("-getautoproxyurl", name); err == nil {
		f := fields(out)
		s.AutoProxy = strings.EqualFold(f["Enabled"], "Yes")
		if u := f["URL"]; u != "" && u != "(null)" {
			s.AutoProxyURL = u
		}
	}
	return s, nil
}

func readSystem() (snapshot, error) {
	snap := snapshot{OS: "darwin"}
	names, err := services()
	if err != nil {
		return snap, err
	}
	var last error
	for _, name := range names {
		s, err := readService(name)
		if err != nil {
			last = err
			continue
		}
		snap.Services = append(snap.Services, s)
	}
	if len(snap.Services) == 0 && last != nil {
		return snap, last
	}
	return snap, nil
}

func pointsAt(cur snapshot, addr string) bool {
	for _, s := range cur.Services {
		if s.Web.is(addr) || s.Secure.is(addr) {
			return true
		}
	}
	return false
}

func describe(cur snapshot) []Service {
	out := make([]Service, 0, len(cur.Services))
	for _, s := range cur.Services {
		out = append(out, Service{Name: s.Name, Web: s.Web.String(), Secure: s.Secure.String()})
	}
	return out
}

func autoConfig(cur snapshot) bool {
	for _, s := range cur.Services {
		if s.AutoProxy {
			return true
		}
	}
	return false
}

func explain(err error, service string) error {
	var se *setupError
	if errors.As(err, &se) && se.rights() {
		return fmt.Errorf("macOS did not let FormNET change the proxy of %q: changing network settings needs an administrator account. Nothing was changed (networksetup said: %s)", service, se.said)
	}
	return fmt.Errorf("FormNET could not change the proxy of %q, so nothing was changed: %w", service, err)
}

func applySystem(addr string, cur snapshot) error {
	host, port, err := splitAddr(addr)
	if err != nil {
		return err
	}
	if len(cur.Services) == 0 {
		return errors.New("macOS lists no enabled network service to set a proxy on")
	}
	for _, s := range cur.Services {
		if s.Web.Auth || s.Secure.Auth {
			return fmt.Errorf("the proxy of %q uses a password; FormNET leaves it alone, because networksetup could not put the password back. Nothing was changed", s.Name)
		}
	}
	p := strconv.Itoa(port)
	var touched []serviceState
	for _, s := range cur.Services {
		touched = append(touched, s)
		for _, args := range [][]string{
			{"-setwebproxy", s.Name, host, p},
			{"-setsecurewebproxy", s.Name, host, p},
			{"-setwebproxystate", s.Name, "on"},
			{"-setsecurewebproxystate", s.Name, "on"},
		} {
			if _, err := run(args...); err != nil {
				for _, t := range touched {
					_ = putBack(t)
				}
				return explain(err, s.Name)
			}
		}
	}
	return nil
}

// putBack writes one service's saved web and secure web proxy. A setting
// that had no server is only turned off: networksetup cannot clear a server.
func putBack(s serviceState) error {
	for _, side := range []struct {
		set, state string
		e          endpoint
	}{{"-setwebproxy", "-setwebproxystate", s.Web}, {"-setsecurewebproxy", "-setsecurewebproxystate", s.Secure}} {
		if err := putEndpoint(s.Name, side.set, side.state, side.e); err != nil {
			return err
		}
	}
	return nil
}

func putEndpoint(service, set, state string, e endpoint) error {
	if e.Host != "" && e.Port > 0 {
		if _, err := run(set, service, e.Host, strconv.Itoa(e.Port)); err != nil {
			return err
		}
	}
	onOff := "off"
	if e.Enabled {
		onOff = "on"
	}
	_, err := run(state, service, onOff)
	return err
}

// restoreSystem puts back each saved setting that still points at addr; one
// the person changed since is left as it is.
func restoreSystem(prev, cur snapshot, addr string) error {
	now := map[string]serviceState{}
	for _, s := range cur.Services {
		now[s.Name] = s
	}
	var errs []error
	for _, s := range prev.Services {
		c, ok := now[s.Name]
		if !ok {
			continue
		}
		if c.Web.is(addr) {
			if err := putEndpoint(s.Name, "-setwebproxy", "-setwebproxystate", s.Web); err != nil {
				errs = append(errs, explainRestore(err, s.Name))
			}
		}
		if c.Secure.is(addr) {
			if err := putEndpoint(s.Name, "-setsecurewebproxy", "-setsecurewebproxystate", s.Secure); err != nil {
				errs = append(errs, explainRestore(err, s.Name))
			}
		}
	}
	return errors.Join(errs...)
}

func explainRestore(err error, service string) error {
	var se *setupError
	if errors.As(err, &se) && se.rights() {
		return fmt.Errorf("macOS did not let FormNET put back the proxy of %q: it needs an administrator account (networksetup said: %s)", service, se.said)
	}
	return fmt.Errorf("FormNET could not put back the proxy of %q: %w", service, err)
}
