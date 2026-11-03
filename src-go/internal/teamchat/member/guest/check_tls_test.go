package guest

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/pki"
)

// Plan 00244 X12, X15, X16: guest check's stdin is v 2 and carries tls_ca; an https base_url is
// reached over TLS with exactly that CA.

// tlsServer is a fake homeserver behind a leaf of ca for 127.0.0.1; the certificates are made on
// the real clock, since the member client under test reads the system's.
func tlsServer(t *testing.T, ca pki.CA) *fakehs.Server {
	t.Helper()
	leaf, err := (pki.Maker{Clock: clock.System()}).NewLeaf(ca, pki.Names{IPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}})
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(leaf.CertPEM(), leaf.KeyPEM())
	if err != nil {
		t.Fatal(err)
	}
	srv := fakehs.New(fakehs.Options{ServerName: "bus.example.com", Now: fixedNow, Quirks: fakehs.TuwunelQuirks()})
	if err := srv.StartTLS(&tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := srv.Close(); err != nil {
			t.Error(err)
		}
	})
	return srv
}

func mustCA(t *testing.T, name string) pki.CA {
	t.Helper()
	ca, err := (pki.Maker{Clock: clock.System()}).NewCA(name)
	if err != nil {
		t.Fatal(err)
	}
	return ca
}

// stdinWith is guest check's stdin for base, with caText as its tls_ca.
func stdinWith(t *testing.T, base, caText string) string {
	t.Helper()
	out, err := json.Marshal(map[string]any{"v": 2, "team": "alpha", "base_url": base, "tls_ca": caText})
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// An https base_url is reached with the CA on stdin.
func TestCheckReachesAnHTTPSHomeserverWithItsCA(t *testing.T) {
	ca := mustCA(t, "team-a")
	srv := tlsServer(t, ca)
	w := newGuestWorld(t)
	if err := w.check(t, stdinWith(t, srv.URL(), string(ca.CertPEM()))); err != nil {
		t.Fatal(err)
	}
	if reqs := srv.Requests(); len(reqs) != 1 || !strings.HasSuffix(reqs[0].Path, "/_matrix/client/versions") {
		t.Errorf("requests %+v", reqs)
	}
}

// Another team's CA does not reach it: the homeserver did not answer, from the guest.
func TestCheckDoesNotReachAnHTTPSHomeserverWithAnotherCA(t *testing.T) {
	ca, other := mustCA(t, "team-a"), mustCA(t, "team-b")
	srv := tlsServer(t, ca)
	w := newGuestWorld(t)
	err := w.check(t, stdinWith(t, srv.URL(), string(other.CertPEM())))
	wantCode(t, err, 78, exits.CodeReach)
	if len(srv.Requests()) != 0 {
		t.Errorf("a request reached the homeserver past a CA that does not sign it: %+v", srv.Requests())
	}
}

// The URL rule is judged before any request: https needs the CA, loopback http takes none, and
// the CA is one certificate block of a CA.
func TestCheckRefusesWhatThePinnedURLRuleRefuses(t *testing.T) {
	ca := mustCA(t, "team-a")
	srv := tlsServer(t, ca)
	for name, stdin := range map[string]string{
		"https with no CA":     stdinWith(t, srv.URL(), ""),
		"loopback http, a CA":  stdinWith(t, "http://127.0.0.1:1", string(ca.CertPEM())),
		"https, junk as a CA":  stdinWith(t, srv.URL(), "junk"),
		"https, a key as a CA": stdinWith(t, srv.URL(), string(ca.KeyPEM())),
	} {
		t.Run(name, func(t *testing.T) {
			w := newGuestWorld(t)
			err := w.check(t, stdin)
			wantCode(t, err, 78, exits.CodeReach)
			if e, ok := errors.AsType[*exits.Error](err); !ok || !strings.Contains(e.Message, "tls_ca") {
				t.Errorf("err = %v, want the member URL rule's refusal naming tls_ca", err)
			}
			if len(srv.Requests()) != 0 {
				t.Errorf("a request was made: %+v", srv.Requests())
			}
		})
	}
}
