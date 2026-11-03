package admin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// stepClock is a clock whose Sleep moves it on, so a lock wait ends.
type stepClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *stepClock) Sleep(_ context.Context, d time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	return nil
}

// fakeRestorer is the engine: it records each call, checks the lock is held, and answers out
// and err.
type fakeRestorer struct {
	t     *testing.T
	sys   *System
	calls []string
	out   []byte
	err   error
}

func (f *fakeRestorer) Run(ctx context.Context, lock *teamlock.Lock, req RestoreRequest) ([]byte, error) {
	f.held(ctx, req.Team, lock)
	f.calls = append(f.calls, fmt.Sprintf("run %s %d offline=%t from-backup=%t", req.Team, req.Backup, req.Offline, req.RosterFromBackup))
	return f.out, f.err
}

func (f *fakeRestorer) Resume(ctx context.Context, lock *teamlock.Lock, team paths.TeamID) ([]byte, error) {
	f.held(ctx, team, lock)
	f.calls = append(f.calls, "resume "+team.String())
	return f.out, f.err
}

func (f *fakeRestorer) Discard(ctx context.Context, lock *teamlock.Lock, team paths.TeamID) ([]byte, error) {
	f.held(ctx, team, lock)
	f.calls = append(f.calls, "discard "+team.String())
	return f.out, f.err
}

func (f *fakeRestorer) held(ctx context.Context, team paths.TeamID, lock *teamlock.Lock) {
	f.t.Helper()
	if lock == nil {
		f.t.Fatal("the engine was given no lock")
	}
	_, err := teamlock.Take(ctx, f.sys.Host.LockDir, team, "probe", 0, f.sys.Host.Clock, teamlock.Unguarded)
	if !errors.Is(err, teamlock.ErrBusy) {
		f.t.Fatalf("the team lock is not held during the engine: %v", err)
	}
}

type restoreWorld struct {
	t       *testing.T
	root    string
	sys     *System
	engine  *fakeRestorer
	engines int
	seamErr error
}

func newRestoreWorld(t *testing.T) *restoreWorld {
	t.Helper()
	w := &restoreWorld{t: t, root: t.TempDir()}
	team := alpha(t)
	for _, dir := range []string{paths.AdminState(team), paths.CtlState(team), paths.AdminRunDir} {
		if err := os.MkdirAll(w.root+dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	w.sys = &System{
		Host:   host.Deps{Root: w.root, LockDir: w.root + paths.AdminRunDir, Clock: &stepClock{now: time.UnixMilli(t0ms)}},
		CtlIDs: func() (User, error) { return User{UID: os.Getuid(), GID: os.Getgid()}, nil },
	}
	w.engine = &fakeRestorer{t: t, sys: w.sys, out: []byte(`{"v":1,"team":"alpha"}`)}
	w.sys.Restore = func() (Restorer, error) {
		w.engines++
		if w.seamErr != nil {
			return nil, w.seamErr
		}
		return w.engine, nil
	}
	return w
}

func (w *restoreWorld) verb(args ...string) outcome {
	w.t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(w.t.Context(), Env{
		Args: append([]string{"restore"}, args...), Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr,
		Proc: rootProc(), Digest: digest, Sys: w.sys,
	})
	return outcome{code, stdout.String(), stderr.String()}
}

func (w *restoreWorld) record() {
	w.t.Helper()
	if err := os.WriteFile(w.root+paths.RestoreRecord(alpha(w.t)), []byte("{}"), 0o600); err != nil {
		w.t.Fatal(err)
	}
}

// hold takes alpha's lock as another verb would, for the test's length.
func (w *restoreWorld) hold() {
	w.t.Helper()
	lock, err := teamlock.Take(w.t.Context(), w.sys.Host.LockDir, alpha(w.t), "team.apply", 0, w.sys.Host.Clock, teamlock.Unguarded)
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() {
		if err := lock.Release(); err != nil {
			w.t.Error(err)
		}
	})
}

// RS15, RO2, BV4: every other combination of the flags is 64, with nothing asked of the engine.
func TestRestoreUsage(t *testing.T) {
	for name, args := range map[string][]string{
		"nothing":                     nil,
		"no team":                     {"--backup", "7"},
		"a bad team":                  {"--team", "Alpha!", "--backup", "7"},
		"no backup":                   {"--team", "alpha"},
		"backup zero":                 {"--team", "alpha", "--backup", "0"},
		"backup negative":             {"--team", "alpha", "--backup", "-1"},
		"backup leading zero":         {"--team", "alpha", "--backup", "07"},
		"backup signed":               {"--team", "alpha", "--backup", "+7"},
		"backup not a number":         {"--team", "alpha", "--backup", "seven"},
		"backup too large":            {"--team", "alpha", "--backup", "9007199254740992"},
		"backup given twice":          {"--team", "alpha", "--backup", "7", "--backup", "8"},
		"resume and discard":          {"--team", "alpha", "--resume", "--discard"},
		"resume and lost":             {"--team", "alpha", "--resume", "--lost", "--backup", "7"},
		"discard and lost":            {"--team", "alpha", "--discard", "--lost", "--backup", "7"},
		"resume with a backup":        {"--team", "alpha", "--resume", "--backup", "7"},
		"resume offline":              {"--team", "alpha", "--resume", "--offline"},
		"discard offline":             {"--team", "alpha", "--discard", "--offline"},
		"discard from the backup":     {"--team", "alpha", "--discard", "--roster-from-backup"},
		"resume from the backup":      {"--team", "alpha", "--resume", "--roster-from-backup"},
		"lost with no backup":         {"--team", "alpha", "--lost"},
		"lost offline":                {"--team", "alpha", "--lost", "--backup", "7", "--offline"},
		"lost from the backup":        {"--team", "alpha", "--lost", "--backup", "7", "--roster-from-backup"},
		"an argument":                 {"--team", "alpha", "--backup", "7", "now"},
		"an unknown flag":             {"--team", "alpha", "--backup", "7", "--force"},
		"a bool given a value":        {"--team", "alpha", "--backup", "7", "--offline", "yes"},
		"offline with no backup":      {"--team", "alpha", "--offline"},
		"from the backup, no backup":  {"--team", "alpha", "--roster-from-backup"},
		"a team with no value":        {"--team", "--backup", "7"},
		"a backup with no value":      {"--team", "alpha", "--backup"},
		"the team given twice":        {"--team", "alpha", "--team", "alpha", "--backup", "7"},
		"resume given twice":          {"--team", "alpha", "--resume", "--resume"},
		"lost and a mode and offline": {"--team", "alpha", "--lost", "--backup", "7", "--resume", "--offline"},
	} {
		w := newRestoreWorld(t)
		got := w.verb(args...)
		if got.code != term.ExitUsage || got.stdout != "" || !strings.HasPrefix(got.stderr, "teamchat-admin: ") {
			t.Errorf("%s: %+v", name, got)
		}
		if w.engines != 0 || len(w.engine.calls) != 0 {
			t.Errorf("%s: the engine was reached", name)
		}
	}
}

// RS1-RS13: a fresh restore, a resume and a discard each run the engine under the team lock,
// and the engine's answer is the one line on stdout.
func TestRestoreRunsTheEngineUnderTheLock(t *testing.T) {
	cases := []struct {
		args   []string
		record bool
		want   string
	}{
		{[]string{"--team", "alpha", "--backup", "7"}, false, "run alpha 7 offline=false from-backup=false"},
		{[]string{"--backup", "9007199254740991", "--offline", "--roster-from-backup", "--team", "alpha"}, false, "run alpha 9007199254740991 offline=true from-backup=true"},
		{[]string{"--team", "alpha", "--resume"}, true, "resume alpha"},
		{[]string{"--team", "alpha", "--discard"}, true, "discard alpha"},
	}
	for _, c := range cases {
		w := newRestoreWorld(t)
		if c.record {
			w.record()
		}
		got := w.verb(c.args...)
		if got.code != term.ExitOK || got.stdout != `{"v":1,"team":"alpha"}`+"\n" || got.stderr != "" {
			t.Errorf("%v: %+v", c.args, got)
		}
		if !slices.Equal(w.engine.calls, []string{c.want}) {
			t.Errorf("%v: engine calls %v", c.args, w.engine.calls)
		}
		if _, err := teamlock.Take(t.Context(), w.sys.Host.LockDir, alpha(t), "probe", 0, w.sys.Host.Clock, teamlock.Unguarded); err != nil {
			t.Errorf("%v: the lock was not released: %v", c.args, err)
		}
	}
}

// RG3: a fresh restore is refused (70) while one is recorded; a resume or a discard with none
// recorded is refused (70) too. The engine is never reached.
func TestRestoreGuards(t *testing.T) {
	w := newRestoreWorld(t)
	w.record()
	got := w.verb("--team", "alpha", "--backup", "7")
	if got.code != ExitRefused || got.stdout != "" || !strings.Contains(got.stderr, "restore --team alpha --resume") {
		t.Errorf("fresh with a record: %+v", got)
	}
	for _, mode := range []string{"--resume", "--discard"} {
		w = newRestoreWorld(t)
		got = w.verb("--team", "alpha", mode)
		if got.code != ExitRefused || got.stdout != "" || !strings.Contains(got.stderr, "no restore of team alpha is recorded") {
			t.Errorf("%s with no record: %+v", mode, got)
		}
		if len(w.engine.calls) != 0 {
			t.Errorf("%s: the engine ran", mode)
		}
	}
}

// RS15: a busy lock is 75, after the wait.
func TestRestoreBusy(t *testing.T) {
	w := newRestoreWorld(t)
	w.hold()
	got := w.verb("--team", "alpha", "--backup", "7")
	if got.code != ExitBusy || got.stdout != "" || len(w.engine.calls) != 0 {
		t.Fatalf("busy: %+v, calls %v", got, w.engine.calls)
	}
}

// RW2, n2g-D8: an engine the wiring refuses is 70 before the lock is taken; no engine wired is
// the wiring's fault (78).
func TestRestoreSeam(t *testing.T) {
	w := newRestoreWorld(t)
	w.hold()
	w.seamErr = &restore.RefusedError{Problem: "restore is not enabled: host checks H-C5 and H-C15 have not passed (DC8)"}
	got := w.verb("--team", "alpha", "--backup", "7")
	if got.code != ExitRefused || got.stdout != "" || !strings.Contains(got.stderr, "restore is not enabled") {
		t.Errorf("unproven: %+v", got)
	}
	w = newRestoreWorld(t)
	w.sys.Restore = nil
	got = w.verb("--team", "alpha", "--resume")
	if got.code != ExitConfig || got.stdout != "" || !strings.Contains(got.stderr, "no restore engine") {
		t.Errorf("no engine: %+v", got)
	}
}

// RS8, RS15: the engine's failure decides the code (a refusal 70, a homeserver lost mid-step
// 69, anything else 1), and an answer beside the failure is still printed.
func TestRestoreEngineFailures(t *testing.T) {
	for name, c := range map[string]struct {
		err  error
		want term.ExitCode
	}{
		"refused":     {&restore.RefusedError{Problem: "backup 7 is below the floor"}, ExitRefused},
		"unreachable": {fmt.Errorf("the export: %w", matrix.ErrUnreachable), ExitUnreachable},
		"failed":      {errors.New("restored; clean-up failed: disk full; restore --team alpha --resume"), term.ExitFailure},
	} {
		w := newRestoreWorld(t)
		w.engine.err = c.err
		got := w.verb("--team", "alpha", "--backup", "7")
		if got.code != c.want || got.stdout != `{"v":1,"team":"alpha"}`+"\n" || !strings.Contains(got.stderr, c.err.Error()) {
			t.Errorf("%s: %+v", name, got)
		}
	}
	w := newRestoreWorld(t)
	w.engine.out, w.engine.err = nil, errors.New("restore not done")
	if got := w.verb("--team", "alpha", "--backup", "7"); got.code != term.ExitFailure || got.stdout != "" {
		t.Errorf("no answer: %+v", got)
	}
}

func (w *restoreWorld) lostFile(content string) {
	w.t.Helper()
	if err := os.WriteFile(w.root+paths.RestoreLostFile(alpha(w.t), 7), []byte(content), 0o600); err != nil {
		w.t.Fatal(err)
	}
}

// RS14: --lost prints the export one compact object a line, a control byte escaped; it takes
// no lock and reaches no engine.
func TestRestoreLost(t *testing.T) {
	w := newRestoreWorld(t)
	w.hold()
	w.sys.Restore = nil
	w.lostFile("{ \"sender\": \"@a:x\",  \"body\": \"hi\\u001b[2J\" }\n{\"n\": [1, 2]}\n")
	got := w.verb("--team", "alpha", "--lost", "--backup", "7")
	want := `{"sender":"@a:x","body":"hi\u001b[2J"}` + "\n" + `{"n":[1,2]}` + "\n"
	if got.code == term.ExitOK && got.stdout == want && got.stderr == "" {
		return
	}
	t.Fatalf("lost: %+v, want %q", got, want)
}

// RS14: the last line needs no newline, and an empty export prints nothing.
func TestRestoreLostEnds(t *testing.T) {
	for content, want := range map[string]string{"": "", `{"a":1}`: `{"a":1}` + "\n", "{\"a\":1}\n": `{"a":1}` + "\n"} {
		w := newRestoreWorld(t)
		w.lostFile(content)
		if got := w.verb("--team", "alpha", "--lost", "--backup", "7"); got.code != term.ExitOK || got.stdout != want {
			t.Errorf("%q: %+v", content, got)
		}
	}
}

// RS14: a line that is not one JSON object, an empty one included, fails the whole print (1), naming the line and
// quoting none of it; a missing export and one not the ctl account's are failures too.
func TestRestoreLostRefuses(t *testing.T) {
	for content, line := range map[string]string{
		"[1]\n":               "line 1",
		"{\"a\":1}\nsecret\n": "line 2",
		"{\"a\":1} {}\n":      "line 1",
		"\"secret\"\n":        "line 1",
		"{\"a\":1}\n\n{}\n":   "line 2",
		"\n":                  "line 1",
	} {
		w := newRestoreWorld(t)
		w.lostFile(content)
		got := w.verb("--team", "alpha", "--lost", "--backup", "7")
		if got.code != term.ExitFailure || got.stdout != "" || !strings.Contains(got.stderr, line) || strings.Contains(got.stderr, "secret") {
			t.Errorf("%q: %+v", content, got)
		}
	}
	w := newRestoreWorld(t)
	got := w.verb("--team", "alpha", "--lost", "--backup", "7")
	if got.code != term.ExitFailure || got.stdout != "" || !strings.Contains(got.stderr, "has no export of the events backup 7 lost") {
		t.Errorf("missing: %+v", got)
	}
	w = newRestoreWorld(t)
	w.lostFile("{}\n")
	w.sys.CtlIDs = func() (User, error) { return User{UID: os.Getuid() + 1}, nil }
	if got = w.verb("--team", "alpha", "--lost", "--backup", "7"); got.code != term.ExitFailure || got.stdout != "" {
		t.Errorf("another owner's export: %+v", got)
	}
	w = newRestoreWorld(t)
	w.sys.CtlIDs = func() (User, error) { return User{}, errors.New("no such account") }
	if got = w.verb("--team", "alpha", "--lost", "--backup", "7"); got.code != ExitConfig {
		t.Errorf("no ctl account: %+v", got)
	}
	w.sys.CtlIDs = func() (User, error) { return User{UID: -1}, nil }
	if got = w.verb("--team", "alpha", "--lost", "--backup", "7"); got.code != ExitConfig || !strings.Contains(got.stderr, "out of range") {
		t.Errorf("a ctl uid out of range: %+v", got)
	}
}

// BV4: restore, --lost included, is root's: the ctl account and anyone else get 77 with
// nothing read.
func TestRestoreIsRoots(t *testing.T) {
	for _, proc := range []*fakeProc{childProc(), ownerProc()} {
		for _, args := range [][]string{{"restore", "--team", "alpha", "--backup", "7"}, {"restore", "--team", "alpha", "--lost", "--backup", "7"}} {
			w := newRestoreWorld(t)
			var stdout, stderr bytes.Buffer
			code := Run(t.Context(), Env{Args: args, Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr, Proc: proc, Sys: w.sys})
			if code != ExitClass || stdout.Len() != 0 || w.engines != 0 {
				t.Errorf("%v by uid %d: %d %q %q", args, proc.uid, code, stdout.String(), stderr.String())
			}
		}
	}
}

// RS15: a restore's refusal is 70 by C 3.3's table.
func TestRestoreRefusedIsSeventy(t *testing.T) {
	if got := ExitCodeOf(fmt.Errorf("validation: %w", &restore.RefusedError{Problem: "no"})); got != ExitRefused {
		t.Fatalf("ExitCodeOf = %d", got)
	}
}

// RS15: a homeserver lost (matrix.ErrUnreachable, or kept rate-limiting) is 69 even beside a
// child's error, which alone is its own class's code.
func TestRestoreLostServerIsSixtyNine(t *testing.T) {
	child := &ChildError{Class: ClassFailed, Message: "the export: connection refused"}
	if got := ExitCodeOf(fmt.Errorf("%w: %w", matrix.ErrUnreachable, child)); got != ExitUnreachable {
		t.Fatalf("ExitCodeOf(unreachable child) = %d", got)
	}
	if got := ExitCodeOf(fmt.Errorf("%w: %w", matrix.ErrRateLimited, child)); got != ExitUnreachable {
		t.Fatalf("ExitCodeOf(rate-limited child) = %d", got)
	}
	if got := ExitCodeOf(child); got != term.ExitFailure {
		t.Fatalf("ExitCodeOf(child) = %d", got)
	}
}
