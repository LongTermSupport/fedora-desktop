package rules

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

const ledgerType = "Ledger"

// LedgerBuilt forbids building a seat-store ledger entry field by field outside the store,
// where one constructor carries every field the store's check requires.
var LedgerBuilt = &analysis.Analyzer{
	Name: "project_ledger_built_by_hand",
	Doc: `project_ledger_built_by_hand flags, outside teamchat's store package
(internal/teamchat/member/state) and outside test files, a state.Ledger built field by field: a
composite literal with any element (keyed or positional, as a value, through & or as the
element of a slice or map literal), or an assignment to a field of one.

The store refuses a whole write when one ledger entry is not well formed, and an entry for a
ping must carry the ping's verb (n3d-D30). The engine built its entries from a pending item in
four places, field by field; two of them (the socket delivery's written-yes write and the
proof's) dropped the verb, so every write that settled a socket-delivered ping was refused, and
the link retried it every round for ever, at high CPU (fedora-desktop#392).

Not flagged: package state, where state.LedgerOf is the one constructor; an empty
state.Ledger{} (the zero value, standing for "none"); a test file, which builds its own store.

How to fix: build the entry with state.LedgerOf(pending, at), which copies every field of the
item the ledger keeps.`,
	Run:        perPass(runLedgerBuilt),
	ResultType: noResultType,
}

func runLedgerBuilt(pass *analysis.Pass) {
	if ownedBy(pass.Pkg.Path(), teamchatStatePkg) {
		return
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			reportLedgerBuilt(pass, n)
			return true
		})
	}
}

// reportLedgerBuilt reports n when it is a non-empty state.Ledger literal or an assignment to
// a field of one.
func reportLedgerBuilt(pass *analysis.Pass, n ast.Node) {
	const msg = "a ledger entry built field by field (project_ledger_built_by_hand): a field the store requires can be dropped; build it with state.LedgerOf"
	switch x := n.(type) {
	case *ast.CompositeLit:
		if len(x.Elts) > 0 && isStateLedger(pass.TypesInfo.TypeOf(x)) {
			pass.Reportf(x.Pos(), "%s", msg)
		}
	case *ast.AssignStmt:
		for _, lhs := range x.Lhs {
			if sel, ok := ast.Unparen(lhs).(*ast.SelectorExpr); ok && isStateLedger(pass.TypesInfo.TypeOf(sel.X)) {
				pass.Reportf(lhs.Pos(), "%s", msg)
			}
		}
	}
}

// isStateLedger is whether t is teamchat's state.Ledger, or a pointer to one.
func isStateLedger(t types.Type) bool {
	if p, isPtr := t.(*types.Pointer); isPtr {
		t = p.Elem()
	}
	named, isNamed := t.(*types.Named)
	if !isNamed {
		return false
	}
	obj := named.Obj()
	return obj.Name() == ledgerType && obj.Pkg() != nil && ownedBy(obj.Pkg().Path(), teamchatStatePkg)
}
