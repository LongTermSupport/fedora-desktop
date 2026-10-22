package ccylaunch

import (
	"bytes"
	"context"
	_ "embed" // the texts the prelude writes
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// F12, the in-repo prelude: what the bash launcher does in the checkout before it answers any
// flag past the standalone ones (check_git_repo, check_ccy_gitignore_safety,
// ccy_env_local_dist_sync, check_allowed_hostname, then the token directories), modelled for a
// checkout that needs no question and no refusal. Everything is read and judged first; only
// then is anything printed or written, so a hand-over leaves the tree as it found it.

const (
	ccyDir        = ".claude/ccy"
	gitignoreRel  = ccyDir + "/.gitignore"
	distRel       = ccyDir + "/ccy.env.local.dist"
	localRel      = ccyDir + "/ccy.env.local"
	allowedRel    = ccyDir + "/allowed-hostnames"
	distVersion   = 3
	openDirMode   = 0o777 // mkdir -p: the umask decides
	openFileMode  = 0o666 // a shell redirection: the umask decides
	privateDir    = 0o700
	colourYellow  = "\x1b[0;33m"
	colourRed     = "\x1b[0;31m"
	colourReset   = "\x1b[0m"
	inRepoWarning = "⚠  "
)

var (
	//go:embed ccy-gitignore.txt
	gitignoreText string
	//go:embed ccy-env-local-dist.txt
	distText string
)

var (
	distVersionLine = regexp.MustCompile(`^# ccy\.env\.local\.dist version ([0-9]+)$`)
	basedOnLine     = regexp.MustCompile(`^# based on ccy\.env\.local\.dist version ([0-9]+)$`)
)

// inRepoHandOver are the variables that make the bash's prelude do more than the front models:
// a restart's mark (taken, said and kept to), and a session limit to check.
var inRepoHandOver = []string{"CCY_RESTART_RELAUNCH", "CCY_MAX_AGE", "CCY_RELAUNCH_DEADLINE_EPOCH"}

// inRepoPresent are the entries whose presence alone makes the bash do more: a migration that
// asks, a hostname check that can refuse (and --prevent's "already disabled"), and a custom
// Dockerfile's tracked-or-ignored warning.
var inRepoPresent = []string{ccyDir + "/sessions", allowedRel, ccyDir + "/Dockerfile"}

// safeTracked are the files check_ccy_gitignore_safety lets be tracked under .claude/ccy.
var safeTracked = []string{".gitignore", "Dockerfile", "allowed-hostnames", "ccy.env", "ccy.env.local.dist", "mounts", "CLAUDE.md", "README.md"}

// inRepo is what the prelude read of the checkout: each text nil when the file is absent.
type inRepo struct {
	f                      Front
	gitignore, dist, local *string
	distAt                 int64 // the dist's version line, -1 when it has none
	basedOn                int64 // the local file's based-on version, -1 when it has none
}

// at is a path in the directory the run starts in.
func (f Front) at(rel string) string {
	return filepath.Join(f.Dir, filepath.FromSlash(rel))
}

// readInRepo reads and judges the checkout: the prelude, or why the bash should run it.
func (f Front) readInRepo(ctx context.Context) (inRepo, string) {
	repo := inRepo{f: f, basedOn: -1}
	if why := f.inRepoShape(); why != "" {
		return repo, why
	}
	var why string
	for _, text := range []struct {
		rel string
		to  **string
	}{{gitignoreRel, &repo.gitignore}, {distRel, &repo.dist}, {localRel, &repo.local}} {
		if *text.to, why = readText(f.at(text.rel)); why != "" {
			return repo, why
		}
	}
	if repo.distAt, why = versionOf(repo.dist, distVersionLine, true); why != "" {
		return repo, why
	}
	if repo.basedOn, why = versionOf(repo.local, basedOnLine, false); why != "" {
		return repo, why
	}
	if why := f.trackedSafely(ctx); why != "" {
		return repo, why
	}
	for _, dir := range f.tokenDirs() {
		if why := notADir(dir); why != "" {
			return repo, why
		}
	}
	return repo, ""
}

// inRepoShape is why the environment, HOME or the checkout's directories would take the bash's
// prelude past what the front models, or "".
func (f Front) inRepoShape() string {
	for _, name := range inRepoHandOver {
		if f.getenv(name) != "" {
			return name + " is set"
		}
	}
	if home := f.getenv("HOME"); !filepath.IsAbs(home) || filepath.Clean(home) != home {
		return "HOME is not a clean absolute path"
	}
	if !isDir(f.at(".git")) {
		return "not at the root of a repository"
	}
	for _, rel := range []string{".claude", ccyDir} {
		if why := notADir(f.at(rel)); why != "" {
			return why
		}
	}
	for _, rel := range inRepoPresent {
		if _, err := os.Lstat(f.at(rel)); !errors.Is(err, fs.ErrNotExist) {
			return rel + " is there"
		}
	}
	return ""
}

// trackedSafely is why git's tracked files under .claude/ccy could make the bash refuse, or "".
func (f Front) trackedSafely(ctx context.Context) string {
	tracked, err := gitx.New(f.Runner).TrackedUnder(ctx, f.Dir, ccyDir)
	if err != nil {
		return "git cannot list the tracked files: " + err.Error()
	}
	for _, file := range tracked {
		if gitQuotes(file) {
			return "a tracked path under " + ccyDir + " that git quotes, which the bash reads with the quotes on"
		}
		base := path.Base(file)
		if !slices.Contains(safeTracked, base) && !strings.HasPrefix(base, "claude-supervise") {
			return "a file that must not be tracked is tracked under " + ccyDir
		}
	}
	return ""
}

// gitQuotes reports whether git's default core.quotePath would quote name: a byte outside
// printable ASCII, a double quote or a backslash.
func gitQuotes(name string) bool {
	for i := range len(name) {
		if c := name[i]; c < ' ' || c > '~' || c == '"' || c == '\\' {
			return true
		}
	}
	return false
}

// tokenDirs are the directories the bash makes in HOME: the store, the parents first.
func (f Front) tokenDirs() []string {
	root := f.getenv("HOME") + "/.claude-tokens/ccy"
	return []string{f.getenv("HOME") + "/.claude-tokens", root, f.tokenDir(), root + "/projects"}
}

// notADir is why path, being there and not a directory, would stop mkdir -p, or "".
func notADir(path string) string {
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return ""
	case err != nil:
		return path + " cannot be read: " + err.Error()
	case !info.IsDir():
		return path + " is not a directory"
	}
	return ""
}

// readText is a file's text, nil when there is none, or why the bash could read it otherwise:
// not a regular file, unreadable, or holding a NUL.
func readText(path string) (*string, string) {
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		return nil, ""
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, path + " is not a regular file"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, path + " cannot be read"
	}
	if bytes.IndexByte(raw, 0) >= 0 {
		return nil, path + " holds a NUL"
	}
	text := string(raw)
	return &text, ""
}

// versionOf is the version line's number in text (its first line only, or its first match),
// -1 when there is none, or why the bash's arithmetic could read it otherwise.
func versionOf(text *string, line *regexp.Regexp, firstOnly bool) (int64, string) {
	if text == nil {
		return -1, ""
	}
	lines := strings.Split(*text, "\n")
	if firstOnly {
		lines = lines[:1]
	}
	for _, l := range lines {
		if m := line.FindStringSubmatch(l); m != nil {
			if len(m[1]) > 1 && m[1][0] == '0' {
				return -1, "a version number with a leading nought, which the bash prints as written: " + m[1]
			}
			v, err := strconv.ParseInt(m[1], 10, 64)
			if err != nil {
				return -1, "a version number too long for the bash: " + m[1]
			}
			return v, ""
		}
	}
	return -1, ""
}

// apply prints and writes what the bash's prelude would, in its order: an error is a write
// that could not be made (D11).
func (r inRepo) apply() error {
	var p term.Printer
	f := r.f
	if err := os.MkdirAll(f.at(ccyDir), openDirMode); err != nil {
		return err
	}
	if r.gitignore == nil || !hasLine(strings.TrimRight(*r.gitignore, "\n"), "*") {
		if r.gitignore == nil {
			p.Print(f.Stdout, "✓ Creating .claude/ccy/.gitignore for security\n")
		} else {
			p.Print(f.Stdout, f.yellow(f.Stdout, inRepoWarning+"Updating .claude/ccy/.gitignore (was missing protection rules)")+"\n")
		}
		if err := os.WriteFile(f.at(gitignoreRel), []byte(gitignoreText), openFileMode); err != nil {
			return err
		}
	}
	for _, exception := range []string{"mounts", "ccy.env.local.dist"} {
		if err := f.gitignoreException(&p, exception); err != nil {
			return err
		}
	}
	if err := r.syncDist(&p); err != nil {
		return err
	}
	for _, dir := range f.tokenDirs()[1:] {
		if err := os.MkdirAll(dir, openDirMode); err != nil {
			return err
		}
		if err := os.Chmod(dir, privateDir); err != nil {
			return err
		}
	}
	return p.Err()
}

// gitignoreException appends !exception to the .gitignore when no line is exactly that.
func (f Front) gitignoreException(p *term.Printer, exception string) error {
	raw, err := os.ReadFile(f.at(gitignoreRel))
	if err != nil {
		return err
	}
	if hasLine(string(raw), "!"+exception) {
		return nil
	}
	p.Print(f.Stdout, "✓ Adding the "+exception+" exception to .claude/ccy/.gitignore\n")
	return appendLine(f.at(gitignoreRel), "!"+exception)
}

// syncDist is ccy_env_local_dist_sync: the dist written when absent or not this version's
// text, left as it is when a newer ccy wrote it, and the local file's based-on line checked.
func (r inRepo) syncDist(p *term.Printer) error {
	f := r.f
	current := int64(distVersion)
	write := false
	switch {
	case r.dist == nil:
		p.Print(f.Stderr, "✓ Writing .claude/ccy/ccy.env.local.dist (version 3); commit it\n")
		write = true
	case r.distAt > distVersion:
		p.Print(f.Stderr, f.yellow(f.Stderr, inRepoWarning+".claude/ccy/ccy.env.local.dist (version "+strconv.FormatInt(r.distAt, 10)+
			") was written by a newer ccy than this one (version 3); left as it is. Update ccy here.")+"\n")
		current = r.distAt
	case strings.TrimRight(*r.dist, "\n") != strings.TrimSuffix(distText, "\n"):
		p.Print(f.Stderr, f.yellow(f.Stderr, inRepoWarning+"Updating .claude/ccy/ccy.env.local.dist to version 3; commit it")+"\n")
		write = true
	}
	if write {
		if err := os.WriteFile(f.at(distRel), []byte(distText), openFileMode); err != nil {
			return err
		}
	}
	switch {
	case r.local == nil:
	case r.basedOn < 0:
		p.Print(f.Stderr, f.yellow(f.Stderr, inRepoWarning+".claude/ccy/ccy.env.local has no '# based on ccy.env.local.dist version N' line.")+"\n"+
			"   Compare it with ccy.env.local.dist and add the line where it is placed (its IaC).\n")
	case r.basedOn < current:
		p.Print(f.Stderr, f.yellow(f.Stderr, inRepoWarning+".claude/ccy/ccy.env.local is based on ccy.env.local.dist version "+
			strconv.FormatInt(r.basedOn, 10)+"; the dist is now version "+strconv.FormatInt(current, 10)+".")+"\n"+
			"   Review: diff .claude/ccy/ccy.env.local.dist .claude/ccy/ccy.env.local\n"+
			"   Update it where it is placed (its IaC), not in the checkout.\n")
	}
	return nil
}

// hasLine reports whether a line of text, as grep reads lines, is exactly want.
func hasLine(text, want string) bool {
	return slices.Contains(strings.Split(text, "\n"), want)
}

// appendLine is echo LINE >> path.
func appendLine(path, line string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, openFileMode)
	if err != nil {
		return err
	}
	_, err = io.WriteString(file, line+"\n")
	return errors.Join(err, file.Close())
}

// colourOnFor is D2 and D10: colour only when w is a terminal, and never with NO_COLOR.
func (f Front) colourOnFor(w io.Writer) bool {
	isTerminal := f.IsTerminal
	if isTerminal == nil {
		isTerminal = term.FileIsTerminal
	}
	return isTerminal(w) && f.getenv("NO_COLOR") == ""
}

// yellow is text in the bash's warning colour, on a terminal (D10).
func (f Front) yellow(w io.Writer, text string) string {
	if f.colourOnFor(w) {
		return colourYellow + text + colourReset
	}
	return text
}

// printError is the bash's print_error on stderr, its red on a terminal (D10).
func (f Front) printError(p *term.Printer, msg string) {
	prefix := "ERROR:"
	if f.colourOnFor(f.Stderr) {
		prefix = colourRed + prefix + colourReset
	}
	p.Print(f.Stderr, prefix+" "+msg+"\n")
}

// inRepoFlag is a flag answered after the prelude: act runs once the prelude is written.
func (f Front) inRepoFlag(ctx context.Context, check func() string, act func(p *term.Printer) (term.ExitCode, error)) (term.ExitCode, string) {
	repo, why := f.readInRepo(ctx)
	if why == "" {
		why = check()
	}
	if why != "" {
		return term.ExitFailure, why
	}
	if err := repo.apply(); err != nil {
		return f.fail("ccy: " + err.Error()), ""
	}
	var p term.Printer
	code, err := act(&p)
	if err = errors.Join(err, p.Err()); err != nil {
		return f.fail("ccy: " + err.Error()), ""
	}
	return code, ""
}

// prevent is --prevent: never, appended to allowed-hostnames (absent, or the bash answers).
func (f Front) prevent(ctx context.Context) (term.ExitCode, string) {
	return f.inRepoFlag(ctx, func() string { return "" }, func(p *term.Printer) (term.ExitCode, error) {
		if err := appendLine(f.at(allowedRel), "never"); err != nil {
			return term.ExitFailure, err
		}
		p.Print(f.Stdout, "✓ CCY disabled for this project.\n  Written 'never' to "+allowedRel+"\n\n"+
			"To re-enable CCY, remove or edit that file:\n  rm "+allowedRel+"\n")
		return term.ExitOK, nil
	})
}
