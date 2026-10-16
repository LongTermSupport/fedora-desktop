package rules

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/fzfbind"
)

// The fzf flags that bind keys.
const (
	bindFlag   = "--bind"
	expectFlag = "--expect"
)

// stringsPkg is the standard library's strings package, whose functions two rules judge.
const stringsPkg = "strings"

// uiPkg owns fzf and gum, and the Action type that pairs a key with its words.
const uiPkg = "internal/ui"

// HiddenBindings forbids an action reachable only by a key.
var HiddenBindings = &analysis.Analyzer{
	Name: "project_hidden_bindings",
	Doc: `project_hidden_bindings forbids an fzf key that does something no visible choice offers.

Every action is a visible, named choice; a key is only a shortcut to one. In Go every key
reaches fzf through internal/ui's Action (ID, Label, Key): ui lists the Label as a row or a menu
line, prints the Key beside it, and builds --expect from those keys in one function
(expectBuilders in table.go). Flagged, wherever the text is spelt (a literal, a constant, a
variable, an append or slices.Concat element, a Sprintf format, a folded concatenation):

  - "--expect" or "--expect=..." outside the expectBuilders function;
  - "--bind=SPEC", or "--bind" followed by SPEC as the next argument, where SPEC binds a key
    (not an event such as start or focus) to anything but navigation: moving, selecting,
    editing the query, changing how the rows are shown, accepting the highlighted row (any
    key: without --expect fzf cannot say which, unless a query edit or select-all before the
    accept changes the answer), or leaving (fzfbind's package doc);
  - a binding the rule cannot read: "--bind" with no constant beside it, a SPEC built at run
    time, or text after a run-time argument that closes it and adds a binding;
  - a ui.Action literal with a Key and no Label.

A literal compared with argv (==, a switch case, strings.HasPrefix, slices.Contains, ...) reads
argv something else built and is not judged; test files are not judged.

Not caught: a binding inside a shell string (project_seam_commands flags sh -c), fzf's
FZF_DEFAULT_OPTS, a binding assembled from non-constant pieces only, and an Action whose Label
ui does not show (ui's own tests hold that).

How to fix: make the action a visible choice: a ui.Action in PickSpec.Actions (its own row) or
in the row's PickSpec.Menus entry (the menu Enter opens), with the key in its Key field.`,
	Run:        perPass(runHiddenBindings),
	ResultType: noResultType,
}

func runHiddenBindings(pass *analysis.Pass) {
	if ownedByAny(pass.Pkg.Path(), bindingSubjects) {
		return
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		skip := readingLiterals(pass, f)
		for _, decl := range f.Decls {
			s := &bindScan{pass: pass, fn: funcName(decl), skip: skip}
			ast.Inspect(decl, s.visit)
		}
	}
}

// bindScan judges the fzf flags of one declaration.
type bindScan struct {
	pass *analysis.Pass
	fn   string
	// skip is the literals already judged, or read rather than built.
	skip map[ast.Expr]bool
}

func (s *bindScan) visit(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.CompositeLit:
		checkAction(s.pass, n)
		s.judgeList(n.Elts)
	case *ast.CallExpr:
		s.judgeList(n.Args)
		s.judgeTrimmed(n)
	case *ast.BinaryExpr:
		return s.judgeConcat(n)
	case *ast.BasicLit:
		if !s.skip[n] {
			if text, ok := stringConst(s.pass, n); ok {
				s.judgeText(n, text)
			}
		}
	}
	return true
}

// judgeList judges a "--bind" word with the next element of the same list as its binding.
func (s *bindScan) judgeList(elts []ast.Expr) {
	for i, e := range elts {
		text, ok := stringConst(s.pass, e)
		if !ok || text != bindFlag || s.skip[e] {
			continue
		}
		s.markLeaves(e)
		if i+1 == len(elts) {
			s.pass.Reportf(e.Pos(), "fzf --bind with no binding beside it: the rule cannot judge it")
			continue
		}
		s.judgeSpec(elts[i+1], "")
	}
}

// judgeConcat judges a string concatenation once, by its leftmost operand; a constant one is
// judged whole. It reports whether to look inside.
func (s *bindScan) judgeConcat(b *ast.BinaryExpr) bool {
	if b.Op != token.ADD {
		return true
	}
	if text, ok := stringConst(s.pass, b); ok {
		if !s.skip[b] {
			s.judgeText(b, text)
		}
		return false
	}
	leaves := concatLeaves(b)
	head, ok := stringConst(s.pass, leaves[0])
	if !ok || s.skip[leaves[0]] {
		return true
	}
	switch {
	case strings.HasPrefix(head, bindFlag+"="):
		s.judgeSpec(b, bindFlag+"=")
	case isExpect(head):
		s.judgeText(leaves[0], head)
	}
	s.markLeaves(b)
	return true
}

// judgeText judges one whole constant.
func (s *bindScan) judgeText(at ast.Expr, text string) {
	switch {
	case isExpect(text):
		s.judgeExpect(at)
	case text == bindFlag:
		s.pass.Reportf(at.Pos(), "fzf --bind with no binding beside it: the rule cannot judge it")
	case strings.HasPrefix(text, bindFlag+"="):
		s.judgeSpec(at, bindFlag+"=")
	}
}

func isExpect(text string) bool {
	return text == expectFlag || strings.HasPrefix(text, expectFlag+"=")
}

// judgeExpect allows --expect only in the function that builds it from ui.Action keys.
func (s *bindScan) judgeExpect(at ast.Expr) {
	allowed := slices.ContainsFunc(expectBuilders, func(a allowance) bool {
		return ownedBy(s.pass.Pkg.Path(), a.pkg) && a.fn == s.fn
	})
	if !allowed {
		s.pass.Reportf(at.Pos(), "fzf --expect outside the ui Action builder: a key with no visible choice; offer it as a ui.Action")
	}
}

// judgeSpec judges the binding spec held by expr after prefix ("--bind=" or "" for a spec
// that is its own argument): its constant head is read, and any run-time rest must sit
// inside an argument the head leaves open and add no binding of its own.
func (s *bindScan) judgeSpec(expr ast.Expr, prefix string) {
	s.markLeaves(expr)
	leaves := concatLeaves(expr)
	head, ok := stringConst(s.pass, leaves[0])
	if bin, isBin := ast.Unparen(expr).(*ast.BinaryExpr); isBin {
		if whole, folded := stringConst(s.pass, bin); folded {
			head, ok, leaves = whole, true, []ast.Expr{expr}
		}
	}
	if !ok || !strings.HasPrefix(head, prefix) || head == prefix {
		s.pass.Reportf(expr.Pos(), "fzf --bind with a binding built at run time: the rule cannot judge it; spell it as a constant")
		return
	}
	open, clean := s.judgeBindings(expr, head, prefix)
	if !clean || len(leaves) == 1 {
		return
	}
	if open == "" {
		s.pass.Reportf(expr.Pos(), "fzf --bind with a binding built at run time: the rule cannot judge it; spell it as a constant")
		return
	}
	for _, leaf := range leaves[1:] {
		if text, isConst := stringConst(s.pass, leaf); isConst && fzfbind.ClosesThenBinds(open, text) {
			s.pass.Reportf(expr.Pos(), "fzf --bind with a binding after a run-time argument: the rule cannot judge it")
			return
		}
	}
}

// judgeBindings reports the first binding of head (after prefix) that is not navigation, at
// at. It returns the argument delimiter head leaves open, and whether every binding was clean.
func (s *bindScan) judgeBindings(at ast.Expr, head, prefix string) (string, bool) {
	bindings, open, err := fzfbind.Read(strings.TrimPrefix(head, prefix))
	if err != nil {
		s.pass.Reportf(at.Pos(), "fzf --bind %q cannot be read as bindings: spell it as fzf documents it", head)
		return "", false
	}
	for _, b := range bindings {
		if a := fzfbind.HiddenAction(b); a != "" {
			s.pass.Reportf(at.Pos(), "fzf --bind %s:%s: a key bound to an action no visible choice offers; offer it as a ui.Action", b.Trigger, a)
			return "", false
		}
	}
	return open, true
}

// judgeTrimmed judges what strings.TrimPrefix or CutPrefix makes of a constant: the flag can
// appear only once the prefix is gone.
func (s *bindScan) judgeTrimmed(call *ast.CallExpr) {
	fn := calledFunc(s.pass, call)
	if len(call.Args) != 2 || (!isPkgFunc(fn, stringsPkg, trimPrefixFn) && !isPkgFunc(fn, stringsPkg, cutPrefixFn)) {
		return
	}
	text, isText := stringConst(s.pass, call.Args[0])
	prefix, isPrefix := stringConst(s.pass, call.Args[1])
	if !isText || !isPrefix || !strings.HasPrefix(text, prefix) || prefix == "" {
		return
	}
	switch rest := strings.TrimPrefix(text, prefix); {
	case isExpect(rest):
		s.judgeExpect(call.Args[0])
	case strings.HasPrefix(rest, bindFlag+"="):
		s.judgeBindings(call.Args[0], rest, bindFlag+"=")
	}
}

// markLeaves records every literal of expr as judged.
func (s *bindScan) markLeaves(expr ast.Expr) {
	ast.Inspect(expr, func(n ast.Node) bool {
		if e, ok := n.(ast.Expr); ok {
			s.skip[e] = true
		}
		return true
	})
}

// concatLeaves is the operands of a chain of + in order; any other expression is its own leaf.
func concatLeaves(expr ast.Expr) []ast.Expr {
	b, ok := ast.Unparen(expr).(*ast.BinaryExpr)
	if !ok || b.Op != token.ADD {
		return []ast.Expr{expr}
	}
	return append(concatLeaves(b.X), concatLeaves(b.Y)...)
}

// stringConst is the constant text of expr when it is a string constant.
func stringConst(pass *analysis.Pass, expr ast.Expr) (string, bool) {
	tv, ok := pass.TypesInfo.Types[expr]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(tv.Value), true
}

const (
	trimPrefixFn = "TrimPrefix"
	cutPrefixFn  = "CutPrefix"
)

// argvReaders are the functions whose string arguments are searched for, not built into argv.
var argvReaders = map[string][]string{
	stringsPkg: {"HasPrefix", "HasSuffix", "Contains", "Index", "Cut", cutPrefixFn, trimPrefixFn, "EqualFold"},
	"slices":   {"Contains", "Index"},
}

// valueReaders are the argvReaders whose first argument becomes their result, so it can become
// argv: only the prefix or separator they search for is read.
var valueReaders = []string{"Cut", cutPrefixFn, trimPrefixFn}

// readingLiterals is the string literals of f that read argv rather than build it: operands
// of == and !=, switch cases, and the searched-for arguments of argvReaders.
func readingLiterals(pass *analysis.Pass, f *ast.File) map[ast.Expr]bool {
	read := map[ast.Expr]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.BinaryExpr:
			if n.Op == token.EQL || n.Op == token.NEQ {
				read[n.X], read[n.Y] = true, true
			}
		case *ast.CaseClause:
			for _, e := range n.List {
				read[e] = true
			}
		case *ast.CallExpr:
			for _, a := range readArgs(pass, n) {
				read[a] = true
			}
		}
		return true
	})
	return read
}

// readArgs is the arguments of call that an argvReader searches for rather than returns.
func readArgs(pass *analysis.Pass, call *ast.CallExpr) []ast.Expr {
	fn := calledFunc(pass, call)
	if fn == nil || fn.Pkg() == nil || !slices.Contains(argvReaders[fn.Pkg().Path()], fn.Name()) {
		return nil
	}
	if fn.Pkg().Path() == stringsPkg && slices.Contains(valueReaders, fn.Name()) && len(call.Args) > 0 {
		return call.Args[1:]
	}
	return call.Args
}

// checkAction reports a ui.Action literal that has a Key and no Label: a key with no words.
func checkAction(pass *analysis.Pass, cl *ast.CompositeLit) {
	named, ok := types.Unalias(pass.TypesInfo.TypeOf(cl)).(*types.Named)
	if !ok || named.Obj().Name() != "Action" || named.Obj().Pkg() == nil || !ownedBy(named.Obj().Pkg().Path(), uiPkg) {
		return
	}
	st, ok := named.Underlying().(*types.Struct)
	if !ok {
		return
	}
	fields := map[string]ast.Expr{}
	for i, e := range cl.Elts {
		if kv, isKV := e.(*ast.KeyValueExpr); isKV {
			if id, isID := kv.Key.(*ast.Ident); isID {
				fields[id.Name] = kv.Value
			}
			continue
		}
		if i < st.NumFields() {
			fields[st.Field(i).Name()] = e
		}
	}
	if isEmptyString(pass, fields["Key"]) || !isEmptyString(pass, fields["Label"]) {
		return
	}
	pass.Reportf(cl.Pos(), "ui.Action with a Key and no Label: a key with no visible choice; give it the words it is listed under")
}

// isEmptyString is whether expr is absent or the constant "".
func isEmptyString(pass *analysis.Pass, expr ast.Expr) bool {
	if expr == nil {
		return true
	}
	text, ok := stringConst(pass, expr)
	return ok && text == ""
}
