package host

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/config"
)

func newHost(t *testing.T) *Host {
	t.Helper()
	root := t.TempDir()
	lib := filepath.Join(root, "lib")
	for _, path := range []string{filepath.Join(lib, "tmux-session.bash"), filepath.Join(root, "helpers", "agent_bus", "declare.py")} {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	env := map[string]string{"CCY_LIB": lib, "HOME": filepath.Join(root, "home")}
	getenv := func(k string) string { return env[k] }
	h := &Host{
		Config: config.Load(root, getenv),
		Runner: &execx.Fake{}, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Getenv: getenv, Stdin: &bytes.Buffer{},
		InContainer: func() bool { return false },
	}
	h.Wire()
	return h
}

// said is what the host wrote to stderr.
func said(h *Host) *bytes.Buffer {
	buf, ok := h.Stderr.(*bytes.Buffer)
	if !ok {
		panic("the test host's stderr is a buffer")
	}
	return buf
}

func TestFreshTermsAreNotShared(t *testing.T) {
	h := newHost(t)
	if a, b := h.FreshTerm(), h.FreshTerm(); a == b || a.Err != h.Stderr {
		t.Fatal("a fresh Term is its own, over the host's stderr")
	}
}

func TestFreshTermsAskTheHostWhatIsATerminal(t *testing.T) {
	h := newHost(t)
	h.IsTerminal = func(any) bool { return true }
	if !h.FreshTerm().IsTerminal(h.Stdin) {
		t.Fatal("the host's terminal rule is the Term's")
	}
}

func TestWireFillsOnlyWhatIsMissing(t *testing.T) {
	h := &Host{Runner: &execx.Fake{}}
	h.Wire()
	built := map[string]bool{
		"Term": h.Term != nil, "Sudo": h.Sudo != nil, "Systemd": h.Systemd != nil, "Tmux": h.Tmux != nil,
		"Git": h.Git != nil, "Declare": h.Declare != nil, "Bus": h.Bus != nil, "Registry": h.Registry != nil,
		"Getenv": h.Getenv != nil, "Gum": h.Gum != nil, "Picker": h.Picker != nil, "Stdout": h.Stdout != nil,
		"Stderr": h.Stderr != nil, "Stdin": h.Stdin != nil, "Out": h.Out != nil, "LookPath": h.LookPath != nil,
		"OpenTTY": h.OpenTTY != nil, "Exec": h.Exec != nil, "InContainer": h.InContainer != nil,
		"IsTerminal": h.IsTerminal != nil,
	}
	for name, ok := range built {
		if !ok {
			t.Errorf("%s was left unset", name)
		}
	}
	kept := h.Git
	h.Wire()
	if h.Git != kept {
		t.Fatal("Wire replaced an adapter that was already set")
	}
}

func TestReportErrPrintsTheReasonAsItWasSaid(t *testing.T) {
	h := newHost(t)
	h.ReportErr(stringError("declare: nope"))
	if said(h).String() != "declare: nope\n" {
		t.Fatalf("stderr %q", said(h).String())
	}
}

type stringError string

func (e stringError) Error() string { return string(e) }

func TestPrintKeepsTheFirstFailedWrite(t *testing.T) {
	h := newHost(t)
	h.Print(failing{}, "x")
	if h.Out.Err() == nil {
		t.Fatal("a failed write was forgotten")
	}
}

type failing struct{}

func (failing) Write([]byte) (int, error) { return 0, os.ErrClosed }

func TestCcyInstalledRefusesWithoutTheLibrary(t *testing.T) {
	h := newHost(t)
	if !h.CcyInstalled() || said(h).Len() != 0 {
		t.Fatalf("installed: stderr %q", said(h).String())
	}
	h.CcyLib = filepath.Join(t.TempDir(), "absent")
	if h.CcyInstalled() {
		t.Fatal("a missing library is a refusal")
	}
	if !strings.Contains(said(h).String(), "CCY is not installed here ("+h.CcyLib+"/tmux-session.bash missing)") {
		t.Fatalf("stderr %q", said(h).String())
	}
	said(h).Reset()
	h.InContainer = func() bool { return true }
	if h.CcyInstalled() || said(h).String() != "ccy-teams runs on the host, not inside a CCY container.\n" {
		t.Fatalf("stderr %q", said(h).String())
	}
}

func TestNeedCheckout(t *testing.T) {
	h := newHost(t)
	if err := h.NeedCheckout(); err != nil {
		t.Fatal(err)
	}
	h.RootDir = filepath.Join(t.TempDir(), "gone")
	if err := h.NeedCheckout(); err == nil {
		t.Fatal("a checkout with no declare.py is refused")
	}
	if !strings.Contains(said(h).String(), "has gone; re-run playbooks/imports/play-claude-yolo.yml") {
		t.Fatalf("stderr %q", said(h).String())
	}
}

func TestHereIsPWDThenTheWorkingDirectory(t *testing.T) {
	h := newHost(t)
	h.Getenv = func(k string) string {
		if k == "PWD" {
			return "/some/where"
		}
		return ""
	}
	if got := h.Here(); got != "/some/where" {
		t.Fatalf("got %q", got)
	}
	h.Getenv = func(string) string { return "" }
	want, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got := h.Here(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLauncherViaPassesTheDirectoryToTheExecerAndLeavesTheProcessCwdAlone(t *testing.T) {
	before := t.TempDir()
	t.Chdir(before)
	dir := t.TempDir()
	f := &execx.Fake{}
	if err := launcherVia(f, "/top/ccy", []string{"ccy", "--teams", "a"}, dir); err != nil {
		t.Fatal(err)
	}
	execs := f.Execs()
	if len(execs) != 1 || execs[0].Name != "/top/ccy" || strings.Join(execs[0].Args, " ") != "--teams a" || execs[0].Dir != dir {
		t.Fatalf("execs %+v", execs)
	}
	want, err := filepath.EvalSymlinks(before)
	if err != nil {
		t.Fatal(err)
	}
	if cwd, err := os.Getwd(); err != nil || cwd != want {
		t.Fatalf("cwd %q, %v; the caller must not chdir", cwd, err)
	}
}

func TestLauncherViaReportsAFailedExec(t *testing.T) {
	f := &execx.Fake{}
	f.ExecError(&execx.NotInstalledError{Name: "ccy"})
	if err := launcherVia(f, "ccy", nil, t.TempDir()); err == nil || !strings.Contains(err.Error(), "start the launcher") {
		t.Fatalf("got %v", err)
	}
}

func TestTheProductionHostIsTheRealSystem(t *testing.T) {
	h := New(config.Config{RootDir: "/checkout"})
	if h.Runner == nil || h.Stdout != os.Stdout || h.Stderr != os.Stderr || h.Stdin != os.Stdin {
		t.Fatalf("host %+v", h)
	}
}
