package ca

import (
	"runtime"
	"strings"
)

// trustInstructions returns the command that makes this user's programs trust
// the root at path, a hint on what it does, and the command that removes it.
// Nothing here runs them.
func trustInstructions(path, commonName string) (command, hint, untrust string) {
	return trustInstructionsFor(runtime.GOOS, path, commonName)
}

func trustInstructionsFor(goos, path, commonName string) (command, hint, untrust string) {
	q := shellQuote(path)
	switch goos {
	case "darwin":
		return "security add-trusted-cert -r trustRoot -p ssl -k ~/Library/Keychains/login.keychain-db " + q,
			"Adds the root to your login keychain, trusted for SSL only and for your user only; macOS asks for your password in a dialog. Safari, Chrome and most apps then accept connections FormNET opens. Firefox 120 and later read the system store.",
			"security delete-certificate -c " + shellQuote(commonName) + " ~/Library/Keychains/login.keychain-db"
	case "linux":
		return "sudo cp " + q + " /usr/local/share/ca-certificates/formnet.crt && sudo update-ca-certificates",
			"Installs the root system-wide (needs sudo) on Debian and Ubuntu; on Fedora copy it to /etc/pki/ca-trust/source/anchors/ and run update-ca-trust. Firefox 120 and later read the system store. For your user only, and for Chrome and Chromium, which keep their own store in $HOME/.local/share/pki/nssdb: certutil -d sql:$HOME/.local/share/pki/nssdb -A -t C,, -n formnet -i " + q + " (remove with certutil -d sql:$HOME/.local/share/pki/nssdb -D -n formnet). If an older $HOME/.pki/nssdb folder already exists, Chromium keeps using that one: put it in both lines instead.",
			"sudo rm /usr/local/share/ca-certificates/formnet.crt && sudo update-ca-certificates --fresh"
	case "windows":
		return "certutil -addstore -user Root " + windowsQuote(path),
			"Adds the root to your user's Trusted Root store (no administrator needed); Windows shows a confirmation dialog. Edge, Chrome and most apps then accept connections FormNET opens. Firefox 120 and later read the system store.",
			"certutil -delstore -user Root " + windowsQuote(commonName)
	default:
		return "",
			"Import " + path + " as a trusted root certificate authority in this system's certificate store.",
			""
	}
}

// windowsQuote wraps s in double quotes for cmd.exe and PowerShell; Windows
// paths and host names cannot hold a double quote.
func windowsQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, "") + `"`
}

// shellQuote wraps s in single quotes, escaping the quotes inside it.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
