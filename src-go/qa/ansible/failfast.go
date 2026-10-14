package ansible

import "regexp"

// One spelling list for every directive, and one templated form: YAML 1.1 reads yes/no/on/off as
// booleans exactly as it reads true/false, and a templated value is as statically unverifiable
// under one key as under another. The trailing \b stops `no` matching inside `not`.
const (
	falsey    = `(false|no|off)\b`
	truthy    = `(true|yes|on)\b`
	templated = `"?[{][{]`
)

// failFastPattern matches a line that hides an error: a failed_when that is always false, an
// ignore_errors or ignore_unreachable that is true or templated.
var failFastPattern = regexp.MustCompile(`(?i)failed_when:[[:space:]]+` + falsey +
	`|ignore_errors:[[:space:]]+` + truthy +
	`|ignore_errors:[[:space:]]+` + templated +
	`|ignore_unreachable:[[:space:]]+` + truthy +
	`|ignore_unreachable:[[:space:]]+` + templated)

// failFastOK is the justification a line may carry, anywhere on it and in any case.
var failFastOK = regexp.MustCompile(`(?i)FAIL-FAST-OK`)

// stripComment cuts s at the first # that starts it or follows a space or tab. A # inside a
// quoted string is not told apart, which is harmless: a line quoting a # beside a fail-fast
// directive is not a real one.
func stripComment(s string) string {
	for i := range len(s) {
		if s[i] == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t') {
			return s[:i]
		}
	}
	return s
}

// failFastViolations keeps the hits that are real violations: not justified by a FAIL-FAST-OK
// on the line's text, and still matching once a comment is cut (a comment that documents a
// removed directive is inert).
func failFastViolations(hits []hit) []string {
	var violations []string
	for _, h := range hits {
		if failFastOK.MatchString(h.text) {
			continue
		}
		if !failFastPattern.MatchString(stripComment(h.text)) {
			continue
		}
		violations = append(violations, h.String())
	}
	return violations
}
