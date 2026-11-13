// Package tlstrust is the tls-trust-check command (Plan 00254 Task 4.2): a TLS handshake to one
// https url, verified against exactly the roots a rustls client given SSL_CERT_FILE and
// SSL_CERT_DIR reads, so a play can prove from inside a Flatpak sandbox that the client there
// will trust a team's server. The play runs it in Lightning's sandbox with the variables the
// team's launch sets (tasks/lightning-team-ca.yml).
//
// Lightning's Matrix TLS is rustls with rustls-native-certs: when SSL_CERT_FILE or SSL_CERT_DIR
// is set, only those are read, every regular file in each SSL_CERT_DIR entry is read as PEM (no
// hash names), and webpki verifies a name only against the certificate's SANs. Go's crypto/x509
// has no Common Name fallback either. The roots are read here, not through
// x509.SystemCertPool, so nothing beyond the two variables can reach the pool.
//
// Expectations (each has a test whose name starts with its number):
//
//	T1 A server whose chain ends at a certificate in SSL_CERT_FILE or in a file of SSL_CERT_DIR
//	   is trusted: "trusted<TAB><url>" on stdout, exit 0.
//	T2 A chain ending at no certificate there fails, naming the two variables.
//	T3 A server certificate with no SAN, or whose SANs do not name the url's host, fails, saying
//	   which.
//	T4 A certificate not valid at the check's time fails, saying so.
//	T5 SSL_CERT_FILE and SSL_CERT_DIR must both be set (the launch sets both: either alone drops
//	   the public roots or the team CA); a file or directory named that cannot be read fails
//	   naming it, as does SSL_CERT_FILE, or SSL_CERT_DIR's directories together, holding no
//	   certificate.
//	T6 SSL_CERT_DIR is a colon-separated list; every regular file of every entry is read, and an
//	   entry that is not a regular file (a pipe) is skipped, never opened.
//	T7 A server that cannot be reached or that ends the handshake fails, naming the address.
//	T8 The url must be https with a host; its port is 443 unless it names one.
//	T9 The arguments are exactly --url <url>; anything else is a usage error (exit 64).
//	T10 A verdict that cannot be written to stdout is exit 1.
//
// Every failure is one "tls-trust-check: " line on stderr, nothing on stdout, exit 1.
package tlstrust

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// handshakeLimit bounds the connection and the handshake together.
const handshakeLimit = 30 * time.Second

const usage = "usage: tls-trust-check --url https://<host>[:<port>]"

// App is one run of the command.
type App struct {
	Args   []string
	Getenv func(string) string
	Now    func() time.Time
	Stdout io.Writer
	Stderr io.Writer
}

// Run checks the handshake and returns the exit code.
func (a App) Run(ctx context.Context) term.ExitCode {
	target, err := parseArgs(a.Args)
	if err != nil {
		return a.fail(term.ExitUsage, err.Error()+"\n"+usage)
	}
	pool, err := a.roots()
	if err != nil {
		return a.fail(term.ExitFailure, err.Error())
	}
	if err := handshake(ctx, target, pool, a.Now); err != nil {
		return a.fail(term.ExitFailure, target.String()+": "+err.Error())
	}
	if _, err := fmt.Fprintf(a.Stdout, "trusted\t%s\n", target); err != nil {
		return a.fail(term.ExitFailure, "writing the verdict: "+err.Error())
	}
	return term.ExitOK
}

func (a App) fail(code term.ExitCode, msg string) term.ExitCode {
	if _, err := fmt.Fprintf(a.Stderr, "tls-trust-check: %s\n", msg); err != nil {
		return term.ExitFailure
	}
	return code
}

func parseArgs(args []string) (*url.URL, error) {
	if len(args) != 2 || args[0] != "--url" {
		return nil, errors.New("one --url argument is needed")
	}
	u, err := url.Parse(args[1])
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return nil, fmt.Errorf("%q is not an https url with a host", args[1])
	}
	return u, nil
}

// address is the url's host and port, 443 when it names none.
func address(u *url.URL) string {
	port := u.Port()
	if port == "" {
		port = "443"
	}
	return net.JoinHostPort(u.Hostname(), port)
}

// roots reads SSL_CERT_FILE and every regular file of each SSL_CERT_DIR entry.
func (a App) roots() (*x509.CertPool, error) {
	file, dirs := a.Getenv("SSL_CERT_FILE"), a.Getenv("SSL_CERT_DIR")
	for _, v := range []struct{ name, value string }{{"SSL_CERT_FILE", file}, {"SSL_CERT_DIR", dirs}} {
		if v.value == "" {
			return nil, errors.New(v.name + " is not set; the launch sets both it and the other")
		}
	}
	pool := x509.NewCertPool()
	data, err := os.ReadFile(filepath.Clean(file))
	if err != nil {
		return nil, fmt.Errorf("SSL_CERT_FILE %s cannot be read: %w", file, err)
	}
	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("SSL_CERT_FILE %s holds no certificate", file)
	}
	found := false
	for dir := range strings.SplitSeq(dirs, ":") {
		some, err := appendDir(pool, dir)
		if err != nil {
			return nil, err
		}
		found = found || some
	}
	if !found {
		return nil, fmt.Errorf("SSL_CERT_DIR %s holds no certificate", dirs)
	}
	return pool, nil
}

// appendDir adds the certificates of every regular file in dir, and says whether there were any.
func appendDir(pool *x509.CertPool, dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("SSL_CERT_DIR %s cannot be read: %w", dir, err)
	}
	found := false
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		info, err := os.Stat(path)
		if err != nil {
			return false, fmt.Errorf("SSL_CERT_DIR %s: %w", dir, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		data, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return false, fmt.Errorf("SSL_CERT_DIR %s: %w", dir, err)
		}
		if pool.AppendCertsFromPEM(data) {
			found = true
		}
	}
	return found, nil
}

func handshake(ctx context.Context, target *url.URL, pool *x509.CertPool, now func() time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, handshakeLimit)
	defer cancel()
	addr := address(target)
	dialer := tls.Dialer{Config: &tls.Config{
		RootCAs: pool, ServerName: target.Hostname(), MinVersion: tls.VersionTLS12, Time: now,
	}}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return cause(err, target.Hostname(), addr)
	}
	if err := conn.Close(); err != nil {
		return fmt.Errorf("closing the connection to %s: %w", addr, err)
	}
	return nil
}

// cause says why the handshake failed, in the terms of the store and the certificate.
func cause(err error, host, addr string) error {
	var unknown x509.UnknownAuthorityError
	var name x509.HostnameError
	var invalid x509.CertificateInvalidError
	switch {
	case errors.As(err, &unknown):
		return errors.New("the server certificate is not signed by a CA in SSL_CERT_FILE or SSL_CERT_DIR")
	case errors.As(err, &name):
		sans := sanNames(name.Certificate)
		if len(sans) == 0 {
			return errors.New("the server certificate carries no SAN (subjectAltName); " +
				"rustls verifies the host against SANs only, never the Common Name")
		}
		return fmt.Errorf("the server certificate's SANs (%s) do not name %s", strings.Join(sans, ", "), host)
	case errors.As(err, &invalid):
		return fmt.Errorf("the server certificate chain is not valid: %w", err)
	}
	return fmt.Errorf("no TLS handshake with %s: %w", addr, err)
}

func sanNames(cert *x509.Certificate) []string {
	names := append([]string{}, cert.DNSNames...)
	for _, ip := range cert.IPAddresses {
		names = append(names, ip.String())
	}
	return names
}
