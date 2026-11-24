package rules

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

const (
	seatPkg          = "internal/teamchat/seat"
	seatStoreType    = "Store"
	seatHoldMethod   = "hold"
	seatTakeMethod   = "Take"
	seatRecordMethod = "Restorable"
)

// SeatHoldUnrecorded holds that a seat holding is made only by Store.Take, after it asks the
// session record: the sweep releases a holding whose session has no record, so a holding made
// without asking is moved out from under its session a minute later.
var SeatHoldUnrecorded = &analysis.Analyzer{
	Name: "project_seat_hold_unrecorded",
	Doc: `project_seat_hold_unrecorded flags, in teamchat's seat store (internal/teamchat/seat) and
outside test files, a call of (*Store).hold, the one step that makes a seat holding (held/ and
held.json), that is not in (*Store).Take, or that is in Take with no call of the session records'
Restorable before it.

The owner's seat sweep releases every holding whose session is not restorable (has no ccy
session record), even while the session's keeper holds the seat. A take that did not ask the
same question first took a seat for a session with no record (a ccy --teams launch from a pane
already on ccy's tmux), and the sweep moved it out from under the running session a minute
later, leaving it unable to send (fedora-desktop#393).

Not flagged: Take calling hold after Restorable; a test file; any other package.

How to fix: make the holding through Store.Take, whose first step asks the session record and
refuses one with none (ErrNoRecord, SPEC-n2c ST0).`,
	Run:        perPass(runSeatHoldUnrecorded),
	ResultType: noResultType,
}

func runSeatHoldUnrecorded(pass *analysis.Pass) {
	if !ownedBy(pass.Pkg.Path(), seatPkg) {
		return
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			if fn, isFunc := decl.(*ast.FuncDecl); isFunc && fn.Body != nil {
				checkSeatHolds(pass, fn)
			}
		}
	}
}

// checkSeatHolds reports each hold call in fn that is outside Store.Take, or in it before the
// first Restorable call.
func checkSeatHolds(pass *analysis.Pass, fn *ast.FuncDecl) {
	var holds []*ast.CallExpr
	asked := token.NoPos
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, isCall := n.(*ast.CallExpr)
		if !isCall {
			return true
		}
		switch seatMethodName(pass, call) {
		case seatHoldMethod:
			holds = append(holds, call)
		case seatRecordMethod:
			if !asked.IsValid() || call.Pos() < asked {
				asked = call.Pos()
			}
		}
		return true
	})
	inTake := fn.Name.Name == seatTakeMethod && isStoreMethod(pass, fn)
	for _, call := range holds {
		switch {
		case !inTake:
			pass.Reportf(call.Pos(), "a seat holding made outside Store.Take (project_seat_hold_unrecorded): the sweep releases a holding whose session has no record; take the seat through Store.Take")
		case !asked.IsValid() || asked > call.Pos():
			pass.Reportf(call.Pos(), "a seat holding made before the session record is asked (project_seat_hold_unrecorded): ask Sessions.Restorable first and refuse a session with no record")
		}
	}
}

// seatMethodName is the name of the seat package's method call calls (Store.hold, or a
// Restorable of the session records), or "" for any other call.
func seatMethodName(pass *analysis.Pass, call *ast.CallExpr) string {
	sel, isSel := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !isSel {
		return ""
	}
	fn, isFunc := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
	if !isFunc || fn.Pkg() == nil || fn.Pkg() != pass.Pkg {
		return ""
	}
	return fn.Name()
}

// isStoreMethod is whether fn is a method of the seat package's Store.
func isStoreMethod(pass *analysis.Pass, fn *ast.FuncDecl) bool {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return false
	}
	t := pass.TypesInfo.TypeOf(fn.Recv.List[0].Type)
	if p, isPtr := t.(*types.Pointer); isPtr {
		t = p.Elem()
	}
	named, isNamed := t.(*types.Named)
	return isNamed && named.Obj().Name() == seatStoreType
}
