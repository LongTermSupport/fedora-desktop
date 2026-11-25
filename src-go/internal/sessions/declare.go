package sessions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

// The words declare prints on stdout after the session's name: what it did to the record.
const (
	declaredNew       = " DECLARED"
	declaredUnchanged = " UNCHANGED"
	declaredReplaced  = " REPLACED"
	// declaredDeferred: the session runs under a record that is not the declared one (or none),
	// so nothing is written now; a converge meets this legitimately and is not failed by it.
	declaredDeferred = " DEFERRED"
)

// declaredName is a name a ccy session may have: the launcher's prefix, then the characters
// get_project_name leaves in a project name, so tmux takes it as it is.
var declaredName = regexp.MustCompile(`^ccy-[A-Za-z0-9_-]+$`)

// genericFolders are the parents get_project_name leaves out of a project name.
var genericFolders = []string{"projects", "repos", "work", "src", "code", "dev", "home"}

// declareRequest is the parsed command line of `declare`.
type declareRequest struct {
	name, dir      string
	fresh, replace bool
	launch         []string
}

// parseDeclare reads `declare [--name NAME] [--fresh-if-no-conversation] [--replace] DIR
// [-- CCY-ARGS...]`: every word after the first `--` is the ccy launch's argv, its own `--`
// included. It returns the request, or the usage mistake.
func parseDeclare(args []string) (declareRequest, string) {
	var req declareRequest
	for i := 0; i < len(args); i++ {
		w := args[i]
		switch {
		case w == "--":
			req.launch = args[i+1:]
			i = len(args)
		case w == "--name":
			if i+1 >= len(args) {
				return req, "--name needs a session name."
			}
			i++
			req.name = args[i]
		case w == "--fresh-if-no-conversation":
			req.fresh = true
		case w == "--replace":
			req.replace = true
		case strings.HasPrefix(w, "-"):
			return req, fmt.Sprintf("unknown declare option '%s'.", w)
		case req.dir != "":
			return req, fmt.Sprintf("unexpected argument '%s' (the ccy arguments go after --).", w)
		default:
			req.dir = w
		}
	}
	return req.complete()
}

// complete checks the directory and the name, and names the session as the launcher would when
// no name was given.
func (req declareRequest) complete() (declareRequest, string) {
	switch {
	case req.dir == "":
		return req, "declare needs the session's project directory."
	case !filepath.IsAbs(req.dir) || filepath.Clean(req.dir) != req.dir:
		return req, fmt.Sprintf("the project directory '%s' must be an absolute path with no '..', '.' or trailing '/', as the launcher records its folder.", req.dir)
	case req.name != "" && !declaredName.MatchString(req.name):
		return req, fmt.Sprintf("the session name '%s' is not ccy- followed by letters, digits, '_' or '-'.", req.name)
	}
	if req.name == "" {
		req.name = "ccy-" + projectName(req.dir)
	}
	return req, ""
}

// projectName is the launcher's get_project_name for dir: its folder's name, lower case, every
// character but a-z, 0-9, '_' and '-' made '_', after its parent's name the same way unless the
// parent is a generic folder.
func projectName(dir string) string {
	clean := func(s string) string {
		return strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
				return r
			}
			return '_'
		}, strings.ToLower(s))
	}
	project, parent := clean(filepath.Base(dir)), clean(filepath.Base(filepath.Dir(dir)))
	if slices.Contains(genericFolders, parent) {
		return project
	}
	return parent + "-" + project
}

// declareSession writes the record a ccy launch of the request's argv in its directory would
// write, for a session that is not running, so the next boot's restore starts it (Plan 00135
// Phase 13: provisioning's way to write a record for a session that has never run). The record
// goes through the registry's one writer and is judged by the restore's own planRestore first:
// what restore --check would call UNRESTORABLE is refused, and nothing is written. The same
// record again is a no-op; a different one under the name is replaced only with --replace, and
// never while that session is live: then it is DEFERRED, rc 0, and nothing is written.
func (a *App) declareSession(ctx context.Context, args []string) term.ExitCode {
	req, problem := parseDeclare(args)
	if problem != "" {
		return a.usageError(problem)
	}
	if a.Getuid() == 0 {
		a.fail("declare writes the session registry of the user it runs as, and root restores no session: run it as the user whose session it is, not root.")
		return term.ExitFailure
	}
	rec, err := declaredRecord(req)
	if err != nil {
		a.fail("nothing is declared: " + err.Error())
		return term.ExitFailure
	}
	store, err := registry.Locate(a.Getenv)
	if err != nil {
		a.failErr(err)
		return term.ExitFailure
	}
	reading, err := store.ReadAll()
	if err != nil {
		a.failErr(err)
		return term.ExitFailure
	}
	plan := a.judgeRestore(ctx, rec, keptFile(reading, rec.Name))
	if plan.why != "" {
		a.fail("nothing is declared: the boot restore could not start " + rec.Name + " (" + plan.why + "): " + plan.sentence)
		return term.ExitFailure
	}
	if plan.note != "" {
		a.sayf("%s: %s\n", rec.Name, plan.note)
	}
	verdict, err := a.declareInto(ctx, store, rec, req.replace)
	if err != nil {
		return a.failed(err)
	}
	if !a.restoreEnabled() {
		a.sayf("session restore is off for this user (ccy-sessions-restore.service is not enabled), so no boot starts %s until it is on.\n", rec.Name)
	}
	a.out(rec.Name + verdict + "\n")
	return term.ExitOK
}

// keptFile is the file a refused declare keeps: the one record already answering to name, read
// from whatever file it is in (fedora-desktop#398), or "" when there is none, or more than one
// and so no one record a refusal could name.
func keptFile(reading registry.Reading, name string) string {
	if files := reading.FilesOf(name); len(files) == 1 {
		return files[0]
	}
	return ""
}

// declareInto writes rec unless the same record is there, refusing a different one unless
// replace. While the session is live it writes nothing, with or without replace, and answers
// DEFERRED (rc 0), so a play that declares every session on every converge is not failed by a
// running one; the record is written by a declare after the session stops.
func (a *App) declareInto(ctx context.Context, store *registry.Store, rec registry.Record, replace bool) (string, error) {
	existing, err := store.Get(rec.Name)
	switch {
	case err == nil && sameRecord(existing, rec):
		return declaredUnchanged, nil
	case err != nil && !errors.Is(err, registry.ErrNoRecord):
		return "", fmt.Errorf("%w; it is left as it is (remove it, or declare with --replace once it can be read)", err)
	}
	had := err == nil
	sessions, err := a.Tmux.List(ctx)
	if err != nil {
		return "", fmt.Errorf("the live sessions could not be listed, so %s is not declared: %w", rec.Name, err)
	}
	if slices.ContainsFunc(sessions, func(s tmuxccy.Session) bool { return s.Name == rec.Name }) {
		a.sayDeferred(rec.Name, had)
		return declaredDeferred, nil
	}
	if had && !replace {
		return "", fmt.Errorf("%s already has a different record in %s; nothing is declared (declare with --replace to write this one over it)", rec.Name, store.Dir())
	}
	if err := store.Write(rec); err != nil {
		return "", err
	}
	if had {
		return declaredReplaced, nil
	}
	return declaredNew, nil
}

// sayDeferred is the one stderr line of a DEFERRED declare: why nothing was written, and when it is.
func (a *App) sayDeferred(name string, had bool) {
	if had {
		a.sayf("%s is running with a record that is not the declared one; nothing is written while it runs: run declare again after it stops to write it (with --replace if a different record is still there then).\n", name)
		return
	}
	a.sayf("%s is running with no record; nothing is written while it runs: run declare again after it stops to write it.\n", name)
}

// sameRecord is whether two records hold the same fields, so a re-run writes nothing.
func sameRecord(x, y registry.Record) bool {
	return x.Name == y.Name && x.Dir == y.Dir && x.Launcher == y.Launcher && x.Prefix == y.Prefix &&
		x.Restore == y.Restore && x.Compose == y.Compose && x.FreshIfNoConversation == y.FreshIfNoConversation &&
		slices.Equal(x.Args, y.Args)
}

// declaredRecord is the record of the request: the directory checked, the arguments a launch
// records (registry.ReplayArgs), and the compose answer --compose start or skip gives, as the
// launcher records it. A launch the restore never starts (--no-restore, opencode) is refused.
func declaredRecord(req declareRequest) (registry.Record, error) {
	if info, err := os.Stat(req.dir); err != nil || !info.IsDir() {
		return registry.Record{}, fmt.Errorf("%s is not a directory, so no session can start in it", req.dir)
	}
	own := req.launch
	if end := slices.Index(own, "--"); end >= 0 {
		own = own[:end]
	}
	if slices.Contains(own, "--no-restore") {
		return registry.Record{}, errors.New("the ccy arguments say --no-restore, so no boot would start the session")
	}
	if registry.Opencode(prefixCcy, req.launch) {
		return registry.Record{}, errors.New("an opencode session (--opencode, -oc) is not restored")
	}
	compose, err := composeOutcome(own)
	if err != nil {
		return registry.Record{}, err
	}
	return registry.Record{
		Name: req.name, Dir: req.dir, Launcher: launcher, Prefix: prefixCcy, Restore: true,
		Compose: compose, FreshIfNoConversation: req.fresh, Args: registry.ReplayArgs(req.launch),
	}, nil
}

// composeOutcome is the compose= a launch records for its own arguments (before `--`): started
// for --compose start, declined for skip, none for ask or no flag (the last one given counts).
// A flag that takes a value consumes the next word, as the launcher's parser does.
func composeOutcome(own []string) (string, error) {
	outcome := ""
	for i := 0; i < len(own); i++ {
		class := registry.FlagClass(own[i])
		if class != registry.ClassDropValue && class != registry.ClassKeepValue {
			continue
		}
		if i+1 >= len(own) {
			return "", fmt.Errorf("the ccy argument %s needs a value", own[i])
		}
		i++
		if own[i-1] != "--compose" {
			continue
		}
		switch own[i] {
		case "start":
			outcome = "started"
		case "skip":
			outcome = "declined"
		case "ask":
			outcome = ""
		default:
			return "", fmt.Errorf("--compose takes start, skip or ask, not '%s'", own[i])
		}
	}
	return outcome, nil
}
