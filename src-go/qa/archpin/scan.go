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

// readAllowlist reads the ratchet.
func (g *gate) readAllowlist() (allowlist, error) {
	text, err := g.readFile(allowlistFile)
	if err != nil {
		return nil, err
	}
	return parseAllowlist(text)
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
// distinct finding lines of each file.
func (g *gate) scan(ctx context.Context, files []string) (map[string][]int, error) {
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
	found := map[string][]int{}
	for _, r := range rep.Results {
		p := semgrepreport.Tidy(r.Path)
		if !slices.Contains(found[p], r.Start.Line) {
			found[p] = append(found[p], r.Start.Line)
		}
	}
	for p := range found {
		slices.Sort(found[p])
	}
	return found, nil
}
