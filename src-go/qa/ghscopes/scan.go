package ghscopes

import (
	"cmp"
	"context"
	"encoding/json"
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
	res, err := g.runTool(ctx, versionTimeout, "semgrep", "--version")
	switch {
	case err != nil:
		return fmt.Errorf("%s (%w)", hint, err)
	case res.Code != 0:
		return fmt.Errorf("%s (semgrep --version exited %d: %s)", hint, res.Code, strings.TrimSpace(res.Stderr))
	}
	return nil
}

// proveRule runs the rule against its annotated fixture. A rule edited into matching nothing is
// worse than a missing tool, so a regression here fails the run and never reports a clean tree.
// A fixture that disagrees with the rule is exit 1, a test that could not run is exit 2.
func (g *gate) proveRule(ctx context.Context) (ExitCode, error) {
	fixture, err := os.ReadFile(g.path(fixtureFile))
	if err != nil {
		return ExitCannotRun, fmt.Errorf("cannot read %s: %w", fixtureFile, err)
	}
	if len(semgrepreport.Annotated(string(fixture))) == 0 {
		return ExitCannotRun, fmt.Errorf("%s marks no ruleid line; the self-test proves nothing", fixtureFile)
	}
	res, err := g.runTool(ctx, semgrepTimeout, "semgrep", "--test", "--config", g.path(ruleFile), g.path(fixtureFile), "--metrics=off")
	if err != nil {
		return ExitCannotRun, err
	}
	if res.Code != 0 {
		return ExitFailed, fmt.Errorf("%s failed its fixture (%s):\n%s%s", ruleName, fixtureFile, res.Stdout, res.Stderr)
	}
	return ExitOK, nil
}

// handOver lists what git would publish and keeps the regular files: a symlink is left out
// (semgrep refuses it, and its target is scanned in its own right or is vendored) and so is a
// file git lists that is gone from the disk.
func (g *gate) handOver(ctx context.Context) ([]string, error) {
	res, err := g.runTool(ctx, gitTimeout, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err == nil && res.Code != 0 {
		err = fmt.Errorf("git exited %d: %s", res.Code, strings.TrimSpace(res.Stderr))
	}
	if err != nil {
		return nil, fmt.Errorf("cannot list the files git would publish under .: %w", err)
	}
	var files []string
	for rel := range strings.SplitSeq(res.Stdout, "\x00") {
		if rel == "" {
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
		return nil, errors.New("git named no files to scan; nothing was checked")
	}
	slices.Sort(files)
	return slices.Compact(files), nil
}

// report is the part of semgrep's JSON that matters.
type semgrepReport struct {
	Results []struct {
		Path  string `json:"path"`
		Start struct {
			Line int `json:"line"`
		} `json:"start"`
	} `json:"results"`
	Errors []struct {
		Type json.RawMessage `json:"type"`
		Path string          `json:"path"`
	} `json:"errors"`
	Paths *struct {
		Scanned []string `json:"scanned"`
		Skipped []struct {
			Path string `json:"path"`
		} `json:"skipped"`
	} `json:"paths"`
}

// scan hands the files to semgrep by name, never as a directory: a directory scan applies
// semgrep's default ignore list, which skips tests/ and would hide a copy there.
func (g *gate) scan(ctx context.Context, files []string) (semgrepReport, error) {
	argv := append([]string{"semgrep", "--config", g.path(ruleFile), "--json", "--quiet", "--metrics=off", "--max-target-bytes=0"}, files...)
	res, err := g.runTool(ctx, semgrepTimeout, argv...)
	if err != nil {
		return semgrepReport{}, err
	}
	var rep semgrepReport
	switch jsonErr := json.Unmarshal([]byte(res.Stdout), &rep); {
	case res.Code >= 2:
		return semgrepReport{}, fmt.Errorf("semgrep failed (exit %d)\n%s", res.Code, res.Stderr)
	case jsonErr != nil:
		return semgrepReport{}, fmt.Errorf("semgrep failed (exit %d): its output is not a JSON report: %w\n%s", res.Code, jsonErr, res.Stderr)
	case rep.Paths == nil:
		return semgrepReport{}, fmt.Errorf("semgrep failed (exit %d): its report has no paths, so it cannot be told what was read\n%s", res.Code, res.Stderr)
	}
	return rep, nil
}

// judge refuses a pass over files the analyser did not read, then reports what it found.
func (g *gate) judge(files []string, excl excludes, rep semgrepReport) (ExitCode, error) {
	if len(rep.Errors) > 0 {
		lines := make([]string, 0, len(rep.Errors))
		for _, e := range rep.Errors {
			lines = append(lines, fmt.Sprintf("%s (%s)", e.Path, strings.Trim(string(e.Type), `"`)))
		}
		return ExitCannotRun, fmt.Errorf("semgrep reported %d error(s), so those files were not fully analysed:\n%s", len(lines), indent(lines))
	}
	seen := map[string]bool{}
	var skipped []string
	for _, p := range rep.Paths.Scanned {
		seen[strings.TrimPrefix(p, "./")] = true
	}
	for _, s := range rep.Paths.Skipped {
		seen[strings.TrimPrefix(s.Path, "./")] = true
		skipped = append(skipped, strings.TrimPrefix(s.Path, "./"))
	}
	var lost []string
	for _, f := range files {
		if !seen[f] && !excl.matches(f) {
			lost = append(lost, f)
		}
	}
	if len(lost) > 0 {
		return ExitCannotRun, fmt.Errorf("semgrep did not scan %d file(s) it was handed and did not say why it skipped them:\n%s\n  A pass reported over files the analyser never opened is not a pass", len(lost), indent(lost))
	}
	if len(rep.Paths.Scanned) == 0 {
		return ExitCannotRun, errors.New("semgrep scanned no file, so a clean result proves nothing")
	}
	if len(skipped) > 0 {
		slices.Sort(skipped)
		fmt.Fprintf(&g.stdout, "⚠ gh-scopes: %d file(s) skipped by semgrep\n%s\n", len(skipped), indent(skipped))
	}
	return g.verdict(rep), nil
}

// finding is one distinct match.
type finding struct {
	path string
	line int
}

// verdict prints the findings and the passed line.
func (g *gate) verdict(rep semgrepReport) ExitCode {
	var found []finding
	byFile := map[string]bool{}
	for _, r := range rep.Results {
		f := finding{path: strings.TrimPrefix(r.Path, "./"), line: r.Start.Line}
		if !slices.Contains(found, f) {
			found = append(found, f)
		}
		byFile[f.path] = true
	}
	scanned := len(rep.Paths.Scanned)
	if len(found) == 0 {
		fmt.Fprintf(&g.stdout, "passed: %d failed: 0\n", scanned)
		return ExitOK
	}
	slices.SortFunc(found, func(a, b finding) int {
		return cmp.Or(strings.Compare(a.path, b.path), cmp.Compare(a.line, b.line))
	})
	for _, f := range found {
		fmt.Fprintf(&g.stdout, "%s:%d  %s\n", f.path, f.line, ruleName)
	}
	fmt.Fprintf(&g.stdout, "%s: %d line(s) in %d file(s) name a GitHub scope outside vars/github-required-scopes.yml. See CLAUDE/QA.md \"%s\".\n", ruleName, len(found), len(byFile), ruleName)
	fmt.Fprintf(&g.stdout, "passed: %d failed: %d\n", scanned-len(byFile), len(byFile))
	return ExitFailed
}

func indent(items []string) string {
	return "    " + strings.Join(items, "\n    ")
}
