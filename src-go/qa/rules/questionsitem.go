package rules

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

const (
	pendingType      = "Pending"
	pendingRoom      = "Room"
	purposeQuestions = "PurposeQuestions"
)

// questionsItemScope is the member code that drops or keeps the questions room's items: the
// engine's writes and the team goroutine's.
var questionsItemScope = []string{"internal/teamchat/member/engine", "internal/teamchat/member/teamsync"}

// QuestionsItem forbids deciding by a pending item's room alone whether it is the questions
// room's own, which cannot tell a relayed answer in flight from the room's stale state.
var QuestionsItem = &analysis.Analyzer{
	Name: "project_questions_item_by_room",
	Doc: `project_questions_item_by_room flags, in teamchat's engine and teamsync
(internal/teamchat/member/engine, internal/teamchat/member/teamsync; not their tests), a == or !=
comparison of a state.Pending's Room field with proto.PurposeQuestions, either way round, by
value or through a pointer.

A pending item in room "questions" is either the questions room's own sync, which a member that
no longer coordinates the room drops (QS9), or a human's answer relayed to this worker through
main (A 10.6), which is a delivery in flight and must be kept. The room alone cannot tell them
apart: the drop owed at every session start (qstart's holdsQuestions) and the demotion's write
(applyQuestions) both tested the room, so a network blip deleted a worker's undelivered
relayed answer, whose relay main's floor had already passed (Plan 00209, R0 step 7 review).

Not flagged: package state, where Pending.OfQuestionsSync is the one test; another type's
room (a send in flight's); a test against main's room; packages that show an item's room
(delivery) rather than drop it.

How to fix: call Pending.OfQuestionsSync, which keeps a relayed item.`,
	Run:        perPass(runQuestionsItem),
	ResultType: noResultType,
}

func runQuestionsItem(pass *analysis.Pass) {
	if !ownedByAny(pass.Pkg.Path(), questionsItemScope) {
		return
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			b, ok := n.(*ast.BinaryExpr)
			if ok && (b.Op == token.EQL || b.Op == token.NEQ) &&
				(pendingRoomOf(pass, b.X) && isQuestionsPurpose(pass, b.Y) || pendingRoomOf(pass, b.Y) && isQuestionsPurpose(pass, b.X)) {
				pass.Reportf(b.Pos(), "a pending item's room tested against the questions room (project_questions_item_by_room): it cannot tell a relayed answer from the room's own items; call state.Pending.OfQuestionsSync")
			}
			return true
		})
	}
}

// pendingRoomOf is whether e is the Room field of a state.Pending (or a pointer to one).
func pendingRoomOf(pass *analysis.Pass, e ast.Expr) bool {
	sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != pendingRoom {
		return false
	}
	t := pass.TypesInfo.TypeOf(sel.X)
	if p, isPtr := t.(*types.Pointer); isPtr {
		t = p.Elem()
	}
	named, isNamed := t.(*types.Named)
	if !isNamed {
		return false
	}
	obj := named.Obj()
	return obj.Name() == pendingType && obj.Pkg() != nil && ownedBy(obj.Pkg().Path(), teamchatStatePkg)
}

// isQuestionsPurpose is whether e names proto.PurposeQuestions.
func isQuestionsPurpose(pass *analysis.Pass, e ast.Expr) bool {
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
	return ok && c.Pkg() != nil && ownedBy(c.Pkg().Path(), teamchatProtoPkg) && c.Name() == purposeQuestions
}
