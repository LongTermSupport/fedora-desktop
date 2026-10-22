package host

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// t0 is the fake clock's start: a fixed instant, never the wall clock.
var t0 = time.UnixMilli(1_760_000_000_000)

// fakeClock is a clock whose Sleep only moves its time forward, and can run a hook per sleep.
type fakeClock struct {
	now     time.Time
	slept   []time.Duration
	onSleep func(n int)
}

func newClock() *fakeClock { return &fakeClock{now: t0} }

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.now = c.now.Add(d)
	c.slept = append(c.slept, d)
	if c.onSleep != nil {
		c.onSleep(len(c.slept))
	}
	return nil
}

func mustTeam(t *testing.T, s string) paths.TeamID {
	t.Helper()
	team, err := paths.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return team
}

func lockDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "run")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func holderLine(verb string, ms int64) string {
	return verb + " ops pid=" + strconv.Itoa(os.Getpid()) + " since=" + strconv.FormatInt(ms, 10)
}

// HS1: the lock file is team-<team>.lock, 0600, and holds the holder line while it is held.
func TestTakeLockWritesTheHolderLine(t *testing.T) {
	dir := lockDir(t)
	l, err := TakeLock(t.Context(), dir, mustTeam(t, "ops"), "team.apply", 0, newClock())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "team-ops.lock")
	data := readFile(t, path)
	if got, want := string(data), holderLine("team.apply", t0.UnixMilli())+"\n"; got != want {
		t.Errorf("holder line %q, want %q", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", info.Mode().Perm())
	}
	if l.Team() != mustTeam(t, "ops") {
		t.Errorf("team %v", l.Team())
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the lock file must never be deleted: %v", err)
	}
}

// HS2: a held lock with no wait is busy at once, and the error carries the holder's line.
func TestTakeLockBusyCarriesTheHolder(t *testing.T) {
	dir := lockDir(t)
	first, err := TakeLock(t.Context(), dir, mustTeam(t, "ops"), "member.add", 0, newClock())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseOrFail(t, first)
	clock := newClock()
	_, err = TakeLock(t.Context(), dir, mustTeam(t, "ops"), "team.apply", 0, clock)
	busy, ok := errors.AsType[*BusyError](err)
	if !ok {
		t.Fatalf("got %v, want a BusyError", err)
	}
	if want := holderLine("member.add", t0.UnixMilli()); busy.Holder != want {
		t.Errorf("holder %q, want %q", busy.Holder, want)
	}
	if len(clock.slept) != 0 {
		t.Errorf("a wait of 0 slept %v", clock.slept)
	}
	if want := "team lock " + filepath.Join(dir, "team-ops.lock") + " is held by: " + busy.Holder; err.Error() != want {
		t.Errorf("message %q, want %q", err.Error(), want)
	}
}

// HS2: a wait is bounded by the clock it is given.
func TestTakeLockWaitIsBounded(t *testing.T) {
	dir := lockDir(t)
	first, err := TakeLock(t.Context(), dir, mustTeam(t, "ops"), "backup", 0, newClock())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseOrFail(t, first)
	clock := newClock()
	_, err = TakeLock(t.Context(), dir, mustTeam(t, "ops"), "team.apply", PlayWait, clock)
	if busy, ok := errors.AsType[*BusyError](err); !ok || busy.Holder == "" {
		t.Fatalf("got %v, want a BusyError", err)
	}
	if waited := clock.now.Sub(t0); waited < PlayWait || waited > PlayWait+lockPoll {
		t.Errorf("waited %v, want %v", waited, PlayWait)
	}
}

// HS2: a holder that lets go during the wait hands the lock over.
func TestTakeLockWaitsForTheHolder(t *testing.T) {
	dir := lockDir(t)
	first, err := TakeLock(t.Context(), dir, mustTeam(t, "ops"), "backup", 0, newClock())
	if err != nil {
		t.Fatal(err)
	}
	clock := newClock()
	clock.onSleep = func(n int) {
		if n == 3 {
			releaseOrFail(t, first)
		}
	}
	second, err := TakeLock(t.Context(), dir, mustTeam(t, "ops"), "team.apply", PlayWait, clock)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseOrFail(t, second)
	if len(clock.slept) != 3 {
		t.Errorf("slept %d times, want 3", len(clock.slept))
	}
	data := readFile(t, filepath.Join(dir, "team-ops.lock"))
	if want := holderLine("team.apply", clock.now.UnixMilli()) + "\n"; string(data) != want {
		t.Errorf("holder %q, want %q", data, want)
	}
}

// HS2: two teams' locks are independent.
func TestTeamLocksAreIndependent(t *testing.T) {
	dir := lockDir(t)
	a, err := TakeLock(t.Context(), dir, mustTeam(t, "ops"), "team.apply", 0, newClock())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseOrFail(t, a)
	b, err := TakeLock(t.Context(), dir, mustTeam(t, "dev"), "team.apply", 0, newClock())
	if err != nil {
		t.Fatal(err)
	}
	releaseOrFail(t, b)
}

func TestTakeLockStopsWhenTheContextEnds(t *testing.T) {
	dir := lockDir(t)
	first, err := TakeLock(t.Context(), dir, mustTeam(t, "ops"), "backup", 0, newClock())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseOrFail(t, first)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := TakeLock(ctx, dir, mustTeam(t, "ops"), "team.apply", PlayWait, newClock()); !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
}

// HS1: the lock is never taken through a link, in a directory that is not 0700, or for a verb
// that would break the holder line.
func TestTakeLockRefusals(t *testing.T) {
	team := mustTeam(t, "ops")
	linked := lockDir(t)
	if err := os.Symlink(filepath.Join(t.TempDir(), "elsewhere"), filepath.Join(linked, "team-ops.lock")); err != nil {
		t.Fatal(err)
	}
	loose := lockDir(t)
	if err := os.Chmod(loose, 0o600); err != nil {
		t.Fatal(err)
	}
	dirLink := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(lockDir(t), dirLink); err != nil {
		t.Fatal(err)
	}
	notDir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notDir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct{ dir, verb string }{
		"lock file is a link":     {linked, "team.apply"},
		"directory is not 0700":   {loose, "team.apply"},
		"directory is a link":     {dirLink, "team.apply"},
		"directory is not a dir":  {notDir, "team.apply"},
		"directory parent absent": {filepath.Join(t.TempDir(), "a", "b"), "team.apply"},
		"empty verb":              {lockDir(t), ""},
		"verb with a space":       {lockDir(t), "team apply"},
	} {
		if l, err := TakeLock(t.Context(), c.dir, team, c.verb, 0, newClock()); err == nil {
			releaseOrFail(t, l)
			t.Errorf("%s: taken", name)
		}
	}
}

// HS1: a missing lock directory is made, 0700.
func TestTakeLockMakesTheDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "teamchat-admin")
	l, err := TakeLock(t.Context(), dir, mustTeam(t, "ops"), "team.apply", 0, newClock())
	if err != nil {
		t.Fatal(err)
	}
	releaseOrFail(t, l)
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("mode %v, want 0700", info.Mode().Perm())
	}
}

func TestSystemClockSleepEndsWithTheContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := SystemClock().Sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("got %v", err)
	}
	if err := SystemClock().Sleep(t.Context(), 0); err != nil {
		t.Errorf("got %v", err)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func releaseOrFail(t *testing.T, l *Lock) {
	t.Helper()
	if err := l.Release(); err != nil {
		t.Error(err)
	}
}
