package rules

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// seatTextTrees are the trees whose text a person reads at a launch, a chooser, a restore or a
// repair: ccy-teams, ccy-sessions and the launch.
var seatTextTrees = []string{
	"internal/teams", "internal/sessions", "internal/conversation", "internal/ccylaunch",
	"internal/registry", "cmd/ccy-teams", "cmd/ccy-sessions",
}

// seatTextAllowances are the functions project_seat_number_text does not judge.
var seatTextAllowances = []allowance{
	{pkg: "internal/teams/shutdown", fn: "v2Label", reason: "names an LXC seat that has no handle yet in the not-reached line of a shutdown, for diagnosis; it is never a choice's row"},
}

// seatValue stands, in a string's template, for a value the program fills in.
const seatValue = "\x00"

// The shapes of text that has a person type or pick a seat, matched on a string's template.
var (
	// A launch with the seat spelt out: --teams <seat>@, SEAT@, 2@, or a filled-in seat.
	seatLaunchRe = regexp.MustCompile(`--teams[ =]+(?:<[^>]*>|[A-Z]+|\d+|` + seatValue + `)@`)
	// A seat placeholder: <seat>@, SEAT@TEAM, S@T.
	seatPlaceholderRe = regexp.MustCompile(`<seat>|\bSEAT@|\bS@T\b`)
	// A hand-over told as a seat to type, or a seat repair that names the seat. A hand-to= filled
	// in is the person's own value echoed back, for diagnosis.
	seatRepairRe = regexp.MustCompile(`hand-to=(?:<[^>]*>|\d+)|seats (?:conversation|model)\b[^\n]*(?:<[^>]*>|\d+|` + seatValue + `)@`)
	// A launch (ccy, cc, or a launcher filled in) whose list is filled in, judged by what fills
	// it; a bare --teams is a record's words, not a command.
	seatListRe = regexp.MustCompile(`(?:(?:\bccy|\bcc|` + seatValue + `) --teams[ =]+|seats (?:conversation|model) )` + seatValue)
	// A row that labels a seat by its name.
	seatRowRe = regexp.MustCompile(`(?i)\bseat ` + seatValue)
	// A function that builds the rows or labels of a choice.
	rowBuilderRe = regexp.MustCompile(`(?i)(?:row|rows|choices|options|label)$`)
	// A value that is a seat list, a seat or a handle, by its spelling.
	seatListOperandRe = regexp.MustCompile(`(?i)seat|ref|list|canonical|handle|member`)
	// A printf verb, which a format fills in.
	printfVerbRe = regexp.MustCompile(`%[-+# 0-9.*]*[a-zA-Z]`)
)

const seatTextFix = " (project_seat_number_text): a person never needs a seat number; name the command by team (ccy --teams <team>, cc --teams <team>, ccy-teams) and the seat by its role and what it last ran"

// SeatNumberText keeps a seat number out of the text a person is told to type or pick from.
var SeatNumberText = &analysis.Analyzer{
	Name: "project_seat_number_text",
	Doc: `project_seat_number_text flags, in internal/teams, internal/sessions, internal/conversation,
internal/ccylaunch, internal/registry, cmd/ccy-teams and cmd/ccy-sessions and below, outside test
files, a string (a literal, a concatenation, or a printf format with its arguments) that has a
person type or pick a seat:

  - a launch with the seat spelt out: --teams <seat>@, --teams SEAT@, --teams 2@, or a seat
    filled in before the @;
  - a seat placeholder anywhere: <seat>, SEAT@, S@T;
  - hand-to=<seat>, or seats conversation / seats model with a seat before an @;
  - --teams followed by a filled-in value whose spelling is a seat, a ref, a list, a handle or
    a member (a canonical seat list);
  - in a function that builds a choice's rows or labels (its name ends in row, rows, choices,
    options, label or labels): a row that labels a seat by its name ("seat " then a value), or
    shows a handle, which carries the seat.

Only a seat as the thing a person types or picks is the defect (Plan 00265 R90: a person never
needs a seat number, which only the machine knows). An info, progress or error line that names a
seat for diagnosis is not flagged, nor are keeper logs and agent-facing text (teamchat), which
no person is asked to act on.

Not caught: a seat list joined into an argv and printed whole (restore's replayed command); a
row built outside a row builder; help text and docs outside Go (embedded .txt, markdown).

How to fix: name the command by its team (ccy --teams <team>, cc --teams <team>, ccy-teams),
which finds the seat, and show a seat by its role and its last conversation.`,
	Run:        perPass(runSeatNumberText),
	ResultType: noResultType,
}

func runSeatNumberText(pass *analysis.Pass) {
	if !slices.ContainsFunc(seatTextTrees, func(tree string) bool { return inTree(pass.Pkg.Path(), tree) }) {
		return
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, d := range f.Decls {
			if allowedIn(pass, funcName(d), seatTextAllowances) {
				continue
			}
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Body != nil {
					reportSeatTexts(pass, d.Body, rowBuilderRe.MatchString(d.Name.Name))
				}
			case *ast.GenDecl:
				reportSeatTexts(pass, d, false)
			}
		}
	}
}

// reportSeatTexts reports each outermost string under root that has a person type or pick a
// seat; rows is whether root builds a choice's rows.
func reportSeatTexts(pass *analysis.Pass, root ast.Node, rows bool) {
	ast.Inspect(root, func(n ast.Node) bool {
		e, isExpr := n.(ast.Expr)
		if !isExpr {
			return true
		}
		tmpl, operands, ok := seatTemplate(pass, e)
		if !ok {
			return true
		}
		if msg := seatTextFinding(tmpl, operands, rows); msg != "" {
			pass.Reportf(e.Pos(), "%s%s", msg, seatTextFix)
		}
		return false
	})
}

// seatTemplate is the text e builds, with each filled-in value as seatValue, and those values
// in order; false for an expression that builds no text of its own.
func seatTemplate(pass *analysis.Pass, e ast.Expr) (string, []ast.Expr, bool) {
	switch e := e.(type) {
	case *ast.BasicLit:
		if s, ok := stringConst(pass, e); ok {
			return s, nil, true
		}
	case *ast.BinaryExpr:
		if e.Op == token.ADD && isString(pass, e) {
			x, xs := concatPart(pass, e.X)
			y, ys := concatPart(pass, e.Y)
			return x + y, append(xs, ys...), true
		}
	case *ast.CallExpr:
		return printfTemplate(pass, e)
	}
	return "", nil, false
}

// concatPart is one operand of a concatenation as template text.
func concatPart(pass *analysis.Pass, e ast.Expr) (string, []ast.Expr) {
	if s, ok := stringConst(pass, e); ok {
		return s, nil
	}
	switch inner := ast.Unparen(e).(type) {
	case *ast.BinaryExpr:
		if inner.Op == token.ADD {
			if t, ops, ok := seatTemplate(pass, inner); ok {
				return t, ops
			}
		}
	case *ast.CallExpr:
		if t, ops, ok := printfTemplate(pass, inner); ok {
			return t, ops
		}
	}
	return seatValue, []ast.Expr{e}
}

// printfTemplate is a fmt format call's text, with its arguments as the filled-in values.
func printfTemplate(pass *analysis.Pass, call *ast.CallExpr) (string, []ast.Expr, bool) {
	fn := calledFunc(pass, call)
	if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != "fmt" || !strings.HasSuffix(fn.Name(), "f") {
		return "", nil, false
	}
	at := 0
	if strings.HasPrefix(fn.Name(), "F") {
		at = 1
	}
	if len(call.Args) <= at {
		return "", nil, false
	}
	format, ok := stringConst(pass, call.Args[at])
	if !ok {
		return "", nil, false
	}
	format = strings.ReplaceAll(format, "%%", "")
	return printfVerbRe.ReplaceAllString(format, seatValue), call.Args[at+1:], true
}

// seatTextFinding is the finding for a template, "" for clean text.
func seatTextFinding(tmpl string, operands []ast.Expr, rows bool) string {
	switch {
	case seatLaunchRe.MatchString(tmpl):
		return "a launch that spells out the seat"
	case seatRepairRe.MatchString(tmpl):
		return "a command that names the seat"
	case seatPlaceholderRe.MatchString(tmpl):
		return "a seat placeholder a person is told to fill"
	case seatListFilled(tmpl, operands):
		return "a launch with its seat list filled in"
	case rows && seatRowRe.MatchString(tmpl):
		return "a choice's row labelled by its seat"
	case rows && slices.ContainsFunc(operands, isHandleValue):
		return "a choice's row that shows a handle, which carries the seat"
	}
	return ""
}

// seatListFilled is whether a --teams in tmpl is filled by a value spelt as a seat or a list.
func seatListFilled(tmpl string, operands []ast.Expr) bool {
	for _, loc := range seatListRe.FindAllStringIndex(tmpl, -1) {
		i := strings.Count(tmpl[:loc[1]-1], seatValue)
		if i < len(operands) && seatListOperandRe.MatchString(types.ExprString(operands[i])) {
			return true
		}
	}
	return false
}

// isHandleValue is whether e reads a handle: a .Handle selector, or a call given one.
func isHandleValue(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "Handle" {
			found = true
		}
		return !found
	})
	return found
}
