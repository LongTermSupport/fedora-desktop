package matrix

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/pki"
)

// tlsWorld is the certificates' fake clock, which is also the clock the client reads their
// validity by, and the maker that signs on it.
type tlsWorld struct {
	clk   *clock.Fake
	maker pki.Maker
}

func newTLSWorld() tlsWorld {
	clk := clock.NewFake(0, fixedNow().UnixMilli())
	return tlsWorld{clk: clk, maker: pki.Maker{Clock: clk}}
}

func (w tlsWorld) ca(t *testing.T, name string) pki.CA {
	t.Helper()
	ca, err := w.maker.NewCA(name)
	if err != nil {
		t.Fatalf("NewCA: %v", err)
	}
	return ca
}

func (w tlsWorld) leaf(t *testing.T, ca pki.CA, ips ...string) pki.Leaf {
	t.Helper()
	addrs := make([]netip.Addr, 0, len(ips))
	for _, ip := range ips {
		addrs = append(addrs, netip.MustParseAddr(ip))
	}
	leaf, err := w.maker.NewLeaf(ca, pki.Names{IPs: addrs})
	if err != nil {
		t.Fatalf("NewLeaf: %v", err)
	}
	return leaf
}

// serveTLS starts the fake homeserver behind leaf, on 127.0.0.1; shape changes the server's TLS
// configuration before it is used.
func serveTLS(t *testing.T, leaf pki.Leaf, shape func(*tls.Config)) *fakehs.Server {
	t.Helper()
	pair, err := tls.X509KeyPair(leaf.CertPEM(), leaf.KeyPEM())
	if err != nil {
		t.Fatalf("key pair: %v", err)
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
	if shape != nil {
		shape(cfg)
	}
	srv := fakehs.New(fakehs.Options{ServerName: serverName, Now: fixedNow, Quirks: fakehs.TuwunelQuirks()})
	if err := srv.StartTLS(cfg); err != nil {
		t.Fatalf("StartTLS: %v", err)
	}
	t.Cleanup(func() {
		if err := srv.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return srv
}

// config is a member client's configuration for base, pinned to ca.
func (w tlsWorld) config(base string, ca pki.CA) Config {
	return Config{BaseURL: base, CAPEM: string(ca.CertPEM()), Token: "unused", Clock: &fakeClock{}, Wall: w.clk}
}

// client is a member client of srv pinned to ca, closed with the test.
func (w tlsWorld) client(t *testing.T, srv *fakehs.Server, ca pki.CA) *Client {
	t.Helper()
	c, err := NewMember(w.config(srv.URL(), ca))
	if err != nil {
		t.Fatalf("NewMember: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

func fingerprintOf(t *testing.T, ca pki.CA) string {
	t.Helper()
	fp, err := pki.Fingerprint(ca.CertPEM())
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	return fp
}

// wantTLSFailure holds that a call from a client pinned to ca fails the way X16 says: a fixed
// detail naming the host and the pinned CA's fingerprint and the class of failure, no
// certificate text, and not a failure to retry.
func (w tlsWorld) wantTLSFailure(t *testing.T, srv *fakehs.Server, ca pki.CA, class string, others ...pki.CA) {
	t.Helper()
	c := w.client(t, srv, ca)
	reached := len(srv.Requests())
	_, err := c.Versions(t.Context())
	host := strings.TrimPrefix(srv.URL(), "https://")
	want := "TLS verification of " + host + " failed against the pinned CA sha256:" + fingerprintOf(t, ca) + ": " + class
	e := wantErr(t, err, ErrUnreachable, want)
	if e.Transient() {
		t.Error("a certificate failure is marked transient")
	}
	if e.Status != 0 || e.Errcode != "" {
		t.Errorf("status %d errcode %q for a call that got no answer", e.Status, e.Errcode)
	}
	text := err.Error()
	for _, leak := range []string{"BEGIN", "x509", "tls:", "certificate signed by"} {
		if strings.Contains(text, leak) {
			t.Errorf("error %q carries %q", text, leak)
		}
	}
	for _, line := range pemBodyLines(append([]pki.CA{ca}, others...)) {
		if strings.Contains(text, line) {
			t.Errorf("error carries certificate bytes %q", line)
		}
	}
	if got := len(srv.Requests()); got != reached {
		t.Errorf("%d requests reached a server the client did not trust", got-reached)
	}
}

// pemBodyLines are the base64 lines of each CA certificate.
func pemBodyLines(cas []pki.CA) []string {
	var out []string
	for _, ca := range cas {
		for line := range strings.SplitSeq(string(ca.CertPEM()), "\n") {
			if line != "" && !strings.HasPrefix(line, "-----") {
				out = append(out, line)
			}
		}
	}
	return out
}

func TestTLSRightCAConnects(t *testing.T) {
	w := newTLSWorld()
	ca := w.ca(t, "team-a")
	srv := serveTLS(t, w.leaf(t, ca, "127.0.0.1"), nil)
	uid := srv.AddUser("alice", "secret-alice", false)
	token, err := srv.MintToken(uid)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	cfg := w.config(srv.URL(), ca)
	cfg.Token = token
	c, err := NewMember(cfg)
	if err != nil {
		t.Fatalf("NewMember: %v", err)
	}
	t.Cleanup(c.Close)
	if _, versionsErr := c.Versions(t.Context()); versionsErr != nil {
		t.Fatalf("versions over TLS: %v", versionsErr)
	}
	who, err := c.Whoami(t.Context())
	if err != nil || who.UserID != uid {
		t.Fatalf("whoami over TLS = %+v, %v", who, err)
	}
	if got := lastRequest(t, srv).Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
		t.Errorf("Authorization = %q", got)
	}
}

func TestTLSAnotherCAFails(t *testing.T) {
	w := newTLSWorld()
	signer := w.ca(t, "team-a")
	pinned := w.ca(t, "team-b")
	srv := serveTLS(t, w.leaf(t, signer, "127.0.0.1"), nil)
	w.wantTLSFailure(t, srv, pinned, "unknown authority", signer)
}

func TestTLSCertificateForAnotherAddressFails(t *testing.T) {
	w := newTLSWorld()
	ca := w.ca(t, "team-a")
	srv := serveTLS(t, w.leaf(t, ca, "192.0.2.10", "2001:db8::1"), nil)
	w.wantTLSFailure(t, srv, ca, "name mismatch")
}

func TestTLSExpiredCertificateFailsOnTheInjectedClock(t *testing.T) {
	w := newTLSWorld()
	ca := w.ca(t, "team-a")
	srv := serveTLS(t, w.leaf(t, ca, "127.0.0.1"), nil)
	c := w.client(t, srv, ca)
	if _, err := c.Versions(t.Context()); err != nil {
		t.Fatalf("a fresh certificate: %v", err)
	}
	w.clk.Advance(400 * 24 * time.Hour)
	c.Close()
	w.wantTLSFailure(t, srv, ca, "expired or not yet valid")
}

func TestTLSCertificateNotYetValidFails(t *testing.T) {
	w := newTLSWorld()
	ca := w.ca(t, "team-a")
	srv := serveTLS(t, w.leaf(t, ca, "127.0.0.1"), nil)
	cfg := w.config(srv.URL(), ca)
	cfg.Wall = clock.NewFake(0, fixedNow().Add(-48*time.Hour).UnixMilli())
	c, err := NewMember(cfg)
	if err != nil {
		t.Fatalf("NewMember: %v", err)
	}
	t.Cleanup(c.Close)
	_, err = c.Versions(t.Context())
	wantErr(t, err, ErrUnreachable, "TLS verification of "+strings.TrimPrefix(srv.URL(), "https://")+
		" failed against the pinned CA sha256:"+fingerprintOf(t, ca)+": expired or not yet valid")
}

func TestTLSOneDotTwoServerFails(t *testing.T) {
	w := newTLSWorld()
	ca := w.ca(t, "team-a")
	srv := serveTLS(t, w.leaf(t, ca, "127.0.0.1"), func(c *tls.Config) {
		c.MinVersion = tls.VersionTLS12
		c.MaxVersion = tls.VersionTLS12
	})
	w.wantTLSFailure(t, srv, ca, "handshake refused (TLS 1.3 is the minimum)")
}

func TestTLSPlainServerUnderAnHTTPSURLFails(t *testing.T) {
	w := newTLSWorld()
	ca := w.ca(t, "team-a")
	plain := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) { rw.WriteHeader(http.StatusOK) }))
	t.Cleanup(plain.Close)
	base := "https://" + strings.TrimPrefix(plain.URL, "http://")
	c, err := NewMember(w.config(base, ca))
	if err != nil {
		t.Fatalf("NewMember: %v", err)
	}
	t.Cleanup(c.Close)
	_, err = c.Versions(t.Context())
	wantErr(t, err, ErrUnreachable, "TLS verification of "+strings.TrimPrefix(base, "https://")+
		" failed against the pinned CA sha256:"+fingerprintOf(t, ca)+": not a TLS server")
}

func TestTLSHTTPSWithoutACAIsRefusedAtConstruction(t *testing.T) {
	for _, base := range []string{"https://127.0.0.1:8448", "https://192.0.2.7:8448", "https://[2001:db8::1]:8448", "https://hs.example.org"} {
		_, err := NewMember(Config{BaseURL: base, Token: "t"})
		wantErr(t, err, ErrInvalid, "an https URL needs the team's CA certificate")
	}
}

func TestTLSUnusableCAIsRefusedAtConstruction(t *testing.T) {
	w := newTLSWorld()
	ca := w.ca(t, "team-a")
	leaf := w.leaf(t, ca, "127.0.0.1")
	const junk = "this-is-not-a-pem-block-9f3a"
	for name, caPEM := range map[string]string{
		"junk":             junk,
		"two certificates": string(ca.CertPEM()) + string(ca.CertPEM()),
		"a leaf, not a CA": string(leaf.CertPEM()),
		"a key":            string(ca.KeyPEM()),
		"trailing junk":    string(ca.CertPEM()) + junk,
	} {
		cfg := Config{BaseURL: "https://127.0.0.1:8448", CAPEM: caPEM, Token: "t", Wall: w.clk}
		_, err := NewMember(cfg)
		e := wantErr(t, err, ErrInvalid, "")
		if !strings.Contains(e.Detail, "CA certificate") {
			t.Errorf("%s: detail %q does not say the CA certificate is the problem", name, e.Detail)
		}
		for _, leak := range []string{junk, "BEGIN", "x509"} {
			if strings.Contains(err.Error(), leak) {
				t.Errorf("%s: error %q carries %q", name, err.Error(), leak)
			}
		}
	}
}

func TestPlainHTTPIsForALoopbackLiteralOnly(t *testing.T) {
	w := newTLSWorld()
	ca := w.ca(t, "team-a")
	refused := []string{
		"http://198.51.100.1:8008", "http://[2001:db8::1]:8008", "http://203.0.113.9", "http://localhost:8008", "http://hs.example.org",
	}
	for _, base := range refused {
		listed := Config{BaseURL: base, PlainHTTPHosts: []string{"198.51.100.1", "2001:db8::1", "203.0.113.9", "localhost"}, Token: "t"}
		_, err := NewMember(listed)
		wantErr(t, err, ErrInvalid, "plain http is only for a loopback IP literal (127.0.0.0/8 or ::1)")
		withCA := Config{BaseURL: base, CAPEM: string(ca.CertPEM()), Token: "t"}
		if _, err := NewMember(withCA); err == nil {
			t.Errorf("%s with a CA accepted over plain http", base)
		}
	}
	for _, base := range []string{"http://127.0.0.1:8008", "http://[::1]:8008", "http://127.0.0.2:8008"} {
		c, err := NewMember(Config{BaseURL: base, Token: "t"})
		if err != nil {
			t.Errorf("%s without a list or a CA refused: %v", base, err)
			continue
		}
		c.Close()
	}
}

func TestPlainHTTPWithACAIsRefused(t *testing.T) {
	w := newTLSWorld()
	ca := w.ca(t, "team-a")
	_, err := NewMember(Config{BaseURL: "http://127.0.0.1:8008", CAPEM: string(ca.CertPEM()), Token: "t"})
	wantErr(t, err, ErrInvalid, "a CA certificate is for an https URL, not a loopback http one")
}

func TestAdminTakesNoCA(t *testing.T) {
	w := newTLSWorld()
	ca := w.ca(t, "team-a")
	_, err := NewAdmin(Config{BaseURL: "http://127.0.0.1:8008", CAPEM: string(ca.CertPEM())})
	wantErr(t, err, ErrInvalid, "an admin client takes no CA certificate")
	if _, err := NewAdmin(Config{BaseURL: "https://127.0.0.1:8008", CAPEM: string(ca.CertPEM())}); err == nil {
		t.Error("an admin client reached an https URL")
	}
}

// transportOf is the transport under a client.
func transportOf(t *testing.T, c *Client) *http.Transport {
	t.Helper()
	tr, ok := c.http.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T", c.http.Transport)
	}
	return tr
}

func TestTLSConfigTrustsExactlyTheOneCAForTheURLHost(t *testing.T) {
	w := newTLSWorld()
	ca := w.ca(t, "team-a")
	other := w.ca(t, "team-b")
	for base, serverName := range map[string]string{
		"https://192.0.2.7:8448":     "192.0.2.7",
		"https://[2001:db8::1]:8448": "2001:db8::1",
		"https://Matrix.Example.ORG": "matrix.example.org",
	} {
		c, err := NewMember(w.config(base, ca))
		if err != nil {
			t.Fatalf("%s: %v", base, err)
		}
		tr := transportOf(t, c)
		if tr.TLSClientConfig == nil {
			t.Fatalf("%s: no TLS configuration", base)
		}
		if tr.Proxy != nil {
			t.Errorf("%s: a proxy is set", base)
		}
		checkRoots(t, base, tr.TLSClientConfig, ca, other)
		checkVerification(t, base, tr.TLSClientConfig, serverName)
		c.Close()
	}
	plain, err := NewMember(Config{BaseURL: "http://127.0.0.1:8008", Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	if transportOf(t, plain).TLSClientConfig != nil {
		t.Error("a plain http client carries a TLS configuration")
	}
}

// checkRoots holds the root pool to exactly the pinned CA: not nil (nil is the system's roots),
// not the pinned CA with another beside it.
func checkRoots(t *testing.T, base string, cfg *tls.Config, pinned, other pki.CA) {
	t.Helper()
	one, err := pki.Pool(pinned.CertPEM())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RootCAs == nil || !cfg.RootCAs.Equal(one) {
		t.Errorf("%s: the root pool is not exactly the pinned CA", base)
	}
	two, err := pki.Pool(other.CertPEM())
	if err != nil {
		t.Fatal(err)
	}
	two.AddCert(parseCert(t, pinned.CertPEM()))
	if cfg.RootCAs != nil && cfg.RootCAs.Equal(two) {
		t.Errorf("%s: the root pool also holds another CA", base)
	}
}

// checkVerification holds the version floor, the name checked, the clock and that nothing
// replaces or skips the verification.
func checkVerification(t *testing.T, base string, cfg *tls.Config, serverName string) {
	t.Helper()
	if cfg.MinVersion != tls.VersionTLS13 || cfg.ServerName != serverName {
		t.Errorf("%s: MinVersion %#x ServerName %q, want TLS 1.3 and %q", base, cfg.MinVersion, cfg.ServerName, serverName)
	}
	if cfg.InsecureSkipVerify || cfg.VerifyConnection != nil || cfg.VerifyPeerCertificate != nil || cfg.Time == nil {
		t.Errorf("%s: the verification is weakened or off the injected clock", base)
		return
	}
	if got := cfg.Time(); !got.Equal(fixedNow()) {
		t.Errorf("%s: certificate time = %v, want the injected clock's %v", base, got, fixedNow())
	}
}

func parseCert(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()
	block, rest := pem.Decode(certPEM)
	if block == nil || len(rest) != 0 {
		t.Fatal("not exactly one PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

// TestTLSSystemRootsAreNotConsulted gives the process a system pool that trusts the signer, then
// pins another CA: the connection still fails. Go reads SSL_CERT_FILE once, when the system roots
// are first needed, so this must be the one test of this package that needs them, and the signer
// has the same key on every run of it (go test -count=2 reuses the pool the first run loaded).
// The check that the pool trusts the signer fails the test loudly if another test loads the pool
// first.
func TestTLSSystemRootsAreNotConsulted(t *testing.T) {
	w := newTLSWorld()
	signerMaker := pki.Maker{Clock: w.clk, Rand: bytes.NewReader(bytes.Repeat([]byte{0x2a}, 64))}
	signer, err := signerMaker.NewCA("team-system")
	if err != nil {
		t.Fatalf("NewCA: %v", err)
	}
	pinned := w.ca(t, "team-pinned")
	roots := filepath.Join(t.TempDir(), "roots.pem")
	if err = os.WriteFile(roots, signer.CertPEM(), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSL_CERT_FILE", roots)
	t.Setenv("SSL_CERT_DIR", filepath.Dir(roots))
	leaf := w.leaf(t, signer, "127.0.0.1")
	opts := x509.VerifyOptions{DNSName: "127.0.0.1", CurrentTime: fixedNow(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	if _, err = parseCert(t, leaf.CertPEM()).Verify(opts); err != nil {
		t.Fatalf("the system roots do not trust the signer, so this test proves nothing (the system pool was loaded before this test set SSL_CERT_FILE): %v", err)
	}
	srv := serveTLS(t, leaf, nil)
	w.wantTLSFailure(t, srv, pinned, "unknown authority", signer)
}
