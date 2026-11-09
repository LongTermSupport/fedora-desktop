package seat

import (
	"crypto/tls"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/pki"
)

// Plan 00246 X12 and X15: a seat bundle is v 2, and tls_ca (the team CA certificate in PEM)
// replaces plain_http_hosts: required for an https base_url, empty for a loopback http one.

const httpsBase = "https://chat.example.org:8448"

// tlsMaker makes certificates on the real clock, so they are valid whenever a test runs: the
// member clients that connect to them read the system clock.
func tlsMaker() pki.Maker { return pki.Maker{Clock: clock.System()} }

func mustCA(t *testing.T, name string) pki.CA {
	t.Helper()
	ca, err := tlsMaker().NewCA(name)
	if err != nil {
		t.Fatalf("NewCA: %v", err)
	}
	return ca
}

func mustLeaf(t *testing.T, ca pki.CA, ip string) pki.Leaf {
	t.Helper()
	leaf, err := tlsMaker().NewLeaf(ca, pki.Names{IPs: []netip.Addr{netip.MustParseAddr(ip)}})
	if err != nil {
		t.Fatalf("NewLeaf: %v", err)
	}
	return leaf
}

// httpsBundle is the test handle's pool seat.json for an https team pinned to ca.
func httpsBundle(t *testing.T, ca pki.CA) Bundle {
	t.Helper()
	b := bundleAt(t, httpsBase)
	b.TLSCA = string(ca.CertPEM())
	return b
}

// v1Form is doc as a v 1 bundle was: plain_http_hosts where v 2 has tls_ca.
func v1Form(t *testing.T, doc []byte) []byte {
	t.Helper()
	v1 := reshape(t, string(doc), "tls_ca", "")
	v1 = reshape(t, string(v1), "plain_http_hosts", `[]`)
	return reshape(t, string(v1), "v", `1`)
}

// X15: the pool seat.json is v 2, has tls_ca and has no plain_http_hosts.
func TestBundleV2CarriesTheCAAndNotPlainHTTPHosts(t *testing.T) {
	data, err := httpsBundle(t, mustCA(t, "team-a")).Encode()
	if err != nil {
		t.Fatal(err)
	}
	got := decodeJSON(t, string(data))
	if got["v"] != 2.0 {
		t.Errorf("v = %v, want 2: %s", got["v"], data)
	}
	if _, has := got["plain_http_hosts"]; has {
		t.Errorf("a v 2 seat.json still has plain_http_hosts: %s", data)
	}
	if ca, isText := got["tls_ca"].(string); !isText || !strings.HasPrefix(ca, "-----BEGIN CERTIFICATE-----") {
		t.Errorf("tls_ca is not the CA certificate in PEM: %s", data)
	}
	loop, err := testBundle(t).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if ca, has := decodeJSON(t, string(loop))["tls_ca"]; !has || ca != "" {
		t.Errorf("a loopback seat.json has tls_ca %v, want the key present and empty: %s", ca, loop)
	}
}

// X15: a v 1 bundle is refused naming its version, by every decoder that takes a bundle.
func TestBundleV1IsRefusedNamingTheVersion(t *testing.T) {
	pool, err := testBundle(t).Encode()
	if err != nil {
		t.Fatal(err)
	}
	held, err := HeldBundle{Bundle: testBundle(t), Claim: mustClaim(t, "00112233aabbccdd")}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	issued, err := Issued{Seat: testBundle(t), Kind: KindToken, Secret: mustSecret(t, "s3cret")}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	v1Issued := reshape(t, string(issued), "seat", string(v1Form(t, pool)))
	decoders := map[string]func() error{
		"pool":   func() error { _, e := DecodeBundle(v1Form(t, pool)); return e },
		"held":   func() error { _, e := DecodeHeldBundle(v1Form(t, held)); return e },
		"member": func() error { _, e := DecodeMemberBundle(v1Form(t, held)); return e },
		"issued": func() error { _, e := DecodeIssued(v1Issued); return e },
		"pool v 3": func() error {
			_, e := DecodeBundle(reshape(t, string(pool), "v", `3`))
			return e
		},
	}
	for name, decode := range decoders {
		err := decode()
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want an ErrInvalid", name, err)
			continue
		}
		want := "v is 1"
		if name == "pool v 3" {
			want = "v is 3"
		}
		if !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "v 2") {
			t.Errorf("%s: %q does not name the version (%q) and the one read (v 2)", name, err, want)
		}
	}
}

// X12, X15: an https base_url needs the CA; a loopback http one takes none, a present value
// with http being refused; both loopback families are plain.
func TestBundleCAIsRequiredForHTTPSAndAbsentForLoopback(t *testing.T) {
	ca := mustCA(t, "team-a")
	for name, tc := range map[string]struct {
		base string
		ca   string
		ok   bool
	}{
		"https with the CA":           {httpsBase, string(ca.CertPEM()), true},
		"https with no CA":            {httpsBase, "", false},
		"https to an address, no CA":  {"https://192.0.2.10:8448", "", false},
		"loopback v4 with no CA":      {"http://127.0.0.1:8448", "", true},
		"loopback v6 with no CA":      {"http://[::1]:8448", "", true},
		"loopback v4 with a CA":       {"http://127.0.0.1:8448", string(ca.CertPEM()), false},
		"loopback v6 with a CA":       {"http://[::1]:8448", string(ca.CertPEM()), false},
		"http to a name, no CA":       {"http://example.org:8448", "", false},
		"http to an address, no CA":   {"http://192.0.2.10:8448", "", false},
		"http to an address, with CA": {"http://192.0.2.10:8448", string(ca.CertPEM()), false},
	} {
		b := bundleAt(t, tc.base)
		b.TLSCA = tc.ca
		data, err := b.Encode()
		if (err == nil) != tc.ok {
			t.Errorf("%s: Encode err = %v, want ok = %v", name, err, tc.ok)
		}
		if err != nil && !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: Encode err = %v, want an ErrInvalid", name, err)
		}
		if err != nil {
			// The same document written by hand is refused by the decoder too.
			hand := reshape(t, mustEncode(t, testBundle(t)), "base_url", encode(t, tc.base))
			data = reshape(t, string(hand), "tls_ca", encode(t, tc.ca))
		}
		_, derr := DecodeBundle(data)
		if (derr == nil) != tc.ok {
			t.Errorf("%s: Decode err = %v, want ok = %v", name, derr, tc.ok)
		}
		if !tc.ok && derr != nil && !strings.Contains(derr.Error(), "tls_ca") {
			t.Errorf("%s: %q does not name tls_ca", name, derr)
		}
	}
}

func mustEncode(t *testing.T, b Bundle) string {
	t.Helper()
	data, err := b.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// X15: tls_ca is one CERTIFICATE PEM block of a CA and nothing else; no refusal shows its text.
func TestBundleCAIsOneCertificateBlockOfACA(t *testing.T) {
	ca, other := mustCA(t, "team-a"), mustCA(t, "team-b")
	leaf := mustLeaf(t, ca, "192.0.2.10")
	for name, text := range map[string]string{
		"junk":              "not a certificate",
		"two certificates":  string(ca.CertPEM()) + string(other.CertPEM()),
		"a leaf":            string(leaf.CertPEM()),
		"the CA key":        string(ca.KeyPEM()),
		"trailing bytes":    string(ca.CertPEM()) + "x",
		"leading bytes":     "x" + string(ca.CertPEM()),
		"a truncated block": string(ca.CertPEM())[:len(ca.CertPEM())/2],
		"white space only":  " \n",
	} {
		b := bundleAt(t, httpsBase)
		b.TLSCA = text
		if _, err := b.Encode(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: Encode err = %v, want an ErrInvalid", name, err)
		}
		doc := reshape(t, mustEncode(t, httpsBundle(t, ca)), "tls_ca", encode(t, text))
		_, err := DecodeBundle(doc)
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: Decode err = %v, want an ErrInvalid", name, err)
			continue
		}
		if strings.Contains(err.Error(), "BEGIN") || strings.Contains(err.Error(), "junk") {
			t.Errorf("%s: the refusal shows the text: %q", name, err)
		}
	}
	if _, err := httpsBundle(t, ca).Encode(); err != nil {
		t.Errorf("the one CA block: %v", err)
	}
}

// X15: the pool record round-trips with the CA.
func TestPoolBundleRoundTripsWithACA(t *testing.T) {
	b := httpsBundle(t, mustCA(t, "team-a"))
	back, err := DecodeBundle([]byte(mustEncode(t, b)))
	if err != nil || encode(t, back) != encode(t, b) {
		t.Errorf("pool round trip: %v, %s", err, encode(t, back))
	}
	if back.TLSCA != b.TLSCA {
		t.Errorf("the CA came back as %q", back.TLSCA)
	}
}

// X15: the held record round-trips with the CA.
func TestHeldBundleRoundTripsWithACA(t *testing.T) {
	h := HeldBundle{Bundle: httpsBundle(t, mustCA(t, "team-a")), Claim: mustClaim(t, "00112233aabbccdd"), ReleasedMS: new(int64(7))}
	data, err := h.Encode()
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeHeldBundle(data)
	if err != nil || encode(t, back) != encode(t, h) {
		t.Errorf("held round trip: %v, %s", err, encode(t, back))
	}
	if back.TLSCA != h.TLSCA {
		t.Errorf("the CA came back as %q", back.TLSCA)
	}
}

// X15: the member's reading of a seat directory's seat.json carries the CA in either shape.
func TestMemberBundleCarriesTheCAInEitherShape(t *testing.T) {
	b := httpsBundle(t, mustCA(t, "team-a"))
	held, err := HeldBundle{Bundle: b, Claim: mustClaim(t, "00112233aabbccdd")}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"pool": []byte(mustEncode(t, b)), "held": held} {
		mb, merr := DecodeMemberBundle(data)
		if merr != nil || mb.Bundle.TLSCA != b.TLSCA {
			t.Errorf("member bundle (%s): %v, %+v", name, merr, mb)
		}
		if (name == "held") != (mb.Held != nil) {
			t.Errorf("member bundle (%s) is in the wrong shape: %+v", name, mb)
		}
	}
}

// X15: the issued document round-trips with the CA and its secret.
func TestIssuedBundleRoundTripsWithACA(t *testing.T) {
	b := httpsBundle(t, mustCA(t, "team-a"))
	issued, err := Issued{Seat: b, Kind: KindPassword, Secret: mustSecret(t, "s3cret")}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeIssued(issued)
	if err != nil || back.Seat.TLSCA != b.TLSCA || back.Secret.Reveal() != "s3cret" || back.Kind != KindPassword {
		t.Errorf("issued round trip: %v, %+v", err, back)
	}
}

// X15: the 16 KiB cap holds, and one ECDSA CA is a small part of it, in every record that
// carries it.
func TestBundleWithACAIsFarWithinTheCap(t *testing.T) {
	ca := mustCA(t, "team-a")
	if n := len(ca.CertPEM()); n > 1024 {
		t.Errorf("an ECDSA CA PEM is %d bytes, over the 1 KiB the cap budgets for", n)
	}
	b := httpsBundle(t, ca)
	held, err := HeldBundle{Bundle: b, Claim: mustClaim(t, "00112233aabbccdd")}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	issued, err := Issued{Seat: b, Kind: KindToken, Secret: mustSecret(t, "s3cret")}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"pool": mustEncode(t, b), "held": string(held), "issued": string(issued)} {
		if len(data) > MaxBundle/4 {
			t.Errorf("a %s record is %d bytes, over a quarter of the %d cap", name, len(data), MaxBundle)
		}
	}
}

// X15: a seat.json over the cap is refused, whichever field makes it large.
func TestBundleCAOverTheCapIsRefused(t *testing.T) {
	ca := mustCA(t, "team-a")
	doc := mustEncode(t, httpsBundle(t, ca))
	big := string(ca.CertPEM()) + strings.Repeat("\n", MaxBundle)
	if _, err := DecodeBundle(reshape(t, doc, "tls_ca", encode(t, big))); !errors.Is(err, ErrInvalid) {
		t.Errorf("a tls_ca over the cap decoded: %v", err)
	}
}

// X15: the member client a bundle gives carries its URL and its CA.
func TestBundleMemberConfigCarriesTheURLAndTheCA(t *testing.T) {
	ca := mustCA(t, "team-a")
	cfg := httpsBundle(t, ca).MemberConfig("tok")
	if cfg.BaseURL != httpsBase || cfg.CAPEM != string(ca.CertPEM()) || cfg.Token != "tok" {
		t.Errorf("MemberConfig = %+v", cfg)
	}
	if cfg := testBundle(t).MemberConfig("tok"); cfg.BaseURL != testBase || cfg.CAPEM != "" {
		t.Errorf("a loopback MemberConfig = %+v", cfg)
	}
}

// startTLSHomeserver is a fakehs behind leaf, with the seat's account made.
func startTLSHomeserver(t *testing.T, leaf pki.Leaf, clk *fakeClock) *homeserver {
	t.Helper()
	pair, err := tls.X509KeyPair(leaf.CertPEM(), leaf.KeyPEM())
	if err != nil {
		t.Fatalf("key pair: %v", err)
	}
	hs := fakehs.New(fakehs.Options{ServerName: paths.ServerName(mustTeam(t)), Now: func() time.Time { return testStart }})
	if err := hs.StartTLS(&tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := hs.Stop(); err != nil {
			t.Error(err)
		}
	})
	return &homeserver{t: t, hs: hs, clock: clk, userID: hs.AddUser(testHandle, testPassword, false)}
}

// X15, X16: an issued bundle for an https team connects to the team's homeserver with the CA it
// carries, and fails with the CA of another team.
func TestIssuedBundleOfAnHTTPSTeamConnectsWithItsCAAndNoOther(t *testing.T) {
	ca, other := mustCA(t, "team-a"), mustCA(t, "team-b")
	clk := newClock()
	h := startTLSHomeserver(t, mustLeaf(t, ca, "127.0.0.1"), clk)
	if !strings.HasPrefix(h.hs.URL(), "https://127.0.0.1:") {
		t.Fatalf("the fake homeserver is at %s", h.hs.URL())
	}
	issue := func(pinned pki.CA) Issued {
		b := bundleAt(t, h.hs.URL())
		b.TLSCA = string(pinned.CertPEM())
		data, err := Issued{Seat: b, Kind: KindPassword, Secret: mustSecret(t, testPassword)}.Encode()
		if err != nil {
			t.Fatal(err)
		}
		is, err := DecodeIssued(data)
		if err != nil {
			t.Fatal(err)
		}
		return is
	}
	right := issue(ca)
	token, err := Password{Clock: clk}.Mint(t.Context(), right.Seat, right.Secret)
	if err != nil || token.Reveal() == "" {
		t.Fatalf("the right CA: %v", err)
	}
	wrong := issue(other)
	_, err = Password{Clock: clk}.Mint(t.Context(), wrong.Seat, wrong.Secret)
	if err == nil {
		t.Fatal("another team's CA connected")
	}
	if !strings.Contains(err.Error(), "unknown authority") || errors.Is(err, ErrReissue) {
		t.Errorf("another CA: %v, want a TLS verification failure that is not a reissue", err)
	}
}
