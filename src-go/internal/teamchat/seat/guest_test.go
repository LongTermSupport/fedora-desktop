package seat

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

const guestToken = "guest-token-1"

func guestLayout(t *testing.T) paths.Layout {
	t.Helper()
	l, err := paths.HomeLayout(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func guestIssued(t *testing.T, token string) Issued {
	t.Helper()
	return Issued{Seat: testBundle(t), Kind: KindToken, Secret: mustSecret(t, token)}
}

// claimsIn are the `<team>.new-*` names left in the guest's state root.
func claimsIn(t *testing.T, l paths.Layout) []string {
	t.Helper()
	entries, err := os.ReadDir(l.State())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if strings.Contains(e.Name(), ".new-") {
			out = append(out, e.Name())
		}
	}
	return out
}

// SP3: a guest bundle is placed as <state>/<team>/, 0700, with token and seat.json 0600.
func TestPlaceGuestANewSeat(t *testing.T) {
	l := guestLayout(t)
	if err := PlaceGuest(l, guestIssued(t, guestToken)); err != nil {
		t.Fatal(err)
	}
	seat := l.Seat(mustTeam(t))
	if mode(t, l.State()).Perm() != 0o700 || mode(t, seat.Dir()).Perm() != 0o700 {
		t.Errorf("the state root or seat directory is not 0700")
	}
	if readFile(t, seat.Token()) != guestToken || mode(t, seat.Token()).Perm() != 0o600 {
		t.Errorf("token %v", mode(t, seat.Token()))
	}
	got, err := DecodeBundle([]byte(readFile(t, seat.Bundle())))
	if err != nil || got.UserID != testBundle(t).UserID || mode(t, seat.Bundle()).Perm() != 0o600 {
		t.Errorf("seat.json %+v, %v", got, err)
	}
	if left := claimsIn(t, l); len(left) != 0 {
		t.Errorf("claims left: %v", left)
	}
}

// SP3: a placement over an old seat swaps the new one in and removes the old directory whole;
// a link in place of the seat is removed, never followed; a leftover claim is removed.
func TestPlaceGuestOverAnOldSeat(t *testing.T) {
	l := guestLayout(t)
	if err := PlaceGuest(l, guestIssued(t, guestToken)); err != nil {
		t.Fatal(err)
	}
	seat := l.Seat(mustTeam(t))
	writeFile(t, seat.Lock(), "")
	writeFile(t, l.State()+"/ops.new-00112233aabbccdd/token", "stale")
	if err := PlaceGuest(l, guestIssued(t, "guest-token-2")); err != nil {
		t.Fatal(err)
	}
	if readFile(t, seat.Token()) != "guest-token-2" || exists(t, seat.Lock()) {
		t.Errorf("the old seat was not replaced whole")
	}
	if left := claimsIn(t, l); len(left) != 0 {
		t.Errorf("claims left: %v", left)
	}
	outside := t.TempDir()
	writeFile(t, outside+"/token", "outside")
	if err := os.RemoveAll(seat.Dir()); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, seat.Dir()); err != nil {
		t.Fatal(err)
	}
	if err := PlaceGuest(l, guestIssued(t, "guest-token-3")); err != nil {
		t.Fatal(err)
	}
	if mode(t, seat.Dir())&os.ModeSymlink != 0 || readFile(t, outside+"/token") != "outside" {
		t.Errorf("the link was followed, or kept")
	}
}

// SP3: only a guest (token) bundle is placed, and a bad one writes nothing; the state root
// must be the agent user's, 0700.
func TestPlaceGuestRefusals(t *testing.T) {
	l := guestLayout(t)
	password := Issued{Seat: testBundle(t), Kind: KindPassword, Secret: mustSecret(t, guestToken)}
	if err := PlaceGuest(l, password); !errors.Is(err, ErrInvalid) {
		t.Errorf("a password bundle: %v", err)
	}
	broken := guestIssued(t, guestToken)
	broken.Seat.DeviceID = "OTHER"
	if err := PlaceGuest(l, broken); !errors.Is(err, ErrInvalid) {
		t.Errorf("a bundle that fails its checks: %v", err)
	}
	if exists(t, l.State()) {
		t.Errorf("a refused bundle wrote the state root")
	}
	if err := os.MkdirAll(l.State(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(l.State(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := PlaceGuest(l, guestIssued(t, guestToken)); !errors.Is(err, ErrConfig) {
		t.Errorf("a 0755 state root: %v", err)
	}
}

// SP4: the control user's copy of a guest token is written 0600 in a 0700 guest/ directory
// under the team's control state, and replaced afresh; links are refused.
func TestKeepGuestToken(t *testing.T) {
	base := t.TempDir()
	team, seat := mustTeam(t), mustSeatName(t, testHandle)
	if err := KeepGuestToken(base, team, seat, mustSecret(t, guestToken)); !errors.Is(err, ErrConfig) {
		t.Errorf("no control state for the team: %v", err)
	}
	if err := os.MkdirAll(base+paths.CtlState(team), 0o700); err != nil {
		t.Fatal(err)
	}
	copyPath := base + paths.GuestTokenCopy(team, seat)
	for _, token := range []string{guestToken, "guest-token-2"} {
		if err := KeepGuestToken(base, team, seat, mustSecret(t, token)); err != nil {
			t.Fatal(err)
		}
		if readFile(t, copyPath) != token || mode(t, copyPath).Perm() != 0o600 {
			t.Errorf("the kept copy %v", mode(t, copyPath))
		}
	}
	guest := base + paths.CtlState(team) + "/guest"
	if mode(t, guest).Perm() != 0o700 || exists(t, copyPath+".new") {
		t.Errorf("guest/ %v", mode(t, guest))
	}
}

// SP4: a guest/ that is a link, or not 0700, is refused and nothing is written through it.
func TestKeepGuestTokenRefusals(t *testing.T) {
	base := t.TempDir()
	team, seat := mustTeam(t), mustSeatName(t, testHandle)
	guest := base + paths.CtlState(team) + "/guest"
	if err := os.MkdirAll(base+paths.CtlState(team), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, guest); err != nil {
		t.Fatal(err)
	}
	if err := KeepGuestToken(base, team, seat, mustSecret(t, guestToken)); err == nil {
		t.Errorf("a linked guest/ was written through")
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Errorf("written through the link: %v, %v", entries, err)
	}
	if err := os.Remove(guest); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(guest, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(guest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := KeepGuestToken(base, team, seat, mustSecret(t, guestToken)); !errors.Is(err, ErrConfig) {
		t.Errorf("a 0755 guest/: %v", err)
	}
}
