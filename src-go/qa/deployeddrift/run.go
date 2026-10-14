// Package deployeddrift is the deployed-drift QA gate: repo-owned files that a play deploys are
// compared byte for byte with their deployed copies on the host, so a repo change that was never
// deployed fails. The gate has a source-tree half (every template maps to a playbook dest:, the
// pair table is sound) that runs everywhere, and a host half that is skipped, with a printed
// reason, only in the ccy container, a linked worktree, or where nothing is deployed.
// Specification: CLAUDE/Plan/00192-.../SPEC-qa-deployed-drift.md.
package deployeddrift

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// ExitCode is the status qa-deployed-drift exits with.
type ExitCode int

// The statuses callers read: 0 in sync or skipped with a reason, 1 drift, 2 the run could not complete.
const (
	ExitOK        ExitCode = 0
	ExitFailed    ExitCode = 1
	ExitCannotRun ExitCode = 2
)

const (
	binDir        = "files/home/.local/bin"
	containerRoot = "/workspace"
	stageName     = "deployed-drift"
)

// Env is everything the command touches outside the arguments: the streams, the working
// directory, HOME, the repository and host file systems, and the one git question.
type Env struct {
	Stdout, Stderr io.Writer
	// Cwd is the default root when neither --root nor --scripts is given.
	Cwd string
	// Home is the user's home directory ("" is unset).
	Home string
	// Open returns the file system rooted at the repository directory.
	Open func(root string) fs.FS
	// Host is the host's file system rooted at "/".
	Host fs.FS
	// LinkedWorktree says whether root is a linked git worktree rather than the main checkout.
	LinkedWorktree func(root string) (bool, error)
}

const usageText = `usage: qa-deployed-drift [--root DIR] [--scripts DIR]

Compares repo-owned files that a play deploys (files/home/.local/bin and the vm test lab and
agent bus trees) with their deployed copies, byte for byte. A file with no deployed copy is
counted, not compared. Skipped, with the reason printed, in the ccy container, in a linked git
worktree, and where $HOME/.local/bin does not exist.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  -h, --help      this text

Exit codes: 0 in sync or skipped, 1 drift, 2 bad usage or the run could not complete.
`

// fatal is a failure that stops the run with exit 2; text is printed as it stands.
type fatal struct{ text string }

func (f *fatal) Error() string { return f.text }

func fatalf(format string, args ...any) error { return &fatal{text: fmt.Sprintf(format, args...)} }

// Main runs the command and returns the exit status.
func Main(args []string, env Env) ExitCode {
	opts, err := parseArgs(args, env.Cwd)
	if err != nil {
		return emit(env, outcome{errOut: "ERROR: " + err.Error() + "\n" + usageText, code: ExitCannotRun})
	}
	if opts.help {
		return emit(env, outcome{out: usageText})
	}
	res, err := run(opts.root, env)
	if err != nil {
		return emit(env, outcome{errOut: failureText(err) + "\n", code: ExitCannotRun})
	}
	return emit(env, res)
}

// emit writes the streams; a stream that cannot be written is a run that did not complete.
func emit(env Env, res outcome) ExitCode {
	_, outErr := io.WriteString(env.Stdout, res.out)
	_, errErr := io.WriteString(env.Stderr, res.errOut)
	if outErr != nil || errErr != nil {
		return ExitCannotRun
	}
	return res.code
}

// failureText is an error as the line printed for it; the gate's own messages say what they are.
func failureText(err error) string {
	var f *fatal
	if errors.As(err, &f) && strings.HasPrefix(f.text, "✗") {
		return f.text
	}
	return "ERROR: " + err.Error()
}

// parseArgs reads the command line. Anything unknown is a usage error.
func parseArgs(args []string, cwd string) (options, error) {
	opts := options{root: cwd}
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; arg {
		case "-h", "--help":
			opts.help = true
		case "--root", "--scripts":
			if i+1 >= len(args) || args[i+1] == "" || args[i+1][0] == '-' {
				return options{}, fmt.Errorf("%s needs a directory", arg)
			}
			i++
			opts.root = args[i]
			if arg == "--scripts" {
				opts.root = filepath.Dir(opts.root)
			}
		default:
			return options{}, fmt.Errorf("unknown argument '%s'", arg)
		}
	}
	return opts, nil
}

// options is the parsed command line.
type options struct {
	root string
	help bool
}

// pair is one entry of the extra table (E10): files matching pattern in a repo directory, where
// they are deployed (homeMarker stands for HOME) and the play that deploys them.
type pair struct {
	repoDir string
	pattern string
	dir     string
	play    string
	exclude string // a file name the play deliberately never deploys
}

const (
	vmLab    = "playbooks/imports/optional/common/play-vm-test-lab.yml"
	agentBus = "playbooks/imports/play-agent-bus.yml"
	kit      = "files/opt/claude-yolo/optional/agent-bus"
	plugin   = kit + "/plugin/pingbus"
	kitDir   = "/usr/local/share/agent-bus/kit"
	allFiles = "*"

	homeMarker = "{home}"
)

var pairs = []pair{
	{"files/home/.local/share/vmtest", allFiles, homeMarker + "/.local/share/vmtest", vmLab, ""},
	{"fedora-install", "ks-vm-*.cfg", homeMarker + "/.local/share/vmtest", vmLab, ""},
	{"files/home/.config/systemd/user", "vmtest-*", homeMarker + "/.config/systemd/user", vmLab, ""},
	{"helpers/vmtest", "*.py", "/usr/local/lib/ccy-helpers/helpers/vmtest", vmLab, "request.py"},
	{"files/usr/local/sbin", "agent-bus-install", "/usr/local/sbin", agentBus, ""},
	{"files/usr/local/bin", "agent-bus", "/usr/local/bin", agentBus, ""},
	{"files/usr/local/share/agent-bus", allFiles, "/usr/local/share/agent-bus", agentBus, ""},
	{"files/etc/systemd/system", "agent-bus-*", "/etc/systemd/system", agentBus, ""},
	{kit, allFiles, kitDir, agentBus, ""},
	{plugin + "/.claude-plugin", allFiles, kitDir + "/plugin/pingbus/.claude-plugin", agentBus, ""},
	{plugin + "/hooks", allFiles, kitDir + "/plugin/pingbus/hooks", agentBus, ""},
	{plugin + "/skills/pingbus", allFiles, kitDir + "/plugin/pingbus/skills/pingbus", agentBus, ""},
}

// item is one repo file to compare with its deployed copy.
type item struct {
	rel      string // repo-relative path
	deployed string // absolute deployed path
	plays    func() []string
}

// repoTree is what the source-tree half learns about the repository.
type repoTree struct {
	fsys      fs.FS
	plays     map[string]string // playbook path to its text
	playPaths []string          // sorted
	templates []string          // "NAME.j2 -> NAME", sorted
	bins      []string          // non-template file names under binDir, sorted
	extras    []item
}

// outcome is a finished run: what goes to each stream and the status.
type outcome struct {
	out, errOut string
	code        ExitCode
}

func run(root string, env Env) (outcome, error) {
	repo, err := scanRepo(root, env)
	if err != nil {
		return outcome{}, err
	}
	skip := func(reason string) (outcome, error) {
		return outcome{out: fmt.Sprintf("⚠ %s: skipped (%s); %d template(s) verified to map to a playbook dest:\n",
			stageName, reason, len(repo.templates))}, nil
	}
	if root == containerRoot {
		return skip("CCY container — no deployed copies to compare")
	}
	linked, err := env.LinkedWorktree(root)
	if err != nil {
		return outcome{}, fmt.Errorf("cannot tell whether %s is a linked git worktree: %w", root, err)
	}
	if linked {
		return skip("linked git worktree — not the deployed checkout")
	}
	if env.Home == "" {
		return outcome{}, errors.New("HOME is not set")
	}
	deployedBin := env.Home + "/.local/bin"
	info, err := fs.Stat(env.Host, hostPath(deployedBin))
	switch {
	case errors.Is(err, fs.ErrNotExist) || (err == nil && !info.IsDir()):
		return skip(deployedBin + " does not exist")
	case err != nil:
		return outcome{}, fmt.Errorf("%s: %w", deployedBin, err)
	}
	return compare(root, env, repo, deployedBin)
}

// hostPath is an absolute path as a name in the host file system.
func hostPath(abs string) string { return strings.TrimPrefix(abs, "/") }

// scanRepo is the source-tree half: it runs before any host check.
func scanRepo(root string, env Env) (*repoTree, error) {
	fsys := env.Open(root)
	repo := &repoTree{fsys: fsys, plays: map[string]string{}}
	if info, err := fs.Stat(fsys, binDir); err != nil || !info.IsDir() {
		return nil, fatalf("source directory not found: %s/%s", root, binDir)
	}
	if info, err := fs.Stat(fsys, "playbooks"); err != nil || !info.IsDir() {
		return nil, fatalf("playbooks directory not found: %s/playbooks", root)
	}
	if err := fs.WalkDir(fsys, "playbooks", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".yml") {
			return nil
		}
		text, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		repo.plays[p] = string(text)
		repo.playPaths = append(repo.playPaths, p)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("reading playbooks: %w", err)
	}
	slices.Sort(repo.playPaths)
	if err := repo.scanBin(); err != nil {
		return nil, err
	}
	return repo, repo.scanPairs()
}

// regularFiles lists the names of the regular files directly under dir, symlinks followed,
// without the names that begin with a dot.
func regularFiles(fsys fs.FS, dir string) ([]string, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := fs.Stat(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", dir, e.Name(), err)
		}
		if info.Mode().IsRegular() {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	return names, nil
}

// scanBin sorts the bin directory into templates (E5, each verified to have a playbook dest:)
// and plain files.
func (r *repoTree) scanBin() error {
	names, err := regularFiles(r.fsys, binDir)
	if err != nil {
		return err
	}
	for _, name := range names {
		rendered, isTemplate := strings.CutSuffix(name, ".j2")
		if !isTemplate {
			r.bins = append(r.bins, name)
			continue
		}
		dest := regexp.MustCompile(`(?m)dest:.*/\.local/bin/` + regexp.QuoteMeta(rendered) + `(["' ]|$)`)
		found := false
		for _, text := range r.plays {
			if dest.MatchString(text) {
				found = true
				break
			}
		}
		if !found {
			return fatalf("✗ %s: %s has no playbook dest: deploying it as '%s'\n"+
				"  A template this gate cannot map to a deployed name is a file it\n"+
				"  would report a pass over without comparing anything. Fix the play,\n"+
				"  or the naming convention this check relies on.", stageName, name, rendered)
		}
		r.templates = append(r.templates, name+" -> "+rendered)
	}
	return nil
}

// scanPairs resolves the extra table against the repo (E6).
func (r *repoTree) scanPairs() error {
	for _, p := range pairs {
		glob := p.repoDir + "/" + p.pattern
		if _, err := fs.Stat(r.fsys, p.play); err != nil {
			return fatalf("pair %s: the play that deploys it is missing: %s", glob, p.play)
		}
		matches, err := fs.Glob(r.fsys, glob)
		if err != nil {
			return fmt.Errorf("pair %s: %w", glob, err)
		}
		found := 0
		for _, m := range matches {
			base := path.Base(m)
			info, err := fs.Stat(r.fsys, m)
			if err != nil {
				return fmt.Errorf("%s: %w", m, err)
			}
			if !info.Mode().IsRegular() || strings.HasPrefix(base, ".") || base == p.exclude {
				continue
			}
			found++
			r.extras = append(r.extras, item{rel: m, deployed: p.dir + "/" + base, plays: func() []string { return []string{p.play} }})
		}
		if found == 0 {
			return fatalf("pair %s matches no repo file; the gate would compare nothing for it", glob)
		}
	}
	return nil
}

// owningPlays names the play(s) that deploy a bin file (E13).
func (r *repoTree) owningPlays(name string) []string {
	declared := regexp.MustCompile(`(?m)src:.*files/home/\.local/bin/` + regexp.QuoteMeta(name) + `(["' ]|$)`)
	var hits []string
	for _, p := range r.playPaths {
		if declared.MatchString(r.plays[p]) {
			hits = append(hits, p)
		}
	}
	if len(hits) > 0 {
		return hits
	}
	word := regexp.MustCompile(`(^|[^A-Za-z0-9._-])` + regexp.QuoteMeta(name) + `($|[^A-Za-z0-9._-])`)
	for _, p := range r.playPaths {
		if strings.Contains(r.plays[p], "files/home/.local/bin/{{") && word.MatchString(r.plays[p]) {
			hits = append(hits, p)
		}
	}
	return hits
}

// compare is the host half.
func compare(root string, env Env, repo *repoTree, deployedBin string) (outcome, error) {
	var items []item
	for _, name := range repo.bins {
		items = append(items, item{
			rel:      binDir + "/" + name,
			deployed: deployedBin + "/" + name,
			plays:    func() []string { return repo.owningPlays(name) },
		})
	}
	for _, e := range repo.extras {
		e.deployed = strings.Replace(e.deployed, homeMarker, env.Home, 1)
		items = append(items, e)
	}
	var drifted []item
	checked, notInstalled := 0, 0
	for _, it := range items {
		info, err := fs.Stat(env.Host, hostPath(it.deployed))
		if errors.Is(err, fs.ErrNotExist) || (err == nil && !info.Mode().IsRegular()) {
			notInstalled++
			continue
		}
		if err != nil {
			return outcome{}, fmt.Errorf("%s: %w", it.deployed, err)
		}
		same, err := sameBytes(repo.fsys, it.rel, env.Host, hostPath(it.deployed))
		if err != nil {
			return outcome{}, err
		}
		checked++
		if !same {
			drifted = append(drifted, it)
		}
	}
	counts := fmt.Sprintf("%d not installed on this host; %d template(s) not byte-comparable", notInstalled, len(repo.templates))
	if len(drifted) > 0 {
		return outcome{errOut: driftReport(root, drifted, checked, counts), code: ExitFailed}, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "✓ %s: %d deployed script(s) match the repo; %s\n", stageName, checked, counts)
	for _, t := range repo.templates {
		fmt.Fprintf(&b, "    template (rendered, so not compared): %s\n", t)
	}
	return outcome{out: b.String()}, nil
}

// sameBytes reads both files; a file that cannot be read stops the run rather than reading as drift (E12).
func sameBytes(repoFS fs.FS, repoRel string, host fs.FS, hostRel string) (bool, error) {
	want, err := fs.ReadFile(repoFS, repoRel)
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", repoRel, err)
	}
	got, err := fs.ReadFile(host, hostRel)
	if err != nil {
		return false, fmt.Errorf("reading /%s: %w", hostRel, err)
	}
	return bytes.Equal(want, got), nil
}

// driftReport is the failure text of E14.
func driftReport(root string, drifted []item, checked int, counts string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "✗ %s: repo changes that were never deployed\n\n", stageName)
	for _, it := range drifted {
		fmt.Fprintf(&b, "  %s\n    repo:     %s/%s\n    deployed: %s\n    deploy it with:\n", it.rel, root, it.rel, it.deployed)
		plays := it.plays()
		if len(plays) == 0 {
			b.WriteString("    (no playbook references this file — it may be orphaned)\n")
		}
		for _, p := range plays {
			fmt.Fprintf(&b, "    ansible-playbook %s\n", p)
		}
		b.WriteString("\n")
	}
	b.WriteString("  The repo and the host disagree. Deploy, test, THEN commit —\n")
	b.WriteString("  see CLAUDE/InfrastructureAsCode.md (edit -> playbook -> deploy -> test).\n\n")
	fmt.Fprintf(&b, "✗ %s: %d of %d deployed script(s) differ from the repo; %s\n", stageName, len(drifted), checked, counts)
	return b.String()
}
