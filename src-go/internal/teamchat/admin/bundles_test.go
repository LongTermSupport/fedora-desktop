package admin

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/pki"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
)

// teamCA stands in for the files the host keeps the team CA in (host.CAPEM in production): it answers one CA
// certificate, or err, and counts the teams it was asked for.
type teamCA struct {
	pem   string
	err   error
	asked []paths.TeamID
}

func (c *teamCA) get(team paths.TeamID) (string, error) {
	c.asked = append(c.asked, team)
	return c.pem, c.err
}

// newTeamCA is a provider of a real CA certificate, made on the test clock.
func newTeamCA(t *testing.T) *teamCA {
	t.Helper()
	ca, err := (pki.Maker{Clock: clock.NewFake(0, t0ms)}).NewCA("team-alpha")
	if err != nil {
		t.Fatal(err)
	}
	return &teamCA{pem: string(ca.CertPEM())}
}

func seatBundles(t *testing.T) SeatBundles {
	t.Helper()
	return seatBundlesWith(t, newTeamCA(t).get)
}

func seatBundlesWith(t *testing.T, ca TeamCA) SeatBundles {
	t.Helper()
	d, err := paths.ParseDigest(digest)
	if err != nil {
		t.Fatal(err)
	}
	return SeatBundles{Clock: func() time.Time { return time.UnixMilli(t0ms) }, Digest: d, CA: ca}
}

// loopbackURL is the one plain http base URL a bundle may carry (Plan 00246 X12, X15): plain
// http is for a loopback literal only, and every other URL is https with the team's CA.
const (
	loopbackURL = "http://127.0.0.1:8448"
	httpsURL    = "https://chat.example.org:8448"
)

func alphaTeam(t *testing.T, url string) declare.Team {
	t.Helper()
	team := alphaInput(t).Team
	team.URL = loopbackURL
	if url != "" {
		team.URL = url
	}
	return team
}

// TestSeatBundlesIsTheIssuedBundle is BU2: seat.json's fields from the applied team, the handle,
// the main room, the seat device, the clock and the build's digest, and the credential, as
// seat.DecodeIssued reads it.
func TestSeatBundlesIsTheIssuedBundle(t *testing.T) {
	ca := newTeamCA(t)
	cases := map[string]struct {
		url    string
		wantCA string
		cred   apply.Credential
	}{
		"loopback literal": {"", "", apply.Credential{Kind: apply.CredPassword, Secret: "pw-1"}},
		"loopback v6":      {"http://[::1]:8448", "", apply.Credential{Kind: apply.CredToken, Secret: "syt_tok~1"}},
		"https":            {httpsURL, ca.pem, apply.Credential{Kind: apply.CredPassword, Secret: "pw-2"}},
	}
	for name, c := range cases {
		team := alphaTeam(t, c.url)
		v, err := seatBundlesWith(t, ca.get).Bundle(t.Context(), team, sampleHandle, sampleRoom, c.cred, ReachURL)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		issued, err := seat.DecodeIssued(v.Compact())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b := issued.Seat
		want := seat.Bundle{
			V: seat.BundleVersion, Team: team.ID, ServerName: server, UserID: proto.UserID(sampleHandle, server), AdminID: proto.AdminID(server),
			MainRoom: sampleRoom, BaseURL: team.URL, TLSCA: c.wantCA, DeviceID: paths.SeatDevice, IssuedMS: t0ms,
			Issuer: seatBundles(t).Digest,
		}
		if !reflect.DeepEqual(b, want) {
			t.Errorf("%s: bundle %+v\nwant %+v", name, b, want)
		}
		if string(issued.Kind) != c.cred.Kind || issued.Secret.Reveal() != c.cred.Secret {
			t.Errorf("%s: credential %s", name, issued.Kind)
		}
	}
}

// lxcHandle is a seat of kind lxc: its handle's type, which is the member's kind (MO3).
const lxcHandle = "thing.2+box-1.lxc"

// lxcURLs stands in for the applied team's LXC seats' url (host.AppliedLXCURL in production): it
// answers url, or err, and records the teams it was asked for.
type lxcURLs struct {
	url   string
	err   error
	asked []declare.Team
}

func (l *lxcURLs) get(team declare.Team) (string, error) {
	l.asked = append(l.asked, team)
	return l.url, l.err
}

// TestSeatBundlesGivesAnLXCSeatTheBridgeURL is X5: a seat the host's lxc apply places in a guest
// on this host (ReachBridge) is given the LXC seats' url, the team's listen address on the
// bridge, pinned to the same team CA. Every seat a person issues (ReachURL) goes to another
// machine through a pipe, an LXC container on another host included (issue 425 A1), and is
// given the team's url; the LXC url is never asked for it.
func TestSeatBundlesGivesAnLXCSeatTheBridgeURL(t *testing.T) {
	ca := newTeamCA(t)
	team := alphaTeam(t, httpsURL)
	cred := apply.Credential{Kind: apply.CredToken, Secret: "syt_tok~1"}
	const bridge = "https://198.51.100.1:8448"
	cases := []struct {
		handle string
		reach  Reach
		want   string
	}{
		{lxcHandle, ReachBridge, bridge},
		{lxcHandle, ReachURL, httpsURL},
		{sampleHandle, ReachURL, httpsURL},
		{"thing.1+desk.host", ReachURL, httpsURL},
		{"thing.1+lab.vm", ReachURL, httpsURL},
	}
	for _, c := range cases {
		urls := &lxcURLs{url: bridge}
		sb := seatBundlesWith(t, ca.get)
		sb.LXCURL = urls.get
		v, err := sb.Bundle(t.Context(), team, c.handle, sampleRoom, cred, c.reach)
		if err != nil {
			t.Fatalf("%s %v: %v", c.handle, c.reach, err)
		}
		issued, err := seat.DecodeIssued(v.Compact())
		if err != nil {
			t.Fatalf("%s: %v", c.handle, err)
		}
		if issued.Seat.BaseURL != c.want || issued.Seat.TLSCA != ca.pem {
			t.Errorf("%s %v: base_url %q, want %q; tls_ca the team's: %t", c.handle, c.reach, issued.Seat.BaseURL, c.want, issued.Seat.TLSCA == ca.pem)
		}
		wantAsked := map[bool]int{true: 1, false: 0}[c.reach == ReachBridge]
		if len(urls.asked) != wantAsked || (wantAsked == 1 && urls.asked[0].ID != team.ID) {
			t.Errorf("%s %v: the LXC url was asked for %v, want %d asks", c.handle, c.reach, urls.asked, wantAsked)
		}
	}
}

// TestSeatBundlesPlacesOnlyAnLXCSeatOnTheBridge: the bridge is for an LXC guest's seat alone;
// any other handle asked for it is issued no bundle.
func TestSeatBundlesPlacesOnlyAnLXCSeatOnTheBridge(t *testing.T) {
	sb := seatBundles(t)
	sb.LXCURL = (&lxcURLs{url: "https://198.51.100.1:8448"}).get
	cred := apply.Credential{Kind: apply.CredToken, Secret: "syt_tok~1"}
	if _, err := sb.Bundle(t.Context(), alphaTeam(t, httpsURL), sampleHandle, sampleRoom, cred, ReachBridge); err == nil {
		t.Fatal("a seat that is not of kind lxc was placed on the bridge")
	}
}

// TestSeatBundlesFailsWithoutTheLXCURL: an LXC seat whose url cannot be had (none recorded on
// the bridge, no provider) is issued no bundle rather than the team's url, which its guest might
// not reach.
func TestSeatBundlesFailsWithoutTheLXCURL(t *testing.T) {
	cred := apply.Credential{Kind: apply.CredToken, Secret: "s3cret-value"}
	errNone := errors.New("no listen address inside the LXC bridge's subnet")
	for name, provider := range map[string]func(declare.Team) (string, error){
		"none on the bridge": (&lxcURLs{err: errNone}).get,
		"no provider":        nil,
	} {
		sb := seatBundles(t)
		sb.LXCURL = provider
		_, err := sb.Bundle(t.Context(), alphaTeam(t, httpsURL), lxcHandle, sampleRoom, cred, ReachBridge)
		if err == nil || strings.Contains(err.Error(), cred.Secret) {
			t.Errorf("%s: %v", name, err)
		}
		if name == "none on the bridge" && !errors.Is(err, errNone) {
			t.Errorf("%s: %v does not carry the provider's error", name, err)
		}
	}
}

// TestSeatBundlesAsksForTheCAOnlyForHTTPS: the provider is asked for the team's CA exactly when
// the base URL is https; a loopback team makes no CA and asks for none (X12, X15).
func TestSeatBundlesAsksForTheCAOnlyForHTTPS(t *testing.T) {
	cred := apply.Credential{Kind: apply.CredPassword, Secret: "pw-1"}
	for url, wantAsked := range map[string]int{loopbackURL: 0, "http://[::1]:8448": 0, httpsURL: 1} {
		ca := newTeamCA(t)
		team := alphaTeam(t, url)
		if _, err := seatBundlesWith(t, ca.get).Bundle(t.Context(), team, sampleHandle, sampleRoom, cred, ReachURL); err != nil {
			t.Fatalf("%s: %v", url, err)
		}
		if len(ca.asked) != wantAsked || (wantAsked == 1 && ca.asked[0] != team.ID) {
			t.Errorf("%s: the CA was asked for %v, want %d ask for %s", url, ca.asked, wantAsked, team.ID)
		}
	}
}

// TestSeatBundlesFailsWhenTheTeamCAFails: an https team whose CA is not there, is unusable or has
// no provider issues nothing, and the error never quotes the secret.
func TestSeatBundlesFailsWhenTheTeamCAFails(t *testing.T) {
	const secret = "s3cret-value"
	cred := apply.Credential{Kind: apply.CredToken, Secret: secret}
	errNotMade := errors.New("the team CA is not made yet")
	two := newTeamCA(t).pem + newTeamCA(t).pem
	cases := map[string]SeatBundles{
		"not made":    seatBundlesWith(t, (&teamCA{err: errNotMade}).get),
		"empty":       seatBundlesWith(t, (&teamCA{}).get),
		"junk":        seatBundlesWith(t, (&teamCA{pem: "junk"}).get),
		"two blocks":  seatBundlesWith(t, (&teamCA{pem: two}).get),
		"no provider": seatBundlesWith(t, nil),
	}
	for name, sb := range cases {
		_, err := sb.Bundle(t.Context(), alphaTeam(t, httpsURL), sampleHandle, sampleRoom, cred, ReachURL)
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		if strings.Contains(err.Error(), secret) {
			t.Errorf("%s: the error quotes the secret: %v", name, err)
		}
		if name == "not made" && !errors.Is(err, errNotMade) {
			t.Errorf("%s: %v does not carry the provider's error", name, err)
		}
	}
}

// TestSeatBundlesRefusesWhatSeatWouldRefuse: Encode refuses a bundle DecodeBundle would, and no
// error quotes the secret.
func TestSeatBundlesRefusesWhatSeatWouldRefuse(t *testing.T) {
	const secret = "s3cret-value"
	good := apply.Credential{Kind: apply.CredPassword, Secret: secret}
	sb := seatBundles(t)
	cases := map[string]struct {
		sb     SeatBundles
		team   declare.Team
		handle string
		room   string
		cred   apply.Credential
	}{
		"room":             {sb, alphaTeam(t, ""), sampleHandle, "#main", good},
		"handle":           {sb, alphaTeam(t, ""), "admin", sampleRoom, good},
		"plain http name":  {sb, alphaTeam(t, "http://chat.example.org:8448"), sampleHandle, sampleRoom, good},
		"plain http ip":    {sb, alphaTeam(t, "http://192.0.2.10:8448"), sampleHandle, sampleRoom, good},
		"credential kind":  {sb, alphaTeam(t, ""), sampleHandle, sampleRoom, apply.Credential{Kind: "key", Secret: secret}},
		"secret":           {sb, alphaTeam(t, ""), sampleHandle, sampleRoom, apply.Credential{Kind: apply.CredToken, Secret: secret + " x"}},
		"no digest":        {SeatBundles{Clock: sb.Clock}, alphaTeam(t, ""), sampleHandle, sampleRoom, good},
		"before the epoch": {SeatBundles{Clock: func() time.Time { return time.UnixMilli(-1) }, Digest: sb.Digest}, alphaTeam(t, ""), sampleHandle, sampleRoom, good},
		"url":              {sb, alphaTeam(t, "http://[::1"), sampleHandle, sampleRoom, good},
	}
	for name, c := range cases {
		_, err := c.sb.Bundle(t.Context(), c.team, c.handle, c.room, c.cred, ReachURL)
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		if strings.Contains(err.Error(), secret) {
			t.Errorf("%s: the error quotes the secret: %v", name, err)
		}
	}
}
