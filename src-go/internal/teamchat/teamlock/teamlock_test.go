package teamlock

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

var since = time.UnixMilli(1_800_000_000_000)

func mustTeam(t *testing.T) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestTryLockWritesTheHolderAndRefusesASecond(t *testing.T) {
	path := filepath.Join(t.TempDir(), "team-alpha.lock")
	held, err := TryLock(path, Holder{Verb: "health", Team: mustTeam(t), PID: 42, Since: since})
	if err != nil {
		t.Fatalf("TryLock: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "health alpha pid=42 since=1800000000000\n" {
		t.Fatalf("holder line %q, %v", data, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, %v", info.Mode().Perm(), err)
	}
	_, err = TryLock(path, Holder{Verb: "apply", Team: mustTeam(t), PID: 7, Since: since})
	var busy *BusyError
	if !errors.Is(err, ErrBusy) || !errors.As(err, &busy) || busy.Holder != "health alpha pid=42 since=1800000000000" {
		t.Fatalf("second TryLock = %v, want busy naming the holder", err)
	}
	if err = held.Unlock(); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	again, err := TryLock(path, Holder{Verb: "apply", Team: mustTeam(t), PID: 7, Since: since})
	if err != nil {
		t.Fatalf("TryLock after Unlock: %v", err)
	}
	data, err = os.ReadFile(path)
	if err != nil || string(data) != "apply alpha pid=7 since=1800000000000\n" {
		t.Fatalf("holder line %q, %v", data, err)
	}
	if err := again.Unlock(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("the lock file was unlinked")
	}
}

func TestTryLockRefusesALink(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "real"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "team-alpha.lock")
	if err := os.Symlink(filepath.Join(dir, "real"), path); err != nil {
		t.Fatal(err)
	}
	if _, err := TryLock(path, Holder{Verb: "health", Team: mustTeam(t), PID: 1, Since: since}); err == nil || errors.Is(err, ErrBusy) {
		t.Fatalf("TryLock through a link = %v", err)
	}
}

func TestBusyHolderLineIsBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "team-alpha.lock")
	held, err := TryLock(path, Holder{Verb: "health", Team: mustTeam(t), PID: 1, Since: since})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if unlockErr := held.Unlock(); unlockErr != nil {
			t.Error(unlockErr)
		}
	})
	if err = os.WriteFile(path, []byte(strings.Repeat("x", 300)+"\x1b[2J\nsecond"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = TryLock(path, Holder{Verb: "apply", Team: mustTeam(t), PID: 2, Since: since})
	var busy *BusyError
	if !errors.As(err, &busy) || busy.Holder != unknownHolder {
		t.Fatalf("busy = %v, want the unknown holder", err)
	}
	if !strings.Contains(err.Error(), unknownHolder) {
		t.Fatalf("error %q does not name the holder", err)
	}
}

func TestHolderRefusesABadVerb(t *testing.T) {
	path := filepath.Join(t.TempDir(), "team-alpha.lock")
	if _, err := TryLock(path, Holder{Verb: "two words", Team: mustTeam(t), PID: 1, Since: since}); err == nil {
		t.Fatal("a verb with a space was written")
	}
}
