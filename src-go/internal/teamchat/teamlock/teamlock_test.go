package teamlock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// t0 is the fake clock's start: a fixed instant, never the wall clock.
var t0 = time.UnixMilli(1_760_000_000_000)

// longWait is a play's wait, as host.PlayWait is.
const longWait = 10 * time.Minute

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

// TL1: the lock file is team-<team>.lock, 0600, holds the holder line while it is held, and
// Release clears the line and keeps the file.
func TestTakeWritesTheHolderLineAndReleaseClearsIt(t *testing.T) {
	dir := lockDir(t)
	l, err := Take(t.Context(), dir, mustTeam(t, "ops"), "team.apply", 0, newClock())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "team-ops.lock")
	if got, want := string(readFile(t, path)), holderLine("team.apply", t0.UnixMilli())+"\n"; got != want {
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
	if data := readFile(t, path); len(data) != 0 {
		t.Errorf("a released lock still holds %q", data)
	}
}

// TL1: a held lock with no wait is busy at once: ErrBusy, a *BusyError naming the path and the
// holder's line.
func TestTakeBusyCarriesTheHolder(t *testing.T) {
	dir := lockDir(t)
	first, err := Take(t.Context(), dir, mustTeam(t, "ops"), "member.add", 0, newClock())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseOrFail(t, first)
	clock := newClock()
	_, err = Take(t.Context(), dir, mustTeam(t, "ops"), "team.apply", 0, clock)
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("got %v, want ErrBusy", err)
	}
	busy, ok := errors.AsType[*BusyError](err)
	if !ok {
		t.Fatalf("got %v, want a BusyError", err)
	}
	path := filepath.Join(dir, "team-ops.lock")
	if want := holderLine("member.add", t0.UnixMilli()); busy.Holder != want || busy.Path != path {
		t.Errorf("busy %+v, want holder %q at %s", busy, want, path)
	}
	if len(clock.slept) != 0 {
		t.Errorf("a wait of 0 slept %v", clock.slept)
	}
	if want := "team lock " + path + " is held by: " + busy.Holder; err.Error() != want {
		t.Errorf("message %q, want %q", err.Error(), want)
	}
	if errors.Is(errors.New("other"), ErrBusy) || (&BusyError{}).Is(context.Canceled) {
		t.Error("only a BusyError is ErrBusy")
	}
}

// TL1: a wait is bounded by the clock it is given.
func TestTakeWaitIsBounded(t *testing.T) {
	dir := lockDir(t)
	first, err := Take(t.Context(), dir, mustTeam(t, "ops"), "backup", 0, newClock())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseOrFail(t, first)
	clock := newClock()
	_, err = Take(t.Context(), dir, mustTeam(t, "ops"), "team.apply", longWait, clock)
	if busy, ok := errors.AsType[*BusyError](err); !ok || busy.Holder == unknownHolder {
		t.Fatalf("got %v, want a BusyError naming the holder", err)
	}
	if waited := clock.now.Sub(t0); waited < longWait || waited > longWait+lockPoll {
		t.Errorf("waited %v, want %v", waited, longWait)
	}
}

// TL1: a holder that lets go during the wait hands the lock over.
func TestTakeWaitsForTheHolder(t *testing.T) {
	dir := lockDir(t)
	first, err := Take(t.Context(), dir, mustTeam(t, "ops"), "backup", 0, newClock())
	if err != nil {
		t.Fatal(err)
	}
	clock := newClock()
	clock.onSleep = func(n int) {
		if n == 3 {
			releaseOrFail(t, first)
		}
	}
	second, err := Take(t.Context(), dir, mustTeam(t, "ops"), "team.apply", longWait, clock)
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

// TL1: two teams' locks are independent.
func TestTeamLocksAreIndependent(t *testing.T) {
	dir := lockDir(t)
	a, err := Take(t.Context(), dir, mustTeam(t, "ops"), "team.apply", 0, newClock())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseOrFail(t, a)
	b, err := Take(t.Context(), dir, mustTeam(t, "dev"), "team.apply", 0, newClock())
	if err != nil {
		t.Fatal(err)
	}
	releaseOrFail(t, b)
}

func TestTakeStopsWhenTheContextEnds(t *testing.T) {
	dir := lockDir(t)
	first, err := Take(t.Context(), dir, mustTeam(t, "ops"), "backup", 0, newClock())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseOrFail(t, first)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Take(ctx, dir, mustTeam(t, "ops"), "team.apply", longWait, newClock()); !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
}

// TL1: the lock is never taken through a link, on a file that is not a regular one, in a
// directory that is a link, not a directory or not 0700, or for a verb outside the grammar.
func TestTakeRefusals(t *testing.T) {
	team := mustTeam(t, "ops")
	linked := lockDir(t)
	if err := os.Symlink(filepath.Join(t.TempDir(), "elsewhere"), filepath.Join(linked, "team-ops.lock")); err != nil {
		t.Fatal(err)
	}
	fifo := lockDir(t)
	if err := unix.Mkfifo(filepath.Join(fifo, "team-ops.lock"), 0o600); err != nil {
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
		"lock file is a fifo":     {fifo, "team.apply"},
		"directory is not 0700":   {loose, "team.apply"},
		"directory is a link":     {dirLink, "team.apply"},
		"directory is not a dir":  {notDir, "team.apply"},
		"directory parent absent": {filepath.Join(t.TempDir(), "a", "b"), "team.apply"},
		"empty verb":              {lockDir(t), ""},
		"verb with a space":       {lockDir(t), "team apply"},
		"verb in capitals":        {lockDir(t), "Apply"},
		"verb too long":           {lockDir(t), strings.Repeat("a", 33)},
	} {
		l, err := Take(t.Context(), c.dir, team, c.verb, 0, newClock())
		if err == nil {
			releaseOrFail(t, l)
			t.Errorf("%s: taken", name)
		} else if errors.Is(err, ErrBusy) {
			t.Errorf("%s: %v is not busy", name, err)
		}
	}
}

// TL1: a lock directory owned by anyone but the taker is refused, so a directory another user
// made first cannot hold or spoof the lock.
func TestTakeRefusesADirectoryOfAnotherOwner(t *testing.T) {
	dir := lockDir(t)
	if _, err := take(t.Context(), dir, mustTeam(t, "ops"), "team.apply", 0, newClock(), os.Geteuid()+1); err == nil || !strings.Contains(err.Error(), "owned") {
		t.Errorf("got %v, want the owner refused", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "team-ops.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused directory gained a lock file: %v", err)
	}
}

// TL1: a missing lock directory is made, 0700.
func TestTakeMakesTheDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "teamchat-admin")
	l, err := Take(t.Context(), dir, mustTeam(t, "ops"), "team.apply", 0, newClock())
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

// TL1: the holder's line reaches a terminal and the journal, so a busy error names it only when
// it is one printable line of at most 256 bytes.
func TestBusyHolderLineIsSanitised(t *testing.T) {
	for name, content := range map[string]string{
		"too long":       strings.Repeat("x", 257) + "\n",
		"escape":         "apply ops \x1b[2J pid=1\n",
		"two lines":      "apply ops pid=1\nsecond\n",
		"not written":    "",
		"no end of line": strings.Repeat("y", 300),
	} {
		dir := lockDir(t)
		held, err := Take(t.Context(), dir, mustTeam(t, "ops"), "backup", 0, newClock())
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, "team-ops.lock"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err = Take(t.Context(), dir, mustTeam(t, "ops"), "team.apply", 0, newClock())
		if busy, ok := errors.AsType[*BusyError](err); !ok || busy.Holder != unknownHolder {
			t.Errorf("%s: got %v, want the unreadable holder", name, err)
		}
		releaseOrFail(t, held)
	}
	dir := lockDir(t)
	held, err := Take(t.Context(), dir, mustTeam(t, "ops"), "backup", 0, newClock())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseOrFail(t, held)
	longest := strings.Repeat("z", 256)
	if err := os.WriteFile(filepath.Join(dir, "team-ops.lock"), []byte(longest+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Take(t.Context(), dir, mustTeam(t, "ops"), "team.apply", 0, newClock()); err == nil || !strings.HasSuffix(err.Error(), ": "+longest) {
		t.Errorf("a 256-byte line was not named: %v", err)
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
	if SystemClock().Now().IsZero() {
		t.Error("the system clock has no time")
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
