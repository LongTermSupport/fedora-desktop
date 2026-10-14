package gobuild

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

const play = "playbooks/imports/play-x.yml"

// driftWorld is a checkout with two commands and a directory for stand-in installed programs.
type driftWorld struct {
	root, bin string
}

// verdict is what a drift check printed and returned.
type verdict struct {
	code     int
	out, err string
}

func newDriftWorld(t *testing.T) driftWorld {
	t.Helper()
	root := toyTree(t)
	put(t, root, "cmd/beta/main.go", "package main\n\n// beta\n")
	return driftWorld{root: root, bin: filepath.Join(t.TempDir(), "bin")}
}

func (w driftWorld) rows(t *testing.T, lines ...string) {
	t.Helper()
	path := filepath.Join(w.root, TablePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# header\n\n"+strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (w driftWorld) row(command string) string {
	return command + "\t" + filepath.Join(w.bin, command) + "\t" + play
}

// install writes a stand-in that answers --version with answer and exits with code.
func (w driftWorld) install(t *testing.T, command, answer string, code int) string {
	t.Helper()
	if err := os.MkdirAll(w.bin, 0o750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(w.bin, command)
	script := "#!/bin/sh\n[ \"$1\" = --version ] || exit 9\necho '" + answer + "'\nexit " + strconv.Itoa(code) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func (w driftWorld) checkWithHome(home string) verdict {
	var out, errOut bytes.Buffer
	code := Drift(context.Background(), proc.OS{}, DriftOptions{Source: w.root, Home: home}, &out, &errOut)
	return verdict{code, out.String(), errOut.String()}
}

func (w driftWorld) check() verdict {
	return w.checkWithHome("/nonexistent-home")
}

func TestInstalledCommandsThatReportTheCheckoutsDigestPass(t *testing.T) {
	w := newDriftWorld(t)
	w.rows(t, w.row("alpha"), w.row("beta"))
	w.install(t, "alpha", mustDigest(t, w.root, "alpha"), 0)
	w.install(t, "beta", mustDigest(t, w.root, "beta"), 0)
	v := w.check()
	if v.code != ExitOK || v.err != "" || !strings.Contains(v.out, "2 deployed Go command(s) match the checkout") {
		t.Fatalf("%+v", v)
	}
}

func TestACommandBuiltFromOlderSourceIsDriftAndNamesThePlay(t *testing.T) {
	w := newDriftWorld(t)
	w.rows(t, w.row("alpha"), w.row("beta"))
	w.install(t, "alpha", mustDigest(t, w.root, "alpha"), 0)
	w.install(t, "beta", strings.Repeat("0", 64), 0)
	v := w.check()
	if v.code != ExitDrift || v.out != "" {
		t.Fatalf("%+v", v)
	}
	for _, want := range []string{"beta", "ansible-playbook " + play, "1 of 2"} {
		if !strings.Contains(v.err, want) {
			t.Errorf("stderr lacks %q: %s", want, v.err)
		}
	}
	if strings.Contains(v.err, "alpha") {
		t.Errorf("stderr names the command that matches: %s", v.err)
	}
}

func TestASourceChangeMovesTheExpectedDigest(t *testing.T) {
	w := newDriftWorld(t)
	w.rows(t, w.row("alpha"))
	w.install(t, "alpha", mustDigest(t, w.root, "alpha"), 0)
	put(t, w.root, "cmd/alpha/main.go", "package main\n\n// changed\n")
	if v := w.check(); v.code != ExitDrift {
		t.Fatalf("%+v", v)
	}
}

func TestADeployedCommandThatCannotReportADigestIsDrift(t *testing.T) {
	w := newDriftWorld(t)
	w.rows(t, w.row("alpha"))
	w.install(t, "alpha", "usage: unknown option --version", 2)
	v := w.check()
	if v.code != ExitDrift || !strings.Contains(v.err, "does not report a source digest") {
		t.Fatalf("%+v", v)
	}
}

func TestACommandThatIsNotInstalledIsCountedNotChecked(t *testing.T) {
	w := newDriftWorld(t)
	w.rows(t, w.row("alpha"), w.row("beta"))
	w.install(t, "alpha", mustDigest(t, w.root, "alpha"), 0)
	v := w.check()
	if v.code != ExitOK || !strings.Contains(v.out, "1 deployed Go command(s) match") || !strings.Contains(v.out, "1 not installed on this host") {
		t.Fatalf("%+v", v)
	}
}

func TestAHomeRelativePathIsExpanded(t *testing.T) {
	w := newDriftWorld(t)
	home := t.TempDir()
	w.rows(t, "alpha\t~/.local/bin/alpha\t"+play)
	w.bin = filepath.Join(home, ".local", "bin")
	w.install(t, "alpha", mustDigest(t, w.root, "alpha"), 0)
	if v := w.checkWithHome(home + "/"); v.code != ExitOK || v.err != "" {
		t.Fatalf("%+v", v)
	}
}

func TestARowForACommandTheSourceDoesNotHaveCouldNotRun(t *testing.T) {
	w := newDriftWorld(t)
	w.rows(t, w.row("ghost"))
	w.install(t, "ghost", "x", 0)
	v := w.check()
	if v.code != ExitCannotRun || !strings.Contains(v.err, "cmd/ghost") {
		t.Fatalf("%+v", v)
	}
}

func TestAMalformedRowCouldNotRun(t *testing.T) {
	w := newDriftWorld(t)
	w.rows(t, "alpha only-two-columns")
	v := w.check()
	if v.code != ExitCannotRun || !strings.Contains(v.err, "go-binary-installs.tsv") {
		t.Fatalf("%+v", v)
	}
}

func TestAMissingTableCouldNotRun(t *testing.T) {
	v := newDriftWorld(t).check()
	if v.code != ExitCannotRun || !strings.Contains(v.err, "go-binary-installs.tsv") {
		t.Fatalf("%+v", v)
	}
}

func TestAFileThatIsNotExecutableIsDriftNotACrash(t *testing.T) {
	w := newDriftWorld(t)
	w.rows(t, w.row("alpha"))
	path := w.install(t, "alpha", mustDigest(t, w.root, "alpha"), 0)
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	v := w.check()
	if v.code != ExitDrift || !strings.Contains(v.err, "alpha") {
		t.Fatalf("%+v", v)
	}
}

func TestADirectoryAtTheInstalledPathIsNotAnInstalledCommand(t *testing.T) {
	w := newDriftWorld(t)
	w.rows(t, w.row("alpha"))
	if err := os.MkdirAll(filepath.Join(w.bin, "alpha"), 0o750); err != nil {
		t.Fatal(err)
	}
	v := w.check()
	if v.code != ExitOK || !strings.Contains(v.out, "1 not installed") {
		t.Fatalf("%+v", v)
	}
}
