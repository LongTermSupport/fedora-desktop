package admin

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
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
	plain, err := plainHTTPHosts(team.URL)
	if err != nil {
		return proto.Value{}, err
	}
	issued := seat.Issued{
		Seat: seat.Bundle{
			V: 1, Team: team.ID, ServerName: team.ServerName, UserID: proto.UserID(handle, team.ServerName),
			AdminID: proto.AdminID(team.ServerName), MainRoom: mainRoom, BaseURL: team.URL, PlainHTTPHosts: plain,
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

func seatKind(kind string) (seat.Kind, error) {
	switch kind {
	case apply.CredPassword:
		return seat.KindPassword, nil
	case apply.CredToken:
		return seat.KindToken, nil
	}
	return "", fmt.Errorf("the credential kind %q is not password or token", kind)
}

// plainHTTPHosts is the base URL's host when it is an IP literal, else none.
func plainHTTPHosts(base string) ([]string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("the team's base URL: %w", err)
	}
	host := u.Hostname()
	if addr, perr := netip.ParseAddr(host); perr == nil && addr.IsValid() {
		return []string{host}, nil
	}
	return []string{}, nil
}
