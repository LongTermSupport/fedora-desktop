package ccc

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	testToken = "sk-ant-oat01-" + "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJ"
	labelFlag = `{{ index .Config.Labels "claude-yolo-version" }}`
)

var testNow = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

// fakeGuard records the session it is given and answers with code.
type fakeGuard struct {
	cmds []execx.Cmd
	code int
	err  error
}

func (g *fakeGuard) Run(c execx.Cmd) (execx.Exit, error) {
	g.cmds = append(g.cmds, c)
	return execx.Exit{Code: g.code}, g.err
}

type world struct {
	app    *App
	run    *execx.Fake
	guard  *fakeGuard
	home   string
	stdout *bytes.Buffer
	stderr *bytes.Buffer
}

func newWorld(t *testing.T) *world {
	t.Helper()
	home := t.TempDir()
	run := &execx.Fake{}
	run.On([]string{"git"}, execx.Result{})
	run.On([]string{"podman", "image", "inspect"}, execx.Result{Stdout: []byte("2.79.15\n")})
	w := &world{run: run, guard: &fakeGuard{}, home: home, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	w.app = &App{
		Runner:          run,
		Foreground:      run,
		Guard:           w.guard,
		Stdin:           strings.NewReader(""),
		Stdout:          w.stdout,
		Stderr:          w.stderr,
		IsTerminal:      func(any) bool { return true },
		Home:            home,
		Now:             func() time.Time { return testNow },
		User:            Identity{Login: "pat", FullName: "Pat Example"},
		TimeZone:        "Europe/London",
		RequiredVersion: "2.79.15",
	}
	return w
}

func (w *world) putToken(t *testing.T, name string) {
	t.Helper()
	dir := poolDir(w.home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(testToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (w *world) gitArgs() [][]string {
	var got [][]string
	for _, c := range w.run.Calls() {
		if c.Name == "git" {
			got = append(got, c.Args)
		}
	}
	return got
}

func TestHelpPrintsUsageOnStdout(t *testing.T) {
	w := newWorld(t)
	if code := w.app.Run(context.Background(), []string{"--help"}); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.stdout.String(), "usage: ccc") || len(w.run.Calls()) != 0 {
		t.Fatalf("stdout %q, calls %v", w.stdout, w.run.Calls())
	}
}

func TestUsageErrorsRunNothing(t *testing.T) {
	for _, args := range [][]string{{"--bogus"}, {"--token"}, {"--token", "a b"}, {"--claude", "--claude"}, {"extra"}} {
		w := newWorld(t)
		if code := w.app.Run(context.Background(), args); code != term.ExitUsage {
			t.Fatalf("%v: exit %d", args, code)
		}
		if len(w.run.Calls()) != 0 {
			t.Fatalf("%v: ran %v", args, w.run.Calls())
		}
	}
}

func TestRefusesWithoutATerminal(t *testing.T) {
	w := newWorld(t)
	w.app.IsTerminal = func(any) bool { return false }
	if code := w.app.Run(context.Background(), nil); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.stderr.String(), "needs a terminal") {
		t.Fatalf("stderr %q", w.stderr)
	}
}

func TestASetupFailureStopsTheRun(t *testing.T) {
	w := newWorld(t)
	w.app.SetupErr = errors.New("no home directory")
	if code := w.app.Run(context.Background(), nil); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.stderr.String(), "no home directory") || len(w.run.Calls()) != 0 {
		t.Fatalf("stderr %q", w.stderr)
	}
}

func TestRefusesABuildWithoutTheRequiredVersion(t *testing.T) {
	w := newWorld(t)
	w.app.RequiredVersion = ""
	if code := w.app.Run(context.Background(), nil); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.stderr.String(), Play) {
		t.Fatalf("stderr %q", w.stderr)
	}
}

func TestFirstRunMakesTheFolderAndStartsClaudeThere(t *testing.T) {
	w := newWorld(t)
	w.putToken(t, "ccc.2027-06-01.token")
	if code := w.app.Run(context.Background(), nil); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.stderr)
	}
	dir := filepath.Join(w.home, FolderName)
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("folder: %v %v", info, err)
	}
	for _, f := range []string{".gitignore", "CLAUDE.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
	}
	if len(w.guard.cmds) != 1 {
		t.Fatalf("sessions %d", len(w.guard.cmds))
	}
	c := w.guard.cmds[0]
	if c.Name != "podman" || !slices.Contains(c.Args, dir+":/workspace:z") {
		t.Fatalf("session %v", c.Args)
	}
	if len(c.Secrets) != 1 || c.Secrets[0].Name() != "CLAUDE_CODE_OAUTH_TOKEN" || !c.Secrets[0].Is(testToken) {
		t.Fatalf("secrets %v", c.Secrets)
	}
	if got := w.gitArgs(); len(got) < 5 || got[0][0] != "init" {
		t.Fatalf("git %v", got)
	}
}

func TestSessionEndCommitsWhatChanged(t *testing.T) {
	w := newWorld(t)
	w.putToken(t, "ccc.2027-06-01.token")
	w.run.On([]string{"git", "status"}, execx.Result{Stdout: []byte("?? notes.txt\n")})
	w.guard.code = 3
	if code := w.app.Run(context.Background(), nil); code != 3 {
		t.Fatalf("exit %d, stderr %q", code, w.stderr)
	}
	got := w.gitArgs()
	last := got[len(got)-1]
	if !slices.Contains(last, "commit") || !slices.Contains(last, "ccc session 2026-10-27 12:00") {
		t.Fatalf("last git %v", last)
	}
}

func TestSessionWithNoChangeMakesNoCommit(t *testing.T) {
	w := newWorld(t)
	w.putToken(t, "ccc.2027-06-01.token")
	if code := w.app.Run(context.Background(), nil); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	got := w.gitArgs()
	if last := got[len(got)-1]; last[0] != "status" {
		t.Fatalf("last git %v", last)
	}
}

func TestAFailedSnapshotFailsTheRun(t *testing.T) {
	w := newWorld(t)
	w.putToken(t, "ccc.2027-06-01.token")
	w.run.On([]string{"git", "status"}, execx.Result{Stdout: []byte(" M a\n")})
	w.run.On([]string{"git", "add"}, execx.Result{ExitCode: 128, Stderr: []byte("fatal: index.lock")})
	if code := w.app.Run(context.Background(), nil); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.stderr.String(), "index.lock") {
		t.Fatalf("stderr %q", w.stderr)
	}
}

func TestAGuardThatCannotStartFails(t *testing.T) {
	w := newWorld(t)
	w.putToken(t, "ccc.2027-06-01.token")
	w.guard.err = errors.New("no pty")
	if code := w.app.Run(context.Background(), nil); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
}

func TestMissingImageNamesThePlay(t *testing.T) {
	w := newWorld(t)
	w.run.On([]string{"podman", "image", "inspect"}, execx.Result{ExitCode: 125, Stderr: []byte("Error: claude-yolo:latest: image not known")})
	if code := w.app.Run(context.Background(), nil); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.stderr.String(), Play) || len(w.guard.cmds) != 0 {
		t.Fatalf("stderr %q", w.stderr)
	}
}

func TestAnImageQuestionPodmanCannotAnswerFails(t *testing.T) {
	w := newWorld(t)
	w.run.On([]string{"podman", "image", "inspect"}, execx.Result{ExitCode: 125, Stderr: []byte("Error: storage is corrupt")})
	if code := w.app.Run(context.Background(), nil); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.stderr.String(), "storage is corrupt") {
		t.Fatalf("stderr %q", w.stderr)
	}
}

func TestAStateDirectoryThatCannotBeMadeFails(t *testing.T) {
	w := newWorld(t)
	w.putToken(t, "ccc.2027-06-01.token")
	if err := os.MkdirAll(filepath.Join(w.home, ".local", "share"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.home, ".local", "share", "ccc"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := w.app.Run(context.Background(), nil); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
}

func TestAnOlderImageNamesThePlay(t *testing.T) {
	for _, label := range []string{"2.79.14", "", "junk"} {
		w := newWorld(t)
		w.run.On([]string{"podman", "image", "inspect"}, execx.Result{Stdout: []byte(label + "\n")})
		if code := w.app.Run(context.Background(), nil); code != term.ExitFailure {
			t.Fatalf("%q: exit %d", label, code)
		}
		if !strings.Contains(w.stderr.String(), Play) {
			t.Fatalf("%q: stderr %q", label, w.stderr)
		}
	}
}

func TestANewerImageIsAccepted(t *testing.T) {
	w := newWorld(t)
	w.putToken(t, "ccc.2027-06-01.token")
	w.run.On([]string{"podman", "image", "inspect"}, execx.Result{Stdout: []byte("2.80.0\n")})
	if code := w.app.Run(context.Background(), nil); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.stderr)
	}
}

func TestContinueResumesTheLastConversation(t *testing.T) {
	w := newWorld(t)
	w.putToken(t, "ccc.2027-06-01.token")
	if code := w.app.Run(context.Background(), []string{"--continue"}); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if args := w.guard.cmds[0].Args; args[len(args)-1] != "--continue" {
		t.Fatalf("args %v", args)
	}
}

func TestNamedTokenIsUsedAndNeverMade(t *testing.T) {
	w := newWorld(t)
	w.putToken(t, "work.2027-06-01.token")
	if code := w.app.Run(context.Background(), []string{"--token", "work"}); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.stderr)
	}
	w2 := newWorld(t)
	if code := w2.app.Run(context.Background(), []string{"--token", "work"}); code != term.ExitFailure {
		t.Fatalf("missing named token: exit %d", code)
	}
	if len(w2.run.Foregrounds()) != 0 {
		t.Fatal("a sign-in was started for a named token")
	}
}

func TestStateIsPreparedOutsideTheFolder(t *testing.T) {
	w := newWorld(t)
	w.putToken(t, "ccc.2027-06-01.token")
	if code := w.app.Run(context.Background(), nil); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	state := filepath.Join(w.home, ".local/share/ccc/claude")
	if _, err := os.Stat(filepath.Join(state, ".claude.json")); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(w.guard.cmds[0].Args, state+":/var/lib/ccc:z") {
		t.Fatalf("args %v", w.guard.cmds[0].Args)
	}
}

func TestAFolderThatCannotBePreparedFails(t *testing.T) {
	w := newWorld(t)
	if err := os.WriteFile(filepath.Join(w.home, FolderName), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := w.app.Run(context.Background(), nil); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
}
