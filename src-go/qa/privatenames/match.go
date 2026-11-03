package privatenames

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// edge is what may not touch a name on either side: a letter, digit or underscore. A hyphen, a
// dot, a slash or a quote may, so a name inside a compound, a path or a handle is still found,
// while a longer unrelated word that merely contains it is not.
const (
	leftEdge  = `(?:^|[^\p{L}\p{N}_])`
	rightEdge = `(?:[^\p{L}\p{N}_]|$)`
)

type entry struct {
	source string
	re     *regexp.Regexp
}

// Matcher finds any term in a line: case-insensitive, on word boundaries.
type Matcher struct {
	whole   *regexp.Regexp
	entries []entry
}

// NewMatcher compiles the terms. An empty list is an error: a matcher that can find nothing
// would read as a pass.
func NewMatcher(terms []Term) (*Matcher, error) {
	if len(terms) == 0 {
		return nil, errors.New("no private terms to match")
	}
	m := &Matcher{}
	quoted := make([]string, 0, len(terms))
	for _, t := range terms {
		q := regexp.QuoteMeta(t.Text)
		re, err := regexp.Compile(`(?i)` + leftEdge + `(` + q + `)` + rightEdge)
		if err != nil {
			return nil, fmt.Errorf("a private term does not compile: %w", err)
		}
		m.entries = append(m.entries, entry{source: t.Source, re: re})
		quoted = append(quoted, q)
	}
	whole, err := regexp.Compile(`(?i)` + leftEdge + `(?:` + strings.Join(quoted, "|") + `)` + rightEdge)
	if err != nil {
		return nil, fmt.Errorf("the private terms do not compile together: %w", err)
	}
	m.whole = whole
	return m, nil
}

// Mask is s with every term replaced by <private>, so a place can be named without the name.
func (m *Matcher) Mask(s string) string {
	for range len(s) {
		replaced := false
		for _, e := range m.entries {
			if loc := e.re.FindStringSubmatchIndex(s); loc != nil {
				s = s[:loc[2]] + "<private>" + s[loc[3]:]
				replaced = true
			}
		}
		if !replaced {
			break
		}
	}
	return s
}

// Source is the source label of the term at a position.
func (m *Matcher) Source(i int) string { return m.entries[i].source }

// Count is the number of terms.
func (m *Matcher) Count() int { return len(m.entries) }

// Find is the source label of the first term in s, and whether any is there.
func (m *Matcher) Find(s string) (string, bool) {
	i, ok := m.Index(s)
	if !ok {
		return "", false
	}
	return m.entries[i].source, true
}

// Index is the position of the first term in s, in the order the terms were given (the list
// file's order), and whether any is there. A position names a term without revealing it.
func (m *Matcher) Index(s string) (int, bool) {
	if !m.whole.MatchString(s) {
		return 0, false
	}
	for i, e := range m.entries {
		if e.re.MatchString(s) {
			return i, true
		}
	}
	return 0, false
}

// termOf is the position of the first term in s; only called on a string Find matched.
func (m *Matcher) termOf(s string) int {
	i, _ := m.Index(s)
	return i
}
