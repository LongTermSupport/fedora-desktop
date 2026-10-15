package patterns

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// partialAllowlistFile is the allowlist of files that may be mostly outside the parse tree,
// in the scripts directory. Rows: <path> TAB <unparsed lines> TAB <the construct that stops
// the parser>. A record may only be lowered; a file that parses again loses its row.
const partialAllowlistFile = "patterns-partial.allowlist"

// ruleParseRatchet names the findings the ratchet adds to the report.
const ruleParseRatchet = "parse-ratchet"

// partialRecord is one row of the allowlist.
type partialRecord struct {
	lines     int
	construct string
}

// loadPartialAllowlist reads the allowlist; a missing file or a malformed row is an error, since
// a ratchet that cannot be read would let every file through.
func (g *gate) loadPartialAllowlist() (map[string]partialRecord, error) {
	path := partialAllowlistFile
	data, err := os.ReadFile(filepath.Join(g.scripts, partialAllowlistFile))
	if err != nil {
		return nil, fmt.Errorf("cannot read the parse allowlist: %w", err)
	}
	records := map[string]partialRecord{}
	for n, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		lines, convErr := 0, error(nil)
		if len(fields) == 3 {
			lines, convErr = strconv.Atoi(fields[1])
		}
		if len(fields) != 3 || convErr != nil || lines < 1 || fields[0] == "" || strings.TrimSpace(fields[2]) == "" {
			return nil, fmt.Errorf("%s:%d: want <path><TAB><unparsed lines, at least 1><TAB><construct>", path, n+1)
		}
		if _, dup := records[fields[0]]; dup {
			return nil, fmt.Errorf("%s:%d: %s is listed twice", path, n+1, fields[0])
		}
		records[fields[0]] = partialRecord{lines: lines, construct: fields[2]}
	}
	return records, nil
}

// parseRatchet is the findings for files mostly outside the parse tree: more than half of the
// lines unparsed fails unless the file is listed, at no more than its record. A record above
// the truth, or a row for a file that parses again, fails too, so the allowlist only shrinks.
func (g *gate) parseRatchet(partial map[string][]lineRange) (map[string][]finding, error) {
	records, err := g.loadPartialAllowlist()
	if err != nil {
		return nil, err
	}
	found := map[string][]finding{}
	add := func(file string, line int, text string) {
		found[file] = append(found[file], finding{rule: ruleParseRatchet, line: line, text: text})
	}
	mostly := map[string]bool{}
	for _, file := range slices.Sorted(maps.Keys(partial)) {
		gap := unionOf(partial[file])
		total, err := g.lineTotal(file)
		if err != nil {
			return nil, err
		}
		if gap.covered*2 <= total {
			continue
		}
		mostly[file] = true
		where := fmt.Sprintf("%d of %d lines outside the parse tree", gap.covered, total)
		rec, listed := records[file]
		switch {
		case !listed:
			add(file, gap.first, where+"; not on "+partialAllowlistFile+" (rewrite the construct semgrep stops at, or list it with that construct)")
		case gap.covered > rec.lines:
			add(file, gap.first, fmt.Sprintf("%s: grew past its record (%d > %d) in %s", where, gap.covered, rec.lines, partialAllowlistFile))
		case gap.covered < rec.lines:
			add(file, gap.first, fmt.Sprintf("%s: under its record (%d < %d); lower the record in %s", where, gap.covered, rec.lines, partialAllowlistFile))
		}
	}
	for _, file := range slices.Sorted(maps.Keys(records)) {
		if !mostly[file] {
			add(file, 1, "is no longer mostly outside the parse tree: remove its line from "+partialAllowlistFile)
		}
	}
	return found, nil
}
