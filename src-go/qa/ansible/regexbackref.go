package ansible

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The regex-backref-escape check (CLAUDE/QA.md, "regex-backref-escape"). A backreference in a
// string argument of regex_search, regex_replace or regex_findall (\1, \g<name>) must reach the
// filter as exactly one backslash. Two layers may have removed backslashes by then:
//
//   - YAML, in a double-quoted scalar only: \\ becomes \. The YAML library decodes the scalar, so
//     the check reads what Jinja reads, whatever the style.
//   - Jinja, outside {{ }} only. Ansible's lexer doubles the backslashes of every string constant
//     inside {{ }} in task data, which cancels Jinja's own escape processing there; conditionals
//     (when, changed_when, failed_when, until, that, evaluated as bare expressions), {% %}
//     statements and .j2 templates keep it: \\ becomes \, and \1 becomes the character 0x01.
//
// A run of k backslashes before a digit or g< therefore reaches the filter as k in {{ }}, and as
// k/2 (k even) or as a control character (k odd) in an unescaping context. A group argument of
// regex_search must receive exactly one (the filter reads it with \\(\d+) or \\g<...>, and two
// make it fail with "'NoneType' object has no attribute 'group'"); a pattern or a replacement may
// receive any odd number (escaped backslashes, then the backreference), and an even number is a
// literal backslash where a backreference was meant (regex_replace writes "\1" and fails nothing).

// regexFilters are the filters whose string arguments the check reads, by their short name.
var regexFilters = map[string]bool{"regex_search": true, "regex_replace": true, "regex_findall": true}

// conditionalKeys are the task keys Ansible evaluates as a bare expression.
var conditionalKeys = map[string]bool{"when": true, "changed_when": true, "failed_when": true, "until": true, "that": true}

// backrefRun is a run of backslashes followed by a group digit or a named group.
var backrefRun = regexp.MustCompile(`\\+(?:[0-9]|g<)`)

// jinjaToken is one token of a Jinja expression: a string constant (the lexer's own pattern), a
// name (dotted names come as names and dots), or any other single character.
var jinjaToken = regexp.MustCompile(`^(?:'([^'\\]*(?:\\.[^'\\]*)*)'|"([^"\\]*(?:\\.[^"\\]*)*)"|[A-Za-z_][A-Za-z0-9_]*|\s+|.)`)

// evalContext is how Jinja treats a backslash in a string constant of an expression.
type evalContext int

const (
	ctxTemplate    evalContext = iota // {{ }} in task data: backslashes reach the filter as written
	ctxStatement                      // {% %}: Jinja unescapes
	ctxConditional                    // a bare conditional: Jinja unescapes
)

func (c evalContext) String() string {
	switch c {
	case ctxStatement:
		return "{% %}"
	case ctxConditional:
		return "conditional"
	case ctxTemplate:
	}
	return "{{ }}"
}

// scalarStyle names a scalar's YAML style as the message says it.
func scalarStyle(n *yaml.Node) string {
	switch {
	case n.Style&yaml.DoubleQuotedStyle != 0:
		return "double-quoted"
	case n.Style&yaml.SingleQuotedStyle != 0:
		return "single-quoted"
	case n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0:
		return "block"
	default:
		return "plain"
	}
}

// yamlErrorLine is the line a YAML parse error names.
var yamlErrorLine = regexp.MustCompile(`line ([0-9]+)`)

// regexBackrefs returns the regex-backref-escape findings of one YAML file, "<rel>:<line>: <why>",
// in document order. A file the YAML library cannot parse is a finding (Ansible cannot load it
// either), so the other checks still run over it.
func regexBackrefs(rel string, data []byte) []string {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var findings []string
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return findings
		}
		if err != nil {
			line := "1"
			if m := yamlErrorLine.FindStringSubmatch(err.Error()); m != nil {
				line = m[1]
			}
			return append(findings, rel+":"+line+": cannot be parsed as YAML, so its backreferences cannot be judged: "+err.Error())
		}
		visit(&doc, false, func(n *yaml.Node, conditional bool) {
			for _, why := range scalarFindings(n, conditional) {
				findings = append(findings, rel+":"+strconv.Itoa(n.Line)+": "+why)
			}
		})
	}
}

// visit calls each for every string scalar under n, saying whether it is a conditional's value.
func visit(n *yaml.Node, conditional bool, each func(*yaml.Node, bool)) {
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Tag == "!!str" {
			each(n, conditional)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			visit(n.Content[i+1], conditionalKeys[n.Content[i].Value], each)
		}
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range n.Content {
			visit(child, conditional && n.Kind == yaml.SequenceNode, each)
		}
	case yaml.AliasNode:
	}
}

// region is one stretch of Jinja expression text and the context it is evaluated in.
type region struct {
	text string
	ctx  evalContext
}

// scalarFindings checks every regex filter call in one scalar's value.
func scalarFindings(n *yaml.Node, conditional bool) []string {
	var regions []region
	trimmed := strings.TrimSpace(n.Value)
	allTemplate := (strings.HasPrefix(trimmed, "{{") || strings.HasPrefix(trimmed, "{%")) &&
		(strings.HasSuffix(trimmed, "}}") || strings.HasSuffix(trimmed, "%}"))
	if conditional && !allTemplate {
		regions = []region{{n.Value, ctxConditional}}
	} else {
		regions = templateRegions(n.Value)
	}
	var out []string
	for _, r := range regions {
		for _, arg := range regexArgs(r.text) {
			if why := judge(arg, r.ctx, scalarStyle(n)); why != "" {
				out = append(out, why)
			}
		}
	}
	return out
}

// templateRegions splits a templated string into its {{ }} and {% %} bodies. Comments and raw
// blocks are not expressions; a string constant may hold a closing delimiter; an unterminated
// body runs to the end of the text.
func templateRegions(text string) []region {
	var regions []region
	for i := 0; i < len(text); {
		open := strings.IndexByte(text[i:], '{')
		if open < 0 || i+open+1 >= len(text) {
			break
		}
		at := i + open
		switch text[at+1] {
		case '{', '%':
			ctx, closer := ctxTemplate, "}}"
			if text[at+1] == '%' {
				ctx, closer = ctxStatement, "%}"
			}
			body, next := expressionBody(text, at+2, closer)
			if ctx == ctxStatement && strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(body, "-"), "-")) == "raw" {
				next = skipRaw(text, next)
			} else {
				regions = append(regions, region{body, ctx})
			}
			i = next
		case '#':
			end := strings.Index(text[at+2:], "#}")
			if end < 0 {
				return regions
			}
			i = at + 2 + end + 2
		default:
			i = at + 1
		}
	}
	return regions
}

// expressionBody is the text from start up to closer outside a string constant, and the index after closer.
func expressionBody(text string, start int, closer string) (string, int) {
	for i := start; i < len(text); {
		if strings.HasPrefix(text[i:], closer) {
			return text[start:i], i + len(closer)
		}
		tok := jinjaToken.FindString(text[i:])
		i += len(tok)
	}
	return text[start:], len(text)
}

// endRaw ends a raw block, whitespace control included.
var endRaw = regexp.MustCompile(`\{%-?\s*endraw\s*-?%\}`)

// skipRaw is the index after the {% endraw %} that ends the raw block opened before from.
func skipRaw(text string, from int) int {
	loc := endRaw.FindStringIndex(text[from:])
	if loc == nil {
		return len(text)
	}
	return from + loc[1]
}

// regexArg is one string constant passed to a regex filter: the filter, whether it is a group
// argument of regex_search, the constant as written between its quotes, and its quote.
type regexArg struct {
	filter string
	group  bool
	body   string
	quote  byte
}

// call is a regex filter call being read: its filter, the depth of its parenthesis, which
// positional argument is being read, and the index of the pattern among them.
type call struct {
	filter  string
	depth   int
	arg     int
	pattern int
}

// regexArgs lists the string constants passed to regex filters in one expression, as the filter
// (`| regex_search(...)`, any namespace) or by name to map, select and the like (`map('regex_replace', ...)`).
func regexArgs(expr string) []regexArg {
	toks := tokens(expr)
	var args []regexArg
	var stack []call
	depth := 0
	for i, tok := range toks {
		switch {
		case tok == "(":
			depth++
			if c, ok := opensCall(toks, i, depth); ok {
				stack = append(stack, c)
			}
		case tok == ")":
			if len(stack) > 0 && stack[len(stack)-1].depth == depth {
				stack = stack[:len(stack)-1]
			}
			depth--
		case tok == "," && len(stack) > 0 && stack[len(stack)-1].depth == depth:
			stack[len(stack)-1].arg++
		case isString(tok) && len(stack) > 0 && stack[len(stack)-1].depth == depth:
			if c := stack[len(stack)-1]; c.arg >= c.pattern { // before the pattern: the filter's name, given to map or select
				group := c.filter == "regex_search" && c.arg > c.pattern
				args = append(args, regexArg{filter: c.filter, group: group, body: tok[1 : len(tok)-1], quote: tok[0]})
			}
		}
	}
	return args
}

// tokens is an expression's tokens, whitespace dropped.
func tokens(expr string) []string {
	var toks []string
	for rest := expr; rest != ""; {
		tok := jinjaToken.FindString(rest)
		rest = rest[len(tok):]
		if strings.TrimSpace(tok) != "" {
			toks = append(toks, tok)
		}
	}
	return toks
}

// isString is whether a token is a string constant.
func isString(tok string) bool { return tok[0] == '\'' || tok[0] == '"' }

// opensCall is whether the parenthesis at toks[i] opens a regex filter's arguments: a regex
// filter name before it, or a call whose first argument names one.
func opensCall(toks []string, i, depth int) (call, bool) {
	if i > 0 && regexFilters[toks[i-1]] {
		return call{filter: toks[i-1], depth: depth}, true
	}
	if i+1 < len(toks) && isString(toks[i+1]) {
		name := toks[i+1][1 : len(toks[i+1])-1]
		name = name[strings.LastIndex(name, ".")+1:]
		if regexFilters[name] {
			return call{filter: name, depth: depth, pattern: 1}, true
		}
	}
	return call{}, false
}

// judge reports what is wrong with one argument's backreferences, or "".
func judge(arg regexArg, ctx evalContext, style string) string {
	for _, loc := range backrefRun.FindAllStringIndex(arg.body, -1) {
		run := strings.Count(arg.body[loc[0]:loc[1]], `\`)
		received, control := run, false
		if ctx != ctxTemplate {
			received, control = run/2, run%2 == 1
		}
		ok := !control && (received == 1 || (!arg.group && received%2 == 1))
		if ok {
			continue
		}
		reached := fmt.Sprintf("%d backslashes, not 1", received)
		if control {
			reached = "a control character, not a backslash"
		}
		want := 1
		if ctx != ctxTemplate {
			want = 2
		}
		fixed := arg.body[:loc[0]] + strings.Repeat(`\`, want) + arg.body[loc[1]-len(strings.TrimLeft(arg.body[loc[0]:loc[1]], `\`)):]
		role := arg.filter + " argument"
		if arg.group {
			role = arg.filter + " group argument"
		}
		return fmt.Sprintf("%s %s reaches the filter as %s; in a %s scalar's %s write %s",
			role, asWritten(arg.body, arg.quote, style), reached, style, ctx, asWritten(fixed, arg.quote, style))
	}
	return ""
}

// asWritten quotes a Jinja string constant as it appears in the file: a double-quoted YAML
// scalar escapes each backslash and double quote, a single-quoted one doubles each single quote.
func asWritten(body string, quote byte, style string) string {
	text := string(quote) + body + string(quote)
	switch style {
	case "double-quoted":
		return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(text)
	case "single-quoted":
		return strings.ReplaceAll(text, "'", "''")
	}
	return text
}

// scanRegexBackrefs runs the check over the YAML files under dirs (roles/vendor skipped).
func scanRegexBackrefs(fsys fs.FS, dirs []string, f *Findings) error {
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
			for _, v := range regexBackrefs(rel, data) {
				f.Out = append(f.Out, "  ERROR (regex-backref-escape): "+v, regexBackrefAdvice)
				f.RegexBackref = append(f.RegexBackref, v)
			}
		}
	}
	return nil
}

// regexBackrefAdvice is the line printed under each finding.
const regexBackrefAdvice = "    Why and how to fix: CLAUDE/QA.md#regex-backref-escape"
