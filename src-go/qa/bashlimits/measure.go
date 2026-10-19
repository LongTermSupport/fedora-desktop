// Package bashlimits is the QA gate behind the repository's rule "tooling is Go; bash only for
// very small helpers" (CLAUDE.md, src-go/CLAUDE.md). It measures every bash file git knows
// against the limits below, exempts the few kinds of file where bash is not the thing being
// judged (test suites, test fixtures and testdata, plan-folder scaffolding, vendored and
// daemon-owned trees: see Exemptions; the limits are for tooling, and a tool stays measured
// even when it is named for tests), and holds the files already over a limit to a ratchet allowlist that may only shrink.
//
// # The measure (MeasurerVersion)
//
// A file is parsed with mvdan.cc/sh/v3 as bash and measured on its syntax tree, so quoting,
// heredocs and several statements on a line are read as bash reads them:
//
//   - Code: the lines that are not blank and are not a comment on a line of their own (the
//     shebang is a comment). A "#" line inside a heredoc or a multi-line string is text, so
//     it counts; a line continuation (backslash) counts as two lines.
//   - Stmts: every statement, however many share a line, so ";" cannot pack a program into
//     few lines. Statements nested in functions, loops, branches and "$( )" all count.
//   - Funcs: every function definition, in any form and wherever it appears.
//   - Assoc: an associative array: declare, local, typeset or readonly with an -A flag in any
//     spelling (-A, -gA, -rA, "-A"), also behind builtin, command or a backslash.
//   - UI: gum, fzf, fzf-tmux, sk, fzy, peco, whiptail, dialog, zenity, yad, kdialog, rofi or
//     dmenu invoked as a command (by path, quoted, backslashed, or behind a wrapper such as
//     sudo, env, timeout, xargs, nice, command, exec), or a "select" loop. The word in a case
//     pattern, an argument, a string or "command -v" is not an invocation.
//   - Data: data parsing past one "jq" call: more than one jq invocation, or python with -c,
//     "-" or a heredoc as its program, or any yq. One "jq -r .field" is fine.
//
// What it cannot see, and accepts: a command named by a variable (the "$PICKER" case), code
// built and run with eval, and a bash -c script held in a string. Ansible "shell:" blocks are
// not bash files; qa-ansible owns them. A file that does not parse as bash is an error, never
// skipped: shellcheck would reject it anyway. Jinja templates are measured with their tags set
// aside (MeasureTemplate).
//
// The numbers are in the constants below, and CLAUDE.md states them in prose. When the
// measure changes, MeasurerVersion rises and the allowlist is rewritten once by --remeasure;
// that is the only time a record may go up.
package bashlimits

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// The limits a bash file must satisfy, and the version of the measure that applies them.
const (
	MaxCodeLines = 60
	MaxFuncs     = 4
	MaxStmts     = 90

	// MeasurerVersion is stamped in the allowlist. Raise it when a change to this file makes
	// any file measure differently; the allowlist is then rewritten with --remeasure.
	MeasurerVersion = 2
)

// Measurement is what the limits are checked against.
type Measurement struct {
	Code  int  // non-blank lines that are not whole-line comments
	Funcs int  // function definitions
	Stmts int  // statements
	Assoc bool // uses an associative array
	UI    bool // runs an interactive UI
	Data  bool // parses data beyond one jq call
}

// Flag names, as written in the allowlist.
const (
	FlagAssoc = "assoc"
	FlagUI    = "ui"
	FlagData  = "data"
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
	if m.Data {
		out = append(out, FlagData)
	}
	return out
}

// Breaches describes each limit the measurement exceeds; empty when the file is within all.
func (m Measurement) Breaches() []string {
	var out []string
	if m.Code > MaxCodeLines {
		out = append(out, fmt.Sprintf("%d code lines (limit %d)", m.Code, MaxCodeLines))
	}
	if m.Stmts > MaxStmts {
		out = append(out, fmt.Sprintf("%d statements (limit %d)", m.Stmts, MaxStmts))
	}
	if m.Funcs > MaxFuncs {
		out = append(out, fmt.Sprintf("%d functions (limit %d)", m.Funcs, MaxFuncs))
	}
	if m.Assoc {
		out = append(out, "associative array (declare -A / local -A): a data structure means a program")
	}
	if m.UI {
		out = append(out, "interactive UI (gum, fzf, whiptail, dialog, select ...)")
	}
	if m.Data {
		out = append(out, "data parsing (more than one jq call, python -c, yq): parsing data means a program")
	}
	return out
}

// WithinLimits reports whether the file breaks none of the limits.
func (m Measurement) WithinLimits() bool {
	return len(m.Breaches()) == 0
}

var (
	// jinjaComment, jinjaTag and jinjaValue are set aside by MeasureTemplate.
	jinjaComment = regexp.MustCompile(`(?s)\{#.*?#\}`)
	jinjaTag     = regexp.MustCompile(`(?s)\{%.*?%\}`)
	jinjaValue   = regexp.MustCompile(`(?s)\{\{.*?\}\}`)
	pythonName   = regexp.MustCompile(`^python[0-9.]*$`)
)

// MeasureTemplate measures a Jinja template that renders to bash: comments and tags are blanked
// (keeping their line breaks), and each expression stands in as one word.
func MeasureTemplate(src []byte) (Measurement, error) {
	text := jinjaComment.ReplaceAllStringFunc(string(src), keepNewlines)
	text = jinjaTag.ReplaceAllStringFunc(text, keepNewlines)
	text = jinjaValue.ReplaceAllStringFunc(text, func(s string) string {
		return "JINJA" + keepNewlines(s)
	})
	return Measure([]byte(text))
}

func keepNewlines(s string) string {
	return strings.Repeat("\n", strings.Count(s, "\n"))
}

// Measure parses a bash file and measures it.
func Measure(src []byte) (Measurement, error) {
	parser := syntax.NewParser(syntax.Variant(syntax.LangBash), syntax.KeepComments(true))
	file, err := parser.Parse(strings.NewReader(string(src)), "")
	if err != nil {
		return Measurement{}, fmt.Errorf("not parseable as bash: %w", err)
	}
	w := walker{lines: strings.Split(string(src), "\n"), commentOnly: map[uint]bool{}}
	syntax.Walk(file, w.visit)
	m := w.m
	m.Code = w.codeLines()
	m.Data = w.jq > 1 || w.data
	return m, nil
}

// walker accumulates what a walk over the syntax tree finds.
type walker struct {
	m           Measurement
	lines       []string
	commentOnly map[uint]bool // lines holding nothing but a comment
	jq          int
	data        bool
}

func (w *walker) visit(n syntax.Node) bool {
	switch x := n.(type) {
	case *syntax.Comment:
		w.comment(x)
	case *syntax.Stmt:
		w.m.Stmts++
		w.stmt(x)
	case *syntax.FuncDecl:
		w.m.Funcs++
	case *syntax.DeclClause:
		w.m.Assoc = w.m.Assoc || assocDecl(x.Variant.Value, declWords(x))
	case *syntax.ForClause:
		w.m.UI = w.m.UI || x.Select
	case *syntax.CallExpr:
		w.call(x)
	}
	return true
}

// comment records a comment that has only blanks before it on its line.
func (w *walker) comment(c *syntax.Comment) {
	line, col := c.Pos().Line(), c.Pos().Col()
	if line == 0 || int(line) > len(w.lines) || col == 0 {
		return
	}
	text := w.lines[line-1]
	if int(col)-1 <= len(text) && strings.TrimSpace(text[:col-1]) == "" {
		w.commentOnly[line] = true
	}
}

func (w *walker) codeLines() int {
	code := 0
	for i, line := range w.lines {
		if strings.TrimSpace(line) != "" && !w.commentOnly[uint(i+1)] {
			code++
		}
	}
	return code
}

// stmt notes a python program fed in on stdin: "python3 <<EOF".
func (w *walker) stmt(s *syntax.Stmt) {
	call, ok := s.Cmd.(*syntax.CallExpr)
	if !ok {
		return
	}
	fed := false
	for _, r := range s.Redirs {
		fed = fed || r.Op == syntax.Hdoc || r.Op == syntax.DashHdoc || r.Op == syntax.WordHdoc
	}
	if !fed {
		return
	}
	for _, inv := range invocations(words(call)) {
		w.data = w.data || pythonName.MatchString(commandName(inv[0]))
	}
}

func (w *walker) call(c *syntax.CallExpr) {
	ws := words(c)
	if len(ws) == 0 {
		return
	}
	switch name := commandName(ws[0]); name {
	case "builtin", cmdCommand:
		if rest := ws[1:]; len(rest) > 0 && assocDecl(commandName(rest[0]), rest[1:]) {
			w.m.Assoc = true
		}
	default:
		// A backslash keeps "declare" from being parsed as the builtin's own clause.
		w.m.Assoc = w.m.Assoc || assocDecl(name, ws[1:])
	}
	for _, inv := range invocations(ws) {
		name := commandName(inv[0])
		switch {
		case uiCommands[name]:
			w.m.UI = true
		case name == "jq":
			w.jq++
		case name == "yq":
			w.data = true
		case pythonName.MatchString(name):
			w.data = w.data || slicesContainsAny(inv[1:], "-c", "-")
		}
	}
}

func slicesContainsAny(list []string, want ...string) bool {
	return slices.ContainsFunc(list, func(s string) bool { return slices.Contains(want, s) })
}

// cmdCommand is the "command" builtin, which runs a command and with -v only looks one up.
const cmdCommand = "command"

var uiCommands = map[string]bool{
	"gum": true, "fzf": true, "fzf-tmux": true, "sk": true, "fzy": true, "peco": true,
	"whiptail": true, "dialog": true, "zenity": true, "yad": true, "kdialog": true,
	"rofi": true, "dmenu": true,
}

// wrapperCommands run the command they are given; the command can follow options and values.
var wrapperCommands = map[string]bool{
	"sudo": true, "doas": true, "exec": true, "env": true, cmdCommand: true, "builtin": true,
	"time": true, "nohup": true, "timeout": true, "nice": true, "ionice": true, "stdbuf": true,
	"unbuffer": true, "setsid": true, "xargs": true,
}

// invocations is the commands a simple command runs: itself, or, behind a wrapper, the first
// word after it that is a command of interest. "command -v x" runs nothing.
func invocations(ws []string) [][]string {
	if len(ws) == 0 {
		return nil
	}
	first := commandName(ws[0])
	if !wrapperCommands[first] {
		return [][]string{ws}
	}
	if first == cmdCommand && slicesContainsAny(ws[1:], "-v", "-V") {
		return nil
	}
	for i := 1; i < len(ws); i++ {
		name := commandName(ws[i])
		if uiCommands[name] || name == "jq" || name == "yq" || pythonName.MatchString(name) {
			return [][]string{ws[i:]}
		}
	}
	return nil
}

// commandName is a command word reduced to the program it names: no directory, no quoting
// backslash.
func commandName(word string) string {
	return path.Base(strings.ReplaceAll(word, `\`, ""))
}

// words are the command's words as literal text; a word built from an expansion is "".
func words(c *syntax.CallExpr) []string {
	out := make([]string, 0, len(c.Args))
	for _, w := range c.Args {
		out = append(out, literal(w))
	}
	return out
}

func declWords(d *syntax.DeclClause) []string {
	var out []string
	for _, a := range d.Args {
		if a.Naked && a.Value != nil {
			out = append(out, literal(a.Value))
		} else {
			out = append(out, "--")
		}
	}
	return out
}

// assocDecl reports whether "<cmd> <args>" declares an associative array: cmd is a declaring
// builtin and an option word before the names has an A.
func assocDecl(cmd string, args []string) bool {
	switch cmd {
	case "declare", "local", "typeset", "readonly":
	default:
		return false
	}
	for _, a := range args {
		if !strings.HasPrefix(a, "-") || a == "--" {
			return false
		}
		if strings.Contains(a, "A") {
			return true
		}
	}
	return false
}

// literal is a word's text when it is only literal and quoted text, else "".
func literal(w *syntax.Word) string {
	var b strings.Builder
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			b.WriteString(p.Value)
		case *syntax.SglQuoted:
			b.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, inner := range p.Parts {
				lit, ok := inner.(*syntax.Lit)
				if !ok {
					return ""
				}
				b.WriteString(lit.Value)
			}
		default:
			return ""
		}
	}
	return b.String()
}
