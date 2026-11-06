package rules

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// The packages whose sends pick a room, and the package that owns the room purposes.
const (
	teamchatProtoPkg = "internal/teamchat/proto"
	purposePrefix    = "Purpose"
	purposeMain      = "PurposeMain"
)

// roomDefaultScope is the member code that posts to a room or handles a post's answer: the
// engine's sender and verbs, and the team goroutine's.
var roomDefaultScope = []string{"internal/teamchat/member/engine", "internal/teamchat/member/teamsync"}

// RoomDefault forbids a room decision that gives every room but one side room main's path.
var RoomDefault = &analysis.Analyzer{
	Name: "project_room_default",
	Doc: `project_room_default flags, in teamchat's engine and teamsync (internal/teamchat/member/engine,
internal/teamchat/member/teamsync; not their tests), a test of a room against a side room's
purpose (a proto.Purpose* constant other than proto.PurposeMain) whose other path is every other
room: a != comparison anywhere; an == comparison that is the condition, or one of the && terms of
the condition, of an if that has an else or whose body ends in a return, a break, a continue, a
goto or a panic; and an == comparison in a case of a switch with no tag that is followed by
another case.

A two-way test on one side room sends every other room down the path written for main. That
was the shape b2's report builder removed from sendingOf, answerSent and recordPosted (each took
main's room for a report), and three remained: postRoom posted every room that was not the
reports room to main, recheck decoded every send that was not a report as main's, and
sendFailed handled a 403 for anything not a report as main's, which stops the team's sends. The
questions room's sends would have met each of them as main's (Plan 00209 n3f-b3).

Not flagged: a test for main itself (== or != proto.PurposeMain), whose path is main's alone; a
side room's own addition (an if on == with no else and a body that falls through); the last
case of a tagless switch; a switch on the room, whose cases name each room. A room compared
through a variable holding a purpose, or a helper that returns whether a room is a side room, is
not followed.

How to fix: switch on the room, a case for each room the code handles and a default that refuses
the rest (an error naming the room), so a room added later is refused until its own case is
written.`,
	Run:        perPass(runRoomDefault),
	ResultType: noResultType,
}

func runRoomDefault(pass *analysis.Pass) {
	if !ownedByAny(pass.Pkg.Path(), roomDefaultScope) {
		return
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		flagged := map[token.Pos]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			for _, e := range roomDefaults(pass, n) {
				if !flagged[e.Pos()] {
					flagged[e.Pos()] = true
					pass.Reportf(e.Pos(), "a room tested against one side room gives every other room main's path (project_room_default): switch on the room, a case for each and a default that refuses")
				}
			}
			return true
		})
	}
}

// roomDefaults are the side-room tests n makes whose other path is every other room's.
func roomDefaults(pass *analysis.Pass, n ast.Node) []*ast.BinaryExpr {
	switch n := n.(type) {
	case *ast.BinaryExpr:
		if n.Op == token.NEQ && sideRoomTest(pass, n) {
			return []*ast.BinaryExpr{n}
		}
	case *ast.IfStmt:
		if n.Else != nil || endsBlock(n.Body) {
			return sideEquals(pass, n.Cond)
		}
	case *ast.SwitchStmt:
		if n.Tag == nil {
			return caseDefaults(pass, n.Body.List)
		}
	}
	return nil
}

// caseDefaults are the side-room == tests in the cases of a tagless switch that another case
// follows.
func caseDefaults(pass *analysis.Pass, clauses []ast.Stmt) []*ast.BinaryExpr {
	var out []*ast.BinaryExpr
	for _, s := range clauses[:max(len(clauses)-1, 0)] {
		if cc, ok := s.(*ast.CaseClause); ok {
			for _, c := range cc.List {
				out = append(out, sideEquals(pass, c)...)
			}
		}
	}
	return out
}

// sideEquals are the == comparisons with a side room among e's && terms (through parentheses).
func sideEquals(pass *analysis.Pass, e ast.Expr) []*ast.BinaryExpr {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return sideEquals(pass, x.X)
	case *ast.BinaryExpr:
		switch {
		case x.Op == token.LAND:
			return append(sideEquals(pass, x.X), sideEquals(pass, x.Y)...)
		case x.Op == token.EQL && sideRoomTest(pass, x):
			return []*ast.BinaryExpr{x}
		}
	}
	return nil
}

// sideRoomTest is whether either operand of e is a side room's purpose constant.
func sideRoomTest(pass *analysis.Pass, e *ast.BinaryExpr) bool {
	return isSidePurpose(pass, e.X) || isSidePurpose(pass, e.Y)
}

// isSidePurpose is whether e names one of proto's Purpose* constants other than PurposeMain.
func isSidePurpose(pass *analysis.Pass, e ast.Expr) bool {
	var id *ast.Ident
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		id = x
	case *ast.SelectorExpr:
		id = x.Sel
	default:
		return false
	}
	c, ok := pass.TypesInfo.Uses[id].(*types.Const)
	return ok && c.Pkg() != nil && ownedBy(c.Pkg().Path(), teamchatProtoPkg) &&
		strings.HasPrefix(c.Name(), purposePrefix) && c.Name() != purposeMain
}

// endsBlock is whether b's last statement leaves it: a return, a branch or a panic.
func endsBlock(b *ast.BlockStmt) bool {
	if len(b.List) == 0 {
		return false
	}
	switch s := b.List[len(b.List)-1].(type) {
	case *ast.ReturnStmt, *ast.BranchStmt:
		return true
	case *ast.ExprStmt:
		call, ok := s.X.(*ast.CallExpr)
		if !ok {
			return false
		}
		fn, isIdent := call.Fun.(*ast.Ident)
		return isIdent && fn.Name == "panic"
	}
	return false
}
