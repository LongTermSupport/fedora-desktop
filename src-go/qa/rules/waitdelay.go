package rules

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// execPkg is the import path of the standard library's process package.
const execPkg = "os/exec"

// WaitDelay forbids setting exec.Cmd.WaitDelay in a function that never names exec.ErrWaitDelay.
var WaitDelay = &analysis.Analyzer{
	Name: "project_waitdelay",
	Doc: `project_waitdelay flags a function that sets (*exec.Cmd).WaitDelay and never names
exec.ErrWaitDelay.

Setting WaitDelay adds a result to Cmd.Run and Cmd.Wait: when the command has exited but a
child still holds its output pipe, they return exec.ErrWaitDelay, and the output written before
that is complete. A runner that treats every non-exit error as a failed run drops that output
(bug B25 of the qa-all port: a stage's whole report was thrown away). The function that sets
WaitDelay is the one that must decide what ErrWaitDelay means, so it must name it: keep the
captured output, and say that a process was left behind.

Only the presence of the decision is checked, not that it is right: a function that names
ErrWaitDelay and still drops the output is not seen, and a WaitDelay set in one function and
its error handled in another is flagged (move the decision beside the setting, which is where a
reader looks for it). Test files are not judged.

How to fix: add a branch for errors.Is(err, exec.ErrWaitDelay) that keeps the captured output.`,
	Run:        perPass(runWaitDelay),
	ResultType: noResultType,
}

func runWaitDelay(pass *analysis.Pass) {
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Body != nil {
				checkWaitDelay(pass, fd)
			}
		}
	}
}

// checkWaitDelay reports each WaitDelay assignment of fd when fd never names ErrWaitDelay.
func checkWaitDelay(pass *analysis.Pass, fd *ast.FuncDecl) {
	var sets []*ast.SelectorExpr
	named := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range n.Lhs {
				if sel, ok := lhs.(*ast.SelectorExpr); ok && isWaitDelayField(pass, sel) {
					sets = append(sets, sel)
				}
			}
		case *ast.Ident:
			if v, ok := pass.TypesInfo.Uses[n].(*types.Var); ok && v.Pkg() != nil && v.Pkg().Path() == execPkg && v.Name() == "ErrWaitDelay" {
				named = true
			}
		}
		return true
	})
	if named {
		return
	}
	for _, sel := range sets {
		pass.Reportf(sel.Sel.Pos(), "WaitDelay set and exec.ErrWaitDelay never named: decide there whether the captured output is kept")
	}
}

// isWaitDelayField is whether sel selects the WaitDelay field of an os/exec.Cmd.
func isWaitDelayField(pass *analysis.Pass, sel *ast.SelectorExpr) bool {
	s, ok := pass.TypesInfo.Selections[sel]
	if !ok || s.Kind() != types.FieldVal {
		return false
	}
	v, isVar := s.Obj().(*types.Var)
	return isVar && v.Name() == "WaitDelay" && v.Pkg() != nil && v.Pkg().Path() == execPkg
}
