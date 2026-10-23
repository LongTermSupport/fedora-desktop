package rules

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// RawConnProbe forbids a RawConn readiness callback that answers without testing its
// descriptor.
var RawConnProbe = &analysis.Analyzer{
	Name: "project_rawconn_probe",
	Doc: `project_rawconn_probe flags a RawConn Read or Write callback that does not read its descriptor.

A call of a method named Read or Write whose one parameter is func(uintptr) bool and whose one
result is error (syscall.RawConn's shape, on any type) is judged by its callback: a function
literal, or a function of the same package, whose descriptor parameter is named and read in
its body. Flagged: a descriptor parameter that is unnamed, blank or never read, and a callback
that is a function value or another package's function (its body cannot be seen here). Test
files are judged too.

RawConn.Read resets the readiness the runtime's poller has already seen before it first calls
the callback, and parks until the next edge whenever the callback answers false. A callback
that answers without testing the descriptor waits for an edge that may have passed: the link's
pidfd watch answered false on its first call to "arm" the wait, so a keeper that died before
the watch goroutine first ran was never seen, and the link waited for ever.

Not flagged: RawConn.Control (it runs once and decides no readiness).
Not caught: a callback that reads its descriptor and still answers without testing its state
(logging it, comparing it with a bound only).

How to fix: test the descriptor on every call, as the keeper's pidfd wait does:
unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, 0), answering true when it
reports the descriptor ready or fails; or read from it and answer false only on EAGAIN.`,
	Run:        perPass(runRawConnProbe),
	ResultType: noResultType,
}

func runRawConnProbe(pass *analysis.Pass) {
	decls := map[*types.Func]*ast.FuncDecl{}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				if fn, isFunc := pass.TypesInfo.Defs[fd.Name].(*types.Func); isFunc {
					decls[fn] = fd
				}
			}
		}
	}
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if ok && isRawConnWait(pass, call) && !callbackTestsFD(pass, decls, call.Args[0]) {
				pass.Reportf(call.Args[0].Pos(), "RawConn callback answers without testing its descriptor, so a readiness Read has reset is missed: poll the descriptor on every call")
			}
			return true
		})
	}
}

// isRawConnWait is whether call is a method Read or Write of RawConn's shape:
// func(func(uintptr) bool) error.
func isRawConnWait(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != "Read" && sel.Sel.Name != "Write") || len(call.Args) != 1 {
		return false
	}
	m, isFunc := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
	if !isFunc {
		return false
	}
	sig, isSig := m.Type().(*types.Signature)
	if !isSig || sig.Recv() == nil || sig.Params().Len() != 1 || sig.Results().Len() != 1 {
		return false
	}
	if !types.Identical(sig.Results().At(0).Type(), types.Universe.Lookup("error").Type()) {
		return false
	}
	return isFDCallback(sig.Params().At(0).Type())
}

// isFDCallback is whether t is func(uintptr) bool.
func isFDCallback(t types.Type) bool {
	cb, ok := t.Underlying().(*types.Signature)
	if !ok || cb.Params().Len() != 1 || cb.Results().Len() != 1 {
		return false
	}
	return types.Identical(cb.Params().At(0).Type(), types.Typ[types.Uintptr]) &&
		types.Identical(cb.Results().At(0).Type(), types.Typ[types.Bool])
}

// callbackTestsFD is whether arg is a function literal or a same-package function that reads
// its descriptor parameter.
func callbackTestsFD(pass *analysis.Pass, decls map[*types.Func]*ast.FuncDecl, arg ast.Expr) bool {
	switch a := ast.Unparen(arg).(type) {
	case *ast.FuncLit:
		return readsFirstParam(pass, a.Type, a.Body)
	case *ast.Ident:
		return declTestsFD(pass, decls, a)
	case *ast.SelectorExpr:
		return declTestsFD(pass, decls, a.Sel)
	}
	return false
}

// declTestsFD is whether id names a same-package function that reads its descriptor.
func declTestsFD(pass *analysis.Pass, decls map[*types.Func]*ast.FuncDecl, id *ast.Ident) bool {
	fn, ok := pass.TypesInfo.Uses[id].(*types.Func)
	if !ok {
		return false
	}
	d, found := decls[fn.Origin()]
	return found && readsFirstParam(pass, d.Type, d.Body)
}

// readsFirstParam is whether the function's first parameter is named and read in body.
func readsFirstParam(pass *analysis.Pass, ft *ast.FuncType, body *ast.BlockStmt) bool {
	if ft.Params == nil || len(ft.Params.List) == 0 || len(ft.Params.List[0].Names) == 0 {
		return false
	}
	name := ft.Params.List[0].Names[0]
	param := pass.TypesInfo.Defs[name]
	if param == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && pass.TypesInfo.Uses[id] == param {
			found = true
		}
		return !found
	})
	return found
}
