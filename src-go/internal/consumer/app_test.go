package consumer

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	vaultName   = "vault.txt"
	pipxListOut = `{"venvs":{"ansible":{"metadata":{"injected_packages":{"jmespath":{}}}}}}`
	consumerYml = "user_login: \"u\"\ninstall_audience: consumer\n"
)

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

type world struct {
	t          *testing.T
	root, home string
	fake       *execx.Fake
	answers    string
	uid        int
	terminal   bool
	gecos      string
	osRelease  string
	userErr    error
	signal     syscall.Signal
	stdout     bytes.Buffer
	stderr     bytes.Buffer
}

func newWorld(t *testing.T) *world {
	t.Helper()
	root := t.TempDir()
	w := &world{t: t, root: root, home: t.TempDir(), fake: &execx.Fake{}, uid: 1000, terminal: true, gecos: "Una \"U\" User"}
	w.osRelease = filepath.Join(root, "os-release")
	writeFile(t, w.osRelease, "NAME=\"Fedora Linux\"\nVERSION_ID=44\n")
	writeFile(t, filepath.Join(root, versionPath), "---\nfedora_version: 44\n")
	writeFile(t, filepath.Join(root, "requirements.yml"), "---\n")
	writeFile(t, filepath.Join(root, hostVarsPath+".dist"), "user_login: \"yourname\"\n")
	writeFile(t, filepath.Join(root, mainPlay), "---\n")
	writeFile(t, filepath.Join(root, commonDir, "play-qobuz.yml"), allPlay)
	writeFile(t, filepath.Join(root, commonDir, "play-rust.yml"), devPlay)
	writeFile(t, filepath.Join(root, commonDir, "play-videography.yml"), allPlay)
	writeFile(t, filepath.Join(root, hardwareDir, "play-nvidia.yml"), allPlay)
	f := w.fake
	f.On([]string{"sudo"}, execx.Result{})
	f.On([]string{"sudo", "-n", "-k", "true"}, execx.Result{ExitCode: 1, Stderr: []byte("sudo: a password is required\n")})
	f.On([]string{"pipx"}, execx.Result{})
	f.On([]string{"pipx", "list", "--json"}, execx.Result{Stdout: []byte(pipxListOut)})
	w.configSays(`[{"name":"DEFAULT_ASK_VAULT_PASS","value":false},{"GALAXY_SERVERS":{}},{"name":"` + vaultFileSetting + `","value":"` + w.path(vaultName) + `"}]`)
	f.On([]string{"ansible-galaxy"}, execx.Result{})
	f.On([]string{"ansible-playbook"}, execx.Result{})
	return w
}

func (w *world) configSays(out string) {
	w.fake.On([]string{"ansible-config", "dump", "--only-changed", "--format", "json"}, execx.Result{Stdout: []byte(out)})
}

func (w *world) path(rel string) string { return filepath.Join(w.root, rel) }

func (w *world) run(args ...string) term.ExitCode {
	w.t.Helper()
	env := Env{
		Runner: w.fake, Foreground: w.fake,
		Getenv: func(k string) string {
			return map[string]string{rootEnv: w.root, "HOME": w.home, "PATH": "/usr/bin"}[k]
		},
		UID: func() int { return w.uid },
		CurrentUser: func() (*user.User, error) {
			return &user.User{Username: "u", Name: w.gecos}, w.userErr
		},
		OSRelease:       w.osRelease,
		Random:          bytes.NewReader(bytes.Repeat([]byte{7}, 64)),
		OpenTTY:         func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("")), nil },
		Stdin:           strings.NewReader(w.answers),
		Stdout:          &w.stdout,
		Stderr:          &w.stderr,
		StdinIsTerminal: w.terminal,
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	if w.signal != 0 {
		cancel(&execx.SignalError{Signal: w.signal})
	}
	return App{Env: env}.Run(ctx, args)
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

// foregrounds is each foreground command's line.
func (w *world) foregrounds() []string {
	cmds := w.fake.Foregrounds()
	out := make([]string, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, c.Line())
	}
	return out
}

// recordOrder makes each of the named commands append its line to the returned slice when it
// runs, through Run or Foreground alike, so a test can see the order across both.
func (w *world) recordOrder(prefixes ...[]string) *[]string {
	order := &[]string{}
	for _, prefix := range prefixes {
		w.fake.OnFunc(prefix, func(c execx.Cmd) (execx.Result, error) {
			*order = append(*order, c.Line())
			return execx.Result{}, nil
		})
	}
	return order
}

func (w *world) marker() string {
	return filepath.Join(w.home, ".local/state/fedora-desktop-setup-complete")
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestFreshInstallRunsEveryStepWithNoGitHub(t *testing.T) {
	w := newWorld(t)
	w.answers = "\n"
	w.wantCode(w.run(), term.ExitOK)
	want := []string{
		"pipx install --include-deps ansible",
		"pipx inject ansible passlib",
		"ansible-galaxy install --keep-scm-meta -r requirements.yml",
		"ansible-playbook " + mainPlay + " --ask-become-pass",
	}
	if got := w.foregrounds(); !slices.Equal(got, want) {
		t.Fatalf("foreground %q\nwant %q", got, want)
	}
	if !w.fake.Called("sudo -n dnf -y install python3 python3-pip python3-libdnf5 jq openssl pipx") {
		t.Errorf("the base packages were not installed: %v", w.fake.Calls())
	}
	for _, c := range w.fake.Calls() {
		if c.Name == "gh" || c.Name == "ssh-keygen" || c.Name == "git" {
			t.Errorf("a consumer install ran %s", c.Line())
		}
	}
	for _, c := range w.fake.Foregrounds() {
		if c.Dir != w.root || !slices.Contains(c.Env, "PATH="+filepath.Join(w.home, ".local/bin")+":/usr/bin") {
			t.Errorf("%s ran in %q with %q", c.Line(), c.Dir, c.Env)
		}
	}
	w.wantHostVars(`user_login: "u"`, `user_name: "Una \"U\" User"`, `install_audience: consumer`, `github_accounts: {}`)
	w.wantRandomVault()
	if !exists(w.marker()) {
		t.Error("the setup-complete marker was not written")
	}
	w.wantErr("Qobuz", "Videography", "Nvidia (hardware)", "reboot")
	if strings.Contains(w.stderr.String(), "Rust") {
		t.Error("a developer play was offered")
	}
	if w.stdout.Len() != 0 {
		t.Errorf("stdout is not the payload: %q", w.stdout.String())
	}
}

// Plan 00229 Task 2.2: the system is upgraded before any Ansible process starts, so no Ansible
// process runs on an interpreter that an upgrade replaced under it.
func TestTheUpgradeFinishesBeforeAnyAnsibleProcessStarts(t *testing.T) {
	w := newWorld(t)
	w.answers = "\n"
	order := w.recordOrder(
		[]string{"sudo", "-n", "dnf"},
		[]string{"pipx", "install"},
		[]string{"ansible-galaxy"},
		[]string{"ansible-playbook"},
	)
	w.wantCode(w.run(), term.ExitOK)
	want := []string{
		"sudo -n dnf -y upgrade --refresh",
		"sudo -n dnf -y install python3 python3-pip python3-libdnf5 jq openssl pipx",
		"pipx install --include-deps ansible",
		"ansible-galaxy install --keep-scm-meta -r requirements.yml",
		"ansible-playbook " + mainPlay + " --ask-become-pass",
	}
	if !slices.Equal(*order, want) {
		t.Fatalf("order %q\nwant %q", *order, want)
	}
}

func TestAFailedUpgradeStopsTheRunBeforeAnsible(t *testing.T) {
	w := newWorld(t)
	w.fake.On([]string{"sudo", "-n", "dnf", "-y", "upgrade"}, execx.Result{ExitCode: 1, Stderr: []byte("no network\n")})
	w.wantCode(w.run(), term.ExitFailure)
	w.wantErr("dnf upgrade failed")
	if w.fake.Called("sudo -n dnf -y install") || len(w.fake.Foregrounds()) != 0 {
		t.Errorf("the run went on after the upgrade failed: %v %v", w.fake.Calls(), w.foregrounds())
	}
}

func (w *world) wantHostVars(lines ...string) {
	w.t.Helper()
	vars, err := os.ReadFile(w.path(hostVarsPath))
	if err != nil {
		w.t.Fatal(err)
	}
	for _, line := range lines {
		if !slices.Contains(strings.Split(string(vars), "\n"), line) {
			w.t.Errorf("host_vars lacks %q:\n%s", line, vars)
		}
	}
}

func (w *world) wantRandomVault() {
	t := w.t
	t.Helper()
	info, err := os.Stat(w.path(vaultName))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("vault password file %v %v", info, err)
	}
	pass, err := os.ReadFile(w.path(vaultName))
	if err != nil {
		t.Fatal(err)
	}
	if raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(pass))); err != nil || len(raw) != 32 {
		t.Fatalf("vault password %q is not 32 random bytes in base64 (%v)", pass, err)
	}
}

func TestReRunKeepsTheConsumerFilesAndInjectsOnlyWhatIsMissing(t *testing.T) {
	w := newWorld(t)
	writeFile(t, w.path(hostVarsPath), consumerYml)
	writeFile(t, w.path(vaultName), "kept\n")
	if err := os.MkdirAll(filepath.Join(w.home, ".local/share/pipx/venvs/ansible"), 0o750); err != nil {
		t.Fatal(err)
	}
	w.fake.On([]string{"pipx", "list", "--json"}, execx.Result{Stdout: []byte(`{"venvs":{"ansible":{"metadata":{"injected_packages":{"jmespath":{},"passlib":{}}}}}}`)})
	w.fake.On([]string{"sudo", "-n", "-k", "true"}, execx.Result{})
	w.answers = "\n"
	w.wantCode(w.run(), term.ExitOK)
	want := []string{
		"ansible-galaxy install --keep-scm-meta -r requirements.yml",
		"ansible-playbook " + mainPlay,
	}
	if got := w.foregrounds(); !slices.Equal(got, want) {
		t.Fatalf("foreground %q, want %q", got, want)
	}
	for rel, text := range map[string]string{hostVarsPath: consumerYml, vaultName: "kept\n"} {
		got, err := os.ReadFile(w.path(rel))
		if err != nil || string(got) != text {
			t.Errorf("%s changed: %q %v", rel, got, err)
		}
	}
}

func TestPicksRunInListOrderAfterABadAnswer(t *testing.T) {
	w := newWorld(t)
	w.answers = "9\n3 1\n"
	w.wantCode(w.run(), term.ExitOK)
	got := w.foregrounds()
	want := []string{
		"ansible-playbook " + commonDir + "/play-qobuz.yml --ask-become-pass",
		"ansible-playbook " + hardwareDir + "/play-nvidia.yml --ask-become-pass",
	}
	if !slices.Equal(got[len(got)-2:], want) {
		t.Fatalf("optional plays %q, want %q", got, want)
	}
	w.wantErr("Choose numbers from 1 to 3")
}

func TestAFailedOptionalPlayStopsTheRest(t *testing.T) {
	w := newWorld(t)
	w.fake.On([]string{"ansible-playbook", commonDir + "/play-qobuz.yml"}, execx.Result{ExitCode: 2})
	w.answers = "1 2\n"
	w.wantCode(w.run(), term.ExitFailure)
	if slices.Contains(w.foregrounds(), "ansible-playbook "+commonDir+"/play-videography.yml --ask-become-pass") {
		t.Error("a play ran after one failed")
	}
	if exists(w.marker()) {
		t.Error("the marker was written after a failure")
	}
	w.wantErr("Qobuz failed (exit 2)", issuesURL)
}

func TestAFailedMainPlayStopsBeforeTheMenu(t *testing.T) {
	w := newWorld(t)
	w.fake.On([]string{"ansible-playbook", mainPlay}, execx.Result{ExitCode: 2})
	w.wantCode(w.run(), term.ExitFailure)
	w.wantErr("The main install failed (exit 2)", issuesURL)
	if strings.Contains(w.stderr.String(), "Qobuz") || exists(w.marker()) {
		t.Error("the run went on after the main play failed")
	}
}

func TestEachEarlierStepFailingStopsTheRun(t *testing.T) {
	for _, tc := range []struct {
		name   string
		prefix []string
		want   string
	}{
		{"sudo", []string{"sudo", "-v"}, "sudo"},
		{"dnf", []string{"sudo", "-n", "dnf"}, "dnf"},
		{"pipx install", []string{"pipx", "install"}, "pipx install"},
		{"pipx list", []string{"pipx", "list"}, "pipx list"},
		{"pipx inject", []string{"pipx", "inject"}, "pipx inject"},
		{"ansible-config", []string{"ansible-config"}, "ansible-config"},
		{"galaxy", []string{"ansible-galaxy"}, "ansible-galaxy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			if tc.name == "sudo" {
				w.fake.On([]string{"sudo", "-n", "-v"}, execx.Result{ExitCode: 1})
				w.fake.On([]string{"sudo", "-n", "true"}, execx.Result{ExitCode: 1})
			}
			w.fake.On(tc.prefix, execx.Result{ExitCode: 3, Stderr: []byte("broken\n")})
			w.wantCode(w.run(), term.ExitFailure)
			w.wantErr(tc.want)
			if slices.Contains(w.foregrounds(), "ansible-playbook "+mainPlay+" --ask-become-pass") {
				t.Error("the main play ran after an earlier step failed")
			}
		})
	}
}

func TestOutputThatIsNotJSONStopsTheRun(t *testing.T) {
	w := newWorld(t)
	w.fake.On([]string{"pipx", "list", "--json"}, execx.Result{Stdout: []byte("nope")})
	w.wantCode(w.run(), term.ExitFailure)
	w.wantErr("pipx list")

	w = newWorld(t)
	w.configSays("nope")
	w.wantCode(w.run(), term.ExitFailure)
	w.wantErr("ansible-config")
}

func TestAnsibleNamingNoVaultPasswordFileStopsTheRun(t *testing.T) {
	w := newWorld(t)
	w.configSays(`[{"name":"DEFAULT_ASK_VAULT_PASS","value":false}]`)
	w.wantCode(w.run(), term.ExitFailure)
	w.wantErr(vaultFileSetting)
	if slices.Contains(w.foregrounds(), "ansible-playbook "+mainPlay+" --ask-become-pass") {
		t.Error("the main play ran with no vault password file")
	}
}

func TestOptionalOnlySkipsTheMainInstall(t *testing.T) {
	w := newWorld(t)
	writeFile(t, w.path(hostVarsPath), consumerYml)
	writeFile(t, w.path(vaultName), "kept\n")
	w.answers = "2\n"
	w.wantCode(w.run("--optional-only=true"), term.ExitOK)
	want := []string{"ansible-playbook " + commonDir + "/play-videography.yml --ask-become-pass"}
	if got := w.foregrounds(); !slices.Equal(got, want) {
		t.Fatalf("foreground %q, want %q", got, want)
	}
	if w.fake.Called("sudo -n dnf") {
		t.Error("--optional-only installed packages")
	}
}

func TestOptionalOnlyBeforeAnyInstallIsRefused(t *testing.T) {
	w := newWorld(t)
	w.wantCode(w.run("--optional-only=true"), term.ExitFailure)
	w.wantErr("./run.bash --consumer")
	if exists(w.path(hostVarsPath)) || len(w.fake.Calls()) != 0 {
		t.Error("--optional-only on a fresh box changed something")
	}
}

func TestOptionalOnlyWithNoVaultPasswordIsRefused(t *testing.T) {
	w := newWorld(t)
	writeFile(t, w.path(hostVarsPath), consumerYml)
	w.wantCode(w.run("--optional-only"), term.ExitFailure)
	w.wantErr(vaultName)
	if exists(w.path(vaultName)) {
		t.Error("--optional-only made a new vault password")
	}
}

func TestADeveloperBoxIsRefused(t *testing.T) {
	w := newWorld(t)
	writeFile(t, w.path(hostVarsPath), "user_login: \"u\"\ninstall_audience: developer\n")
	w.wantCode(w.run(), term.ExitFailure)
	w.wantErr("developer install")
	if len(w.fake.Calls()) != 0 || exists(w.path(vaultName)) {
		t.Error("a developer box was changed")
	}
}

func TestRefusals(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*world)
		want  string
	}{
		{"root", func(w *world) { w.uid = 0 }, "not as root"},
		{"no terminal", func(w *world) { w.terminal = false }, "terminal"},
		{"other Fedora", func(w *world) { writeFile(t, w.osRelease, "VERSION_ID=43\n") }, "Fedora 43"},
		{"not Fedora", func(w *world) { writeFile(t, w.osRelease, "NAME=x\n") }, "VERSION_ID"},
		{"no os-release", func(w *world) { w.osRelease = filepath.Join(w.root, "absent") }, "absent"},
		{"no version file", func(w *world) { removeFile(t, w.path(versionPath)) }, versionPath},
		{"bad version file", func(w *world) { writeFile(t, w.path(versionPath), "fedora_version: x\n") }, versionPath},
		{"version file not YAML", func(w *world) { writeFile(t, w.path(versionPath), "[\n") }, versionPath},
		{"no fedora_version", func(w *world) { writeFile(t, w.path(versionPath), "x: 1\n") }, "fedora_version"},
		{"no user", func(w *world) { w.userErr = errors.New("no passwd entry") }, "no passwd entry"},
		{"user name with a newline", func(w *world) { w.gecos = "a\nb" }, "user name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			tc.setup(w)
			w.wantCode(w.run(), term.ExitFailure)
			w.wantErr(tc.want)
			if len(w.fake.Calls()) != 0 {
				t.Errorf("ran %v before refusing", w.fake.Calls())
			}
		})
	}
}

func removeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

func TestAnEmptyGECOSUsesTheLogin(t *testing.T) {
	w := newWorld(t)
	w.gecos = ""
	w.answers = "\n"
	w.wantCode(w.run(), term.ExitOK)
	vars, err := os.ReadFile(w.path(hostVarsPath))
	if err != nil || !strings.Contains(string(vars), "user_name: \"u\"\n") {
		t.Fatalf("host_vars %q %v", vars, err)
	}
}

func TestTheRootDirectoryMustBeSet(t *testing.T) {
	w := newWorld(t)
	w.root = ""
	w.wantCode(w.run(), term.ExitFailure)
	w.wantErr(rootEnv)
}

func TestUsage(t *testing.T) {
	w := newWorld(t)
	w.wantCode(w.run("--help"), term.ExitOK)
	if !strings.Contains(w.stdout.String(), "Usage: consumer-install") {
		t.Errorf("help %q", w.stdout.String())
	}
	for _, args := range [][]string{{"extra"}, {"--optional-only=maybe"}, {"--optional-only", "--optional-only"}, {"--optional-only=false", "--help"}} {
		w := newWorld(t)
		w.wantCode(w.run(args...), term.ExitUsage)
	}
}

func TestTheInputEndingAtTheMenuIsACancel(t *testing.T) {
	w := newWorld(t)
	w.answers = ""
	w.wantCode(w.run(), term.ExitCancelled)
	w.wantErr("input ended")
	if exists(w.marker()) {
		t.Error("the marker was written with no answer")
	}
}

func TestAPlayEndedBySignalIsTheShellsCode(t *testing.T) {
	w := newWorld(t)
	w.fake.On([]string{"ansible-playbook", mainPlay}, execx.Result{ExitCode: 130})
	w.wantCode(w.run(), term.ExitCode(130))
}

func TestASignalBeforeTheRunIsItsCode(t *testing.T) {
	w := newWorld(t)
	w.signal = syscall.SIGTERM
	w.wantCode(w.run(), term.ExitCode(128+int(syscall.SIGTERM)))
}

func TestNewEnvIsTheRealMachine(t *testing.T) {
	env := NewEnv()
	if env.Runner == nil || env.Foreground == nil || env.Getenv == nil || env.UID == nil || env.CurrentUser == nil ||
		env.OSRelease != "/etc/os-release" || env.Random == nil || env.OpenTTY == nil || env.Stdin == nil {
		t.Fatalf("NewEnv left a seam empty: %+v", env)
	}
}
