package setuprclone

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const probeOut = "setup_rclone_probe: !vault |\n          $ANSIBLE_VAULT;1.2;AES256;localhost\n          3232\n"

const confText = "[gdrive]\ntype = drive\ntoken = {\"access_token\":\"t\"}\n"

// fg is the foreground half of the fake machine: each program's exit, the output a play
// writes, and the commands handed to it in order.
type fg struct {
	mu      sync.Mutex
	exits   map[string]execx.Exit
	playOut string
	err     error
	calls   []execx.Cmd
	onCall  func(execx.Cmd)
}

func (f *fg) Foreground(c execx.Cmd) (execx.Exit, error) {
	f.mu.Lock()
	f.calls = append(f.calls, c)
	onCall := f.onCall
	f.mu.Unlock()
	if f.err != nil {
		return execx.Exit{}, f.err
	}
	if onCall != nil {
		onCall(c)
	}
	if c.Name == "ansible-playbook" && c.Stdout != nil {
		if _, err := io.WriteString(c.Stdout, f.playOut); err != nil {
			return execx.Exit{}, err
		}
	}
	return f.exits[c.Name], nil
}

func (f *fg) names() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		out = append(out, c.Line())
	}
	return out
}

type world struct {
	t           *testing.T
	root, home  string
	conf        string
	vars        string
	fake        *execx.Fake
	fg          *fg
	answers     string
	env         map[string]string
	missing     map[string]bool
	inContainer bool
	release     string
	terminal    bool
	errOut      io.Writer
	stdout      bytes.Buffer
	stderr      bytes.Buffer
	// signal, when set, has already ended the run's context, as SignalContext would.
	signal syscall.Signal
	// cancel ends the run's context, as a signal arriving mid-run would.
	cancel context.CancelCauseFunc
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func newWorld(t *testing.T) *world {
	t.Helper()
	root, home := t.TempDir(), t.TempDir()
	w := &world{
		t: t, root: root, home: home,
		conf:    filepath.Join(home, ".config/rclone/rclone.conf"),
		vars:    filepath.Join(root, "environment/localhost/host_vars/localhost.yml"),
		fake:    &execx.Fake{},
		fg:      &fg{exits: map[string]execx.Exit{}, playOut: "PLAY RECAP ok=3\n"},
		env:     map[string]string{"SETUP_RCLONE_ROOT": root, "HOME": home},
		missing: map[string]bool{},
		release: filepath.Join(root, "redhat-release"),
	}
	writeFile(t, w.vars, baseVars)
	writeFile(t, filepath.Join(root, playPath), "---\n")
	writeFile(t, filepath.Join(root, "ansible.cfg"), "[defaults]\n")
	writeFile(t, filepath.Join(root, "vars/fedora-version.yml"), "---\nfedora_version: 44\n")
	writeFile(t, w.release, "Fedora release 44 (Forty Four)\n")
	writeFile(t, w.conf, confText)
	w.fake.On([]string{"ansible-vault", "encrypt_string", "--encrypt-vault-id", "localhost", "--stdin-name", "setup_rclone_probe"}, execx.Result{Stdout: []byte(probeOut)})
	w.fake.OnFunc([]string{"ansible-vault", "encrypt_string", "--encrypt-vault-id", "localhost", "--stdin-name", "rclone_config"}, func(c execx.Cmd) (execx.Result, error) {
		got, err := io.ReadAll(c.Stdin)
		if err != nil {
			return execx.Result{}, err
		}
		if string(got) != confText {
			return execx.Result{ExitCode: 9, Stderr: []byte("wrong stdin")}, nil
		}
		return execx.Result{Stdout: []byte(vaultOut)}, nil
	})
	w.remotes("gdrive:\ns3:\n")
	w.fake.On(slices.Concat(w.rclone(), []string{"--contimeout", "15s", "--timeout", "30s", "lsd"}), execx.Result{Stdout: []byte("          -1 2024-01-01 00:00:00        -1 Photos\n          -1 2024-01-01 00:00:00        -1 Work\n")})
	return w
}

func (w *world) rclone() []string { return []string{"rclone", "--config", w.conf} }

func (w *world) remotes(out string) {
	w.fake.On(append(w.rclone(), "listremotes"), execx.Result{Stdout: []byte(out)})
}

func (w *world) run(args ...string) term.ExitCode {
	w.t.Helper()
	env := Env{
		Runner:     w.fake,
		Foreground: w.fg,
		Getenv:     func(k string) string { return w.env[k] },
		LookPath: func(name string) (string, error) {
			if w.missing[name] {
				return "", exec.ErrNotFound
			}
			return "/usr/bin/" + name, nil
		},
		InContainer:      func() bool { return w.inContainer },
		ReleaseFile:      w.release,
		Stdin:            strings.NewReader(w.answers),
		Stdout:           &w.stdout,
		Stderr:           &w.stderr,
		StderrIsTerminal: w.terminal,
	}
	if w.errOut != nil {
		env.Stderr = w.errOut
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	w.cancel = cancel
	if w.signal != 0 {
		cancel(&execx.SignalError{Signal: w.signal})
	}
	return App{Env: env}.Run(ctx, args)
}

func (w *world) varsText() string {
	w.t.Helper()
	data, err := os.ReadFile(w.vars)
	if err != nil {
		w.t.Fatal(err)
	}
	return string(data)
}

func (w *world) wantCode(got, want term.ExitCode) {
	w.t.Helper()
	if got != want {
		w.t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, w.stdout.String(), w.stderr.String())
	}
}

func (w *world) wantErr(parts ...string) {
	w.t.Helper()
	for _, p := range parts {
		if !strings.Contains(w.stderr.String(), p) {
			w.t.Errorf("stderr lacks %q:\n%s", p, w.stderr.String())
		}
	}
}

func (w *world) wantVarsUnchanged() {
	w.t.Helper()
	if w.varsText() != baseVars {
		w.t.Errorf("host_vars changed:\n%s", w.varsText())
	}
}

// E1.
func TestHelp(t *testing.T) {
	w := newWorld(t)
	w.wantCode(w.run("--help"), term.ExitOK)
	if !strings.Contains(w.stdout.String(), "Usage: setup-rclone") || w.stderr.Len() != 0 {
		t.Errorf("stdout %q stderr %q", w.stdout.String(), w.stderr.String())
	}
	w = newWorld(t)
	w.wantCode(w.run("-h"), term.ExitOK)
	if len(w.fake.Calls()) != 0 {
		t.Errorf("help ran commands")
	}
}

// E1.
func TestUsageError(t *testing.T) {
	w := newWorld(t)
	w.wantCode(w.run("extra"), term.ExitUsage)
	w.wantErr("setup-rclone: this command takes no arguments — see 'setup-rclone --help'.")
	if w.stdout.Len() != 0 || len(w.fake.Calls()) != 0 {
		t.Errorf("usage error wrote stdout or ran commands")
	}
}

// E2.
func TestRootAndHome(t *testing.T) {
	for _, root := range []string{"", "relative", "/does/not/exist"} {
		w := newWorld(t)
		w.env["SETUP_RCLONE_ROOT"] = root
		w.wantCode(w.run(), term.ExitFailure)
		w.wantErr("scripts/setup-rclone.bash")
	}
	w := newWorld(t)
	w.env["HOME"] = "rel"
	w.wantCode(w.run(), term.ExitFailure)
	w.wantErr("HOME")
}

// E3.
func TestRefusesContainer(t *testing.T) {
	w := newWorld(t)
	w.inContainer = true
	w.wantCode(w.run(), term.ExitFailure)
	w.wantErr("host")
	if len(w.fake.Calls()) != 0 {
		t.Errorf("ran commands in a container")
	}
}

// E4.
func TestPreflightFiles(t *testing.T) {
	w := newWorld(t)
	w.missing["ansible-vault"] = true
	w.wantCode(w.run(), term.ExitFailure)
	w.wantErr("ansible-vault")

	w = newWorld(t)
	w.missing["ansible-playbook"] = true
	w.wantCode(w.run(), term.ExitFailure)
	w.wantErr("ansible-playbook")

	for _, rel := range []string{"environment/localhost/host_vars/localhost.yml", playPath, "ansible.cfg"} {
		w = newWorld(t)
		if err := os.Remove(filepath.Join(w.root, rel)); err != nil {
			t.Fatal(err)
		}
		w.wantCode(w.run(), term.ExitFailure)
		w.wantErr(rel)
	}

	w = newWorld(t)
	writeFile(t, w.vars, "rclone_mounts: {a: 1}\n")
	w.answers = "n\n"
	w.wantCode(w.run(), term.ExitFailure)
	if strings.Contains(w.stderr.String(), "wizard") {
		t.Errorf("asked a question before refusing host_vars:\n%s", w.stderr.String())
	}
}

// E5.
func TestVaultProbe(t *testing.T) {
	w := newWorld(t)
	w.answers = "n\n0\nn\n"
	w.wantCode(w.run(), term.ExitOK)
	var probe *execx.Cmd
	for _, c := range w.fake.Calls() {
		if strings.Contains(c.Line(), "setup_rclone_probe") {
			probe = &c
		}
	}
	if probe == nil {
		t.Fatal("no probe")
	}
	if probe.Dir != w.root || !slices.Contains(probe.Env, "ANSIBLE_CONFIG="+filepath.Join(w.root, "ansible.cfg")) {
		t.Errorf("probe dir %q env %v", probe.Dir, probe.Env)
	}

	w = newWorld(t)
	w.fake.On([]string{"ansible-vault", "encrypt_string", "--encrypt-vault-id", "localhost", "--stdin-name", "setup_rclone_probe"}, execx.Result{ExitCode: 1, Stderr: []byte("ERROR! no vault secrets")})
	w.wantCode(w.run(), term.ExitFailure)
	w.wantErr("no vault secrets", "ansible.cfg")
	if len(w.fg.names()) != 0 {
		t.Errorf("wizard ran after a failed probe")
	}

	w = newWorld(t)
	w.fake.On([]string{"ansible-vault", "encrypt_string", "--encrypt-vault-id", "localhost", "--stdin-name", "setup_rclone_probe"}, execx.Result{Stdout: []byte("setup_rclone_probe: plain\n")})
	w.wantCode(w.run(), term.ExitFailure)
}

// E6.
func TestInstallsRcloneThroughThePlay(t *testing.T) {
	w := newWorld(t)
	w.missing["rclone"] = true
	w.fg.onCall = func(c execx.Cmd) {
		if c.Name == "ansible-playbook" {
			w.fg.mu.Lock()
			w.missing["rclone"] = false
			w.fg.mu.Unlock()
		}
	}
	w.answers = "n\n0\nn\n"
	w.wantCode(w.run(), term.ExitOK)
	w.wantErr("rclone not found")
	if got := w.fg.names(); len(got) != 1 || got[0] != "ansible-playbook "+playPath {
		t.Errorf("foreground = %v", got)
	}

	w = newWorld(t)
	w.missing["rclone"] = true
	w.wantCode(w.run(), term.ExitFailure)
	w.wantErr("rclone is still not installed")

	w = newWorld(t)
	w.missing["rclone"] = true
	w.fg.exits["ansible-playbook"] = execx.Exit{Code: 2}
	w.wantCode(w.run(), term.ExitFailure)
	w.wantErr("Playbook failed")
}

// E7.
func TestFedoraVersion(t *testing.T) {
	w := newWorld(t)
	writeFile(t, w.release, "Ubuntu 24.04\n")
	w.wantCode(w.run(), term.ExitFailure)
	w.wantErr("Fedora")

	w = newWorld(t)
	if err := os.Remove(w.release); err != nil {
		t.Fatal(err)
	}
	w.wantCode(w.run(), term.ExitFailure)

	w = newWorld(t)
	writeFile(t, filepath.Join(w.root, "vars/fedora-version.yml"), "fedora_version: forty\n")
	w.wantCode(w.run(), term.ExitFailure)
	w.wantErr("fedora_version")

	w = newWorld(t)
	writeFile(t, w.release, "Fedora release 45 (Rawhide)\n")
	w.answers = "\n"
	w.wantCode(w.run(), term.ExitFailure)
	w.wantErr("Fedora 45", "Fedora 44", "F45", "Aborted: Fedora version mismatch")

	w = newWorld(t)
	writeFile(t, w.release, "Fedora release 45 (Rawhide)\n")
	w.answers = "y\nn\n0\nn\n"
	w.wantCode(w.run(), term.ExitOK)
}

// E8: no remote opens the wizard without asking.
func TestNoRemoteOpensWizard(t *testing.T) {
	w := newWorld(t)
	calls := 0
	w.fake.OnFunc(append(w.rclone(), "listremotes"), func(execx.Cmd) (execx.Result, error) {
		calls++
		if calls == 1 {
			return execx.Result{}, nil
		}
		return execx.Result{Stdout: []byte("gdrive:\n")}, nil
	})
	w.answers = "\n0\nn\n"
	w.wantCode(w.run(), term.ExitOK)
	if strings.Contains(w.stderr.String(), "Open the rclone wizard") {
		t.Errorf("asked about the wizard with no remote")
	}
	if got := w.fg.names(); len(got) != 1 || got[0] != strings.Join(append(w.rclone(), "config"), " ") {
		t.Errorf("foreground = %v", got)
	}
	if !strings.Contains(w.varsText(), "3131") {
		t.Errorf("not vaulted:\n%s", w.varsText())
	}
}

// E8.
func TestRemotesListed(t *testing.T) {
	w := newWorld(t)
	w.remotes("gdrive\n")
	w.wantCode(w.run(), term.ExitFailure)

	w = newWorld(t)
	w.fake.On(append(w.rclone(), "listremotes"), execx.Result{ExitCode: 1, Stderr: []byte("boom")})
	w.wantCode(w.run(), term.ExitFailure)
	w.wantErr("boom")

	w = newWorld(t)
	w.answers = "\n0\n\n"
	w.wantCode(w.run(), term.ExitOK)
	w.wantErr("gdrive", "s3", "Open the rclone wizard to add or edit remotes? [y/N]")
	if len(w.fg.names()) != 0 {
		t.Errorf("wizard ran on the default answer")
	}
	w.wantErr("host_vars unchanged")
	w.wantVarsUnchanged()
}

// E9.
func TestWizardFailure(t *testing.T) {
	w := newWorld(t)
	w.fg.exits["rclone"] = execx.Exit{Code: 1}
	w.answers = "y\n\nn\n"
	w.wantCode(w.run(), term.ExitFailure)
	w.wantErr("rclone config exited 1", "Vault the rclone configuration as it is now?")
	w.wantVarsUnchanged()

	w = newWorld(t)
	w.fg.exits["rclone"] = execx.Exit{Code: 1}
	w.answers = "y\n\ny\n0\nn\n"
	w.wantCode(w.run(), term.ExitOK)
	if !strings.Contains(w.varsText(), "3131") {
		t.Errorf("not vaulted after yes")
	}

	w = newWorld(t)
	w.fg.exits["rclone"] = execx.Exit{Code: 130, Signal: syscall.SIGINT}
	w.answers = "y\n\n"
	w.wantCode(w.run(), 130)
	w.wantVarsUnchanged()

	w = newWorld(t)
	w.fg.err = errors.New("cannot start")
	w.answers = "y\n\n"
	w.wantCode(w.run(), term.ExitFailure)
	w.wantVarsUnchanged()

	w = newWorld(t)
	calls := 0
	w.fake.OnFunc(append(w.rclone(), "listremotes"), func(execx.Cmd) (execx.Result, error) {
		calls++
		if calls == 1 {
			return execx.Result{Stdout: []byte("gdrive:\n")}, nil
		}
		return execx.Result{}, nil
	})
	w.answers = "y\n\n"
	w.wantCode(w.run(), term.ExitFailure)
	w.wantErr("No remote")
}

// E10.
func TestVault(t *testing.T) {
	w := newWorld(t)
	if err := os.Remove(w.conf); err != nil {
		t.Fatal(err)
	}
	w.answers = "y\n\n"
	w.wantCode(w.run(), term.ExitFailure)
	w.wantErr(w.conf)
	w.wantVarsUnchanged()

	w = newWorld(t)
	w.fake.On([]string{"ansible-vault", "encrypt_string", "--encrypt-vault-id", "localhost", "--stdin-name", "rclone_config"}, execx.Result{Stdout: []byte("rclone_config: plain\n")})
	w.answers = "y\n\n"
	w.wantCode(w.run(), term.ExitFailure)
	w.wantVarsUnchanged()

	w = newWorld(t)
	w.answers = "y\n\n0\nn\n"
	w.wantCode(w.run(), term.ExitOK)
	for _, c := range w.fake.Calls() {
		if strings.Contains(c.Line(), "token") || strings.Contains(c.Line(), "access_token") {
			t.Errorf("configuration in an argv: %s", c.Line())
		}
	}
	if strings.Contains(w.stderr.String(), "access_token") || strings.Contains(w.stderr.String(), "3131") {
		t.Errorf("configuration or vault text printed")
	}
	w.wantErr("host_vars updated")
}

// E11.
func TestMenu(t *testing.T) {
	w := newWorld(t)
	w.remotes("gdrive:\nbox:\n")
	w.answers = "n\n0\nn\n"
	w.wantCode(w.run(), term.ExitOK)
	w.wantErr(
		"1) Change gdrive: gdrive:/ at /home/u/mnt/gdrive\n",
		"2) Change photos: s3:bucket/photos at /home/u/mnt/photos (remote not configured)\n",
		"3) Add a mount of gdrive\n",
		"4) Add a mount of box\n",
		"0) Done\n",
		"setup-rclone › Mounts",
	)
}

// E12: remove.
func TestRemoveMount(t *testing.T) {
	w := newWorld(t)
	w.answers = "n\n2\nr\n0\nn\n"
	w.wantCode(w.run(), term.ExitOK)
	w.wantErr("Removed mount photos", "rclone-photos.service", "keeps running", "section 6")
	if strings.Contains(w.stderr.String(), "systemctl") {
		t.Errorf("handed the person a manual systemctl command:\n%s", w.stderr.String())
	}
	text := w.varsText()
	if strings.Contains(text, "photos") || !strings.Contains(text, "rc_port: 5590") {
		t.Errorf("after remove:\n%s", text)
	}
}

// E12: cancel changes nothing; E16.
func TestCancelMount(t *testing.T) {
	w := newWorld(t)
	w.answers = "n\n1\nc\n0\nn\n"
	w.wantCode(w.run(), term.ExitOK)
	w.wantErr("host_vars unchanged")
	w.wantVarsUnchanged()
}

// E12, E13: edit keeps the other keys and offers the old values as defaults.
func TestEditMount(t *testing.T) {
	w := newWorld(t)
	w.answers = "n\n1\ne\n\n\n/home/u/mnt/drive2\n0\nn\n"
	w.wantCode(w.run(), term.ExitOK)
	w.wantErr("Folder to mount [/]", "Mount name [gdrive]", "Local mountpoint [/home/u/mnt/gdrive]", "Photos", "Work")
	text := w.varsText()
	for _, want := range []string{"rc_port: 5590", "cache_mode: writes", "/home/u/mnt/drive2"} {
		if !strings.Contains(text, want) {
			t.Errorf("lacks %q:\n%s", want, text)
		}
	}
	if !w.fake.Called(strings.Join(slices.Concat(w.rclone(), []string{"--contimeout", "15s", "--timeout", "30s", "lsd", "gdrive:/"}), " ")) {
		t.Errorf("no lsd of gdrive:/")
	}
}

// E13: add with the defaults, appended.
func TestAddMount(t *testing.T) {
	w := newWorld(t)
	w.answers = "n\n4\nbackup\n\n\n0\nn\n"
	w.wantCode(w.run(), term.ExitOK)
	hv := mustParse(t, w.varsText())
	if len(hv.mounts) != 3 {
		t.Fatalf("mounts = %+v", hv.mounts)
	}
	m := hv.mounts[2]
	if m.name != "s3" || m.remote != "s3:backup" || m.mountpoint != filepath.Join(w.home, "mnt/s3") {
		t.Errorf("added = %+v", m)
	}
}

// E13: the root of an empty remote.
func TestAddMountEmptyRemote(t *testing.T) {
	w := newWorld(t)
	w.fake.On(slices.Concat(w.rclone(), []string{"--contimeout", "15s", "--timeout", "30s", "lsd", "s3:/"}), execx.Result{})
	w.answers = "n\n4\n\n\n\n0\nn\n"
	w.wantCode(w.run(), term.ExitOK)
	w.wantErr("(remote root is empty)")
}

// E13: a remote that cannot be reached goes back to the menu with a hint.
func TestConnectFailureHints(t *testing.T) {
	cases := map[string][]string{
		"2024/01/01 00:00:00 ERROR : googleapi: Error 403: Drive API has not been used in project 123456 before or it is disabled. accessNotConfigured": {"Drive API", "project=123456", "googleapi: Error 403"},
		"2024/01/01 00:00:00 Failed to create file system: couldn't fetch token: invalid_grant":                                                         {"rclone config reconnect s3:"},
		"2024/01/01 00:00:00 ERROR : dial tcp: lookup example.com: no such host":                                                                        {"Network"},
		"2024/01/01 00:00:00 ERROR : something odd":                                                                                                     {"rclone lsd s3:/", "something odd"},
	}
	for msg, want := range cases {
		w := newWorld(t)
		w.fake.On(slices.Concat(w.rclone(), []string{"--contimeout", "15s", "--timeout", "30s", "lsd", "s3:/"}), execx.Result{ExitCode: 1, Stderr: []byte(msg + "\n")})
		w.answers = "n\n4\n0\nn\n"
		w.wantCode(w.run(), term.ExitOK)
		w.wantErr(append(want, "Could not connect to s3")...)
		if strings.Contains(w.stderr.String(), "2024/01/01") {
			t.Errorf("date prefix not stripped:\n%s", w.stderr.String())
		}
		w.wantVarsUnchanged()
	}
}

// E15: a bad answer is said and asked again; three bad ones end the run.
func TestValidationReasks(t *testing.T) {
	w := newWorld(t)
	w.answers = "n\n4\n\nBad Name\ngdrive\nok-name\n/home/u/mnt/gdrive/x\n/srv/ok\n0\nn\n"
	w.wantCode(w.run(), term.ExitOK)
	hv := mustParse(t, w.varsText())
	if m := hv.mounts[2]; m.name != "ok-name" || m.mountpoint != "/srv/ok" {
		t.Errorf("added = %+v", m)
	}

	w = newWorld(t)
	w.answers = "n\n4\n\nA\nB\nC\n"
	w.wantCode(w.run(), term.ExitFailure)
	w.wantVarsUnchanged()

	w = newWorld(t)
	w.remotes("my drive:\n")
	w.answers = "n\n3\n0\nn\n"
	w.wantCode(w.run(), term.ExitOK)
	w.wantErr("my drive")
	if w.fake.Called("rclone --config " + w.conf + " --contimeout 15s --timeout 30s lsd my drive") {
		t.Errorf("listed a remote that cannot be mounted")
	}
}

// E18: the file is written through a symlink, its mode kept, though the umask the run has
// narrows it.
func TestWriteThroughSymlinkKeepsMode(t *testing.T) {
	w := newWorld(t)
	target := filepath.Join(w.root, "target.yml")
	writeFile(t, target, baseVars)
	if err := os.Chmod(target, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(w.vars); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, w.vars); err != nil {
		t.Fatal(err)
	}
	w.answers = "n\n2\nr\n0\nn\n"
	// The new file would be created 0400 without the mode put back.
	old := syscall.Umask(0o277)
	code := w.run()
	syscall.Umask(old)
	w.wantCode(code, term.ExitOK)
	fi, err := os.Lstat(w.vars)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("symlink replaced: %v", err)
	}
	st, err := os.Stat(target)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v %v", st.Mode(), err)
	}
}

// E18: host_vars changed by something else while the run waited is not overwritten.
func TestHostVarsChangedMeanwhile(t *testing.T) {
	w := newWorld(t)
	newer := baseVars + "added_meanwhile: 1\n"
	w.fg.onCall = func(c execx.Cmd) {
		if c.Name == "rclone" {
			writeFile(t, w.vars, newer)
		}
	}
	w.answers = "y\n\n0\nn\n"
	w.wantCode(w.run(), term.ExitFailure)
	w.wantErr(w.vars, "changed while setup-rclone ran", "nothing was written")
	if w.varsText() != newer {
		t.Errorf("the newer host_vars was overwritten:\n%s", w.varsText())
	}
	if strings.Contains(w.stderr.String(), "Run the rclone play now?") {
		t.Errorf("went on to the deploy after refusing the write")
	}
}

// E12, E15: editing a mount whose remote name cannot be mounted is refused up front, as an
// add is, and the menu comes back.
func TestEditUnsafeLegacyRemote(t *testing.T) {
	w := newWorld(t)
	writeFile(t, w.vars, "rclone_mounts:\n  - name: legacy\n    remote: \"bad%r:/\"\n    mountpoint: /home/u/mnt/legacy\n")
	w.answers = "n\n1\ne\n0\nn\n"
	w.wantCode(w.run(), term.ExitOK)
	w.wantErr("The remote \"bad%r\" cannot be mounted")
	if w.fake.Called(strings.Join(slices.Concat(w.rclone(), []string{"--contimeout"}), " ")) {
		t.Errorf("checked the connection of a remote that cannot be mounted")
	}
	w.wantErr("host_vars unchanged")
}

// E13, E22: a signal during the connection check ends the run with 128+n and no hint.
func TestSignalDuringConnect(t *testing.T) {
	w := newWorld(t)
	w.fake.OnFunc(slices.Concat(w.rclone(), []string{"--contimeout", "15s", "--timeout", "30s", "lsd"}), func(execx.Cmd) (execx.Result, error) {
		w.cancel(&execx.SignalError{Signal: syscall.SIGINT})
		return execx.Result{}, context.Canceled
	})
	w.answers = "n\n3\n0\nn\n"
	w.wantCode(w.run(), 130)
	for _, unwanted := range []string{"Could not connect", "Unexpected error", "▶"} {
		if strings.Contains(w.stderr.String(), unwanted) {
			t.Errorf("stderr holds %q after the signal:\n%s", unwanted, w.stderr.String())
		}
	}
	w.wantVarsUnchanged()
}

// E19, E20, E21.
func TestDeploy(t *testing.T) {
	w := newWorld(t)
	w.answers = "n\n0\n\n"
	w.wantCode(w.run(), term.ExitOK)
	w.wantErr(filepath.Join(w.root, playPath))
	if len(w.fg.names()) != 0 {
		t.Errorf("play ran on the default answer")
	}

	w = newWorld(t)
	w.answers = "n\n0\ny\n"
	w.wantCode(w.run(), term.ExitOK)
	w.wantErr("PLAY RECAP", "Mount services started and enabled", "systemctl --user status rclone-gdrive.service", "systemctl --user status rclone-photos.service")
	c := w.fg.calls[0]
	if c.Dir != w.root || c.Line() != "ansible-playbook "+playPath {
		t.Errorf("play = %q in %q", c.Line(), c.Dir)
	}
	if w.stdout.Len() != 0 {
		t.Errorf("play output on stdout")
	}

	w = newWorld(t)
	w.fg.playOut = "PLAY [desktop]\nskipping: no hosts matched\n"
	w.answers = "n\n0\ny\n"
	w.wantCode(w.run(), term.ExitFailure)
	w.wantErr("matched no hosts")

	w = newWorld(t)
	w.fg.exits["ansible-playbook"] = execx.Exit{Code: 4}
	w.answers = "n\n0\ny\n"
	w.wantCode(w.run(), term.ExitFailure)
	w.wantErr("Playbook failed")

	w = newWorld(t)
	w.fg.exits["ansible-playbook"] = execx.Exit{Code: 143, Signal: syscall.SIGTERM}
	w.answers = "n\n0\ny\n"
	w.wantCode(w.run(), 143)
}

// The guard sees a phrase split across two writes.
func TestWatchSplitWrites(t *testing.T) {
	var out bytes.Buffer
	s := &watch{w: &out, phrase: noHostsPhrase}
	for _, part := range []string{"PLAY\nskipping: no ho", "sts matched\n"} {
		if _, err := s.Write([]byte(part)); err != nil {
			t.Fatal(err)
		}
	}
	if !s.matched() || out.String() != "PLAY\nskipping: no hosts matched\n" {
		t.Errorf("matched %v, out %q", s.matched(), out.String())
	}
}

// E22.
func TestSignalAtQuestion(t *testing.T) {
	w := newWorld(t)
	w.signal = syscall.SIGINT
	w.answers = "n\n"
	w.wantCode(w.run(), 130)
	w.wantVarsUnchanged()

	w = newWorld(t)
	w.answers = "n\n1\n"
	w.wantCode(w.run(), term.ExitFailure)
	w.wantVarsUnchanged()
}

// E23.
func TestColourAndStderrFailure(t *testing.T) {
	w := newWorld(t)
	w.answers = "n\n0\nn\n"
	w.wantCode(w.run(), term.ExitOK)
	if strings.Contains(w.stderr.String(), "\x1b[") {
		t.Errorf("colour on a pipe")
	}

	w = newWorld(t)
	w.answers = "n\n0\nn\n"
	w.terminal = true
	w.wantCode(w.run(), term.ExitOK)
	if !strings.Contains(w.stderr.String(), "\x1b[") {
		t.Errorf("no colour on a terminal")
	}

	w = newWorld(t)
	w.answers = "n\n0\nn\n"
	w.terminal = true
	w.env["NO_COLOR"] = "1"
	w.wantCode(w.run(), term.ExitOK)
	if strings.Contains(w.stderr.String(), "\x1b[") {
		t.Errorf("colour with NO_COLOR")
	}

	w = newWorld(t)
	w.answers = "n\n0\nn\n"
	w.errOut = failWriter{}
	if code := w.run(); code != term.ExitFailure {
		t.Errorf("exit %d with a failing stderr", code)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

// NewEnv is the production wiring.
func TestNewEnv(t *testing.T) {
	env := NewEnv()
	if env.Runner == nil || env.Foreground == nil || env.Getenv == nil || env.LookPath == nil || env.InContainer == nil || env.ReleaseFile != "/etc/redhat-release" || env.Stdin == nil {
		t.Errorf("NewEnv = %+v", env)
	}
}
