package consumer

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
	// password is the login password the tests type; become is how a play line shows the file
	// that hands it to Ansible (its random path normalised by foregrounds).
	password    = "s3cr3t-placeholder"
	become      = " --become-password-file PWFILE"
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
	runtime    string
	// typed is what is typed at the password question, before answers; echo counts the
	// terminal's echo turned off and put back; handed is the password file as each play saw it.
	typed     string
	answers   string
	echoOff   int
	echoBack  int
	handed    []string
	uid       int
	terminal  bool
	gecos     string
	osRelease string
	userErr   error
	signal    syscall.Signal
	cancel    context.CancelCauseFunc
	stdout    bytes.Buffer
	stderr    bytes.Buffer
}

func newWorld(t *testing.T) *world {
	t.Helper()
	root := t.TempDir()
	w := &world{t: t, root: root, home: t.TempDir(), runtime: t.TempDir(), typed: password + "\n", fake: &execx.Fake{}, uid: 1000, terminal: true, gecos: "Una \"U\" User"}
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
	f.OnFunc([]string{"ansible-playbook"}, func(c execx.Cmd) (execx.Result, error) {
		w.seeBecomeFile(c)
		return execx.Result{}, nil
	})
	return w
}

// passwordless is a box whose sudo needs no password: nothing is asked.
func (w *world) passwordless() {
	w.fake.On([]string{"sudo", "-n", "-k", "true"}, execx.Result{})
	w.typed = ""
}

// seeBecomeFile records the password file a play is handed as the play finds it: its text and
// the modes of the file and its directory.
func (w *world) seeBecomeFile(c execx.Cmd) {
	i := slices.Index(c.Args, "--become-password-file")
	if i < 0 || i+1 >= len(c.Args) {
		return
	}
	file := c.Args[i+1]
	dir, fi, err := statBoth(file)
	if err != nil {
		w.handed = append(w.handed, err.Error())
		return
	}
	text, err := fs.ReadFile(os.DirFS(filepath.Dir(file)), filepath.Base(file))
	if err != nil {
		w.handed = append(w.handed, err.Error())
		return
	}
	w.handed = append(w.handed, fmt.Sprintf("%s %v %v %v", text, fi.Mode().Perm(), dir.Mode().Perm(), filepath.Dir(filepath.Dir(file)) == w.runtime))
}

func statBoth(file string) (os.FileInfo, os.FileInfo, error) {
	dir, err := os.Stat(filepath.Dir(file))
	if err != nil {
		return nil, nil, err
	}
	fi, err := os.Stat(file)
	return dir, fi, err
}

// wantNoPasswordLeft fails when anything of the run is left in the runtime directory.
func (w *world) wantNoPasswordLeft() {
	w.t.Helper()
	left, err := os.ReadDir(w.runtime)
	if err != nil || len(left) != 0 {
		w.t.Fatalf("the runtime directory holds %v (%v) after the run", left, err)
	}
}

// wantPasswordNowhere fails when the password is in any command's argv or environment.
func (w *world) wantPasswordNowhere() {
	w.t.Helper()
	for _, c := range append(w.fake.Calls(), w.fake.Foregrounds()...) {
		if strings.Contains(c.Line()+strings.Join(c.Env, " "), password) {
			w.t.Errorf("the password is in %s", c.Line())
		}
	}
	if strings.Contains(w.stderr.String()+w.stdout.String(), password) {
		w.t.Error("the password was printed")
	}
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
			return map[string]string{rootEnv: w.root, "HOME": w.home, "PATH": "/usr/bin", "XDG_RUNTIME_DIR": w.runtime}[k]
		},
		EchoOff: func() (func() error, error) {
			w.echoOff++
			return func() error { w.echoBack++; return nil }, nil
		},
		UID: func() int { return w.uid },
		CurrentUser: func() (*user.User, error) {
			return &user.User{Username: "u", Name: w.gecos}, w.userErr
		},
		OSRelease:       w.osRelease,
		Random:          bytes.NewReader(bytes.Repeat([]byte{7}, 64)),
		OpenTTY:         func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("")), nil },
		Stdin:           strings.NewReader(w.typed + w.answers),
		Stdout:          &w.stdout,
		Stderr:          &w.stderr,
		StdinIsTerminal: w.terminal,
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	w.cancel = cancel
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
		args := slices.Clone(c.Args)
		if i := slices.Index(args, "--become-password-file"); i >= 0 && i+1 < len(args) {
			args[i+1] = "PWFILE"
		}
		out = append(out, strings.Join(append([]string{c.Name}, args...), " "))
	}
	return out
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
		"ansible-playbook " + mainPlay + become,
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
	w.passwordless()
	w.answers = "\n"
	w.wantCode(w.run(), term.ExitOK)
	if w.echoOff != 0 || w.fake.Called("sudo -k -S") {
		t.Error("a passwordless sudo was asked for a password")
	}
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
		"ansible-playbook " + commonDir + "/play-qobuz.yml" + become,
		"ansible-playbook " + hardwareDir + "/play-nvidia.yml" + become,
	}
	if !slices.Equal(got[len(got)-2:], want) {
		t.Fatalf("optional plays %q, want %q", got, want)
	}
	w.wantErr("Choose numbers from 1 to 3")
}

// optionalLines is the ansible-playbook line of each optional play, by label.
var optionalLines = map[string]string{
	"Qobuz":       "ansible-playbook " + commonDir + "/play-qobuz.yml" + become,
	"Videography": "ansible-playbook " + commonDir + "/play-videography.yml" + become,
	"Nvidia":      "ansible-playbook " + hardwareDir + "/play-nvidia.yml" + become,
}

func (w *world) wantOptionalRan(labels ...string) {
	w.t.Helper()
	var got []string
	for _, line := range w.foregrounds() {
		if strings.HasPrefix(line, "ansible-playbook playbooks/imports/optional/") {
			got = append(got, line)
		}
	}
	want := make([]string, 0, len(labels))
	for _, l := range labels {
		want = append(want, optionalLines[l])
	}
	if !slices.Equal(got, want) {
		w.t.Fatalf("optional plays ran %q, want %q", got, want)
	}
}

// retryLine is the command the summary gives to run one play again, in a test's checkout.
func (w *world) retryLine(dir, file string) string {
	return filepath.Join(w.root, "run.bash") + " " + dir + "/" + file
}

func TestAFailedOptionalPlayDoesNotStopTheRest(t *testing.T) {
	w := newWorld(t)
	w.fake.On([]string{"ansible-playbook", commonDir + "/play-qobuz.yml"}, execx.Result{ExitCode: 2})
	w.answers = "1 2 3\n"
	w.wantCode(w.run(), term.ExitFailure)
	w.wantOptionalRan("Qobuz", "Videography", "Nvidia")
	if exists(w.marker()) {
		t.Error("the marker was written after a failure")
	}
	w.wantErr(
		"✗ Qobuz: failed (exit 2); its output is above, in this terminal (it is not saved to a file)",
		"✓ Videography: installed",
		"✓ Nvidia (hardware): installed",
		w.retryLine(commonDir, "play-qobuz.yml"),
		"run.bash --optional-only",
		issuesURL,
		"1 of 3 optional apps failed",
	)
	if strings.Contains(w.stderr.String(), w.retryLine(commonDir, "play-videography.yml")) {
		t.Error("a play that passed was offered for a retry")
	}
}

func TestEveryFailedOptionalPlayIsCountedAndOfferedAgain(t *testing.T) {
	w := newWorld(t)
	w.fake.On([]string{"ansible-playbook", commonDir + "/play-qobuz.yml"}, execx.Result{ExitCode: 2})
	w.fake.OnError([]string{"ansible-playbook", hardwareDir + "/play-nvidia.yml"}, errors.New("no such file"))
	w.answers = "1 2 3\n"
	w.wantCode(w.run(), term.ExitFailure)
	w.wantOptionalRan("Qobuz", "Videography", "Nvidia")
	w.wantErr(
		"✗ Qobuz: failed (exit 2)",
		"✗ Nvidia (hardware): could not run (",
		"no such file",
		w.retryLine(commonDir, "play-qobuz.yml"),
		w.retryLine(hardwareDir, "play-nvidia.yml"),
		"2 of 3 optional apps failed",
	)
}

func TestEveryOptionalPlayPassingIsSummedUp(t *testing.T) {
	w := newWorld(t)
	w.answers = "1 3\n"
	w.wantCode(w.run(), term.ExitOK)
	w.wantOptionalRan("Qobuz", "Nvidia")
	w.wantErr("✓ Qobuz: installed", "✓ Nvidia (hardware): installed", "reboot")
	if strings.Contains(w.stderr.String(), "Videography") && strings.Contains(w.stderr.String(), "not run") {
		t.Error("a play not chosen is in the summary")
	}
	if strings.Contains(w.stderr.String(), "failed") || !exists(w.marker()) {
		t.Errorf("a clean run was not reported clean:\n%s", w.stderr.String())
	}
}

func TestACtrlCDuringAnOptionalPlayStopsAtOnce(t *testing.T) {
	w := newWorld(t)
	w.fake.OnFunc([]string{"ansible-playbook", commonDir + "/play-qobuz.yml"}, func(execx.Cmd) (execx.Result, error) {
		w.cancel(&execx.SignalError{Signal: syscall.SIGINT})
		return execx.Result{ExitCode: 130}, nil
	})
	w.answers = "1 2 3\n"
	w.wantCode(w.run(), term.ExitCode(130))
	w.wantOptionalRan("Qobuz")
	w.wantErr("✗ Qobuz: stopped part way (exit 130)", "- Videography: not run", "- Nvidia (hardware): not run",
		w.retryLine(commonDir, "play-videography.yml"))
	if exists(w.marker()) {
		t.Error("the marker was written after a stop")
	}
}

func TestAnOptionalPlayEndedBySignalStopsTheRest(t *testing.T) {
	w := newWorld(t)
	w.fake.On([]string{"ansible-playbook", commonDir + "/play-videography.yml"}, execx.Result{ExitCode: 143})
	w.answers = "1 2 3\n"
	w.wantCode(w.run(), term.ExitCode(143))
	w.wantOptionalRan("Qobuz", "Videography")
	w.wantErr("✓ Qobuz: installed", "✗ Videography: stopped part way (exit 143)", "- Nvidia (hardware): not run")
}

// checks is how many times sudo was asked whether a password is right.
func (w *world) checks() int {
	n := 0
	for _, c := range w.fake.Calls() {
		if strings.HasPrefix(c.Line(), "sudo -k -S") {
			n++
		}
	}
	return n
}

func TestThePasswordIsAskedOnceForTheMainPlayAndEveryOptionalOne(t *testing.T) {
	w := newWorld(t)
	w.answers = "1 2 3\n"
	w.wantCode(w.run(), term.ExitOK)
	w.wantOptionalRan("Qobuz", "Videography", "Nvidia")
	if w.echoOff != 1 || w.echoBack != 1 || w.checks() != 1 {
		t.Fatalf("asked %d times (echo back %d), checked %d times", w.echoOff, w.echoBack, w.checks())
	}
	for _, line := range w.foregrounds() {
		if strings.HasPrefix(line, "ansible-playbook") && !strings.HasSuffix(line, become) || strings.Contains(line, "--ask-become-pass") {
			t.Errorf("a play was not handed the password file: %s", line)
		}
	}
	want := slices.Repeat([]string{password + " -rw------- -rwx------ true"}, 4)
	if !slices.Equal(w.handed, want) {
		t.Fatalf("the plays found %q, want %q", w.handed, want)
	}
	w.wantPasswordNowhere()
	w.wantNoPasswordLeft()
}

func TestAWrongPasswordIsAskedAgain(t *testing.T) {
	w := newWorld(t)
	w.fake.OnFunc([]string{"sudo", "-k", "-S"}, func(c execx.Cmd) (execx.Result, error) {
		if c.Feed == nil || !c.Feed.Secret.Is(password) {
			return execx.Result{ExitCode: 1, Stderr: []byte("Sorry, try again.\nsudo: 1 incorrect password attempt\n")}, nil
		}
		return execx.Result{}, nil
	})
	w.typed = "wrong\n\n" + password + "\n"
	w.answers = "1\n"
	w.wantCode(w.run(), term.ExitOK)
	if w.echoOff != 3 || w.echoBack != 3 || w.checks() != 2 {
		t.Fatalf("asked %d times (echo back %d), checked %d times", w.echoOff, w.echoBack, w.checks())
	}
	w.wantErr("sudo did not accept that password", "type your password")
	if want := slices.Repeat([]string{password + " -rw------- -rwx------ true"}, 2); !slices.Equal(w.handed, want) {
		t.Fatalf("the plays found %q, want %q", w.handed, want)
	}
	w.wantPasswordNowhere()
	w.wantNoPasswordLeft()
}

func TestThreeWrongPasswordsStopBeforeAnythingIsInstalled(t *testing.T) {
	w := newWorld(t)
	w.fake.On([]string{"sudo", "-k", "-S"}, execx.Result{ExitCode: 1, Stderr: []byte("Sorry, try again.\n")})
	w.typed = "a\nb\nc\n"
	w.wantCode(w.run(), term.ExitFailure)
	w.wantErr("not accepted 3 times")
	if len(w.foregrounds()) != 0 || w.fake.Called("sudo -n dnf") {
		t.Errorf("ran %q after the password was refused", w.foregrounds())
	}
	w.wantNoPasswordLeft()
}

func TestNoRuntimeDirectoryStopsBeforeThePasswordIsAsked(t *testing.T) {
	w := newWorld(t)
	w.runtime = ""
	w.wantCode(w.run(), term.ExitFailure)
	w.wantErr("XDG_RUNTIME_DIR")
	if w.echoOff != 0 || len(w.foregrounds()) != 0 {
		t.Error("the password was asked with nowhere private to keep it")
	}
}

func TestThePasswordFileIsGoneAfterAFailureAndAStop(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*world)
		code  term.ExitCode
	}{
		{"main play failed", func(w *world) {
			w.fake.On([]string{"ansible-playbook", mainPlay}, execx.Result{ExitCode: 2})
		}, term.ExitFailure},
		{"optional play failed", func(w *world) {
			w.fake.On([]string{"ansible-playbook", commonDir + "/play-qobuz.yml"}, execx.Result{ExitCode: 2})
		}, term.ExitFailure},
		{"Ctrl-C in a play", func(w *world) {
			w.fake.OnFunc([]string{"ansible-playbook", commonDir + "/play-qobuz.yml"}, func(execx.Cmd) (execx.Result, error) {
				w.cancel(&execx.SignalError{Signal: syscall.SIGINT})
				return execx.Result{ExitCode: 130}, nil
			})
		}, term.ExitCode(130)},
		{"input ended at the menu", func(w *world) { w.answers = "" }, term.ExitCancelled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			w.answers = "1 2\n"
			tc.setup(w)
			w.wantCode(w.run(), tc.code)
			if len(w.handed) == 0 && tc.name != "input ended at the menu" && tc.name != "main play failed" {
				t.Error("no play was handed the password file")
			}
			w.wantNoPasswordLeft()
		})
	}
}

func TestRetryCommandAbbreviatesTheHome(t *testing.T) {
	got := retryCommand("/home/u/Projects/fedora-desktop", "/home/u", commonDir+"/play-qobuz.yml")
	if want := "~/Projects/fedora-desktop/run.bash " + commonDir + "/play-qobuz.yml"; got != want {
		t.Errorf("retry %q, want %q", got, want)
	}
	got = retryCommand("/srv/a checkout", "/home/u", "--optional-only")
	if want := "'/srv/a checkout/run.bash' --optional-only"; got != want {
		t.Errorf("retry %q, want %q", got, want)
	}
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
			if slices.Contains(w.foregrounds(), "ansible-playbook "+mainPlay+become) {
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
	if slices.Contains(w.foregrounds(), "ansible-playbook "+mainPlay+become) {
		t.Error("the main play ran with no vault password file")
	}
}

func TestOptionalOnlySkipsTheMainInstall(t *testing.T) {
	w := newWorld(t)
	writeFile(t, w.path(hostVarsPath), consumerYml)
	writeFile(t, w.path(vaultName), "kept\n")
	w.answers = "2\n"
	w.wantCode(w.run("--optional-only=true"), term.ExitOK)
	want := []string{"ansible-playbook " + commonDir + "/play-videography.yml" + become}
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
