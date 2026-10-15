package gates

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var (
	passedCount = regexp.MustCompile(`passed:[[:space:]]+([0-9]+)`)
	failedCount = regexp.MustCompile(`failed:[[:space:]]+([0-9]+)`)
)

// The three reasons a suite that exited 0 still has not passed. A caller tells them apart with
// errors.Is and prints the error's text after its own prefix.
var (
	// ErrNoCount: no line holds "passed: <digits>".
	ErrNoCount = errors.New("printed no 'passed: <n>' line, so it proved nothing")
	// ErrZeroCases: the count is zero, so no assertion ran.
	ErrZeroCases = errors.New("counted 0 passed cases ('passed: 0'), so it proved nothing")
	// ErrFailedCases: the count line itself reports failures.
	ErrFailedCases = errors.New("its count line reports failed cases")
)

// CaseCount turns a suite's output into the text of its stage line: the number from the last
// line holding "passed: <digits>", the last such match on that line, as "passed: N". It is the
// one reader qa-all and qa-changed share, and a suite that exited 0 passes only if it answers a
// count: no count line (ErrNoCount, the bash answered the bare word "passed" there), a count of
// zero (ErrZeroCases: case discovery matched nothing) and a "failed: <m>" with m above zero on
// that line (ErrFailedCases: a summary that forgot its exit 1) each have proved nothing or
// failed. A wrong number is the one thing this must never give, which is why it is the last
// match of the last line and nothing broader.
func CaseCount(capture string) (string, error) {
	last := ""
	for line := range strings.SplitSeq(capture, "\n") {
		if passedCount.MatchString(line) {
			last = line
		}
	}
	matches := passedCount.FindAllStringSubmatch(last, -1)
	if len(matches) == 0 {
		return "", ErrNoCount
	}
	count := matches[len(matches)-1][1]
	for _, m := range failedCount.FindAllStringSubmatch(last, -1) {
		if strings.Trim(m[1], "0") != "" {
			return "", fmt.Errorf("%w: %s failed", ErrFailedCases, m[1])
		}
	}
	if strings.Trim(count, "0") == "" {
		return "", ErrZeroCases
	}
	return "passed: " + count, nil
}

// Detail is the text a gate's pattern matches on the last line of the capture that matches it,
// or ("", false) when nothing does. The pattern is a POSIX ERE compiled leftmost-longest, as
// the bash reader (awk, then bash =~) read it.
func Detail(capture string, re *regexp.Regexp) (string, bool) {
	last := ""
	for line := range strings.SplitSeq(capture, "\n") {
		if re.MatchString(line) {
			last = line
		}
	}
	if last == "" {
		return "", false
	}
	return re.FindString(last), true
}

// Summarise is the text of a gate's stage line from its captured output, or the reason it cannot
// give one: qa-all and qa-changed both judge a script they ran by it, so neither can be more
// lenient than the other. A gate's every reader must find its line (a count reader a count, a
// detail reader its pattern); a drift gate must print its own stage line; a helpers or stage
// gate's verdict is not in its output, so it is refused rather than passed on nothing. The
// errors keep ErrNoCount, ErrZeroCases and ErrFailedCases reachable with errors.Is.
func Summarise(g Gate, capture string) (string, error) {
	switch g.Kind {
	case KindDrift:
		return driftLine(g, capture)
	case KindHelpers:
		return "", errors.New("its counts travel in a file, not in its output, so its output cannot be judged here")
	case KindStage:
		return "", errors.New("its verdict is a JSON report, not its output, so its output cannot be judged here")
	case KindGate:
	}
	parts := make([]string, 0, len(g.Readers))
	for _, r := range g.Readers {
		if r.Detail == nil {
			got, err := CaseCount(capture)
			if err != nil {
				return "", fmt.Errorf("exited 0 but %w", err)
			}
			parts = append(parts, got)
			continue
		}
		got, ok := Detail(capture, r.Detail)
		if !ok {
			return "", errors.New("ran, but its summary line was not found: " + r.Detail.String())
		}
		parts = append(parts, got)
	}
	return strings.Join(parts, "; "), nil
}

// driftLine is the text after the drift gate's own "<mark> <name>: " stage line.
func driftLine(g Gate, capture string) (string, error) {
	line := regexp.MustCompile(`(?m)^[✓⚠] ` + regexp.QuoteMeta(g.Name) + `: (.*)$`)
	m := line.FindStringSubmatch(capture)
	if m == nil {
		return "", errors.New("ran, but printed no " + g.Name + " stage line")
	}
	return m[1], nil
}

// countKeys are the five keys of the helper-tests counts file, which the runner
// (helpers/qa_environment/unittest_counts.py) writes from unittest's TestResult. The first four
// are numbers.
var countKeys = []string{"tests", "skipped", "modules", "tracked", "token"}

var digits = regexp.MustCompile(`^[0-9]+$`)

// HelperCounts reads the helper-tests counts file and returns its stage-line summary,
// "Ran 1464 tests in 64 modules (64 tracked), 1 skipped". It refuses, with a reason, a missing
// or empty file, an unknown or repeated key, a token that is not the one asked for, and a
// missing or non-numeric count: each is a bug rather than a state a run can reach, and answering
// "0 skipped" for any of them would make an unreadable file look like a clean machine. The
// token detects a file written by something that never read this run's argv (a stale or
// concurrent run); it is not a lock against a test that reads argv, and write ordering is what
// defeats a clobber from inside the suite.
func HelperCounts(path, wantToken string) (string, error) {
	n, err := HelperRun(path, wantToken)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Ran %d %s in %d %s (%d tracked), %d skipped",
		n.Tests, plural(n.Tests, "test", "tests"),
		n.Modules, plural(n.Modules, "module", "modules"),
		n.Tracked, n.Skipped), nil
}

// HelperNumbers are the four numbers of a helper-tests counts file.
type HelperNumbers struct {
	Tests, Skipped, Modules, Tracked int
}

// HelperRun reads the counts file under the refusals of HelperCounts and returns the numbers as
// integers; a number too large for an int is refused too.
func HelperRun(path, wantToken string) (HelperNumbers, error) {
	values, err := readCounts(path, wantToken)
	if err != nil {
		return HelperNumbers{}, err
	}
	var n HelperNumbers
	for key, into := range map[string]*int{"tests": &n.Tests, "skipped": &n.Skipped, "modules": &n.Modules, "tracked": &n.Tracked} {
		v, convErr := strconv.Atoi(values[key])
		if convErr != nil {
			return HelperNumbers{}, fmt.Errorf("%s=%s is not a number this tool can hold in %s", key, values[key], path)
		}
		*into = v
	}
	return n, nil
}

// readCounts is the validated key/value map of a counts file.
func readCounts(path, wantToken string) (map[string]string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no counts file at %s", path)
		}
		return nil, fmt.Errorf("counts file %s: %w", path, err)
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("%s is empty: the runner did not write it", path)
	}
	values, err := parseCounts(string(body), path)
	if err != nil {
		return nil, err
	}
	// Before the digits: if the file is not the one this run produced, its numbers describe
	// something else and validating them would only lend them credibility.
	if values["token"] != wantToken {
		return nil, fmt.Errorf("%s carries token %q, expected %q: the file was written by something other than the run that asked for it", path, values["token"], wantToken)
	}
	for _, key := range countKeys[:4] {
		if !digits.MatchString(values[key]) {
			return nil, fmt.Errorf("%s= is missing or not a number in %s", key, path)
		}
	}
	return values, nil
}

// parseCounts splits the file into its keys. A key is split at the first "="; a line with an
// empty key is skipped; repeats are tracked by having seen the key, not by its value being
// non-empty (tests= then tests=5 is a repeat).
func parseCounts(body, path string) (map[string]string, error) {
	values := map[string]string{}
	for line := range strings.SplitSeq(body, "\n") {
		parts := strings.SplitN(line, "=", 2)
		key, value := parts[0], ""
		if len(parts) == 2 {
			value = parts[1]
		}
		if key == "" {
			continue
		}
		if !slices.Contains(countKeys, key) {
			return nil, fmt.Errorf("unexpected key %q in %s", key, path)
		}
		if _, seen := values[key]; seen {
			return nil, fmt.Errorf("duplicate %s= in %s", key, path)
		}
		values[key] = value
	}
	return values, nil
}

// plural is the singular only for exactly one.
func plural(count int, singular, many string) string {
	if count == 1 {
		return singular
	}
	return many
}
