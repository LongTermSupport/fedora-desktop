package vmtest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// kit is a lab on disk under a temporary directory, a fake runner answering every program the
// CLI runs, and a clock that only moves when the CLI sleeps. Every flow test is built on it.
type kit struct {
	t        *testing.T
	home     string // VMTEST_HOME
	userHome string
	gpg      string
	fake     *execx.Fake
	out, err bytes.Buffer
	now      time.Time
	slept    []time.Duration
	env      map[string]string
	stdin    string
	port     int
	bases    map[string]string // base key -> the validator's VMTEST-BASE line
	scens    map[string]string // scenario id -> the validator's VMTEST-SCENARIO line
	tweak    func(*cli)
	built    []string // base keys stubBuild was asked for
	lv       *libvirt // the fake libvirt, when a test installs one
	// served is the fake web server: URL to the body curl gets; a missing URL fails.
	served map[string]string
	gw     *guestWorld // the fake guest, when a test installs one
}

// stubBuild replaces build-base with a recorder, for the commands that call it.
func (k *kit) stubBuild() {
	k.tweak = func(c *cli) {
		c.build = func(_ context.Context, _ *lab, key string) error {
			k.built = append(k.built, key)
			return nil
		}
	}
}

const (
	testFedora   = "44"
	testRepoURL  = "https://github.com/example/fedora-desktop"
	testBranch   = "F44"
	testCommit   = "0123456789abcdef0123456789abcdef01234567"
	testProbeRev = "1700000000"
)

func newKit(t *testing.T) *kit {
	t.Helper()
	root := t.TempDir()
	k := &kit{
		t:        t,
		home:     filepath.Join(root, "lab"),
		userHome: filepath.Join(root, "user"),
		gpg:      filepath.Join(root, "gpg"),
		fake:     &execx.Fake{},
		now:      time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC),
		port:     40123,
		bases:    map[string]string{},
		scens:    map[string]string{},
	}
	k.env = map[string]string{"VMTEST_HOME": k.home, "VMTEST_HELPERS_LIB": "/opt/helpers"}
	k.write("lab.env", "VMTEST_FEDORA_VERSION="+testFedora+"\nVMTEST_REPO_URL="+testRepoURL+"\nVMTEST_REPO_BRANCH="+testBranch+"\nVMTEST_CHECKOUT="+root+"\n")
	k.write("scenarios.json", "{}\n")
	k.write("ssh/lab-key", "private\n")
	k.write("ssh/lab-key.pub", "ssh-ed25519 AAAA vmtest-lab\n")
	k.write("guest-cleanup.bash", "cleanup\n")
	k.write("ks-vm-server.cfg", "ks server\n")
	k.write("ks-vm-desktop.cfg", "ks desktop\n")
	k.write("vmtest-session-provision.bash", "runner\n")
	k.writeAbs(filepath.Join(k.gpg, "RPM-GPG-KEY-fedora-44-primary"), "key\n")
	k.base("server-fast", "server-fast-44", "fast", "server", "-")
	k.fake.OnFunc(helperArgv("validate_manifest"), k.validator)
	k.fake.On(helperArgv("probe_upstream"), execx.Result{Stdout: []byte(k.probeText())})
	return k
}

func helperArgv(module string) []string {
	return []string{"python3", "-P", "-m", "helpers.vmtest." + module}
}

func (k *kit) path(rel string) string { return filepath.Join(k.home, rel) }

func (k *kit) write(rel, body string) { k.writeAbs(k.path(rel), body) }

func (k *kit) writeAbs(path, body string) {
	k.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		k.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		k.t.Fatal(err)
	}
}

func (k *kit) read(rel string) string {
	k.t.Helper()
	body, err := os.ReadFile(k.path(rel))
	if err != nil {
		k.t.Fatal(err)
	}
	return string(body)
}

func (k *kit) exists(rel string) bool {
	_, err := os.Lstat(k.path(rel))
	return err == nil
}

// base declares a base the validator knows.
func (k *kit) base(key, name, kind, profile, tree string) {
	k.bases[key] = fmt.Sprintf("VMTEST-BASE key=%s name=%s kind=%s profile=%s tree=%s vcpus=2 ram_mib=4096\n", key, name, kind, profile, tree)
}

// scenario declares a scenario the validator knows, on the server-fast base unless changed.
func (k *kit) scenario(id string, fields map[string]string) {
	f := map[string]string{
		"id": id, "base": "server-fast", "base_name": "server-fast-44", "profile": "server", "planned": "5",
		"max_skipped": "0", "runnable": "true", "host_only": "false", "reboot_before_checks": "false", "run_env": "-",
	}
	maps.Copy(f, fields)
	order := []string{"id", "base", "base_name", "profile", "planned", "max_skipped", "runnable", "host_only", "reboot_before_checks", "run_env"}
	parts := make([]string, 0, len(order))
	for _, key := range order {
		parts = append(parts, key+"="+f[key])
	}
	k.scens[id] = "VMTEST-SCENARIO " + strings.Join(parts, " ") + "\n"
}

func (k *kit) validator(c execx.Cmd) (execx.Result, error) {
	for i, a := range c.Args {
		if i+1 >= len(c.Args) {
			break
		}
		switch a {
		case "--base":
			if line, ok := k.bases[c.Args[i+1]]; ok {
				return execx.Result{Stdout: []byte(line)}, nil
			}
			return execx.Result{Stderr: []byte("ERROR: no base " + c.Args[i+1] + "\n"), ExitCode: 1}, nil
		case "--scenario":
			if line, ok := k.scens[c.Args[i+1]]; ok {
				return execx.Result{Stdout: []byte(line)}, nil
			}
			return execx.Result{Stderr: []byte("ERROR: no scenario " + c.Args[i+1] + "\n"), ExitCode: 1}, nil
		}
	}
	return execx.Result{ExitCode: 2}, nil
}

func (k *kit) probeText() string {
	return "VMTEST-FRESHNESS-COMPOSE-ID Fedora-44-20261001.0\n" +
		"VMTEST-FRESHNESS-REVISION " + testProbeRev + "\n" +
		"VMTEST-FRESHNESS-ARTEFACT server-fast-44 Fedora-Cloud-Base-44.qcow2 " + strings.Repeat("a", 64) +
		" label=44-1.1 variant=Cloud url=https://dl.example.org/44/Cloud/x86_64/images/Fedora-Cloud-Base-44.qcow2\n" +
		"VMTEST-FRESHNESS-DONE unreadable=0\n"
}

// builtBase puts a base disk and record on disk for name.
func (k *kit) builtBase(name string) {
	k.write("bases/"+name+"/base.qcow2", "disk")
	k.write("bases/"+name+"/base.json", "{}")
}

// gate answers the freshness gate: the record matching the disk on disk, and the verdict rc.
func (k *kit) gate(rc int, decision string) {
	k.fake.OnFunc(helperArgv("freshness_gate"), func(c execx.Cmd) (execx.Result, error) {
		baseJSON := argAfter(c.Args, "--base-json")
		disk := filepath.Join(filepath.Dir(baseJSON), "base.qcow2")
		info, err := os.Stat(disk)
		if err != nil {
			return execx.Result{}, fmt.Errorf("the stub gate cannot read %s: %w", disk, err)
		}
		name := filepath.Base(filepath.Dir(baseJSON))
		record := fmt.Sprintf("VMTEST-BASE-RECORD name=%s kind=fast profile=server compose_label=44-1.1 revision=1 refresh_state=complete base_sha256=%s base_size=%d base_mtime=%d\n",
			name, strings.Repeat("b", 64), info.Size(), info.ModTime().Unix())
		if slices.Contains(c.Args, "--record-only") {
			return execx.Result{Stdout: []byte(record)}, nil
		}
		verdict := "VMTEST-FRESHNESS-VERDICT decision=" + decision + "\tdegraded=false\tdivergences=\treason=stub\n"
		return execx.Result{Stdout: []byte(record + verdict), ExitCode: rc}, nil
	})
}

func argAfter(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func (k *kit) environment() Env {
	return Env{
		Runner: k.fake,
		Stdin:  strings.NewReader(k.stdin),
		Stdout: &k.out,
		Stderr: &k.err,
		Getenv: func(key string) string { return k.env[key] },
		Home:   k.userHome,
		Now:    func() time.Time { return k.now },
		Sleep: func(ctx context.Context, d time.Duration) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			k.slept = append(k.slept, d)
			k.now = k.now.Add(d)
			return nil
		},
		FreePort:  func() (int, error) { return k.port, nil },
		Random:    zeroReader{},
		GPGKeyDir: k.gpg,
		TempDir:   k.t.TempDir(),
	}
}

// expiredBound stands in for a bound that has already passed.
func expiredBound(parent context.Context, _ time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, 0)
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 7
	}
	return len(p), nil
}

func (k *kit) run(args ...string) term.ExitCode {
	return k.runCtx(context.Background(), args...)
}

func (k *kit) runCtx(ctx context.Context, args ...string) term.ExitCode {
	k.t.Helper()
	k.out.Reset()
	k.err.Reset()
	app := &App{Env: k.environment(), tweak: k.tweak}
	return app.Run(ctx, args)
}

// lines is every recorded command line matching the substring.
func (k *kit) lines(sub string) []string {
	var out []string
	for _, c := range k.fake.Calls() {
		if strings.Contains(c.Line(), sub) {
			out = append(out, c.Line())
		}
	}
	return out
}

func (k *kit) count(sub string) int { return len(k.lines(sub)) }

// findCall is the first recorded command whose line contains sub.
func (k *kit) findCall(sub string) (execx.Cmd, bool) {
	for _, c := range k.fake.Calls() {
		if strings.Contains(c.Line(), sub) {
			return c, true
		}
	}
	return execx.Cmd{}, false
}

// mustCall is the first recorded command whose line contains sub; the test fails without one.
func (k *kit) mustCall(sub string) execx.Cmd {
	k.t.Helper()
	c, ok := k.findCall(sub)
	if !ok {
		k.t.Fatalf("no command matching %q was run:\n%s", sub, strings.Join(k.lines(""), "\n"))
	}
	return c
}

func (k *kit) wantCode(got, want term.ExitCode) {
	k.t.Helper()
	if got != want {
		k.t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, k.out.String(), k.err.String())
	}
}

func (k *kit) wantErr(sub string) {
	k.t.Helper()
	if !strings.Contains(k.err.String(), sub) {
		k.t.Fatalf("stderr lacks %q:\n%s", sub, k.err.String())
	}
}

// stdinText reads what a command was given on stdin.
func stdinText(t *testing.T, c execx.Cmd) string {
	t.Helper()
	if c.Stdin == nil {
		return ""
	}
	body, err := io.ReadAll(c.Stdin)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
