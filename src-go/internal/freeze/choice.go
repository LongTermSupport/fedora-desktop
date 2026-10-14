package freeze

import (
	"strconv"
	"strings"
)

// ParseMemberChoice resolves a drill-down reply such as "2,4" against the displayed rows. Row 1
// is the "all" row, so row k addresses rows[k-2]. Tokens split on commas and whitespace and every
// one must be a number in 2..len(rows)+1: one bad token rejects the whole reply, because a
// partial answer would act on some of what was typed and say nothing about the rest. The result
// is in typed order with repeats dropped (the first wins).
func ParseMemberChoice(reply string, rows []string) ([]string, bool) {
	tokens := strings.FieldsFunc(reply, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
	if len(tokens) == 0 {
		return nil, false
	}
	var picked []string
	seen := make(map[string]bool, len(tokens))
	for _, token := range tokens {
		n, ok := parseNumber(token)
		if !ok || n < 2 || n > len(rows)+1 {
			return nil, false
		}
		name := rows[n-2]
		if !seen[name] {
			seen[name] = true
			picked = append(picked, name)
		}
	}
	return picked, true
}

// parseNumber accepts only a run of digits (no sign, no spaces), and nothing too big to be a row.
func parseNumber(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}
