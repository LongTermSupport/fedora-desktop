package all

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/gates"
)

// lastEnv is the value a command's Env gives key, the last entry winning as in os/exec.
func lastEnv(env []string, key string) (string, bool) {
	for _, kv := range slices.Backward(env) {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			return v, true
		}
	}
	return "", false
}

// installedCopyWorld is the real table over a machine like the host that found the class: an
// installed ccy-token-usable on PATH beside a third-party tool. It answers the world and that
// PATH directory.
func installedCopyWorld(t *testing.T) (*world, string) {
	t.Helper()
	w := newRealWorld(t)
	installed := t.TempDir()
	for _, name := range []string{"ccy-token-usable", "semgrep"} {
		if err := os.WriteFile(filepath.Join(installed, name), []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range map[string]string{"scripts/go-binary-suites.tsv": "ccy-token-usable\tqa-go\n", "files/home/.local/bin/tool-x": ""} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(w.root, path)), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(w.root, path), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	w.env["PATH"] = installed + ":/usr/bin"
	return w, installed
}

// suite-env-isolated: every row of the real table runs in gates.Isolation, so its verdict is
// the code's and not the machine's.
func TestSuiteEnvIsolatedEveryRowOfTheRealTableRunsIsolated(t *testing.T) {
	w, installed := installedCopyWorld(t)
	if code := w.main(context.Background()); code != ExitOK {
		t.Fatalf("exit %d\nstderr:\n%s", code, w.errOut.String())
	}
	if len(w.runner.cmds) != len(tableOrder(t)) {
		t.Fatalf("ran %d commands, want one per row", len(w.runner.cmds))
	}
	for i, c := range w.runner.cmds {
		key := w.runner.log[i]
		if v, ok := lastEnv(c.Env, "LC_ALL"); !ok || v != gates.SuiteLocale {
			t.Errorf("suite-env-isolated: %s runs in the machine's locale (LC_ALL %q)", key, v)
		}
		if c.Drop == nil || !c.Drop("LANG") || !c.Drop("BASH_FUNC_which%%") {
			t.Errorf("suite-env-isolated: %s inherits the machine's LANG or exported functions", key)
		}
		path, ok := lastEnv(c.Env, "PATH")
		if !ok || strings.HasPrefix(path, installed+":") || !strings.HasSuffix(path, ":/usr/bin") {
			t.Errorf("suite-env-isolated: %s can reach an installed ccy-token-usable (PATH %q)", key, path)
		}
	}
}

// keepUmask is an Env.Umask that changes nothing, for a test that does not judge the umask.
func keepUmask(int) int { return machineUmask }

// suite-env-isolated: every row of the real table runs under gates.SuiteUmask, whatever umask
// the machine started the run with, so the modes a suite sees created are not the machine's.
func TestSuiteEnvIsolatedEveryRowRunsUnderTheSuiteUmask(t *testing.T) {
	w := newRealWorld(t)
	if code := w.main(context.Background()); code != ExitOK {
		t.Fatalf("exit %d\nstderr:\n%s", code, w.errOut.String())
	}
	if len(w.runner.masks) != len(tableOrder(t)) {
		t.Fatalf("ran %d commands, want one per row", len(w.runner.masks))
	}
	for i, mask := range w.runner.masks {
		if mask != gates.SuiteUmask {
			t.Errorf("suite-env-isolated: %s runs under the machine's umask %04o, not %04o", w.runner.log[i], mask, gates.SuiteUmask)
		}
	}
}

// A command with no way to set its umask cannot isolate its gates: it runs none of them.
func TestSuiteEnvIsolatedNoUmaskControlRunsNothing(t *testing.T) {
	w := newRealWorld(t)
	env := w.env0()
	env.Umask = nil
	if code := Main(context.Background(), []string{"--root", w.root}, env); code != ExitCannotRun {
		t.Fatalf("exit %d, want %d\nstderr:\n%s", code, ExitCannotRun, w.errOut.String())
	}
	if len(w.runner.log) != 0 {
		t.Fatalf("ran %v with no umask control", w.runner.log)
	}
	if !strings.Contains(w.errOut.String(), "no umask control is wired into this command") {
		t.Fatalf("stderr does not name the missing umask control:\n%s", w.errOut.String())
	}
}
