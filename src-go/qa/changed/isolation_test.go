package changed

import (
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
