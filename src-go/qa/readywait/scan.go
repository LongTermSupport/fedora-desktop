package readywait

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/discovery"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/semgrepreport"
)

// requireSemgrep is that semgrep runs at all.
func (g *gate) requireSemgrep(ctx context.Context) error {
	res, err := g.runTool(ctx, versionTimeout, "semgrep", "--version")
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return errors.New("semgrep not found. Install with: pipx install semgrep")
	case err != nil:
		return fmt.Errorf("semgrep --version could not run: %w", err)
	case res.Code != 0:
		return fmt.Errorf("semgrep --version exited %d: %s", res.Code, strings.TrimSpace(res.Stderr))
	}
	return nil
}

// relative is a named file as the repository names it: relative to the root. A path that is
// absolute or climbs out of the root must lie inside it, else it is refused rather than judged
// under another name.
func (g *gate) relative(p string) (string, error) {
	rel := p
	if filepath.IsAbs(p) {
		var err error
		if rel, err = filepath.Rel(g.root, p); err != nil {
			return "", fmt.Errorf("%s is outside the repository root", p)
		}
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("%s is outside the repository root", p)
	}
	return rel, nil
}

// proveRules runs each half of the rule against its annotated fixture. A rule that silently
// stops matching is worse than a missing tool, so a regression here is the run failing and never
// "0 findings".
func (g *gate) proveRules(ctx context.Context) error {
	if _, err := g.readFile(rulesPath); err != nil {
		return err
	}
	if _, err := g.readFile(pyFixturePath); err != nil {
		return err
	}
	fixture, err := g.readFile(shFixturePath)
	if err != nil {
		return err
	}
	res, err := g.runTool(ctx, semgrepTimeout, "semgrep", "--test", "--metrics=off", "--disable-version-check",
		"--config", g.path(rulesPath), g.path(pyFixturePath))
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return failed("%s failed its fixture %s:\n%s%s", rulesPath, pyFixturePath, res.Stdout, res.Stderr)
	}
	got, err := Findings(string(fixture))
	if err != nil {
		return fmt.Errorf("the bash rule cannot read its fixture %s: %w", shFixturePath, err)
	}
	marks := FixtureMarks(string(fixture))
	if len(marks) == 0 || !slices.Equal(marks, got) {
		return failed("the bash rule failed its fixture %s: the fixture marks lines %v, the rule reports %v", shFixturePath, marks, got)
	}
	return nil
}

// fileSets is the files to judge, by half.
type fileSets struct{ py, sh []string }

// files is the Python and shell files to judge: those named, else every tracked one.
func (g *gate) files(paths []string) (fileSets, error) {
	if len(paths) > 0 {
		return g.classify(paths)
	}
	py, err := g.discover("tracked-python")
	if err != nil {
		return fileSets{}, err
	}
	py = slices.DeleteFunc(py, func(rel string) bool { return strings.HasPrefix(rel, fixtureTree) })
	sh, err := g.discover("tracked-shell")
	if err != nil {
		return fileSets{}, err
	}
	if len(py) == 0 || len(sh) == 0 {
		return fileSets{}, fmt.Errorf("discovery found %d Python and %d shell files; nothing would be checked", len(py), len(sh))
	}
	return fileSets{py: py, sh: sh}, nil
}

// discover asks the shared discovery for one list.
func (g *gate) discover(list string) ([]string, error) {
	var out, errs bytes.Buffer
	env := discovery.Env{Stdout: &out, Stderr: &errs, Cwd: g.root, Runner: g.env.Runner}
	if code := discovery.Main([]string{flagRoot, g.root, flagScripts, g.scripts, list}, env); code != discovery.ExitOK {
		return nil, errors.New(strings.TrimPrefix(strings.TrimSpace(errs.String()), "ERROR: "))
	}
	var files []string
	for rel := range strings.SplitSeq(out.String(), "\x00") {
		if rel != "" {
			files = append(files, rel)
		}
	}
	return files, nil
}

// classify sorts named files into Python and shell: by name, else by a shebang naming python.
func (g *gate) classify(paths []string) (fileSets, error) {
	var sets fileSets
	seen := map[string]bool{}
	for _, named := range paths {
		rel, err := g.relative(named)
		if err != nil {
			return fileSets{}, err
		}
		if seen[rel] {
			continue
		}
		seen[rel] = true
		isPython, err := g.isPython(rel)
		if err != nil {
			return fileSets{}, err
		}
		if isPython {
			sets.py = append(sets.py, rel)
		} else {
			sets.sh = append(sets.sh, rel)
		}
	}
	return sets, nil
}

func (g *gate) isPython(rel string) (bool, error) {
	data, err := g.readFile(rel)
	if err != nil {
		return false, err
	}
	switch filepath.Ext(rel) {
	case ".py":
		return true, nil
	case ".bash", ".sh":
		return false, nil
	}
	first := bytes.SplitN(data, []byte("\n"), 2)[0]
	return bytes.HasPrefix(first, []byte("#!")) && bytes.Contains(first, []byte("python")), nil
}

// finding is one ready-wait in a file.
type finding struct {
	path string
	line int
	rule string
}

func (f finding) String() string { return fmt.Sprintf("%s:%d: %s", f.path, f.line, f.rule) }

// scan judges both halves.
func (g *gate) scan(ctx context.Context, sets fileSets) ([]finding, error) {
	var found []finding
	if len(sets.py) > 0 {
		pyFound, err := g.scanPython(ctx, sets.py)
		if err != nil {
			return nil, err
		}
		found = append(found, pyFound...)
	}
	for _, rel := range sets.sh {
		data, err := g.readFile(rel)
		if err != nil {
			return nil, err
		}
		lines, findErr := Findings(string(data))
		if findErr != nil {
			return nil, fmt.Errorf("%s: %w", rel, findErr)
		}
		for _, line := range lines {
			found = append(found, finding{path: rel, line: line, rule: RuleID})
		}
	}
	return found, nil
}

// semgrepReport is the part of semgrep's JSON that matters.
type semgrepReport struct {
	Results []semgrepreport.Result `json:"results"`
	Errors  []struct {
		Path    string `json:"path"`
		Message string `json:"message"`
	} `json:"errors"`
	Paths *struct {
		Scanned []string `json:"scanned"`
	} `json:"paths"`
}

// tidy is a path as the repository names it: no leading ./.
func tidy(p string) string { return filepath.ToSlash(filepath.Clean(strings.TrimPrefix(p, "./"))) }

// scanPython hands the files to semgrep and refuses a pass over any it did not read.
func (g *gate) scanPython(ctx context.Context, py []string) ([]finding, error) {
	argv := slices.Concat([]string{
		"semgrep", "--metrics=off", "--disable-version-check", "--config", g.path(rulesPath),
		"--no-rewrite-rule-ids", "--scan-unknown-extensions", "--json", "--quiet",
	}, py)
	res, err := g.runTool(ctx, semgrepTimeout, argv...)
	if err != nil {
		return nil, err
	}
	if res.Code >= 2 || strings.TrimSpace(res.Stdout) == "" {
		return nil, fmt.Errorf("semgrep failed on the Python files (exit %d):\n%s", res.Code, strings.TrimSpace(res.Stderr))
	}
	var report semgrepReport
	if err := json.Unmarshal([]byte(res.Stdout), &report); err != nil {
		return nil, fmt.Errorf("semgrep's report cannot be read: %w", err)
	}
	if err := g.judge(py, report); err != nil {
		return nil, err
	}
	found := make([]finding, 0, len(report.Results))
	for _, r := range report.Results {
		found = append(found, finding{path: tidy(r.Path), line: r.Start.Line, rule: r.CheckID})
	}
	return found, nil
}

// judge refuses a report over code the analyser did not read: a parse error, or a file handed
// over that it never opened.
func (g *gate) judge(py []string, report semgrepReport) error {
	if len(report.Errors) > 0 {
		lines := make([]string, 0, len(report.Errors))
		for _, e := range report.Errors {
			path := e.Path
			if path == "" {
				path = "?"
			}
			lines = append(lines, fmt.Sprintf("    %s: %s", path, e.Message))
		}
		return fmt.Errorf("semgrep could not parse some Python files, so the rule did not run on them:\n%s", strings.Join(lines, "\n"))
	}
	if report.Paths == nil {
		return errors.New("semgrep's report has no paths.scanned: it cannot be told what was read")
	}
	scanned := map[string]bool{}
	for _, p := range report.Paths.Scanned {
		scanned[tidy(p)] = true
	}
	var lost []string
	for _, rel := range py {
		if !scanned[tidy(rel)] {
			lost = append(lost, "    "+rel)
		}
	}
	if len(lost) > 0 {
		slices.Sort(lost)
		return fmt.Errorf("semgrep did not scan Python files it was handed:\n%s", strings.Join(lost, "\n"))
	}
	return nil
}

// finish prints the verdict: the coverage line, the findings sorted without duplicates, and the
// counts the qa-all reader matches.
func (g *gate) finish(pyCount, shCount int, found []finding) ExitCode {
	scanned := pyCount + shCount
	fmt.Fprintf(&g.stdout, "scanned: %d Python file(s) with semgrep, %d shell file(s) with the bash rule\n", pyCount, shCount)
	lines := make([]string, 0, len(found))
	files := map[string]bool{}
	for _, f := range found {
		lines = append(lines, f.String())
		files[f.path] = true
	}
	slices.Sort(lines)
	lines = slices.Compact(lines)
	if len(lines) == 0 {
		fmt.Fprintf(&g.stdout, "passed: %d failed: 0\n", scanned)
		return ExitOK
	}
	fmt.Fprintln(&g.stdout, strings.Join(lines, "\n"))
	fmt.Fprintf(&g.stdout, "ready-wait rule: %d finding(s) in %d file(s). See CLAUDE/QA.md \"%s\".\n", len(lines), len(files), RuleID)
	fmt.Fprintf(&g.stdout, "passed: %d failed: %d\n", scanned-len(files), len(files))
	return ExitFailed
}
