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
// an action: moving, editing the text, accepting, and leaving. Standard terminal UX, so no
// visible control is owed for them. Tab (tabKeys) is one too, but only with its hint drawn.
var lineKeys = []string{
	"enter", "esc", "backspace", "delete", "space",
	"left", "right", "up", "down", "home", "end", "pgup", "pgdown",
	"ctrl+a", "ctrl+e", "ctrl+u", "ctrl+k", "ctrl+w", "ctrl+h", "ctrl+c",
}

// lineCodes are Bubble Tea's key codes for the lineKeys.
var lineCodes = []string{
	"KeyEnter", "KeyEscape", "KeyBackspace", "KeyDelete", "KeySpace",
	"KeyLeft", "KeyRight", "KeyUp", "KeyDown", "KeyHome", "KeyEnd", "KeyPgUp", "KeyPgDown",
}

// tabKeys are Tab and Shift+Tab, and tabCode Bubble Tea's code for Tab. Completing a text
// input on Tab is navigation by the owner's decision ("acceptable as a magic key because it's
// standard UX") on the condition that a hint says Tab works: so Tab taken by hand is allowed
// only in a package that binds Tab with constant help, which ui draws as that hint.
var tabKeys = []string{"tab", "shift+tab"}

const tabCode = "KeyTab"

// teaScan is one package's Bubble Tea keys being judged.
type teaScan struct {
	pass *analysis.Pass
	// outside is the package being outside internal/ui, where no key message may be read.
	outside bool
	// tabBound is the package binding Tab with constant help (its hint is drawn).
	tabBound bool
	// keyVars are variables holding a key read (k := msg.String()), compared like the read.
	keyVars map[types.Object]bool
}

// judgeTeaKeys judges a package's Bubble Tea keys: bindings without words, key messages read
// outside internal/ui, and keys compared by hand. Test files are not judged.
func judgeTeaKeys(pass *analysis.Pass) {
	s := &teaScan{pass: pass, outside: !insideUI(pass.Pkg.Path()), keyVars: map[types.Object]bool{}}
	files := slices.DeleteFunc(slices.Clone(pass.Files), func(f *ast.File) bool { return isTestFile(pass, f) })
	for _, f := range files {
		ast.Inspect(f, s.collect)
	}
	for _, f := range files {
		ast.Inspect(f, s.visit)
	}
}

// collect finds what the judging needs first: a Tab binding with help, and key-read variables.
func (s *teaScan) collect(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.CallExpr:
		if bindsTabWithHelp(s.pass, n) {
			s.tabBound = true
		}
	case *ast.AssignStmt:
		if len(n.Lhs) == len(n.Rhs) {
			for i, lhs := range n.Lhs {
				s.noteKeyVar(lhs, n.Rhs[i])
			}
		}
	case *ast.ValueSpec:
		if len(n.Names) == len(n.Values) {
			for i, name := range n.Names {
				s.noteKeyVar(name, n.Values[i])
			}
		}
	}
	return true
}

func (s *teaScan) noteKeyVar(lhs, rhs ast.Expr) {
	id, isIdent := ast.Unparen(lhs).(*ast.Ident)
	if !isIdent || !s.isKeyRead(rhs) {
		return
	}
	if obj := s.pass.TypesInfo.ObjectOf(id); obj != nil {
		s.keyVars[obj] = true
	}
}

func (s *teaScan) visit(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.Ident:
		if s.outside && isTeaKeyType(s.pass.TypesInfo.Uses[n]) {
			s.pass.Reportf(n.Pos(), "Bubble Tea key message read outside internal/ui: only ui's components take keys, and they show each one")
		}
	case *ast.CallExpr:
		judgeBinding(s.pass, n)
		s.judgeStringsCall(n)
	case *ast.CompositeLit:
		s.judgeBindingLiteral(n)
	case *ast.BinaryExpr:
		if n.Op == token.EQL || n.Op == token.NEQ {
			s.judgeKeyCompare(n.X, n.Y, n.Pos())
			s.judgeKeyCompare(n.Y, n.X, n.Pos())
		}
	case *ast.SwitchStmt:
		s.judgeKeySwitch(n)
	}
	return true
}

// judgeKeySwitch judges each case of a switch on which key a message is.
func (s *teaScan) judgeKeySwitch(sw *ast.SwitchStmt) {
	if sw.Tag == nil || !s.isKeyRead(sw.Tag) {
		return
	}
	for _, stmt := range sw.Body.List {
		clause, isClause := stmt.(*ast.CaseClause)
		if !isClause {
			continue
		}
		for _, e := range clause.List {
			s.judgeKeyConst(e, e.Pos())
		}
	}
}

// judgeStringsCall judges a strings function that searches text (argvReaders' list) handed a
// key read, against its constant arguments.
func (s *teaScan) judgeStringsCall(call *ast.CallExpr) {
	fn := calledFunc(s.pass, call)
	if !slices.ContainsFunc(argvReaders[stringsPkg], func(name string) bool { return isPkgFunc(fn, stringsPkg, name) }) {
		return
	}
	if !slices.ContainsFunc(call.Args, s.isKeyRead) {
		return
	}
	for _, arg := range call.Args {
		if !s.isKeyRead(arg) {
			s.judgeKeyConst(arg, call.Pos())
		}
	}
}

// judgeBindingLiteral reports a key.Binding built as a struct literal: its help cannot be set.
func (s *teaScan) judgeBindingLiteral(lit *ast.CompositeLit) {
	named, isNamed := types.Unalias(s.pass.TypesInfo.TypeOf(lit)).(*types.Named)
	if !isNamed || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != keyPkg || named.Obj().Name() != "Binding" {
		return
	}
	s.pass.Reportf(lit.Pos(), "key.Binding with no help text: a struct literal carries none; use key.NewBinding with key.WithHelp(key, words)")
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

// isKeyRead is whether expr reads which key a message is: its String or Keystroke, its Code or
// Text field, or a variable holding one of those.
func (s *teaScan) isKeyRead(expr ast.Expr) bool {
	expr = ast.Unparen(expr)
	switch e := expr.(type) {
	case *ast.Ident:
		return s.keyVars[s.pass.TypesInfo.ObjectOf(e)]
	case *ast.CallExpr:
		sel, isSel := e.Fun.(*ast.SelectorExpr)
		return len(e.Args) == 0 && isSel && (sel.Sel.Name == "String" || sel.Sel.Name == "Keystroke") &&
			isTeaKeyValue(s.pass, sel.X)
	case *ast.SelectorExpr:
		return (e.Sel.Name == "Code" || e.Sel.Name == "Text") && isTeaKeyValue(s.pass, e.X)
	}
	return false
}

// judgeKeyCompare judges read == other when read reads a key.
func (s *teaScan) judgeKeyCompare(read, other ast.Expr, at token.Pos) {
	if s.isKeyRead(read) {
		s.judgeKeyConst(other, at)
	}
}

// judgeKeyConst reports a key compared by hand unless it is line editing or navigation, or the
// empty text (whether a key typed anything); Tab only where its hint is drawn.
func (s *teaScan) judgeKeyConst(expr ast.Expr, at token.Pos) {
	tv, ok := s.pass.TypesInfo.Types[expr]
	if !ok || tv.Value == nil {
		return
	}
	isTab := isTeaConst(s.pass, expr, tabCode)
	if tv.Value.Kind() == constant.String {
		text := constant.StringVal(tv.Value)
		if text == "" || slices.Contains(lineKeys, text) {
			return
		}
		isTab = slices.Contains(tabKeys, text)
	} else if isLineCode(s.pass, expr) {
		return
	}
	switch {
	case isTab && s.tabBound:
	case isTab:
		s.pass.Reportf(at, "Tab taken by hand with no key.Binding for it: bind Tab with key.WithHelp, so ui draws the hint that Tab works")
	default:
		s.pass.Reportf(at, "a key compared by hand: match it through a key.Binding with help, which ui draws")
	}
}

// isLineCode is whether expr names one of Bubble Tea's line-editing key codes.
func isLineCode(pass *analysis.Pass, expr ast.Expr) bool {
	return slices.ContainsFunc(lineCodes, func(code string) bool { return isTeaConst(pass, expr, code) })
}

// isTeaConst is whether expr names Bubble Tea's constant name.
func isTeaConst(pass *analysis.Pass, expr ast.Expr, name string) bool {
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
	return obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == teaPkg && obj.Name() == name
}

// bindsTabWithHelp is whether call is a key.NewBinding of Tab or Shift+Tab with constant,
// non-empty help: the hint ui draws.
func bindsTabWithHelp(pass *analysis.Pass, call *ast.CallExpr) bool {
	if !isPkgFunc(calledFunc(pass, call), keyPkg, "NewBinding") {
		return false
	}
	tab, helped := false, false
	for _, arg := range call.Args {
		opt, isCall := ast.Unparen(arg).(*ast.CallExpr)
		if !isCall {
			continue
		}
		fn := calledFunc(pass, opt)
		switch {
		case isPkgFunc(fn, keyPkg, "WithKeys"):
			tab = tab || slices.ContainsFunc(opt.Args, func(a ast.Expr) bool {
				text, isText := stringConst(pass, a)
				return isText && slices.Contains(tabKeys, text)
			})
		case isPkgFunc(fn, keyPkg, "WithHelp"):
			helped = len(opt.Args) == 2 && !slices.ContainsFunc(opt.Args, func(a ast.Expr) bool {
				text, isText := stringConst(pass, a)
				return !isText || text == ""
			})
		}
	}
	return tab && helped
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
