package tlstrust

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fifotest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// testNow is the instant every certificate here is valid at and the check verifies at.
var testNow = time.Date(2030, 6, 1, 12, 0, 0, 0, time.UTC)

type authority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func newAuthority(t *testing.T, name string) authority {
	t.Helper()
	key := newKey(t)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             testNow.Add(-time.Hour),
		NotAfter:              testNow.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return authority{cert: cert, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

// leaf is a server certificate the CA signs, naming ips as its SANs (none: a CN only).
func (a authority) leaf(t *testing.T, notAfter time.Time, ips ...net.IP) tls.Certificate {
	t.Helper()
	key := newKey(t)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    testNow.Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, a.cert, &key.PublicKey, a.key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// serve starts a TLS server presenting cert and returns its https url.
func serve(t *testing.T, cert tls.Certificate) string {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.NotFoundHandler())
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv.URL
}

// store writes the public bundle (SSL_CERT_FILE) and the team CA directory (SSL_CERT_DIR).
type store struct {
	file string
	dir  string
}

func newStore(t *testing.T, public authority, team ...authority) store {
	t.Helper()
	root := t.TempDir()
	s := store{file: filepath.Join(root, "ca-certificates.crt"), dir: filepath.Join(root, "teamchat-ca")}
	if err := os.WriteFile(s.file, public.pem, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(s.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for i, ca := range team {
		name := filepath.Join(s.dir, "team"+string(rune('a'+i))+".pem")
		if err := os.WriteFile(name, ca.pem, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func (s store) env(name string) string {
	switch name {
	case "SSL_CERT_FILE":
		return s.file
	case "SSL_CERT_DIR":
		return s.dir
	}
	return ""
}

type result struct {
	code   term.ExitCode
	stdout string
	stderr string
}

func check(t *testing.T, getenv func(string) string, args ...string) result {
	t.Helper()
	var out, errOut bytes.Buffer
	code := App{
		Args: args, Getenv: getenv, Now: func() time.Time { return testNow },
		Stdout: &out, Stderr: &errOut,
	}.Run(context.Background())
	return result{code: code, stdout: out.String(), stderr: errOut.String()}
}

func wantFailure(t *testing.T, r result, cause string) {
	t.Helper()
	if r.code != term.ExitFailure || r.stdout != "" || !strings.Contains(r.stderr, cause) {
		t.Fatalf("exit %d stdout %q stderr %q, want exit 1 naming %q", r.code, r.stdout, r.stderr, cause)
	}
	if !strings.HasPrefix(r.stderr, "tls-trust-check: ") {
		t.Fatalf("stderr %q does not name the command", r.stderr)
	}
}

func TestT1ATeamCAInTheDirectoryIsTrusted(t *testing.T) {
	public, team := newAuthority(t, "public root"), newAuthority(t, "team CA")
	url := serve(t, team.leaf(t, testNow.Add(time.Hour), net.IPv4(127, 0, 0, 1)))
	r := check(t, newStore(t, public, team).env, "--url", url)
	if r.code != term.ExitOK || r.stdout != "trusted\t"+url+"\n" || r.stderr != "" {
		t.Fatalf("exit %d stdout %q stderr %q", r.code, r.stdout, r.stderr)
	}
}

func TestT1APublicRootInTheFileIsTrusted(t *testing.T) {
	public := newAuthority(t, "public root")
	url := serve(t, public.leaf(t, testNow.Add(time.Hour), net.IPv4(127, 0, 0, 1)))
	if r := check(t, newStore(t, public, newAuthority(t, "team CA")).env, "--url", url); r.code != term.ExitOK {
		t.Fatalf("exit %d stderr %q", r.code, r.stderr)
	}
}

func TestT2APublicOnlyStoreDoesNotTrustTheTeam(t *testing.T) {
	public, team, other := newAuthority(t, "public root"), newAuthority(t, "team CA"), newAuthority(t, "other")
	url := serve(t, team.leaf(t, testNow.Add(time.Hour), net.IPv4(127, 0, 0, 1)))
	r := check(t, newStore(t, public, other).env, "--url", url)
	wantFailure(t, r, "not signed by a CA in SSL_CERT_FILE or SSL_CERT_DIR")
}

func TestT3ACertificateWithoutSANFails(t *testing.T) {
	public, team := newAuthority(t, "public root"), newAuthority(t, "team CA")
	url := serve(t, team.leaf(t, testNow.Add(time.Hour)))
	wantFailure(t, check(t, newStore(t, public, team).env, "--url", url), "carries no SAN")
}

func TestT3ASANForAnotherHostFails(t *testing.T) {
	public, team := newAuthority(t, "public root"), newAuthority(t, "team CA")
	url := serve(t, team.leaf(t, testNow.Add(time.Hour), net.IPv4(127, 0, 0, 2)))
	wantFailure(t, check(t, newStore(t, public, team).env, "--url", url), "do not name 127.0.0.1")
}

func TestT4AnExpiredCertificateFails(t *testing.T) {
	public, team := newAuthority(t, "public root"), newAuthority(t, "team CA")
	url := serve(t, team.leaf(t, testNow.Add(-time.Minute), net.IPv4(127, 0, 0, 1)))
	wantFailure(t, check(t, newStore(t, public, team).env, "--url", url), "is not valid")
}

func TestT5BothVariablesMustBeSet(t *testing.T) {
	s := newStore(t, newAuthority(t, "public root"), newAuthority(t, "team CA"))
	for _, unset := range []string{"SSL_CERT_FILE", "SSL_CERT_DIR"} {
		getenv := func(name string) string {
			if name == unset {
				return ""
			}
			return s.env(name)
		}
		wantFailure(t, check(t, getenv, "--url", "https://127.0.0.1:1"), unset+" is not set")
	}
}

func TestT5AnUnreadableStoreFailsNamingIt(t *testing.T) {
	s := newStore(t, newAuthority(t, "public root"), newAuthority(t, "team CA"))
	missing := filepath.Join(t.TempDir(), "absent")
	noFile := store{file: missing, dir: s.dir}
	wantFailure(t, check(t, noFile.env, "--url", "https://127.0.0.1:1"), missing)
	noDir := store{file: s.file, dir: s.dir + ":" + missing}
	wantFailure(t, check(t, noDir.env, "--url", "https://127.0.0.1:1"), missing)
}

func TestT5AStoreWithNoCertificateFails(t *testing.T) {
	s := newStore(t, newAuthority(t, "public root"))
	if err := os.WriteFile(filepath.Join(s.dir, "notes.txt"), []byte("no certificate here\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wantFailure(t, check(t, s.env, "--url", "https://127.0.0.1:1"), "SSL_CERT_DIR "+s.dir+" holds no certificate")
	empty := store{file: filepath.Join(s.dir, "notes.txt"), dir: newStore(t, newAuthority(t, "x"), newAuthority(t, "y")).dir}
	wantFailure(t, check(t, empty.env, "--url", "https://127.0.0.1:1"), "SSL_CERT_FILE "+empty.file+" holds no certificate")
}

func TestT6EveryDirectoryInTheListIsReadAndAPipeIsSkipped(t *testing.T) {
	public, team := newAuthority(t, "public root"), newAuthority(t, "team CA")
	url := serve(t, team.leaf(t, testNow.Add(time.Hour), net.IPv4(127, 0, 0, 1)))
	first := newStore(t, public)
	hit := fifotest.Plant(t, filepath.Join(first.dir, "pipe.pem"))
	second := newStore(t, public, team)
	both := store{file: first.file, dir: first.dir + ":" + second.dir}
	if r := check(t, both.env, "--url", url); r.code != term.ExitOK {
		t.Fatalf("exit %d stderr %q", r.code, r.stderr)
	}
	if hit() {
		t.Fatal("the named pipe in SSL_CERT_DIR was opened")
	}
}

func TestT7AServerThatCannotBeReachedFails(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	s := newStore(t, newAuthority(t, "public root"), newAuthority(t, "team CA"))
	wantFailure(t, check(t, s.env, "--url", "https://"+addr), "no TLS handshake with "+addr)
}

func TestT8TheURLMustBeHTTPSWithAHost(t *testing.T) {
	s := newStore(t, newAuthority(t, "public root"), newAuthority(t, "team CA"))
	for _, url := range []string{"http://127.0.0.1", "https://", "https://%zz", "127.0.0.1"} {
		r := check(t, s.env, "--url", url)
		if r.code != term.ExitUsage || !strings.Contains(r.stderr, "an https url") {
			t.Fatalf("url %q: exit %d stderr %q", url, r.code, r.stderr)
		}
	}
}

func TestT8TheDefaultPortIs443(t *testing.T) {
	if got := address(mustURL(t, "https://team.example")); got != "team.example:443" {
		t.Fatalf("address %q", got)
	}
	if got := address(mustURL(t, "https://[::1]:8448/x")); got != "[::1]:8448" {
		t.Fatalf("address %q", got)
	}
}

func TestT9ArgumentsOtherThanOneURLAreAUsageError(t *testing.T) {
	s := newStore(t, newAuthority(t, "public root"), newAuthority(t, "team CA"))
	for _, args := range [][]string{nil, {"--url"}, {"--url", "https://a", "x"}, {"--other", "https://a"}} {
		r := check(t, s.env, args...)
		if r.code != term.ExitUsage || !strings.Contains(r.stderr, "usage: tls-trust-check --url") {
			t.Fatalf("args %q: exit %d stderr %q", args, r.code, r.stderr)
		}
	}
}

func TestT10AVerdictThatCannotBeWrittenFails(t *testing.T) {
	public, team := newAuthority(t, "public root"), newAuthority(t, "team CA")
	url := serve(t, team.leaf(t, testNow.Add(time.Hour), net.IPv4(127, 0, 0, 1)))
	var errOut bytes.Buffer
	code := App{
		Args: []string{"--url", url}, Getenv: newStore(t, public, team).env, Now: func() time.Time { return testNow },
		Stdout: failingWriter{}, Stderr: &errOut,
	}.Run(context.Background())
	if code != term.ExitFailure || !strings.Contains(errOut.String(), "writing the verdict") {
		t.Fatalf("exit %d stderr %q", code, errOut.String())
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }
