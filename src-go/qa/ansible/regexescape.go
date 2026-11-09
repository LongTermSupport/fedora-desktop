package ansible

import (
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The regex-class-escape check (CLAUDE/QA.md, "regex-class-escape"). A letter escape (\d, \s,
// \w, \S, \b ...) in a regex pattern given to regex_search, regex_replace or regex_findall, or
// to the tests match, search and regex, must reach the regex as one backslash and the letter
// (or as escaped backslashes, then one). The backslashes are counted as regexbackref.go counts
// them: YAML halves them in a double-quoted scalar only (the YAML library has decoded the
// value), and Jinja halves them everywhere but between {{ and }} in task data, keeping the
// backslash of an escape it does not know (\d, \s, \w) and turning \b into a backspace.
//
// So '\\d' between {{ }} in a plain, single-quoted or block scalar reaches the regex as a
// literal backslash and d: the regex silently matches something else (Plan 00235 Task 4.10).
// The same text in a bare conditional reaches it as \d, which is right.

// regexTests are the tests whose first argument is a regex pattern, by their short name.
var regexTests = map[string]bool{"match": true, "search": true, "regex": true}

// byNameCalls are the filters that take a filter or test by name, its arguments after it.
var byNameCalls = map[string]bool{"map": true, "select": true, "reject": true, "selectattr": true, "rejectattr": true}

// letterEscape is a run of backslashes followed by an ASCII letter.
var letterEscape = regexp.MustCompile(`\\+[A-Za-z]`)

// patternArg is one string constant of a regex pattern: what takes it ("regex_findall",
// "match test"), the constant as written between its quotes, and its quote.
type patternArg struct {
	taker string
	body  string
	quote byte
}

// patternCall is a call being read whose pattern is one of its positional arguments.
type patternCall struct {
	taker   string
	depth   int
	arg     int
	pattern int // -1 until a by-name call names a regex filter or test
}

// regexEscapes returns the regex-class-escape findings of one YAML file, "<rel>:<line>: <why>".
func regexEscapes(rel string, data []byte) []string {
	return scalarChecks(rel, data, "regex escapes", func(n *yaml.Node, bare evalContext) []string {
		var out []string
		for _, r := range expressionRegions(n, bare) {
			for _, arg := range patternArgs(r.text) {
				if why := judgeEscapes(arg, r.ctx, scalarStyle(n)); why != "" {
					out = append(out, why)
				}
			}
		}
		return out
	})
}

// patternArgs lists the string constants of the regex patterns in one expression: the first
// argument of a regex filter (`| regex_search(...)`) or test (`is match(...)`, `is not regex(...)`),
// in any namespace, or the argument after a regex filter or test given by name to map, select,
// reject, selectattr or rejectattr. A pattern built by `~` gives each of its constants.
func patternArgs(expr string) []patternArg {
	toks := tokens(expr)
	var args []patternArg
	var stack []patternCall
	depth := 0
	top := func() *patternCall {
		if len(stack) == 0 || stack[len(stack)-1].depth != depth {
			return nil
		}
		return &stack[len(stack)-1]
	}
	for i, tok := range toks {
		switch {
		case tok == "(":
			depth++
			if c, ok := opensPatternCall(toks, i, depth); ok {
				stack = append(stack, c)
			}
		case tok == ")":
			if top() != nil {
				stack = stack[:len(stack)-1]
			}
			depth--
		case tok == ",":
			if c := top(); c != nil {
				c.arg++
			}
		case isString(tok):
			if c := top(); c != nil {
				args = c.read(tok, args)
			}
		}
	}
	return args
}

// read takes one string constant given to the call: the name of the regex filter or test a
// by-name call applies, or a constant of its pattern, appended to args.
func (c *patternCall) read(tok string, args []patternArg) []patternArg {
	body := tok[1 : len(tok)-1]
	if c.pattern < 0 {
		if name := body[strings.LastIndex(body, ".")+1:]; regexFilters[name] || regexTests[name] {
			c.taker, c.pattern = takerName(name), c.arg+1
		}
		return args
	}
	if c.arg == c.pattern {
		args = append(args, patternArg{taker: c.taker, body: body, quote: tok[0]})
	}
	return args
}

// takerName is how a message names the filter or test that takes the pattern.
func takerName(name string) string {
	if regexTests[name] {
		return name + " test"
	}
	return name
}

// opensPatternCall is whether the parenthesis at toks[i] opens a call with a pattern argument.
func opensPatternCall(toks []string, i, depth int) (patternCall, bool) {
	if i == 0 {
		return patternCall{}, false
	}
	name := toks[i-1]
	switch {
	case regexFilters[name]:
		return patternCall{taker: name, depth: depth}, true
	case regexTests[name] && afterIs(toks, i-1):
		return patternCall{taker: takerName(name), depth: depth}, true
	case byNameCalls[name]:
		return patternCall{depth: depth, pattern: -1}, true
	}
	return patternCall{}, false
}

// afterIs is whether the (possibly dotted) name ending at toks[j] follows `is` or `is not`.
func afterIs(toks []string, j int) bool {
	for j >= 2 && toks[j-1] == "." {
		j -= 2
	}
	k := j - 1
	if k >= 0 && toks[k] == "not" {
		k--
	}
	return k >= 0 && toks[k] == "is"
}

// escapeReach is how one run of k backslashes before letter c reaches the regex in ctx: the
// number of backslashes left before the letter, and whether Jinja turned the last one and the
// letter into a backspace.
func escapeReach(k int, c byte, ctx evalContext) (int, bool) {
	if ctx == ctxTemplate {
		return k, false
	}
	// Jinja decodes with Python's unicode-escape: \\ is one backslash, \b a backspace (\a, \f,
	// \n, \r, \t, \v, \x, \u, \U and \N become the character the regex escape means), and an
	// unknown escape keeps its backslash.
	switch {
	case k%2 == 0:
		return k / 2, false
	case c == 'b':
		return k / 2, true
	case strings.IndexByte("afnrtvxuUN", c) >= 0:
		return 1, false
	}
	return k/2 + 1, false
}

// judgeEscapes reports the first letter escape of one constant that reaches the regex wrong,
// with the constant rewritten so every such escape reaches it right, or "".
func judgeEscapes(arg patternArg, ctx evalContext, style string) string {
	want := 1
	if ctx != ctxTemplate {
		want = 2
	}
	var why string
	var fixed strings.Builder
	last := 0
	for _, loc := range letterEscape.FindAllStringIndex(arg.body, -1) {
		k, c := loc[1]-loc[0]-1, arg.body[loc[1]-1]
		received, backspace := escapeReach(k, c, ctx)
		if !backspace && received%2 == 1 {
			continue
		}
		if why == "" {
			why = fmt.Sprintf("a literal backslash and %c (%d backslashes, not 1)", c, received)
			if backspace {
				why = `a backspace character, not \b`
			}
		}
		fixed.WriteString(arg.body[last:loc[0]] + strings.Repeat(`\`, want) + string(c))
		last = loc[1]
	}
	if why == "" {
		return ""
	}
	fixed.WriteString(arg.body[last:])
	return fmt.Sprintf("%s pattern %s reaches the regex as %s; in a %s scalar's %s write %s",
		arg.taker, asWritten(arg.body, arg.quote, style), why, style, ctx, asWritten(fixed.String(), arg.quote, style))
}

// scanRegexEscapes runs the check over the YAML files under dirs (roles/vendor skipped).
func scanRegexEscapes(fsys fs.FS, dirs []string, f *Findings) error {
	for _, dir := range dirs {
		files, err := yamlFiles(fsys, dir, true)
		if err != nil {
			return err
		}
		for _, rel := range files {
			data, err := fs.ReadFile(fsys, rel)
			if err != nil {
				return fmt.Errorf("a file could not be read: %w", err)
			}
			for _, v := range regexEscapes(rel, data) {
				f.Out = append(f.Out, "  ERROR (regex-class-escape): "+v, regexEscapeAdvice)
				f.RegexEscape = append(f.RegexEscape, v)
			}
		}
	}
	return nil
}

// regexEscapeAdvice is the line printed under each finding.
const regexEscapeAdvice = "    Why and how to fix: CLAUDE/QA.md#regex-class-escape"
