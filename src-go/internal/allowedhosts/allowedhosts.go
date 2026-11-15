// Package allowedhosts is the ccy launcher's hostname rule, check_allowed_hostname in
// lib/common.bash: a checkout's .claude/ccy/allowed-hostnames says which hosts may run ccy in
// it. ccy-sessions asks it so that `restore --check` refuses a record whose launch the rule
// would stop (fedora-desktop#284).
//
// Intended behaviour, each held by a test (bash, where it differs, is noted):
//
//   - H1. No file, or an entry that is not a regular file, is no restriction.
//   - H2. A pattern is a line with everything from its first # removed and every whitespace
//     character removed; blank results are dropped. A last line with no newline is read, as
//     the bash reads it.
//   - H3. A file with no pattern is no restriction.
//   - H4. The patterns are tried in file order: `never` refuses (whatever follows), a pattern
//     that matches the host allows (whatever follows), and a host no pattern matches is refused.
//   - H5. A file that is there and cannot be read is ErrUnreadable: the bash stops the launch on
//     a file it cannot open (fedora-desktop#293). An entry that cannot be looked at (a symlink
//     loop) is another error: the bash's `-f` test is false for it, so the launch goes ahead.
//   - H6. A pattern matches as bash's case statement matches an unquoted pattern (Match).
package allowedhosts

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Rel is the rule's file, from the checkout's root.
const Rel = ".claude/ccy/allowed-hostnames"

// ErrUnreadable is a regular rule file that could not be read: the launcher stops on it. An
// entry that could not even be looked at is another error, which the launcher's `-f` test
// reads as no file.
var ErrUnreadable = errors.New("the allowed-hostnames file could not be read")

// never is the pattern that refuses every host.
const never = "never"

// Verdict is the rule's answer for one host: allowed, or refused by never or by no match. The
// patterns are what the file said.
type Verdict struct {
	Allowed  bool
	Never    bool
	Patterns []string
}

// Read is the checkout's patterns, and whether the file is there (H1, H2, H5).
func Read(dir string) ([]string, bool, error) {
	path := filepath.Join(dir, Rel)
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("%s could not be looked at: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, false, nil
	}
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, false, fmt.Errorf("%w: %s: %w", ErrUnreadable, path, err)
	}
	var patterns []string
	for line := range strings.SplitSeq(string(raw), "\n") {
		if hash := strings.IndexByte(line, '#'); hash >= 0 {
			line = line[:hash]
		}
		line = strings.Map(func(r rune) rune {
			if strings.ContainsRune(" \t\n\v\f\r", r) {
				return -1
			}
			return r
		}, line)
		if line != "" {
			patterns = append(patterns, line)
		}
	}
	return patterns, true, nil
}

// Judge is the rule of the checkout at dir for host (H1-H5).
func Judge(dir, host string) (Verdict, error) {
	patterns, present, err := Read(dir)
	if err != nil {
		return Verdict{}, err
	}
	if !present || len(patterns) == 0 {
		return Verdict{Allowed: true}, nil
	}
	for _, p := range patterns {
		if p == never {
			return Verdict{Never: true, Patterns: patterns}, nil
		}
		if Match(p, host) {
			return Verdict{Allowed: true, Patterns: patterns}, nil
		}
	}
	return Verdict{Patterns: patterns}, nil
}

// Match is bash's case matching of name against an unquoted pattern (H6): * any run, ? any one
// character, [...] a bracket expression (ranges, ! or ^ to negate, ] first taken literally,
// POSIX [:class:] names, an unknown class matching nothing), a backslash quoting the next
// character, and a [ with no closing ] standing for itself.
func Match(pattern, name string) bool {
	return match([]rune(pattern), []rune(name))
}

func match(p, s []rune) bool {
	for len(p) > 0 {
		if p[0] == '*' {
			for i := 0; i <= len(s); i++ {
				if match(p[1:], s[i:]) {
					return true
				}
			}
			return false
		}
		n, ok := step(p, s)
		if !ok {
			return false
		}
		p, s = p[n:], s[1:]
	}
	return len(s) == 0
}

// step matches the pattern element at the start of p against s's first character: how many
// pattern characters it took, and whether it matched.
func step(p, s []rune) (int, bool) {
	if len(s) == 0 {
		return 0, false
	}
	switch p[0] {
	case '?':
		return 1, true
	case '[':
		if n, closed, matched := bracket(p, s[0]); closed {
			return n, matched
		}
	case '\\':
		if len(p) > 1 {
			return 2, p[1] == s[0]
		}
	}
	return 1, p[0] == s[0]
}

// bracket reads the bracket expression at the start of p: how many pattern characters it
// takes, false when it has no closing ], and whether c matches it.
func bracket(p []rune, c rune) (int, bool, bool) {
	i := 1
	negate := i < len(p) && (p[i] == '!' || p[i] == '^')
	if negate {
		i++
	}
	matched, valid := false, true
	for first := true; i < len(p); first = false {
		if p[i] == ']' && !first {
			return i + 1, true, valid && matched != negate
		}
		n, in, known := bracketItem(p[i:], c)
		matched, valid, i = matched || in, valid && known, i+n
	}
	return 0, false, false
}

// bracketItem reads one item of a bracket expression (a class, a range or one character): how
// many pattern characters it takes, whether c is in it, and whether it is one bash knows.
func bracketItem(p []rune, c rune) (int, bool, bool) {
	if len(p) > 1 && p[0] == '[' && p[1] == ':' {
		if end := closeClass(p, 2); end >= 0 {
			in, known := inClass(string(p[2:end]), c)
			return end + 2, in, known
		}
	}
	lo, n := literal(p)
	if n+1 < len(p) && p[n] == '-' && p[n+1] != ']' {
		hi, m := literal(p[n+1:])
		return n + 1 + m, c >= lo && c <= hi, true
	}
	return n, c == lo, true
}

// literal is the character at the start of p, a backslash quoting it, and its length.
func literal(p []rune) (rune, int) {
	if p[0] == '\\' && len(p) > 1 {
		return p[1], 2
	}
	return p[0], 1
}

// closeClass is the index of the ":]" that ends the class name starting at from, or -1.
func closeClass(p []rune, from int) int {
	for j := from; j+1 < len(p); j++ {
		if p[j] == ':' && p[j+1] == ']' {
			return j
		}
		if !unicode.IsLower(p[j]) {
			return -1
		}
	}
	return -1
}

// classes are the POSIX character classes bash knows.
var classes = map[string]func(rune) bool{
	"alnum": func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) },
	"alpha": unicode.IsLetter, "blank": func(r rune) bool { return r == ' ' || r == '\t' },
	"cntrl": unicode.IsControl, "digit": func(r rune) bool { return r >= '0' && r <= '9' },
	"graph": func(r rune) bool { return unicode.IsGraphic(r) && !unicode.IsSpace(r) },
	"lower": unicode.IsLower, "print": unicode.IsPrint, "punct": unicode.IsPunct,
	"space": unicode.IsSpace, "upper": unicode.IsUpper,
	"xdigit": func(r rune) bool { return strings.ContainsRune("0123456789abcdefABCDEF", r) },
}

// inClass says whether c is in the named class, and whether the class is one bash knows.
func inClass(name string, c rune) (bool, bool) {
	is, known := classes[name]
	return known && is(c), known
}
