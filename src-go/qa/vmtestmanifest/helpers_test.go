package vmtestmanifest

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

const okMarker = "VMTEST-MANIFEST-OK scenarios=3 runnable=2 bridge=2 host_only=0 bases=2\n"

// goodManifest has two runnable scenarios (one judged by its profile's checker, one by a
// checker of its own name that reboots before it runs) and one that declares no count yet.
const goodManifest = `vm_test_ttl_upgrade_days: 7
vm_test_ttl_rebuild_days: 90
vm_test_bases:
  server-fast:
    kind: fast
    profile: server
  desktop:
    kind: full
    profile: desktop
vm_test_scenarios:
  server-fast-provision:
    base: server-fast
    planned: 13   # = PLANNED in guest-acceptance-server.bash
    max_skipped: 0
  desktop-fresh-install:
    base: desktop
    planned: 16
    max_skipped: 0
    reboot_before_checks: true
  not-yet:
    base: server-fast
    planned: null
    max_skipped: 0
`

// script is a guest checker that declares the count the way the real ones do.
func script(planned string) string {
	return "#!/usr/bin/bash\nset -euo pipefail\nPLANNED=" + planned + "\nreadonly PLANNED\nprintf '%d\\n' \"${PLANNED}\"\n"
}

// call is one python3 run the fake saw.
type call struct {
	argv  []string
	dir   string
	env   []string
	stdin string
}

// fakeRunner answers python3 with its fields and runs the real git, which only lists files.
type fakeRunner struct {
	reply    proc.Result
	startErr error
	calls    []call
}

func (f *fakeRunner) Run(ctx context.Context, cmd proc.Cmd) (proc.Result, error) {
	if cmd.Argv[0] == "git" {
		return proc.OS{}.Run(ctx, cmd)
	}
	c := call{argv: cmd.Argv, dir: cmd.Dir, env: cmd.Env}
	if cmd.Stdin != nil {
		data, err := io.ReadAll(cmd.Stdin)
		if err != nil {
			return proc.Result{}, err
		}
		c.stdin = string(data)
	}
	f.calls = append(f.calls, c)
	return f.reply, f.startErr
}

type world struct {
	t      *testing.T
	root   string
	fake   *fakeRunner
	stdout bytes.Buffer
	stderr bytes.Buffer
}

// newWorld writes a repository under a fresh root and tracks it in git. files overrides or adds
// files; an empty text removes one.
func newWorld(t *testing.T, files map[string]string) *world {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := &world{t: t, root: root, fake: &fakeRunner{reply: proc.Result{Stdout: okMarker}}}
	all := map[string]string{
		manifestFile:           goodManifest,
		versionFile:            "# target\nfedora_version: 44\n",
		"scripts/qa-trees.tsv": "shell\t.git\nshell\tnode_modules\npython\t.git\n",
		guestDir + "guest-acceptance-server.bash":                script("13"),
		guestDir + "guest-acceptance-desktop-fresh-install.bash": script("16"),
		guestDir + "guest-prepare-desktop-fresh-install.bash":    "#!/usr/bin/bash\necho prepare\n",
		guestDir + "vmtest-session-provision.bash":               "#!/usr/bin/bash\necho unrelated\n",
	}
	for k, v := range files {
		if v == "" {
			delete(all, k)
		} else {
			all[k] = v
		}
	}
	for rel, text := range all {
		w.write(rel, text)
	}
	w.track()
	return w
}

// track makes git track every file written so far.
func (w *world) track() {
	w.t.Helper()
	for _, argv := range [][]string{{"git", "init", "-q"}, {"git", "add", "-A", "-f"}} {
		if res, err := (proc.OS{}).Run(context.Background(), proc.Cmd{Argv: argv, Dir: w.root}); err != nil || res.Code != 0 {
			w.t.Fatalf("%v: %v %+v", argv, err, res)
		}
	}
}

// git runs a git command in the repository.
func (w *world) git(args ...string) {
	w.t.Helper()
	argv := append([]string{"git"}, args...)
	if res, err := (proc.OS{}).Run(context.Background(), proc.Cmd{Argv: argv, Dir: w.root}); err != nil || res.Code != 0 {
		w.t.Fatalf("%v: %v %+v", argv, err, res)
	}
}

func (w *world) write(rel, text string) {
	w.t.Helper()
	path := filepath.Join(w.root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) env() Env {
	return Env{Stdout: &w.stdout, Stderr: &w.stderr, Cwd: w.root, Runner: w.fake, Timeout: time.Minute}
}

func (w *world) run(args ...string) ExitCode {
	w.t.Helper()
	return Main(append([]string{"--root", w.root}, args...), w.env())
}

func (w *world) wantExit(code ExitCode, got ExitCode) {
	w.t.Helper()
	if got != code {
		w.t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", got, code, w.stdout.String(), w.stderr.String())
	}
}

func (w *world) wantStderr(parts ...string) {
	w.t.Helper()
	for _, p := range parts {
		if !strings.Contains(w.stderr.String(), p) {
			w.t.Fatalf("stderr lacks %q:\n%s", p, w.stderr.String())
		}
	}
}

func (w *world) wantNoStdout() {
	w.t.Helper()
	if w.stdout.Len() != 0 {
		w.t.Fatalf("stdout must be empty on a failed run:\n%s", w.stdout.String())
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }
