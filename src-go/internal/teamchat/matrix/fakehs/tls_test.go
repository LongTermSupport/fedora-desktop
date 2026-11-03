package fakehs

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/pki"
)

// tlsWall is the certificates' clock: 2026-10-21 12:00:00 UTC, a whole second.
const tlsWall = int64(1_792_584_000_000)

// tlsEnds is the two ends of a TLS connection a test makes: the configuration StartTLS takes
// (a leaf for 127.0.0.1 from a CA of its own) and a client that trusts only that CA.
type tlsEnds struct {
	server *tls.Config
	client *http.Client
}

func newTLSEnds(t *testing.T) tlsEnds {
	t.Helper()
	maker := pki.Maker{Clock: clock.NewFake(0, tlsWall)}
	ca, err := maker.NewCA("fakehs-test")
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := maker.NewLeaf(ca, pki.Names{IPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}})
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(leaf.CertPEM(), leaf.KeyPEM())
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pki.Pool(ca.CertPEM())
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{
		Proxy:             nil,
		DisableKeepAlives: true,
		TLSClientConfig: &tls.Config{
			RootCAs:    pool,
			MinVersion: tls.VersionTLS13,
			Time:       func() time.Time { return time.UnixMilli(tlsWall) },
		},
	}}
	return tlsEnds{
		server: &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12},
		client: client,
	}
}

func versionsStatus(t *testing.T, client *http.Client, base string) int {
	t.Helper()
	resp, err := client.Get(base + "/_matrix/client/versions")
	if err != nil {
		t.Fatalf("GET %s: %v", base, err)
	}
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode
}

func TestStartTLSServesHTTPSAndKeepsItAcrossARestart(t *testing.T) {
	tm := newTeam(t)
	ends := newTLSEnds(t)
	if err := tm.s.StartTLS(ends.server); err != nil {
		t.Fatalf("StartTLS: %v", err)
	}
	t.Cleanup(func() {
		if err := tm.s.Close(); err != nil {
			t.Error(err)
		}
	})
	before := tm.s.URL()
	if !strings.HasPrefix(before, "https://127.0.0.1:") {
		t.Fatalf("URL = %q, want https://127.0.0.1:<port>", before)
	}
	if got := versionsStatus(t, ends.client, before); got != http.StatusOK {
		t.Fatalf("versions over TLS = %d", got)
	}
	if err := tm.s.Restart(); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if after := tm.s.URL(); after != before {
		t.Fatalf("URL after Restart = %q, want %q", after, before)
	}
	if got := versionsStatus(t, ends.client, before); got != http.StatusOK {
		t.Fatalf("versions over TLS after Restart = %d", got)
	}
}

func TestStartTLSRefusesNoConfigurationAndASecondStart(t *testing.T) {
	tm := newTeam(t)
	if err := tm.s.StartTLS(nil); err == nil {
		t.Fatal("StartTLS(nil) accepted")
	}
	ends := newTLSEnds(t)
	if err := tm.s.StartTLS(ends.server); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := tm.s.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := tm.s.StartTLS(ends.server); err == nil {
		t.Fatal("a second StartTLS accepted")
	}
	if err := tm.s.Start(); err == nil {
		t.Fatal("Start after StartTLS accepted")
	}
}

func TestPlainStartIsStillHTTP(t *testing.T) {
	tm := newTeam(t)
	if err := tm.s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := tm.s.Close(); err != nil {
			t.Error(err)
		}
	})
	if got := tm.s.URL(); !strings.HasPrefix(got, "http://127.0.0.1:") {
		t.Fatalf("URL = %q, want http://127.0.0.1:<port>", got)
	}
}
