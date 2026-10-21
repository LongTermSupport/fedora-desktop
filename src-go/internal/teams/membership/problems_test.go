package membership

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func plainDesired() Desired {
	return Desired{V: 1, Session: "s", Kind: KindCcy, Checkout: "/c", Generation: 1, Seats: []Seat{}, Farewell: []Farewell{}}
}

func TestACheckedMembershipNeedsItsSeatsAndFarewellsAndTheirLimits(t *testing.T) {
	missing := plainDesired()
	missing.Seats = nil
	tooManyFarewells := plainDesired()
	tooManyFarewells.Farewell = make([]Farewell, maxFarewells+1)
	for name, d := range map[string]Desired{"no seats list": missing, "too many farewells": tooManyFarewells} {
		if _, err := d.validate(); err == nil {
			t.Errorf("%s: validated", name)
		}
	}
	if _, err := plainDesired().validate(); err != nil {
		t.Fatalf("a plain membership: %v", err)
	}
}

func TestWriteSaysWhenTheDirectoryIsInTheWay(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(filepath.Join(blocker, "a", "b"), plainDesired()); err == nil || !strings.Contains(err.Error(), "could not make the membership directory") {
		t.Fatalf("a file in the way: %v", err)
	}
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := Write(filepath.Join(link, "x", "y"), plainDesired()); err == nil || !strings.Contains(err.Error(), "is not a directory") {
		t.Fatalf("a symlink in the way: %v", err)
	}
}

func TestInitFailsWhenItCannotAnswerTheRelaunchOrRecordTheSeats(t *testing.T) {
	r := newRig(t)
	stuck := filepath.Join(DirOf(r.runtime, KindCcy, "s"), RelaunchFile)
	if err := os.MkdirAll(filepath.Join(stuck, "inner"), 0o700); err != nil {
		t.Fatal(err)
	}
	if code := r.init.Run(t.Context(), []string{"s", "/c", "1@a"}); code != term.ExitFailure || !strings.Contains(r.ErrOut.String(), RelaunchFile) {
		t.Fatalf("a relaunch request that cannot be removed: exit %d: %s", code, r.ErrOut.String())
	}
	r = newRig(t)
	r.init.Host.Registry = func() (*registry.Store, error) { return nil, errors.New("records unreadable") }
	if code := r.init.Run(t.Context(), []string{"--kind=cc", "s", "/c", "1@a"}); code != term.ExitFailure || !strings.Contains(r.ErrOut.String(), "records unreadable") {
		t.Fatalf("a cc session whose record cannot be reached: exit %d: %s", code, r.ErrOut.String())
	}
}

func TestRelaunchRequestsAreReadAndWithdrawn(t *testing.T) {
	dir := t.TempDir()
	if gone, err := WithdrawRestart(dir); gone || err != nil {
		t.Fatalf("none to withdraw: %v %v", gone, err)
	}
	if err := WriteRelaunch(dir, 2, stamp); err != nil {
		t.Fatal(err)
	}
	if gone, err := WithdrawRestart(dir); gone || err != nil {
		t.Fatalf("a first join's request is not a restart's: %v %v", gone, err)
	}
	if err := WriteRestart(dir, 3, stamp); err != nil {
		t.Fatal(err)
	}
	if pending, err := RelaunchPending(dir); !pending || err != nil {
		t.Fatalf("pending: %v %v", pending, err)
	}
	if gone, err := WithdrawRestart(dir); !gone || err != nil {
		t.Fatalf("a restart's request is withdrawn: %v %v", gone, err)
	}
}
