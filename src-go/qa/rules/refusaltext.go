package rules

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
)

// The packages the rule reads, matched as path suffixes so the fixtures and the real module
// share them.
const (
	refusalProtoPkg = "internal/teamchat/proto"
	refusalExitsPkg = "internal/teamchat/member/exits"
)

// refusalCodeParam is the parameter of an exits function that takes the error object's code,
// where a refusal's reason is the right value.
const refusalCodeParam = "code"

const refusalTextFix = " (project_refusal_reason_text): the code is already the error object's; choose a template by the reason (and the refusal's cause) that names what is wrong and the next step"

// RefusalReasonText keeps a teamchat refusal's reason code out of the message it shows.
var RefusalReasonText = &analysis.Analyzer{
	Name: "project_refusal_reason_text",
	Doc: `project_refusal_reason_text flags, in internal/teamchat, cmd/teamchat and cmd/teamchat-admin
and below, outside test files, the Reason of a proto.RefusalError (a pointer or a value) put into
an exits message: an argument of a function of package exits (New, Format, Words, Name, Join and
the rest) holds a read of .Reason, or a local variable a read of .Reason was assigned to, other
than the parameter named code (New's and ExitOf's), where the reason is the right value.

A refusal's reason is a code (ref, schema, target), and the error object already carries it as
its code. A message filled with it says nothing more: "the send was refused (ref); fix the
message" left an agent answering a fetch with done and no --ref with no way to know the
reference was missing, nor what it had to be (fedora-desktop#421). The message must say the
cause and the next step, so it is a template chosen by the reason and its cause.

Not caught: a reason copied into a struct field or a parameter of another function and read back
there; a message built outside package exits from the reason.

How to fix: switch on the reason (and the refusal's cause, such as a reference refusal's) and
choose a template that names what is wrong and what to do.`,
	Run:        perPass(runRefusalReasonText),
	ResultType: noResultType,
}

func runRefusalReasonText(pass *analysis.Pass) {
	if !slices.ContainsFunc(transportTrees, func(tree string) bool { return inTree(pass.Pkg.Path(), tree) }) {
		return
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			copies := reasonCopies(pass, fd.Body)
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if call, isCall := n.(*ast.CallExpr); isCall {
					reportReasonFills(pass, call, copies)
				}
				return true
			})
		}
	}
}

// reasonCopies are the local variables of body a read of a refusal's Reason is assigned to.
func reasonCopies(pass *analysis.Pass, body *ast.BlockStmt) map[types.Object]bool {
	copies := map[types.Object]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || (as.Tok != token.DEFINE && as.Tok != token.ASSIGN) || len(as.Lhs) != len(as.Rhs) {
			return true
		}
		for i, rhs := range as.Rhs {
			id, isIdent := as.Lhs[i].(*ast.Ident)
			if !isIdent || !holdsReason(pass, rhs, nil) {
				continue
			}
			if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
				copies[obj] = true
			}
		}
		return true
	})
	return copies
}

// reportReasonFills reports each argument of an exits call, but its code, that holds a reason.
func reportReasonFills(pass *analysis.Pass, call *ast.CallExpr, copies map[types.Object]bool) {
	fn := calledFunc(pass, call)
	if fn == nil || fn.Pkg() == nil || !ownedBy(fn.Pkg().Path(), refusalExitsPkg) {
		return
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return
	}
	for i, arg := range call.Args {
		if paramName(sig, i) == refusalCodeParam {
			continue
		}
		// A nested exits call is judged on its own; its own code is not a fill.
		if inner, isCall := arg.(*ast.CallExpr); isCall && isExitsCall(pass, inner) {
			continue
		}
		if holdsReason(pass, arg, copies) {
			pass.Reportf(arg.Pos(), "a refusal's reason code in its message%s", refusalTextFix)
		}
	}
}

func isExitsCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	fn := calledFunc(pass, call)
	return fn != nil && fn.Pkg() != nil && ownedBy(fn.Pkg().Path(), refusalExitsPkg)
}

// paramName is the name of the parameter argument i fills, the variadic one for every
// argument past the last.
func paramName(sig *types.Signature, i int) string {
	params := sig.Params()
	if params.Len() == 0 {
		return ""
	}
	if i >= params.Len() {
		i = params.Len() - 1
	}
	return params.At(i).Name()
}

// holdsReason is whether e reads a refusal's Reason, or one of copies.
func holdsReason(pass *analysis.Pass, e ast.Expr, copies map[types.Object]bool) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			if n.Sel.Name == "Reason" && isRefusalError(pass.TypesInfo.TypeOf(n.X)) {
				found = true
			}
		case *ast.Ident:
			if obj := pass.TypesInfo.Uses[n]; obj != nil && copies[obj] {
				found = true
			}
		}
		return !found
	})
	return found
}

// isRefusalError is whether t is proto.RefusalError or a pointer to it.
func isRefusalError(t types.Type) bool {
	if t == nil {
		return false
	}
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Name() == "RefusalError" && obj.Pkg() != nil && ownedBy(obj.Pkg().Path(), refusalProtoPkg)
}
