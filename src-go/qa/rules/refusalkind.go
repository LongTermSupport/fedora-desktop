package rules

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// declarePkg owns declare.py's refusal type, Error, and its kinds, the Err* variables.
const declarePkg = "internal/declare"

// RefusalKind forbids recovering a declare.py refusal by its type without reading its kind.
var RefusalKind = &analysis.Analyzer{
	Name: "project_refusal_kind",
	Doc: `project_refusal_kind flags a function that recovers a *declare.Error (errors.AsType,
errors.As, a type assertion or a type switch) and never tells the one kind asking again can fix
from the rest: it names no declare.ErrUsage (errors.Is(err, declare.ErrUsage), .Is(declare.ErrUsage))
and does not hand the exit code on whole (a return of .Code, of .Code converted, or of
term.FromChild(.Code)) whatever the
recovery finds. A code handed on counts only where the return is unconditional in the block
that recovers: a direct statement of the if body whose Init or Cond recovers, when the Cond is
the bare recovery result (the found result of an Init that is one recovery alone, or the errors.As call) or that && .Code != 0; a later direct statement
after an assignment that recovers, or after an if whose Cond is the negated bare result; a direct
statement of the type switch case. A kind or a particular code tested in the Cond is a filter.
A code handed on for one kind inside a branch of its own leaves the rest asked again. Naming
another kind (ErrInterrupted, ErrRefused) or reading .Code to print it does not count: either way
the rest are still asked again.

declare.py refuses in kinds, and its exit code says which: 64 (declare.ErrUsage) is the value
just given; 78 (declare.ErrRefused) is the file, its existing entries, or the answer against
them; 130 is an interrupt. A question that reads every refusal as one verdict asks again when
the file is at fault, so retyping can never succeed: ccy-teams lxc-add asked for the user three
times over "entries 5 and 6 both declare <user>@<container>" (Plan 00201), where entry 6 was
its own answer and entry 5 the member an earlier run had written. Re-asking is for a recoverable
input mistake (CLAUDE/InteractiveScripts.md); a refusal of the file's state stops at once and
names the file and the entry.

The kind must be read in the function that recovers the type, judged per function literal as
well as per declaration, where a reader looks for the decision. Test files are not judged.

How to fix: re-ask only when errors.Is(err, declare.ErrUsage); for every other kind stop, saying
the refusal and the host_vars file it is about.`,
	Run:        perPass(runRefusalKind),
	ResultType: noResultType,
}

func runRefusalKind(pass *analysis.Pass) {
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Body != nil {
				checkRefusalKind(pass, fd.Body)
			}
		}
	}
}

// checkRefusalKind judges one function body; each function literal in it is judged on its own.
// ErrUsage named anywhere in the body reads the kind for every recovery in it; a code handed on
// reads it only for the recoveries whose block hands it on unconditionally (codeHandedOn).
func checkRefusalKind(pass *analysis.Pass, body *ast.BlockStmt) {
	var recovered []token.Pos
	var handedOn []ast.Node
	usageNamed := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			checkRefusalKind(pass, n.Body)
			return false
		case *ast.Ident:
			usageNamed = usageNamed || isUsageKind(pass.TypesInfo.Uses[n])
		case *ast.BlockStmt:
			handedOn = append(handedOn, codeHandedOn(pass, n.List)...)
		case *ast.CaseClause:
			handedOn = append(handedOn, codeHandedOn(pass, n.Body)...)
			if returnsCode(pass, n.Body) {
				for _, e := range n.List {
					handedOn = append(handedOn, e)
				}
			}
			recovered = append(recovered, refusalsAt(pass, n)...)
		case *ast.CommClause:
			handedOn = append(handedOn, codeHandedOn(pass, n.Body)...)
		default:
			recovered = append(recovered, refusalsAt(pass, n)...)
		}
		return true
	})
	if usageNamed {
		return
	}
	for _, pos := range recovered {
		if !within(pos, handedOn) {
			pass.Reportf(pos, "a *declare.Error is read without its kind: re-ask only on declare.ErrUsage, stop on the rest naming the file")
		}
	}
}

// codeHandedOn is the parts of a statement list whose recoveries are followed, whatever they
// find, by a return that hands the code on: an if's Init and Cond when its body returns the code
// as a direct statement, and a simple statement (an assignment, a declaration, a call) when a
// later direct statement of the list returns it. A return inside a branch of its own hands the
// code on for that branch only, so the other kinds may still be asked again.
func codeHandedOn(pass *analysis.Pass, stmts []ast.Stmt) []ast.Node {
	var parts []ast.Node
	for i, s := range stmts {
		switch s := s.(type) {
		case *ast.IfStmt:
			not, isNot := ast.Unparen(s.Cond).(*ast.UnaryExpr)
			switch {
			case unfiltered(pass, s.Init, s.Cond) && returnsCode(pass, s.Body.List):
			case isNot && not.Op == token.NOT && unfiltered(pass, s.Init, not.X) && returnsCode(pass, stmts[i+1:]):
			default:
				continue
			}
			if s.Init != nil {
				parts = append(parts, s.Init)
			}
			parts = append(parts, s.Cond)
		case *ast.AssignStmt, *ast.DeclStmt, *ast.ExprStmt:
			if returnsCode(pass, stmts[i+1:]) {
				parts = append(parts, s)
			}
		}
	}
	return parts
}

// unfiltered is an if condition true for every refusal recovered, whatever its kind: the bare
// result of the recovery (recoveryOk, or the errors.As call itself), or that && a non-zero
// .Code. Any other test (a kind, a particular code, a bool from elsewhere) leaves the rest to the
// statements after the if.
func unfiltered(pass *analysis.Pass, init ast.Stmt, cond ast.Expr) bool {
	switch c := ast.Unparen(cond).(type) {
	case *ast.Ident:
		return recoveryOk(pass, init, c)
	case *ast.CallExpr:
		return recoversRefusal(pass, c)
	case *ast.BinaryExpr:
		return c.Op == token.LAND && unfiltered(pass, init, c.X) && codeNonZero(pass, c.Y)
	}
	return false
}

// recoveryOk is whether id is the found result an if's Init assigns from one recovery and
// nothing else: the last name of `de, ok := errors.AsType[...](err)` or `de, ok := err.(...)`,
// or the one name of `ok := errors.As(err, &de)`.
func recoveryOk(pass *analysis.Pass, init ast.Stmt, id *ast.Ident) bool {
	as, isAssign := init.(*ast.AssignStmt)
	if !isAssign || len(as.Rhs) != 1 || len(as.Lhs) == 0 || len(refusalsAt(pass, ast.Unparen(as.Rhs[0]))) == 0 {
		return false
	}
	last, isIdent := as.Lhs[len(as.Lhs)-1].(*ast.Ident)
	if !isIdent {
		return false
	}
	obj := pass.TypesInfo.Defs[last]
	if obj == nil {
		obj = pass.TypesInfo.Uses[last]
	}
	return obj != nil && obj == pass.TypesInfo.Uses[id]
}

// codeNonZero is `de.Code != 0` or `0 != de.Code`.
func codeNonZero(pass *analysis.Pass, e ast.Expr) bool {
	b, ok := ast.Unparen(e).(*ast.BinaryExpr)
	if !ok || b.Op != token.NEQ {
		return false
	}
	isZero := func(x ast.Expr) bool {
		lit, isLit := ast.Unparen(x).(*ast.BasicLit)
		return isLit && lit.Value == "0"
	}
	isCode := func(x ast.Expr) bool {
		sel, isSel := ast.Unparen(x).(*ast.SelectorExpr)
		return isSel && isCodeField(pass, sel)
	}
	return (isCode(b.X) && isZero(b.Y)) || (isZero(b.X) && isCode(b.Y))
}

// returnsCode is whether a direct statement of stmts returns a declare.Error's Code (handsCodeOn).
func returnsCode(pass *analysis.Pass, stmts []ast.Stmt) bool {
	for _, s := range stmts {
		if ret, ok := s.(*ast.ReturnStmt); ok {
			for _, r := range ret.Results {
				if handsCodeOn(pass, r) {
					return true
				}
			}
		}
	}
	return false
}

// within is whether pos lies inside one of nodes.
func within(pos token.Pos, nodes []ast.Node) bool {
	for _, n := range nodes {
		if n.Pos() <= pos && pos < n.End() {
			return true
		}
	}
	return false
}

// refusalsAt is where n recovers a *declare.Error by its type: a call (recoversRefusal), a type
// assertion, or each case of a type switch that names it.
func refusalsAt(pass *analysis.Pass, n ast.Node) []token.Pos {
	switch n := n.(type) {
	case *ast.CallExpr:
		if recoversRefusal(pass, n) {
			return []token.Pos{n.Pos()}
		}
	case *ast.TypeAssertExpr:
		if n.Type != nil && isRefusalPtr(pass.TypesInfo.TypeOf(n.Type)) {
			return []token.Pos{n.Pos()}
		}
	case *ast.CaseClause:
		var found []token.Pos
		for _, e := range n.List {
			if isRefusalPtr(pass.TypesInfo.TypeOf(e)) {
				found = append(found, e.Pos())
			}
		}
		return found
	}
	return nil
}

// recoversRefusal is errors.AsType[*declare.Error](...) or errors.As(err, &v) with v a
// *declare.Error.
func recoversRefusal(pass *analysis.Pass, call *ast.CallExpr) bool {
	fun := call.Fun
	if ix, ok := fun.(*ast.IndexExpr); ok {
		fun = ix.X
	}
	var id *ast.Ident
	switch f := fun.(type) {
	case *ast.SelectorExpr:
		id = f.Sel
	case *ast.Ident:
		id = f
	default:
		return false
	}
	fn, ok := pass.TypesInfo.Uses[id].(*types.Func)
	switch {
	case !ok:
		return false
	case isPkgFunc(fn, "errors", "AsType"):
		inst, found := pass.TypesInfo.Instances[id]
		return found && inst.TypeArgs.Len() == 1 && isRefusalPtr(inst.TypeArgs.At(0))
	case isPkgFunc(fn, "errors", "As") && len(call.Args) == 2:
		target, isPtr := pass.TypesInfo.TypeOf(call.Args[1]).(*types.Pointer)
		return isPtr && isRefusalPtr(target.Elem())
	}
	return false
}

// isRefusalPtr is whether t is *declare.Error.
func isRefusalPtr(t types.Type) bool {
	ptr, ok := t.(*types.Pointer)
	if !ok {
		return false
	}
	return isRefusalType(ptr.Elem())
}

func isRefusalType(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Name() == "Error" && obj.Pkg() != nil && ownedBy(obj.Pkg().Path(), declarePkg)
}

// handsCodeOn is a returned value that is a declare.Error's Code whole, so the caller gets the
// kind itself: Code, Code converted to another type (an exit code) or translated by
// term.FromChild (which keeps 64, 78 and 130), or any of them as a field of a returned composite
// literal or its address (&stop{term.FromChild(de.Code)}). Code inside any other call (a
// Printf) is not handed on.
func handsCodeOn(pass *analysis.Pass, e ast.Expr) bool {
	switch n := ast.Unparen(e).(type) {
	case *ast.UnaryExpr:
		return n.Op == token.AND && handsCodeOn(pass, n.X)
	case *ast.CompositeLit:
		for _, elt := range n.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				elt = kv.Value
			}
			if handsCodeOn(pass, elt) {
				return true
			}
		}
		return false
	case *ast.CallExpr:
		if len(n.Args) != 1 {
			return false
		}
		if fn := calledFunc(pass, n); fn != nil {
			return isFromChild(fn) && handsCodeOn(pass, n.Args[0])
		}
		tv, found := pass.TypesInfo.Types[n.Fun]
		return found && tv.IsType() && handsCodeOn(pass, n.Args[0])
	case *ast.SelectorExpr:
		return isCodeField(pass, n)
	}
	return false
}

// isCodeField is sel the Code field of a declare.Error.
func isCodeField(pass *analysis.Pass, sel *ast.SelectorExpr) bool {
	s, found := pass.TypesInfo.Selections[sel]
	if !found || s.Kind() != types.FieldVal || s.Obj().Name() != codeField {
		return false
	}
	recv := s.Recv()
	if ptr, isPtr := recv.(*types.Pointer); isPtr {
		recv = ptr.Elem()
	}
	return isRefusalType(recv)
}

// isUsageKind is obj declare.ErrUsage, the package-level kind of internal/declare that says the
// value just given is at fault.
func isUsageKind(obj types.Object) bool {
	v, ok := obj.(*types.Var)
	if !ok || v.Pkg() == nil || !ownedBy(v.Pkg().Path(), declarePkg) || v.Parent() != v.Pkg().Scope() {
		return false
	}
	return v.Name() == "ErrUsage"
}
