package branchdelete

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/branchhygiene"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/emit"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootargs"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootpath"
)

// ExitCode is the status branch-delete-list exits with.
type ExitCode int

// The statuses: 0 every candidate passes, 1 refused, 2 could not judge.
const (
	ExitOK        ExitCode = 0
	ExitRefused   ExitCode = 1
	ExitCannotRun ExitCode = 2
)

const gitTimeout = 2 * time.Minute

// Env is everything the command touches outside the arguments.
type Env struct {
	// Stdin is the candidate list when no file is named, or the file is "-".
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	// Cwd is the default root, and what a relative list file is read against.
	Cwd  string
	Eval rootpath.Eval
	// Git runs git in the repository root.
	Git      proc.Runner
	ReadFile func(path string) ([]byte, error)
}

const usageText = `usage: branch-delete-list [--root DIR] [--scripts DIR] [FILE]

Turns a candidate list of origin's branch names (one per line, from FILE, or stdin when FILE is
absent or "-") into one "git push origin --delete <name>" line per branch on stdout, for the
owner to read and run. It deletes nothing.

It REFUSES the whole list, naming each offender on stderr, when any candidate is exempt or parked
in scripts/branch-policy.toml, is a release line (F[0-9]*), is origin's default branch, is not a
branch on origin, or has no refs/archive/<name> on origin at exactly the branch's tip. It never
drops a name: take an offender out of the list yourself, or archive its tip, and run it again.
Origin is asked with "git ls-remote --symref origin".

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  -h, --help      this text

Exit codes: 0 every candidate passes, 1 refused, 2 could not judge (bad usage, no candidates,
unreadable policy or list, origin unreachable).
`

// Main runs the command and returns the exit status.
func Main(args []string, env Env) ExitCode {
	out, errOut := emit.New(env.Stdout), emit.New(env.Stderr)
	code := run(args, env, out, errOut)
	return emit.Finish(out, errOut, code, ExitCannotRun)
}

func run(args []string, env Env, out, errOut *emit.Emitter) ExitCode {
	flags, file, err := splitArgs(args)
	var opts rootargs.Args
	if err == nil {
		opts, err = rootargs.Strict(flags)
	}
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		errOut.Block(usageText)
		return ExitCannotRun
	}
	if opts.Help {
		out.Block(usageText)
		return ExitOK
	}
	names, remote, pol, err := gather(env, opts, file)
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	if refusals := Judge(pol, remote, names); len(refusals) > 0 {
		for _, r := range refusals {
			errOut.Line("REFUSED " + r.Branch + ": " + strings.Join(r.Reasons, "; "))
		}
		errOut.Line(fmt.Sprintf("✗ %d of %d candidates refused; no delete list printed. Take each out of the list, or archive its tip, and run again.", len(refusals), len(names)))
		return ExitRefused
	}
	for _, name := range names {
		out.Line("git push origin --delete " + name)
	}
	noun := "candidates pass"
	if len(names) == 1 {
		noun = "candidate passes"
	}
	errOut.Line(fmt.Sprintf("✓ %d %s: none exempt, parked, a release line or the default branch; each archived at its tip. Read every name before running.", len(names), noun))
	return ExitOK
}

// splitArgs takes the one optional FILE out of the arguments; the rest are rootargs' flags.
func splitArgs(args []string) ([]string, string, error) {
	var flags []string
	file := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--root" || arg == "--scripts":
			flags = append(flags, arg)
			if i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		case arg != "-" && strings.HasPrefix(arg, "-"):
			flags = append(flags, arg)
		case file != "":
			return nil, "", errors.New("give at most one FILE")
		default:
			file = arg
		}
	}
	return flags, file, nil
}

// gather is the candidates, origin's refs and the policy, or why they could not be read.
func gather(env Env, opts rootargs.Args, file string) ([]string, Remote, branchhygiene.Policy, error) {
	root, err := rootpath.RootVia(env.Eval, env.Cwd, opts.Root, opts.Scripts)
	if err != nil {
		return nil, Remote{}, branchhygiene.Policy{}, err
	}
	pol, err := branchhygiene.ReadPolicy(env.ReadFile, root)
	if err != nil {
		return nil, Remote{}, pol, err
	}
	names, err := readCandidates(env, file)
	if err != nil {
		return nil, Remote{}, pol, err
	}
	remote, err := askOrigin(env, root)
	return names, remote, pol, err
}

func readCandidates(env Env, file string) ([]string, error) {
	var data []byte
	var err error
	if file == "" || file == "-" {
		data, err = io.ReadAll(env.Stdin)
	} else {
		if !filepath.IsAbs(file) {
			file = filepath.Join(env.Cwd, file)
		}
		data, err = env.ReadFile(file)
	}
	if err != nil {
		return nil, fmt.Errorf("candidate list: %w", err)
	}
	names := Candidates(string(data))
	if len(names) == 0 {
		return nil, errors.New("the candidate list is empty")
	}
	return names, nil
}

func askOrigin(env Env, root string) (Remote, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	res, err := env.Git.Run(ctx, proc.Cmd{Argv: []string{"git", "ls-remote", "--symref", "origin"}, Dir: root})
	switch {
	case err != nil:
		return Remote{}, fmt.Errorf("git ls-remote: %w", err)
	case res.TimedOut:
		return Remote{}, errors.New("git ls-remote origin timed out")
	case res.Code != 0:
		return Remote{}, fmt.Errorf("git ls-remote origin exited %d: %s", res.Code, strings.TrimSpace(res.Stderr))
	}
	return ParseRemote(res.Stdout)
}

// ParseRemote reads `git ls-remote --symref origin`: the HEAD symref line names the default
// branch, refs/heads/ the branches, refs/archive/ the archives; every other ref is ignored.
func ParseRemote(text string) (Remote, error) {
	r := Remote{Heads: map[string]string{}, Archives: map[string]string{}}
	for line := range strings.SplitSeq(strings.TrimSpace(text), "\n") {
		left, ref, ok := strings.Cut(line, "\t")
		if !ok {
			return Remote{}, fmt.Errorf("unreadable git ls-remote line %q", line)
		}
		if target, isSym := strings.CutPrefix(left, "ref: "); isSym {
			if ref == "HEAD" {
				r.Default = strings.TrimPrefix(target, "refs/heads/")
			}
			continue
		}
		if name, isHead := strings.CutPrefix(ref, "refs/heads/"); isHead {
			r.Heads[name] = left
		} else if name, isArchive := strings.CutPrefix(ref, archivePrefix); isArchive {
			r.Archives[name] = left
		}
	}
	if r.Default == "" {
		return Remote{}, errors.New("git ls-remote named no default branch (no HEAD symref) for origin")
	}
	return r, nil
}
