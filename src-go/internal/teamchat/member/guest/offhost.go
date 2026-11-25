package guest

import (
	"bytes"
	"context"
	"errors"
	"strconv"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/stdinread"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/placeissued"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

const (
	msgOffHostCheckout exits.Template = "teamchat seat place-lxc takes --checkout DIR, an absolute, clean path"
	msgOffHostBundle   exits.Template = "the document on stdin is not an issued LXC seat: issue it with member add --kind lxc --credential token"
)

// discarded takes a placing op's changes, which seat place-lxc does not report.
type discarded struct{}

func (discarded) Changed(What) error { return nil }

// PlaceOffHost is `seat place-lxc` (issue 425 A1): an LXC container on a host other than the
// homeserver's places its issued seat itself, piped from `member add --kind lxc --credential
// token` (or its reissue or return, whole answer or bare bundle), where the lxc apply places a
// seat in a guest on the homeserver's host. The seat is keyed as the lxc apply keys it (team,
// checkout, the handle's seat number): the token bundle is placed as GU3 places it, then a
// desired membership of that one seat for the key's session, generation the wall clock, so a
// running session takes it. The answer is the key, for `teamchat run --kind lxc --key`. A
// refusal places nothing.
func PlaceOffHost(ctx context.Context, h *Identity, checkout string, clock Clock, in Input) (paths.SeatKey, error) {
	if !paths.ValidGuestPath(checkout) {
		return paths.SeatKey{}, exits.New(exits.CodeUsage, nil, msgOffHostCheckout)
	}
	is, err := placeissued.ReadWatched(in.Stdin, stdinread.Options{Err: in.Err, Clock: in.Clock})
	var refused *placeissued.FailureError
	if errors.As(err, &refused) || errors.Is(err, seat.ErrInvalid) {
		return paths.SeatKey{}, exits.New(exits.CodeBundle, nil, msgOffHostBundle)
	}
	if err != nil {
		return paths.SeatKey{}, err
	}
	handle, ok := proto.ParseHandle(is.Seat.Handle().String())
	n, numErr := strconv.Atoi(handle.Seat)
	if !ok || handle.Type != status.KindLXC || is.Kind != seat.KindToken || numErr != nil ||
		!paths.ValidLXCSeat(n) || strconv.Itoa(n) != handle.Seat {
		return paths.SeatKey{}, exits.New(exits.CodeBundle, &is.Seat.Team, msgOffHostBundle)
	}
	k := paths.LXCSeatKey(is.Seat.Team, checkout, n)
	raw, err := is.Encode()
	if err != nil {
		return paths.SeatKey{}, exits.New(exits.CodeBundle, &is.Seat.Team, msgOffHostBundle)
	}
	if err := PlaceBundle(ctx, h, k, clock, Input{Stdin: bytes.NewReader(raw)}, discarded{}); err != nil {
		return paths.SeatKey{}, err
	}
	now := clock.Now().UnixMilli()
	desired, err := state.Desired{
		V: state.Version, Session: paths.LXCSession(k), Generation: now, WrittenMS: now,
		Seats:    []state.DesiredSeat{{Team: is.Seat.Team, Seat: handle.Seat}},
		Farewell: []state.Farewell{}, Inbound: state.InboundAccept,
	}.Encode()
	if err != nil {
		return paths.SeatKey{}, err
	}
	if err := Membership(h, k, Input{Stdin: bytes.NewReader(desired)}, discarded{}); err != nil {
		return paths.SeatKey{}, err
	}
	return k, nil
}
