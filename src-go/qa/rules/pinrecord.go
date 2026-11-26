package rules

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

const pinRecordFix = " (project_pin_record_unconfirmed): a recorded digest lets every later launch nobody attends apply that content, so record only after a person was shown it and answered yes (Confirm, or a ui prompt's YesNo defaulting to no)"

// pinRecordSelf is the one recorder that asks nobody: it records the digest of the bytes it has
// just written itself, so no checkout content reaches the store through it.
const pinRecordSelf = "WriteLastLaunch"

// PinRecordUnconfirmed holds that an approval is recorded only where a person was asked.
var PinRecordUnconfirmed = &analysis.Analyzer{
	Name: "project_pin_record_unconfirmed",
	Doc: `project_pin_record_unconfirmed flags, in src-go's internal/ and cmd/ trees, every call of
internal/checkoutpin's Store.Record in a function that asks no person first: no call of a method
or function field named Confirm or YesNo comes before it in the same function (a closure is a
function of its own). checkoutpin.WriteLastLaunch is the one exception: it records the digest of
the bytes it has just written, which are the launcher's own.

A recorded digest is an approval: every later launch nobody attends (a restore after a reboot, a
team restart) applies the content it covers unasked. A ccy container writes its checkout, so a
record of whatever the checkout holds, made without showing it, launders a container's write into
an approval (Plan 00266 security review, finding 3, class "an approval does not cover what is
actually run or mounted"). The defect the rule exists for was the attended compose check, which
recorded the compose files' digest whenever a person had a terminal, unshown, after a [Y/n]
question whose Enter meant yes.

Test files are not judged: they plant approvals. The nested qa module is not judged.

Not caught: a question whose answer is not consulted, a question asked in another function before
this one is called, and a digest written to the store's files by other means.

How to fix: show the content (Shown), ask with Confirm or YesNo defaulting to no, and record only
on a yes.`,
	Run:        perPass(runPinRecordUnconfirmed),
	ResultType: noResultType,
}

func runPinRecordUnconfirmed(pass *analysis.Pass) {
	path := pass.Pkg.Path()
	judged := inTree(path, "internal") || inTree(path, "cmd")
	if !judged || inTree(path, "qa") {
		return
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil || (fd.Recv == nil && fd.Name.Name == pinRecordSelf && ownedBy(path, checkoutPinPkg)) {
				continue
			}
			judgePinRecords(pass, fd.Body)
		}
	}
}

// judgePinRecords reports each Record in body (a closure judged on its own) with no question
// asked before it in the same body.
func judgePinRecords(pass *analysis.Pass, body *ast.BlockStmt) {
	asked := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			judgePinRecords(pass, n.Body)
			return false
		case *ast.CallExpr:
			sel, ok := n.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if sel.Sel.Name == "Confirm" || sel.Sel.Name == "YesNo" {
				asked = true
			}
			if !asked && isPinRecord(pass, sel) {
				pass.Reportf(n.Pos(), "an approval recorded where no person was asked%s", pinRecordFix)
			}
		}
		return true
	})
}

// isPinRecord is whether sel is the Record method of internal/checkoutpin's Store.
func isPinRecord(pass *analysis.Pass, sel *ast.SelectorExpr) bool {
	fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
	if !ok || fn.Name() != "Record" || fn.Pkg() == nil || !ownedBy(fn.Pkg().Path(), checkoutPinPkg) {
		return false
	}
	recv := fn.Signature().Recv()
	if recv == nil {
		return false
	}
	named, ok := types.Unalias(recv.Type()).(*types.Named)
	return ok && named.Obj().Name() == "Store"
}
