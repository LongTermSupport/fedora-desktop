package teamchatseparate

import (
	"fmt"
	"slices"
	"strings"
)

// Allowed is one row of the allowlist: a remaining v1 reference in a v2 file, outside the two
// named homes. A hit on a line of Path is allowed when an occurrence of Literal on that line
// covers it.
type Allowed struct {
	Line    int
	Path    string
	Literal string
	Reason  string
}

// ParseAllowed reads the allowlist: path, literal and reason, tab separated; # starts a comment.
func ParseAllowed(data string) ([]Allowed, error) {
	var rows []Allowed
	for i, line := range strings.Split(data, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			return nil, fmt.Errorf("line %d: want 3 tab-separated fields (path, literal, reason), got %d", i+1, len(fields))
		}
		if slices.ContainsFunc(fields, func(f string) bool { return strings.TrimSpace(f) == "" }) {
			return nil, fmt.Errorf("line %d: an empty field", i+1)
		}
		row := Allowed{Line: i + 1, Path: fields[0], Literal: fields[1], Reason: fields[2]}
		if slices.ContainsFunc(rows, func(r Allowed) bool { return r.Path == row.Path && r.Literal == row.Literal }) {
			return nil, fmt.Errorf("line %d: %s %s is listed twice", i+1, row.Path, row.Literal)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// covers is whether an occurrence of the row's literal on the line covers the hit.
func (a Allowed) covers(line string, h Hit) bool {
	for i := 0; ; {
		j := strings.Index(line[i:], a.Literal)
		if j < 0 {
			return false
		}
		start := i + j
		if start <= h.Start && h.End <= start+len(a.Literal) {
			return true
		}
		i = start + 1
	}
}
