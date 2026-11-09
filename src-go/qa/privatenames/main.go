package privatenames

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/emit"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// ExitCode is the status qa-private-names exits with.
type ExitCode int

// The statuses callers read.
const (
	ExitOK        ExitCode = 0
	ExitFindings  ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// ListName is the term list's file name inside the git directory. The git directory is never
// tracked, so no `git add` (forced or not) can commit the list; it is shared by every work tree
// of the checkout; and a clean clone has none, so CI neither has nor needs one.
const ListName = "private-terms"

// AllowlistPath is the public-by-design allowlist the localhost.yml denylist already subtracts.
const AllowlistPath = ".claude/public-token-allowlist.yml"

// alwaysPublic is this repository's own name, the one self-reference CLAUDE.md permits.
var alwaysPublic = []string{"fedora-desktop"}

// zeroID is the all-zero object id a pushed ref has when the remote does not hold it.
const zeroID = "0000000000000000000000000000000000000000"

// gitTimeout bounds the whole run.
const gitTimeout = 10 * time.Minute

// Env is everything the command touches outside the repository tree.
type Env struct {
	Stdout, Stderr io.Writer
	// Stdin carries the pre-push hook's ref lines.
	Stdin io.Reader
	// Cwd is the directory git runs in.
	Cwd string
	// Run runs git and the agent bus client.
	Run proc.Runner
	// Container is whether this is a ccy container, where a list that cannot be built is fatal.
	Container bool
	// Hostnames are this machine's names (its own and the host's).
	Hostnames []string
	// Context prints each finding's line too, with every term masked: for triage.
	Context bool
	// ReadFile and WriteFile reach the list and the allowlist (fs.ErrNotExist when absent).
	ReadFile  func(path string) ([]byte, error)
	WriteFile func(path string, data []byte) error
}

const usageText = `usage: qa-private-names COMMAND

Install-specific names (agent teams, seats, containers, hosts, client projects) must not reach
this public repository. The terms are learned from the agent bus and the machine's names, kept in
<git dir>/private-terms, and never printed: findings name the place and the source only.

  refresh            learn the terms and write the list; print the count
  staged             the added lines of the staged change (pre-commit)
  push REMOTE URL    the commits about to be pushed, ref lines on stdin (pre-push)
  range ARG...       the commits 'git log' selects with ARG..., messages and added lines
  tree REV           every file of a commit
  -h, --help         this text

In a ccy container a list that cannot be built is exit 2, never a skip. A session on no team
(pingbus status exits 78) has no team names to learn and judges against the machine's names; the
list is unbuilt only when no name at all was learned. Elsewhere, with no agent bus and no list,
the scan says so and passes. Exit codes: 0 clean, 1 findings, 2 cannot judge.
`

// Main runs the command and returns the exit status.
func Main(args []string, env Env) ExitCode {
	out, errOut := emit.New(env.Stdout), emit.New(env.Stderr)
	return emit.Finish(out, errOut, run(args, env, out, errOut), ExitCannotRun)
}

func fail(errOut *emit.Emitter, err error) ExitCode {
	errOut.Line("ERROR: " + err.Error())
	return ExitCannotRun
}

func run(args []string, env Env, out, errOut *emit.Emitter) ExitCode {
	if len(args) == 0 {
		errOut.Block(usageText)
		return ExitCannotRun
	}
	if args[0] == "-h" || args[0] == "--help" {
		out.Block(usageText)
		return ExitOK
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	a := app{env: env, run: func(c proc.Cmd) (proc.Result, error) { return env.Run.Run(ctx, c) }}
	m, code, ok := a.matcher(errOut)
	if !ok {
		return code
	}
	res, err := a.scan(args[0], args[1:], m)
	if err != nil {
		return fail(errOut, err)
	}
	return report(res, args[0], m, out, errOut, env.Context)
}

type app struct {
	env Env
	run func(proc.Cmd) (proc.Result, error)
}

// result is what one command looked at and found.
type result struct {
	hits    []Hit
	scanned string
}

// git runs git in the working directory and fails on a non-zero status.
func (a app) git(stdin io.Reader, argv ...string) (string, error) {
	res, err := a.run(proc.Cmd{Argv: append([]string{"git"}, argv...), Dir: a.env.Cwd, Stdin: stdin})
	if err != nil {
		return "", fmt.Errorf("git %s: %w", argv[0], err)
	}
	if res.Code != 0 {
		return "", fmt.Errorf("git %s exited %d: %s", strings.Join(argv, " "), res.Code, strings.TrimSpace(res.Stderr))
	}
	return res.Stdout, nil
}

// matcher builds the matcher: the fresh terms from the bus and host names, joined to the list
// the last run left, less the public tokens. ok is false when the run ends here, with code.
func (a app) matcher(errOut *emit.Emitter) (*Matcher, ExitCode, bool) {
	root, err := a.git(nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fail(errOut, err), false
	}
	common, err := a.git(nil, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, fail(errOut, err), false
	}
	listPath := filepath.Join(strings.TrimSpace(common), ListName)
	public, err := a.public(strings.TrimSpace(root))
	if err != nil {
		return nil, fail(errOut, err), false
	}
	status, found, err := a.busStatus()
	if err != nil {
		return nil, fail(errOut, err), false
	}
	fresh := Derive(Inputs{Status: status, Hostnames: a.env.Hostnames, Public: public})
	if a.env.Container && len(fresh) == 0 {
		return nil, fail(errOut, fmt.Errorf("in a ccy container the private-name guard learned no names (agent bus found: %t, host names: %d); a gate that cannot build its list has not passed, it has not run", found, len(a.env.Hostnames))), false
	}
	stored, err := a.stored(listPath)
	if err != nil {
		return nil, fail(errOut, err), false
	}
	terms := withoutPublic(Union(fresh, stored), public)
	if len(terms) == 0 {
		errOut.Line("private-names: SKIPPED, not in a ccy container and no agent bus or term list here (" + ListName + " absent)")
		return nil, ExitOK, false
	}
	if werr := a.env.WriteFile(listPath, []byte(Format(terms))); werr != nil {
		return nil, fail(errOut, fmt.Errorf("the term list cannot be written: %w", werr)), false
	}
	m, err := NewMatcher(terms)
	if err != nil {
		return nil, fail(errOut, err), false
	}
	return m, ExitOK, true
}

// busNoTeamExit is the status `pingbus status` exits with in a session that is on no team: an
// ordinary state, since a session need not be launched on one, and it has no team or member name
// to report.
const busNoTeamExit = 78

// busStatus is the agent bus's status report. A client that cannot be started is "not found"; one
// that answers "no team" has nothing to report, which is an empty status; one that runs and fails
// otherwise is an error, since a half-known list would read as a pass. A session with no team
// still learns the machine's host names, and matcher refuses a container that learns no names.
func (a app) busStatus() (string, bool, error) {
	res, err := a.run(proc.Cmd{Argv: []string{"pingbus", "status"}, Dir: a.env.Cwd})
	if err == nil {
		switch res.Code {
		case 0:
			return res.Stdout, true, nil
		case busNoTeamExit:
			return "", true, nil
		default:
			return "", true, fmt.Errorf("pingbus status exited %d", res.Code)
		}
	}
	return "", false, nil
}

// stored is the list the last run left; an absent one is empty, an unreadable or damaged one an error.
func (a app) stored(path string) ([]Term, error) {
	data, err := a.env.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("the private-term list is unreadable: %w", err)
	}
	return Parse(string(data))
}

// public is the allowlist's public_tokens plus the repository's own name.
func (a app) public(root string) ([]string, error) {
	out := append([]string(nil), alwaysPublic...)
	data, err := a.env.ReadFile(filepath.Join(root, AllowlistPath))
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("the public-token allowlist is unreadable: %w", err)
	}
	var doc map[string][]string
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("the public-token allowlist is not YAML: %w", err)
	}
	return append(out, doc["public_tokens"]...), nil
}

func withoutPublic(terms []Term, public []string) []Term {
	drop := map[string]bool{}
	for _, p := range public {
		drop[strings.ToLower(strings.TrimSpace(p))] = true
	}
	var out []Term
	for _, t := range terms {
		if !isPublic(t.Text, drop) {
			out = append(out, t)
		}
	}
	return out
}

// scan runs one command.
func (a app) scan(cmd string, rest []string, m *Matcher) (result, error) {
	switch cmd {
	case "refresh":
		return result{}, nil
	case "staged":
		diff, err := a.git(nil, "diff", "--cached", "-U0", "--no-color", "--no-ext-diff")
		return result{hits: ScanDiff(m, "", diff), scanned: "the staged change"}, err
	case "range":
		hits, commits, err := a.scanRange(rest, m)
		return result{hits: hits, scanned: strconv.Itoa(commits) + " commit(s)"}, err
	case "push":
		return a.scanPush(m)
	case "tree":
		return a.scanTree(rest, m)
	}
	return result{}, fmt.Errorf("unknown command %q (try --help)", cmd)
}

func (a app) scanRange(sel []string, m *Matcher) ([]Hit, int, error) {
	if len(sel) == 0 {
		return nil, 0, errors.New("range needs a revision argument")
	}
	argv := append([]string{"log", "-p", "-U0", "--no-color", "--no-ext-diff", "--format=" + LogFormat}, sel...)
	log, err := a.git(nil, argv...)
	if err != nil {
		return nil, 0, err
	}
	return ScanLog(m, log), strings.Count(log, "\x01"), nil
}

// scanPush reads the pre-push ref lines "LOCALREF LOCALID REMOTEREF REMOTEID" from stdin.
func (a app) scanPush(m *Matcher) (result, error) {
	if a.env.Stdin == nil {
		return result{}, errors.New("push needs the pre-push ref lines on stdin")
	}
	data, err := io.ReadAll(a.env.Stdin)
	if err != nil {
		return result{}, fmt.Errorf("reading the pre-push ref lines: %w", err)
	}
	var res result
	commits := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if len(f) != 4 {
			return result{}, errors.New("a pre-push ref line does not have four fields")
		}
		localID, remoteID := f[1], f[3]
		if localID == zeroID {
			continue
		}
		sel := []string{localID, "--not", "--remotes"}
		if remoteID != zeroID {
			sel = []string{remoteID + ".." + localID}
		}
		hits, n, err := a.scanRange(sel, m)
		if err != nil {
			return result{}, err
		}
		res.hits = append(res.hits, hits...)
		commits += n
	}
	res.scanned = strconv.Itoa(commits) + " commit(s)"
	return res, nil
}

// scanTree reads every file of a commit through one `git cat-file --batch`.
func (a app) scanTree(rest []string, m *Matcher) (result, error) {
	if len(rest) != 1 {
		return result{}, errors.New("tree needs exactly one revision")
	}
	rev := rest[0]
	list, err := a.git(nil, "ls-tree", "-r", "-z", "--format=%(objectmode) %(path)", rev)
	if err != nil {
		return result{}, err
	}
	var paths []string
	var req strings.Builder
	for rec := range strings.SplitSeq(list, "\x00") {
		mode, path, ok := strings.Cut(rec, " ")
		if ok && mode != "160000" {
			paths = append(paths, path)
			req.WriteString(rev + ":" + path + "\n")
		}
	}
	blobs, err := a.git(strings.NewReader(req.String()), "cat-file", "--batch")
	if err != nil {
		return result{}, err
	}
	var res result
	rd := bytes.NewReader([]byte(blobs))
	for _, p := range paths {
		var oid, kind string
		var size int
		if _, err := fmt.Fscanf(rd, "%s %s %d\n", &oid, &kind, &size); err != nil {
			return result{}, fmt.Errorf("cat-file output for a tracked file is unreadable: %w", err)
		}
		body := make([]byte, size+1)
		if _, err := io.ReadFull(rd, body); err != nil {
			return result{}, fmt.Errorf("cat-file output ends early: %w", err)
		}
		res.hits = append(res.hits, ScanBlob(m, p, body[:size])...)
	}
	res.scanned = strconv.Itoa(len(paths)) + " file(s)"
	return res, nil
}

type termCount struct{ term, hits, places int }

// perTerm counts the hits of each term by its position in the list: enough to see which term is
// loud (and so likely a public word) without naming it.
func perTerm(hits []Hit) []termCount {
	byTerm := map[int]*termCount{}
	seen := map[string]bool{}
	for _, h := range hits {
		c := byTerm[h.Term]
		if c == nil {
			c = &termCount{term: h.Term}
			byTerm[h.Term] = c
		}
		c.hits++
		if key := strconv.Itoa(h.Term) + "\x00" + h.Where; !seen[key] {
			seen[key] = true
			c.places++
		}
	}
	out := make([]termCount, 0, len(byTerm))
	for _, c := range byTerm {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].term < out[j].term })
	return out
}

func report(res result, cmd string, m *Matcher, out, errOut *emit.Emitter, context bool) ExitCode {
	hits := res.hits
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Where != hits[j].Where {
			return hits[i].Where < hits[j].Where
		}
		return hits[i].Line < hits[j].Line
	})
	for _, h := range hits {
		place := m.Mask(h.Where)
		if h.Line > 0 {
			place += ":" + strconv.Itoa(h.Line)
		}
		out.Line("PRIVATE-NAME " + place + " (" + h.Source + " #" + strconv.Itoa(h.Term+1) + ")")
		if context && h.Text != "" {
			masked := m.Mask(strings.TrimSpace(h.Text))
			out.Line("    > " + masked[:min(len(masked), 160)])
		}
	}
	if cmd == "refresh" {
		out.Line("private-names: " + strconv.Itoa(m.Count()) + " terms learned")
		return ExitOK
	}
	for _, c := range perTerm(hits) {
		out.Line("  term #" + strconv.Itoa(c.term+1) + " (list line, " + m.Source(c.term) + "): " + strconv.Itoa(c.hits) + " hit(s) in " + strconv.Itoa(c.places) + " place(s)")
	}
	out.Line("private-names: " + res.scanned + " scanned against " + strconv.Itoa(m.Count()) + " terms, " + strconv.Itoa(len(hits)) + " finding(s)")
	if len(hits) == 0 {
		return ExitOK
	}
	errOut.Block(`This is a PUBLIC repository (CLAUDE.md "Public Repository"): no team, seat, container, host,
client or project name of an install belongs in it. Replace each with a placeholder (container-A,
project B, <repo>). The names are not printed; the list is <git dir>/private-terms. A name that
is public by design goes in .claude/public-token-allowlist.yml, by the owner's decision.`)
	return ExitFindings
}
