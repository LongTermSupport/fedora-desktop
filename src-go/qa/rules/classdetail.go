package rules

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// classPackage is teamchat's Matrix client, whose Class reduces a failed call to its class.
const classPackage = "internal/teamchat/matrix"

const classDetailFix = " (project_class_without_detail): the class reads the same for a TLS pin failure, a timeout and a refused connection; name the cause with matrix.Cause(err), the class and the failed call's fixed detail"

// ClassWithoutDetail keeps a failed call's class alone out of every line but a bounded record.
var ClassWithoutDetail = &analysis.Analyzer{
	Name: "project_class_without_detail",
	Doc: `project_class_without_detail flags, in every package of both modules but teamchat's matrix
package (which defines it), a use of matrix.Class: a call, or the function taken as a value.

matrix.Class is a failed call's class as fixed text ("the homeserver did not answer usably").
A *matrix.Error carries more, written by the matrix package and safe to print (SPEC M9: the
method, the path, the status, the errcode and a fixed detail, never a token or the server's
words): a TLS handshake that failed against the seat's pinned CA, a timeout and a refused
connection are one class and three details. A log line or a message that prints the class alone
drops the cause an operator acts on: the teamchat link logged "down: the homeserver did not
answer usably" for a member whose seat did not trust the server's certificate (fedora-desktop#265).

Not flagged: a use whose value (through parentheses and string concatenation) is a field of a
literal of a bounded record listed in classRecords (table.go), whose decoder refuses any text
but a short one-line class: there the detail is not available to the line, and is reported
beside it.

Not caught: another function that reduces an error to fixed text; a detail dropped by a line
that prints no class at all.

How to fix: print matrix.Cause(err), the class followed by the failed call's own text (or, for
an error that is not a homeserver answer, its own text).`,
	Run:        perPass(runClassWithoutDetail),
	ResultType: noResultType,
}

func runClassWithoutDetail(pass *analysis.Pass) {
	if ownedBy(pass.Pkg.Path(), classPackage) {
		return
	}
	for _, f := range pass.Files {
		var stack []ast.Node
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			if id, ok := n.(*ast.Ident); ok && isClassFunc(pass.TypesInfo.Uses[id]) && !inClassRecord(pass, id, stack) {
				pass.Reportf(id.Pos(), "a failed call's class printed without its detail%s", classDetailFix)
			}
			stack = append(stack, n)
			return true
		})
	}
}

// isClassFunc is whether obj is matrix.Class.
func isClassFunc(obj types.Object) bool {
	f, ok := obj.(*types.Func)
	return ok && f.Name() == "Class" && f.Pkg() != nil && ownedBy(f.Pkg().Path(), classPackage) &&
		f.Signature().Recv() == nil
}

// inClassRecord is whether the use id, with its ancestors stack (innermost last), is called
// and its value, through parentheses and string concatenation, is a field of a literal of a
// classRecords type.
func inClassRecord(pass *analysis.Pass, id *ast.Ident, stack []ast.Node) bool {
	var child ast.Node = id
	i := len(stack) - 1
	if i >= 0 {
		if sel, ok := stack[i].(*ast.SelectorExpr); ok && sel.Sel == id {
			child, i = sel, i-1
		}
	}
	if i < 0 {
		return false
	}
	call, ok := stack[i].(*ast.CallExpr)
	if !ok || call.Fun != child {
		return false
	}
	child, i = call, i-1
	for ; i >= 0; i-- {
		switch p := stack[i].(type) {
		case *ast.ParenExpr:
		case *ast.BinaryExpr:
			if p.Op != token.ADD {
				return false
			}
		case *ast.KeyValueExpr:
			if p.Value != child {
				return false
			}
		case *ast.CompositeLit:
			return isClassRecord(pass.TypesInfo.TypeOf(p))
		default:
			return false
		}
		child = stack[i]
	}
	return false
}

// isClassRecord is whether t is a type classRecords lists.
func isClassRecord(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	for _, r := range classRecords {
		if named.Obj().Name() == r.typ && ownedBy(named.Obj().Pkg().Path(), r.pkg) {
			return true
		}
	}
	return false
}
