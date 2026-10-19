package patterns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/semgrepreport"
)

// requireSemgrep is that semgrep runs at all.
func (g *gate) requireSemgrep(ctx context.Context) error {
	res, err := g.runTool(ctx, gitTimeout, g.root, "semgrep", "--version")
	if err != nil || res.Code != 0 {
		return errors.New("semgrep not found: run ./playbooks/imports/play-python.yml (it installs the pinned semgrep; do not install it by hand)")
	}
	return nil
}

// proveRules runs the rules against their annotated fixture. A rule that silently stops
// matching, or over-matches, is worse than a missing tool, so a regression here is the run
// failing and never "0 issues".
func (g *gate) proveRules(ctx context.Context) error {
	rules := filepath.Join(g.root, filepath.FromSlash(rulesPath))
	fixture := filepath.Join(g.root, filepath.FromSlash(fixturePath))
	for _, p := range []string{rules, fixture} {
		info, err := os.Stat(p)
		if err != nil {
			return fmt.Errorf("cannot read %s: %w", p, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", p)
		}
	}
	body, err := os.ReadFile(fixture)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", fixture, err)
	}
	if len(semgrepreport.Annotated(string(body))) == 0 {
		return fmt.Errorf("%s marks no ruleid line; the self-test proves nothing", fixturePath)
	}
	res, err := g.runTool(ctx, semgrepTimeout, g.root, "semgrep", "--test", "--config", rules, fixture, "--metrics=off")
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("semgrep rule self-tests failed (see %s)\n%s%s", fixturePath, res.Stdout, res.Stderr)
	}
	return nil
}

// finding is one rule match in a script.
type finding struct {
	rule string
	line int
	text string
}

// lineRange is a stretch of lines semgrep did not parse.
type lineRange struct{ first, last int }

// scanResult is what the rules found and what semgrep could not parse.
type scanResult struct {
	findings map[string][]finding
	partial  map[string][]lineRange
}

// noPath names the file of an error semgrep gives none.
const noPath = "(no path)"

// semgrepReport is the part of semgrep's JSON that matters.
type semgrepReport struct {
	Results []semgrepreport.Result `json:"results"`
	Errors  []struct {
		Type json.RawMessage `json:"type"`
		Path string          `json:"path"`
	} `json:"errors"`
	Paths *struct {
		Scanned []string `json:"scanned"`
	} `json:"paths"`
}

// scan mirrors the scripts, runs semgrep over the mirror and judges what comes back. The work
// directory (mirror and semgrep's output) is removed on every exit.
func (g *gate) scan(ctx context.Context, scripts []script) (scanResult, error) {
	work, err := os.MkdirTemp(g.env.TempDir, "qa-patterns-")
	if err != nil {
		return scanResult{}, fmt.Errorf("cannot make the scan mirror: %w", err)
	}
	res, scanErr := g.scanIn(ctx, work, scripts)
	if rmErr := os.RemoveAll(work); scanErr == nil && rmErr != nil {
		return scanResult{}, fmt.Errorf("cannot remove the scan mirror %s: %w", work, rmErr)
	}
	return res, scanErr
}

func (g *gate) scanIn(ctx context.Context, work string, scripts []script) (scanResult, error) {
	mirror := filepath.Join(work, "mirror")
	if err := g.buildMirror(mirror, scripts); err != nil {
		return scanResult{}, err
	}
	out := filepath.Join(work, "semgrep.json")
	if err := g.copyRules(filepath.Join(work, "rules.yml")); err != nil {
		return scanResult{}, err
	}
	// The rules sit beside the mirror and are named relative to it: a config file outside the
	// scan root makes semgrep prefix every rule id with that file's dotted absolute path.
	run, err := g.runTool(ctx, semgrepTimeout, mirror, "semgrep", "--config", "../rules.yml", "--json-output", out, "--metrics=off", "--quiet", semgrepreport.TimeoutFlag, "--max-target-bytes=0", ".")
	if err != nil {
		return scanResult{}, err
	}
	if run.Code != 0 {
		return scanResult{}, fmt.Errorf("semgrep failed (exit %d)\n%s", run.Code, run.Stderr)
	}
	data, err := os.ReadFile(out)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && len(data) == 0) {
		return scanResult{}, fmt.Errorf("semgrep produced no JSON output\n%s", run.Stderr)
	}
	if err != nil {
		return scanResult{}, fmt.Errorf("cannot read semgrep's report: %w", err)
	}
	var report semgrepReport
	if err := json.Unmarshal(data, &report); err != nil {
		return scanResult{}, fmt.Errorf("cannot read semgrep's report: %w", err)
	}
	return g.judge(scripts, report)
}

// copyRules puts the rules file where the scan reads it.
func (g *gate) copyRules(dest string) error {
	data, err := os.ReadFile(filepath.Join(g.root, filepath.FromSlash(rulesPath)))
	if err != nil {
		return fmt.Errorf("cannot read the rules: %w", err)
	}
	if err := os.WriteFile(dest, data, 0o600); err != nil {
		return fmt.Errorf("cannot copy the rules beside the scan mirror: %w", err)
	}
	return nil
}

// judge maps the report back to repository paths and refuses a pass over code the analyser
// did not read: a script semgrep never opened, one it could not parse, or any other error.
func (g *gate) judge(scripts []script, report semgrepReport) (scanResult, error) {
	res := scanResult{findings: map[string][]finding{}, partial: map[string][]lineRange{}}
	if report.Paths == nil {
		return res, errors.New("semgrep's report has no paths.scanned: it cannot be told what was read")
	}
	byMirror := make(map[string]string, len(scripts))
	for _, s := range scripts {
		byMirror[s.mirror] = s.rel
	}
	unmirror := func(p string) (string, error) {
		rel, ok := byMirror[strings.TrimPrefix(p, "./")]
		if !ok {
			return "", fmt.Errorf("semgrep reported on %q, which was not handed to it", p)
		}
		return rel, nil
	}
	seen := map[string]bool{}
	for _, p := range report.Paths.Scanned {
		seen[strings.TrimPrefix(p, "./")] = true
	}
	var missed []string
	for _, s := range scripts {
		if !seen[s.mirror] {
			missed = append(missed, s.rel)
		}
	}
	if len(missed) > 0 {
		return res, fmt.Errorf("semgrep did not scan %d discovered shell script(s):\n%s\n  A pass reported over files the analyser never opened is not a pass", len(missed), indent(missed))
	}
	if err := g.judgeErrors(report, unmirror, &res); err != nil {
		return res, err
	}
	for _, r := range report.Results {
		rel, err := unmirror(r.Path)
		if err != nil {
			return res, err
		}
		res.findings[rel] = append(res.findings[rel], finding{rule: r.CheckID, line: r.Start.Line, text: strings.Join(strings.Fields(r.Extra.Message), " ")})
	}
	return res, nil
}

// judgeErrors sorts semgrep's errors into the gating ones and the reported ones.
func (g *gate) judgeErrors(report semgrepReport, unmirror func(string) (string, error), res *scanResult) error {
	var unparsed, other []string
	for _, e := range report.Errors {
		name, ranges, err := errorType(e.Type)
		if err != nil {
			return err
		}
		rel := noPath
		if e.Path != "" {
			if rel, err = unmirror(e.Path); err != nil {
				return err
			}
		}
		switch name {
		case "Syntax error":
			unparsed = append(unparsed, rel)
		case "PartialParsing":
			res.partial[rel] = append(res.partial[rel], ranges...)
		default:
			other = append(other, fmt.Sprintf("%s (%s)", rel, name))
		}
	}
	if len(unparsed) > 0 {
		slices.Sort(unparsed)
		unparsed = slices.Compact(unparsed)
		return fmt.Errorf("semgrep could not parse %d file(s), so NO rule ran on them:\n%s\n  A file the analyser never read cannot be reported as passing", len(unparsed), indent(unparsed))
	}
	if len(other) > 0 {
		slices.Sort(other)
		return fmt.Errorf("semgrep reported %d error(s) other than a parse gap, so those files were not fully analysed:\n%s", len(other), indent(other))
	}
	return nil
}

// errorType reads an error's type, which is a string, or for a partial parse an array of the
// name and the ranges semgrep skipped.
func errorType(raw json.RawMessage) (string, []lineRange, error) {
	var name string
	if err := json.Unmarshal(raw, &name); err == nil {
		return name, nil, nil
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil || len(parts) == 0 {
		return "", nil, fmt.Errorf("semgrep reported an error of an unreadable type: %s", raw)
	}
	if err := json.Unmarshal(parts[0], &name); err != nil {
		return "", nil, fmt.Errorf("semgrep reported an error of an unreadable type: %s", raw)
	}
	var ranges []lineRange
	if len(parts) > 1 {
		var spans []struct {
			Start struct {
				Line int `json:"line"`
			} `json:"start"`
			End struct {
				Line int `json:"line"`
			} `json:"end"`
		}
		if err := json.Unmarshal(parts[1], &spans); err != nil {
			return "", nil, fmt.Errorf("semgrep reported a %s error with unreadable ranges: %w", name, err)
		}
		for _, s := range spans {
			ranges = append(ranges, lineRange{first: s.Start.Line, last: s.End.Line})
		}
	}
	return name, ranges, nil
}

func indent(items []string) string {
	return "    " + strings.Join(items, "\n    ")
}
