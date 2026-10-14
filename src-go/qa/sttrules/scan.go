package sttrules

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/semgrepreport"
)

// finding is one rule match in a file.
type finding struct {
	path string
	line int
	rule string
}

func (f finding) String() string { return fmt.Sprintf("%s:%d: %s", f.path, f.line, f.rule) }

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

// outcome is what the scans found and how many files semgrep read.
type outcome struct {
	found   []finding
	scanned int
}

// scan judges each language's files with that language's rules alone, in a private directory that
// is removed when the scan ends, whatever its outcome.
func (g *gate) scan(ctx context.Context, set ruleSet, sets fileSets) (outcome, error) {
	dir, err := os.MkdirTemp("", "qa-speech-to-text-rules-")
	if err != nil {
		return outcome{}, fmt.Errorf("cannot make a directory for the rule files: %s", reason(err))
	}
	got, scanErr := g.scanAll(ctx, set, sets, dir)
	if rmErr := os.RemoveAll(dir); rmErr != nil {
		return outcome{}, errors.Join(scanErr, fmt.Errorf("cannot remove %s: %s", dir, reason(rmErr)))
	}
	return got, scanErr
}

// scanAll scans each language that has files, writing its rule file into dir.
func (g *gate) scanAll(ctx context.Context, set ruleSet, sets fileSets, dir string) (outcome, error) {
	var total outcome
	for _, lang := range languages {
		files := sets[lang]
		if len(files) == 0 {
			continue
		}
		rules, err := set.rulesFor(lang, dir)
		if err != nil {
			return outcome{}, err
		}
		got, err := g.scanLanguage(ctx, lang, rules, files)
		if err != nil {
			return outcome{}, err
		}
		total.found = append(total.found, got...)
		total.scanned += len(files)
	}
	return total, nil
}

// scanLanguage hands the files to semgrep by name, never as a directory (a directory scan applies
// semgrep's default ignore list, which skips tests/), and refuses a pass over any it did not read.
func (g *gate) scanLanguage(ctx context.Context, lang language, rules string, files []string) ([]finding, error) {
	argv := slices.Concat([]string{
		"semgrep", "--metrics=off", "--disable-version-check", "--config", rules,
		"--no-rewrite-rule-ids", "--scan-unknown-extensions", "--json", "--quiet", "--max-target-bytes=0",
	}, files)
	res, err := g.runTool(ctx, semgrepTimeout, argv...)
	if err != nil {
		return nil, err
	}
	if res.Code >= 2 || strings.TrimSpace(res.Stdout) == "" {
		return nil, fmt.Errorf("semgrep failed on the %s files (exit %d):\n%s", nounOf[lang], res.Code, g.scrub(strings.TrimSpace(res.Stderr)))
	}
	var rep semgrepReport
	if err := json.Unmarshal([]byte(res.Stdout), &rep); err != nil {
		return nil, fmt.Errorf("semgrep's report on the %s files cannot be read: %w", nounOf[lang], err)
	}
	if err := judge(lang, files, rep); err != nil {
		return nil, err
	}
	found := make([]finding, 0, len(rep.Results))
	for _, r := range rep.Results {
		found = append(found, finding{path: tidy(r.Path), line: r.Start.Line, rule: r.CheckID})
	}
	return found, nil
}

// judge refuses a report over code the analyser did not read: a parse error, or a file handed
// over that it never opened (or skipped).
func judge(lang language, files []string, rep semgrepReport) error {
	if len(rep.Errors) > 0 {
		lines := make([]string, 0, len(rep.Errors))
		for _, e := range rep.Errors {
			path := e.Path
			if path == "" {
				path = "?"
			}
			lines = append(lines, fmt.Sprintf("    %s: %s", path, e.Message))
		}
		return fmt.Errorf("semgrep could not parse some %s files, so no rule ran on them:\n%s", nounOf[lang], strings.Join(lines, "\n"))
	}
	if rep.Paths == nil {
		return errors.New("semgrep's report has no paths.scanned: it cannot be told what was read")
	}
	read := map[string]bool{}
	for _, p := range rep.Paths.Scanned {
		read[tidy(p)] = true
	}
	var lost []string
	for _, rel := range files {
		if !read[tidy(rel)] {
			lost = append(lost, "    "+rel)
		}
	}
	if len(lost) > 0 {
		slices.Sort(lost)
		return fmt.Errorf("semgrep did not scan files it was handed:\n%s\n  A pass reported over files the analyser never opened is not a pass", strings.Join(lost, "\n"))
	}
	return nil
}

// finish prints the verdict: the findings sorted without duplicates, then the counts the qa-all
// reader matches.
func (g *gate) finish(got outcome) ExitCode {
	scanned, found := got.scanned, got.found
	slices.SortFunc(found, func(a, b finding) int {
		return cmp.Or(strings.Compare(a.path, b.path), cmp.Compare(a.line, b.line), strings.Compare(a.rule, b.rule))
	})
	found = slices.Compact(found)
	if len(found) == 0 {
		fmt.Fprintf(&g.stdout, "passed: %d failed: 0\n", scanned)
		return ExitOK
	}
	files := map[string]bool{}
	for _, f := range found {
		fmt.Fprintln(&g.stdout, f)
		files[f.path] = true
	}
	fmt.Fprintf(&g.stdout, "speech-to-text rules: %d finding(s) in %d file(s). See CLAUDE/QA.md under each identifier.\n", len(found), len(files))
	fmt.Fprintf(&g.stdout, "passed: %d failed: %d\n", scanned-len(files), len(files))
	return ExitFailed
}
