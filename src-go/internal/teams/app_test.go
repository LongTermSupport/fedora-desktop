package teams

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

type harness struct {
	app    *App
	out    *bytes.Buffer
	errOut *bytes.Buffer
}

// newHarness is an App over a scratch root with an installed ccy lib and the declare helper
// present, so dispatch reaches the subcommand.
func newHarness(t *testing.T) harness {
	t.Helper()
	root := t.TempDir()
	lib := filepath.Join(root, "lib")
	mustWrite(t, filepath.Join(lib, "tmux-session.bash"), "")
	mustWrite(t, filepath.Join(root, "helpers", "agent_bus", "declare.py"), "")
	h := harness{out: &bytes.Buffer{}, errOut: &bytes.Buffer{}}
	env := map[string]string{"CCY_LIB": lib, "HOME": filepath.Join(root, "home")}
	h.app = &App{
		Config: LoadConfig(root, func(k string) string { return env[k] }),
		Runner: &execx.Fake{}, Stdout: h.out, Stderr: h.errOut,
		inContainer: func() bool { return false },
	}
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
		if code := h.app.Run([]string{flag}); code != ExitOK {
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
		if code := h.app.Run(args); code != ExitUsage {
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
	if code := h.app.Run([]string{"--list"}); code != ExitFailure {
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
