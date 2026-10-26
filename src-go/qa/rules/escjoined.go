package rules

import (
	"go/ast"
	"go/constant"
	"path"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/fzfbind"
)

// esckeyPkg is the package that tells a Bubble Tea key press that is an Esc joined to the next
// key, and joinedFn its function.
const (
	esckeyPkg = "internal/ui/esckey"
	joinedFn  = "Joined"
)

// fzfNames are the names fzf is run by.
var fzfNames = []string{"fzf", "fzf-tmux"}

// EscJoined forbids a screen that drops an Esc joined to the next key.
var EscJoined = &analysis.Analyzer{
	Name: "project_esc_joined",
	Doc: `project_esc_joined forbids a screen that loses an Esc typed just before another key.

A terminal sends Esc as one byte, which also starts every escape sequence, so a key reader
waits a moment after it: fzf 100 ms, Bubble Tea 50 ms, and tmux, at its default escape-time,
500 ms before it hands the pane both keys in one write. An Esc and the key after it inside that
wait are read as that key with Alt: Esc then Down is alt-down. A screen with no meaning for the
Alt key drops it, so the person stays on a screen they meant to leave and their next keys act on
it (Plan 00177: Esc then Down in a ccy-teams row menu left the menu open and moved its cursor).

Flagged in Go (test files are not judged):

  - an execx.Cmd literal running fzf (Name a constant whose base name is fzf or fzf-tmux) whose
    Args do not hold, as "--bind=..." constants, a binding of every Esc-joined key
    (fzfbind.EscJoined: the arrows, Enter, Space, Backspace, every letter, Ctrl+letter and digit
    with Alt) to abort alone; the message names the first keys missing. An fzf given an
    option that draws no screen (fzfbind.DrawsNoScreen: --version, --help, --filter, a shell's
    integration script) reads no key and is not judged;
  - an execx.Cmd whose Name is set to fzf field by field, where its Args cannot be read;
  - a Bubble Tea key.NewBinding whose keys include "esc" in a package that never calls
    esckey.Joined (internal/ui/esckey): Bubble Tea hands an Esc joined to the next key over as
    that key with Alt, which no binding of "esc" matches.

The shell half is the gate qa-hidden-bindings: every fzf a shell file runs gets the same check.

Not caught: an fzf started through a program runner (sudo, a shell string: project_seam_commands
flags those), the bindings passed as a separate "--bind" and spec pair, FZF_DEFAULT_OPTS, a
package that calls esckey.Joined on some other path than its Esc binding, and gum, whose
prompts cannot be given bindings.

How to fix: in Go, start fzf through internal/ui, whose fzfEscJoined constant binds every one
of the keys to abort; a key a screen hands back (a menu's Esc then an arrow) goes in --expect,
which fzf prefers to the binding. In Bubble Tea, cancel on esckey.Joined(k) where Esc cancels.`,
	Run:        perPass(runEscJoined),
	ResultType: noResultType,
}

func runEscJoined(pass *analysis.Pass) {
	files := slices.DeleteFunc(slices.Clone(pass.Files), func(f *ast.File) bool { return isTestFile(pass, f) })
	var escBindings []*ast.CallExpr
	joined := false
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CompositeLit:
				judgeFzfLiteral(pass, n)
			case *ast.AssignStmt:
				judgeFzfField(pass, n)
			case *ast.CallExpr:
				if bindsEsc(pass, n) {
					escBindings = append(escBindings, n)
				}
				joined = joined || callsJoined(pass, n)
			}
			return true
		})
	}
	if joined {
		return
	}
	for _, b := range escBindings {
		pass.Reportf(b.Pos(), "Esc bound with no esckey.Joined check: an Esc typed just before another key arrives as that key with Alt and is lost; cancel on esckey.Joined too")
	}
}

// isFzfName is whether expr is a constant naming fzf, by name or by path.
func isFzfName(pass *analysis.Pass, expr ast.Expr) bool {
	text, ok := stringConst(pass, expr)
	return ok && slices.Contains(fzfNames, path.Base(text))
}

// judgeFzfLiteral reports an execx.Cmd literal running fzf whose Args leave a joined key unbound.
func judgeFzfLiteral(pass *analysis.Pass, lit *ast.CompositeLit) {
	if !isCmdType(pass.TypesInfo.TypeOf(lit)) {
		return
	}
	name, args := cmdNameArgs(lit)
	if name == nil || !isFzfName(pass, name) {
		return
	}
	words := argConsts(pass, args)
	if slices.ContainsFunc(words, fzfbind.DrawsNoScreen) {
		return
	}
	var specs []string
	for _, w := range words {
		if spec, isBind := strings.CutPrefix(w, bindFlag+"="); isBind {
			specs = append(specs, spec)
		}
	}
	missing := fzfbind.NotLeaving(specs)
	if len(missing) == 0 {
		return
	}
	pass.Reportf(lit.Pos(), "fzf started without the Esc-joined bindings: %s not bound to abort, so an Esc typed just before them is lost; add ui's fzfEscJoined to Args",
		firstKeys(missing))
}

// shownKeys is how many missing keys a message names.
const shownKeys = 3

// firstKeys is the first few keys, and "..." when there are more.
func firstKeys(keys []string) string {
	if len(keys) <= shownKeys {
		return strings.Join(keys, ", ")
	}
	return strings.Join(keys[:shownKeys], ", ") + ", ..."
}

// argConsts is the text of every outermost string constant in expr.
func argConsts(pass *analysis.Pass, expr ast.Expr) []string {
	if expr == nil {
		return nil
	}
	var words []string
	ast.Inspect(expr, func(n ast.Node) bool {
		e, isExpr := n.(ast.Expr)
		if !isExpr {
			return true
		}
		tv, ok := pass.TypesInfo.Types[e]
		if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
			return true
		}
		words = append(words, constant.StringVal(tv.Value))
		return false
	})
	return words
}

// judgeFzfField reports cmd.Name = "fzf": the Args set elsewhere cannot be read with it.
func judgeFzfField(pass *analysis.Pass, as *ast.AssignStmt) {
	for i, lhs := range as.Lhs {
		sel, ok := lhs.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != cmdNameField || !isCmdType(pass.TypesInfo.TypeOf(sel.X)) || i >= len(as.Rhs) {
			continue
		}
		if isFzfName(pass, as.Rhs[i]) {
			pass.Reportf(as.Pos(), "fzf command built field by field: the rule cannot read its Args; build it as one execx.Cmd literal whose Args hold ui's fzfEscJoined")
		}
	}
}

// bindsEsc is whether call is a key.NewBinding whose keys include "esc".
func bindsEsc(pass *analysis.Pass, call *ast.CallExpr) bool {
	if !isPkgFunc(calledFunc(pass, call), keyPkg, "NewBinding") {
		return false
	}
	for _, arg := range call.Args {
		opt, isCall := ast.Unparen(arg).(*ast.CallExpr)
		if !isCall || !isPkgFunc(calledFunc(pass, opt), keyPkg, "WithKeys") {
			continue
		}
		if slices.ContainsFunc(opt.Args, func(a ast.Expr) bool {
			text, isText := stringConst(pass, a)
			return isText && text == "esc"
		}) {
			return true
		}
	}
	return false
}

// callsJoined is whether call is esckey.Joined.
func callsJoined(pass *analysis.Pass, call *ast.CallExpr) bool {
	fn := calledFunc(pass, call)
	return fn != nil && fn.Pkg() != nil && ownedBy(fn.Pkg().Path(), esckeyPkg) && isPkgFunc(fn, fn.Pkg().Path(), joinedFn)
}
