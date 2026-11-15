package apply

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
)

var errInjected = errors.New("injected failure")

// faultAdmin is the world's admin client with one failure injected before a call, and a record
// of every password a reset set, printed or not (HU15).
type faultAdmin struct {
	Admin

	failAccount, failReset bool
	afterReset             func()
	set                    []Password
}

func (f *faultAdmin) Account(ctx context.Context, userID string) (matrix.Account, bool, error) {
	if f.failAccount {
		f.failAccount = false
		return matrix.Account{}, false, errInjected
	}
	return f.Admin.Account(ctx, userID)
}

func (f *faultAdmin) ResetPassword(ctx context.Context, userID, password string) error {
	if f.failReset {
		f.failReset = false
		return errInjected
	}
	if err := f.Admin.ResetPassword(ctx, userID, password); err != nil {
		return err
	}
	f.set = append(f.set, Password(password))
	if f.afterReset != nil {
		after := f.afterReset
		f.afterReset = nil
		after()
	}
	return nil
}

// breakRoster makes the next roster write fail, with the roster file a directory that is not
// empty (a rename cannot replace it, whoever runs the test), and returns what puts the file back.
func (w *world) breakRoster(t *testing.T) func() {
	t.Helper()
	dir, name := filepath.Split(w.deps().RosterPath)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := root.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	saved, err := root.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if err = root.Remove(name); err != nil {
		t.Fatal(err)
	}
	if err = root.MkdirAll(filepath.Join(name, "blocker"), 0o700); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := root.RemoveAll(name); err != nil {
			t.Fatal(err)
		}
		if err := root.WriteFile(name, saved, fileMode); err != nil {
			t.Fatal(err)
		}
	}
}

// setNeeds sets alice's needs, as a restore does.
func (w *world) setNeeds(t *testing.T, needs string) {
	t.Helper()
	s := w.file(t)
	h := s.roster.Humans["alice"]
	h.Needs = needs
	s.roster.Humans["alice"] = h
	if err := s.saveRoster(); err != nil {
		t.Fatal(err)
	}
}

// noPasswordWritten fails when any file of the team's tree holds one of the passwords.
func (w *world) noPasswordWritten(t *testing.T, passwords []Password) {
	t.Helper()
	root, err := os.OpenRoot(w.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	tree := root.FS()
	err = fs.WalkDir(tree, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !d.Type().IsRegular() {
			t.Errorf("%s is not a regular file", path)
			return nil
		}
		body, err := fs.ReadFile(tree, path)
		if err != nil {
			return err
		}
		for _, pw := range passwords {
			if bytes.Contains(body, []byte(pw.Reveal())) {
				t.Errorf("%s holds a password a reset set", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// HU3: the roster write clearing needs fails after a successful reset. The verb fails and prints
// no password, the old one is already dead, needs still says password, and a second run mends it.
func TestHumanPasswordSecondRosterWriteFailing(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	old := w.human(t, team, OpHumanPassword).Password
	w.setNeeds(t, needsPass)
	var repair func()
	admin := &faultAdmin{Admin: w.admin}
	admin.afterReset = func() { repair = w.breakRoster(t) }
	res, err := RunHuman(t.Context(), w.memberDeps(admin), team, HumanRequest{Op: OpHumanPassword, Name: "alice"})
	if err == nil {
		t.Fatal("a failed second roster write is not a failure")
	}
	if res.Password != "" {
		t.Error("a failed second roster write answered a password")
	}
	if len(admin.set) != 1 || repair == nil {
		t.Fatalf("the reset set %d passwords, want 1", len(admin.set))
	}
	if strings.Contains(err.Error(), admin.set[0].Reveal()) {
		t.Error("the failure carries the password")
	}
	repair()
	if err = w.signIn(t, old, "OLD"); err == nil {
		t.Error("the old password still signs in")
	}
	if got := w.file(t).roster.Humans["alice"].Needs; got != needsPass {
		t.Errorf("needs = %q after the failure, want %q", got, needsPass)
	}
	again := w.human(t, team, OpHumanPassword)
	w.onlyThisSignsIn(t, again.Password, admin.set)
	if got := w.file(t).roster.Humans["alice"].Needs; got != "" {
		t.Errorf("needs = %q after the second run, want none", got)
	}
}

// onlyThisSignsIn fails unless the password signs in and none of the earlier ones does.
func (w *world) onlyThisSignsIn(t *testing.T, password Password, earlier []Password) {
	t.Helper()
	if err := w.signIn(t, password, "PHONE"); err != nil {
		t.Errorf("the last password does not sign in: %v", err)
	}
	for _, pw := range earlier {
		if pw != password && w.signIn(t, pw, "STALE") == nil {
			t.Error("an earlier password still signs in")
		}
	}
}

// HU15: a failure injected before each call and each roster write of human password and human
// logout, then the verb run again, ends with alice enrolled, needs cleared by a password, and no
// second valid password; no file of the tree holds a password any reset set.
func TestHumanVerbsRecoverFromEachFailure(t *testing.T) {
	faults := []struct {
		name   string
		inject func(*testing.T, *world, *faultAdmin) func()
	}{
		{"the account lookup", func(_ *testing.T, _ *world, a *faultAdmin) func() { a.failAccount = true; return nil }},
		{"the first roster write", func(t *testing.T, w *world, _ *faultAdmin) func() { t.Helper(); return w.breakRoster(t) }},
		{"the reset", func(_ *testing.T, _ *world, a *faultAdmin) func() { a.failReset = true; return nil }},
	}
	for _, op := range []string{OpHumanPassword, OpHumanLogout} {
		for _, f := range faults {
			t.Run(op+" "+f.name, func(t *testing.T) {
				w := newWorld(t)
				team := w.team(t)
				w.setNeeds(t, needsPass)
				admin := &faultAdmin{Admin: w.admin}
				repair := f.inject(t, w, admin)
				res, err := RunHuman(t.Context(), w.memberDeps(admin), team, HumanRequest{Op: op, Name: "alice"})
				if err == nil {
					t.Fatalf("a failure in %s is not a failure", f.name)
				}
				if res.Password != "" {
					t.Error("a failed run answered a password")
				}
				if repair != nil {
					repair()
				}
				w.recovered(t, team, admin)
			})
		}
	}
}

// recovered runs human password again, through admin so its reset is recorded, and checks the
// state HU15 asks for.
func (w *world) recovered(t *testing.T, team declare.Team, admin *faultAdmin) {
	t.Helper()
	res, err := RunHuman(t.Context(), w.memberDeps(admin), team, HumanRequest{Op: OpHumanPassword, Name: "alice"})
	if err != nil {
		t.Fatalf("the run after the failure: %v", err)
	}
	w.onlyThisSignsIn(t, res.Password, admin.set)
	h := w.file(t).roster.Humans["alice"]
	if h.State != HumanEnrolled || h.Needs != "" {
		t.Errorf("alice is %q with needs %q, want enrolled with none", h.State, h.Needs)
	}
	w.noPasswordWritten(t, admin.set)
}
