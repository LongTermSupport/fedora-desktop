package bashlimits

// The rule assignment-masks-substitution-failure (CLAUDE/QA.md#assignment-masks-substitution-failure):
// an assignment whose exit status does not carry the failure of every command substitution in
// the values it assigns, so errexit, "if" and "||" see success after a failed read. Bash gives a
// statement made only of assignments the status of the LAST substitution it performed, gives a
// declaration builtin (local, declare, typeset, export, readonly) its own status, and gives a
// command its own status over its prefix assignments. So the shapes are:
//
//   - ShapeSeveral: a statement of assignments only, holding two or more substitutions that are
//     not nested in one another, across all its assignments: x="$(a)$(b)", x=$(a) y=$(b),
//     a=("$(a)" "$(b)"), a+=(...), and substitutions in an index, a default or arithmetic.
//   - ShapeDeclaration: a declaration builtin assigning any substitution, also spelled
//     "builtin local", "command export" or "\local". shellcheck's SC2155 reports the scalar
//     forms; it does not report an array literal (local -a a=("$(a)")).
//   - ShapePrefix: a substitution in an assignment that prefixes a command (X=$(a) cmd).
//
// Not this class: a substitution nested in another (its status is lost inside the outer
// subshell; inherit_errexit is the remedy), a substitution as a command's argument (the
// command's status is reported, not the assignment's), and a process substitution, whose status
// bash never reports.

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// MaskedRuleID is the rule's stable identifier, a heading of CLAUDE/QA.md.
const MaskedRuleID = "assignment-masks-substitution-failure"

// KindMasked is a finding of the rule.
const KindMasked = "masked"

// The shapes the rule reports.
const (
	ShapeSeveral     = "several"
	ShapeDeclaration = "declaration"
	ShapePrefix      = "prefix"
)

// shapeText is each shape's finding text.
var shapeText = map[string]string{
	ShapeSeveral:     "assignment holds several command substitutions and reports only the last one's status",
	ShapeDeclaration: "declaration builtin assigns a command substitution and reports its own status",
	ShapePrefix:      "command's prefix assignment holds a command substitution whose status is never reported",
}

// MaskedSubstitution is one finding: the line the statement starts on, and its shape.
type MaskedSubstitution struct {
	Line  uint
	Shape string
}

// MaskedSubstitutions parses a bash file and reports each assignment that masks a
// substitution's failure, in source order.
func MaskedSubstitutions(src []byte) ([]MaskedSubstitution, error) {
	parser := syntax.NewParser(syntax.Variant(syntax.LangBash))
	file, err := parser.Parse(strings.NewReader(string(src)), "")
	if err != nil {
		return nil, fmt.Errorf("not parseable as bash: %w", err)
	}
	var out []MaskedSubstitution
	syntax.Walk(file, func(n syntax.Node) bool {
		if shape := maskedShape(n); shape != "" {
			out = append(out, MaskedSubstitution{n.Pos().Line(), shape})
		}
		return true
	})
	slices.SortStableFunc(out, func(a, b MaskedSubstitution) int { return int(a.Line) - int(b.Line) })
	return out, nil
}

// MaskedSubstitutionsTemplate judges a Jinja template that renders to bash, with its Jinja set
// aside as MeasureTemplate sets it aside, so lines keep their numbers.
func MaskedSubstitutionsTemplate(src []byte) ([]MaskedSubstitution, error) {
	return MaskedSubstitutions(untemplate(src))
}

func maskedShape(n syntax.Node) string {
	switch x := n.(type) {
	case *syntax.DeclClause:
		for _, a := range x.Args {
			if substitutions(a) > 0 {
				return ShapeDeclaration
			}
		}
	case *syntax.CallExpr:
		return callShape(x)
	}
	return ""
}

func callShape(c *syntax.CallExpr) string {
	total := 0
	for _, a := range c.Assigns {
		total += substitutions(a)
	}
	switch {
	case len(c.Args) == 0 && total > 1:
		return ShapeSeveral
	case len(c.Args) > 0 && total > 0:
		return ShapePrefix
	case len(c.Args) > 0 && spelledDeclaration(c.Args):
		return ShapeDeclaration
	}
	return ""
}

// spelledDeclaration is a declaration builtin run as a command ("builtin local", "command
// export", "\local") with a substitution in a word after it.
func spelledDeclaration(args []*syntax.Word) bool {
	name, rest := commandName(literal(args[0])), args[1:]
	if (name == cmdBuiltin || name == cmdCommand) && len(rest) > 0 {
		name, rest = commandName(literal(rest[0])), rest[1:]
	}
	switch name {
	case "local", "declare", "typeset", "export", "readonly":
	default:
		return false
	}
	return slices.ContainsFunc(rest, func(w *syntax.Word) bool { return substitutions(w) > 0 })
}

// substitutions counts the command substitutions in n that are not inside another one.
func substitutions(n syntax.Node) int {
	count := 0
	syntax.Walk(n, func(inner syntax.Node) bool {
		switch inner.(type) {
		case *syntax.CmdSubst:
			count++
			return false
		case *syntax.ProcSubst:
			return false
		}
		return true
	})
	return count
}

// MaskedFindings reports the rule's findings of a scan, by path and line.
func MaskedFindings(scan Scan) []Finding {
	var out []Finding
	for _, p := range slices.Sorted(maps.Keys(scan.Masked)) {
		for _, m := range scan.Masked[p] {
			out = append(out, Finding{p, KindMasked, fmt.Sprintf(
				"%s:%d: %s: give each substitution a plain assignment of its own (%s, CLAUDE/QA.md#%s)",
				p, m.Line, shapeText[m.Shape], MaskedRuleID, MaskedRuleID)})
		}
	}
	return out
}
