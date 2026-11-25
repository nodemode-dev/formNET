package server

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nodemode-dev/formnet/internal/app"
	"github.com/nodemode-dev/formnet/internal/flow"
)

// curlBodyMax is the largest request body a cURL script carries; a larger
// one is left out with a comment, so a script stays something to paste.
const curlBodyMax = 64 << 10

// curlSkip names headers curl sets itself or that belong to one connection.
func curlSkip(name string) bool {
	switch strings.ToLower(name) {
	case "host", "content-length", "connection", "keep-alive", "te", "trailer", "transfer-encoding", "upgrade", "proxy-connection", "proxy-authenticate":
		return true
	}
	return strings.HasPrefix(name, ":")
}

func (s *handler) exportCurl(w http.ResponseWriter, r *http.Request) {
	q, err := queryFrom(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sh := r.URL.Query().Get("shell")
	if sh == "" {
		sh = "sh"
	}
	if sh != "sh" && sh != "powershell" && sh != "cmd" {
		writeError(w, http.StatusBadRequest, "shell must be sh, powershell or cmd")
		return
	}
	listed, _, err := s.search(r, q)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	secrets := r.URL.Query().Get("secrets") == "1"
	var flows []flow.Flow
	skipped := 0
	for _, f := range listed {
		if f.Kind != flow.KindHTTP {
			skipped++
			continue
		}
		if g, ok := s.Store.Get(f.ID); ok {
			f = g
		}
		flows = append(flows, f)
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(curlScript(sh, flows, skipped, secrets, time.Now())))
}

type curlWriter struct {
	shell string
	b     strings.Builder
	seq   int
}

func (c *curlWriter) comment(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	if c.shell == "cmd" {
		c.b.WriteString("rem " + strings.ReplaceAll(line, "%", "%%") + "\r\n")
		return
	}
	c.b.WriteString("# " + line + "\n")
}

func (c *curlWriter) line(s string) {
	if c.shell == "cmd" {
		c.b.WriteString(s + "\r\n")
		return
	}
	c.b.WriteString(s + "\n")
}

func (c *curlWriter) arg(v string) string {
	switch c.shell {
	case "powershell":
		q := shell{name: "powershell"}.quote(v)
		if strings.Contains(v, `"`) {
			return "(Q " + q + ")"
		}
		return q
	case "cmd":
		return cmdArg(v)
	}
	return shell{name: "sh"}.quote(v)
}

// curlScript writes one curl command per flow, in order, for shell.
func curlScript(sh string, flows []flow.Flow, skipped int, secrets bool, now time.Time) string {
	c := &curlWriter{shell: sh}
	switch sh {
	case "sh":
		c.line("#!/bin/sh")
	case "cmd":
		c.line("@echo off")
		c.line("setlocal")
	}
	secret := "credential headers (Authorization, Proxy-Authorization, Cookie, Set-Cookie) read " + flow.Redacted + "; export with secrets=1 to keep them"
	if secrets {
		secret = "with secrets: credential headers are as captured"
	}
	c.comment("%d requests from FormNET %s, %s; %s.", len(flows), app.Version, now.UTC().Format(time.RFC3339), secret)
	if skipped > 0 {
		c.comment("%d matching flows that are not plain HTTP (tunnels, WebSockets, refused handshakes) are left out.", skipped)
	}
	switch sh {
	case "powershell":
		c.comment("PowerShell 5.1 and 7: curl.exe on Windows, curl elsewhere. Q passes double quotes intact before PowerShell 7.3.")
		c.line(`$curl = if ($null -eq $IsWindows -or $IsWindows) { 'curl.exe' } else { 'curl' }`)
		c.line(`function Q([string]$s) { if ($PSVersionTable.PSVersion -ge [version]'7.3') { return $s }; $s = $s -replace '(\\*)"', '$1$1\"'; if ($s -match '\s') { $s = $s -replace '(\\+)$', '$1$1' }; $s }`)
	case "cmd":
		c.comment("Save as a .cmd file and run it: percent signs are doubled for a batch file. curl.exe and certutil ship with Windows 10 and later.")
	}
	for i, f := range flows {
		c.seq = i + 1
		c.line("")
		c.flow(f, secrets)
	}
	return c.b.String()
}

func (c *curlWriter) flow(f flow.Flow, secrets bool) {
	status := "no answer"
	if f.Status > 0 {
		status = fmt.Sprintf("status %d", f.Status)
	}
	c.comment("%d. %s %s (%s, %s)", c.seq, f.Method, f.URL, f.Start.UTC().Format(time.RFC3339), status)
	body := f.Request.Body
	dropEncoding := false
	switch {
	case f.Request.BodyTruncated:
		c.comment("the request body is left out: FormNET captured only %d of its %d bytes.", len(body), f.Request.BodyBytes)
		body = nil
	case len(body) == 0 && f.Request.BodyBytes > 0:
		c.comment("the request body (%d bytes) was not captured and is left out.", f.Request.BodyBytes)
	case len(body) > curlBodyMax:
		c.comment("the request body is left out: it is %d bytes, over the 64 KiB a script carries.", len(body))
		body = nil
	case len(body) > 0 && f.Request.Decoded && f.Request.Encoding != "":
		c.comment("the body is sent decoded, so its Content-Encoding (%s) header is left out.", f.Request.Encoding)
		dropEncoding = true
	}
	args := []string{}
	method := strings.ToUpper(f.Method)
	switch {
	case method == "HEAD":
		args = append(args, "-I")
	case method == "GET" && len(body) == 0, method == "POST" && len(body) > 0:
	default:
		args = append(args, "-X "+c.arg(method))
	}
	args = append(args, c.arg(f.URL))
	for _, h := range f.Request.Headers {
		if curlSkip(h.Name) || (dropEncoding && strings.EqualFold(h.Name, "Content-Encoding")) {
			continue
		}
		v := h.Value
		if !secrets && flow.SecretHeader(h.Name) {
			v = flow.Redacted
		}
		v = strings.NewReplacer("\r", " ", "\n", " ").Replace(v)
		if v == "" {
			args = append(args, "-H "+c.arg(h.Name+";"))
			continue
		}
		args = append(args, "-H "+c.arg(h.Name+": "+v))
	}
	if len(body) == 0 {
		c.command(args, "")
		return
	}
	enc := base64.StdEncoding.EncodeToString(body)
	switch c.shell {
	case "sh":
		args = append(args, "--data-binary @-")
		if utf8.Valid(body) && !strings.ContainsRune(string(body), 0) {
			c.command(args, "printf '%s' "+c.arg(string(body))+" | ")
		} else {
			c.command(args, "printf '%s' "+c.arg(enc)+" | base64 --decode | ")
		}
	case "powershell":
		c.line("$body = [IO.Path]::GetTempFileName()")
		c.line("[IO.File]::WriteAllBytes($body, [Convert]::FromBase64String('" + enc + "'))")
		c.command(append(args, `--data-binary "@$body"`), "")
		c.line("Remove-Item -LiteralPath $body")
	case "cmd":
		c.line(`set "FORMNET_BODY=%TEMP%\formnet-body-%RANDOM%%RANDOM%"`)
		c.line(`> "%FORMNET_BODY%.b64" (`)
		for len(enc) > 0 {
			n := min(76, len(enc))
			c.line("echo " + enc[:n])
			enc = enc[n:]
		}
		c.line(")")
		c.line(`certutil -f -decode "%FORMNET_BODY%.b64" "%FORMNET_BODY%.bin" >nul`)
		c.command(append(args, `--data-binary "@%FORMNET_BODY%.bin"`), "")
		c.line(`del "%FORMNET_BODY%.b64" "%FORMNET_BODY%.bin"`)
	}
}

// command writes one curl command, continued over lines where the shell allows it.
func (c *curlWriter) command(args []string, prefix string) {
	switch c.shell {
	case "sh":
		c.line(prefix + "curl " + strings.Join(args, " \\\n  "))
	case "powershell":
		c.line("& $curl " + strings.Join(args, " `\n  "))
	case "cmd":
		line := "curl.exe " + strings.Join(args, " ")
		if len(line) > 8000 {
			c.comment("this command is longer than cmd accepts (8191 characters); use the PowerShell script for it.")
		}
		c.line(line)
	}
}

// cmdArg quotes one argument for a batch file running curl.exe: plain
// double quotes when the value holds no double quote, otherwise the C
// runtime's quoting with every character cmd reads escaped by a caret, so
// cmd never sees a quote and passes the rest as written. Percent signs are
// doubled, as a batch file needs.
func cmdArg(v string) string {
	if !strings.Contains(v, `"`) {
		trail := len(v) - len(strings.TrimRight(v, `\`))
		return `"` + strings.ReplaceAll(v, "%", "%%") + strings.Repeat(`\`, trail) + `"`
	}
	var q strings.Builder
	q.WriteByte('"')
	slashes := 0
	for _, r := range v {
		switch r {
		case '\\':
			slashes++
			continue
		case '"':
			q.WriteString(strings.Repeat(`\`, slashes*2+1))
		default:
			q.WriteString(strings.Repeat(`\`, slashes))
		}
		slashes = 0
		q.WriteRune(r)
	}
	q.WriteString(strings.Repeat(`\`, slashes*2))
	q.WriteByte('"')
	var out strings.Builder
	for _, r := range q.String() {
		switch r {
		case '%':
			out.WriteString("%%")
		case '(', ')', '!', '^', '"', '<', '>', '&', '|':
			out.WriteByte('^')
			out.WriteRune(r)
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}
