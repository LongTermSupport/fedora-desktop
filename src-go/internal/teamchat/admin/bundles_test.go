package admin

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
)

func seatBundles(t *testing.T) SeatBundles {
	t.Helper()
	d, err := paths.ParseDigest(digest)
	if err != nil {
		t.Fatal(err)
	}
	return SeatBundles{Clock: func() time.Time { return time.UnixMilli(t0ms) }, Digest: d}
}

func alphaTeam(t *testing.T, url string) declare.Team {
	t.Helper()
	team := alphaInput(t).Team
	if url != "" {
		team.URL = url
	}
	return team
}

// TestSeatBundlesIsTheIssuedBundle is BU2: seat.json's fields from the applied team, the handle,
// the main room, the seat device, the clock and the build's digest, and the credential, as
// seat.DecodeIssued reads it.
func TestSeatBundlesIsTheIssuedBundle(t *testing.T) {
	cases := map[string]struct {
		url   string
		plain []string
		cred  apply.Credential
	}{
		"ip literal": {"", []string{"192.0.2.10"}, apply.Credential{Kind: apply.CredPassword, Secret: "pw-1"}},
		"name":       {"https://chat.example.org:8448", []string{}, apply.Credential{Kind: apply.CredToken, Secret: "syt_tok~1"}},
	}
	for name, c := range cases {
		team := alphaTeam(t, c.url)
		v, err := seatBundles(t).Bundle(t.Context(), team, sampleHandle, sampleRoom, c.cred)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		issued, err := seat.DecodeIssued(v.Compact())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b := issued.Seat
		want := seat.Bundle{
			V: 1, Team: team.ID, ServerName: server, UserID: proto.UserID(sampleHandle, server), AdminID: proto.AdminID(server),
			MainRoom: sampleRoom, BaseURL: team.URL, PlainHTTPHosts: c.plain, DeviceID: paths.SeatDevice, IssuedMS: t0ms,
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
		"credential kind":  {sb, alphaTeam(t, ""), sampleHandle, sampleRoom, apply.Credential{Kind: "key", Secret: secret}},
		"secret":           {sb, alphaTeam(t, ""), sampleHandle, sampleRoom, apply.Credential{Kind: apply.CredToken, Secret: secret + " x"}},
		"no digest":        {SeatBundles{Clock: sb.Clock}, alphaTeam(t, ""), sampleHandle, sampleRoom, good},
		"before the epoch": {SeatBundles{Clock: func() time.Time { return time.UnixMilli(-1) }, Digest: sb.Digest}, alphaTeam(t, ""), sampleHandle, sampleRoom, good},
		"url":              {sb, alphaTeam(t, "http://[::1"), sampleHandle, sampleRoom, good},
	}
	for name, c := range cases {
		_, err := c.sb.Bundle(t.Context(), c.team, c.handle, c.room, c.cred)
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		if strings.Contains(err.Error(), secret) {
			t.Errorf("%s: the error quotes the secret: %v", name, err)
		}
	}
}
