package rules

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const unitNoInstallMsg = "a unit kept running by Restart=always with no [Install] section is static: enable does nothing and no boot starts it (project_unit_no_install): add [Install] with WantedBy= the target that should start it"

// UnitNoInstall forbids a systemd unit text that is kept running but carries no [Install].
var UnitNoInstall = &analysis.Analyzer{
	Name: "project_unit_no_install",
	Doc: `project_unit_no_install flags a systemd unit text, written in Go, that is kept running
(Restart=always) and has no [Install] section.

Such a unit is static: systemctl enable (and D-Bus EnableUnitFiles) succeeds and does nothing,
UnitFileState stays "static", and nothing wants the unit at boot, so a reboot leaves the service
down until something starts it by hand. A converge step that enables the unit when it is not
enabled then reports the enable as a change on every run (issue 247: teamchat-server@.service
and teamchat-tls@.service).

A unit text is a string concatenation (constant parts folded, non-constant parts read as a
line break), a raw or quoted string, or a list of lines ([]string literal). It is flagged when it
has a "[Service]" line, a "Description=" line, a "Restart=always" line and no "[Install]" line.
A text built on a constant that is itself flagged is not flagged again. Test files are not
judged.

Not flagged: a unit with [Install]; a unit with any other Restart= (a oneshot retried on
failure, started by a timer, is static on purpose); a text with no Description= line, which is
read as a drop-in (its base unit carries [Install], and systemd ignores [Install] in a drop-in).
Not caught: a unit text read from a file at run time or assembled by a builder (strings.Builder,
fmt.Sprintf with the sections as arguments); unit files outside Go.

How to fix: add an [Install] section with WantedBy= the target that should start the unit
(multi-user.target for a system service wanted at boot), and enable the unit (for a template,
each instance).`,
	Run:        perPass(runUnitNoInstall),
	ResultType: noResultType,
}

func runUnitNoInstall(pass *analysis.Pass) {
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		consumed := map[ast.Node]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			if consumed[n] {
				return true
			}
			if parts, sep, ok := unitText(pass, n, consumed); ok &&
				staticDaemon(foldParts(pass, parts, sep)) && !builtOnFlagged(pass, parts) {
				pass.Reportf(n.Pos(), unitNoInstallMsg)
			}
			return true
		})
	}
}

// unitText is n read as a text: its parts and the separator between them, and whether n is a
// text at all (a string addition, a string literal or a []string literal). The additions inside
// it are marked consumed, so each is judged as part of n and not again alone.
func unitText(pass *analysis.Pass, n ast.Node, consumed map[ast.Node]bool) ([]ast.Expr, string, bool) {
	switch x := n.(type) {
	case *ast.BinaryExpr:
		if x.Op == token.ADD && isString(pass, x) {
			return stringChain(x, consumed), "", true
		}
	case *ast.CompositeLit:
		if isStringSlice(pass, x) {
			for _, e := range x.Elts {
				if b, ok := ast.Unparen(e).(*ast.BinaryExpr); ok && b.Op == token.ADD {
					stringChain(b, consumed)
				}
			}
			return x.Elts, "\n", true
		}
	case *ast.BasicLit:
		if x.Kind == token.STRING {
			return []ast.Expr{x}, "", true
		}
	}
	return nil, "", false
}

// stringChain is the operands of a chain of string additions, in order, each addition marked
// consumed so it is not judged again as a text of its own.
func stringChain(e *ast.BinaryExpr, consumed map[ast.Node]bool) []ast.Expr {
	consumed[e] = true
	var out []ast.Expr
	for _, side := range []ast.Expr{e.X, e.Y} {
		if b, ok := ast.Unparen(side).(*ast.BinaryExpr); ok && b.Op == token.ADD {
			out = append(out, stringChain(b, consumed)...)
			continue
		}
		if lit, ok := ast.Unparen(side).(*ast.BasicLit); ok {
			consumed[lit] = true
		}
		out = append(out, side)
	}
	return out
}

// foldParts is the text of parts joined by sep: each constant part's value, each other part read
// as a line break, so a value computed at run time never joins two lines into one.
func foldParts(pass *analysis.Pass, parts []ast.Expr, sep string) string {
	texts := make([]string, 0, len(parts))
	for _, p := range parts {
		if lit, ok := ast.Unparen(p).(*ast.BasicLit); ok {
			p = lit
		}
		if s, ok := constString(pass, p); ok {
			texts = append(texts, s)
			continue
		}
		if b, ok := ast.Unparen(p).(*ast.BinaryExpr); ok && b.Op == token.ADD {
			texts = append(texts, foldParts(pass, stringChain(b, map[ast.Node]bool{}), ""))
			continue
		}
		texts = append(texts, "\n")
	}
	return strings.Join(texts, sep)
}

// builtOnFlagged is whether a part is a named constant whose own text is flagged where it is
// declared, so the text built on it is the same finding.
func builtOnFlagged(pass *analysis.Pass, parts []ast.Expr) bool {
	for _, p := range parts {
		var id *ast.Ident
		switch x := ast.Unparen(p).(type) {
		case *ast.Ident:
			id = x
		case *ast.SelectorExpr:
			id = x.Sel
		default:
			continue
		}
		if c, ok := pass.TypesInfo.Uses[id].(*types.Const); ok && c.Val().Kind() == constant.String &&
			staticDaemon(constant.StringVal(c.Val())) {
			return true
		}
	}
	return false
}

// staticDaemon is whether a unit text is kept running and has no [Install] section.
func staticDaemon(text string) bool {
	var service, described, always, install bool
	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)
		switch {
		case line == "[Service]":
			service = true
		case line == "[Install]":
			install = true
		case strings.HasPrefix(line, "Description="):
			described = true
		case line == "Restart=always":
			always = true
		}
	}
	return service && described && always && !install
}

// isString is whether e is of a string type.
func isString(pass *analysis.Pass, e ast.Expr) bool {
	t := pass.TypesInfo.TypeOf(e)
	if t == nil {
		return false
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsString != 0
}

// isStringSlice is whether a composite literal is a slice of strings.
func isStringSlice(pass *analysis.Pass, lit *ast.CompositeLit) bool {
	t := pass.TypesInfo.TypeOf(lit)
	if t == nil {
		return false
	}
	s, ok := t.Underlying().(*types.Slice)
	if !ok {
		return false
	}
	b, ok := s.Elem().Underlying().(*types.Basic)
	return ok && b.Info()&types.IsString != 0
}
