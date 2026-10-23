package rules

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// boardPkg owns the teams board's Row and the reading of a handle's kind, KindOf.
const boardPkg = "internal/teams/board"

// RowKind forbids a board row that has a member's handle and states its KIND some other way.
var RowKind = &analysis.Analyzer{
	Name: "project_row_kind",
	Doc: `project_row_kind flags a composite literal of the teams board's Row (internal/teams/board)
that gives Handle a value known only at run time (a member's handle) and gives Kind a
compile-time constant (board.KindCcy, board.Kind("lxc"), an untyped "ccy", a named constant of
any package), or no Kind at all. A Row literal with unkeyed fields is flagged too, as neither
field can be read. The type is matched through types.Info, so an alias does not hide it, and an
elided type inside a slice or map literal is judged as well.

A member's handle is <repo>.<seat>+<host>.<type>, and its type word says what runs the member:
podman a ccy container, host a cc session on the desktop, lxc, docker or vm. A row that has the
handle and states the kind beside it says whatever the constant says, whatever the handle does:
the board called every seat of a checkout ccy, so a host cc session on seat <team>.2+local.host
showed as ccy. A row with no member (a team, a person, an LXC member that has not joined: Handle
"-") has no handle to read, and its constant kind is the row's own.

Not caught: an assignment to a Row's Kind or Handle after the literal (row.Kind = ...), and a
Kind computed by a function that ignores the handle. Test files are not judged.

How to fix: Kind: board.KindOf(handle), the one reading of a handle's type word.`,
	Run:        perPass(runRowKind),
	ResultType: noResultType,
}

func runRowKind(pass *analysis.Pass) {
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if lit, ok := n.(*ast.CompositeLit); ok && isBoardRow(pass.TypesInfo.TypeOf(lit)) {
				checkRowLiteral(pass, lit)
			}
			return true
		})
	}
}

// checkRowLiteral judges one Row literal: a run-time Handle needs a Kind that is not a constant.
func checkRowLiteral(pass *analysis.Pass, lit *ast.CompositeLit) {
	var kind, handle ast.Expr
	for _, elt := range lit.Elts {
		kv, keyed := elt.(*ast.KeyValueExpr)
		if !keyed {
			pass.Reportf(lit.Pos(), "a board.Row with unkeyed fields: name Kind and Handle, and take Kind from board.KindOf(handle)")
			return
		}
		key, isIdent := kv.Key.(*ast.Ident)
		switch {
		case !isIdent:
		case key.Name == "Kind":
			kind = kv.Value
		case key.Name == "Handle":
			handle = kv.Value
		}
	}
	if handle == nil || isConstant(pass, handle) {
		return
	}
	switch {
	case kind == nil:
		pass.Reportf(lit.Pos(), "the row has a handle and no KIND: take it from board.KindOf(handle)")
	case isConstant(pass, kind):
		pass.Reportf(kind.Pos(), "the row's KIND is a constant though its handle says what the member is: take it from board.KindOf(handle)")
	}
}

// isConstant is whether e has a compile-time value.
func isConstant(pass *analysis.Pass, e ast.Expr) bool {
	tv, found := pass.TypesInfo.Types[e]
	return found && tv.Value != nil
}

// isBoardRow is whether t is the board's Row, or a pointer to it.
func isBoardRow(t types.Type) bool {
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Name() == "Row" && obj.Pkg() != nil && ownedBy(obj.Pkg().Path(), boardPkg)
}
