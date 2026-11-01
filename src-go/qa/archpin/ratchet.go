package archpin

import (
	"fmt"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"
)

// record is one allowlist row: a file's known findings and what is known about them.
type record struct {
	count int
	note  string
	line  int
}

// allowlist is the ratchet, by repository path.
type allowlist map[string]record

// parseAllowlist reads `path TAB count TAB note` rows; a # line or a blank line is not a row. A
// malformed row is an error naming its line, so a typo cannot silently drop a record.
func parseAllowlist(text string) (allowlist, error) {
	list := allowlist{}
	for i, raw := range strings.Split(text, "\n") {
		n := i + 1
		if strings.TrimSpace(raw) == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		fields := strings.Split(raw, "\t")
		if len(fields) != 3 {
			return nil, fmt.Errorf("%s:%d: want path TAB count TAB note, got %d field(s)", allowlistFile, n, len(fields))
		}
		p, countText, note := fields[0], fields[1], strings.TrimSpace(fields[2])
		count, err := strconv.Atoi(countText)
		switch {
		case p == "" || path.IsAbs(p) || path.Clean(p) != p || strings.HasPrefix(p, "../"):
			return nil, fmt.Errorf("%s:%d: %q is not a clean repository path", allowlistFile, n, p)
		case err != nil || count < 1:
			return nil, fmt.Errorf("%s:%d: the count %q is not a number of findings above 0", allowlistFile, n, countText)
		case note == "":
			return nil, fmt.Errorf("%s:%d: the row says nothing about its findings", allowlistFile, n)
		}
		if prev, dup := list[p]; dup {
			return nil, fmt.Errorf("%s:%d: %s is already listed on line %d", allowlistFile, n, p, prev.line)
		}
		list[p] = record{count: count, note: note, line: n}
	}
	return list, nil
}

// problem is one file the ratchet refuses, with the finding lines to show.
type problem struct {
	path  string
	why   string
	lines []int
}

// ratchet holds each scanned file to its record: none for a file not listed, exactly the record
// for a listed one. A listed file that is not among the scanned files must leave the list.
func ratchet(list allowlist, found map[string][]int, scanned []string) []problem {
	var out []problem
	for _, p := range slices.Sorted(maps.Keys(found)) {
		lines := found[p]
		rec, listed := list[p]
		switch {
		case !listed:
			out = append(out, problem{p, fmt.Sprintf("%d finding(s), and the file is not on %s", len(lines), allowlistFile), lines})
		case len(lines) > rec.count:
			out = append(out, problem{p, fmt.Sprintf("%d finding(s), above its record of %d on %s line %d", len(lines), rec.count, allowlistFile, rec.line), lines})
		case len(lines) < rec.count:
			out = append(out, problem{p, fmt.Sprintf("%d finding(s): lower its record from %d to %d on %s line %d", len(lines), rec.count, len(lines), allowlistFile, rec.line), nil})
		}
	}
	for _, p := range slices.Sorted(maps.Keys(list)) {
		if len(found[p]) > 0 {
			continue
		}
		why := "has no finding left: delete its row"
		if !slices.Contains(scanned, p) {
			why = "is not a scanned file: delete its row"
		}
		out = append(out, problem{p, fmt.Sprintf("%s %s (line %d)", allowlistFile, why, list[p].line), nil})
	}
	return out
}

// verdict prints the problems, the known instances and the passed line.
func (g *gate) verdict(list allowlist, found map[string][]int, scanned []string) ExitCode {
	problems := ratchet(list, found, scanned)
	failing := map[string]bool{}
	for _, pr := range problems {
		for _, l := range pr.lines {
			fmt.Fprintf(&g.stdout, "%s:%d  %s\n", pr.path, l, ruleName)
		}
		fmt.Fprintf(&g.stdout, "%s: %s: %s\n", ruleName, pr.path, pr.why)
		if slices.Contains(scanned, pr.path) {
			failing[pr.path] = true
		}
	}
	known, knownFiles := 0, 0
	for p, rec := range list {
		if len(found[p]) == rec.count {
			known += rec.count
			knownFiles++
		}
	}
	if known > 0 {
		fmt.Fprintf(&g.stdout, "%s: %d known instance(s) in %d file(s) held by %s; each is still to be fixed\n", ruleName, known, knownFiles, allowlistFile)
	}
	if len(problems) > 0 {
		fmt.Fprintf(&g.stdout, "%s: select the artefact and its checksum by the architecture, or fail naming what has no build. See CLAUDE/QA.md \"%s\".\n", ruleName, ruleName)
	}
	fmt.Fprintf(&g.stdout, "passed: %d failed: %d\n", len(scanned)-len(failing), len(failing))
	if len(problems) > 0 {
		return ExitFailed
	}
	return ExitOK
}
