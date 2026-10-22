package restore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// opsTeam is the team every guard test runs on.
func opsTeam(t *testing.T) paths.TeamID {
	t.Helper()
	team, err := paths.ParseTeamID("ops")
	if err != nil {
		t.Fatal(err)
	}
	return team
}

// teamDir makes root/<team>/ under a temp root and returns the root.
func teamDir(t *testing.T, team paths.TeamID) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(root+paths.AdminState(team), 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

// The guard is a teamlock.Guard, so every taker passes it as one.
var _ teamlock.Guard = Guard("")

// RG2: no record, no refusal.
func TestTheGuardPassesWithNoRecord(t *testing.T) {
	ops := opsTeam(t)
	if err := Guard(teamDir(t, ops))(ops); err != nil {
		t.Fatal(err)
	}
}

// RG2: a record refuses, as a PendingError naming the resume and the discard.
func TestTheGuardRefusesWhileARestoreIsRecorded(t *testing.T) {
	ops := opsTeam(t)
	root := teamDir(t, ops)
	if err := os.WriteFile(root+paths.RestoreRecord(ops), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := Guard(root)(ops)
	pending, ok := errors.AsType[*PendingError](err)
	if !ok || pending.Team != ops {
		t.Fatalf("Guard = %v, want a *PendingError for ops", err)
	}
	for _, want := range []string{"restore --team ops --resume", "--discard"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q does not name %q", err, want)
		}
	}
	if !errors.Is(err, ErrPending) {
		t.Error("a PendingError is not ErrPending")
	}
}

// RG2: anything of the name counts, a link (even a dangling one) and a directory included.
func TestTheGuardCountsALinkOrADirectory(t *testing.T) {
	ops := opsTeam(t)
	for name, plant := range map[string]func(string) error{
		"dangling link": func(p string) error { return os.Symlink(filepath.Join(filepath.Dir(p), "nowhere"), p) },
		"directory":     func(p string) error { return os.Mkdir(p, 0o700) },
	} {
		root := teamDir(t, ops)
		if err := plant(root + paths.RestoreRecord(ops)); err != nil {
			t.Fatal(err)
		}
		if err := Guard(root)(ops); !errors.Is(err, ErrPending) {
			t.Errorf("%s: Guard = %v, want ErrPending", name, err)
		}
	}
}

// RG2: a record that cannot be looked at is a failure, never a pass.
func TestTheGuardFailsWhenItCannotLook(t *testing.T) {
	ops := opsTeam(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(root+paths.AdminState(ops)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root+paths.AdminState(ops), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err := Guard(root)(ops)
	if err == nil || errors.Is(err, ErrPending) {
		t.Fatalf("Guard over a team directory that is a file = %v, want a failure", err)
	}
}

// RG2: the guard under a held lock: a take refused by it leaves the lock free.
func TestTheGuardRefusesATake(t *testing.T) {
	ops := opsTeam(t)
	root := teamDir(t, ops)
	if err := os.WriteFile(root+paths.RestoreRecord(ops), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := filepath.Join(t.TempDir(), "run")
	if _, err := teamlock.Take(t.Context(), run, ops, "team.apply", 0, fixedClock{}, Guard(root)); !errors.Is(err, ErrPending) {
		t.Fatalf("Take = %v, want ErrPending", err)
	}
}

// fixedClock is a clock that never moves: a take with no wait never sleeps.
type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.UnixMilli(1_760_000_000_000) }

func (fixedClock) Sleep(context.Context, time.Duration) error { return nil }
