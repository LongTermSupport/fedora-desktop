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
)

// SeatBundles is the production Bundles (BU2): seat.Issued from the applied team, issued at
// Clock's time by the build whose digest is Digest.
type SeatBundles struct {
	Clock  func() time.Time
	Digest paths.Digest
	CA     TeamCA
}

// TeamCA gives a team's CA certificate in PEM: a function of the host's files, which the seat
// side does not read (Plan 00246 X13, X15).
type TeamCA func(team paths.TeamID) (string, error)

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
	caPEM, err := teamCAPEM(s.CA, team)
	if err != nil {
		return proto.Value{}, err
	}
	issued := seat.Issued{
		Seat: seat.Bundle{
			V: seat.BundleVersion, Team: team.ID, ServerName: team.ServerName, UserID: proto.UserID(handle, team.ServerName),
			AdminID: proto.AdminID(team.ServerName), MainRoom: mainRoom, BaseURL: team.URL, TLSCA: caPEM,
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
