package archpin

import (
	"fmt"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"
)

// allowlistFormat is the shape of the allowlist this gate writes and reads as its own: one row
// per known finding. Format 1 (no format line) held a count per file; it is read only as a base.
const allowlistFormat = 2

const formatPrefix = "# format: "

// finding is one line the rule reports, by its identity: the file and the line's text with its
// whitespace collapsed. The line number moves with every edit above it, so it is not part of the
// identity; it is kept to name the line.
type finding struct {
	text string
	line int
}

// identity is a finding's text as the allowlist records it: the whitespace runs of the line
// collapsed to one space, so a tab cannot split a row and re-indenting is not a new finding.
func identity(line string) string { return strings.Join(strings.Fields(line), " ") }

// entry is one allowlist row: a known finding of a file and what is known about it.
type entry struct {
	path, text, note string
	line             int
}

// allowlist is the ratchet as read: its format, and the rows (format 2) or the count per file
// (format 1).
type allowlist struct {
	format  int
	entries []entry
	counts  map[string]int
}

// perFile is how many rows each file has.
func (a allowlist) perFile() map[string]int {
	if a.format != allowlistFormat {
		return a.counts
	}
	out := map[string]int{}
	for _, e := range a.entries {
		out[e.path]++
	}
	return out
}

// parseAllowlist reads the allowlist: a `# format: N` line, then rows; another # line or a blank
// line is not a row. Format 2 rows are `path TAB finding TAB note`, format 1 rows (no format
// line) `path TAB count TAB note`. A malformed row is an error naming its line, so a typo cannot
// silently drop a record.
func parseAllowlist(text string) (allowlist, error) {
	list := allowlist{format: 1, counts: map[string]int{}}
	for i, raw := range strings.Split(text, "\n") {
		n := i + 1
		if rest, ok := strings.CutPrefix(raw, formatPrefix); ok {
			v, err := strconv.Atoi(strings.TrimSpace(rest))
			if err != nil || (v != 1 && v != allowlistFormat) {
				return allowlist{}, fmt.Errorf("%s:%d: %q is not a known format (1 or %d)", allowlistFile, n, raw, allowlistFormat)
			}
			list.format = v
			continue
		}
		if strings.TrimSpace(raw) == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		if err := addRow(&list, raw, n); err != nil {
			return allowlist{}, err
		}
	}
	return list, nil
}

// addRow reads one row into the list.
func addRow(a *allowlist, raw string, n int) error {
	fields := strings.Split(raw, "\t")
	if len(fields) != 3 {
		middle := "count"
		if a.format == allowlistFormat {
			middle = "finding"
		}
		return fmt.Errorf("%s:%d: want path TAB %s TAB note, got %d field(s)", allowlistFile, n, middle, len(fields))
	}
	p, mid, note := fields[0], fields[1], strings.TrimSpace(fields[2])
	switch {
	case p == "" || path.IsAbs(p) || path.Clean(p) != p || strings.HasPrefix(p, "../"):
		return fmt.Errorf("%s:%d: %q is not a clean repository path", allowlistFile, n, p)
	case note == "":
		return fmt.Errorf("%s:%d: the row says nothing about its finding", allowlistFile, n)
	}
	if a.format == allowlistFormat {
		if identity(mid) != mid || mid == "" {
			return fmt.Errorf("%s:%d: the finding %q is not a line's text with its whitespace collapsed", allowlistFile, n, mid)
		}
		a.entries = append(a.entries, entry{path: p, text: mid, note: note, line: n})
		return nil
	}
	count, err := strconv.Atoi(mid)
	switch {
	case err != nil || count < 1:
		return fmt.Errorf("%s:%d: the count %q is not a number of findings above 0", allowlistFile, n, mid)
	case a.counts[p] > 0:
		return fmt.Errorf("%s:%d: %s is listed twice", allowlistFile, n, p)
	}
	a.counts[p] = count
	return nil
}

// problem is one file the ratchet refuses, with the finding lines to show.
type problem struct {
	path  string
	why   string
	lines []int
}

// unmatched is what is left of found once each listed text has taken one finding with that
// text, and the rows that took none.
func unmatched(rows []entry, found []finding) ([]finding, []entry) {
	left := slices.Clone(found)
	var stale []entry
	for _, e := range rows {
		i := slices.IndexFunc(left, func(f finding) bool { return f.text == e.text })
		if i < 0 {
			stale = append(stale, e)
			continue
		}
		left = slices.Delete(left, i, i+1)
	}
	return left, stale
}

// ratchet holds each scanned file to its rows: a file not listed may have no finding, a listed
// one exactly the findings its rows name, by text. A new finding fails however many old ones
// were fixed beside it; a row whose finding is gone (fixed, or its line edited) must leave the
// list. A listed file that is not among the scanned files must leave the list.
func ratchet(list allowlist, found map[string][]finding, scanned []string) []problem {
	rows := map[string][]entry{}
	for _, e := range list.entries {
		rows[e.path] = append(rows[e.path], e)
	}
	var out []problem
	for _, p := range slices.Sorted(maps.Keys(found)) {
		listed := rows[p]
		fresh, stale := unmatched(listed, found[p])
		if len(fresh) > 0 {
			why := fmt.Sprintf("%d finding(s), and the file is not on %s", len(fresh), allowlistFile)
			if len(listed) > 0 {
				why = fmt.Sprintf("%d finding(s) that no row of %s names", len(fresh), allowlistFile)
			}
			out = append(out, problem{p, why, linesOf(fresh)})
		}
		for _, e := range stale {
			out = append(out, problem{p, fmt.Sprintf("%s line %d names a finding the file no longer has (fixed, or its line edited): delete the row", allowlistFile, e.line), nil})
		}
	}
	for _, p := range slices.Sorted(maps.Keys(rows)) {
		if len(found[p]) > 0 {
			continue
		}
		why := "has no finding left: delete its rows"
		if !slices.Contains(scanned, p) {
			why = "is not a scanned file: delete its rows"
		}
		out = append(out, problem{p, fmt.Sprintf("%s %s (line %d)", allowlistFile, why, rows[p][0].line), nil})
	}
	return out
}

func linesOf(fs []finding) []int {
	out := make([]int, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.line)
	}
	return out
}

// againstBase is what cur gained over base, the allowlist at the base commit: a row base does
// not have (by file and text for a format 2 base; by its file's count for a format 1 base, which
// recorded no text). The list only shrinks: a new instance is fixed, never listed.
func againstBase(base, cur allowlist, ref string) []problem {
	var out []problem
	if base.format == allowlistFormat {
		had := map[string]int{}
		for _, e := range base.entries {
			had[e.path+"\t"+e.text]++
		}
		for _, e := range cur.entries {
			k := e.path + "\t" + e.text
			if had[k] > 0 {
				had[k]--
				continue
			}
			out = append(out, problem{e.path, fmt.Sprintf("%s line %d is a row %s does not have: the list only shrinks, so select the artefact by architecture instead", allowlistFile, e.line, ref), nil})
		}
		return out
	}
	was := base.perFile()
	now := cur.perFile()
	for _, p := range slices.Sorted(maps.Keys(now)) {
		switch {
		case was[p] == 0:
			out = append(out, problem{p, fmt.Sprintf("the file has rows on %s and none at %s: the list only shrinks, so select the artefact by architecture instead", allowlistFile, ref), nil})
		case now[p] > was[p]:
			out = append(out, problem{p, fmt.Sprintf("%d row(s) on %s, above the %d at %s: the list only shrinks", now[p], allowlistFile, was[p], ref), nil})
		}
	}
	return out
}

// known is how many rows still name a finding, and in how many files.
func known(list allowlist, found map[string][]finding) (int, int) {
	rows := map[string][]entry{}
	for _, e := range list.entries {
		rows[e.path] = append(rows[e.path], e)
	}
	n, files := 0, 0
	for p, rs := range rows {
		_, stale := unmatched(rs, found[p])
		if held := len(rs) - len(stale); held > 0 {
			n += held
			files++
		}
	}
	return n, files
}

// verdict prints the problems (the ratchet's, then the base's), the known instances and the
// passed line.
func (g *gate) verdict(list allowlist, found map[string][]finding, scanned []string, base []problem) ExitCode {
	problems := append(ratchet(list, found, scanned), base...)
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
	if n, files := known(list, found); n > 0 {
		fmt.Fprintf(&g.stdout, "%s: %d known instance(s) in %d file(s) held by %s; each is still to be fixed\n", ruleName, n, files, allowlistFile)
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
