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
and does not hand the exit code on whole (a return of .Code, or of .Code converted). Naming
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
func checkRefusalKind(pass *analysis.Pass, body *ast.BlockStmt) {
	var recovered []token.Pos
	kindRead := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			checkRefusalKind(pass, n.Body)
			return false
		case *ast.ReturnStmt:
			for _, r := range n.Results {
				kindRead = kindRead || handsCodeOn(pass, r)
			}
		case *ast.Ident:
			kindRead = kindRead || isUsageKind(pass.TypesInfo.Uses[n])
		default:
			recovered = append(recovered, refusalsAt(pass, n)...)
		}
		return true
	})
	if kindRead {
		return
	}
	for _, pos := range recovered {
		pass.Reportf(pos, "a *declare.Error is read without its kind: re-ask only on declare.ErrUsage, stop on the rest naming the file")
	}
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
// kind itself: Code, Code converted to another type (an exit code), or either as a field of a
// returned composite literal or its address (&stop{term.ExitCode(de.Code)}). Code inside any
// other call (a Printf) is not handed on.
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
		tv, found := pass.TypesInfo.Types[n.Fun]
		return found && tv.IsType() && len(n.Args) == 1 && handsCodeOn(pass, n.Args[0])
	case *ast.SelectorExpr:
		return isCodeField(pass, n)
	}
	return false
}

// isCodeField is sel the Code field of a declare.Error.
func isCodeField(pass *analysis.Pass, sel *ast.SelectorExpr) bool {
	s, found := pass.TypesInfo.Selections[sel]
	if !found || s.Kind() != types.FieldVal || s.Obj().Name() != "Code" {
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
