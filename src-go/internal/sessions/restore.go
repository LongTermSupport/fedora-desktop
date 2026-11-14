package sessions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/conversation"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

// unlockFileEnv names the file holding the SSH key passphrase on a headless server.
const unlockFileEnv = "CCY_RESTORE_SSH_PASSPHRASE_FILE"

// unlockFileFix is what to do about a passphrase file that cannot be used.
const unlockFileFix = "Re-run play-claude-yolo.yml on this server (it writes the file from github_ssh_passphrase in host_vars)."

// flagCheck is `restore --check`: judge every record, start nothing.
const flagCheck = "--check"

// The words a record that cannot be restored is reported by: the manifest's detail for a
// not-started entry, and what `restore --check` prints after UNRESTORABLE.
const (
	whyDirectoryGone      = "directory-gone"
	whyNoConversation     = "no-conversation"
	whyNoComposeAnswer    = "no-compose-answer"
	whyArgumentsUnknown   = "arguments-unworkable"
	whyStartFailed        = "start-failed"
	whyRecordUnreadable   = "record-unreadable"
	checkLineRestorable   = " RESTORABLE"
	checkLineUnrestorable = " UNRESTORABLE "
	// detailFresh is the manifest's detail for a session started fresh, not resumed.
	detailFresh = "fresh"
)

// parseRestore reads the arguments after `restore`: nothing, --dry-run or --check.
func parseRestore(args []string) (string, string) {
	if len(args) == 0 {
		return "", ""
	}
	if args[0] != flagDryRun && args[0] != flagCheck {
		return "", fmt.Sprintf("unknown restore option '%s'.", args[0])
	}
	if len(args) > 1 {
		return "", fmt.Sprintf("unexpected argument '%s'.", args[1])
	}
	return args[0], ""
}

// runRestore starts every recorded session that is not running, or with --check judges them.
func (a *App) runRestore(ctx context.Context, args []string) term.ExitCode {
	mode, problem := parseRestore(args)
	if problem != "" {
		return a.usageError(problem)
	}
	if mode == flagCheck {
		return a.checkRestore()
	}
	if err := a.restoreSessions(ctx, mode == flagDryRun); err != nil {
		return a.failed(err)
	}
	return term.ExitOK
}

// restorePlan is how one record would be started, or why it cannot be: why is a word from the
// list above, empty when it can, and sentence says the same to a person. start is
// conversation.StartPlan's answer.
type restorePlan struct {
	why, sentence string
	start         conversation.Start
}

// planRestore decides whether a record can be restored, by the rules the restore starts it by:
// its directory is there, and conversation.StartPlan can start it (its own recorded
// conversation, or fresh for a record marked fresh-if-no-conversation that has none to resume,
// with arguments that can be worked out). `restore`, `restore --check` and `declare` all ask
// this, so the check cannot pass a record the restore would refuse.
func (a *App) planRestore(rec registry.Record, file string) restorePlan {
	if info, statErr := os.Stat(rec.Dir); statErr != nil || !info.IsDir() {
		return restorePlan{why: whyDirectoryGone, sentence: "its directory no longer exists: " + rec.Dir +
			" (the record is kept at " + file + "; remove it if the project is gone)."}
	}
	start, err := conversation.StartPlan(rec, conversation.ClaudeHome(a.Getenv))
	var argsErr *conversation.ArgsError
	switch {
	case errors.As(err, &argsErr):
		return restorePlan{why: whyArgumentsUnknown, sentence: "the arguments to restore it with could not be worked out: " +
			err.Error() + " (the record is kept at " + file + ")."}
	case err != nil:
		return restorePlan{why: whyNoConversation, sentence: err.Error() + " (the record is kept at " + file + ")."}
	}
	return restorePlan{start: start}
}

// judgeRestore is planRestore with composeUnanswered: whether the next boot can restore the
// record unattended, as `restore --check` and `declare` judge it.
func (a *App) judgeRestore(rec registry.Record, file string) restorePlan {
	plan := a.planRestore(rec, file)
	if plan.why == "" {
		if unanswered := composeUnanswered(rec, file); unanswered != "" {
			return restorePlan{why: whyNoComposeAnswer, sentence: unanswered}
		}
	}
	return plan
}

// noComposeWhyEnv is what the launcher's compose question says, in a restore, about why the
// record holds no compose answer (ccy_compose_answer); noComposeProvisioned is its value for a
// record provisioning wrote.
const (
	noComposeWhyEnv      = "CCY_RESTORE_NO_COMPOSE_WHY"
	noComposeProvisioned = "the record was provisioned without one: declare it with -- --compose start or skip"
)

// prefixCcy is a ccy session's launcher prefix.
const prefixCcy = "ccy"

// composeFiles are the compose files the launcher's has_compose_files looks for in a project.
var composeFiles = []string{"docker-compose.yml", "docker-compose.yaml", "podman-compose.yml", "podman-compose.yaml", "compose.yml", "compose.yaml"}

// composeUnanswered is why a ccy record would stop its first restore at the compose question,
// or "": its project has compose files, it holds no compose= answer, and its launch can meet the
// question (a recorded --no-network meets none). `restore --check` and `declare` refuse such a
// record (fedora-desktop#270); the restore still starts it, so a person at an attended reboot
// can answer in its pane.
func composeUnanswered(rec registry.Record, file string) string {
	if rec.Prefix != prefixCcy || rec.Compose != "" || slices.Contains(ownArgs(rec.Args), "--no-network") {
		return ""
	}
	var found []string
	for _, name := range composeFiles {
		if info, err := os.Stat(filepath.Join(rec.Dir, name)); err == nil && info.Mode().IsRegular() {
			found = append(found, name)
		}
	}
	if len(found) == 0 {
		return ""
	}
	has := ", and " + rec.Dir + " has compose files (" + strings.Join(found, ", ") +
		"), so its first restore would stop at the compose question. "
	fresh := ""
	if rec.FreshIfNoConversation {
		fresh = " --fresh-if-no-conversation"
	}
	declare := "ccy-sessions declare --replace --name " + rec.Name + fresh + " " + rec.Dir + " -- --compose start|skip ..."
	if rec.FreshIfNoConversation {
		return "its record was provisioned without a compose answer" + has + "Declare it with one: " + declare +
			" (the record is kept at " + file + ")."
	}
	return "its record holds no compose answer (a ccy older than 3.89.1 wrote none, and a launch that never met the compose question writes none)" +
		has + "Launch it once with --compose start|skip, or declare it with one: " + declare + " (the record is kept at " + file + ")."
}

// ownArgs is the launcher's own arguments of a recorded launch: those before any `--`.
func ownArgs(args []string) []string {
	if end := slices.Index(args, "--"); end >= 0 {
		return args[:end]
	}
	return args
}

// restoreEnabled says whether this user's restore unit is enabled (the wants-symlink the play
// manages). A link that points at nothing pulls in nothing at boot, so it counts as off.
func (a *App) restoreEnabled() bool {
	_, err := os.Stat(a.Getenv("HOME") + "/.config/systemd/user/default.target.wants/ccy-sessions-restore.service")
	return err == nil
}

// checkRestore judges every record the next boot's restore would start, live sessions included
// (a reboot takes them down too), and starts nothing: "<name> RESTORABLE" or
// "<name> UNRESTORABLE <why>" on stdout, each refusal's sentence on stderr. Exit 1 if any record
// cannot be restored or the registry cannot be read. With the restore off, the next boot restores
// nothing, so there is nothing to judge. The self-update cycle asks this before it warns anyone of
// a reboot (fedora-desktop#108).
func (a *App) checkRestore() term.ExitCode {
	if !a.restoreEnabled() {
		a.sayf("session restore is off for this user (ccy-sessions-restore.service is not enabled), so the next boot restores no session; nothing to check.\n")
		return term.ExitOK
	}
	regdir, err := registry.Dir(a.Getenv)
	if err != nil {
		a.failErr(err)
		return term.ExitFailure
	}
	absent, err := a.registryListable(regdir)
	if err != nil {
		return term.ExitFailure
	}
	if absent {
		a.sayf("ccy session registry: nothing to restore (%s does not exist).\n", regdir)
		return term.ExitOK
	}
	reading, err := registry.New(regdir).ReadAll()
	if err != nil {
		a.failErr(err)
		return term.ExitFailure
	}
	var lines strings.Builder
	var refused []string
	restorable := 0
	for _, p := range reading.Problems {
		a.failErr(p.Err)
		name := filepath.Base(p.File)
		lines.WriteString(name + checkLineUnrestorable + whyRecordUnreadable + "\n")
		refused = append(refused, name)
	}
	for _, rec := range reading.Records {
		if !a.restoreTries(rec) {
			continue
		}
		plan := a.judgeRestore(rec, filepath.Join(regdir, rec.Name))
		if plan.why != "" {
			a.fail(rec.Name + " cannot be restored: " + plan.sentence)
			lines.WriteString(rec.Name + checkLineUnrestorable + plan.why + "\n")
			refused = append(refused, rec.Name)
			continue
		}
		if plan.start.Fresh {
			a.sayf("%s will be started fresh (its record is marked fresh-if-no-conversation): %s.\n", rec.Name, plan.start.Why)
		}
		lines.WriteString(rec.Name + checkLineRestorable + "\n")
		restorable++
	}
	a.out(lines.String())
	if len(refused) > 0 {
		a.fail(fmt.Sprintf("%d recorded session(s) could not be restored after a reboot: %s.", len(refused), strings.Join(refused, ", ")))
		return term.ExitFailure
	}
	a.sayf("every recorded session can be restored after a reboot (%d).\n", restorable)
	return term.ExitOK
}

// restoreTries says whether the restore would try to start the record, and says why not when
// it would not: marked no-restore, or an opencode session.
func (a *App) restoreTries(rec registry.Record) bool {
	if !rec.Restore {
		a.sayf("skip %s: marked no-restore.\n", rec.Name)
		return false
	}
	if registry.Opencode(rec.Prefix, rec.Args) {
		a.sayf("skip %s: an opencode session is not restored (Plan 00215); start it again with ccy --opencode.\n", rec.Name)
		return false
	}
	return true
}

// restoreRun is one restore in progress.
type restoreRun struct {
	app        *App
	dryRun     bool
	passphrase string
	regdir     string
	live       map[string]bool
	started    int
	failures   int
	entries    []restore.Entry
}

// restoreSessions starts every recorded session that is not running. Per record: marked
// no-restore, skipped; its session name already live, skipped (a hand-started session is
// ordinary, and starting a second under the same name is impossible anyway); its directory
// gone, an ERROR with the record kept for the operator, and the run goes on to the next record
// so one deleted checkout does not hold back every other session. The status is non-zero if
// anything failed. Nothing is started at all if the live set cannot be read: restoring on top
// of an unknown set could double every session.
//
// Each session is started with CCY_SESSION_RESTORE=1, which lets the launcher answer the
// prompts that have one safe answer. A ccy record's compose outcome becomes --compose. The
// rest still ask, in the pane, and `verify-restore` names them from the manifest written at the
// end. This returns as soon as the sessions exist, because a boot's user manager waits for it.
//
// On a headless server the unit's drop-in also sets CCY_RESTORE_SSH_PASSPHRASE_FILE. It is
// checked before anything starts, and a bad one starts nothing: every ccy session would only
// stop at a passphrase prompt. It is then dropped from the environment, so tmux's server, whose
// environment every later pane inherits, never holds it. Each ccy session is given its path on
// its own command (never its contents); a cc session starts no container and loads no key.
func (a *App) restoreSessions(ctx context.Context, dryRun bool) error {
	passphrase := a.cfg.PassphraseFile
	if err := a.Unsetenv(unlockFileEnv); err != nil {
		a.failErr(err)
		return errReported
	}
	if passphrase != "" && !a.passphraseUsable(passphrase) {
		a.fail("nothing is restored: every ccy session would stop at its SSH key passphrase prompt.")
		return errReported
	}
	regdir, err := registry.Dir(a.Getenv)
	if err != nil {
		a.failErr(err)
		return errReported
	}
	run := &restoreRun{app: a, dryRun: dryRun, passphrase: passphrase, regdir: regdir}
	manifest := restore.NewStore(regdir, a.cfg.BootIDFile)
	absent, err := a.registryListable(regdir)
	if err != nil {
		return err
	}
	if absent {
		a.sayf("ccy session registry: nothing to restore (%s does not exist).\n", regdir)
		run.writeManifest(manifest)
		return run.result()
	}
	sessions, err := a.Tmux.List(ctx)
	if err != nil {
		a.failErr(err)
		a.fail("the live session list could not be read, so nothing is restored: starting sessions on top of an unknown live set could double every one of them.")
		return errReported
	}
	run.live = map[string]bool{}
	for _, s := range sessions {
		run.live[s.Name] = true
	}
	reading, err := registry.New(regdir).ReadAll()
	if err != nil {
		a.failErr(err)
		return errReported
	}
	return run.restoreAll(ctx, reading, manifest)
}

// registryListable says whether the registry directory is absent (an empty registry), and
// refuses one that is there but cannot be listed: it would read as "nothing to restore" on a
// boot that restored nothing.
func (a *App) registryListable(regdir string) (bool, error) {
	info, err := os.Stat(regdir)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	const readable, searchable = 4, 1
	if err != nil || !info.IsDir() || syscall.Access(regdir, readable|searchable) != nil {
		a.fail("the session registry " + regdir + " is not a readable directory, so the sessions recorded there cannot be listed; nothing is restored.")
		return false, errReported
	}
	return false, nil
}

// writeManifest merges the entries into this boot's manifest, counting a failure.
func (r *restoreRun) writeManifest(manifest *restore.Store) {
	if r.dryRun {
		return
	}
	warn := func(err error) {
		r.app.sayf("ccy session restore: replacing the earlier restore manifest: %s\n", err)
	}
	if err := manifest.Merge(r.entries, warn); err != nil {
		r.app.failErr(err)
		r.failures++
	}
}

// result is the restore's outcome: a failure if anything failed.
func (r *restoreRun) result() error {
	if r.failures > 0 {
		return errReported
	}
	return nil
}

// restoreAll restores each record in file-name order and reports the totals.
func (r *restoreRun) restoreAll(ctx context.Context, reading registry.Reading, manifest *restore.Store) error {
	a := r.app
	// An unreadable record is a failure, and the reader's reason is shown. It has no name to
	// read, so its file's name stands for it in the manifest.
	for _, p := range reading.Problems {
		a.failErr(p.Err)
		r.notStarted(restore.Entry{Name: filepath.Base(p.File)}, whyRecordUnreadable)
	}
	for _, rec := range reading.Records {
		r.record(ctx, rec)
	}
	r.writeManifest(manifest)
	if len(reading.Records)+len(reading.Problems) == 0 {
		a.sayf("ccy session registry: nothing to restore (no records in %s).\n", r.regdir)
	} else {
		a.sayf("ccy session restore: %d started, %d failed.\n", r.started, r.failures)
	}
	return r.result()
}

// notStarted counts a session the restore could not start and puts it in the manifest as
// not-started with why, so that verify-restore reports it: left out, an all-failed restore would
// write an empty manifest, which verify-restore reads as nothing to verify (fedora-desktop#108).
func (r *restoreRun) notStarted(entry restore.Entry, why string) {
	r.failures++
	entry.Going, entry.Detail = restore.GoingNotStarted, why
	r.entries = append(r.entries, entry)
}

// record restores one session from its record.
func (r *restoreRun) record(ctx context.Context, rec registry.Record) {
	a := r.app
	file := filepath.Join(r.regdir, rec.Name)
	if !a.restoreTries(rec) {
		return
	}
	entry := restore.Entry{
		Name: rec.Name, Prefix: rec.Prefix, Dir: rec.Dir, Resume: registry.ResumeID(rec.Args),
		Supervised: registry.Supervised(rec.Prefix, rec.Args), Going: restore.GoingPending,
	}
	if r.live[rec.Name] {
		// Still one of the sessions that should be up, so still one to verify. Not this
		// restore's to set going: whoever started it is driving it, and an entry an earlier
		// restore this boot made for it is kept as it is.
		a.sayf("skip %s: already running.\n", rec.Name)
		if id, convErr := conversation.For(rec, conversation.ClaudeHome(a.Getenv)); convErr == nil {
			entry.Resume = id
		}
		entry.Going = restore.GoingKeep
		r.entries = append(r.entries, entry)
		return
	}
	plan := a.planRestore(rec, file)
	if plan.why != "" {
		a.fail("cannot restore " + rec.Name + ": " + plan.sentence)
		r.notStarted(entry, plan.why)
		return
	}
	args := plan.start.Args
	entry.Resume = plan.start.ID
	fresh := plan.start.Fresh
	if fresh {
		// Nothing to compact or continue: set-going leaves a none entry alone, and verify-restore
		// judges it by its screen. The detail says it was not a resume.
		entry.Going, entry.Detail = restore.GoingNone, detailFresh
	}
	if r.dryRun {
		how, suffix := "", ""
		if fresh {
			how, suffix = " fresh", " (no conversation to resume)"
		}
		a.sayf("would start %s%s in %s%s: %s %s\n", rec.Name, how, rec.Dir, suffix, rec.Launcher, strings.Join(args, " "))
		return
	}
	command := []string{"env", "CCY_SESSION_RESTORE=1"}
	if r.passphrase != "" && rec.Prefix == prefixCcy {
		command = append(command, unlockFileEnv+"="+r.passphrase)
	}
	if rec.Prefix == prefixCcy && rec.Compose == "" && rec.FreshIfNoConversation {
		// The pane says why the compose question is asked: not a record from before compose=.
		command = append(command, noComposeWhyEnv+"="+noComposeProvisioned)
	}
	command = append(append(command, rec.Launcher), args...)
	spec := tmuxccy.StartSpec{Name: rec.Name, Dir: rec.Dir, Command: command, RecordPath: file, Prefix: rec.Prefix}
	if err := a.Tmux.StartDetached(ctx, spec); err != nil {
		a.failErr(err)
		a.fail("could not start " + rec.Name + " in " + rec.Dir + " (the record is kept at " + file + ").")
		r.notStarted(entry, whyStartFailed)
		return
	}
	if fresh {
		a.sayf("started %s fresh in %s (its record is marked fresh-if-no-conversation): %s.\n", rec.Name, rec.Dir, plan.start.Why)
	} else {
		a.sayf("restored %s in %s.\n", rec.Name, rec.Dir)
	}
	r.started++
	r.entries = append(r.entries, entry)
}

// passphraseUsable says whether file is a usable SSH key passphrase file for a restore: a
// regular file, owned by this user, readable by nobody else, not empty. It says why not on
// stderr. A session that went ahead without it would stop at ssh-add's passphrase prompt with
// nobody to answer.
func (a *App) passphraseUsable(file string) bool {
	info, err := os.Stat(file)
	if err != nil || !info.Mode().IsRegular() {
		a.fail("the SSH key passphrase file for session restore is missing: " + file + ". " + unlockFileFix)
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		a.fail("could not read the owner and mode of the session-restore passphrase file " + file + ".")
		return false
	}
	if int(stat.Uid) != a.Getuid() {
		a.fail(fmt.Sprintf("the session-restore passphrase file %s is owned by uid %d, not by you. %s", file, stat.Uid, unlockFileFix))
		return false
	}
	const permissionBits = 0o7777
	if mode := fmt.Sprintf("%o", stat.Mode&permissionBits); mode != "600" && mode != "400" {
		a.fail(fmt.Sprintf("the session-restore passphrase file %s has mode %s; it must be readable by you alone (600). %s", file, mode, unlockFileFix))
		return false
	}
	if !readable(file) || info.Size() == 0 {
		a.fail("the session-restore passphrase file " + file + " is unreadable or empty. " + unlockFileFix)
		return false
	}
	return true
}

// readable reports whether the file can be opened for reading.
func readable(path string) bool {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return false
	}
	return f.Close() == nil
}
