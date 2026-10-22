package health

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

func TestStatfsSpace(t *testing.T) {
	free, total, err := StatfsSpace{Path: t.TempDir()}.Space()
	if err != nil || total <= 0 || free < 0 || free > total {
		t.Fatalf("Space = %d, %d, %v", free, total, err)
	}
	if _, _, err := (StatfsSpace{Path: filepath.Join(t.TempDir(), "none")}).Space(); err == nil {
		t.Fatal("a missing path has free space")
	}
}

func TestTeamLocks(t *testing.T) {
	dir := t.TempDir()
	locks := TeamLocks{Path: func(string) string { return filepath.Join(dir, "team.lock") }, PID: 5, Clock: &clock{now: t0}}
	team := id(t, "alpha")
	held, err := locks.TryLock(team)
	if err != nil {
		t.Fatalf("TryLock: %v", err)
	}
	if _, err := locks.TryLock(team); !errors.Is(err, teamlock.ErrBusy) {
		t.Fatalf("second TryLock = %v, want busy", err)
	}
	if err := held.Unlock(); err != nil {
		t.Fatal(err)
	}
}
