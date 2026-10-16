package rules

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// The Bubble Tea packages whose keys project_hidden_bindings judges.
const (
	teaPkg = "charm.land/bubbletea/v2"
	keyPkg = "charm.land/bubbles/v2/key"
)

// teaKeyTypes are Bubble Tea's key messages: what a program reads a key press as.
var teaKeyTypes = []string{"KeyMsg", "KeyPressMsg", "KeyReleaseMsg", "Key"}

// lineKeys are the keys a text input or a list takes as line editing and navigation, never as
// an action: moving, editing the text, Tab completing it, accepting, and leaving. Standard
// terminal UX, so no visible control is owed for them (the owner, of Tab completion: "acceptable
// as a magic key because it's standard UX", with a hint line saying it works).
var lineKeys = []string{
	"tab", "shift+tab", "enter", "esc", "backspace", "delete", "space",
	"left", "right", "up", "down", "home", "end", "pgup", "pgdown",
	"ctrl+a", "ctrl+e", "ctrl+u", "ctrl+k", "ctrl+w", "ctrl+h", "ctrl+c",
}

// lineCodes are Bubble Tea's key codes for the lineKeys.
var lineCodes = []string{
	"KeyTab", "KeyEnter", "KeyEscape", "KeyBackspace", "KeyDelete", "KeySpace",
	"KeyLeft", "KeyRight", "KeyUp", "KeyDown", "KeyHome", "KeyEnd", "KeyPgUp", "KeyPgDown",
}

// judgeTeaKeys judges one file's Bubble Tea keys: bindings without words, key messages read
// outside internal/ui, and keys compared by hand.
func judgeTeaKeys(pass *analysis.Pass, f *ast.File) {
	outside := !insideUI(pass.Pkg.Path())
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident:
			if outside && isTeaKeyType(pass.TypesInfo.Uses[n]) {
				pass.Reportf(n.Pos(), "Bubble Tea key message read outside internal/ui: only ui's components take keys, and they show each one")
			}
		case *ast.CallExpr:
			judgeBinding(pass, n)
		case *ast.BinaryExpr:
			if n.Op == token.EQL || n.Op == token.NEQ {
				judgeKeyCompare(pass, n.X, n.Y, n.Pos())
				judgeKeyCompare(pass, n.Y, n.X, n.Pos())
			}
		case *ast.SwitchStmt:
			judgeKeySwitch(pass, n)
		}
		return true
	})
}

// judgeKeySwitch judges each case of a switch on which key a message is.
func judgeKeySwitch(pass *analysis.Pass, sw *ast.SwitchStmt) {
	if sw.Tag == nil || !isKeyRead(pass, sw.Tag) {
		return
	}
	for _, stmt := range sw.Body.List {
		clause, isClause := stmt.(*ast.CaseClause)
		if !isClause {
			continue
		}
		for _, e := range clause.List {
			judgeKeyConst(pass, e, e.Pos())
		}
	}
}

// insideUI is whether the package is internal/ui or one of its subpackages.
func insideUI(pkgPath string) bool {
	path := strings.TrimSuffix(strings.Fields(pkgPath + " ")[0], "_test")
	return ownedBy(path, uiPkg) || strings.Contains(path, "/"+uiPkg+"/") || strings.HasPrefix(path, uiPkg+"/")
}

// isTeaKeyType is whether obj is one of Bubble Tea's key message types.
func isTeaKeyType(obj types.Object) bool {
	tn, ok := obj.(*types.TypeName)
	return ok && tn.Pkg() != nil && tn.Pkg().Path() == teaPkg && slices.Contains(teaKeyTypes, tn.Name())
}

// isTeaKeyValue is whether expr has a Bubble Tea key message type.
func isTeaKeyValue(pass *analysis.Pass, expr ast.Expr) bool {
	named, ok := types.Unalias(pass.TypesInfo.TypeOf(expr)).(*types.Named)
	return ok && isTeaKeyType(named.Obj())
}

// isKeyRead is whether expr reads which key a message is: its String or Keystroke, or its
// Code or Text field.
func isKeyRead(pass *analysis.Pass, expr ast.Expr) bool {
	expr = ast.Unparen(expr)
	if call, isCall := expr.(*ast.CallExpr); isCall && len(call.Args) == 0 {
		sel, isSel := call.Fun.(*ast.SelectorExpr)
		return isSel && (sel.Sel.Name == "String" || sel.Sel.Name == "Keystroke") && isTeaKeyValue(pass, sel.X)
	}
	sel, isSel := expr.(*ast.SelectorExpr)
	return isSel && (sel.Sel.Name == "Code" || sel.Sel.Name == "Text") && isTeaKeyValue(pass, sel.X)
}

// judgeKeyCompare judges read == other when read reads a key.
func judgeKeyCompare(pass *analysis.Pass, read, other ast.Expr, at token.Pos) {
	if isKeyRead(pass, read) {
		judgeKeyConst(pass, other, at)
	}
}

// judgeKeyConst reports a key compared by hand unless it is line editing or navigation, or the
// empty text (whether a key typed anything).
func judgeKeyConst(pass *analysis.Pass, expr ast.Expr, at token.Pos) {
	tv, ok := pass.TypesInfo.Types[expr]
	if !ok || tv.Value == nil {
		return
	}
	if tv.Value.Kind() == constant.String {
		text := constant.StringVal(tv.Value)
		if text == "" || slices.Contains(lineKeys, text) {
			return
		}
	} else if isLineCode(pass, expr) {
		return
	}
	pass.Reportf(at, "a key compared by hand: match it through a key.Binding with help, which ui draws")
}

// isLineCode is whether expr names one of Bubble Tea's line-editing key codes.
func isLineCode(pass *analysis.Pass, expr ast.Expr) bool {
	var id *ast.Ident
	switch e := ast.Unparen(expr).(type) {
	case *ast.Ident:
		id = e
	case *ast.SelectorExpr:
		id = e.Sel
	default:
		return false
	}
	obj := pass.TypesInfo.Uses[id]
	return obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == teaPkg && slices.Contains(lineCodes, obj.Name())
}

// judgeBinding reports a key.NewBinding whose help (the words ui draws for it) is missing or
// cannot be read.
func judgeBinding(pass *analysis.Pass, call *ast.CallExpr) {
	if !isPkgFunc(calledFunc(pass, call), keyPkg, "NewBinding") {
		return
	}
	readable := call.Ellipsis == token.NoPos
	for _, arg := range call.Args {
		opt, isCall := ast.Unparen(arg).(*ast.CallExpr)
		if !isCall {
			readable = false
			continue
		}
		if !isPkgFunc(calledFunc(pass, opt), keyPkg, "WithHelp") || len(opt.Args) != 2 {
			continue
		}
		judgeHelp(pass, call, opt)
		return
	}
	if !readable {
		pass.Reportf(call.Pos(), "key.Binding whose help the rule cannot read: give it key.WithHelp(key, words) in the call")
		return
	}
	pass.Reportf(call.Pos(), "key.Binding with no help text: a key with no visible hint; give it key.WithHelp(key, words)")
}

// judgeHelp judges key.WithHelp(key, words): both constant and neither empty.
func judgeHelp(pass *analysis.Pass, binding, help *ast.CallExpr) {
	for _, arg := range help.Args {
		text, isText := stringConst(pass, arg)
		if !isText {
			pass.Reportf(binding.Pos(), "key.Binding whose help the rule cannot read: spell key.WithHelp's key and words as constants")
			return
		}
		if text == "" {
			pass.Reportf(binding.Pos(), "key.Binding with no help text: a key with no visible hint; give it key.WithHelp(key, words)")
			return
		}
	}
}
