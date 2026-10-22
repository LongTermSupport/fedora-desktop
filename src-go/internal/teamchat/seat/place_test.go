package seat

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// issued is member add's answer for the world's seat.
func (w *world) issued(password string) Issued {
	return Issued{Seat: w.bundle(), Kind: KindPassword, Secret: mustSecret(w.t, password)}
}

// place claims and fills the world's seat as member add's answer would, under the lock.
func (w *world) place(password string) error {
	w.t.Helper()
	a := w.lock()
	defer w.unlock(a)
	c, err := w.store.Claim(a, w.seat)
	if err != nil {
		return err
	}
	return c.Fill(w.issued(password))
}

// SP1, SP2: a placement claims <seat>.new-<claim>, fills it, and swaps it in; the seat can be
// taken.
func TestPlaceANewSeat(t *testing.T) {
	w := newWorld(t)
	if err := w.place(testPassword); err != nil {
		t.Fatal(err)
	}
	if readFile(t, w.pool().Password()) != testPassword || mode(t, w.pool().Password()).Perm() != 0o600 {
		t.Errorf("the pool password")
	}
	b, err := DecodeBundle([]byte(readFile(t, w.pool().Bundle())))
	if err != nil || encode(t, b) != encode(t, w.bundle()) {
		t.Errorf("the pool seat.json %+v, %v", b, err)
	}
	if mode(t, w.pool().Dir()).Perm() != 0o700 || len(quarantined(t, w)) != 0 {
		t.Errorf("the pool directory")
	}
	if left, err := w.store.leftovers(w.seat); err != nil || len(left) != 0 {
		t.Errorf("leftovers %v, %v", left, err)
	}
	w.take("s1")
}

// SP2: a placement over an existing record exchanges them; the old one goes to quarantine
// without its credential.
func TestPlaceOverAnExistingSeat(t *testing.T) {
	w := newWorld(t)
	if err := w.place(testPassword); err != nil {
		t.Fatal(err)
	}
	if err := w.place("second-password"); err != nil {
		t.Fatal(err)
	}
	if readFile(t, w.pool().Password()) != "second-password" {
		t.Errorf("the new password is not in place")
	}
	moved := quarantined(t, w)
	if len(moved) != 1 || exists(t, moved[0]+"/password") || !exists(t, moved[0]+"/seat.json") {
		t.Errorf("quarantine %v", moved)
	}
}

// SP1: a leftover claim or a held seat refuses a claim; Abandon removes the empty claim.
func TestClaimRefusals(t *testing.T) {
	w := newWorld(t)
	a := w.lock()
	defer w.unlock(a)
	if _, err := w.store.Claim(nil, w.seat); !errors.Is(err, ErrNotLocked) {
		t.Errorf("a claim with no lock: %v", err)
	}
	c, err := w.store.Claim(a, w.seat)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.store.Claim(a, w.seat); !errors.Is(err, ErrLeftover) || !strings.Contains(err.Error(), c.dir) {
		t.Errorf("a second claim: %v", err)
	}
	if err := c.Abandon(); err != nil || exists(t, c.dir) {
		t.Errorf("abandon: %v", err)
	}
	if err := c.Abandon(); err == nil {
		t.Errorf("a second abandon")
	}
	writeFile(t, w.pool().HeldRecord(), "")
	if _, err := w.store.Claim(a, w.seat); !errors.Is(err, ErrHeld) {
		t.Errorf("a held seat: %v", err)
	}
}

// SP2: an answer that is not this seat's pool record is refused before anything is written.
func TestFillRefusesAnotherSeat(t *testing.T) {
	w := newWorld(t)
	other := w.issued(testPassword)
	other.Seat.UserID = "@repo.2+site.podman:" + other.Seat.ServerName
	otherTeam := w.issued(testPassword)
	otherTeam.Seat.Team = mustTeamID(t, "dev")
	otherTeam.Seat.ServerName = paths.ServerName(otherTeam.Seat.Team)
	otherTeam.Seat.UserID = "@" + testHandle + ":" + otherTeam.Seat.ServerName
	otherTeam.Seat.AdminID = "@admin:" + otherTeam.Seat.ServerName
	token := w.issued(testPassword)
	token.Kind = KindToken
	invalidSeat := w.issued(testPassword)
	invalidSeat.Seat.DeviceID = "OTHER"
	a := w.lock()
	defer w.unlock(a)
	for name, is := range map[string]Issued{"another handle": other, "another team": otherTeam, "a token": token, "invalid": invalidSeat} {
		c, err := w.store.Claim(a, w.seat)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Fill(is); err == nil {
			t.Errorf("%s: filled", name)
		}
		if names, err := w.store.root.entries(c.dir); err != nil || len(names) != 0 {
			t.Errorf("%s: written before the refusal: %v, %v", name, names, err)
		}
		if err := c.Abandon(); err != nil {
			t.Fatal(err)
		}
	}
}

func mustTeamID(t *testing.T, s string) paths.TeamID {
	t.Helper()
	team, err := paths.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return team
}

// SL1: a fill needs the claim's allocation lock still held; a filled claim cannot be abandoned.
func TestFillNeedsTheLock(t *testing.T) {
	w := newWorld(t)
	a := w.lock()
	c, err := w.store.Claim(a, w.seat)
	if err != nil {
		t.Fatal(err)
	}
	w.unlock(a)
	if err := c.Fill(w.issued(testPassword)); !errors.Is(err, ErrNotLocked) {
		t.Errorf("a fill after the unlock: %v", err)
	}
	if err := w.place(testPassword); !errors.Is(err, ErrLeftover) {
		t.Errorf("a placement over the leftover: %v", err)
	}
}

// SP1: the reissue's first step moves every leftover claim to quarantine, its credential
// unlinked.
func TestQuarantineLeftovers(t *testing.T) {
	w := newWorld(t)
	for _, claim := range []string{"0000000000000001", "0000000000000002"} {
		writeFile(t, w.pool().Dir()+".new-"+claim+"/password", "half")
	}
	if err := os.Mkdir(w.store.store.PoolTeam(w.key, w.seat.Team)+"/repo.2+site.podman.new-0000000000000003", 0o700); err != nil {
		t.Fatal(err)
	}
	a := w.lock()
	if err := w.store.QuarantineLeftovers(a, w.seat); err != nil {
		t.Fatal(err)
	}
	if err := w.store.QuarantineLeftovers(nil, w.seat); !errors.Is(err, ErrNotLocked) {
		t.Errorf("no lock: %v", err)
	}
	w.unlock(a)
	moved := quarantined(t, w)
	if len(moved) != 2 || exists(t, moved[0]+"/password") || exists(t, moved[1]+"/password") {
		t.Errorf("quarantine %v", moved)
	}
	if err := w.place(testPassword); err != nil {
		t.Errorf("a placement after the leftovers went: %v", err)
	}
}
