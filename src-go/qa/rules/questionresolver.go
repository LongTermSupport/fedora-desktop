package rules

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

const (
	fieldQuestion = "Question"
	fieldHeadline = "Headline"
	inQuestion    = "InQuestion"
	teamchatTeam  = "internal/teamchat/member/team"
)

// questionCarriers are the types that show which question an event is in, by package and name.
var questionCarriers = []struct{ pkg, name string }{
	{"internal/teamchat/member/records", "Human"},
	{"internal/teamchat/member/records", "Verified"},
	{"internal/teamchat/member/deliver", "Item"},
}

// QuestionUnresolved keeps every shown question to the one resolver's answer.
var QuestionUnresolved = &analysis.Analyzer{
	Name: "project_question_unresolved",
	Doc: `project_question_unresolved forbids a question shown without asking the one resolver.

Which question an event of the questions room is in is one fact, decided by
proto.QuestionIndex.QuestionIn and handed on as a team.InQuestion. The delivery's item and recv's
record of a human's reply in a question's thread were built with no question at all, while
verify derived it on a path of its own: the envelope said "question: none" for a reply verify
named Q8, and the coordinator answered in the wrong thread (Plan 00265 R34).

Flagged, under internal/teamchat in files that are not tests: a composite literal of
records.Human, records.Verified or deliver.Item with elements but no Question key; and a value of
a Question or Headline field of one of those types, in a literal or an assignment, that is not
a call of a team.InQuestion method.

Not caught: the fact derived by hand into a team.InQuestion (team.QuestionNamed is for a source
other than the index: a relay's account, or verify's read of a question root); a carrier type
added later and not listed in questionCarriers.

How to fix: ask team.InQuestionOf (or the team's QuestionIn), and set Question and Headline from
its Entry, Name and HeadlineName.`,
	Run:        perPass(runQuestionUnresolved),
	ResultType: noResultType,
}

func runQuestionUnresolved(pass *analysis.Pass) {
	if !inTeamchat(pass.Pkg.Path()) {
		return
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CompositeLit:
				checkCarrierLiteral(pass, n)
			case *ast.AssignStmt:
				checkCarrierAssign(pass, n)
			}
			return true
		})
	}
}

// carrierOf is the carrier's short name ("records.Human") when t is one (or a pointer to one).
func carrierOf(t types.Type) (string, bool) {
	if p, isPtr := t.(*types.Pointer); isPtr {
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return "", false
	}
	obj := named.Obj()
	for _, c := range questionCarriers {
		if obj.Name() == c.name && ownedBy(obj.Pkg().Path(), c.pkg) {
			return obj.Pkg().Name() + "." + c.name, true
		}
	}
	return "", false
}

func checkCarrierLiteral(pass *analysis.Pass, lit *ast.CompositeLit) {
	name, ok := carrierOf(pass.TypesInfo.TypeOf(lit))
	if !ok || len(lit.Elts) == 0 {
		return
	}
	hasQuestion := false
	for _, e := range lit.Elts {
		kv, isKV := e.(*ast.KeyValueExpr)
		if !isKV {
			continue
		}
		key, isIdent := kv.Key.(*ast.Ident)
		if !isIdent || key.Name != fieldQuestion && key.Name != fieldHeadline {
			continue
		}
		hasQuestion = hasQuestion || key.Name == fieldQuestion
		checkQuestionValue(pass, name, key.Name, kv.Value)
	}
	if !hasQuestion {
		pass.Reportf(lit.Pos(), "a %s built without its question (project_question_unresolved): set Question from "+
			"team.InQuestionOf, the one resolver, so the event's question is shown as every other path shows it", name)
	}
}

// checkCarrierAssign judges each carrier's Question or Headline assigned: its own value, or, in
// a tuple assignment from one call, that call, which is never an InQuestion method (each gives
// one value).
func checkCarrierAssign(pass *analysis.Pass, as *ast.AssignStmt) {
	for i, lhs := range as.Lhs {
		sel, ok := ast.Unparen(lhs).(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != fieldQuestion && sel.Sel.Name != fieldHeadline {
			continue
		}
		name, isCarrier := carrierOf(pass.TypesInfo.TypeOf(sel.X))
		switch {
		case !isCarrier:
		case len(as.Lhs) == len(as.Rhs):
			checkQuestionValue(pass, name, sel.Sel.Name, as.Rhs[i])
		default:
			reportQuestionValue(pass, name, sel.Sel.Name, lhs)
		}
	}
}

// checkQuestionValue reports a carrier's Question or Headline value that is not a call of a
// team.InQuestion method.
func checkQuestionValue(pass *analysis.Pass, carrier, field string, value ast.Expr) {
	call, ok := ast.Unparen(value).(*ast.CallExpr)
	if ok {
		if fn := calledFunc(pass, call); fn != nil && isInQuestionMethod(fn) {
			return
		}
	}
	reportQuestionValue(pass, carrier, field, value)
}

func reportQuestionValue(pass *analysis.Pass, carrier, field string, at ast.Expr) {
	pass.Reportf(at.Pos(), "a %s's %s not from team.InQuestion (project_question_unresolved): which question an "+
		"event is in is decided once, by proto.QuestionIndex.QuestionIn; take it from team.InQuestionOf", carrier, field)
}

func isInQuestionMethod(fn *types.Func) bool {
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return false
	}
	named, ok := sig.Recv().Type().(*types.Named)
	return ok && named.Obj().Name() == inQuestion && named.Obj().Pkg() != nil && ownedBy(named.Obj().Pkg().Path(), teamchatTeam)
}
