// Package lightning is the agent-bus-lightning command (Plan 00254 Task 5.2,
// DESIGN-client-choice.md): one Lightning instance per team the desktop's human watches in
// Lightning, each with its own data, settings and cache, a settings file naming the team's
// homeserver, and a launcher. play-agent-bus-element.yml runs it as the human's desktop user,
// with the agent_bus_element_teams list on stdin, each entry's client resolved.
//
// Lightning has no profile option; each team's instance is its own XDG_DATA_HOME,
// XDG_CONFIG_HOME and XDG_CACHE_HOME under the app's data, set inside the sandbox through
// `flatpak run --command=env` (RESEARCH-integration.md section 1). The launcher also sets
// SSL_CERT_FILE to the runtime's public bundle and SSL_CERT_DIR to the folder of team CAs
// tasks/lightning-team-ca.yml fills, for every team, http ones too: the two can only add trust.
//
// Expectations (each has a test whose name starts with its number):
//
//	L1 The arguments are exactly check, apply or watch; anything else is a usage error (64).
//	L2 Run as root, it refuses (78) before reading anything.
//	L3 Stdin is a JSON array of objects, a repeated key refused, each entry checked as
//	   helpers/agent_bus/element.py checks it: the keys team, base_url, server_name, ca_file,
//	   client and lightning_diagnostics only; team and base_url required; team in pingbus's team
//	   grammar; base_url https://<host>[:port], or http:// to an IP literal; server_name, by
//	   default <team>.agent-bus.internal, a lower-case DNS name ending in .internal; ca_file only
//	   for an https team, an absolute plain path; client element (the default) or lightning;
//	   lightning_diagnostics true or false (the default), true only for a lightning team; a team
//	   listed once. A refusal (78) names the entry or team and the key, never the value.
//	L4 The user may hold no pingbus bundles: ~/.config/pingbus there in any form, a dangling
//	   link included, is a refusal (78), with element.py check_user's one exception, a directory
//	   holding only the owner's consent record as pingbus counts it (matrixclients.List.Accepted).
//	   A path that cannot be looked at is a refusal, not a pass.
//	L5 check validates and refuses only; it writes nothing.
//	L6 apply gives each lightning team teams/<team>/{data,config,cache} under the app's data,
//	   with teams and teams/<team> themselves, mode 0700, narrowing a wider mode; anything there
//	   that is not a directory (a link included) is refused, never followed.
//	L7 The settings file config/MatrixClient/matrix-client.conf, "[homeserver]" and
//	   "url=<base_url>", is written only when absent, mode 0600: Lightning rewrites and owns it
//	   afterwards. Anything there that is not a regular file is refused.
//	L8 The launcher ~/.local/share/applications/agent-bus-<team>-lightning.desktop is rewritten
//	   when its content or mode (0644) differs; a non-regular file there is refused.
//	L9 A launcher of that name for a valid team not declared for Lightning is removed only when
//	   it is a regular file holding the Exec line this command writes for that team; anything
//	   else, a link or a pipe included, is left alone and never opened.
//	L10 No team's instance is ever removed: a team declared for Element keeps its instance, and
//	   one the list does not declare keeps it and is named on a "KEPT<TAB><path>" line each run.
//	L11 stdout is a "CHANGED<TAB><path>" line for each change, in order, then the KEPT lines; a
//	   failure part way still prints the changes made before it, then one "agent-bus-lightning: "
//	   line on stderr.
//	L12 A home a desktop entry's Exec line would have to quote is refused when a team is to be
//	   written; a home that cannot be found is a failure (1).
//	L13 A line that cannot be written to stdout is a failure (1).
//	L14 The one Matrix client list (matrixclients) names the folder the teams' instances live in.
//	L15 HoldsAccount says whether a team's instance holds a person's account: the folder
//	   Lightning keeps it in, named from the user ID as Lightning names it, is a directory.
//	   ccy-teams asks it for a team that opens in Lightning (Plan 00254 Task 7.1's gap).
//	L16 A team with lightning_diagnostics true launches with LIGHTNING_SCROLL_MEMORY_TRACE=1 and
//	   LIGHTNING_SYNC_TRACE=100, and lightning-matrix --log-file
//	   <team>/cache/lightning-diagnostic.log (Plan 00268 Task 1.4); turning it on or off rewrites
//	   the launcher (L8), and either launcher is this command's (L9). Lightning writes the log;
//	   this command never does.
//	L17 watch is the diagnostics watchdog (internal/lightning/watch, Plan 00268 Task 1.7) over
//	   the instances under this user's teams folder: it reads no stdin and writes nothing to
//	   stdout, names the folder on stderr, looks once every watch.Interval until stopped (exit 0),
//	   and ends with 1 on a look that fails, for systemd to restart it. It watches only this
//	   user's processes, and reads only /proc and the diagnostic logs' modification times.
package lightning

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/lightning/watch"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/matrixclients"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/stdinread"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	usage  = "usage: agent-bus-lightning check|apply|watch (check and apply read the team list as JSON on stdin)"
	prefix = "agent-bus-lightning: "

	clientName = "Lightning" // the list's name for the client
	teamsDir   = "teams"
	caDir      = "teamchat-ca" // where tasks/lightning-team-ca.yml puts each https team's CA
	runtimeCA  = "/etc/ssl/certs/ca-certificates.crt"
	command    = "lightning-matrix"
	appsDir    = ".local/share/applications"
	settingsIn = "config/MatrixClient/matrix-client.conf"

	dirMode      fs.FileMode = 0o700
	settingsMode fs.FileMode = 0o600
	launcherMode fs.FileMode = 0o644
)

// instanceDirs are the folders of each team's instance, in the order the launcher names them.
var instanceDirs = []string{"data", "config", "cache"}

// diagnosticLogName is the file, in an instance's cache folder, Lightning mirrors its log to
// with diagnostics on (L16). Lightning caps it, rolling once to <name>.1 at 32 MiB (Plan 00268
// RESEARCH.md 7.1 names the releases that do).
const diagnosticLogName = "lightning-diagnostic.log"

// diagnosticEnv is what a team with diagnostics on launches with (L16; Plan 00268 RESEARCH.md
// 7.1): a memory line every 5 s, and a line for each event that takes 100 ms or more from the
// homeserver to the screen (the lowest threshold Lightning takes; the Flathub build compiles the
// line for every event out). None of them logs message content, keys or tokens.
var diagnosticEnv = []string{
	"LIGHTNING_SCROLL_MEMORY_TRACE=1",
	"LIGHTNING_SYNC_TRACE=100",
}

// launcherName is a launcher this command could have written; the group is its team.
var launcherName = regexp.MustCompile(`^agent-bus-(.+)-lightning\.desktop$`)

// execSafe is a path that needs no quotes in an Exec line.
var execSafe = regexp.MustCompile(`^/[A-Za-z0-9._/+@-]*$`)

// App is one run of the command.
type App struct {
	Args   []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Euid is the effective uid, root refused, and UID the real one, whose consent record
	// counts: element.py's os.geteuid and os.getuid.
	Euid, UID int
	// Home is a uid's passwd home.
	Home func(uid int) (string, error)
	// Clock times the wait for the team list on Stdin, given up on once nothing has arrived for
	// stdinread.PipedGiveUp (issue 260); the real clock when nil.
	Clock stdinread.Clock
	// Watch is what the watch verb runs on (L17); without it the verb fails.
	Watch *WatchEnv
}

// WatchEnv is the watch verb's world.
type WatchEnv struct {
	Ctx  context.Context // stopping it ends the watch, with 0
	Proc fs.FS           // /proc
	Run  execx.Runner    // the backtrace tools
	Now  func() time.Time
	Wait func() <-chan time.Time // one watch.Interval: IntervalWait on the real clock
}

// IntervalWait is WatchEnv.Wait on the real clock. The bound is the constant watch.Interval, so
// no caller can shorten it into a race.
func IntervalWait() <-chan time.Time { return time.After(watch.Interval) }

// refusal is a refused run (78), element.py's ElementError.
type refusal struct{ msg string }

func (r refusal) Error() string { return r.msg }

func refuse(format string, args ...any) error { return refusal{fmt.Sprintf(format, args...)} }

func isRefusal(err error) bool { return errors.As(err, new(refusal)) }

// Run is the command; it returns the exit code.
func (a App) Run() term.ExitCode {
	if len(a.Args) != 1 || !slices.Contains([]string{"check", "apply", "watch"}, a.Args[0]) {
		return a.fail(term.ExitUsage, usage)
	}
	if a.Euid == 0 {
		return a.report(refuse("run as the human's desktop user, not root"))
	}
	home, err := a.Home(a.Euid)
	if err != nil {
		return a.fail(term.ExitFailure, "the desktop user's home: "+err.Error())
	}
	list, err := matrixclients.Load()
	if err != nil {
		return a.fail(term.ExitFailure, err.Error())
	}
	if a.Args[0] == "watch" {
		return a.watch(list, home)
	}
	data, err := stdinread.Read(a.Stdin, stdinread.Options{
		Prog: "agent-bus-lightning " + a.Args[0], Err: a.Stderr, GiveUpAfter: stdinread.PipedGiveUp, Clock: a.Clock,
	})
	if err != nil {
		return a.fail(term.ExitFailure, "reading the team list: "+err.Error())
	}
	entries, err := parseEntries(string(data))
	if err != nil {
		return a.report(err)
	}
	if refused := checkUser(list, home, a.UID); refused != nil {
		return a.report(refused)
	}
	if a.Args[0] == "check" {
		return term.ExitOK
	}
	client, err := lightningClient(list)
	if err != nil {
		return a.fail(term.ExitFailure, err.Error())
	}
	t := tree{home: home, root: filepath.Join(home, client.FlatpakDir()), appID: client.AppID}
	lines, applyErr := t.apply(entries)
	for _, line := range lines {
		if err := writeAll(a.Stdout, line+"\n"); err != nil {
			return a.fail(term.ExitFailure, "writing the changes: "+err.Error())
		}
	}
	if applyErr != nil {
		return a.report(applyErr)
	}
	return term.ExitOK
}

// watch looks at the instances under home's teams folder every watch.Interval until a.Watch.Ctx
// stops (L17).
func (a App) watch(list matrixclients.List, home string) term.ExitCode {
	env := a.Watch
	if env == nil {
		return a.fail(term.ExitFailure, "watch: no watch environment was given")
	}
	client, err := lightningClient(list)
	if err != nil {
		return a.fail(term.ExitFailure, err.Error())
	}
	instances := filepath.Join(home, client.FlatpakDir(), teamsDir)
	if err := writeAll(a.Stderr, prefix+"watching the instances in "+instances+"\n"); err != nil {
		return term.ExitFailure
	}
	w := &watch.Watcher{
		Proc: env.Proc, Run: env.Run, Err: a.Stderr, UID: a.Euid,
		Instances: instances, LogName: diagnosticLogName,
	}
	for {
		if err := w.Tick(env.Ctx, env.Now()); err != nil {
			if env.Ctx.Err() != nil {
				return term.ExitOK
			}
			return a.fail(term.ExitFailure, "watch: "+err.Error())
		}
		select {
		case <-env.Ctx.Done():
			return term.ExitOK
		case <-env.Wait():
		}
	}
}

// report is a refusal's 78, or any other error's 1.
func (a App) report(err error) term.ExitCode {
	if isRefusal(err) {
		return a.fail(term.ExitConfig, err.Error())
	}
	return a.fail(term.ExitFailure, err.Error())
}

func (a App) fail(code term.ExitCode, msg string) term.ExitCode {
	if err := writeAll(a.Stderr, prefix+msg+"\n"); err != nil {
		return term.ExitFailure
	}
	return code
}

// writeAll is the whole of text written to w, or why not.
func writeAll(w io.Writer, text string) error {
	n, err := io.WriteString(w, text)
	if err == nil && n < len(text) {
		err = io.ErrShortWrite
	}
	return err
}

func lightningClient(list matrixclients.List) (matrixclients.Client, error) {
	at := slices.IndexFunc(list.Clients, func(c matrixclients.Client) bool { return c.Name == clientName })
	if at < 0 {
		return matrixclients.Client{}, errors.New("the Matrix client list names no " + clientName)
	}
	return list.Clients[at], nil
}

// checkUser is element.py check_user (L4): a user who holds pingbus bundles (~/.config/pingbus,
// the default PINGBUS_HOME, there in any form) is refused, except when it is a real directory
// holding only the owner's consent record, as pingbus counts it.
func checkUser(list matrixclients.List, home string, uid int) error {
	dir := filepath.Join(home, matrixclients.AcceptedDir)
	info, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return refuse("%s cannot be checked for pingbus bundles (%s)", dir, cause(err))
	}
	var names []string
	if info.IsDir() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return refuse("%s cannot be checked for pingbus bundles (%s)", dir, cause(err))
		}
		for _, e := range entries {
			names = append(names, e.Name())
		}
	}
	if slices.Equal(names, []string{matrixclients.AcceptedFile}) {
		if list.Accepted(home, uid) != nil {
			return nil
		}
		return refuse("%s holds only the Matrix consent record, and the acceptance record is not valid "+
			"(it must be a regular file owned by this user, readable by no one else, in a directory this "+
			"user owns, holding a consent record pingbus can read); remove it and rerun cc --teams to "+
			"write it again", dir)
	}
	return refuse("%s exists: this user holds pingbus bundles, so no Lightning session may live beside "+
		"them; configure Lightning for a human user, never an agent user", dir)
}

// cause is an OS error's own words, without the path the message already names.
func cause(err error) string {
	if pathErr, ok := errors.AsType[*fs.PathError](err); ok {
		return pathErr.Err.Error()
	}
	return err.Error()
}

// tree is the Lightning folders and launchers under one home.
type tree struct{ home, root, appID string }

func (t tree) team(team string) string { return filepath.Join(t.root, teamsDir, team) }

func (t tree) launcher(team string) string { return Launcher(t.home, team) }

// Launcher is a team's Lightning launcher under a home (L8): ccy-teams opens the team by it.
func Launcher(home, team string) string {
	return filepath.Join(home, appsDir, "agent-bus-"+team+"-lightning.desktop")
}

// accountsIn is the folder of an instance Lightning keeps each account in: its primaryRoot,
// <XDG_DATA_HOME>/MatrixClient/matrix-client (src/storage/AppDataPaths.cpp), one folder each.
const accountsIn = "data/MatrixClient/matrix-client"

// HoldsAccount is whether team's Lightning instance under home holds userID's account (L15):
// the folder Lightning names by safeUserSlug, the ID without its "@" and with every ":", "/"
// and "\" an underscore. Lightning makes it at sign-in and removes it at sign-out, so a missing
// one is an instance that person is not signed in to, whether or not the homeserver holds a
// session of theirs from another client. A user ID not of the form @<localpart>:<server>, or a
// folder that cannot be looked at or is not a directory, is an error.
func HoldsAccount(home, team, userID string) (bool, error) {
	local, server, ok := strings.Cut(strings.TrimPrefix(userID, "@"), ":")
	if !strings.HasPrefix(userID, "@") || !ok || local == "" || server == "" {
		return false, fmt.Errorf("%q is not a Matrix user ID (@<localpart>:<server>)", userID)
	}
	list, err := matrixclients.Load()
	if err != nil {
		return false, err
	}
	client, err := lightningClient(list)
	if err != nil {
		return false, err
	}
	slug := strings.NewReplacer(":", "_", "/", "_", "\\", "_").Replace(local + ":" + server)
	t := tree{home: home, root: filepath.Join(home, client.FlatpakDir()), appID: client.AppID}
	path := filepath.Join(t.team(team), accountsIn, slug)
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("whether %s's Lightning instance holds %s's account cannot be told: %w", team, userID, err)
	case !info.IsDir():
		return false, fmt.Errorf("%s is not a folder, so whether %s's Lightning instance holds %s's account cannot be told", path, team, userID)
	}
	return true, nil
}

// execLine is the launcher's Exec line for a team, every path absolute (flatpak expands none);
// with diagnostics, the traces and the log file of L16.
func (t tree) execLine(team string, diagnostics bool) string {
	dir := t.team(team)
	words := make([]string, 0, 4+len(instanceDirs)+2+len(diagnosticEnv)+3)
	words = append(words, "Exec=flatpak", "run", "--command=env", t.appID)
	for _, name := range instanceDirs {
		words = append(words, "XDG_"+strings.ToUpper(name)+"_HOME="+filepath.Join(dir, name))
	}
	words = append(words, "SSL_CERT_FILE="+runtimeCA, "SSL_CERT_DIR="+filepath.Join(t.root, caDir))
	if diagnostics {
		words = append(words, diagnosticEnv...)
	}
	words = append(words, command)
	if diagnostics {
		words = append(words, "--log-file", DiagnosticLog(dir))
	}
	return strings.Join(words, " ") + "\n"
}

// DiagnosticLog is the log file a team's instance writes with diagnostics on (L16), in the
// instance's own cache folder; the watchdog (internal/lightning/watch) watches it.
func DiagnosticLog(teamDir string) string {
	return filepath.Join(teamDir, "cache", diagnosticLogName)
}

func (t tree) launcherText(e entry) string {
	return "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Name=" + e.team + " team (Lightning)\n" +
		"Comment=Watch and command the " + e.team + " agent team\n" +
		t.execLine(e.team, e.diagnostics) +
		"Icon=" + t.appID + "\n" +
		"Terminal=false\n" +
		"Categories=Network;InstantMessaging;\n"
}

// apply writes every Lightning team, removes the stale launchers of ours and names the kept
// instances; it returns the output lines, with those of the changes made before any error.
func (t tree) apply(entries []entry) ([]string, error) {
	var w writer
	var mine []string
	for _, e := range entries {
		if e.client == clientLightning {
			mine = append(mine, e.team)
		}
	}
	if len(mine) > 0 && !execSafe.MatchString(t.root) {
		return nil, refuse("the home %s holds a character a launcher's Exec line would have to quote; "+
			"give the desktop user a plain home path", t.home)
	}
	for _, e := range entries {
		if e.client != clientLightning {
			continue
		}
		if err := t.write(&w, e); err != nil {
			return w.lines, err
		}
	}
	if err := t.prune(&w, mine); err != nil {
		return w.lines, err
	}
	kept, err := t.kept(entries)
	return append(w.lines, kept...), err
}

// write is one team's instance folders, settings file and launcher (L6 to L8).
func (t tree) write(w *writer, e entry) error {
	dir := t.team(e.team)
	settings := filepath.Join(dir, settingsIn)
	launcher := t.launcher(e.team)
	steps := []func() error{
		func() error { return w.parent(t.root) },
		func() error { return w.dir(filepath.Dir(dir)) },
		func() error { return w.dir(dir) },
	}
	for _, name := range instanceDirs {
		steps = append(steps, func() error { return w.dir(filepath.Join(dir, name)) })
	}
	steps = append(steps,
		func() error { return w.dir(filepath.Dir(settings)) },
		func() error { return w.fileIfAbsent(settings, "[homeserver]\nurl="+e.baseURL+"\n", settingsMode) },
		func() error { return w.parent(filepath.Dir(launcher)) },
		func() error { return w.file(launcher, t.launcherText(e), launcherMode) },
	)
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

// prune removes each launcher of ours whose team is not one of mine (L9).
func (t tree) prune(w *writer, mine []string) error {
	apps := filepath.Join(t.home, appsDir)
	entries, err := os.ReadDir(apps)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("listing %s: %w", apps, err)
	}
	for _, d := range entries {
		m := launcherName.FindStringSubmatch(d.Name())
		if m == nil || !teamName.MatchString(m[1]) || slices.Contains(mine, m[1]) || !d.Type().IsRegular() {
			continue
		}
		path := filepath.Join(apps, d.Name())
		data, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		ours := strings.Contains(string(data), t.execLine(m[1], false)) ||
			strings.Contains(string(data), t.execLine(m[1], true))
		if !ours {
			continue
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("removing %s: %w", path, err)
		}
		w.changed(path)
	}
	return nil
}

// kept is a KEPT line for each team instance whose team the list does not declare (L10).
func (t tree) kept(entries []entry) ([]string, error) {
	dir := filepath.Join(t.root, teamsDir)
	found, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", dir, err)
	}
	var lines []string
	for _, d := range found {
		name := d.Name()
		declared := slices.ContainsFunc(entries, func(e entry) bool { return e.team == name })
		if d.IsDir() && teamName.MatchString(name) && !declared {
			lines = append(lines, "KEPT\t"+filepath.Join(dir, name))
		}
	}
	return lines, nil
}

// writer makes the changes and keeps a CHANGED line for each.
type writer struct{ lines []string }

func (w *writer) changed(path string) { w.lines = append(w.lines, "CHANGED\t"+path) }

// parent makes a folder this command writes in but does not own (the app's data, the human's
// applications) when it is absent, and never judges its mode. A link or file there is refused.
func (w *writer) parent(path string) error {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if mkErr := os.MkdirAll(path, dirMode); mkErr != nil {
			return mkErr
		}
		w.changed(path)
		return nil
	case err != nil:
		return err
	case !info.IsDir():
		return refuse("%s is not a directory", path)
	}
	return nil
}

// dir makes a folder of this command's, mode 0700, or narrows its mode to that.
func (w *writer) dir(path string) error {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if mkErr := os.Mkdir(path, dirMode); mkErr != nil {
			return mkErr
		}
	case err != nil:
		return err
	case !info.IsDir():
		return refuse("%s is not a directory", path)
	case info.Mode().Perm() == dirMode:
		return nil
	}
	if err := os.Chmod(path, dirMode); err != nil {
		return err
	}
	w.changed(path)
	return nil
}

// file writes path whole when its content or mode differs; a non-regular file there is refused.
func (w *writer) file(path, content string, mode fs.FileMode) error {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	case !info.Mode().IsRegular():
		return refuse("%s is not a regular file", path)
	default:
		old, readErr := os.ReadFile(filepath.Clean(path))
		if readErr != nil {
			return readErr
		}
		if info.Mode().Perm() == mode && string(old) == content {
			return nil
		}
	}
	tmp, err := temporary(path, content, mode)
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return errors.Join(err, os.Remove(tmp))
	}
	w.changed(path)
	return nil
}

// fileIfAbsent writes path only when nothing is there; a non-regular file there is refused.
func (w *writer) fileIfAbsent(path, content string, mode fs.FileMode) error {
	info, err := os.Lstat(path)
	switch {
	case err == nil && !info.Mode().IsRegular():
		return refuse("%s is not a regular file", path)
	case err == nil:
		return nil
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}
	tmp, err := temporary(path, content, mode)
	if err != nil {
		return err
	}
	// A link, not a rename, so a file Lightning made since the look above is never replaced.
	linkErr := os.Link(tmp, path)
	if err := os.Remove(tmp); err != nil {
		return errors.Join(err, linkErr)
	}
	switch {
	case linkErr == nil:
		w.changed(path)
	case !errors.Is(linkErr, fs.ErrExist):
		return linkErr
	}
	return nil
}

// temporary is content written, at mode, to a new file beside path, never through a link.
func temporary(path, content string, mode fs.FileMode) (string, error) {
	tmp := filepath.Join(filepath.Dir(path), fmt.Sprintf(".%s.tmp-%d", filepath.Base(path), os.Getpid()))
	f, err := os.OpenFile(filepath.Clean(tmp), os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, mode)
	if err != nil {
		return "", err
	}
	if err := errors.Join(f.Chmod(mode), writeAll(f, content), f.Close()); err != nil {
		return "", errors.Join(err, os.Remove(tmp))
	}
	return tmp, nil
}
