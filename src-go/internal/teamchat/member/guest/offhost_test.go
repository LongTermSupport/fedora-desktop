package guest

import (
	"bytes"
	"errors"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
)

// offHostCheckout is the checkout seatKey keys on, where an off-host seat is placed.
const offHostCheckout = "/srv/checkouts/thing"

// lxcBundle is team ops's seat with a handle of type lxc whose seat part is seatPart.
func (w *world) lxcBundle(seatPart string) seat.Bundle {
	w.t.Helper()
	b := w.bundle(1)
	b.UserID = "@thing." + seatPart + "+site.lxc:" + paths.ServerName(w.team)
	return b
}

func (w *world) placeOffHost(checkout string, data []byte) (paths.SeatKey, error) {
	return PlaceOffHost(w.t.Context(), w.home, checkout, w.clock, Input{Stdin: bytes.NewReader(data)})
}

// Issue 425 A1: an LXC container on another host places its issued token seat itself, as the
// lxc apply would: the bundle as the keyed seat (team, checkout, the handle's seat number), and a
// desired membership of that one seat for the key's session. The key is the answer, for the
// launcher's --key.
func TestPlaceOffHostPlacesTheKeyedSeat(t *testing.T) {
	w := newWorld(t)
	want := seatKey(t, 2)
	k, err := w.placeOffHost(offHostCheckout, w.issued(w.lxcBundle("2"), seat.KindToken, "syt_tok~1"))
	check(t, err)
	if k != want {
		t.Fatalf("key %s, want %s", k, want)
	}
	l, err := paths.LXCLayout(w.home.Root.Path(), paths.GuestRuntimeDir(k), k)
	check(t, err)
	if got := string(w.read(l.Seat(w.team).Token())); got != "syt_tok~1" {
		t.Fatalf("token %q", got)
	}
	d, err := state.DecodeDesired(w.read(l.DesiredFile()))
	check(t, err)
	if d.Session != paths.LXCSession(k) || len(d.Seats) != 1 || d.Seats[0].Team != w.team || d.Seats[0].Seat != "2" ||
		d.Inbound != state.InboundAccept || d.Generation < 1 {
		t.Fatalf("desired %+v", d)
	}
	// Placed again (a reissue): the same key, and a later generation, so a running session takes it.
	again, err := w.placeOffHost(offHostCheckout, w.issued(w.lxcBundle("2"), seat.KindToken, "syt_tok~2"))
	check(t, err)
	if again != k || string(w.read(l.Seat(w.team).Token())) != "syt_tok~2" {
		t.Fatalf("again %s", again)
	}
}

// What an off-host LXC seat cannot be is refused before anything is placed: a pool seat's
// password (its place is `seat place-issued`), a handle not of type lxc, a seat part that is not
// a seat number, a checkout the key cannot take, and a document that is not an issued seat.
func TestPlaceOffHostRefuses(t *testing.T) {
	cases := map[string]struct {
		checkout string
		data     func(w *world) []byte
		code     string
	}{
		"a password":     {offHostCheckout, func(w *world) []byte { return w.issued(w.lxcBundle("2"), seat.KindPassword, "pw-1") }, exits.CodeBundle},
		"a podman seat":  {offHostCheckout, func(w *world) []byte { return w.issued(w.bundle(2), seat.KindToken, "syt_tok~1") }, exits.CodeBundle},
		"a named seat":   {offHostCheckout, func(w *world) []byte { return w.issued(w.lxcBundle("probe"), seat.KindToken, "syt_tok~1") }, exits.CodeBundle},
		"seat 10000":     {offHostCheckout, func(w *world) []byte { return w.issued(w.lxcBundle("10000"), seat.KindToken, "syt_tok~1") }, exits.CodeBundle},
		"a relative one": {"thing", func(w *world) []byte { return w.issued(w.lxcBundle("2"), seat.KindToken, "syt_tok~1") }, exits.CodeUsage},
		"not a bundle":   {offHostCheckout, func(*world) []byte { return []byte(`{"v":1}`) }, exits.CodeBundle},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			_, err := w.placeOffHost(c.checkout, c.data(w))
			e, ok := errors.AsType[*exits.Error](err)
			if !ok || e.Code != c.code {
				t.Fatalf("error %v, want %s", err, c.code)
			}
			if w.exists(w.home.Root.Path() + "/.config") {
				t.Fatal("a refusal placed something")
			}
		})
	}
}
