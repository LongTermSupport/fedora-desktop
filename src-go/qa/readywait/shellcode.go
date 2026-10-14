package readywait

import (
	"fmt"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// shellCode is a script read from its syntax tree: its text with quoted text, comments,
// here-document bodies and arithmetic blanked, and its functions. Every byte keeps its offset and
// every newline its place, so a position in the code is a position in the source.
type shellCode struct {
	code      string
	functions []function
}

// function is one shell function: where its definition starts and where its body runs (the text
// between the braces).
type function struct {
	name                 string
	defStart, start, end int
}

// readShell parses text as bash. A script that does not parse is an error: a rule that could not
// read a file has not judged it.
func readShell(text string) (*shellCode, error) {
	parser := syntax.NewParser(syntax.Variant(syntax.LangBash), syntax.KeepComments(true))
	file, err := parser.Parse(strings.NewReader(text), "")
	if err != nil {
		return nil, fmt.Errorf("cannot parse as bash: %w", err)
	}
	m := &masker{text: text, blank: make([]bool, len(text))}
	syntax.Walk(file, m.visit)
	out := []byte(text)
	for i, hide := range m.blank {
		if hide && out[i] != '\n' {
			out[i] = ' '
		}
	}
	return &shellCode{code: string(out), functions: m.functions}, nil
}

// masker marks the bytes of the source that are not code.
type masker struct {
	text      string
	blank     []bool
	functions []function
	index     map[string]int
}

// mark sets the bytes from..to (clamped to the text) to blank or kept.
func (m *masker) mark(from, to uint, hide bool) {
	for i := from; i < to && i < uint(len(m.blank)); i++ {
		m.blank[i] = hide
	}
}

func (m *masker) visit(node syntax.Node) bool {
	switch n := node.(type) {
	case *syntax.Comment:
		m.mark(n.Hash.Offset(), n.End().Offset(), true)
	case *syntax.SglQuoted:
		m.mark(n.Left.Offset(), n.Right.Offset()+1, true)
	case *syntax.DblQuoted:
		m.dblQuoted(n)
	case *syntax.Redirect:
		if n.Hdoc != nil {
			m.mark(n.Hdoc.Pos().Offset(), n.Hdoc.End().Offset(), true)
		}
	case *syntax.ArithmExp:
		skip := uint(3)
		if n.Bracket {
			skip = 2
		}
		m.mark(n.Left.Offset()+skip, n.Right.Offset(), true)
	case *syntax.ArithmCmd:
		m.mark(n.Left.Offset()+2, n.Right.Offset(), true)
	case *syntax.CStyleLoop:
		m.mark(n.Lparen.Offset()+2, n.Rparen.Offset(), true)
	case *syntax.Lit:
		m.escapes(n)
	case *syntax.FuncDecl:
		m.function(n)
	}
	return true
}

// dblQuoted blanks a double-quoted string except the parts bash runs or reads as code: a command
// or process substitution, and $!, which is how a script keeps a pid.
func (m *masker) dblQuoted(q *syntax.DblQuoted) {
	m.mark(q.Pos().Offset(), q.End().Offset(), true)
	for _, part := range q.Parts {
		syntax.Walk(part, func(node syntax.Node) bool {
			switch n := node.(type) {
			case *syntax.CmdSubst, *syntax.ProcSubst:
				m.mark(n.Pos().Offset(), n.End().Offset(), false)
				return false
			case *syntax.ParamExp:
				if n.Param != nil && n.Param.Value == "!" && n.Short {
					m.mark(n.Pos().Offset(), n.End().Offset(), false)
					return false
				}
			}
			return true
		})
	}
}

// escapes blanks each backslash in an unquoted word and the character it escapes.
func (m *masker) escapes(lit *syntax.Lit) {
	from, to := lit.ValuePos.Offset(), min(lit.ValueEnd.Offset(), uint(len(m.text)))
	for i := from; i < to; i++ {
		if m.text[i] != '\\' {
			continue
		}
		m.blank[i] = true
		if i+1 < to && m.text[i+1] != '\n' {
			m.blank[i+1] = true
		}
		i++
	}
}

// function records a function whose body is a brace group; a later definition of a name
// replaces the earlier.
func (m *masker) function(decl *syntax.FuncDecl) {
	body, ok := decl.Body.Cmd.(*syntax.Block)
	if !ok {
		return
	}
	f := function{
		name:     decl.Name.Value,
		defStart: int(decl.Pos().Offset()),
		start:    int(body.Lbrace.Offset()) + 1,
		end:      int(body.Rbrace.Offset()),
	}
	if m.index == nil {
		m.index = map[string]int{}
	}
	if at, seen := m.index[f.name]; seen {
		m.functions[at] = f
		return
	}
	m.index[f.name] = len(m.functions)
	m.functions = append(m.functions, f)
}
