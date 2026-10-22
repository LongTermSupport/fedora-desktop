package seat

import (
	"errors"
	"os"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

const guestToken = "guest-token-1"

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
