// Package setuprclone is the setup-rclone command (Plan 00192 SPEC-setup-rclone.md, E1-E23):
// rclone's own wizard for the remotes, the configuration vaulted into host_vars through
// ansible-vault's stdin, the rclone_mounts list edited in place and read back before it is
// written, and the rclone play offered at the end. Every program runs through execx and every
// question through ui.Lines.
package setuprclone

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccyenv"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

const (
	tool          = "setup-rclone"
	playPath      = "playbooks/imports/optional/common/play-rclone.yml"
	hostVarsPath  = "environment/localhost/host_vars/localhost.yml"
	versionPath   = "vars/fedora-version.yml"
	noHostsPhrase = "skipping: no hosts matched"
	probeKey      = "setup_rclone_probe"
	vaultID       = "localhost"
	// headingWidth is what a question's heading wraps to: ui.Lines draws a zero Width two
	// columns wide, and the headings here are short.
	headingWidth = 80
)

const usageText = `Usage: setup-rclone

Configures rclone remotes with rclone's own wizard, vaults the rclone configuration into
host_vars (rclone_config), edits the rclone_mounts list and offers to run the rclone play.

Run it on the host as scripts/setup-rclone.bash, which builds it from this checkout.

Options:
  -h, --help    Show this help
`

// Env is everything a run touches outside its own memory and the files it reads and writes.
type Env struct {
	Runner           execx.Runner
	Foreground       execx.Foregrounder
	Getenv           func(string) string
	LookPath         func(string) (string, error)
	InContainer      func() bool
	ReleaseFile      string
	Stdin            io.Reader
	Stdout, Stderr   io.Writer
	StderrIsTerminal bool
}

// NewEnv is the production environment.
func NewEnv() Env {
	return Env{
		Runner: execx.OS{}, Foreground: execx.OS{}, Getenv: os.Getenv, LookPath: exec.LookPath,
		InContainer: ccyenv.InContainer, ReleaseFile: "/etc/redhat-release",
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		StderrIsTerminal: term.FileIsTerminal(os.Stderr),
	}
}

// App is one setup-rclone invocation.
type App struct{ Env Env }

// errReported is a failure already explained on stderr.
var errReported = errors.New("failure reported")

// exitError is a run that ends with a code of its own: a child ended by a signal (E9, E21).
type exitError struct{ code term.ExitCode }

func (e *exitError) Error() string { return "exit " + strconv.Itoa(int(e.code)) }

const (
	reset  = "\x1b[0m"
	bold   = "\x1b[1m"
	red    = "\x1b[0;31m"
	green  = "\x1b[0;32m"
	yellow = "\x1b[1;33m"
	cyan   = "\x1b[0;36m"
)

// run holds one invocation's state.
type run struct {
	env        Env
	p          term.Printer
	colour     bool
	lines      *ui.Lines
	root, home string
	conf, vars string
	remotes    []string
	wizardRan  bool
	vault      string
}

// Run is the whole command; it returns the exit code.
func (a App) Run(ctx context.Context, args []string) term.ExitCode {
	env := a.Env
	r := &run{env: env, colour: env.StderrIsTerminal && env.Getenv("NO_COLOR") == ""}
	r.lines = &ui.Lines{In: env.Stdin, Out: env.Stderr, Colour: r.colour, Width: headingWidth}
	switch {
	case len(args) == 1 && (args[0] == "-h" || args[0] == "--help"):
		r.p.Print(env.Stdout, usageText)
		return r.finish(term.ExitOK)
	case len(args) > 0:
		r.p.Print(env.Stderr, tool+": this command takes no arguments — see '"+tool+" --help'.\n")
		return term.ExitUsage
	}
	return r.finish(r.exitFor(ctx, r.main(ctx)))
}

func (r *run) finish(code term.ExitCode) term.ExitCode {
	if r.p.Err() != nil && code == term.ExitOK {
		return term.ExitFailure
	}
	return code
}

// exitFor is the run's exit code: 128+n after signal n (E22), a child's own 128+n, else 1 for
// a failure.
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
	var gaveUp *ui.GaveUpError
	switch {
	case errors.Is(err, ui.ErrInputEnded):
		r.bad("The input ended before an answer; nothing was changed")
	case errors.As(err, &gaveUp):
		r.bad(err.Error() + "; nothing was changed")
	case !errors.Is(err, errReported):
		r.bad(err.Error())
	}
	return term.ExitFailure
}

func (r *run) main(ctx context.Context) error {
	if r.env.InContainer() {
		return r.failf("setup-rclone runs on the host, not in a CCY container")
	}
	if err := r.locate(); err != nil {
		return err
	}
	hv, text, err := r.preflight(ctx)
	if err != nil {
		return err
	}
	if err = r.stepRemotes(ctx); err != nil {
		return err
	}
	mounts, err := r.stepMounts(ctx, hv.mounts)
	if err != nil {
		return err
	}
	if err = r.stepWrite(text, hv.mounts, mounts); err != nil {
		return err
	}
	return r.stepDeploy(ctx, mounts)
}

// --- output (E23) ---

func (r *run) paint(code, text string) string {
	if !r.colour {
		return text
	}
	return code + text + reset
}

func (r *run) mark(symbol, code, text string) {
	r.p.Print(r.env.Stderr, r.paint(code, symbol+" ")+text+"\n")
}

func (r *run) info(text string) { r.mark("i", cyan, text) }
func (r *run) ok(text string)   { r.mark("✓", green, text) }
func (r *run) warn(text string) { r.mark("⚠", yellow, text) }
func (r *run) bad(text string)  { r.mark("✗", red, text) }
func (r *run) plain(text string) {
	r.p.Print(r.env.Stderr, text+"\n")
}

func (r *run) step(name string) {
	r.p.Print(r.env.Stderr, "\n"+r.paint(bold, "━━━ "+name+" ━━━")+"\n")
}

// failf reports a failure on stderr; the run ends with it.
func (r *run) failf(format string, args ...any) error {
	r.bad(fmt.Sprintf(format, args...))
	return errReported
}

func here(crumbs ...string) ui.Heading { return ui.Here(append([]string{tool}, crumbs...)...) }

// --- where things are (E2) and the preflight (E4-E7) ---

func (r *run) locate() error {
	root := r.env.Getenv("SETUP_RCLONE_ROOT")
	if fi, err := os.Stat(root); root == "" || !filepath.IsAbs(root) || err != nil || !fi.IsDir() {
		r.bad(fmt.Sprintf("SETUP_RCLONE_ROOT is not this checkout's directory ('%s')", root))
		r.plain("   Run scripts/setup-rclone.bash, which builds this command and sets it")
		return errReported
	}
	home := r.env.Getenv("HOME")
	if !filepath.IsAbs(home) {
		return r.failf("HOME is not an absolute directory ('%s')", home)
	}
	r.root, r.home = root, home
	r.conf = filepath.Join(home, ".config/rclone/rclone.conf")
	r.vars = filepath.Join(root, hostVarsPath)
	return nil
}

func (r *run) preflight(ctx context.Context) (hostVars, []byte, error) {
	for _, name := range []string{"ansible-playbook", "ansible-vault"} {
		if _, err := r.env.LookPath(name); err != nil {
			return hostVars{}, nil, r.failf("%s is not installed (not on PATH)", name)
		}
	}
	for _, rel := range []string{hostVarsPath, playPath, "ansible.cfg"} {
		if _, err := os.Stat(filepath.Join(r.root, rel)); err != nil {
			return hostVars{}, nil, r.failf("%s is missing: %v", rel, err)
		}
	}
	text, err := os.ReadFile(r.vars)
	if err != nil {
		return hostVars{}, nil, r.failf("Cannot read %s: %v", r.vars, err)
	}
	hv, err := parseHostVars(text)
	if err != nil {
		return hostVars{}, nil, r.failf("%s: %v", r.vars, err)
	}
	if _, err := r.encrypt(ctx, probeKey, strings.NewReader("probe")); err != nil {
		r.bad(err.Error())
		return hostVars{}, nil, r.failf("The vault password ansible.cfg names cannot be used; run run.bash first to set up the vault")
	}
	if err := r.ensureRclone(ctx); err != nil {
		return hostVars{}, nil, err
	}
	if err := r.checkFedora(ctx); err != nil {
		return hostVars{}, nil, err
	}
	return hv, text, nil
}

// encrypt is ansible-vault's encrypt_string of stdin under key, checked to be one !vault
// value (E5, E10).
func (r *run) encrypt(ctx context.Context, key string, stdin io.Reader) (string, error) {
	res, err := r.env.Runner.Run(ctx, execx.Cmd{
		Name:  "ansible-vault",
		Args:  []string{"encrypt_string", "--encrypt-vault-id", vaultID, "--stdin-name", key},
		Dir:   r.root,
		Env:   []string{"ANSIBLE_CONFIG=" + filepath.Join(r.root, "ansible.cfg")},
		Stdin: stdin,
	})
	if err != nil {
		return "", fmt.Errorf("ansible-vault encrypt_string failed: %w", err)
	}
	if _, err := parseVault(res.Stdout, key); err != nil {
		return "", err
	}
	return string(res.Stdout), nil
}

// ensureRclone installs rclone through the play when it is not on PATH (E6).
func (r *run) ensureRclone(ctx context.Context) error {
	if _, err := r.env.LookPath("rclone"); err == nil {
		return nil
	}
	r.warn("rclone not found — running the rclone play to install it")
	if err := r.play(ctx); err != nil {
		return err
	}
	if _, err := r.env.LookPath("rclone"); err != nil {
		return r.failf("rclone is still not installed after the rclone play")
	}
	return nil
}

var releaseRule = regexp.MustCompile(`^Fedora release ([0-9]+)\b`)

// checkFedora compares the running release with the checkout's (E7).
func (r *run) checkFedora(ctx context.Context) error {
	data, err := os.ReadFile(r.env.ReleaseFile)
	if err != nil {
		return r.failf("Cannot read %s (%v): this runs on a Fedora host", r.env.ReleaseFile, err)
	}
	m := releaseRule.FindSubmatch(data)
	if m == nil {
		return r.failf("Not running on Fedora (%s says %q); this repository targets Fedora only", r.env.ReleaseFile, strings.TrimSpace(string(data)))
	}
	running, err := strconv.Atoi(string(m[1]))
	if err != nil {
		return r.failf("%s: %v", r.env.ReleaseFile, err)
	}
	wanted, err := r.wantedFedora()
	if err != nil {
		return r.failf("%s: %v", versionPath, err)
	}
	if running == wanted {
		r.ok(fmt.Sprintf("Fedora %d matches this checkout (%s)", running, versionPath))
		return nil
	}
	r.warn(fmt.Sprintf("Fedora version mismatch: running Fedora %d but this checkout targets Fedora %d; the branch for Fedora %d is F%d", running, wanted, running, running))
	yes, err := r.lines.YesNo(ctx, here("Fedora version"), "Continue anyway?", false)
	if err != nil {
		return err
	}
	if !yes {
		return r.failf("Aborted: Fedora version mismatch")
	}
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

// --- step 1: remotes and the wizard (E8, E9), step 2: the vault (E10) ---

func (r *run) rclone(args ...string) execx.Cmd {
	return execx.Cmd{Name: "rclone", Args: append([]string{"--config", r.conf}, args...)}
}

func (r *run) listRemotes(ctx context.Context) ([]string, error) {
	res, err := r.env.Runner.Run(ctx, r.rclone("listremotes"))
	if err != nil {
		return nil, r.failf("rclone listremotes failed: %v", err)
	}
	var remotes []string
	for line := range strings.SplitSeq(string(res.Stdout), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, ok := strings.CutSuffix(line, ":")
		if !ok || name == "" {
			return nil, r.failf("rclone listremotes printed %q, not <remote>:", line)
		}
		remotes = append(remotes, name)
	}
	return remotes, nil
}

func (r *run) stepRemotes(ctx context.Context) error {
	r.step("Step 1: rclone remotes")
	remotes, err := r.listRemotes(ctx)
	if err != nil {
		return err
	}
	r.remotes = remotes
	if len(remotes) > 0 {
		r.info("Remotes configured:")
		for _, name := range remotes {
			r.plain("    " + name)
		}
		yes, askErr := r.lines.YesNo(ctx, here("Remotes"), "Open the rclone wizard to add or edit remotes?", false)
		if askErr != nil || !yes {
			return askErr
		}
	}
	if err = r.wizard(ctx); err != nil {
		return err
	}
	if r.remotes, err = r.listRemotes(ctx); err != nil {
		return err
	}
	if len(r.remotes) == 0 {
		return r.failf("No remote is configured; run this again and add one in the rclone wizard")
	}
	return r.stepVault(ctx)
}

func (r *run) wizard(ctx context.Context) error {
	r.plain("")
	r.plain("  The rclone configuration wizard opens next:")
	r.plain("    n  New remote   (add a cloud storage account)")
	r.plain("    e  Edit remote  (fix a client ID or secret, or reconnect)")
	r.plain("    q  Quit         (when finished; this continues to the mounts)")
	r.plain("    s  Set configuration password: skip it, Ansible Vault protects the file")
	if _, err := r.lines.Ask(ctx, here("Remotes", "rclone config"), "Press Enter to open rclone config", nil); err != nil {
		return err
	}
	exit, err := r.env.Foreground.Foreground(r.rclone("config"))
	if se := stoppedBy(ctx); se != nil {
		return se
	}
	if err != nil {
		return r.failf("rclone config could not run: %v", err)
	}
	if exit.Signal != 0 {
		return &exitError{code: term.ExitCode(128 + int(exit.Signal))}
	}
	r.wizardRan = true
	if exit.Code == 0 {
		return nil
	}
	r.warn(fmt.Sprintf("rclone config exited %d", exit.Code))
	yes, err := r.lines.YesNo(ctx, here("Remotes", "rclone config"), "Vault the rclone configuration as it is now?", false)
	if err != nil {
		return err
	}
	if !yes {
		return r.failf("Stopped after rclone config failed; nothing was written")
	}
	return nil
}

func stoppedBy(ctx context.Context) error {
	if se, ok := errors.AsType[*execx.SignalError](context.Cause(ctx)); ok {
		return se
	}
	return nil
}

func (r *run) stepVault(ctx context.Context) error {
	r.step("Step 2: vault the rclone configuration")
	fi, err := os.Stat(r.conf)
	if err != nil || !fi.Mode().IsRegular() {
		return r.failf("%s is not a regular file (%v)", r.conf, err)
	}
	f, err := os.Open(r.conf)
	if err != nil {
		return r.failf("Cannot read %s: %v", r.conf, err)
	}
	vault, encErr := r.encrypt(ctx, configKey, f)
	if err := f.Close(); err != nil && encErr == nil {
		encErr = err
	}
	if encErr != nil {
		return r.failf("Cannot vault %s: %v", r.conf, encErr)
	}
	r.vault = vault
	r.ok("rclone configuration vaulted")
	return nil
}

// --- step 3: mounts (E11-E15) ---

const keyDone = "0"

func (r *run) configured(remote string) bool {
	return slices.Contains(r.remotes, remote)
}

func (r *run) stepMounts(ctx context.Context, read []mount) ([]mount, error) {
	r.step("Step 3: mounts")
	mounts := append([]mount(nil), read...)
	for {
		choices := make([]ui.Choice, 0, len(mounts)+len(r.remotes)+1)
		for i, m := range mounts {
			label := fmt.Sprintf("Change %s: %s at %s", m.name, m.remote, m.mountpoint)
			if !r.configured(m.remoteName()) {
				label += " (remote not configured)"
			}
			choices = append(choices, ui.Choice{Key: strconv.Itoa(i + 1), Label: label})
		}
		for i, name := range r.remotes {
			choices = append(choices, ui.Choice{Key: strconv.Itoa(len(mounts) + i + 1), Label: "Add a mount of " + name})
		}
		choices = append(choices, ui.Choice{Key: keyDone, Label: "Done"})
		key, err := r.lines.Choose(ctx, here("Mounts"), "Choose", choices)
		if err != nil {
			return nil, err
		}
		if key == keyDone {
			return mounts, nil
		}
		n, err := strconv.Atoi(key)
		if err != nil {
			return nil, fmt.Errorf("menu key %q: %w", key, err)
		}
		if n <= len(mounts) {
			mounts, err = r.changeMount(ctx, mounts, n-1)
		} else {
			mounts, err = r.addMount(ctx, mounts, r.remotes[n-len(mounts)-1])
		}
		if err != nil {
			return nil, err
		}
	}
}

func (r *run) changeMount(ctx context.Context, mounts []mount, i int) ([]mount, error) {
	m := mounts[i]
	action, err := r.lines.Choose(ctx, here("Mounts", m.name), "Choose", []ui.Choice{
		{Key: "e", Label: "Edit"}, {Key: "r", Label: "Remove"}, {Key: "c", Label: "Cancel"},
	})
	if err != nil {
		return nil, err
	}
	switch action {
	case "r":
		r.ok("Removed mount " + m.name)
		r.warn(fmt.Sprintf("The play neither stops nor deletes rclone-%s.service; stop it yourself: systemctl --user disable --now rclone-%s.service", m.name, m.name))
		return append(append([]mount(nil), mounts[:i]...), mounts[i+1:]...), nil
	case "e":
		others := append(append([]mount(nil), mounts[:i]...), mounts[i+1:]...)
		spec, name, mp, done, err := r.askMount(ctx, m.remoteName(), m.folder(), m.name, m.mountpoint, others)
		if err != nil || !done {
			return mounts, err
		}
		out := append([]mount(nil), mounts...)
		out[i] = m.with(spec, name, mp)
		return out, nil
	}
	return mounts, nil
}

func (r *run) addMount(ctx context.Context, mounts []mount, remote string) ([]mount, error) {
	if err := checkRemote(remote + ":"); err != nil {
		r.bad(fmt.Sprintf("The remote %q cannot be mounted: %v", remote, err))
		return mounts, nil
	}
	spec, name, mp, done, err := r.askMount(ctx, remote, "/", defaultName(remote), "", mounts)
	if err != nil || !done {
		return mounts, err
	}
	return append(append([]mount(nil), mounts...), newMount(spec, name, mp)), nil
}

// askMount checks the remote can be reached, then asks the three values; done is false when
// the remote could not be reached (E13).
func (r *run) askMount(ctx context.Context, remote, folder, name, mp string, others []mount) (string, string, string, bool, error) {
	if !r.connect(ctx, remote) {
		return "", "", "", false, nil
	}
	h := here("Mounts", remote)
	folder, err := r.askDefault(ctx, h, "Folder to mount", folder, func(v string) error { return checkRemote(remote + ":" + v) })
	if err != nil {
		return "", "", "", false, err
	}
	if name, err = r.askDefault(ctx, h, "Mount name", name, func(v string) error { return checkName(v, others) }); err != nil {
		return "", "", "", false, err
	}
	if mp == "" {
		mp = filepath.Join(r.home, "mnt", name)
	}
	if mp, err = r.askDefault(ctx, h, "Local mountpoint", mp, func(v string) error { return checkMountpoint(v, others) }); err != nil {
		return "", "", "", false, err
	}
	return remote + ":" + folder, name, mp, true, nil
}

// askDefault is a line with a default Enter takes, checked as resolved (E15).
func (r *run) askDefault(ctx context.Context, h ui.Heading, prompt, def string, check func(string) error) (string, error) {
	resolve := func(v string) string {
		if v == "" {
			return def
		}
		return v
	}
	got, err := r.lines.Ask(ctx, h, prompt+" ["+def+"]", func(v string) error { return check(resolve(v)) })
	if err != nil {
		return "", err
	}
	return resolve(got), nil
}

var datePrefix = regexp.MustCompile(`^[0-9]{4}/[0-9/: ]*`)

var projectRule = regexp.MustCompile(`project[= ]([0-9]+)`)

// connect lists the remote's top-level folders, or explains why it cannot (E13).
func (r *run) connect(ctx context.Context, remote string) bool {
	r.info("Connecting to " + remote + "...")
	res, err := r.env.Runner.Run(ctx, r.rclone("--contimeout", "15s", "--timeout", "30s", "lsd", remote+":/"))
	if err == nil {
		r.info("Top-level folders in " + remote + ":")
		dirs := 0
		for line := range strings.SplitSeq(string(res.Stdout), "\n") {
			if f := strings.Fields(line); len(f) > 0 {
				r.plain("    /" + f[len(f)-1])
				dirs++
			}
		}
		if dirs == 0 {
			r.plain("    (remote root is empty)")
		}
		return true
	}
	msg := string(res.Stderr)
	if msg == "" {
		msg = err.Error()
	}
	r.warn("Could not connect to " + remote)
	for _, line := range logLines(msg) {
		r.plain("    rclone: " + line)
	}
	r.hint(remote, msg)
	return false
}

// logLines is rclone's log lines with the date stripped, or every line when none is dated.
func logLines(msg string) []string {
	var dated, all []string
	for line := range strings.SplitSeq(msg, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		all = append(all, strings.TrimSpace(line))
		if loc := datePrefix.FindStringIndex(line); loc != nil {
			dated = append(dated, strings.TrimSpace(line[loc[1]:]))
		}
	}
	if len(dated) > 0 {
		return dated
	}
	return all
}

func containsAny(s string, parts ...string) bool {
	for _, p := range parts {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func (r *run) hint(remote, msg string) {
	switch {
	case containsAny(msg, "SERVICE_DISABLED", "API has not been used", "accessNotConfigured"):
		url := "https://console.developers.google.com/apis/api/drive.googleapis.com/overview"
		if m := projectRule.FindStringSubmatch(msg); m != nil {
			url += "?project=" + m[1]
		}
		r.plain("  ▶ The Google Drive API is not enabled on the Google Cloud project. Enable it at:")
		r.plain("    " + url)
		r.plain("  then wait a minute and choose this remote again.")
	case containsAny(msg, "AuthError", "oauth", "token", "401", "invalid_grant"):
		r.plain("  ▶ Authentication failed: the OAuth token is missing or expired. In another terminal run:")
		r.plain("    rclone config reconnect " + remote + ":")
		r.plain("  then choose this remote again.")
	case containsAny(msg, "connection refused", "no such host", "network", "dial tcp"):
		r.plain("  ▶ Network error: the remote service could not be reached. Check the connection and choose this remote again.")
	default:
		r.plain("  ▶ Unexpected error. Investigate with: rclone lsd " + remote + ":/")
	}
}

// --- step 4: host_vars (E16-E18) ---

func (r *run) stepWrite(text []byte, read, mounts []mount) error {
	r.step("Step 4: host_vars")
	if !r.wizardRan && !mountsChanged(read, mounts) {
		r.info("host_vars unchanged")
		return nil
	}
	out, err := editHostVars(text, r.vault, mounts)
	if err != nil {
		return r.failf("%s: %v", r.vars, err)
	}
	if err := writeKeepingMode(r.vars, out); err != nil {
		return r.failf("%v", err)
	}
	r.ok("host_vars updated")
	return nil
}

// writeKeepingMode replaces the file (through a symlink, its target) whole, its mode kept.
func writeKeepingMode(path string, data []byte) error {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("cannot resolve %s: %w", path, err)
	}
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", path, err)
	}
	if err := fsx.WriteAtomic(target, "host_vars", data, info.Mode().Perm()); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	// The umask narrowed the new file's mode; the old file's is kept.
	if err := os.Chmod(target, info.Mode().Perm()); err != nil {
		return fmt.Errorf("cannot keep the mode of %s: %w", path, err)
	}
	return nil
}

// --- step 5: deploy (E19-E21) ---

func (r *run) stepDeploy(ctx context.Context, mounts []mount) error {
	r.step("Step 5: deploy")
	yes, err := r.lines.YesNo(ctx, here("Deploy"), "Run the rclone play now?", false)
	if err != nil {
		return err
	}
	if !yes {
		r.info("Run the play later:")
		r.plain("    " + filepath.Join(r.root, playPath))
		return nil
	}
	if err := r.play(ctx); err != nil {
		return err
	}
	r.ok("Mount services started and enabled")
	for _, m := range mounts {
		r.plain("    systemctl --user status rclone-" + m.name + ".service")
	}
	return nil
}

// play runs the rclone play in the checkout, its output shown and watched for no hosts (E21).
func (r *run) play(ctx context.Context) error {
	w := &watch{w: r.env.Stderr, phrase: noHostsPhrase}
	exit, err := r.env.Foreground.Foreground(execx.Cmd{
		Name: "ansible-playbook", Args: []string{playPath}, Dir: r.root, Stdout: w, Stderr: w,
	})
	if se := stoppedBy(ctx); se != nil {
		return se
	}
	switch {
	case err != nil:
		return r.failf("Playbook failed: %v", err)
	case exit.Signal != 0:
		return &exitError{code: term.ExitCode(128 + int(exit.Signal))}
	case exit.Code != 0:
		return r.failf("Playbook failed (exit %d)", exit.Code)
	case w.matched():
		return r.failf("The rclone play matched no hosts, so nothing was deployed; check the inventory")
	}
	return nil
}

// watch passes the play's output on and notes whether phrase went by, across writes.
type watch struct {
	w      io.Writer
	phrase string
	mu     sync.Mutex
	tail   []byte
	hit    bool
}

func (s *watch) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hit {
		seen := slices.Concat(s.tail, p)
		s.hit = bytes.Contains(seen, []byte(s.phrase))
		keep := min(len(seen), len(s.phrase))
		s.tail = append([]byte(nil), seen[len(seen)-keep:]...)
	}
	n, err := s.w.Write(p)
	if err != nil {
		return n, fmt.Errorf("show the play's output: %w", err)
	}
	return n, nil
}

func (s *watch) matched() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hit
}
