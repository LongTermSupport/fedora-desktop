package health

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	dir := filepath.Join(t.TempDir(), "teamchat-admin")
	locks := TeamLocks{Dir: dir, Clock: &clock{now: t0}}
	team := id(t, "alpha")
	held, err := locks.TryLock(t.Context(), team)
	if err != nil {
		t.Fatalf("TryLock: %v", err)
	}
	line, err := os.ReadFile(filepath.Clean(filepath.Join(dir, "team-alpha.lock")))
	if err != nil || !strings.HasPrefix(string(line), "health alpha pid=") {
		t.Fatalf("holder line %q, %v", line, err)
	}
	if _, err := locks.TryLock(t.Context(), team); !errors.Is(err, teamlock.ErrBusy) {
		t.Fatalf("second TryLock = %v, want busy", err)
	}
	if err := held.Release(); err != nil {
		t.Fatal(err)
	}
}
