package rules

import (
	"go/ast"
	"go/constant"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// SeamCommands keeps each external program inside the package that owns it.
var SeamCommands = &analysis.Analyzer{
	Name: "project_seam_commands",
	Doc: `project_seam_commands forbids naming an owned program as a command outside its package.

An execx.Cmd whose Name is sudo, tmux, gum, fzf, systemctl or git (a literal or a constant,
in a composite literal or assigned to .Name) may be built only in the package that owns the
program: internal/sudo, internal/tmuxccy, internal/ui, internal/systemd, internal/gitx. The
owner decides the flags and target forms once: sudo only with -n; tmux targets as =name:, not
bare =name (fedora-desktop#69). Test files are exempt: a test spells out the argv it expects.
exec.LookPath and other text naming the program are not commands and are not flagged.

How to fix: call the owner's method; add the operation to the owner if it is missing.`,
	Run:        perPass(runSeamCommands),
	ResultType: noResultType,
}

func runSeamCommands(pass *analysis.Pass) {
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CompositeLit:
				checkCmdLiteral(pass, n)
			case *ast.AssignStmt:
				checkNameAssign(pass, n)
			}
			return true
		})
	}
}

// isCmdType is whether t is the runner's Cmd, matched by owner path so fixtures share it.
func isCmdType(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Name() == "Cmd" && ownedBy(named.Obj().Pkg().Path(), execOwner)
}

func checkCmdLiteral(pass *analysis.Pass, lit *ast.CompositeLit) {
	if !isCmdType(pass.TypesInfo.TypeOf(lit)) {
		return
	}
	for i, elt := range lit.Elts {
		switch e := elt.(type) {
		case *ast.KeyValueExpr:
			if key, ok := e.Key.(*ast.Ident); ok && key.Name == "Name" {
				checkProgram(pass, e.Value)
			}
		default:
			if i == 0 { // an unkeyed literal starts with Name
				checkProgram(pass, e)
			}
		}
	}
}

func checkNameAssign(pass *analysis.Pass, as *ast.AssignStmt) {
	if len(as.Lhs) != len(as.Rhs) {
		return
	}
	for i, lhs := range as.Lhs {
		sel, ok := lhs.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Name" || !isCmdType(pass.TypesInfo.TypeOf(sel.X)) {
			continue
		}
		checkProgram(pass, as.Rhs[i])
	}
}

// checkProgram reports expr when it is a constant naming a program this package does not own.
func checkProgram(pass *analysis.Pass, expr ast.Expr) {
	tv, ok := pass.TypesInfo.Types[expr]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return
	}
	name := constant.StringVal(tv.Value)
	owner, owned := commandOwners[name]
	if !owned || ownedBy(pass.Pkg.Path(), owner) {
		return
	}
	pass.Reportf(expr.Pos(), "%q is owned by %s: use its methods instead of running %s here",
		name, owner, strings.TrimSpace(name))
}
