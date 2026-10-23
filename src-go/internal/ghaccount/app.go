// Package ghaccount is the gh-account-setup command (Plan 00192 SPEC-gh-account-setup.md,
// E1-E30): each configured GitHub account logged into gh with the required scopes, its SSH key
// made and uploaded, and the key proven to log in as that account. gh, ssh-keygen, ssh,
// ansible and the scope helper (helpers/github_scopes) run through execx; the passphrase
// reaches ssh-keygen and ssh only through this binary as their askpass program (askpass.go).
package ghaccount

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

//go:embed usage.txt
var usageText string

const (
	tool         = "gh-account-setup"
	ghHost       = "github.com"
	scopesModule = "helpers.github_scopes.cli"
	sshTimeout   = 15 * time.Second
	playbook     = "playbooks/imports/play-github-cli-multi.yml"
)

// Env is everything a run touches outside its own memory and the files it reads and writes.
type Env struct {
	Runner     execx.Runner
	Foreground execx.Foregrounder
	Getenv     func(string) string
	TakeSecret func(string) (execx.Secret, bool, error)
	LookPath   func(string) (string, error)
	Hostname   func() (string, error)
	Executable func() (string, error)
	// SystemBrowserHelper is the play's copy of gh-print-auth-url (E14).
	SystemBrowserHelper string
	Stdout, Stderr      io.Writer
	StdoutIsTerminal    bool
	StderrIsTerminal    bool
}

// NewEnv is the production environment.
func NewEnv() Env {
	return Env{
		Runner: execx.OS{}, Foreground: execx.OS{}, Getenv: os.Getenv, TakeSecret: execx.TakeSecret,
		LookPath: exec.LookPath, Hostname: os.Hostname, Executable: os.Executable,
		SystemBrowserHelper: "/usr/local/bin/gh-print-auth-url",
		Stdout:              os.Stdout, Stderr: os.Stderr,
		StdoutIsTerminal: term.FileIsTerminal(os.Stdout), StderrIsTerminal: term.FileIsTerminal(os.Stderr),
	}
}

// App is one gh-account-setup invocation.
type App struct {
	Env Env
}

// errReported is a failure already explained on stderr.
var errReported = errors.New("failure reported")

// stream is stdout or stderr, and whether it gets colour (E28).
type stream struct {
	w      io.Writer
	colour bool
}

// run holds one invocation's state.
type run struct {
	env               Env
	out, err          stream
	p                 term.Printer
	root, home        string
	yml, scopes       string
	headless          bool
	pass              execx.Secret
	hasPass           bool
	startActive       string
	executable        string
	executableChecked bool
}

const (
	reset  = "\x1b[0m"
	bold   = "\x1b[1m"
	red    = "\x1b[0;31m"
	green  = "\x1b[0;32m"
	yellow = "\x1b[1;33m"
	cyan   = "\x1b[0;36m"
)

// Run is the whole command; it returns the exit code.
func (a App) Run(ctx context.Context, args []string) term.ExitCode {
	env := a.Env
	plain := env.Getenv("NO_COLOR") != ""
	r := &run{
		env: env,
		out: stream{env.Stdout, env.StdoutIsTerminal && !plain},
		err: stream{env.Stderr, env.StderrIsTerminal && !plain},
	}
	opts, err := parseArgs(args)
	if err != nil {
		r.p.Print(env.Stderr, fmt.Sprintf("%s: %s — see '%s --help'.\n", tool, err, tool))
		return term.ExitUsage
	}
	if opts.help {
		r.p.Print(env.Stdout, usageText)
		return r.finish(term.ExitOK)
	}
	return r.finish(r.main(ctx, opts))
}

func (r *run) finish(code term.ExitCode) term.ExitCode {
	if r.p.Err() != nil && code == term.ExitOK {
		return term.ExitFailure
	}
	return code
}

func (r *run) main(ctx context.Context, opts options) term.ExitCode {
	if err := r.locate(); err != nil {
		return term.ExitFailure
	}
	if err := r.preflight(ctx); err != nil {
		return exitFor(ctx, err)
	}
	st, err := r.status(ctx)
	if err != nil {
		return exitFor(ctx, err)
	}
	r.startActive = st.active
	// Every mode returns rather than exits, so the restore runs however the mode ended (E26);
	// a signal only cancels ctx, which the restore does not use.
	code := exitFor(ctx, r.runMode(ctx, opts))
	return r.restore(context.WithoutCancel(ctx), code)
}

func (r *run) runMode(ctx context.Context, opts options) error {
	switch opts.mode {
	case modeAdd:
		return r.add(ctx, account{alias: opts.alias, username: opts.username})
	case modeSetupAll:
		return r.setupAll(ctx)
	case modeCheck, modeNone:
	}
	return r.check(ctx)
}

// exitFor is the run's exit code: 128+n after signal n (E27), else 1 for a failure.
func exitFor(ctx context.Context, err error) term.ExitCode {
	if se := signalOf(ctx); se != nil {
		return term.ExitCode(se.ExitCode())
	}
	if err != nil {
		return term.ExitFailure
	}
	return term.ExitOK
}

func signalOf(ctx context.Context) *execx.SignalError {
	if se, ok := errors.AsType[*execx.SignalError](context.Cause(ctx)); ok {
		return se
	}
	return nil
}

// stopped is the signal that ended ctx, so a run goes no further than the step under way.
func stopped(ctx context.Context) error {
	if se := signalOf(ctx); se != nil {
		return se
	}
	return nil
}

// --- output (E28) ---

func paint(s stream, code, text string) string {
	if !s.colour {
		return text
	}
	return code + text + reset
}

func (r *run) mark(s stream, symbol, code, text string) {
	r.p.Print(s.w, paint(s, code, symbol+" ")+text+"\n")
}

func (r *run) infof(s stream, format string, args ...any) {
	r.mark(s, "i", cyan, fmt.Sprintf(format, args...))
}

func (r *run) okf(s stream, format string, args ...any) {
	r.mark(s, "✓", green, fmt.Sprintf(format, args...))
}

func (r *run) warnf(s stream, format string, args ...any) {
	r.mark(s, "⚠", yellow, fmt.Sprintf(format, args...))
}

func (r *run) badf(s stream, format string, args ...any) {
	r.mark(s, "✗", red, fmt.Sprintf(format, args...))
}

// failf reports a failure on stderr; the run ends with it.
func (r *run) failf(format string, args ...any) error {
	r.badf(r.err, format, args...)
	return errReported
}

func (r *run) advicef(s stream, format string, args ...any) {
	r.p.Print(s.w, "   "+paint(s, yellow, "➜")+" "+fmt.Sprintf(format, args...)+"\n")
}

func (r *run) say(s stream, text string) {
	r.p.Print(s.w, "\n"+paint(s, bold, text)+"\n")
}

func (r *run) heading(s stream, a account) {
	r.say(s, fmt.Sprintf("━━━ %s (%s) ━━━", a.alias, a.username))
}

func (r *run) plain(s stream, text string) { r.p.Print(s.w, text+"\n") }

// detail is what a failed command said, or the error when it said nothing.
func detail(out string, err error) string {
	if s := strings.TrimSpace(out); s != "" {
		return s
	}
	return err.Error()
}

// --- where things are (E4) and the preflight (E5, E6) ---

func (r *run) locate() error {
	root := r.env.Getenv("GH_ACCOUNT_SETUP_ROOT")
	if fi, err := os.Stat(root); root == "" || !filepath.IsAbs(root) || err != nil || !fi.IsDir() {
		r.badf(r.err, "GH_ACCOUNT_SETUP_ROOT is not this checkout's directory ('%s')", root)
		r.advicef(r.err, "Run scripts/gh-account-setup.bash, which builds this command and sets it")
		return errReported
	}
	home := r.env.Getenv("HOME")
	if home == "" || !filepath.IsAbs(home) {
		return r.failf("HOME is not an absolute directory ('%s')", home)
	}
	r.root, r.home = root, home
	r.yml = r.envOr("LOCALHOST_YML", filepath.Join(root, "environment/localhost/host_vars/localhost.yml"))
	r.scopes = r.envOr("SCOPES_FILE", filepath.Join(root, "vars/github-required-scopes.yml"))
	r.headless = r.env.Getenv("RUN_BASH_HEADLESS") == "true"
	return nil
}

func (r *run) envOr(name, fallback string) string {
	if v := r.env.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func (r *run) preflight(ctx context.Context) error {
	if _, err := r.env.LookPath("gh"); err != nil {
		return r.failf("GitHub CLI (gh) is not installed")
	}
	res, err := r.env.Runner.Run(ctx, ghCmd("--version"))
	if err != nil {
		return r.failf("gh --version failed: %v", err)
	}
	v, err := ghVersion(string(res.Stdout))
	if err != nil {
		return r.failf("GitHub CLI v%s+ required: multi-account auth status as JSON (found: %v)", minGhVersion, err)
	}
	if !v.atLeast(minGhVersion) {
		return r.failf("GitHub CLI v%s+ required: multi-account auth status as JSON (found: %s)", minGhVersion, v)
	}
	return nil
}

func ghCmd(args ...string) execx.Cmd { return execx.Cmd{Name: "gh", Args: args} }

// readStatus is gh's local view of the github.com accounts (E6).
func (r *run) readStatus(ctx context.Context) (status, error) {
	res, err := r.env.Runner.Run(ctx, ghCmd("auth", "status", "--hostname", ghHost, "--json", "hosts"))
	if err != nil {
		return status{}, fmt.Errorf("gh auth status failed: %w", err)
	}
	st, err := parseStatus(res.Stdout)
	if err != nil {
		return status{}, fmt.Errorf("gh auth status: %w: %q", err, strings.TrimSpace(string(res.Stdout)))
	}
	return st, nil
}

func (r *run) status(ctx context.Context) (status, error) {
	st, err := r.readStatus(ctx)
	if err != nil {
		return status{}, r.failf("%v", err)
	}
	return st, nil
}

// restore makes the account active at the start active again (E26).
func (r *run) restore(ctx context.Context, code term.ExitCode) term.ExitCode {
	if r.startActive == "" {
		return code
	}
	if st, err := r.readStatus(ctx); err == nil && strings.EqualFold(st.active, r.startActive) {
		return code
	}
	out, err := execx.Combined(ctx, r.env.Runner, ghCmd("auth", "switch", "--hostname", ghHost, "--user", r.startActive))
	if err != nil {
		r.badf(r.err, "Could not switch gh back to %s, the account active when this started: %s", r.startActive, detail(out, err))
		r.advicef(r.err, "Plain gh calls act as the active account. Run: gh auth switch --hostname %s --user %s", ghHost, r.startActive)
		if code == term.ExitOK {
			return term.ExitFailure
		}
		return code
	}
	r.infof(r.err, "gh is back on %s, the account active when this started", r.startActive)
	return code
}

// --- configuration (E7, E8) ---

func (r *run) accounts() ([]account, error) {
	accounts, err := readAccounts(r.yml)
	if err != nil {
		r.badf(r.err, "%v", err)
		if missing, ok := errors.AsType[configMissingError](err); ok && missing.path != "" {
			r.advicef(r.err, "Run run.bash first to create the initial configuration")
		}
		return nil, errReported
	}
	return accounts, nil
}

// --- the scope helper (E9, E10) ---

func (r *run) scopesCmd(args ...string) execx.Cmd {
	argv := append([]string{"-m", scopesModule}, args...)
	return execx.Cmd{Name: "python3", Args: append(argv, "--scopes-file", r.scopes), Dir: r.root}
}

func (r *run) requiredScopes(ctx context.Context) (string, error) {
	res, err := r.env.Runner.Run(ctx, r.scopesCmd("required"))
	if err != nil {
		return "", r.failf("Could not read the required scopes from %s: %v", r.scopes, err)
	}
	scopes := strings.TrimSpace(string(res.Stdout))
	if scopes == "" {
		return "", r.failf("The scope helper named no required scope in %s", r.scopes)
	}
	return scopes, nil
}

func (r *run) audit(ctx context.Context, logins []string) ([]verdict, error) {
	args := make([]string, 0, 1+2*len(logins))
	args = append(args, "audit")
	for _, l := range logins {
		args = append(args, "--user", l)
	}
	res, err := r.env.Runner.Run(ctx, r.scopesCmd(args...))
	if err != nil {
		return nil, fmt.Errorf("the scope audit failed: %w", err)
	}
	return parseAudit(string(res.Stdout), logins)
}

// --- the passphrase (E11, E21) ---

// loadPassphrase is the passphrase from run.bash, else the vault's; it reports its failure.
func (r *run) loadPassphrase(ctx context.Context) error {
	// run.bash hands the passphrase over on a fresh install.
	s, ok, err := r.env.TakeSecret("GITHUB_SSH_PASSPHRASE")
	if err != nil {
		return r.failf("Cannot read the passphrase run.bash gave: %v", err)
	}
	if ok {
		if r.pass, err = s.Renamed(askpassSecretVar); err != nil {
			return r.failf("%v", err)
		}
		r.hasPass = true
		return nil
	}
	return r.decryptPassphrase(ctx)
}

func (r *run) decryptPassphrase(ctx context.Context) error {
	cmd := execx.Cmd{
		Name: "ansible",
		Args: []string{"localhost", "-c", "local", "-e", "@" + r.yml, "-m", "debug", "-a", "msg={{ github_ssh_passphrase }}"},
		Dir:  r.root,
		Env: []string{
			"ANSIBLE_CONFIG=" + filepath.Join(r.root, "ansible.cfg"),
			"ANSIBLE_STDOUT_CALLBACK=ansible.builtin.minimal", "ANSIBLE_NOCOLOR=1",
		},
	}
	if given := r.env.Getenv("VAULT_PASS_FILE"); given != "" {
		if _, err := os.Stat(given); err != nil {
			return r.failf("Vault password file not found: %s", given)
		}
		cmd.Env = append(cmd.Env, "ANSIBLE_VAULT_PASSWORD_FILE="+given)
	}
	res, err := r.env.Runner.Run(ctx, cmd)
	if err != nil {
		r.p.Print(r.err.w, string(res.Stderr))
		return r.decryptFailed("ansible failed: " + err.Error())
	}
	pass, err := parseVaultOutput(string(res.Stdout))
	if err != nil {
		return r.decryptFailed(err.Error())
	}
	if r.pass, err = execx.NewSecret(askpassSecretVar, pass); err != nil {
		return r.decryptFailed(err.Error())
	}
	r.hasPass = true
	return nil
}

func (r *run) decryptFailed(why string) error {
	r.badf(r.err, "Failed to decrypt github_ssh_passphrase from the vault (%s)", why)
	r.advicef(r.err, "Check the vault password file ansible.cfg names, or run run.bash first to set up the vault")
	return errReported
}

// askpass is c set up to get the passphrase from this binary as its askpass program (E21).
func (r *run) askpass(c execx.Cmd, mode string) (execx.Cmd, error) {
	if !r.executableChecked {
		exe, err := r.env.Executable()
		if err != nil {
			return c, r.failf("Cannot find this program to answer %s's passphrase prompt: %v", c.Name, err)
		}
		r.executable, r.executableChecked = exe, true
	}
	c.Env = append(c.Env, "SSH_ASKPASS="+r.executable, "SSH_ASKPASS_REQUIRE=force", AskpassModeVar+"="+mode)
	c.Secrets = append(c.Secrets, r.pass)
	return c, nil
}

// --- --add (E22, E23) ---

func (r *run) add(ctx context.Context, a account) error {
	r.say(r.err, fmt.Sprintf("Adding GitHub account: %s (%s)", a.alias, a.username))
	accounts, err := r.accounts()
	if err != nil {
		return err
	}
	configured := false
	for _, c := range accounts {
		if !strings.EqualFold(c.alias, a.alias) {
			continue
		}
		if !strings.EqualFold(c.username, a.username) {
			return r.failf("Account '%s' is already in %s as '%s', not '%s'", c.alias, r.yml, c.username, a.username)
		}
		r.infof(r.err, "Account '%s' is already in %s", c.alias, r.yml)
		a, configured = c, true
	}
	if err := r.loadPassphrase(ctx); err != nil {
		return err
	}
	if !configured {
		if err := addToConfig(r.yml, a); err != nil {
			return r.failf("%v", err)
		}
		r.okf(r.err, "Added %s: %s to %s", a.alias, a.username, r.yml)
	}
	if err := r.setupAccount(ctx, a); err != nil {
		return err
	}
	r.say(r.err, fmt.Sprintf("Done! Account %s is ready.", a.alias))
	r.infof(r.err, "Run the playbook to deploy the SSH config and git helpers:")
	r.plain(r.err, "   "+filepath.Join(r.root, playbook))
	return nil
}

// --- --setup-all (E24) ---

func (r *run) setupAll(ctx context.Context) error {
	accounts, err := r.accounts()
	if err != nil {
		return err
	}
	if len(accounts) == 0 {
		r.warnf(r.err, "No github_accounts entries found in %s", r.yml)
		return nil
	}
	r.say(r.err, fmt.Sprintf("Setting up %d GitHub account(s)", len(accounts)))
	if r.headless {
		if err := r.auditHeadless(ctx, accounts); err != nil {
			return err
		}
	} else {
		r.infof(r.err, "Each account needs at most one browser authorisation, asking for every scope it lacks — log in as the correct user when prompted")
	}
	if err := r.loadPassphrase(ctx); err != nil {
		return err
	}
	for _, a := range accounts {
		if err := r.setupAccount(ctx, a); err != nil {
			return err
		}
	}
	r.say(r.err, "All accounts configured!")
	return nil
}

// auditHeadless fails once, naming every account that a headless run could not fix (E24).
func (r *run) auditHeadless(ctx context.Context, accounts []account) error {
	st, err := r.status(ctx)
	if err != nil {
		return err
	}
	logins := make([]string, 0, len(accounts))
	for _, a := range accounts {
		logins = append(logins, ghLogin(st, a.username))
	}
	verdicts, err := r.audit(ctx, logins)
	if err != nil {
		return r.failf("Headless: could not audit the GitHub accounts' scopes: %v", err)
	}
	var problems []string
	for _, v := range verdicts {
		if v.kind != scopesOK {
			problems = append(problems, v.problem())
		}
	}
	if len(problems) == 0 {
		return nil
	}
	r.badf(r.err, "Headless: these GitHub accounts cannot be used as they are, and headless cannot run the browser flow that would fix them:")
	for _, p := range problems {
		r.plain(r.err, "     "+p)
	}
	r.advicef(r.err, "Give each account a token carrying every scope in %s", r.scopes)
	return errReported
}

// ghLogin is the account's login as gh spells it when gh holds it, else as configured.
func ghLogin(st status, username string) string {
	if l := st.login(username); l != "" {
		return l
	}
	return username
}

// --- one account's setup (E13-E21) ---

func (r *run) setupAccount(ctx context.Context, a account) error {
	if err := stopped(ctx); err != nil {
		return err
	}
	r.heading(r.err, a)
	helper, err := r.browserHelper()
	if err != nil {
		return err
	}
	st, err := r.status(ctx)
	if err != nil {
		return err
	}
	login := st.login(a.username)
	if login != "" {
		r.okf(r.err, "Authenticated: %s", a.username)
	} else if login, err = r.logIn(ctx, a, helper); err != nil {
		return err
	}
	if err = r.ensureScopes(ctx, a, login, helper); err != nil {
		return err
	}
	key, err := r.ensureKey(ctx, a)
	if err != nil {
		return err
	}
	if err := r.ensureUploaded(ctx, a, login, key); err != nil {
		return err
	}
	if v := r.sshTest(ctx, r.err, a, key); v == sshWrongAccount || v == sshFailed {
		return errReported
	}
	return stopped(ctx)
}

// browserHelper is GH_BROWSER's value: the play's gh-print-auth-url, else the checkout's (E14).
func (r *run) browserHelper() (string, error) {
	inRepo := filepath.Join(r.root, "files/usr/local/bin/gh-print-auth-url")
	for _, path := range []string{r.env.SystemBrowserHelper, inRepo} {
		if fi, err := os.Stat(path); err != nil || fi.IsDir() {
			continue
		}
		if strings.ContainsAny(path, "\"\\$`") {
			return "", r.failf("The browser helper's path %s holds a character gh's command line cannot keep", path)
		}
		return `bash "` + path + `"`, nil
	}
	r.badf(r.err, "gh-print-auth-url helper not found at %s or %s", r.env.SystemBrowserHelper, inRepo)
	r.advicef(r.err, "Restore %s from git", inRepo)
	return "", errReported
}

// logIn runs gh's browser login and proves it authorised the expected account (E15).
func (r *run) logIn(ctx context.Context, a account, helper string) (string, error) {
	u := a.username
	if r.headless {
		r.badf(r.err, "Headless: GitHub account '%s' is not authenticated and headless cannot run the interactive browser/device-code login.", u)
		r.advicef(r.err, "Ensure run.bash token-auth succeeded and RUN_BASH_GITHUB_TOKEN_FILE holds a PAT for this account.")
		return "", errReported
	}
	scopes, err := r.requiredScopes(ctx)
	if err != nil {
		return "", err
	}
	r.infof(r.err, "Authenticating %s...", u)
	r.plain(r.err, "")
	r.plain(r.err, "   1. Copy the one-time code shown below")
	r.plain(r.err, "   2. Press Enter when prompted — the URL will be displayed (browser will NOT open)")
	r.plain(r.err, "   3. Open the URL in the browser profile logged into "+paint(r.err, bold, u))
	r.plain(r.err, "   4. Paste the code and authorise")
	r.plain(r.err, "")
	login := ghCmd("auth", "login", "--hostname", ghHost, "--git-protocol", "ssh", "--web", "--skip-ssh-key", "--scopes", scopes)
	login.Env = []string{"GH_BROWSER=" + helper}
	exit, fgErr := r.env.Foreground.Foreground(login)
	if err = stopped(ctx); err != nil {
		return "", err
	}
	if fgErr != nil || exit.Code != 0 {
		return "", r.failf("Authentication failed for %s%s", u, startFailure(fgErr))
	}
	st, err := r.status(ctx)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(st.active, u) {
		r.badf(r.err, "Authenticated as '%s' but expected '%s'", st.active, u)
		r.advicef(r.err, "You used the wrong browser profile")
		if st.active != "" {
			r.advicef(r.err, "Run: gh auth logout --hostname %s --user %s", ghHost, st.active)
		}
		r.advicef(r.err, "Then re-run this using the browser profile for '%s'", u)
		return "", errReported
	}
	r.okf(r.err, "Authenticated: %s", u)
	return st.active, nil
}

func startFailure(err error) string {
	if err == nil {
		return ""
	}
	return ": " + err.Error()
}

// ensureScopes asks for every missing scope in one refresh (E16, E19).
func (r *run) ensureScopes(ctx context.Context, a account, login, helper string) error {
	v, err := r.auditOne(ctx, a, login)
	if err != nil {
		return err
	}
	if v.kind == scopesOK {
		r.okf(r.err, "Scopes OK: %s", a.username)
		return nil
	}
	if r.headless {
		r.badf(r.err, "Headless: GitHub account '%s' is missing scopes (%s) and headless cannot run the interactive scope refresh.", a.username, v.detail)
		r.advicef(r.err, "Provide a PAT carrying every scope in %s", r.scopes)
		return errReported
	}
	if err = r.switchTo(ctx, login); err != nil {
		return err
	}
	r.warnf(r.err, "Missing scopes: %s", v.detail)
	r.plain(r.err, "")
	r.plain(r.err, "   Press Enter when prompted — the URL will be displayed (browser will NOT open)")
	r.plain(r.err, "   Open the URL in the browser profile for "+paint(r.err, bold, a.username))
	r.plain(r.err, "")
	refresh := ghCmd("auth", "refresh", "--hostname", ghHost, "--scopes", v.detail)
	refresh.Env = []string{"GH_BROWSER=" + helper}
	exit, fgErr := r.env.Foreground.Foreground(refresh)
	if err = stopped(ctx); err != nil {
		return err
	}
	if fgErr != nil || exit.Code != 0 {
		return r.failf("Failed to update scopes for %s%s", a.username, startFailure(fgErr))
	}
	if v, err = r.auditOne(ctx, a, login); err != nil {
		return err
	}
	if v.kind == scopesMissing {
		return r.failf("The refresh for %s still left these scopes missing: %s", a.username, v.detail)
	}
	r.okf(r.err, "Scopes updated: %s", a.username)
	return nil
}

// auditOne is one account's verdict, OK or missing scopes; any other is reported as a failure.
func (r *run) auditOne(ctx context.Context, a account, login string) (verdict, error) {
	verdicts, err := r.audit(ctx, []string{login})
	if err != nil {
		return verdict{}, r.failf("Could not read the scopes of %s: %v", a.username, err)
	}
	v := verdicts[0]
	switch v.kind {
	case scopesOK, scopesMissing:
		return v, nil
	case scopesNotAuthenticated:
		return v, r.failf("The scope helper finds %s not logged in to gh", a.username)
	case scopesUnreadable:
		return v, r.failf("Could not read the scopes of %s: %s", a.username, v.detail)
	}
	return v, r.failf("The scope helper gave no verdict for %s", a.username)
}

// switchTo makes login gh's active account, for the refresh only (E19).
func (r *run) switchTo(ctx context.Context, login string) error {
	st, err := r.status(ctx)
	if err != nil {
		return err
	}
	if strings.EqualFold(st.active, login) {
		return nil
	}
	out, err := execx.Combined(ctx, r.env.Runner, ghCmd("auth", "switch", "--hostname", ghHost, "--user", login))
	if err != nil {
		return r.failf("Could not switch gh to %s: %s", login, detail(out, err))
	}
	return nil
}

func (r *run) keyPath(a account) string { return filepath.Join(r.home, ".ssh", "github_"+a.alias) }

// ensureKey is the account's key, made with the passphrase when it is not there (E17).
func (r *run) ensureKey(ctx context.Context, a account) (string, error) {
	key := r.keyPath(a)
	pub := key + ".pub"
	_, err := os.Stat(key)
	switch {
	case err == nil:
		r.okf(r.err, "SSH key exists: github_%s", a.alias)
		if _, err = os.Stat(pub); err != nil {
			r.badf(r.err, "The SSH key %s has no public key %s", key, pub)
			r.advicef(r.err, "Recreate it: ssh-keygen -y -f %s > %s", key, pub)
			return "", errReported
		}
		return key, nil
	case !errors.Is(err, fs.ErrNotExist):
		return "", r.failf("Cannot read %s: %v", key, err)
	}
	if err = makeSSHDir(filepath.Dir(key)); err != nil {
		return "", r.failf("%v", err)
	}
	r.infof(r.err, "Generating SSH key: github_%s", a.alias)
	keygen, err := r.askpass(execx.Cmd{Name: "ssh-keygen", Args: []string{"-q", "-t", "ed25519", "-C", a.username + "@github", "-f", key}}, askpassKeygen)
	if err != nil {
		return "", err
	}
	if _, err := r.env.Runner.Run(ctx, keygen); err != nil {
		return "", r.failf("ssh-keygen could not make %s: %v", key, err)
	}
	if err := os.Chmod(key, 0o600); err != nil {
		return "", r.failf("Cannot make %s private: %v", key, err)
	}
	for _, f := range []string{key, pub} {
		if _, err := os.Stat(f); err != nil {
			return "", r.failf("ssh-keygen did not make %s: %v", f, err)
		}
	}
	r.okf(r.err, "SSH key generated: github_%s", a.alias)
	return key, nil
}

// makeSSHDir creates ~/.ssh, mode 0700, when it is missing.
func makeSSHDir(dir string) error {
	err := os.Mkdir(dir, 0o700)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot create %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("cannot make %s private: %w", dir, err)
	}
	return nil
}

// token is the account's own gh token, so a call acts as it and not as the active account.
func (r *run) token(ctx context.Context, login string) (execx.Secret, error) {
	res, err := r.env.Runner.Run(ctx, ghCmd("auth", "token", "--hostname", ghHost, "--user", login))
	if err != nil {
		return execx.Secret{}, fmt.Errorf("gh holds no token for %s: %w", login, err)
	}
	s, err := execx.NewSecret("GH_TOKEN", strings.TrimSpace(string(res.Stdout)))
	if err != nil {
		return execx.Secret{}, fmt.Errorf("gh's token for %s: %w", login, err)
	}
	return s, nil
}

// registered reports whether the key's .pub is on the account (E18).
func (r *run) registered(ctx context.Context, token execx.Secret, pub string) (bool, error) {
	data, err := os.ReadFile(pub)
	if err != nil {
		return false, fmt.Errorf("cannot read %s: %w", pub, err)
	}
	field, err := publicKeyField(string(data))
	if err != nil {
		return false, fmt.Errorf("%s: %w", pub, err)
	}
	list := ghCmd("api", "user/keys", "--paginate", "--jq", ".[].key")
	list.Secrets = []execx.Secret{token}
	res, err := r.env.Runner.Run(ctx, list)
	if err != nil {
		return false, fmt.Errorf("the key listing failed: %w", err)
	}
	return keyListed(string(res.Stdout), field), nil
}

func (r *run) ensureUploaded(ctx context.Context, a account, login, key string) error {
	token, err := r.token(ctx, login)
	if err != nil {
		return r.failf("%v", err)
	}
	pub := key + ".pub"
	on, err := r.registered(ctx, token, pub)
	if err != nil {
		return r.failf("Cannot tell whether the key is on %s: %v", a.username, err)
	}
	if on {
		r.okf(r.err, "Key registered on GitHub: %s", a.username)
		return nil
	}
	host, err := r.env.Hostname()
	if err != nil {
		return r.failf("Cannot read this machine's hostname for the key's title: %v", err)
	}
	title := host + "-" + a.alias
	r.infof(r.err, "Uploading key to GitHub: %s", a.username)
	upload := ghCmd("ssh-key", "add", pub, "--title", title, "--type", "authentication")
	upload.Secrets = []execx.Secret{token}
	if _, err := r.env.Runner.Run(ctx, upload); err != nil {
		r.badf(r.err, "Key upload failed for %s: %v", a.username, err)
		r.advicef(r.err, "Manual: gh auth switch --hostname %s --user %s && gh ssh-key add %s --title '%s' --type authentication", ghHost, login, pub, title)
		return errReported
	}
	r.okf(r.err, "Key uploaded: %s", a.username)
	return nil
}

type sshVerdict int

const (
	sshVerified sshVerdict = iota
	sshWrongAccount
	sshInconclusive
	sshFailed
)

// sshTest logs in to GitHub with the key alone and reads whom it greets (E20, E21).
func (r *run) sshTest(ctx context.Context, s stream, a account, key string) sshVerdict {
	r.infof(s, "Testing SSH: github_%s...", a.alias)
	args := []string{"-F", "/dev/null", "-o", "IdentityAgent=none", "-o", "IdentitiesOnly=yes"}
	if !r.hasPass {
		args = append(args, "-o", "BatchMode=yes")
	}
	args = append(args, "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new",
		"-o", "PreferredAuthentications=publickey", "-o", "PasswordAuthentication=no",
		"-o", "KbdInteractiveAuthentication=no", "-i", key, "-T", "git@github.com")
	cmd := execx.Cmd{Name: "ssh", Args: args}
	if r.hasPass {
		var err error
		if cmd, err = r.askpass(cmd, askpassSSH); err != nil {
			return sshFailed
		}
	}
	tctx, cancel := context.WithTimeout(ctx, sshTimeout)
	defer cancel()
	out, err := execx.Combined(tctx, r.env.Runner, cmd)
	var exitErr *execx.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		r.badf(s, "SSH test of github_%s could not run: %v", a.alias, err)
		return sshFailed
	}
	login := sshLogin(out)
	switch {
	case strings.EqualFold(login, a.username):
		r.okf(s, "SSH verified: %s → %s", a.alias, a.username)
		return sshVerified
	case login != "":
		r.badf(s, "SSH key github_%s authenticates as '%s', expected '%s'", a.alias, login, a.username)
		r.advicef(s, "Delete this key from %s's GitHub settings, then re-run", login)
		return sshWrongAccount
	}
	r.warnf(s, "SSH test inconclusive for %s — the key may need time to propagate", a.alias)
	r.advicef(s, "Manual test: ssh -F /dev/null -o IdentityAgent=none -i ~/.ssh/github_%s -T git@github.com", a.alias)
	return sshInconclusive
}

// --- --check (E25) ---

func (r *run) check(ctx context.Context) error {
	accounts, err := r.accounts()
	if err != nil {
		return err
	}
	if len(accounts) == 0 {
		r.warnf(r.err, "No github_accounts entries found in %s", r.yml)
		return nil
	}
	r.say(r.out, fmt.Sprintf("Checking %d GitHub account(s)", len(accounts)))
	if err := r.loadPassphrase(ctx); err != nil {
		r.warnf(r.err, "Could not decrypt the passphrase — SSH tests may be limited")
	}
	healthy := true
	for _, a := range accounts {
		if err := stopped(ctx); err != nil {
			return err
		}
		if !r.checkAccount(ctx, a) {
			healthy = false
		}
	}
	r.plain(r.out, "")
	if !healthy {
		return r.failf("Some accounts have issues — run with --setup-all to fix")
	}
	r.okf(r.out, "All accounts healthy")
	return nil
}

// checkAccount reports each check of one account on stdout; true when every one passed.
func (r *run) checkAccount(ctx context.Context, a account) bool {
	r.heading(r.out, a)
	healthy := true
	issue := func(format string, args ...any) {
		r.badf(r.out, format, args...)
		healthy = false
	}
	login := ""
	if st, err := r.readStatus(ctx); err != nil {
		issue("Auth: could not be read (%v)", err)
	} else if login = st.login(a.username); login != "" {
		r.okf(r.out, "Auth: OK")
	} else {
		issue("Auth: not authenticated as %s", a.username)
	}
	if login != "" {
		r.checkScopes(ctx, login, issue)
	}
	key := r.keyPath(a)
	if _, err := os.Stat(key); err != nil {
		issue("SSH key: missing (~/.ssh/github_%s)", a.alias)
	} else {
		r.okf(r.out, "SSH key: exists")
	}
	if _, err := os.Stat(key + ".pub"); err == nil && login != "" {
		r.checkRegistered(ctx, login, key+".pub", issue)
	}
	if _, err := os.Stat(key); err == nil && r.sshTest(ctx, r.out, a, key) != sshVerified {
		healthy = false
	}
	if healthy {
		r.okf(r.out, "Overall: healthy")
	} else {
		r.badf(r.out, "Overall: issues found")
	}
	return healthy
}

func (r *run) checkScopes(ctx context.Context, login string, issue func(string, ...any)) {
	verdicts, err := r.audit(ctx, []string{login})
	if err != nil {
		issue("Scopes: could not be read (%v)", err)
		return
	}
	switch v := verdicts[0]; v.kind {
	case scopesOK:
		r.okf(r.out, "Scopes: OK")
	case scopesMissing:
		issue("Scopes: missing %s", v.detail)
	case scopesNotAuthenticated:
		issue("Scopes: the helper finds %s not logged in", login)
	case scopesUnreadable:
		issue("Scopes: could not be read (%s)", v.detail)
	}
}

func (r *run) checkRegistered(ctx context.Context, login, pub string, issue func(string, ...any)) {
	token, err := r.token(ctx, login)
	if err != nil {
		issue("GitHub key: could not be read (%v)", err)
		return
	}
	on, err := r.registered(ctx, token, pub)
	switch {
	case err != nil:
		issue("GitHub key: could not be read (%v)", err)
	case on:
		r.okf(r.out, "GitHub key: registered")
	default:
		issue("GitHub key: not registered")
	}
}
