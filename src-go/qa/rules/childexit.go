package rules

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// termPkg owns the tools' exit codes, term.ExitCode, and the translation of another program's
// code into one of them, term.FromChild.
const termPkg = "internal/term"

// codeField is the field an error type carries a program's exit code in.
const codeField = "Code"

// ChildExit forbids handing another program's exit code on as the tool's own.
var ChildExit = &analysis.Analyzer{
	Name: "project_child_exit",
	Doc: `project_child_exit flags a conversion to term.ExitCode (through an alias too) whose operand
reads the Code field of an error type (a struct whose value or pointer implements error:
execx.ExitError, agentbus.CommandError, declare.Error), directly, inside an expression, or
through a local variable assigned from such a read in the same function. The internal/term
package itself, which owns the translation, is not judged, and nor are test files.

A tool documents its exit codes, and the person's shell names the ones it knows: ccy --teams
with a team this host does not have exited 78, the code agent-bus seat choose had exited with,
which no ccy usage text named, and the owner's prompt reported "Unknown error code". A child's
code means what the child's documentation says, which is not the tool's.

Not caught: a code returned by a helper function as an int (helperExitCode(err)), a Code field
read in another function and passed in as a plain int, and a field named other than Code.

How to fix: term.FromChild(code), which keeps a code the tools document and makes any other a
failure; say the child's own code in the message if it helps.`,
	Run:        perPass(runChildExit),
	ResultType: noResultType,
}

func runChildExit(pass *analysis.Pass) {
	if ownedBy(pass.Pkg.Path(), termPkg) {
		return
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Body != nil {
				checkChildExit(pass, fd.Body)
			}
		}
	}
}

// checkChildExit judges one function body, its function literals with it: the locals that hold
// a child's code are found to a fixed point, then every conversion to term.ExitCode is judged.
func checkChildExit(pass *analysis.Pass, body *ast.BlockStmt) {
	tainted := map[types.Object]bool{}
	for changed := true; changed; {
		changed = false
		ast.Inspect(body, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok || len(as.Lhs) != len(as.Rhs) {
				return true
			}
			for i, lhs := range as.Lhs {
				obj := identObject(pass, lhs)
				if obj != nil && !tainted[obj] && readsChildCode(pass, as.Rhs[i], tainted) {
					tainted[obj] = true
					changed = true
				}
			}
			return true
		})
	}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if ok && len(call.Args) == 1 && isExitCodeConversion(pass, call) && readsChildCode(pass, call.Args[0], tainted) {
			pass.Reportf(call.Pos(), "another program's exit code becomes this tool's own: translate it with term.FromChild")
		}
		return true
	})
}

// identObject is the variable an assignment's left side names, or nil.
func identObject(pass *analysis.Pass, e ast.Expr) types.Object {
	id, ok := e.(*ast.Ident)
	if !ok {
		return nil
	}
	if obj := pass.TypesInfo.Defs[id]; obj != nil {
		return obj
	}
	return pass.TypesInfo.Uses[id]
}

// readsChildCode is whether e reads an error's Code field or a local holding one.
func readsChildCode(pass *analysis.Pass, e ast.Expr, tainted map[types.Object]bool) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			found = found || isErrorCodeField(pass, n)
		case *ast.Ident:
			found = found || tainted[pass.TypesInfo.Uses[n]]
		case *ast.FuncLit:
			return false
		}
		return !found
	})
	return found
}

// isErrorCodeField is sel the Code field of a struct whose value or pointer is an error.
func isErrorCodeField(pass *analysis.Pass, sel *ast.SelectorExpr) bool {
	s, found := pass.TypesInfo.Selections[sel]
	if !found || s.Kind() != types.FieldVal || s.Obj().Name() != codeField {
		return false
	}
	recv := s.Recv()
	if ptr, isPtr := recv.(*types.Pointer); isPtr {
		recv = ptr.Elem()
	}
	errType, ok := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	if !ok {
		return false
	}
	return types.Implements(recv, errType) || types.Implements(types.NewPointer(recv), errType)
}

// isFromChild is whether fn is term.FromChild, the translation of a child's code.
func isFromChild(fn *types.Func) bool {
	sig, ok := fn.Type().(*types.Signature)
	return ok && sig.Recv() == nil && fn.Name() == "FromChild" && fn.Pkg() != nil && ownedBy(fn.Pkg().Path(), termPkg)
}

// isExitCodeConversion is whether call converts to term.ExitCode.
func isExitCodeConversion(pass *analysis.Pass, call *ast.CallExpr) bool {
	tv, found := pass.TypesInfo.Types[call.Fun]
	if !found || !tv.IsType() {
		return false
	}
	named, ok := types.Unalias(tv.Type).(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Name() == "ExitCode" && obj.Pkg() != nil && ownedBy(obj.Pkg().Path(), termPkg)
}
