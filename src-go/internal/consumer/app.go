// Package consumer is the consumer-install command (Plan 00228): the install for someone who
// does not write code, started by `./run.bash --consumer`. It needs no GitHub account, no SSH
// key and no vault password typed in: host_vars says install_audience consumer (so every
// developer play ends itself), the vault password is made at random and kept only on this
// machine, Ansible is installed with pipx, the main playbook runs, and the optional plays for
// everyone (apps such as Qobuz, no programming tools) are offered as a numbered menu.
//
// Every program runs through execx, root through internal/sudo, and every question through
// ui.Lines. Each step up to the main playbook fails fast: the first failure ends the run with
// exit 1, a child ended by a signal with its own 128+n. The optional plays chosen are
// independent, so a failed one is recorded and the next runs; a summary of every chosen play
// follows, and the run exits 1 if any failed.
package consumer

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/shellword"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sudo"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ttyecho"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

const (
	tool         = "consumer-install"
	rootEnv      = "CONSUMER_INSTALL_ROOT"
	hostVarsPath = "environment/localhost/host_vars/localhost.yml"
	versionPath  = "vars/fedora-version.yml"
	mainPlay     = "playbooks/playbook-main.yml"
	issuesURL    = "https://github.com/LongTermSupport/fedora-desktop/issues"
	consumerLine = "install_audience: consumer"
	// vaultFileSetting is the ansible-config name of the file ansible.cfg points the vault at.
	vaultFileSetting = "DEFAULT_VAULT_PASSWORD_FILE"
	vaultBytes       = 32
	headingWidth     = 80
)

// basePackages is what Ansible needs before its first play.
var basePackages = []string{"python3", "python3-pip", "python3-libdnf5", "jq", "openssl", pipx}

// pipx installs Ansible into its own venv, named ansibleVenv; install is the verb dnf, pipx
// and ansible-galaxy share.
const (
	pipx        = "pipx"
	ansibleVenv = "ansible"
	install     = "install"
)

// ansibleDeps are injected into pipx's ansible venv when it lacks them.
var ansibleDeps = []string{"jmespath", "passlib"}

const usageText = `Usage: consumer-install [--optional-only[=true|false]]

The install for someone who does not write code: no GitHub account, no SSH key, no
programming tools. It writes host_vars for a consumer install (once), makes the vault
password at random (once, kept only on this machine), installs Ansible, runs the main
playbook, then offers the optional apps.

Run it as ./run.bash --consumer, which installs Go and git, fetches the checkout and runs
this through scripts/consumer-install.bash.

Options:
  --optional-only   Skip the main install; only offer the optional apps
  -h, --help        Show this help
`

// Env is everything a run touches outside its own memory and the checkout's files.
type Env struct {
	Runner      execx.Runner
	Foreground  execx.Foregrounder
	Getenv      func(string) string
	UID         func() int
	CurrentUser func() (*user.User, error)
	OSRelease   string
	Random      io.Reader
	OpenTTY     func() (io.ReadCloser, error)
	// EchoOff turns the terminal's echo off while the password is typed (ttyecho.Off).
	EchoOff         func() (restore func() error, err error)
	Stdin           io.Reader
	Stdout, Stderr  io.Writer
	StdinIsTerminal bool
}

// NewEnv is the production environment.
func NewEnv() Env {
	return Env{
		Runner: execx.OS{}, Foreground: execx.OS{}, Getenv: os.Getenv, UID: os.Getuid,
		CurrentUser: user.Current, OSRelease: "/etc/os-release", Random: rand.Reader,
		OpenTTY: openTTY, EchoOff: ttyecho.Off(int(os.Stdin.Fd())), Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		StdinIsTerminal: term.FileIsTerminal(os.Stdin),
	}
}

func openTTY() (io.ReadCloser, error) {
	f, err := os.Open("/dev/tty")
	if err != nil {
		return nil, fmt.Errorf("open /dev/tty: %w", err)
	}
	return f, nil
}

// App is one consumer-install invocation.
type App struct{ Env Env }

// errReported is a failure already explained on stderr.
var errReported = errors.New("failure reported")

// exitError is a run that ends with a child's own code: one ended by a signal.
type exitError struct{ code term.ExitCode }

func (e *exitError) Error() string { return "exit " + strconv.Itoa(int(e.code)) }

type run struct {
	env          Env
	p            term.Printer
	lines        *ui.Lines
	sudo         *sudo.Sudo
	optionalOnly bool
	root, home   string
	path         string
	become       []string
	// becomeDir is the private directory holding the password file every play is handed;
	// empty when sudo needs no password. Run removes it however the run ends.
	becomeDir string
}

// Run is the whole command; it returns the exit code.
func (a App) Run(ctx context.Context, args []string) term.ExitCode {
	r := &run{env: a.Env}
	r.lines = &ui.Lines{In: a.Env.Stdin, Out: a.Env.Stderr, Width: headingWidth, EchoOff: a.Env.EchoOff}
	help, err := r.parse(args)
	switch {
	case err != nil:
		r.p.Print(a.Env.Stderr, tool+": "+err.Error()+" — see '"+tool+" --help'.\n")
		return term.ExitUsage
	case help:
		r.p.Print(a.Env.Stdout, usageText)
		return r.finish(term.ExitOK)
	}
	err = r.main(ctx)
	if dropErr := r.dropBecomeFile(); dropErr != nil {
		r.bad(dropErr.Error())
		if err == nil {
			err = errReported
		}
	}
	return r.finish(r.exitFor(ctx, err))
}

func (r *run) parse(args []string) (bool, error) {
	seen := false
	for _, arg := range args {
		value, isFlag := strings.CutPrefix(arg, "--optional-only")
		switch {
		case (arg == "-h" || arg == "--help") && len(args) == 1:
			return true, nil
		case !isFlag || seen:
			return false, fmt.Errorf("unexpected argument %q", arg)
		case value == "" || value == "=true":
			r.optionalOnly = true
		case value == "=false":
			r.optionalOnly = false
		default:
			return false, fmt.Errorf("--optional-only takes true or false, not %q", strings.TrimPrefix(value, "="))
		}
		seen = true
	}
	return false, nil
}

func (r *run) finish(code term.ExitCode) term.ExitCode {
	if r.p.Err() != nil && code == term.ExitOK {
		return term.ExitFailure
	}
	return code
}

func (r *run) exitFor(ctx context.Context, err error) term.ExitCode {
	if se, ok := errors.AsType[*execx.SignalError](context.Cause(ctx)); ok {
		return term.ExitCode(se.ExitCode())
	}
	if ee, ok := errors.AsType[*exitError](err); ok {
		return ee.code
	}
	if err == nil {
		return term.ExitOK
	}
	switch {
	case errors.Is(err, ui.ErrInputEnded):
		// The person left the question: a cancel (ui.ErrInputEnded's contract), not a failure.
		r.bad("The input ended before an answer; the install stopped there")
		return term.ExitCancelled
	case !errors.Is(err, errReported):
		r.bad(err.Error())
	}
	return term.ExitFailure
}

func (r *run) main(ctx context.Context) error {
	if err := r.preflight(); err != nil {
		return err
	}
	if err := r.hostVars(); err != nil {
		return err
	}
	if r.optionalOnly {
		if err := r.vaultExists(ctx); err != nil {
			return err
		}
	}
	if err := r.becomeOnce(ctx); err != nil {
		return err
	}
	if !r.optionalOnly {
		if err := r.install(ctx); err != nil {
			return err
		}
	}
	if !r.optionalOnly {
		r.step("The main install")
		if err := r.play(ctx, mainPlay, "The main install"); err != nil {
			return err
		}
	}
	if err := r.optional(ctx); err != nil {
		return err
	}
	return r.done()
}

// --- output ---

func (r *run) mark(symbol, text string) { r.p.Print(r.env.Stderr, symbol+" "+text+"\n") }
func (r *run) ok(text string)           { r.mark("✓", text) }
func (r *run) info(text string)         { r.mark("i", text) }
func (r *run) bad(text string)          { r.mark("✗", text) }
func (r *run) plain(text string)        { r.p.Print(r.env.Stderr, text+"\n") }
func (r *run) step(name string)         { r.p.Print(r.env.Stderr, "\n━━━ "+name+" ━━━\n") }

func (r *run) failf(format string, args ...any) error {
	r.bad(fmt.Sprintf(format, args...))
	return errReported
}

// --- preflight: who, where, which Fedora ---

func (r *run) preflight() error {
	if r.env.UID() == 0 {
		return r.failf("Run this as your normal user, not as root")
	}
	if !r.env.StdinIsTerminal {
		return r.failf("This install asks questions, so it needs a terminal to type in")
	}
	root := r.env.Getenv(rootEnv)
	if fi, err := os.Stat(root); root == "" || !filepath.IsAbs(root) || err != nil || !fi.IsDir() {
		r.bad(fmt.Sprintf("%s is not this checkout's directory ('%s')", rootEnv, root))
		r.plain("   Run ./run.bash --consumer, which sets it")
		return errReported
	}
	home := r.env.Getenv("HOME")
	if !filepath.IsAbs(home) {
		return r.failf("HOME is not an absolute directory ('%s')", home)
	}
	r.root, r.home = root, home
	r.path = "PATH=" + filepath.Join(home, ".local/bin") + ":" + r.env.Getenv("PATH")
	r.sudo = &sudo.Sudo{Runner: r.env.Runner, Term: &term.Term{Err: r.env.Stderr, IsTerminal: func(any) bool { return false }, Getenv: r.env.Getenv}, Stdin: r.env.Stdin, Stdout: r.env.Stderr, OpenTTY: r.env.OpenTTY}
	return r.checkFedora()
}

func (r *run) checkFedora() error {
	data, err := os.ReadFile(r.env.OSRelease)
	if err != nil {
		return r.failf("Cannot read %v: this installs Fedora Workstation only", err)
	}
	running := ""
	for line := range strings.SplitSeq(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "VERSION_ID="); ok {
			running = strings.Trim(v, `"`)
		}
	}
	if running == "" {
		return r.failf("%s has no VERSION_ID: this installs Fedora Workstation only", r.env.OSRelease)
	}
	wanted, err := r.wantedFedora()
	if err != nil {
		return r.failf("%s: %v", versionPath, err)
	}
	if running != strconv.Itoa(wanted) {
		return r.failf("This is Fedora %s, but this checkout installs Fedora %d; use the F%s branch", running, wanted, running)
	}
	r.ok("Fedora " + running)
	return nil
}

func (r *run) wantedFedora() (int, error) {
	data, err := os.ReadFile(filepath.Join(r.root, versionPath))
	if err != nil {
		return 0, err
	}
	var v map[string]yaml.Node
	if err := yaml.Unmarshal(data, &v); err != nil {
		return 0, fmt.Errorf("not a YAML mapping: %w", err)
	}
	node, ok := v["fedora_version"]
	if !ok {
		return 0, errors.New("fedora_version is not set")
	}
	var n int
	if err := node.Decode(&n); err != nil {
		return 0, fmt.Errorf("fedora_version is not an integer: %w", err)
	}
	return n, nil
}

// --- host_vars (written once; a developer box is refused) ---

// consumerVars is a fresh consumer host_vars; each value is YAML double-quoted (Go's quoting
// escapes are a subset of YAML's).
func consumerVars(login, name string) []byte {
	return []byte("# A consumer install (./run.bash --consumer): no GitHub account, no developer plays.\n" +
		"user_login: " + strconv.Quote(login) + "\n" +
		"user_name: " + strconv.Quote(name) + "\n" +
		consumerLine + "\n" +
		"github_accounts: {}\n")
}

func (r *run) hostVars() error {
	file := filepath.Join(r.root, hostVarsPath)
	data, err := os.ReadFile(file)
	switch {
	case err == nil:
		if slices.Contains(strings.Split(string(data), "\n"), consumerLine) {
			r.ok("host_vars is set up for a consumer install (kept)")
			return nil
		}
		return r.failf("%s has no '%s' line: this is a developer install. Run ./run.bash without --consumer", hostVarsPath, consumerLine)
	case !errors.Is(err, os.ErrNotExist):
		return r.failf("Cannot read %s: %v", hostVarsPath, err)
	case r.optionalOnly:
		return r.failf("Nothing is installed yet: run ./run.bash --consumer first")
	}
	u, err := r.env.CurrentUser()
	if err != nil {
		return r.failf("Cannot look up your user account: %v", err)
	}
	name := u.Name
	if name == "" {
		name = u.Username
	}
	if strings.ContainsAny(name+u.Username, "\n\r") {
		return r.failf("Your user name holds a line break; fix it in Settings > Users first")
	}
	if err := fsx.WriteAtomic(file, "host_vars", consumerVars(u.Username, name), 0o600); err != nil {
		return r.failf("Cannot write %s: %v", hostVarsPath, err)
	}
	r.ok("host_vars written for a consumer install")
	return nil
}

// --- the tools Ansible needs ---

func (r *run) install(ctx context.Context) error {
	r.step("Installing what the install needs")
	if err := r.sudo.Ready(ctx, "Installing Python, pipx and the tools Ansible needs."); err != nil {
		return r.failf("sudo was not available: %v", err)
	}
	if err := r.sudo.Run(ctx, append([]string{"dnf", "-y", install}, basePackages...)...); err != nil {
		return r.failf("dnf install failed: %v", err)
	}
	for _, dir := range []string{".local/bin", ".local/share", ".local/state"} {
		if err := os.MkdirAll(filepath.Join(r.home, dir), 0o750); err != nil {
			return r.failf("Cannot make ~/%s: %v", dir, err)
		}
	}
	if err := r.pipx(ctx); err != nil {
		return err
	}
	if err := r.vault(ctx); err != nil {
		return err
	}
	return r.foreground(ctx, "ansible-galaxy install", execx.Cmd{Name: "ansible-galaxy", Args: []string{install, "--keep-scm-meta", "-r", "requirements.yml"}})
}

func (r *run) pipx(ctx context.Context) error {
	_, statErr := os.Stat(filepath.Join(r.home, ".local/share/pipx/venvs/ansible"))
	switch {
	case errors.Is(statErr, os.ErrNotExist):
		if err := r.foreground(ctx, "pipx install", execx.Cmd{Name: pipx, Args: []string{install, "--include-deps", ansibleVenv}}); err != nil {
			return err
		}
	case statErr != nil:
		return r.failf("Cannot check pipx's ansible: %v", statErr)
	}
	res, err := r.env.Runner.Run(ctx, execx.Cmd{Name: pipx, Args: []string{"list", "--json"}, Env: []string{r.path}})
	if err != nil {
		return r.failf("pipx list failed: %v", err)
	}
	injected, err := injectedPackages(res.Stdout)
	if err != nil {
		return r.failf("pipx list: %v", err)
	}
	for _, dep := range ansibleDeps {
		if _, have := injected[dep]; have {
			continue
		}
		if err := r.foreground(ctx, "pipx inject", execx.Cmd{Name: pipx, Args: []string{"inject", ansibleVenv, dep}}); err != nil {
			return err
		}
	}
	r.ok("Ansible installed")
	return nil
}

// injectedPackages is what `pipx list --json` says is injected into the ansible venv; pipx's
// own keys are snake_case, so the document is walked by key rather than decoded into tags.
func injectedPackages(out []byte) (map[string]json.RawMessage, error) {
	raw := json.RawMessage(out)
	for _, key := range []string{"venvs", ansibleVenv, "metadata", "injected_packages"} {
		var level map[string]json.RawMessage
		if err := json.Unmarshal(raw, &level); err != nil {
			return nil, fmt.Errorf("no %s object in its JSON: %w", key, err)
		}
		next, ok := level[key]
		if !ok {
			return nil, fmt.Errorf("its JSON has no %s (is pipx's ansible installed?)", key)
		}
		raw = next
	}
	var injected map[string]json.RawMessage
	if err := json.Unmarshal(raw, &injected); err != nil {
		return nil, fmt.Errorf("injected_packages is not an object: %w", err)
	}
	return injected, nil
}

// vaultFile is the file ansible.cfg names for the vault password, as Ansible resolves it.
func (r *run) vaultFile(ctx context.Context) (string, error) {
	res, err := r.env.Runner.Run(ctx, execx.Cmd{
		Name: "ansible-config", Args: []string{"dump", "--only-changed", "--format", "json"},
		Dir: r.root, Env: []string{r.path},
	})
	if err != nil {
		return "", r.failf("ansible-config failed: %v", err)
	}
	var settings []struct {
		Name  string          `json:"name"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(res.Stdout, &settings); err != nil {
		return "", r.failf("ansible-config printed what is not JSON: %v", err)
	}
	for _, s := range settings {
		var file string
		if s.Name == vaultFileSetting && json.Unmarshal(s.Value, &file) == nil && filepath.IsAbs(file) {
			return file, nil
		}
	}
	return "", r.failf("ansible-config names no %s: ansible.cfg must point the vault at a file", vaultFileSetting)
}

// vault makes the vault password at random when there is none; it is never asked for, and it
// stays on this machine only.
func (r *run) vault(ctx context.Context) error {
	file, err := r.vaultFile(ctx)
	if err != nil {
		return err
	}
	if _, err := os.Stat(file); err == nil {
		r.ok("Vault password present (kept)")
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return r.failf("Cannot check %s: %v", file, err)
	}
	raw := make([]byte, vaultBytes)
	if _, err := io.ReadFull(r.env.Random, raw); err != nil {
		return r.failf("Cannot make a random vault password: %v", err)
	}
	if err := fsx.WriteAtomic(file, "vault password", []byte(base64.StdEncoding.EncodeToString(raw)+"\n"), 0o600); err != nil {
		return r.failf("Cannot write %s: %v", file, err)
	}
	r.ok("Vault password made at random (kept on this machine only)")
	return nil
}

func (r *run) vaultExists(ctx context.Context) error {
	file, err := r.vaultFile(ctx)
	if err != nil {
		return err
	}
	if _, err := os.Stat(file); err != nil {
		return r.failf("The vault password %s is missing (%v): run ./run.bash --consumer first", file, err)
	}
	return nil
}

// --- plays ---

// becomeFile is the name of the password file in becomeDir.
const becomeFile = "become-password"

// becomeOnce asks for the login password once per run, so that no play asks again. Only a real
// passwordless sudo (-k ignores a cached credential, which Ansible's become cannot use) is asked
// nothing. Otherwise the password is typed with the echo off and checked by sudo at once, asked
// again when sudo refuses it (three tries), and written to a 0600 file in a 0700 directory of
// its own under XDG_RUNTIME_DIR (memory only, this user's alone), which every ansible-playbook
// is handed with --become-password-file: never argv, never an environment. Run removes it.
func (r *run) becomeOnce(ctx context.Context) error {
	if err := stoppedBy(ctx); err != nil {
		return err
	}
	_, err := r.sudo.Capture(ctx, "-k", "true")
	switch {
	case err == nil:
		r.become = nil
		return nil
	case !errors.Is(err, sudo.ErrNeedsPassword):
		return r.failf("sudo could not be checked: %v", err)
	}
	runtimeDir := r.env.Getenv("XDG_RUNTIME_DIR")
	if !filepath.IsAbs(runtimeDir) {
		return r.failf("XDG_RUNTIME_DIR is not set ('%s'), so there is nowhere private to keep your password for the install: run this from a terminal in your desktop session", runtimeDir)
	}
	r.step("Your password")
	r.info("The install needs your login password once. It is checked now, used for every step, and forgotten when the install ends")
	var pw execx.Secret
	_, err = r.lines.Secret(ctx, ui.Here(tool, "Your password"), "Your login password (nothing shows as you type)", func(line string) error {
		s, emptyErr := execx.NewSecret("SUDO_PASSWORD", line)
		if emptyErr != nil {
			return errors.New("nothing was typed: type your password, then press Enter")
		}
		switch checkErr := r.sudo.CheckPassword(ctx, s); {
		case errors.Is(checkErr, sudo.ErrWrongPassword):
			return errors.New("sudo did not accept that password; type it again")
		case checkErr != nil:
			return fmt.Errorf("sudo could not check it (%w); try again", checkErr)
		}
		pw = s
		return nil
	})
	if gaveUp, ok := errors.AsType[*ui.GaveUpError](err); ok {
		return r.failf("Your password was not accepted %d times, so nothing was installed. Run ./run.bash --consumer again to retry", gaveUp.Tries)
	}
	if err != nil {
		return err
	}
	r.ok("Password accepted")
	return r.writeBecomeFile(runtimeDir, pw)
}

func (r *run) writeBecomeFile(runtimeDir string, pw execx.Secret) error {
	dir, err := os.MkdirTemp(runtimeDir, tool+"-")
	if err != nil {
		return r.failf("Cannot make a private directory in %s: %v", runtimeDir, err)
	}
	r.becomeDir = dir
	if err := pw.WriteFile(dir, becomeFile); err != nil {
		return r.failf("Cannot keep the password for the install: %v", err)
	}
	r.become = []string{"--become-password-file", filepath.Join(dir, becomeFile)}
	return nil
}

// dropBecomeFile removes the password file and its directory, if this run made them.
func (r *run) dropBecomeFile() error {
	if r.becomeDir == "" {
		return nil
	}
	if err := os.RemoveAll(r.becomeDir); err != nil {
		return fmt.Errorf("could not remove the password file in %s: %w; remove that directory now", r.becomeDir, err)
	}
	r.becomeDir = ""
	return nil
}

// playRun is one play's run in the terminal: its exit, an error starting it, and the signal
// that ended this whole run while it ran (nil when none did).
type playRun struct {
	exit execx.Exit
	err  error
	stop *execx.SignalError
}

func (r *run) runPlay(ctx context.Context, path string) playRun {
	exit, err := r.env.Foreground.Foreground(execx.Cmd{
		Name: "ansible-playbook", Args: append([]string{path}, r.become...), Dir: r.root, Env: []string{r.path},
	})
	pr := playRun{exit: exit, err: err}
	if se, ok := errors.AsType[*execx.SignalError](context.Cause(ctx)); ok {
		pr.stop = se
	}
	return pr
}

// play runs the main playbook, failing fast.
func (r *run) play(ctx context.Context, path, what string) error {
	pr := r.runPlay(ctx, path)
	if pr.stop != nil {
		return pr.stop
	}
	exit, err := pr.exit, pr.err
	if err == nil && exit.Code == 0 {
		r.ok(what + " done")
		return nil
	}
	if err != nil {
		r.bad(fmt.Sprintf("%s could not run: %v", what, err))
	} else {
		r.bad(fmt.Sprintf("%s failed (exit %d)", what, exit.Code))
	}
	r.plain("   The output above shows the task that failed. Fix what it says and run ./run.bash --consumer again")
	r.plain("   (it carries on from where it is), or report it at " + issuesURL)
	if err == nil && exit.Code > 128 {
		return &exitError{code: term.ExitCode(exit.Code)}
	}
	return errReported
}

// foreground runs a setup program in the terminal, in the checkout, and fails on a non-zero exit.
func (r *run) foreground(ctx context.Context, what string, cmd execx.Cmd) error {
	cmd.Dir, cmd.Env = r.root, []string{r.path}
	exit, err := r.env.Foreground.Foreground(cmd)
	if se := stoppedBy(ctx); se != nil {
		return se
	}
	if err != nil {
		return r.failf("%s could not run: %v", what, err)
	}
	if exit.Code != 0 {
		return r.failf("%s failed (exit %d)", what, exit.Code)
	}
	return nil
}

func stoppedBy(ctx context.Context) error {
	if se, ok := errors.AsType[*execx.SignalError](context.Cause(ctx)); ok {
		return se
	}
	return nil
}

func (r *run) optional(ctx context.Context) error {
	r.step("Optional apps")
	plays, err := optionalPlays(r.root)
	if err != nil {
		return r.failf("%v", err)
	}
	var menu bytes.Buffer
	for i, p := range plays {
		fmt.Fprintf(&menu, "  %d) %s\n", i+1, p.label)
	}
	r.p.Print(r.env.Stderr, menu.String())
	var picks []int
	_, err = r.lines.Ask(ctx, ui.Here(tool, "Optional apps"), "Numbers to install (e.g. 1 3), or Enter for none", func(line string) error {
		var perr error
		picks, perr = parsePicks(line, len(plays))
		return perr
	})
	if err != nil {
		return err
	}
	if len(picks) == 0 {
		r.info("No optional app chosen; run ./run.bash --consumer --optional-only to add some later")
		return nil
	}
	chosen := make([]play, 0, len(picks))
	for _, i := range picks {
		chosen = append(chosen, plays[i])
	}
	return r.runOptional(ctx, chosen)
}

// outcome is what became of one chosen optional play.
type outcome int

const (
	notRun outcome = iota
	installed
	failed
	stopped
)

type playResult struct {
	play

	outcome outcome
	detail  string
}

// runOptional runs every chosen play in menu order. The plays are independent, so a failed one
// is recorded and the next one runs; a signal (Ctrl-C), or a play ended by one, stops at once.
// The summary is printed however the loop ends.
func (r *run) runOptional(ctx context.Context, chosen []play) error {
	results := make([]playResult, len(chosen))
	var stop error
	for i, p := range chosen {
		res := &results[i]
		res.play = p
		if stop != nil {
			continue
		}
		r.step(p.label)
		pr := r.runPlay(ctx, p.path)
		exit, err := pr.exit, pr.err
		switch {
		case pr.stop != nil:
			code := exit.Code
			if err != nil || code == 0 {
				code = pr.stop.ExitCode()
			}
			res.outcome, res.detail, stop = stopped, fmt.Sprintf("stopped part way (exit %d)", code), pr.stop
		case err != nil:
			res.outcome, res.detail = failed, fmt.Sprintf("could not run (%v)", err)
		case exit.Code == 0:
			res.outcome = installed
		case exit.Code > 128:
			res.outcome, res.detail = stopped, fmt.Sprintf("stopped part way (exit %d)", exit.Code)
			stop = &exitError{code: term.ExitCode(exit.Code)}
		default:
			res.outcome, res.detail = failed, fmt.Sprintf("failed (exit %d); its output is above, in this terminal (it is not saved to a file)", exit.Code)
		}
		if res.outcome == installed {
			r.ok(p.label + " done")
		} else {
			r.bad(p.label + ": " + res.detail)
		}
	}
	nFailed := r.summary(results)
	if stop != nil {
		return stop
	}
	if nFailed > 0 {
		return r.failf("%d of %d optional apps failed", nFailed, len(results))
	}
	return nil
}

// summary prints one line per chosen play and how to retry the ones not installed; it returns
// how many failed.
func (r *run) summary(results []playResult) int {
	r.step("Optional apps: what happened")
	nFailed := 0
	retry := make([]string, 0, len(results))
	for _, res := range results {
		switch res.outcome {
		case installed:
			r.ok(res.label + ": installed")
			continue
		case notRun:
			r.mark("-", res.label+": not run")
		case failed:
			nFailed++
			r.bad(res.label + ": " + res.detail)
		case stopped:
			r.bad(res.label + ": " + res.detail)
		}
		retry = append(retry, retryCommand(r.root, r.home, res.path))
	}
	if len(retry) == 0 {
		return 0
	}
	r.plain("To try again only the apps that did not install, run these one at a time:")
	for _, line := range retry {
		r.plain("   " + line)
	}
	r.plain("or choose them again from the menu: " + retryCommand(r.root, r.home, "--optional-only"))
	if nFailed > 0 {
		r.plain("A play that fails again names the task that failed in its output; fix what it says, or report it at " + issuesURL)
	}
	return nFailed
}

// retryCommand is the checkout's run.bash with one argument, as a person would type it: the
// home shown as ~ unless the path needs quoting.
func retryCommand(root, home, arg string) string {
	runBash := filepath.Join(root, "run.bash")
	shown := shellword.Quote(runBash)
	if shown == runBash {
		shown = term.AbbrevHome(runBash, home)
	}
	return shown + " " + shellword.Quote(arg)
}

func (r *run) done() error {
	marker := filepath.Join(r.home, ".local/state/fedora-desktop-setup-complete")
	if err := os.MkdirAll(filepath.Dir(marker), 0o750); err != nil {
		return r.failf("Cannot make %s: %v", filepath.Dir(marker), err)
	}
	if err := fsx.WriteAtomic(marker, "setup marker", nil, 0o644); err != nil {
		return r.failf("Cannot write %s: %v", marker, err)
	}
	r.step("Finished")
	r.ok("The install is complete. A reboot is recommended: restart the computer now")
	return nil
}
