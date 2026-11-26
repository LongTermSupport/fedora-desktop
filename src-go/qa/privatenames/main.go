package privatenames

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/emit"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/prepush"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/stdinread"
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
	// Hostnames are this machine's names (its own and the host's).
	Hostnames []string
	// ReadFile and WriteFile reach the list and the allowlist (fs.ErrNotExist when absent).
	ReadFile  func(path string) ([]byte, error)
	WriteFile func(path string, data []byte) error
	// ReplaceFile replaces a file whole and at once (a sibling renamed over it), owner-only.
	ReplaceFile func(path string, data []byte) error
	// CreateFile writes a new owner-only file and refuses an existing path with fs.ErrExist.
	CreateFile func(path string, data []byte) error
	// EvalSymlinks resolves a path's symbolic links (filepath.EvalSymlinks).
	EvalSymlinks func(path string) (string, error)
	// Clock times the wait for the ref lines on Stdin; nil is the real clock.
	Clock stdinread.Clock
	// Container is whether this is a ccy container, where a list that cannot be built is fatal.
	Container bool
	// Context prints each finding's line too, with every term masked: for triage.
	Context bool
}

const usageText = `usage: qa-private-names COMMAND

Install-specific names (agent teams, seats, containers, hosts, client projects) must not reach
this public repository. The terms are learned from both agent buses ('pingbus status' and
'teamchat status': this session's own teams and their members only) and the machine's names, kept
in <git dir>/private-terms, and never printed: findings name the place and the source only.

No bus client reports the other teams its bus host serves, so those, and any other name the
owner wants blocked, are in <git dir>/private-terms-pinned (one per line, # lines skipped). A
pinned name always blocks, whatever its length, and is never copied into the learned list. The
host's play-git-hooks-security.yml writes it with 'pin': the names the machine's inventory
knows and the owner's vault-encrypted private_name_terms. It is not edited by hand.

  refresh            learn the terms and write the list; print the count
  staged             the added lines of the staged change (pre-commit)
  push REMOTE URL    the commits about to be pushed, ref lines on stdin (pre-push)
  range ARG...       the commits 'git log' selects with ARG..., messages and added lines
  tree REV           every file of a commit
  message FILE       a commit message before it is committed (commit-msg), # lines skipped
  denylist FILE      write the terms that block a commit to FILE (new, mode 0600, outside the
                     repository) as go-qa-ci's public-safety denylist: one ERE per line for
                     grep -E -i, on this tool's word boundaries; print only the count
  pin                write <git dir>/private-terms-pinned (mode 0600, replaced whole) from the
                     inventory JSON on stdin (the play's call): team ids, display names,
                     handles and containers held to the learned-name rule, the bus host's
                     served teams, and private_name_terms as written; print only the count.
                     Refuses a list written by hand and an empty one on a machine with teams
  -h, --help         this text

go-qa-ci's GQACI_DENYLIST repository secret is set from the denylist mode's output; refresh it
when the bus's members change. Terms named in <git dir>/private-terms-export-skip (one per
line, by the owner's decision) are left out of it and still blocked here.

In a ccy container a list that cannot be built is exit 2, never a skip. A session on no team
(pingbus status exits 78, teamchat status 69) has no team names to learn and judges against the
machine's names; the list is unbuilt when no name at all was learned, or when a bus reported a
session but taught no team or member name and nothing is pinned. Elsewhere, with no agent bus and
no list, the scan says so and passes. Exit codes: 0 clean, 1 findings, 2 cannot judge.
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
	switch args[0] {
	case "denylist":
		return a.denylist(args[1:], errOut)
	case "pin":
		return a.pin(args[1:], out, errOut)
	}
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

// matcher builds the matcher over the terms that block a commit. ok is false when the run ends
// here, with code.
func (a app) matcher(errOut *emit.Emitter) (*Matcher, ExitCode, bool) {
	terms, code, ok := a.terms(errOut)
	if !ok {
		return nil, code, false
	}
	if len(terms) == 0 {
		errOut.Line("private-names: SKIPPED, not in a ccy container and no agent bus or term list here (" + ListName + " absent)")
		return nil, ExitOK, false
	}
	m, err := NewMatcher(terms)
	if err != nil {
		return nil, fail(errOut, err), false
	}
	return m, ExitOK, true
}

// terms are the terms that block a commit: the fresh terms from the bus and host names, joined to
// the list the last run left, less the public tokens. A non-empty set is written back as the list.
// ok is false when the run ends here, with code.
func (a app) terms(errOut *emit.Emitter) ([]Term, ExitCode, bool) {
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
	bus, err := a.busStatus()
	if err != nil {
		return nil, fail(errOut, err), false
	}
	fresh := Derive(Inputs{Status: bus.status, Hostnames: a.env.Hostnames, Public: public})
	if a.env.Container && len(fresh) == 0 {
		return nil, fail(errOut, fmt.Errorf("in a ccy container the private-name guard learned no names (agent bus found: %t, host names: %d); a gate that cannot build its list has not passed, it has not run", bus.found, len(a.env.Hostnames))), false
	}
	stored, err := a.stored(listPath)
	if err != nil {
		return nil, fail(errOut, err), false
	}
	pinned, err := a.pinned(filepath.Join(strings.TrimSpace(common), PinnedName))
	if err != nil {
		return nil, fail(errOut, err), false
	}
	learned := withoutPublic(Union(fresh, withoutFirstWords(stored, fresh)), public)
	if a.env.Container && !bus.found && !holdsBusName(learned) && len(pinned) == 0 {
		return nil, fail(errOut, errors.New("in a ccy container no agent bus client is installed to ask, and the private-name guard knows no team or member name, learned or pinned; it has not run. On the host, run play-git-hooks-security.yml, which writes <git dir>/"+PinnedName)), false
	}
	if a.env.Container && bus.session && !holdsBusName(learned) && len(pinned) == 0 {
		return nil, fail(errOut, errors.New("in a ccy container the agent bus reported a session but taught the private-name guard no team or member name, so the session's own team is unguarded; put the team's name in <git dir>/"+PinnedName+" (one per line)")), false
	}
	if len(learned) > 0 {
		if werr := a.env.WriteFile(listPath, []byte(Format(learned))); werr != nil {
			return nil, fail(errOut, fmt.Errorf("the term list cannot be written: %w", werr)), false
		}
	}
	return Union(learned, pinned), ExitOK, true
}

// holdsBusName is whether terms hold a name learned from an agent bus.
func holdsBusName(terms []Term) bool {
	for _, t := range terms {
		if t.Source == SourceTeam || t.Source == SourceMember {
			return true
		}
	}
	return false
}

// pinned is the owner's pinned list: one name per line, `#` lines and blank lines ignored. A
// pinned name always blocks (no length, generic-word or public-token rule drops it), and it is
// never copied into the learned list, so deleting the pin ends the block. An absent list is
// empty; an unreadable one is an error.
func (a app) pinned(path string) ([]Term, error) {
	data, err := a.env.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("the pinned list %s is unreadable: %w", PinnedName, err)
	}
	var out []Term
	for line := range strings.SplitSeq(string(data), "\n") {
		name := strings.TrimSpace(line)
		if name != "" && !strings.HasPrefix(name, "#") {
			out = append(out, Term{Source: SourcePinned, Text: name})
		}
	}
	return out, nil
}

// PinnedName is the owner's pinned list's file name inside the git directory, beside the term
// list and as untracked: the names no bus client in a container can report (another team on the
// same bus host, a client), blocked always. Unlike ExportSkipName it adds terms; it removes none.
const PinnedName = "private-terms-pinned"

// teamchatNoRuntimeExit is `teamchat status` outside a member session (exits.ExitNoRuntime).
const teamchatNoRuntimeExit = 69

// busNoTeamExit is the status `pingbus status` exits with in a session that is on no team: an
// ordinary state, since a session need not be launched on one, and it has no team or member name
// to report.
const busNoTeamExit = 78

// busReport is what the agent buses said: the status lines Derive reads, whether any bus client
// could be started, and whether one answered for a session (so had a team to report).
type busReport struct {
	status         string
	found, session bool
}

// busStatus asks both agent buses for this session's teams: v1 (`pingbus status`, TSV lines) and
// v2 (`teamchat status`, JSON Lines records, turned into the same lines). Each reports only the
// session's own teams and their members; no bus client in a container can name the other teams
// its bus host serves, which is what the owner's pinned list is for. A client that cannot be
// started is "not found"; one that answers "no team" (pingbus 78) or "not a member session"
// (teamchat 69) has nothing to report; one that runs and fails otherwise is an error, since a
// half-known list would read as a pass. A session with no team still learns the machine's host
// names, and terms refuses a container that learns no names.
func (a app) busStatus() (busReport, error) {
	var rep busReport
	clients := []struct {
		name    string
		noTeam  int
		toLines func(string) (string, error)
	}{
		{"pingbus", busNoTeamExit, func(s string) (string, error) { return s, nil }},
		{"teamchat", teamchatNoRuntimeExit, teamchatLines},
	}
	for _, c := range clients {
		res, err := a.run(proc.Cmd{Argv: []string{c.name, "status"}, Dir: a.env.Cwd})
		if errors.Is(err, exec.ErrNotFound) {
			continue
		}
		if err != nil {
			return busReport{}, fmt.Errorf("%s status could not be started: %w", c.name, err)
		}
		rep.found = true
		switch res.Code {
		case 0:
			lines, err := c.toLines(res.Stdout)
			if err != nil {
				return busReport{}, err
			}
			rep.status += lines
			rep.session = true
		case c.noTeam:
		default:
			return busReport{}, fmt.Errorf("%s status exited %d", c.name, res.Code)
		}
	}
	return rep, nil
}

// teamchatRecord is the part of a `teamchat status` record that names a team or a member.
type teamchatRecord struct {
	Kind   string `json:"kind"`
	Team   string `json:"team"`
	Name   string `json:"name"`
	Handle string `json:"handle"`
}

// teamchatLines turns teamchat's team and member records into Derive's TEAM and MEMBER lines; a
// line that is not a JSON record is an error (by its number, never its text).
func teamchatLines(out string) (string, error) {
	var b strings.Builder
	for n, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec teamchatRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return "", fmt.Errorf("teamchat status output is unreadable at line %d", n+1)
		}
		switch rec.Kind {
		case "team":
			b.WriteString("TEAM\t" + rec.Team + "\t" + rec.Handle + "\n")
			if rec.Name != rec.Team {
				b.WriteString("TEAM\t" + rec.Name + "\n")
			}
		case "member":
			b.WriteString("MEMBER\t" + rec.Team + "\t" + rec.Handle + "\n")
		}
	}
	return b.String(), nil
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

// withoutFirstWords drops a stored term that is only the first word of another stored name (the
// text before its first hyphen), unless it is freshly learned as a name of its own: a part of a
// name is never a term alone (#310), and a list kept from an older rule may still hold one.
func withoutFirstWords(stored, fresh []Term) []Term {
	learned := map[string]bool{}
	for _, t := range fresh {
		learned[strings.ToLower(t.Text)] = true
	}
	firstWords := map[string]bool{}
	for _, t := range stored {
		if first := before(t.Text, "-"); first != t.Text {
			firstWords[strings.ToLower(first)] = true
		}
	}
	var out []Term
	for _, t := range stored {
		k := strings.ToLower(t.Text)
		if !firstWords[k] || learned[k] {
			out = append(out, t)
		}
	}
	return out
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
		remote := ""
		if len(rest) > 0 {
			remote = rest[0]
		}
		return a.scanPush(m, remote)
	case "tree":
		return a.scanTree(rest, m)
	case "message":
		if len(rest) != 1 {
			return result{}, errors.New("message needs the commit message file")
		}
		msg, err := a.env.ReadFile(rest[0])
		if err != nil {
			return result{}, fmt.Errorf("reading the commit message: %w", err)
		}
		return result{hits: ScanMessage(m, "", string(msg), true), scanned: "the commit message"}, nil
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

// scanPush reads the pre-push ref lines from stdin and scans each pushed commit the remote does
// not already hold (prepush.Ref.Selection says which).
func (a app) scanPush(m *Matcher, remote string) (result, error) {
	refs, err := prepush.ReadStdin(a.env.Stdin, stdinread.Options{Prog: "qa-private-names push", Err: a.env.Stderr, GiveUpAfter: stdinread.PipedGiveUp, Clock: a.env.Clock})
	if err != nil {
		return result{}, err
	}
	var res result
	commits := 0
	for _, ref := range refs {
		if ref.Deletes() {
			continue
		}
		hits, n, err := a.scanRange(ref.Selection(remote), m)
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
