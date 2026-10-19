package playlock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// start is the fixed instant every test's clock begins at.
var start = time.Date(2026, 10, 16, 12, 0, 5, 0, time.UTC)

// rig is a Waiter over a scratch runtime directory, a clock that only the fake sleep moves,
// and a hook run at every sleep (where a test lets the other holder go).
type rig struct {
	w      *Waiter
	dir    string
	now    time.Time
	sleeps int
	onNap  func(n int)
}

func newRig(t *testing.T) *rig {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "run")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	r := &rig{dir: dir, now: start}
	r.w = &Waiter{
		RuntimeDir: dir, UID: os.Getuid(), GID: os.Getgid(), PID: 4321,
		Now: func() time.Time { return r.now },
		Sleep: func(ctx context.Context, d time.Duration) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			r.sleeps++
			r.now = r.now.Add(d)
			if r.onNap != nil {
				r.onNap(r.sleeps)
			}
			return nil
		},
	}
	return r
}

// holdElsewhere takes the lock on an open file description of its own, as another play run
// does, with note as what that run wrote; release lets it go.
func (r *rig) holdElsewhere(t *testing.T, note string) func() {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(r.dir, Name), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte(note), 0); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}
}

func (r *rig) note(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(r.dir, Name))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

const basicNote = "pid=999 what=run.bash playbooks/imports/play-basic-configs.yml since=2026-10-16T11:59:20Z\n"

func TestTakeAFreeLockHoldsItAndWritesTheHolderAsTheShellTakerDoes(t *testing.T) {
	r := newRig(t)
	lock, err := r.w.Take(t.Context(), "ccy-teams apply alpha", time.Minute, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := r.note(t), "pid=4321 what=ccy-teams apply alpha since=2026-10-16T12:00:05Z\n"; got != want {
		t.Errorf("note %q, want %q", got, want)
	}
	if r.sleeps != 0 {
		t.Errorf("a free lock waited %d times", r.sleeps)
	}
	if _, busyErr := r.w.Take(t.Context(), "second", 0, nil); !errors.As(busyErr, new(*TimeoutError)) {
		t.Errorf("a second taker got %v, want a timeout: the first still holds it", busyErr)
	}
	if err = lock.Release(); err != nil {
		t.Fatal(err)
	}
	if err = lock.Release(); err != nil {
		t.Errorf("a second release: %v", err)
	}
	again, err := r.w.Take(t.Context(), "third", 0, nil)
	if err != nil {
		t.Fatalf("released, it could not be taken again: %v", err)
	}
	if err := again.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestTakeWaitsForTheHolderSayingWhoItIsThenTakesTheLockItself(t *testing.T) {
	r := newRig(t)
	release := r.holdElsewhere(t, basicNote)
	var seen []Holder
	var waited []time.Duration
	r.onNap = func(n int) {
		if n == 3 {
			release()
		}
	}
	lock, err := r.w.Take(t.Context(), "ccy-teams apply alpha", 30*time.Minute, func(h Holder, d time.Duration) {
		seen = append(seen, h)
		waited = append(waited, d)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := lock.Release(); err != nil {
			t.Error(err)
		}
	}()
	if len(seen) != 3 {
		t.Fatalf("told of the holder %d times, want 3 (once before each wait)", len(seen))
	}
	want := Holder{
		PID: 999, What: "run.bash playbooks/imports/play-basic-configs.yml",
		Since: time.Date(2026, 10, 16, 11, 59, 20, 0, time.UTC), Note: strings.TrimSpace(basicNote),
	}
	if seen[0] != want {
		t.Errorf("holder %+v, want %+v", seen[0], want)
	}
	if waited[0] != 0 || waited[2] != 2*time.Second {
		t.Errorf("waited %v", waited)
	}
	if !strings.HasPrefix(r.note(t), "pid=4321 what=ccy-teams apply alpha ") {
		t.Errorf("the note is not the new holder's: %q", r.note(t))
	}
}

func TestTakeGivesUpAtTheBoundNamingTheHolderAndHoldsNothing(t *testing.T) {
	r := newRig(t)
	release := r.holdElsewhere(t, basicNote)
	defer release()
	_, err := r.w.Take(t.Context(), "ccy-teams apply alpha", 5*time.Second, func(Holder, time.Duration) {})
	te, ok := errors.AsType[*TimeoutError](err)
	if !ok {
		t.Fatalf("got %v, want a TimeoutError", err)
	}
	if te.Holder.PID != 999 || te.Waited != 5*time.Second {
		t.Errorf("timeout %+v", te)
	}
	if !strings.Contains(err.Error(), "run.bash playbooks/imports/play-basic-configs.yml (pid 999)") {
		t.Errorf("error %q does not name the holder", err)
	}
	if r.note(t) != basicNote {
		t.Errorf("a taker that gave up wrote the note: %q", r.note(t))
	}
}

func TestTakeStopsWhenTheContextEnds(t *testing.T) {
	r := newRig(t)
	release := r.holdElsewhere(t, basicNote)
	defer release()
	ctx, cancel := context.WithCancel(t.Context())
	r.onNap = func(int) { cancel() }
	_, err := r.w.Take(ctx, "x", time.Hour, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if r.sleeps != 1 {
		t.Errorf("slept %d times after the cancel", r.sleeps)
	}
}

func TestTakeRefusesAnUnsafeRuntimeDirectoryOrLockFile(t *testing.T) {
	cases := map[string]func(t *testing.T, r *rig) string{
		"missing directory": func(_ *testing.T, r *rig) string {
			r.w.RuntimeDir = filepath.Join(r.dir, "nope")
			return "does not exist"
		},
		"directory writable by others": func(t *testing.T, r *rig) string {
			t.Helper()
			if err := os.Chmod(r.dir, 0o720); err != nil {
				t.Fatal(err)
			}
			return "writable by others"
		},
		"directory is a symlink": func(t *testing.T, r *rig) string {
			t.Helper()
			link := r.dir + "-link"
			if err := os.Symlink(r.dir, link); err != nil {
				t.Fatal(err)
			}
			r.w.RuntimeDir = link
			return "not a real directory"
		},
		"lock is a symlink": func(t *testing.T, r *rig) string {
			t.Helper()
			if err := os.Symlink(filepath.Join(r.dir, "elsewhere"), filepath.Join(r.dir, Name)); err != nil {
				t.Fatal(err)
			}
			return "is a symlink"
		},
		"lock is a directory": func(t *testing.T, r *rig) string {
			t.Helper()
			if err := os.Mkdir(filepath.Join(r.dir, Name), 0o700); err != nil {
				t.Fatal(err)
			}
			return "cannot be opened"
		},
		"directory owned by another uid": func(_ *testing.T, r *rig) string {
			r.w.UID = os.Getuid() + 1
			return "is owned by uid"
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			want := setup(t, r)
			_, err := r.w.Take(t.Context(), "x", time.Hour, nil)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("got %v, want an error containing %q", err, want)
			}
			if errors.As(err, new(*TimeoutError)) {
				t.Errorf("an unsafe lock is reported as busy: %v", err)
			}
		})
	}
}

func TestTakeCreatesTheLockFileOwnerOnly(t *testing.T) {
	r := newRig(t)
	lock, err := r.w.Take(t.Context(), "x", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if relErr := lock.Release(); relErr != nil {
			t.Error(relErr)
		}
	}()
	fi, err := os.Stat(filepath.Join(r.dir, Name))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", fi.Mode().Perm())
	}
}

func TestDelegationIsTheDescriptorTheChildSeesAndTheVariableNamingIt(t *testing.T) {
	r := newRig(t)
	lock, err := r.w.Take(t.Context(), "x", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := lock.Release(); err != nil {
			t.Error(err)
		}
	}()
	files, env := lock.Delegate()
	if len(files) != 1 || files[0] == nil {
		t.Fatalf("files %v", files)
	}
	if len(env) != 1 || env[0] != "FEDORA_DESKTOP_PLAY_LOCK_FD=3" {
		t.Errorf("env %v", env)
	}
	var released Lock
	if f, e := released.Delegate(); f != nil || e != nil {
		t.Errorf("a released lock delegates %v %v", f, e)
	}
}

func TestParseNoteKeepsWhatItCannotRead(t *testing.T) {
	if h := ParseNote(""); h != (Holder{}) {
		t.Errorf("empty: %+v", h)
	}
	h := ParseNote("written by something else\n")
	if h.Note != "written by something else" || h.PID != 0 || h.What != "" {
		t.Errorf("odd note: %+v", h)
	}
	h = ParseNote("pid=12 what=a b c since=bad\n")
	if h.PID != 12 || h.What != "a b c" || !h.Since.IsZero() {
		t.Errorf("bad stamp: %+v", h)
	}
}

func TestHolderDescribesItselfForAPerson(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 3, 30, 0, time.UTC)
	cases := map[string]struct {
		h    Holder
		want string
	}{
		"full":     {ParseNote(basicNote), "run.bash playbooks/imports/play-basic-configs.yml (pid 999), held since 11:59:20 UTC, for 4 minutes"},
		"one":      {Holder{PID: 1, What: "w", Since: now.Add(-70 * time.Second)}, "w (pid 1), held since 12:02:20 UTC, for 1 minute"},
		"seconds":  {Holder{PID: 1, What: "w", Since: now.Add(-20 * time.Second)}, "w (pid 1), held since 12:03:10 UTC, for less than a minute"},
		"no stamp": {Holder{PID: 1, What: "w"}, "w (pid 1)"},
		"odd":      {ParseNote("something\n"), "something"},
		"nothing":  {Holder{}, "a play run that has not said what it is"},
	}
	for name, c := range cases {
		if got := c.h.Describe(now); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

func TestSecondsIsForAWaitThatHasJustEnded(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0: "0 seconds", time.Second: "1 second", 59 * time.Second: "59 seconds", 61 * time.Second: "1 minute",
		-time.Second: "0 seconds",
	} {
		if got := Seconds(d); got != want {
			t.Errorf("Seconds(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestRuntimeDirIsTheSessionsElseTheUsersRunDirectory(t *testing.T) {
	if got := RuntimeDir(func(string) string { return "/run/user/1000" }, 1000); got != "/run/user/1000" {
		t.Errorf("got %s", got)
	}
	if got := RuntimeDir(func(string) string { return "" }, 1000); got != "/run/user/1000" {
		t.Errorf("got %s", got)
	}
	if got := RuntimeDir(func(string) string { return "/custom" }, 7); got != "/custom" {
		t.Errorf("got %s", got)
	}
}
