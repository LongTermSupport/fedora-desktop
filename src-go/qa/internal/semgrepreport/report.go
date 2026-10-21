// Package semgrepreport is what every gate reading semgrep shares: the finding, the JSON report
// that carries it, the judgement of whether semgrep read every file it was handed, and the fixture
// annotation a rule self-test needs. semgrep names its keys in snake_case, which is why this is
// the one place the camelCase lint rule is excused for them.
package semgrepreport

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// TimeoutFlag is semgrep's limit on one rule over one file (parse included), which every scan
// passes. semgrep's default is 5 s, measured in wall time, so on a loaded host a clean file it
// was starved on comes back as a Timeout error, which a gate must treat as not analysed. The
// largest tracked scripts take well under a second; 120 s is far beyond any honest file and still
// names the file that a runaway rule stalls on, inside the gates' own whole-run deadline.
const TimeoutFlag = "--timeout=120"

// Result is one match of a rule.
type Result struct {
	CheckID string `json:"check_id"`
	Path    string `json:"path"`
	Start   struct {
		Line int `json:"line"`
	} `json:"start"`
	Extra struct {
		Message string `json:"message"`
	} `json:"extra"`
}

// Report is the part of semgrep's --json report that a gate judges.
type Report struct {
	Results []Result `json:"results"`
	Errors  []struct {
		Path    string `json:"path"`
		Message string `json:"message"`
	} `json:"errors"`
	Paths *struct {
		Scanned []string `json:"scanned"`
	} `json:"paths"`
}

// Decode reads semgrep's stdout as a report. A report with no paths is refused: it cannot be told
// what was read.
func Decode(stdout string) (Report, error) {
	var rep Report
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		return Report{}, fmt.Errorf("semgrep's report cannot be read: %w", err)
	}
	if rep.Paths == nil {
		return Report{}, errors.New("semgrep's report has no paths.scanned: it cannot be told what was read")
	}
	return rep, nil
}

// Tidy is a path as the repository names it: no leading ./.
func Tidy(p string) string { return filepath.ToSlash(filepath.Clean(strings.TrimPrefix(p, "./"))) }

// Judge refuses a report over code the analyser did not read: a parse error, or a handed file
// that is not among the paths it scanned. noun names the files ("Python") in the message.
func (r Report) Judge(noun string, handed []string) error {
	if len(r.Errors) > 0 {
		lines := make([]string, 0, len(r.Errors))
		for _, e := range r.Errors {
			path := e.Path
			if path == "" {
				path = "?"
			}
			lines = append(lines, fmt.Sprintf("    %s: %s", path, e.Message))
		}
		return fmt.Errorf("semgrep could not parse some %s files, so no rule ran on them:\n%s", noun, strings.Join(lines, "\n"))
	}
	read := map[string]bool{}
	for _, p := range r.Paths.Scanned {
		read[Tidy(p)] = true
	}
	var lost []string
	for _, rel := range handed {
		if !read[Tidy(rel)] {
			lost = append(lost, "    "+rel)
		}
	}
	if len(lost) > 0 {
		slices.Sort(lost)
		return fmt.Errorf("semgrep did not scan %s files it was handed:\n%s\n  A pass reported over files the analyser never opened is not a pass", noun, strings.Join(lost, "\n"))
	}
	return nil
}

// ScannedCount is the number of distinct files semgrep says it read.
func (r Report) ScannedCount() int {
	read := map[string]bool{}
	for _, p := range r.Paths.Scanned {
		read[Tidy(p)] = true
	}
	return len(read)
}

// annotationRe is a fixture annotation that must fire. It starts at anything but a letter, digit
// or underscore, so todoruleid (semgrep does not fail a todoruleid line that does not match) is
// not one.
var annotationRe = regexp.MustCompile(`(?:^|[^\w])ruleid:\s*([\w.-]+(?:\s*,\s*[\w.-]+)*)`)

// Annotated is the rule ids a fixture marks with a ruleid annotation. semgrep --test exits 0 on a
// fixture with none, so a gate must see at least one before it takes the self-test as a proof.
func Annotated(fixture string) map[string]bool {
	ids := map[string]bool{}
	for _, m := range annotationRe.FindAllStringSubmatch(fixture, -1) {
		for id := range strings.SplitSeq(m[1], ",") {
			ids[strings.TrimSpace(id)] = true
		}
	}
	return ids
}
