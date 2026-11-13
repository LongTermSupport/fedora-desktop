package rules

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// The tmuxccy methods that type text and press Enter, and the one function that may use both:
// Tmux.Submit, which reads the input box after each Enter and presses it again.
const (
	sendKeysMethod  = "SendKeys"
	sendEnterMethod = "SendEnter"
	tmuxTypeName    = "Tmux"
	submitterFn     = "Tmux.Submit"
)

// submitKeys are the tmux key names that submit a line.
var submitKeys = []string{"Enter", "C-m", "KPEnter", "C-j"}

// textWords are the tmux argv words that put text into a pane: send-keys' literal flag and
// paste-buffer.
var textWords = []string{"-l", "paste-buffer"}

// SubmitUnchecked forbids text typed into a pane and submitted with an Enter nothing checks.
var SubmitUnchecked = &analysis.Analyzer{
	Name: "project_submit_unchecked",
	Doc: `project_submit_unchecked forbids text typed into a TUI and submitted with one Enter whose effect is never checked.

An Enter sent to Claude Code's TUI can arrive before the TUI is ready, or while it is still
taking the typed text, and be lost: the text sits in the input box, the caller believes it was
submitted, and the session idles (fedora-desktop#264: ccy-sessions restore left /compact in two
sessions' boxes after a reboot; a second Enter submitted it).

Flagged, outside test files:

  - a function that uses both tmuxccy's Tmux.SendKeys and Tmux.SendEnter (called, or taken as a
    method value), other than Tmux.Submit itself;
  - a Tmux.SendKeys whose text holds a constant with a newline or carriage return, which
    submits the line inside the typing;
  - in internal/tmuxccy, a function building text (send-keys -l, paste-buffer) and a submit
    key (Enter, C-m, KPEnter, C-j) as constants, other than Tmux.Submit.

Not caught: typing in one function and its Enter in another (a lone SendEnter is how a dialog
is answered and a stuck line re-entered, so it is not judged alone); text typed through a runner
other than tmuxccy's. The shell and Python sides have no tmux submit today (see the catalogue).

How to fix: submit with tmuxccy's Tmux.Submit, which types the text, presses Enter, reads the
box until the text has left it, presses Enter again a bounded number of times, and returns
tmuxccy.ErrNotSubmitted when it never leaves: report that, never carry on as if it was taken.`,
	Run:        perPass(runSubmitUnchecked),
	ResultType: noResultType,
}

func runSubmitUnchecked(pass *analysis.Pass) {
	owner := ownedBy(pass.Pkg.Path(), tmuxOwner)
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			submitter := owner && funcName(fd) == submitterFn
			judgeSubmitFunc(pass, fd.Body, submitter)
			if owner && !submitter {
				judgeRawSubmit(pass, fd.Body)
			}
		}
	}
}

// judgeSubmitFunc reports each SendEnter of a function that also types with SendKeys, and each
// SendKeys of a line ending.
func judgeSubmitFunc(pass *analysis.Pass, body *ast.BlockStmt, submitter bool) {
	var typed bool
	var enters []*ast.SelectorExpr
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			switch tmuxMethod(pass, n) {
			case sendKeysMethod:
				typed = true
			case sendEnterMethod:
				enters = append(enters, n)
			}
		case *ast.CallExpr:
			judgeTypedLineEnd(pass, n)
		}
		return true
	})
	if !typed || submitter {
		return
	}
	for _, e := range enters {
		pass.Reportf(e.Sel.Pos(), "text typed with SendKeys and submitted with an Enter nothing checks, which a TUI not yet ready loses (fedora-desktop#264): use Tmux.Submit")
	}
}

// judgeTypedLineEnd reports a SendKeys whose text holds a newline or carriage return.
func judgeTypedLineEnd(pass *analysis.Pass, call *ast.CallExpr) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || tmuxMethod(pass, sel) != sendKeysMethod || len(call.Args) == 0 {
		return
	}
	for _, leaf := range plusLeaves(call.Args[len(call.Args)-1]) {
		if s, isConst := stringConst(pass, leaf); isConst && strings.ContainsAny(s, "\r\n") {
			pass.Reportf(call.Pos(), "a line ending typed with SendKeys submits the text unchecked (fedora-desktop#264): type the text alone and use Tmux.Submit")
			return
		}
	}
}

// judgeRawSubmit reports, in the tmux owner, a function building both a text command and a
// submit key.
func judgeRawSubmit(pass *analysis.Pass, body *ast.BlockStmt) {
	var text bool
	var keys []ast.Expr
	ast.Inspect(body, func(n ast.Node) bool {
		e, isExpr := n.(ast.Expr)
		if !isExpr {
			return true
		}
		s, isConst := stringConst(pass, e)
		if !isConst {
			return true
		}
		text = text || slices.Contains(textWords, s)
		if slices.Contains(submitKeys, s) {
			keys = append(keys, e)
		}
		return false
	})
	if !text {
		return
	}
	for _, k := range keys {
		pass.Reportf(k.Pos(), "a tmux text command and a submit key in one function: the Enter is never checked (fedora-desktop#264); submit through Tmux.Submit")
	}
}

// plusLeaves is the operands of a chain of + (the expression itself when it is not one).
func plusLeaves(e ast.Expr) []ast.Expr {
	b, ok := ast.Unparen(e).(*ast.BinaryExpr)
	if !ok || b.Op != token.ADD {
		return []ast.Expr{e}
	}
	return append(plusLeaves(b.X), plusLeaves(b.Y)...)
}

// tmuxMethod is the name of the tmuxccy Tmux method sel selects, or "".
func tmuxMethod(pass *analysis.Pass, sel *ast.SelectorExpr) string {
	fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
	if !ok || fn.Pkg() == nil || !ownedBy(fn.Pkg().Path(), tmuxOwner) {
		return ""
	}
	sig, isSig := fn.Type().(*types.Signature)
	if !isSig || sig.Recv() == nil {
		return ""
	}
	recv := sig.Recv().Type()
	if p, isPtr := recv.(*types.Pointer); isPtr {
		recv = p.Elem()
	}
	named, isNamed := recv.(*types.Named)
	if !isNamed || named.Obj().Name() != tmuxTypeName {
		return ""
	}
	return fn.Name()
}
