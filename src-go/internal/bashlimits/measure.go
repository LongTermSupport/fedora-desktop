// Package bashlimits is the QA gate behind the repository's rule "tooling is Go; bash only for
// very small helpers" (CLAUDE.md, src-go/CLAUDE.md). It measures every tracked bash file
// against four limits, exempts the few kinds of file where bash is the right tool, and holds
// the files already over a limit to a ratchet allowlist that may only shrink.
//
// The measurements are line-and-regexp heuristics, not a bash parser. Their stated reading:
//
//   - a code line is any line that is not blank and whose first non-blank character is not
//     "#" (so the shebang is a comment); heredoc body lines count like any other line;
//   - the function, associative-array and interactive-UI checks look at each line with its
//     trailing comment removed (quotes respected), and skip heredoc bodies, which are data;
//   - an associative array is a declare, local, typeset or readonly with an -A flag
//     (-A, -gA, -Ar, "-g -A") at the start of a command;
//   - interactive UI is gum, fzf, whiptail or dialog at the start of a command (after |, ;,
//     &, (, $(, a backtick, then/do/else, and sudo, exec, env and VAR=value prefixes), or a
//     "select NAME in" loop. A word inside a quoted string, "command -v fzf" and a case
//     pattern are not invocations.
package bashlimits

import (
	"fmt"
	"regexp"
	"strings"
)

// The limits a bash file must satisfy.
const (
	MaxCodeLines = 60
	MaxFuncs     = 4
)

// Measurement is what the four limits are checked against.
type Measurement struct {
	Code  int  // non-blank, non-comment lines
	Funcs int  // function definitions
	Assoc bool // uses an associative array
	UI    bool // runs an interactive UI
}

// Flag names, as written in the allowlist.
const (
	FlagAssoc = "assoc"
	FlagUI    = "ui"
)

// Flags lists the boolean findings by their allowlist names, in a fixed order.
func (m Measurement) Flags() []string {
	var out []string
	if m.Assoc {
		out = append(out, FlagAssoc)
	}
	if m.UI {
		out = append(out, FlagUI)
	}
	return out
}

// Breaches describes each limit the measurement exceeds; empty when the file is within all.
func (m Measurement) Breaches() []string {
	var out []string
	if m.Code > MaxCodeLines {
		out = append(out, fmt.Sprintf("%d code lines (limit %d)", m.Code, MaxCodeLines))
	}
	if m.Funcs > MaxFuncs {
		out = append(out, fmt.Sprintf("%d functions (limit %d)", m.Funcs, MaxFuncs))
	}
	if m.Assoc {
		out = append(out, "associative array (declare -A / local -A): a data structure means a program")
	}
	if m.UI {
		out = append(out, "interactive UI (gum, fzf, whiptail, dialog, select)")
	}
	return out
}

// WithinLimits reports whether the file breaks none of the limits.
func (m Measurement) WithinLimits() bool {
	return len(m.Breaches()) == 0
}

// Command position: the start of a line or what introduces a command, then any wrapper words.
const (
	cmdStart = "(?:^|[|;&(`{!]|\\$\\(|\\b(?:then|do|else|if|elif|while|until)\\b)\\s*"
	wrappers = "(?:(?:\\w+=\\S*|sudo|exec|command|env|time|nohup)\\s+)*"
)

var (
	funcRe  = regexp.MustCompile(`^\s*(?:function\s+[A-Za-z_][\w:.@+-]*|[A-Za-z_][\w:.@+-]*\s*\(\s*\))`)
	assocRe = regexp.MustCompile(cmdStart + wrappers +
		`(?:declare|local|typeset|readonly)\s+(?:-[A-Za-z]+\s+)*-[A-Za-z]*A[A-Za-z]*\b`)
	uiRe = regexp.MustCompile(cmdStart + wrappers +
		`(?:gum|fzf|whiptail|dialog)(?:\s|$|[|;&)<>])`)
	selectRe  = regexp.MustCompile(`^\s*select\s+[A-Za-z_]\w*(?:\s+in\b|\s*;|\s*$)`)
	heredocRe = regexp.MustCompile(`(?:^|[^<])<<-?\s*(?:'([^']+)'|"([^"]+)"|\\?([A-Za-z_][A-Za-z0-9_]*))`)
)

// Measure counts a bash file's code lines, functions, associative arrays and UI invocations.
func Measure(src []byte) Measurement {
	var m Measurement
	heredoc := "" // the delimiter of the heredoc body being read, or ""
	for line := range strings.SplitSeq(string(src), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			m.Code++
		}
		if heredoc != "" {
			if trimmed == heredoc {
				heredoc = ""
			}
			continue
		}
		code := stripComment(line)
		if funcRe.MatchString(code) {
			m.Funcs++
		}
		m.Assoc = m.Assoc || assocRe.MatchString(code)
		m.UI = m.UI || uiRe.MatchString(code) || selectRe.MatchString(code)
		heredoc = openedHeredoc(code)
	}
	return m
}

// openedHeredoc is the delimiter of a heredoc the line opens, or "". A here-string (<<<) and an
// arithmetic shift inside (( )) open none.
func openedHeredoc(code string) string {
	if strings.Contains(code, "((") {
		return ""
	}
	match := heredocRe.FindStringSubmatch(code)
	if match == nil {
		return ""
	}
	for _, delim := range match[1:] {
		if delim != "" {
			return delim
		}
	}
	return ""
}

// stripComment drops a trailing comment: a "#" outside quotes that starts a word.
func stripComment(line string) string {
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\\':
			if quote != '\'' {
				i++ // the escaped character is never a quote or a comment start
			}
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
			return line[:i]
		}
	}
	return line
}
