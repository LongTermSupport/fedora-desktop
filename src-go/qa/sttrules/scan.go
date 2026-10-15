package sttrules

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
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

// outcome is what the scans found and how many files semgrep says it read.
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
		total.found = append(total.found, got.found...)
		total.scanned += got.scanned
	}
	return total, nil
}

// scanLanguage hands the files to semgrep by name, never as a directory (a directory scan applies
// semgrep's default ignore list, which skips tests/), and refuses a pass over any it did not read.
func (g *gate) scanLanguage(ctx context.Context, lang language, rules string, files []string) (outcome, error) {
	argv := slices.Concat([]string{
		"semgrep", "--metrics=off", "--disable-version-check", "--config", rules,
		"--no-rewrite-rule-ids", "--scan-unknown-extensions", "--json", "--quiet", "--max-target-bytes=0",
	}, files)
	res, err := g.runTool(ctx, semgrepTimeout, argv...)
	if err != nil {
		return outcome{}, err
	}
	if res.Code >= 2 || strings.TrimSpace(res.Stdout) == "" {
		return outcome{}, fmt.Errorf("semgrep failed on the %s files (exit %d):\n%s", nounOf[lang], res.Code, g.scrub(strings.TrimSpace(res.Stderr)))
	}
	rep, err := semgrepreport.Decode(res.Stdout)
	if err != nil {
		return outcome{}, fmt.Errorf("on the %s files: %w", nounOf[lang], err)
	}
	if err := rep.Judge(nounOf[lang], files); err != nil {
		return outcome{}, err
	}
	found := make([]finding, 0, len(rep.Results))
	for _, r := range rep.Results {
		found = append(found, finding{path: semgrepreport.Tidy(r.Path), line: r.Start.Line, rule: r.CheckID})
	}
	return outcome{found: found, scanned: rep.ScannedCount()}, nil
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
