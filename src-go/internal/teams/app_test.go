package teams

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestLauncherViaChangesDirectoryThenReplacesTheProcessThroughTheExecer(t *testing.T) {
	t.Chdir(t.TempDir()) // registers the restore of the working directory launcherVia changes
	dir := t.TempDir()
	f := &execx.Fake{}
	if err := launcherVia(f, "/top/ccy", []string{"ccy", "--teams", "a"}, dir); err != nil {
		t.Fatal(err)
	}
	execs := f.Execs()
	if len(execs) != 1 || execs[0].Name != "/top/ccy" || strings.Join(execs[0].Args, " ") != "--teams a" {
		t.Fatalf("execs %+v", execs)
	}
	if cwd, err := os.Getwd(); err != nil || cwd != dir {
		t.Fatalf("cwd %q, %v", cwd, err)
	}
}

func TestLauncherViaReportsAnUnenterableDirectoryAndAFailedExec(t *testing.T) {
	t.Chdir(t.TempDir())
	f := &execx.Fake{}
	if err := launcherVia(f, "ccy", []string{"ccy"}, filepath.Join(t.TempDir(), "absent")); err == nil || len(f.Execs()) != 0 {
		t.Fatalf("got %v, execs %v", err, f.Execs())
	}
	f.ExecError(&execx.NotInstalledError{Name: "ccy"})
	if err := launcherVia(f, "ccy", nil, t.TempDir()); err == nil || !strings.Contains(err.Error(), "start the launcher") {
		t.Fatalf("got %v", err)
	}
}

type harness struct {
	app    *App
	out    *bytes.Buffer
	errOut *bytes.Buffer
	fake   *execx.Fake
	env    map[string]string
	root   string
}

// newHarness is an App over a scratch root with an installed ccy lib and the declare helper
// present, so dispatch reaches the subcommand.
func newHarness(t *testing.T) harness {
	t.Helper()
	root := t.TempDir()
	lib := filepath.Join(root, "lib")
	mustWrite(t, filepath.Join(lib, "tmux-session.bash"), "")
	mustWrite(t, filepath.Join(root, "helpers", "agent_bus", "declare.py"), "")
	h := harness{out: &bytes.Buffer{}, errOut: &bytes.Buffer{}, fake: &execx.Fake{}, root: root}
	h.env = map[string]string{"CCY_LIB": lib, "HOME": filepath.Join(root, "home")}
	getenv := func(k string) string { return h.env[k] }
	h.app = &App{
		Config: LoadConfig(root, getenv),
		Runner: h.fake, Stdout: h.out, Stderr: h.errOut, Getenv: getenv, Stdin: &bytes.Buffer{},
		inContainer: func() bool { return false },
	}
	h.app.AppliedDir = filepath.Join(root, "applied")
	h.app.wire()
	return h
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestHelpPrintsTheUsageToStdout(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		h := newHarness(t)
		if code := h.app.Run([]string{flag}); code != term.ExitOK {
			t.Fatalf("%s: exit %d", flag, code)
		}
		if !strings.Contains(h.out.String(), "ccy-teams -l|--list") || strings.Contains(h.out.String(), "set -euo") {
			t.Fatalf("%s: usage %q", flag, h.out.String())
		}
	}
}

func TestUsageErrors(t *testing.T) {
	cases := map[string][]string{
		"ccy-teams: unknown option '--nope'. See ccy-teams --help.\n":             {"--nope"},
		"ccy-teams: '--list' takes no further arguments. See ccy-teams --help.\n": {"--list", "x"},
		"ccy-teams: '-l' takes no further arguments. See ccy-teams --help.\n":     {"-l", "x"},
	}
	for want, args := range cases {
		h := newHarness(t)
		if code := h.app.Run(args); code != term.ExitUsage {
			t.Fatalf("%v: exit %d", args, code)
		}
		if h.errOut.String() != want {
			t.Fatalf("%v: stderr %q", args, h.errOut.String())
		}
	}
}

func TestMissingCcyLibRefuses(t *testing.T) {
	h := newHarness(t)
	h.app.CcyLib = filepath.Join(t.TempDir(), "absent")
	if code := h.app.Run([]string{"--list"}); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.errOut.String(), "CCY is not installed here ("+h.app.CcyLib+"/tmux-session.bash missing)") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
	h = newHarness(t)
	h.app.CcyLib = filepath.Join(t.TempDir(), "absent")
	h.app.inContainer = func() bool { return true }
	h.app.Run([]string{"--list"})
	if h.errOut.String() != "ccy-teams runs on the host, not inside a CCY container.\n" {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestNeedCheckout(t *testing.T) {
	h := newHarness(t)
	if err := h.app.needCheckout(); err != nil {
		t.Fatal(err)
	}
	h.app.RootDir = filepath.Join(t.TempDir(), "gone")
	if err := h.app.needCheckout(); err == nil {
		t.Fatal("a checkout with no declare.py is refused")
	}
	if !strings.Contains(h.errOut.String(), "has gone; re-run playbooks/imports/play-claude-yolo.yml") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}
