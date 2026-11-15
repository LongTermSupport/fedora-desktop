package apply

import (
	"errors"
	"net/http"
	"regexp"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
)

var humanPassword = regexp.MustCompile(`^[A-Za-z0-9]{32}$`)

// human runs op for alice, the declared human every test here names.
func (w *world) human(t *testing.T, team declare.Team, op string) Result {
	t.Helper()
	res, err := RunHuman(t.Context(), w.memberDeps(nil), team, HumanRequest{Op: op, Name: "alice"})
	if err != nil {
		t.Fatalf("%s alice: %v\nchanged: %v", op, err, res.Changed)
	}
	return res
}

// signIn is a login of alice with password as a new device, the proof a password works.
func (w *world) signIn(t *testing.T, password Password, device string) error {
	t.Helper()
	_, err := w.admin.LoginDevice(t.Context(), "alice", password.Reveal(), device)
	return err
}

// HU2: a password of 32 characters of [A-Za-z0-9] that signs the human in, answered once.
func TestHumanPasswordSetsOneThatSignsIn(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	res := w.human(t, team, OpHumanPassword)
	if !humanPassword.MatchString(res.Password.Reveal()) {
		t.Fatalf("the password is not 32 characters of [A-Za-z0-9]")
	}
	if err := w.signIn(t, res.Password, "PHONE"); err != nil {
		t.Fatalf("the new password does not sign in: %v", err)
	}
	if !slices.Contains(res.Changed, "human alice password set") {
		t.Errorf("changed = %v", res.Changed)
	}
	again := w.human(t, team, OpHumanPassword)
	if again.Password == res.Password {
		t.Error("a second human password gave the same password")
	}
	if err := w.signIn(t, res.Password, "OLD"); err == nil {
		t.Error("the first password still signs in after a second human password")
	}
}

// HU2: the reset signs every device out.
func TestHumanPasswordSignsEveryDeviceOut(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	first := w.human(t, team, OpHumanPassword)
	if err := w.signIn(t, first.Password, "PHONE"); err != nil {
		t.Fatal(err)
	}
	w.human(t, team, OpHumanPassword)
	if got := w.human(t, team, OpHumanDevices).Devices; len(got) != 0 {
		t.Errorf("devices after a password reset = %v, want none", got)
	}
}

// HU3: the roster is written before the homeserver call, revoked_seq set to that write's seq,
// and a reset that fails leaves it raised and needs as it was.
func TestHumanPasswordWritesTheRosterFirst(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	before := w.file(t).roster
	w.srv.Inject(fakehs.Fault{Method: http.MethodPut, Path: "v2/users/%40alice", Status: http.StatusForbidden, Body: []byte(`{"errcode":"M_FORBIDDEN","error":"refused"}`)})
	res, err := RunHuman(t.Context(), w.memberDeps(nil), team, HumanRequest{Op: OpHumanPassword, Name: "alice"})
	if err == nil {
		t.Fatal("a failed reset is not a failure")
	}
	if res.Password != "" {
		t.Error("a failed reset answered a password")
	}
	after := w.file(t).roster
	if after.Seq != before.Seq+1 {
		t.Errorf("seq %d after a failed reset, want %d", after.Seq, before.Seq+1)
	}
	if got := after.Humans["alice"].RevokedSeq; got != after.Seq {
		t.Errorf("revoked_seq %d, want the write's seq %d", got, after.Seq)
	}
}

// HU3: a human whose needs is password (set by a restore) has it cleared after the reset.
func TestHumanPasswordClearsNeeds(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	s := w.file(t)
	h := s.roster.Humans["alice"]
	h.Needs = needsPass
	s.roster.Humans["alice"] = h
	if err := s.saveRoster(); err != nil {
		t.Fatal(err)
	}
	w.human(t, team, OpHumanPassword)
	if got := w.file(t).roster.Humans["alice"].Needs; got != "" {
		t.Errorf("needs = %q after human password, want none", got)
	}
}

// HU4: logout signs every device out, answers no password, and leaves needs as it was.
func TestHumanLogoutSignsOutAndAnswersNoPassword(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	first := w.human(t, team, OpHumanPassword)
	if err := w.signIn(t, first.Password, "PHONE"); err != nil {
		t.Fatal(err)
	}
	res := w.human(t, team, OpHumanLogout)
	if res.Password != "" {
		t.Error("human logout answered a password")
	}
	if !slices.Contains(res.Changed, "human alice signed out") {
		t.Errorf("changed = %v", res.Changed)
	}
	if err := w.signIn(t, first.Password, "AGAIN"); err == nil {
		t.Error("the old password still signs in after human logout")
	}
	if got := w.human(t, team, OpHumanDevices).Devices; len(got) != 0 {
		t.Errorf("devices after logout = %v, want none", got)
	}
}

// HU5: devices answers the account's devices and writes nothing.
func TestHumanDevicesListsAndWritesNothing(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	pw := w.human(t, team, OpHumanPassword).Password
	if err := w.signIn(t, pw, "PHONE"); err != nil {
		t.Fatal(err)
	}
	seq := w.file(t).roster.Seq
	mark := w.mark()
	res := w.human(t, team, OpHumanDevices)
	if len(res.Devices) != 1 || res.Devices[0].ID != "PHONE" {
		t.Errorf("devices = %v, want PHONE", res.Devices)
	}
	if got := w.writes(mark); len(got) != 0 {
		t.Errorf("human devices wrote %v", got)
	}
	if got := w.file(t).roster.Seq; got != seq {
		t.Errorf("human devices moved seq %d to %d", seq, got)
	}
}

// HU1: an undeclared, removed or account-less human is refused before any change.
func TestHumanRefusals(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	s := w.file(t)
	s.roster.Humans["bob"] = Human{State: HumanRemoved, SinceMS: 1}
	if err := s.saveRoster(); err != nil {
		t.Fatal(err)
	}
	ghost := team
	ghost.Humans = append(slices.Clone(team.Humans), "dave")
	s = w.file(t)
	s.roster.Humans["dave"] = Human{State: HumanEnrolled, SinceMS: 1}
	if err := s.saveRoster(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		team declare.Team
	}{{"carol", team}, {"bob", team}, {"dave", ghost}}
	for _, c := range cases {
		for _, op := range []string{OpHumanPassword, OpHumanLogout, OpHumanDevices} {
			seq := w.file(t).roster.Seq
			mark := w.mark()
			_, err := RunHuman(t.Context(), w.memberDeps(nil), c.team, HumanRequest{Op: op, Name: c.name})
			var e *Error
			if !errors.As(err, &e) || !e.Drift {
				t.Errorf("%s %s: err = %v, want a refusal", op, c.name, err)
			}
			if got := w.writes(mark); len(got) != 0 {
				t.Errorf("%s %s wrote %v", op, c.name, got)
			}
			if got := w.file(t).roster.Seq; got != seq {
				t.Errorf("%s %s moved seq", op, c.name)
			}
		}
	}
}

// The op is one of the three this build serves.
func TestHumanUnknownOpIsRefused(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	if _, err := RunHuman(t.Context(), w.memberDeps(nil), team, HumanRequest{Op: "human.lock", Name: "alice"}); err == nil {
		t.Error("human.lock is served")
	}
}
