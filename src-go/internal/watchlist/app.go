package watchlist

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const usage = `usage: watchlist [--root DIR] <command>

  sync [repo...]                 read the stars (with no repo named), add new ones, mark unstarred
                                 ones, clone what is missing, fetch and fast-forward the clones,
                                 and report what changed
  add <url>                      add https://github.com/<owner>/<repo> by hand, and clone it
  set <repo> [--licence ID] [--state new|study|excluded|unstarred] [--group NAME]
                                 change a row; the clone moves with its licence and state
  run new <run-id> --mode baseline|diff [--group G]... [--repo R]...
                                 start a study run (<mode>-<yyyymmdd>-<hhmm>) of the study rows
  run rows <run-id> [group]      print a run's rows joined with the list, for the study brief
  run mark <run-id> <group>      mark a group done once its report records every study
  run status [run-id]            print a run's progress (the newest run by default)

--root DIR is the fedora-desktop checkout (default: the working directory).
`

var urlPattern = regexp.MustCompile(`^https://github\.com/([A-Za-z0-9-]+/[A-Za-z0-9._-]+)$`)

// App is the watchlist command over a checkout, a process runner and two output streams.
type App struct {
	root   string
	runner execx.Runner
	git    *gitx.Git
	out    io.Writer
	err    io.Writer
	p      term.Printer
}

// NewApp is the command rooted at root (overridden by --root), running git and gh through r.
func NewApp(root string, r execx.Runner, out, err io.Writer) *App {
	return &App{root: root, runner: r, git: gitx.New(r), out: out, err: err}
}

// Run executes one command line and returns the exit code.
func (a *App) Run(ctx context.Context, args []string) term.ExitCode {
	code := a.dispatch(ctx, args)
	if a.p.Err() != nil && code == term.ExitOK {
		return term.ExitFailure
	}
	return code
}

// usageMistake is a command line that does not parse; it is answered with the usage text.
type usageMistake string

func (u usageMistake) Error() string { return string(u) }

// flags consumes the leading flags. done is true when the run is already decided (help, or a
// usage mistake), with the exit code to return.
func (a *App) flags(args []string) ([]string, term.ExitCode, bool) {
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "-h", "--help":
			a.p.Print(a.out, usage)
			return nil, term.ExitOK, true
		case "--root":
			if len(args) < 2 {
				return nil, a.usageError("--root needs a directory"), true
			}
			a.root, args = args[1], args[2:]
		default:
			return nil, a.usageError("unknown flag " + args[0]), true
		}
	}
	return args, term.ExitOK, false
}

func (a *App) dispatch(ctx context.Context, args []string) term.ExitCode {
	args, code, done := a.flags(args)
	if done {
		return code
	}
	if len(args) == 0 {
		return a.usageError("no command given")
	}
	cmd, rest := args[0], args[1:]
	var err error
	switch cmd {
	case "sync":
		err = a.sync(ctx, rest)
	case "add":
		if len(rest) != 1 {
			return a.usageError("add takes exactly one URL")
		}
		err = a.add(ctx, rest[0])
	case "set":
		err = a.set(rest)
	case "run":
		err = a.runCommand(ctx, rest)
	default:
		return a.usageError("unknown command " + cmd)
	}
	if mistake, ok := errors.AsType[usageMistake](err); ok {
		return a.usageError(string(mistake))
	}
	return a.finish(err)
}

func (a *App) usageError(msg string) term.ExitCode {
	a.p.Print(a.err, "ERROR: "+msg+"\n"+usage)
	return term.ExitUsage
}

func (a *App) finish(err error) term.ExitCode {
	if err == nil {
		return term.ExitOK
	}
	a.p.Print(a.err, "ERROR: "+err.Error()+"\n")
	return term.ExitFailure
}

func (a *App) event(format string, args ...any) { a.p.Print(a.out, fmt.Sprintf(format, args...)+"\n") }

func (a *App) progress(format string, args ...any) {
	a.p.Print(a.err, fmt.Sprintf(format, args...)+"\n")
}

func (a *App) load() ([]Entry, error) {
	path := filepath.Clean(ListPath(a.root))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read the watch list: %w", err)
	}
	return Parse(data, path)
}

func (a *App) save(entries []Entry) error {
	return fsx.WriteAtomic(ListPath(a.root), "watch list", Format(entries), 0o644)
}

// find is the index of the entry named by owner/repo or its URL, in any case.
func find(entries []Entry, name string) (int, error) {
	want := strings.TrimPrefix(name, "https://github.com/")
	for i, e := range entries {
		if strings.EqualFold(e.Repo, want) {
			return i, nil
		}
	}
	return -1, fmt.Errorf("%s is not on the watch list", name)
}

// ghRepo is the part of GitHub's repository object the list uses.
type ghRepo struct {
	FullName string `json:"full_name"`
	License  *struct {
		SPDXID string `json:"spdx_id"`
	} `json:"license"`
}

// star is the repository and GitHub's licence id for it (NONE when GitHub has none).
func (g ghRepo) star() (Entry, error) {
	licence := licenceNone
	if g.License != nil && g.License.SPDXID != "" {
		licence = g.License.SPDXID
	}
	e := Entry{Repo: g.FullName, Licence: licence}
	return e, errors.Join(checkRepo(g.FullName), checkLicence(licence))
}

func (a *App) gh(ctx context.Context, args ...string) ([]byte, error) {
	res, err := a.runner.Run(ctx, execx.Cmd{Name: "gh", Args: args})
	if err != nil {
		return nil, fmt.Errorf("gh %s: %w", strings.Join(args, " "), err)
	}
	return res.Stdout, nil
}

// stars is S1: the owner's stars, every page; gh's paginated reply is one JSON array per page.
func (a *App) stars(ctx context.Context) ([]Entry, error) {
	out, err := a.gh(ctx, "api", "user/starred", "--paginate")
	if err != nil {
		return nil, fmt.Errorf("reading the stars: %w", err)
	}
	var stars []Entry
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var page []ghRepo
		if err := dec.Decode(&page); errors.Is(err, io.EOF) {
			return stars, nil
		} else if err != nil {
			return nil, fmt.Errorf("reading the stars: gh api user/starred gave no list of repositories: %w", err)
		}
		for _, g := range page {
			s, starErr := g.star()
			if starErr != nil {
				return nil, fmt.Errorf("reading the stars: %w", starErr)
			}
			stars = append(stars, s)
		}
	}
}

// applyStars is S2 and S3 over the list: it returns the list with the new stars added, the
// repositories added, and whether anything changed.
func (a *App) applyStars(entries []Entry, stars []Entry) ([]Entry, map[string]bool, bool) {
	starred := map[string]Entry{}
	for _, s := range stars {
		starred[strings.ToLower(s.Repo)] = s
	}
	changed := false
	for i := range entries {
		e := &entries[i]
		key := strings.ToLower(e.Repo)
		_, isStarred := starred[key]
		delete(starred, key)
		switch {
		case isStarred && e.State == stateUnstarred:
			e.State = stateNew
			a.event("restarred %s", e.Repo)
			changed = true
		case !isStarred && e.Source == sourceStar && e.State != stateUnstarred:
			e.State = stateUnstarred
			a.event("unstarred %s", e.Repo)
			changed = true
		}
	}
	added := map[string]bool{}
	for _, s := range stars {
		if _, left := starred[strings.ToLower(s.Repo)]; !left || added[s.Repo] {
			continue
		}
		entries = append(entries, Entry{
			Repo: s.Repo, Source: sourceStar, Licence: s.Licence, Reuse: Reuse(s.Licence), State: stateNew,
			Group: none, LicenceFile: none, LicenceSHA: none,
		})
		added[s.Repo] = true
		changed = true
	}
	news := make([]string, 0, len(added))
	for repo := range added {
		news = append(news, repo)
	}
	slices.Sort(news)
	for _, repo := range news {
		a.event("new %s", repo)
	}
	return entries, added, changed
}

// sync is S1 to S9; the list is written once, at the end, and only when something changed.
func (a *App) sync(ctx context.Context, named []string) error {
	entries, err := a.load()
	if err != nil {
		return err
	}
	runs, err := loadRuns(a.root)
	if err != nil {
		return err
	}
	before := map[string]string{}
	for _, e := range entries {
		before[e.Repo] = e.CloneDir(a.root)
	}
	added, changed := map[string]bool{}, false
	if len(named) == 0 {
		stars, starsErr := a.stars(ctx)
		if starsErr != nil {
			return starsErr
		}
		entries, added, changed = a.applyStars(entries, stars)
	}
	targets, err := syncTargets(entries, named)
	if err != nil {
		return err
	}
	for _, repo := range targets {
		i, findErr := find(entries, repo)
		if findErr != nil {
			return findErr
		}
		rowChanged, syncErr := a.syncOne(ctx, &entries[i], before[repo], added[repo], runs)
		if syncErr != nil {
			return fmt.Errorf("%s: %w", repo, syncErr)
		}
		changed = changed || rowChanged
	}
	if !changed {
		return nil
	}
	return a.save(entries)
}

// syncTargets is the repositories a sync works on, sorted: the named ones, or every one.
func syncTargets(entries []Entry, named []string) ([]string, error) {
	var targets []string
	for _, e := range entries {
		if len(named) == 0 {
			targets = append(targets, e.Repo)
		}
	}
	for _, name := range named {
		i, err := find(entries, name)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(targets, entries[i].Repo) {
			targets = append(targets, entries[i].Repo)
		}
	}
	slices.Sort(targets)
	return targets, nil
}

// syncOne brings one clone up to date and reports what changed in it (S3 to S7, S9).
func (a *App) syncOne(ctx context.Context, e *Entry, before string, fresh bool, runs []run) (bool, error) {
	dir := e.CloneDir(a.root)
	if before != "" {
		if err := relocate(before, dir); err != nil {
			return false, fmt.Errorf("move clone: %w", err)
		}
	}
	if err := a.ensureClone(ctx, e.Repo, dir); err != nil {
		return false, err
	}
	changed := false
	file, sha, err := LicenceOf(dir)
	if err != nil {
		return false, fmt.Errorf("licence: %w", err)
	}
	if file != e.LicenceFile || sha != e.LicenceSHA {
		if !fresh {
			a.event("licence-changed %s %s", e.Repo, file)
		}
		e.LicenceFile, e.LicenceSHA, changed = file, sha, true
	}
	if e.State != stateStudy {
		return changed, nil
	}
	head, err := a.git.Head(ctx, dir)
	if err != nil {
		return false, fmt.Errorf("head: %w", err)
	}
	switch last := lastStudied(runs, e.Repo, ""); {
	case last == "":
		a.event("unstudied %s", e.Repo)
	case last != head:
		a.event("moved %s %s..%s", e.Repo, last, head)
	}
	return changed, nil
}

// ensureClone is S4, S5 and S9: a missing clone is cloned in full; a present one is completed if
// shallow, fetched and fast-forwarded, and one that cannot be is an error naming the remedy.
func (a *App) ensureClone(ctx context.Context, repo, dir string) error {
	present, err := cloneExists(dir)
	if err != nil {
		return err
	}
	if !present {
		if err := os.MkdirAll(filepath.Dir(dir), 0o750); err != nil {
			return fmt.Errorf("clone: cannot create its directory: %w", err)
		}
		a.progress("cloning %s", repo)
		if err := a.git.Clone(ctx, "https://github.com/"+repo, dir); err != nil {
			return fmt.Errorf("clone: %w", err)
		}
		a.event("cloned %s", repo)
		return nil
	}
	remedy := fmt.Sprintf("re-clone it: delete %s and run watchlist sync %s", dir, repo)
	a.progress("fetching %s", repo)
	dirty, err := a.git.Dirty(ctx, dir)
	if err != nil {
		return fmt.Errorf("status: %w", err)
	}
	if dirty {
		return fmt.Errorf("the clone at %s has local changes; %s", dir, remedy)
	}
	shallow, err := a.git.IsShallow(ctx, dir)
	if err != nil {
		return fmt.Errorf("shallow: %w", err)
	}
	if shallow {
		if err := a.git.Unshallow(ctx, dir); err != nil {
			return fmt.Errorf("unshallow: %w", err)
		}
		a.event("unshallowed %s", repo)
	}
	if err := a.git.FetchAll(ctx, dir); err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	if err := a.git.FastForwardToOriginHead(ctx, dir); err != nil {
		return fmt.Errorf("fast-forward: cannot fast-forward to origin/HEAD (rewritten history or a local commit); %s: %w", remedy, err)
	}
	return nil
}

// cloneExists is whether dir holds a clone. A directory without .git (a killed clone) is an error.
func cloneExists(dir string) (bool, error) {
	_, err := os.Stat(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("cannot inspect the clone %s: %w", dir, err)
	}
	if _, gitErr := os.Stat(filepath.Join(dir, ".git")); gitErr != nil {
		return false, fmt.Errorf("%s exists but is not a git clone (an interrupted clone?); delete it and sync again", dir)
	}
	return true, nil
}

func exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("cannot inspect %s: %w", path, err)
	}
	return true, nil
}

// relocate moves a clone from old to dest (T2). A clone already at dest with none at old is
// where it belongs; one at both is refused.
func relocate(old, dest string) error {
	if old == dest {
		return nil
	}
	oldThere, err := exists(old)
	if err != nil {
		return err
	}
	destThere, err := exists(dest)
	if err != nil {
		return err
	}
	if !oldThere {
		return nil
	}
	if destThere {
		return fmt.Errorf("cannot move the clone from %s: %s already exists", old, dest)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return fmt.Errorf("cannot create %s: %w", filepath.Dir(dest), err)
	}
	if err := os.Rename(old, dest); err != nil {
		return fmt.Errorf("cannot move the clone: %w", err)
	}
	return nil
}

// add is A1 and A2.
func (a *App) add(ctx context.Context, url string) error {
	m := urlPattern.FindStringSubmatch(url)
	if m == nil || strings.HasSuffix(m[1], ".git") {
		return fmt.Errorf("%q is not an https://github.com/<owner>/<repo> URL (no .git suffix, no trailing slash)", url)
	}
	repo := m[1]
	entries, err := a.load()
	if err != nil {
		return err
	}
	if i, dupErr := find(entries, repo); dupErr == nil {
		return fmt.Errorf("%s is already on the watch list (%s)", repo, entries[i].Repo)
	}
	out, err := a.gh(ctx, "api", "repos/"+repo)
	if err != nil {
		return fmt.Errorf("%s: licence: %w", repo, err)
	}
	var g ghRepo
	if err := json.Unmarshal(out, &g); err != nil {
		return fmt.Errorf("%s: licence: gh gave no repository: %w", repo, err)
	}
	s, err := g.star()
	if err != nil {
		return fmt.Errorf("%s: licence: %w", repo, err)
	}
	if !strings.EqualFold(s.Repo, repo) {
		return fmt.Errorf("%s: GitHub names it %s (renamed or moved); add that URL instead", repo, s.Repo)
	}
	e := Entry{Repo: s.Repo, Source: sourceAdded, Licence: s.Licence, Reuse: Reuse(s.Licence), State: stateNew, Group: none}
	dir := e.CloneDir(a.root)
	if err := a.ensureClone(ctx, e.Repo, dir); err != nil {
		return fmt.Errorf("%s: %w", e.Repo, err)
	}
	if e.LicenceFile, e.LicenceSHA, err = LicenceOf(dir); err != nil {
		return fmt.Errorf("%s: licence: %w", e.Repo, err)
	}
	if err := a.save(append(entries, e)); err != nil {
		return err
	}
	a.event("added %s", e.Repo)
	return nil
}

// setFlags is set's command line: the repository and each field given at most once.
func setFlags(args []string) (string, map[string]string, error) {
	if len(args) == 0 {
		return "", nil, usageMistake("set takes <repo> and at least one of --licence, --state, --group")
	}
	repo, given := args[0], map[string]string{}
	for rest := args[1:]; len(rest) > 0; rest = rest[2:] {
		name := rest[0]
		if name != "--licence" && name != "--state" && name != "--group" {
			return "", nil, usageMistake("set: unknown argument " + name)
		}
		if len(rest) < 2 {
			return "", nil, usageMistake("set: " + name + " needs a value")
		}
		if _, twice := given[name]; twice {
			return "", nil, usageMistake("set: " + name + " is given twice")
		}
		given[name] = rest[1]
	}
	if len(given) == 0 {
		return "", nil, usageMistake("set takes at least one of --licence, --state, --group")
	}
	return repo, given, nil
}

// set is T1 to T3.
func (a *App) set(args []string) error {
	repo, given, err := setFlags(args)
	if err != nil {
		return err
	}
	entries, err := a.load()
	if err != nil {
		return err
	}
	i, err := find(entries, repo)
	if err != nil {
		return err
	}
	e := entries[i]
	before := e.CloneDir(a.root)
	if licence, ok := given["--licence"]; ok {
		e.Licence, e.Reuse = licence, Reuse(licence)
	}
	if state, ok := given["--state"]; ok {
		e.State = state
	}
	group, groupGiven := given["--group"]
	if groupGiven {
		e.Group = group
	}
	if e.State == stateExcluded {
		if groupGiven && group != none {
			return fmt.Errorf("%s: an excluded row has no group; drop --group", e.Repo)
		}
		e.Group = none
	}
	if e.State == stateStudy && e.Group == none {
		return fmt.Errorf("%s: --state study needs a group: give --group", e.Repo)
	}
	if err := e.check(); err != nil {
		return fmt.Errorf("%s: %w", e.Repo, err)
	}
	dir := e.CloneDir(a.root)
	if err := relocate(before, dir); err != nil {
		return fmt.Errorf("%s: %w", e.Repo, err)
	}
	if e.LicenceFile, e.LicenceSHA, err = LicenceOf(dir); err != nil {
		return fmt.Errorf("%s: no clone to read the licence from (run watchlist sync %s): %w", e.Repo, e.Repo, err)
	}
	entries[i] = e
	if err := a.save(entries); err != nil {
		return err
	}
	a.event("set %s licence=%s reuse=%s state=%s group=%s", e.Repo, e.Licence, e.Reuse, e.State, e.Group)
	return nil
}
