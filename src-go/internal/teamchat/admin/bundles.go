package admin

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// SeatBundles is the production Bundles (BU2): seat.Issued from the applied team, issued at
// Clock's time by the build whose digest is Digest.
type SeatBundles struct {
	Clock  func() time.Time
	Digest paths.Digest
	CA     TeamCA
	// LXCURL is the base url a seat of kind lxc is given in place of the team's url (X5).
	LXCURL LXCURL
}

// TeamCA gives a team's CA certificate in PEM: a function of the host's files, which the seat
// side does not read (Plan 00246 X13, X15).
type TeamCA func(team paths.TeamID) (string, error)

// LXCURL gives the base url an applied team's LXC seats reach it by: its listen address on the
// LXC bridge, a function of the host's files (host.AppliedLXCURL, X5).
type LXCURL func(team declare.Team) (string, error)

// seatBaseURL is the base url the seat handle is given: the LXC seats' url for a handle of type
// lxc, which is the member's kind (MO3), and the team's url for every other kind.
func seatBaseURL(lxcURL LXCURL, team declare.Team, handle string) (string, error) {
	h, ok := proto.ParseHandle(handle)
	if !ok || h.Type != status.KindLXC {
		return team.URL, nil
	}
	if lxcURL == nil {
		return "", errors.New("the LXC seats' url has no provider")
	}
	base, err := lxcURL(team)
	if err != nil {
		return "", fmt.Errorf("the url of team %s's LXC seats: %w", team.ID, err)
	}
	return base, nil
}

// Bundle implements Bundles. No error quotes the credential's secret.
func (s SeatBundles) Bundle(_ context.Context, team declare.Team, handle, mainRoom string, c apply.Credential) (proto.Value, error) {
	kind, err := seatKind(c.Kind)
	if err != nil {
		return proto.Value{}, err
	}
	secret, err := seat.ParseSecret(c.Secret)
	if err != nil {
		return proto.Value{}, errors.New("the credential is not one seat.json can carry")
	}
	base, err := seatBaseURL(s.LXCURL, team, handle)
	if err != nil {
		return proto.Value{}, err
	}
	caPEM, err := teamCAPEM(s.CA, team)
	if err != nil {
		return proto.Value{}, err
	}
	issued := seat.Issued{
		Seat: seat.Bundle{
			V: seat.BundleVersion, Team: team.ID, ServerName: team.ServerName, UserID: proto.UserID(handle, team.ServerName),
			AdminID: proto.AdminID(team.ServerName), MainRoom: mainRoom, BaseURL: base, TLSCA: caPEM,
			DeviceID: paths.SeatDevice, IssuedMS: s.Clock().UnixMilli(), Issuer: s.Digest,
		},
		Kind: kind, Secret: secret,
	}
	raw, err := issued.Encode()
	if err != nil {
		return proto.Value{}, err
	}
	return proto.Parse(raw)
}

// teamCAPEM is the CA certificate a bundle or a reach fact for team carries: the provider's for
// an https URL, and none for plain http, which the member client refuses except at a loopback
// literal, where there is nothing to pin (X12, X15).
func teamCAPEM(ca TeamCA, team declare.Team) (string, error) {
	u, err := url.Parse(team.URL)
	if err != nil {
		return "", fmt.Errorf("the team's base URL: %w", err)
	}
	if u.Scheme != "https" {
		return "", nil
	}
	if ca == nil {
		return "", errors.New("the team CA has no provider")
	}
	pem, err := ca(team.ID)
	if err != nil {
		return "", fmt.Errorf("the CA of team %s: %w", team.ID, err)
	}
	return pem, nil
}

func seatKind(kind string) (seat.Kind, error) {
	switch kind {
	case apply.CredPassword:
		return seat.KindPassword, nil
	case apply.CredToken:
		return seat.KindToken, nil
	}
	return "", fmt.Errorf("the credential kind %q is not password or token", kind)
}
