package untrackedaudit

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// gitAsker runs git questions in the repository and answers them as text.
type gitAsker struct {
	run  proc.Runner
	root string
}

// ask runs git with args in dir ("" is the root); a command that could not start or exited
// non-zero is an error carrying git's own words.
func (g gitAsker) ask(ctx context.Context, dir string, args ...string) (string, error) {
	if dir == "" {
		dir = g.root
	}
	res, err := g.run.Run(ctx, proc.Cmd{Argv: append([]string{"git"}, args...), Dir: dir})
	if err != nil {
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	if res.Code != 0 || res.TimedOut {
		return "", fmt.Errorf("git %s failed (exit %d): %s", strings.Join(args, " "), res.Code, strings.TrimSpace(res.Stderr))
	}
	return res.Stdout, nil
}

// count asks a git command that prints one integer.
func (g gitAsker) count(ctx context.Context, dir string, args ...string) (int, error) {
	out, err := g.ask(ctx, dir, args...)
	if err != nil {
		return 0, err
	}
	n, convErr := strconv.Atoi(strings.TrimSpace(out))
	if convErr != nil || n < 0 {
		return 0, fmt.Errorf("git %s printed %q, not a count", strings.Join(args, " "), strings.TrimSpace(out))
	}
	return n, nil
}

// unixTime reads a Unix timestamp git printed.
func unixTime(s string) (time.Time, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("git printed %q, not a timestamp", strings.TrimSpace(s))
	}
	return time.Unix(n, 0).UTC(), nil
}

const notOnAnyRemote = "--not"

// auditBranches reports each local branch with commits that no remote-tracking branch holds.
func (g gitAsker) auditBranches(ctx context.Context, pol Policy, now time.Time) ([]Finding, error) {
	out, err := g.ask(ctx, "", "for-each-ref", "--format=%(refname:short)%09%(committerdate:unix)", "refs/heads")
	if err != nil {
		return nil, err
	}
	var findings []Finding
	for line := range strings.SplitSeq(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		name, stamp, found := strings.Cut(line, "\t")
		if !found {
			return nil, fmt.Errorf("git for-each-ref printed an unreadable line %q", line)
		}
		if slices.Contains(pol.ActiveBranches, name) {
			continue
		}
		n, err := g.count(ctx, "", "rev-list", "--count", name, notOnAnyRemote, "--remotes")
		if err != nil {
			return nil, err
		}
		if n == 0 {
			continue
		}
		tip, err := unixTime(stamp)
		if err != nil {
			return nil, err
		}
		findings = append(findings, Finding{
			Kind: KindBranch, Subject: name, Age: now.Sub(tip),
			Detail: fmt.Sprintf("%d commit(s) on no remote", n),
			Home:   "push it: git push -u origin " + name + ", then merge or close it",
		})
	}
	return findings, nil
}

type worktree struct {
	dir      string
	detached bool
}

// listWorktrees returns the linked work trees (the main one, listed first, is the checkout
// itself and is judged by the branch check).
func (g gitAsker) listWorktrees(ctx context.Context) ([]worktree, error) {
	out, err := g.ask(ctx, "", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var all []worktree
	for line := range strings.SplitSeq(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			all = append(all, worktree{dir: strings.TrimPrefix(line, "worktree ")})
		case line == "detached" && len(all) > 0:
			all[len(all)-1].detached = true
		}
	}
	if len(all) == 0 {
		return nil, errors.New("git worktree list printed no work tree")
	}
	return all[1:], nil
}

// auditWorktrees reports linked work trees older than the floor that hold uncommitted changes,
// or (detached ones, which no branch check covers) commits no remote holds.
func (g gitAsker) auditWorktrees(ctx context.Context, pol Policy, now time.Time, mtime func(string) (time.Time, error)) ([]Finding, error) {
	trees, err := g.listWorktrees(ctx)
	if err != nil {
		return nil, err
	}
	var findings []Finding
	for _, wt := range trees {
		found, err := g.judgeWorktree(ctx, wt, pol, now, mtime)
		if err != nil {
			return nil, err
		}
		findings = append(findings, found...)
	}
	return findings, nil
}

func (g gitAsker) judgeWorktree(ctx context.Context, wt worktree, pol Policy, now time.Time, mtime func(string) (time.Time, error)) ([]Finding, error) {
	stamp, err := g.ask(ctx, wt.dir, "log", "-1", "--format=%ct")
	if err != nil {
		return nil, err
	}
	headTime, err := unixTime(stamp)
	if err != nil {
		return nil, err
	}
	dirty, err := g.dirtyFinding(ctx, wt, pol, now, headTime, mtime)
	if err != nil {
		return nil, err
	}
	commits, err := g.detachedFinding(ctx, wt, pol, now, headTime)
	if err != nil {
		return nil, err
	}
	return slices.Concat(dirty, commits), nil
}

func (g gitAsker) dirtyFinding(ctx context.Context, wt worktree, pol Policy, now, head time.Time, mtime func(string) (time.Time, error)) ([]Finding, error) {
	status, err := g.ask(ctx, wt.dir, "status", "--porcelain")
	if err != nil {
		return nil, err
	}
	changed := changedPaths(status)
	if len(changed) == 0 {
		return nil, nil
	}
	newest, err := newestChange(wt.dir, changed, head, mtime)
	if err != nil {
		return nil, err
	}
	age := now.Sub(newest)
	if age <= pol.floor() {
		return nil, nil
	}
	return []Finding{{
		Kind: KindDirty, Subject: wt.dir, Age: age,
		Detail: fmt.Sprintf("%d changed path(s) uncommitted", len(changed)),
		Home:   "commit it and push the branch",
	}}, nil
}

// detachedFinding covers a detached HEAD, which no branch check sees.
func (g gitAsker) detachedFinding(ctx context.Context, wt worktree, pol Policy, now, head time.Time) ([]Finding, error) {
	if !wt.detached {
		return nil, nil
	}
	n, err := g.count(ctx, wt.dir, "rev-list", "--count", "HEAD", notOnAnyRemote, "--remotes")
	if err != nil {
		return nil, err
	}
	age := now.Sub(head)
	if n == 0 || age <= pol.floor() {
		return nil, nil
	}
	return []Finding{{
		Kind: KindWorktreeCommits, Subject: wt.dir, Age: age,
		Detail: fmt.Sprintf("%d commit(s) on no remote, no branch", n),
		Home:   "name a branch at HEAD and push it",
	}}, nil
}

// changedPaths reads the paths out of `git status --porcelain`.
func changedPaths(status string) []string {
	var paths []string
	for line := range strings.SplitSeq(status, "\n") {
		if len(line) < 4 {
			continue
		}
		p := line[3:]
		if _, after, renamed := strings.Cut(p, " -> "); renamed {
			p = after
		}
		paths = append(paths, strings.Trim(p, `"`))
	}
	return paths
}

// newestChange is the latest modification among the changed paths that still exist, or the
// HEAD commit's time when none can be stat-ed (every change is a deletion).
func newestChange(dir string, changed []string, head time.Time, mtime func(string) (time.Time, error)) (time.Time, error) {
	newest := time.Time{}
	for _, p := range changed {
		m, err := mtime(filepath.Join(dir, p))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return time.Time{}, fmt.Errorf("cannot stat %s: %w", filepath.Join(dir, p), err)
		}
		newest = latest(newest, m)
	}
	if newest.IsZero() {
		return head, nil
	}
	return newest, nil
}

func latest(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// auditStashes reports every stash: a stash is never a home for work.
func (g gitAsker) auditStashes(ctx context.Context, now time.Time) ([]Finding, error) {
	out, err := g.ask(ctx, "", "stash", "list", "--format=%gd%x09%ct%x09%gs")
	if err != nil {
		return nil, err
	}
	var findings []Finding
	for line := range strings.SplitSeq(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("git stash list printed an unreadable line %q", line)
		}
		made, err := unixTime(parts[1])
		if err != nil {
			return nil, err
		}
		findings = append(findings, Finding{
			Kind: KindStash, Subject: parts[0], Age: now.Sub(made), Detail: parts[2],
			Home: "commit it onto a branch and push it, then drop the stash",
		})
	}
	return findings, nil
}
