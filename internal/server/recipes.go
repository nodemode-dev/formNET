package server

import (
	"net"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
)

// Recipe is a set of lines that sends one kind of program through this run's
// proxy, for one shell, with this run's address and root certificate path.
type Recipe struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Shell string   `json:"shell"` // sh or powershell
	Lines []string `json:"lines"`
	Note  string   `json:"note"`
}

type recipesBody struct {
	ProxyAddr string   `json:"proxy_addr"`
	CAPath    string   `json:"ca_path"`
	Platform  string   `json:"platform"`
	Recipes   []Recipe `json:"recipes"`
}

func (s *handler) recipes(w http.ResponseWriter, r *http.Request) {
	addr, ca := s.Proxy.Status().Addr, s.CA.Info().Path
	writeJSON(w, http.StatusOK, recipesBody{ProxyAddr: addr, CAPath: ca, Platform: runtime.GOOS, Recipes: buildRecipes(runtime.GOOS, addr, ca)})
}

// shell writes lines for one shell; every value is quoted, so paths with
// spaces or quotes stay one argument.
type shell struct {
	name    string
	windows bool
}

func (sh shell) quote(v string) string {
	if sh.name == "powershell" {
		return "'" + strings.ReplaceAll(v, "'", "''") + "'"
	}
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

func (sh shell) env(name, value string) string {
	if sh.name == "powershell" {
		return "$env:" + name + " = " + sh.quote(value)
	}
	return "export " + name + "=" + sh.quote(value)
}

// buildRecipes returns the recipes for a server on goos: sh and PowerShell on
// macOS and Linux, PowerShell on Windows.
func buildRecipes(goos, proxyAddr, caPath string) []Recipe {
	shells := []shell{{name: "sh"}, {name: "powershell"}}
	if goos == "windows" {
		shells = []shell{{name: "powershell", windows: true}}
	}
	proxyURL := "http://" + proxyAddr
	host, port, err := net.SplitHostPort(proxyAddr)
	if err != nil {
		host, port = proxyAddr, ""
	}
	trustStore := filepath.Join(filepath.Dir(caPath), "formnet-truststore.p12")
	python, curl := "python3", "curl"
	if goos == "windows" {
		python, curl = "python", "curl.exe"
	}
	onlyRoot := " With it, HTTPS to a host FormNET does not open (one on the passthrough list, or any host while interception is off) fails verification."
	var out []Recipe
	for _, sh := range shells {
		q := sh.quote
		suffix := "-" + sh.name

		curlNote := "--cacert makes curl check servers against FormNET's root alone, for this command only." + onlyRoot
		if sh.windows {
			curlNote += " PowerShell's own curl is another command, so the line calls curl.exe."
		}
		out = append(out, Recipe{ID: "curl" + suffix, Title: "curl", Shell: sh.name, Lines: []string{
			curl + " --proxy " + q(proxyURL) + " --cacert " + q(caPath) + " https://example.com/",
		}, Note: curlNote})

		out = append(out, Recipe{ID: "node" + suffix, Title: "Node.js", Shell: sh.name, Lines: []string{
			sh.env("NODE_USE_ENV_PROXY", "1"),
			sh.env("HTTPS_PROXY", proxyURL),
			sh.env("HTTP_PROXY", proxyURL),
			sh.env("NODE_EXTRA_CA_CERTS", caPath),
			"node app.js",
		}, Note: "NODE_USE_ENV_PROXY=1 makes Node's built-in fetch, http and https follow HTTPS_PROXY and HTTP_PROXY; a Node.js version without it ignores the variable and connects directly. NODE_EXTRA_CA_CERTS adds FormNET's root to the roots Node already trusts, and is read only when the process starts."})

		out = append(out, Recipe{ID: "python" + suffix, Title: "Python requests and pip", Shell: sh.name, Lines: []string{
			sh.env("HTTPS_PROXY", proxyURL),
			sh.env("HTTP_PROXY", proxyURL),
			sh.env("REQUESTS_CA_BUNDLE", caPath),
			sh.env("SSL_CERT_FILE", caPath),
			python + " app.py",
			"pip install --proxy " + q(proxyURL) + " --cert " + q(caPath) + " some-package",
		}, Note: "requests reads HTTPS_PROXY, HTTP_PROXY and REQUESTS_CA_BUNDLE, which replaces the roots it trusts with FormNET's." + onlyRoot + " SSL_CERT_FILE is the certificate file OpenSSL's default paths name, which Python's ssl module (urllib and others) loads. pip takes the proxy and the root as options here."})

		javaArgs := []string{
			"-Dhttp.proxyHost=" + host, "-Dhttp.proxyPort=" + port,
			"-Dhttps.proxyHost=" + host, "-Dhttps.proxyPort=" + port,
			"-Djavax.net.ssl.trustStore=" + trustStore, "-Djavax.net.ssl.trustStorePassword=changeit", "-Djavax.net.ssl.trustStoreType=PKCS12",
		}
		for i, a := range javaArgs {
			javaArgs[i] = q(a)
		}
		out = append(out, Recipe{ID: "java" + suffix, Title: "Java", Shell: sh.name, Lines: []string{
			"keytool -importcert -noprompt -alias formnet -file " + q(caPath) + " -keystore " + q(trustStore) + " -storetype PKCS12 -storepass changeit",
			"java " + strings.Join(javaArgs, " ") + " -jar app.jar",
		}, Note: "keytool writes a trust store beside the root that holds FormNET's root alone; its password guards nothing secret. With that trust store Java trusts no other root." + onlyRoot + " Run keytool once: a second run stops because the alias exists. Java sends no request for localhost or 127.* through the proxy unless http.nonProxyHosts says otherwise."})

		goLines := []string{sh.env("HTTPS_PROXY", proxyURL), sh.env("HTTP_PROXY", proxyURL)}
		goNote := "Programs that use net/http's default transport (ProxyFromEnvironment) read HTTPS_PROXY and HTTP_PROXY, but never use a proxy for localhost or loopback addresses, so requests to local servers are not recorded."
		switch goos {
		case "darwin", "windows":
			goNote += " Go on this system checks certificates with the system's own store and does not read SSL_CERT_FILE, so trust the root first: formnet ca trust."
		default:
			goLines = append(goLines, sh.env("SSL_CERT_FILE", caPath))
			goNote += " SSL_CERT_FILE takes the place of the system's certificate bundle file for that program."
		}
		goLines = append(goLines, "./your-program")
		if sh.windows {
			goLines[len(goLines)-1] = ".\\your-program.exe"
		}
		out = append(out, Recipe{ID: "go" + suffix, Title: "Go programs", Shell: sh.name, Lines: goLines, Note: goNote})

		gitNote := "http.proxy covers https URLs too. http.sslCAInfo replaces the roots git trusts, for this command only." + onlyRoot
		if sh.windows {
			gitNote += " Git for Windows set to the schannel backend (http.sslBackend) ignores http.sslCAInfo unless http.schannelUseSSLCAInfo is true, and checks the Windows store instead: trust the root there (formnet ca trust)."
		}
		out = append(out, Recipe{ID: "git" + suffix, Title: "git", Shell: sh.name, Lines: []string{
			"git -c " + q("http.proxy="+proxyURL) + " -c " + q("http.sslCAInfo="+caPath) + " clone https://example.com/repo.git",
		}, Note: gitNote})
	}
	return out
}
