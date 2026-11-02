package archpin

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/semgrepreport"
)

// requireSemgrep is that semgrep runs at all; the reason it does not is part of the error.
func (g *gate) requireSemgrep(ctx context.Context) error {
	const hint = "semgrep not found: run ./playbooks/imports/play-python.yml (it installs the pinned semgrep; do not install it by hand)"
	res, err := g.runTool(ctx, versionBound, "semgrep", "--version")
	switch {
	case err != nil:
		return fmt.Errorf("%s (%w)", hint, err)
	case res.Code != 0:
		return fmt.Errorf("%s (semgrep --version exited %d: %s)", hint, res.Code, strings.TrimSpace(res.Stderr))
	}
	return nil
}

// readFile reads a regular file under the root.
func (g *gate) readFile(rel string) (string, error) {
	info, err := os.Stat(g.path(rel))
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", rel, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", rel)
	}
	data, err := os.ReadFile(g.path(rel))
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", rel, err)
	}
	return string(data), nil
}

// readRule reads the rule's excludes after checking the fixture is there to prove it.
func (g *gate) readRule() (semgrepreport.Excludes, error) {
	if _, err := g.readFile(fixtureFile); err != nil {
		return nil, err
	}
	rule, err := g.readFile(ruleFile)
	if err != nil {
		return nil, err
	}
	return semgrepreport.ParseExcludes(ruleFile, rule)
}

// readAllowlist reads the ratchet, which must be in the current format.
func (g *gate) readAllowlist() (allowlist, error) {
	text, err := g.readFile(allowlistFile)
	if err != nil {
		return allowlist{}, err
	}
	list, err := parseAllowlist(text)
	if err == nil && list.format != allowlistFormat {
		err = fmt.Errorf("%s is format %d: write it as `%s%d` with one row per finding (path TAB finding TAB note)", allowlistFile, list.format, formatPrefix, allowlistFormat)
	}
	return list, err
}

// proveRule runs the rule against its annotated fixture. A rule edited into matching nothing is
// worse than a missing tool, so a regression here fails the run and never reports a clean tree.
func (g *gate) proveRule(ctx context.Context) (ExitCode, error) {
	fixture, err := g.readFile(fixtureFile)
	if err != nil {
		return ExitCannotRun, err
	}
	if !semgrepreport.Annotated(fixture)[ruleName] {
		return ExitCannotRun, fmt.Errorf("%s marks no ruleid line for %s; the self-test proves nothing", fixtureFile, ruleName)
	}
	res, err := g.runTool(ctx, semgrepBound, "semgrep", "--test", "--config", g.path(ruleFile), g.path(fixtureFile), "--metrics=off", semgrepreport.TimeoutFlag)
	if err != nil {
		return ExitCannotRun, err
	}
	if res.Code != 0 {
		return ExitFailed, fmt.Errorf("%s failed its fixture (%s):\n%s%s", ruleName, fixtureFile, res.Stdout, res.Stderr)
	}
	return ExitOK, nil
}

// handOver lists what git would publish and keeps the regular files the rule does not exclude:
// a symlink is left out (semgrep refuses it, and its target is scanned in its own right) and so
// is a file git lists that is gone from the disk.
func (g *gate) handOver(ctx context.Context, excl semgrepreport.Excludes) ([]string, error) {
	res, err := g.runTool(ctx, gitBound, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err == nil && res.Code != 0 {
		err = fmt.Errorf("git exited %d: %s", res.Code, strings.TrimSpace(res.Stderr))
	}
	if err != nil {
		return nil, fmt.Errorf("cannot list the files git would publish: %w", err)
	}
	var files []string
	for rel := range strings.SplitSeq(res.Stdout, "\x00") {
		if rel == "" || excl.Matches(rel) {
			continue
		}
		info, err := os.Lstat(g.path(rel))
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			return nil, fmt.Errorf("cannot look at %s: %w", rel, err)
		case info.Mode().IsRegular():
			files = append(files, rel)
		}
	}
	if len(files) == 0 {
		return nil, errors.New("git named no file to scan; nothing was checked")
	}
	slices.Sort(files)
	return slices.Compact(files), nil
}

// scan hands the files to semgrep by name, never as a directory (a directory scan applies
// semgrep's default ignore list), refuses a report over files it did not read, and returns the
// findings of each file: each distinct line, with its text read from the file.
func (g *gate) scan(ctx context.Context, files []string) (map[string][]finding, error) {
	argv := append([]string{"semgrep", "--config", g.path(ruleFile), "--json", "--quiet", "--metrics=off", semgrepreport.TimeoutFlag, "--max-target-bytes=0"}, files...)
	res, err := g.runTool(ctx, semgrepBound, argv...)
	if err != nil {
		return nil, err
	}
	if res.Code >= 2 {
		return nil, fmt.Errorf("semgrep failed (exit %d)\n%s", res.Code, res.Stderr)
	}
	rep, err := semgrepreport.Decode(res.Stdout)
	if err != nil {
		return nil, err
	}
	if err := rep.Judge("published", files); err != nil {
		return nil, err
	}
	lines := map[string][]int{}
	for _, r := range rep.Results {
		p := semgrepreport.Tidy(r.Path)
		if !slices.Contains(lines[p], r.Start.Line) {
			lines[p] = append(lines[p], r.Start.Line)
		}
	}
	found := map[string][]finding{}
	for p, ls := range lines {
		slices.Sort(ls)
		text, err := g.readFile(p)
		if err != nil {
			return nil, err
		}
		all := strings.Split(text, "\n")
		for _, l := range ls {
			if l < 1 || l > len(all) {
				return nil, fmt.Errorf("semgrep reported %s:%d, and the file has %d line(s)", p, l, len(all))
			}
			found[p] = append(found[p], finding{text: identity(all[l-1]), line: l})
		}
	}
	return found, nil
}

// baseVariable names the commit the allowlist is compared with when the caller sets it: the
// variable the bash-limits and shellcheck-directive ratchets read, which CI sets (the head
// before a push to the default branch).
const baseVariable = "QA_BASH_LIMITS_BASE"

// git runs git from the root and returns its stdout; a non-zero status is an error.
func (g *gate) git(ctx context.Context, args ...string) (string, error) {
	res, err := g.runTool(ctx, gitBound, append([]string{"git"}, args...)...)
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		return "", fmt.Errorf("git %s exited %d: %s", strings.Join(args, " "), res.Code, strings.TrimSpace(res.Stderr))
	}
	return res.Stdout, nil
}

// baseRef is the commit the allowlist is compared with: QA_BASH_LIMITS_BASE when set, else the
// merge-base of HEAD and origin's default branch, so commits not pushed yet are compared too.
func (g *gate) baseRef(ctx context.Context) (string, error) {
	if g.env.Getenv != nil {
		if set := strings.TrimSpace(g.env.Getenv(baseVariable)); set != "" {
			return set, nil
		}
	}
	head, err := g.git(ctx, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD")
	if err != nil {
		return "", fmt.Errorf("no base to compare %s with: origin's default branch is not known here (run: git remote set-head origin --auto, or set %s): %w", allowlistFile, baseVariable, err)
	}
	base, err := g.git(ctx, "merge-base", "HEAD", strings.TrimSpace(head))
	if err != nil {
		return "", fmt.Errorf("no base to compare %s with: %w", allowlistFile, err)
	}
	return strings.TrimSpace(base), nil
}

// baseAllowlist is the allowlist the current one may only shrink from, and the commit it was
// read at: the copy at the base, or, for a base from before the gate landed (it has none), the
// first copy committed on HEAD's history, the baseline accepted with the rule. A base that does
// not resolve, or a list never committed, is an error: nothing could tell a new row from an old.
func (g *gate) baseAllowlist(ctx context.Context) (allowlist, string, error) {
	ref, err := g.baseRef(ctx)
	if err != nil {
		return allowlist{}, "", err
	}
	if _, verifyErr := g.git(ctx, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); verifyErr != nil {
		return allowlist{}, "", fmt.Errorf("the base %q does not resolve to a commit: %w", ref, verifyErr)
	}
	listed, err := g.git(ctx, "ls-tree", "--name-only", ref, "--", allowlistFile)
	if err != nil {
		return allowlist{}, "", err
	}
	if strings.TrimSpace(listed) == "" {
		added, logErr := g.git(ctx, "log", "--diff-filter=A", "--format=%H", "HEAD", "--", allowlistFile)
		if logErr != nil {
			return allowlist{}, "", logErr
		}
		commits := strings.Fields(added)
		if len(commits) == 0 {
			return allowlist{}, "", fmt.Errorf("the base %s has no %s and HEAD's history never committed one: commit the list before the gate can hold it", ref, allowlistFile)
		}
		ref = commits[len(commits)-1]
	}
	text, err := g.git(ctx, "show", ref+":"+allowlistFile)
	if err != nil {
		return allowlist{}, "", err
	}
	list, err := parseAllowlist(text)
	if err != nil {
		return allowlist{}, "", fmt.Errorf("%s at %s: %w", allowlistFile, ref, err)
	}
	return list, ref, nil
}
