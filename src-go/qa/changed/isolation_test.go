package changed

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/gates"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// noEnv is the Getenv of a machine with an empty environment.
func noEnv(string) string { return "" }

// ownEnv is a command's Env without what gates.Isolation adds: the entries the check itself set.
func ownEnv(env []string) []string {
	return slices.DeleteFunc(slices.Clone(env), func(kv string) bool {
		return strings.HasPrefix(kv, "LC_ALL=") || strings.HasPrefix(kv, "PATH=")
	})
}

// lastEnv is the value a command's Env gives key, the last entry winning as in os/exec.
func lastEnv(env []string, key string) (string, bool) {
	for _, kv := range slices.Backward(env) {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			return v, true
		}
	}
	return "", false
}

// suite-env-isolated: every command qa-changed starts, its suites above all, runs in
// gates.Isolation, as under qa-all. The machine is the host that found the class: an
// installed ccy-token-usable on PATH.
func TestSuiteEnvIsolatedEveryCommandRunsIsolated(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	writeFile(t, w.scripts, "go-binary-suites.tsv", "ccy-token-usable\tqa-go\n")
	writeFile(t, w.root, "files/home/.local/bin/tool-x", "")
	installed := t.TempDir()
	writeFile(t, installed, "ccy-token-usable", "#!/bin/sh\n")
	w.env["PATH"] = installed + ":/usr/bin"
	if code := w.main(); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	suites := 0
	for _, c := range w.runner.calls {
		key := strings.Join(c.Argv, " ")
		if strings.HasPrefix(key, "bash "+filepath.Join(w.scripts, "test-")) {
			suites++
		}
		checkIsolated(t, key, c, installed)
	}
	if suites != 2 {
		t.Fatalf("ran %d suites, want both", suites)
	}
}

func checkIsolated(t *testing.T, key string, c proc.Cmd, installed string) {
	t.Helper()
	if v, ok := lastEnv(c.Env, "LC_ALL"); !ok || v != gates.SuiteLocale {
		t.Errorf("suite-env-isolated: %s runs in the machine's locale (LC_ALL %q)", key, v)
	}
	if c.Drop == nil || !c.Drop("LANG") || !c.Drop("BASH_FUNC_which%%") {
		t.Errorf("suite-env-isolated: %s inherits the machine's LANG or exported functions", key)
	}
	if path, ok := lastEnv(c.Env, "PATH"); !ok || strings.HasPrefix(path, installed+":") {
		t.Errorf("suite-env-isolated: %s can reach an installed ccy-token-usable (PATH %q)", key, path)
	}
}

// suite-env-isolated: every command qa-changed starts runs under gates.SuiteUmask, whatever
// umask the machine started the run with.
func TestSuiteEnvIsolatedEveryCommandRunsUnderTheSuiteUmask(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	if code := w.main(); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	if len(w.runner.masks) == 0 {
		t.Fatal("no command ran")
	}
	for i, mask := range w.runner.masks {
		if mask != gates.SuiteUmask {
			t.Errorf("suite-env-isolated: %s runs under the machine's umask %04o, not %04o", strings.Join(w.runner.calls[i].Argv, " "), mask, gates.SuiteUmask)
		}
	}
}

// A command with no way to set its umask cannot isolate its checks: it runs none of them.
func TestSuiteEnvIsolatedNoUmaskControlRunsNoCheck(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	env := Env{Getenv: noEnv, Stdout: &w.out, Stderr: &w.errOut, Runner: w.runner, LookPath: func(string) error { return nil }, Cwd: w.root, Lock: freeLock}
	if code := Main(context.Background(), []string{"--scripts", w.scripts, "--root", w.root}, env); code != ExitCannotRun {
		t.Fatalf("exit %d, want %d, stderr %q", code, ExitCannotRun, w.errOut.String())
	}
	for _, c := range w.runner.calls {
		if c.Argv[0] != "git" {
			t.Errorf("ran %q with no umask control", c.Argv)
		}
	}
	if !strings.Contains(w.errOut.String(), "no umask control is wired into this command") {
		t.Fatalf("stderr does not name the missing umask control: %q", w.errOut.String())
	}
}
