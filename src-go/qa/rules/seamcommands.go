package rules

import (
	"go/ast"
	"go/constant"
	"go/types"
	"path"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// wrapper describes a program that runs another program given in its arguments, so the rule
// can find the program that actually runs.
type wrapper struct {
	valued     map[string]bool // options that take the next argument as their value
	assigns    bool            // NAME=value words may precede the program
	positional int             // positional arguments before the program (timeout's duration)
	until      string          // the argument that ends the options, when there is one
	inner      bool            // the program inside is judged even where the wrapper itself is the owner's
}

var wrappers = map[string]wrapper{
	"env":  {valued: set("-u", "--unset", "-C", "--chdir"), assigns: true},
	"sudo": {valued: set("-u", "-g", "-h", "-p", "-C", "-D", "-R", "-T", "-U", "-r", "-t", "--user", "--group", "--host", "--prompt", "--close-from", "--chdir", "--chroot", "--command-timeout", "--other-user", "--role", "--type"), assigns: true},
	"nice": {valued: set("-n", "--adjustment")},
	"timeout": {
		valued:     set("-s", "--signal", "-k", "--kill-after"),
		positional: 1,
	},
	"setsid":      {},
	"nohup":       {},
	"systemd-run": {until: "--"},
	"lxc-attach":  {until: "--", inner: true},
}

func set(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

// SeamCommands keeps each external program inside the package that owns it.
var SeamCommands = newSeamCommands(dynamicNames)

func newSeamCommands(dynamic []allowance) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "project_seam_commands",
		Doc: `project_seam_commands keeps every program an execx.Cmd runs inside the package that owns it.

For an execx.Cmd built outside internal/execx (a composite literal, or an assignment to .Name):

- Name must be a compile-time constant (a literal or a named constant), so the table can judge
  it. A Name chosen at run time needs a row in the dynamicNames table of the rules package,
  with its reason.
- The program is judged by its base name: /usr/bin/gum and ./bin/tmux are gum and tmux.
- A wrapper is seen through: env [-u X] [VAR=val] prog, sudo [-flags] prog, timeout [-s S] D
  prog, nice [-n N] prog, setsid prog, nohup prog and systemd-run [opts] -- prog are judged by
  prog. The wrapper's argv must be a literal (or append/slices.Concat of literals) that shows
  the program; when it does not, the finding says so.
- An owned program (the commandOwners table: sudo, tmux, gum, fzf, systemctl, busctl, git, ps, podman, docker,
  the lxc-* tools, nmcli, and the installer's firewall-cmd, ip, ss, curl, rpm, dnf, getent,
  useradd, uname, zstd, journalctl) may be run only by its owner (podman by sessprobe or podfreeze). Any other program is unowned and free.
- A wrapper that is its owner's own program and runs another inside (lxc-attach ... -- prog) has
  prog judged too: lxcfreeze owns nmcli, but not what else it could run in a container.
- A call of a program runner (the programRunners table: sudo.Sudo Capture, AsRoot and Run, and
  lxcfreeze's runAct and probe, systemd's RunTransient) is judged the same way: its argument from the table's index on
  is the program, the rest its arguments. A spread slice must show its program (a literal head),
  or the function needs a dynamicNames row.
  A program runner's own body is excused a wrapper whose program is its parameter (its callers
  are judged); no other function is, whatever its row. A runner method taken as a value (f := s.Capture)
  hides the program from every later call and is flagged where it is taken.
- A shell with -c (sh, bash, dash, zsh) is a shell string handed to the runner and is flagged.

Test files are exempt: a test spells out the argv it expects. exec.LookPath and text naming a
program are not commands and are not flagged.

What the owner does with its own program (sudo -n, the tmux target forms) is not checked here:
the owner's tests pin sudo's flags, and project_tmux_targets checks the tmux targets. The
programs run THROUGH the owner's methods are judged, whoever calls them. Not caught: an
execx.Cmd built by copying another, and a wrapper's argv that is not visible as a literal in
the Cmd (reported as such).

How to fix: call the owner's method; add the operation to the owner if it is missing.`,
		Run: perPass(func(pass *analysis.Pass) {
			runSeamCommands(pass, dynamic)
		}),
		ResultType: noResultType,
	}
}

// excuses are what the enclosing function is excused from. name: its dynamicNames row, so a
// program chosen at run time is no finding. wrapper: it is itself a program runner.
type excuses struct{ name, wrapper bool }

// isRunnerDecl is whether decl declares one of the programRunners.
func isRunnerDecl(pass *analysis.Pass, decl ast.Decl) bool {
	fd, ok := decl.(*ast.FuncDecl)
	if !ok {
		return false
	}
	fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func)
	if !ok {
		return false
	}
	_, runner := runnerFunc(fn)
	return runner
}

func runSeamCommands(pass *analysis.Pass, dynamic []allowance) {
	if ownedBy(pass.Pkg.Path(), execOwner) {
		return
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		called := map[ast.Expr]bool{}
		for _, decl := range f.Decls {
			ex := excuses{name: allowedIn(pass, funcName(decl), dynamic), wrapper: isRunnerDecl(pass, decl)}
			ast.Inspect(decl, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.CompositeLit:
					checkCmdLiteral(pass, n, ex)
				case *ast.AssignStmt:
					checkNameAssign(pass, n, ex)
				case *ast.CallExpr:
					called[ast.Unparen(n.Fun)] = true
					checkRunnerCall(pass, n, ex)
				case *ast.SelectorExpr:
					checkRunnerValue(pass, n, called[n])
				}
				return true
			})
		}
	}
}

// isCmdType is whether t is the runner's Cmd, matched by owner path so fixtures share it.
func isCmdType(t types.Type) bool {
	if t == nil {
		return false
	}
	named, ok := types.Unalias(t).(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Name() == "Cmd" && ownedBy(named.Obj().Pkg().Path(), execOwner)
}

func checkCmdLiteral(pass *analysis.Pass, lit *ast.CompositeLit, ex excuses) {
	if !isCmdType(pass.TypesInfo.TypeOf(lit)) {
		return
	}
	var name, args ast.Expr
	for i, elt := range lit.Elts {
		switch e := elt.(type) {
		case *ast.KeyValueExpr:
			key, ok := e.Key.(*ast.Ident)
			if !ok {
				continue
			}
			switch key.Name {
			case "Name":
				name = e.Value
			case "Args":
				args = e.Value
			}
		default: // an unkeyed literal is Name, Args, ...
			switch i {
			case 0:
				name = e
			case 1:
				args = e
			}
		}
	}
	if name != nil {
		checkProgram(pass, name, args, true, ex)
	}
}

func checkNameAssign(pass *analysis.Pass, as *ast.AssignStmt, ex excuses) {
	if len(as.Lhs) != len(as.Rhs) {
		return
	}
	for i, lhs := range as.Lhs {
		sel, ok := lhs.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Name" || !isCmdType(pass.TypesInfo.TypeOf(sel.X)) {
			continue
		}
		checkProgram(pass, as.Rhs[i], nil, false, ex)
	}
}

// argElem is one element of an argv as far as the source shows it: its constant text, or the
// constant prefix of a concatenation, and whether that is all of it.
type argElem struct {
	text string
	full bool
}

// runnerFunc is the program runner a function is, if it is one.
func runnerFunc(fn *types.Func) (programRunner, bool) {
	if fn == nil || fn.Pkg() == nil {
		return programRunner{}, false
	}
	recv := ""
	if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
		t := sig.Recv().Type()
		if ptr, isPtr := t.(*types.Pointer); isPtr {
			t = ptr.Elem()
		}
		if named, isNamed := types.Unalias(t).(*types.Named); isNamed {
			recv = named.Obj().Name()
		}
	}
	for _, r := range programRunners {
		if r.method == fn.Name() && r.recv == recv && ownedBy(fn.Pkg().Path(), r.pkg) {
			return r, true
		}
	}
	return programRunner{}, false
}

// isMethodExpr is whether sel is T.Method or (*T).Method, which takes the receiver as its
// first argument.
func isMethodExpr(pass *analysis.Pass, sel *ast.SelectorExpr) bool {
	s, ok := pass.TypesInfo.Selections[sel]
	return ok && s.Kind() == types.MethodExpr
}

// checkRunnerCall judges the program a program-runner call runs.
func checkRunnerCall(pass *analysis.Pass, call *ast.CallExpr, ex excuses) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || isMethodExpr(pass, sel) {
		return
	}
	runner, ok := runnerFunc(calledFunc(pass, call))
	if !ok {
		return
	}
	if len(call.Args) <= runner.argIndex {
		if !ex.name {
			pass.Reportf(call.Pos(), "the call names no program: pass the program as the first argument after the context")
		}
		return
	}
	first := call.Args[runner.argIndex]
	if call.Ellipsis.IsValid() && runner.argIndex == len(call.Args)-1 {
		elems, complete := leadingArgs(pass, first)
		if len(elems) == 0 || !elems[0].full {
			reportDynamicProgram(pass, first, ex.name)
			return
		}
		walkProgram(pass, first, path.Base(elems[0].text), elems[1:], complete, ex.wrapper)
		return
	}
	name, ok := programName(pass, first, ex.name)
	if !ok {
		return
	}
	rest := call.Args[runner.argIndex+1:]
	complete := true
	if call.Ellipsis.IsValid() {
		rest, complete = rest[:len(rest)-1], false
	}
	args := make([]argElem, 0, len(rest))
	for _, a := range rest {
		args = append(args, textOf(pass, a))
	}
	walkProgram(pass, first, name, args, complete, ex.wrapper)
}

// checkRunnerValue flags a program runner taken as a value: the later call of that value names
// the program where no rule can see it.
func checkRunnerValue(pass *analysis.Pass, sel *ast.SelectorExpr, calledDirectly bool) {
	fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
	if !ok {
		return
	}
	if _, runner := runnerFunc(fn); runner && (!calledDirectly || isMethodExpr(pass, sel)) {
		pass.Reportf(sel.Pos(), "sudo seam method used as a value: call %s directly so the program it runs can be judged", sel.Sel.Name)
	}
}

func reportDynamicProgram(pass *analysis.Pass, at ast.Expr, dynamicOK bool) {
	if !dynamicOK {
		pass.Reportf(at.Pos(), "Name is not a compile-time constant: name the program with a constant so its owner can be judged (a program chosen at run time needs a dynamicNames row)")
	}
}

// programName is the base name of the constant program an expression names; false (after the
// finding, unless excused) when it is not a constant.
func programName(pass *analysis.Pass, nameExpr ast.Expr, dynamicOK bool) (string, bool) {
	tv, ok := pass.TypesInfo.Types[nameExpr]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		reportDynamicProgram(pass, nameExpr, dynamicOK)
		return "", false
	}
	return path.Base(constant.StringVal(tv.Value)), true
}

// checkProgram judges the program a Cmd runs: Name, and through a wrapper the program after it.
// argsShown is false when the Args are not part of the code being judged (a bare .Name =).
func checkProgram(pass *analysis.Pass, nameExpr, argsExpr ast.Expr, argsShown bool, ex excuses) {
	name, ok := programName(pass, nameExpr, ex.name)
	if !ok {
		return
	}
	var args []argElem
	complete := argsShown
	if argsShown {
		args, complete = leadingArgs(pass, argsExpr)
	}
	walkProgram(pass, nameExpr, name, args, complete, ex.wrapper)
}

// walkProgram judges a program and, through wrappers, the program each one runs. complete is
// whether args is every argument the call passes. callersJudged is true in the body of a
// program runner (programRunners): the program under a wrapper there is its parameter, and the
// calls of the runner are judged instead, so a wrapper whose arguments are not a literal is not
// a finding there. Anywhere else it is, whatever dynamicNames excuses (one whose arguments end
// before naming a program is a finding in a runner too).
func walkProgram(pass *analysis.Pass, nameExpr ast.Expr, name string, args []argElem, complete, callersJudged bool) {
	for {
		if !judgeProgram(pass, nameExpr, name, args) {
			return
		}
		spec, isWrapper := wrappers[name]
		if !isWrapper {
			return
		}
		at, found := programIndex(spec, args)
		if !found || !args[at].full {
			why := "its arguments end before naming a program"
			if !complete || slices.ContainsFunc(args, func(a argElem) bool { return !a.full }) {
				why = "its arguments are not a literal"
				if callersJudged {
					return
				}
			}
			pass.Reportf(nameExpr.Pos(), "cannot see which program %q runs: %s; spell the arguments as a literal that names the program", name, why)
			return
		}
		name = path.Base(args[at].text)
		args = args[at+1:]
	}
}

// judgeProgram reports a program this package may not run, and says whether the walk through
// wrappers should go on: it does not once the owner is reached (the owner decides its own
// program's arguments, unless the program is a wrapper marked inner), after a finding, or at a
// plain program.
func judgeProgram(pass *analysis.Pass, at ast.Expr, name string, args []argElem) bool {
	owners, owned := commandOwners[name]
	switch {
	case owned && slices.ContainsFunc(owners, func(o string) bool { return ownedBy(pass.Pkg.Path(), o) }):
		return wrappers[name].inner
	case owned:
		pass.Reportf(at.Pos(), "%q is owned by %s: use its methods instead of running %s here", name, strings.Join(owners, " or "), name)
		return false
	case shells[name] && runsShellString(args):
		pass.Reportf(at.Pos(), "%s -c is a shell string handed to the runner: run the program with its arguments as separate elements", name)
		return false
	}
	return true
}

// programIndex is the index in args of the program a wrapper runs, false when the arguments
// do not show it.
func programIndex(spec wrapper, args []argElem) (int, bool) {
	positional := spec.positional
	for i := 0; i < len(args); i++ {
		a := args[i]
		if spec.until != "" {
			if a.full && a.text == spec.until {
				return i + 1, i+1 < len(args)
			}
			continue
		}
		if a.text == "" && !a.full {
			return 0, false
		}
		switch {
		case a.text == "--":
			return i + 1, i+1 < len(args)
		case strings.HasPrefix(a.text, "-") && a.text != "-":
			if spec.valued[a.text] {
				i++
			}
		case spec.assigns && strings.Contains(a.text, "="):
		case positional > 0:
			positional--
		default:
			return i, true
		}
	}
	return 0, false
}

// runsShellString is whether the arguments, past the interpreter's options, include -c.
func runsShellString(args []argElem) bool {
	for _, a := range args {
		if !a.full || !strings.HasPrefix(a.text, "-") || strings.HasPrefix(a.text, "--") {
			return false
		}
		if strings.Contains(a.text, "c") {
			return true
		}
	}
	return false
}

// leadingArgs is the elements of an argv expression the source shows, and whether that is
// every element. A literal, append of literals and slices.Concat of literals are shown; a
// variable or a call is not.
func leadingArgs(pass *analysis.Pass, expr ast.Expr) ([]argElem, bool) {
	if expr == nil {
		return nil, true
	}
	switch e := ast.Unparen(expr).(type) {
	case *ast.CompositeLit:
		elems := make([]argElem, 0, len(e.Elts))
		for _, elt := range e.Elts {
			if _, keyed := elt.(*ast.KeyValueExpr); keyed {
				return elems, false
			}
			elems = append(elems, textOf(pass, elt))
		}
		return elems, true
	case *ast.Ident:
		return nil, pass.TypesInfo.Types[e].IsNil()
	case *ast.CallExpr:
		return leadingCallArgs(pass, e)
	}
	return nil, false
}

// isAppend is whether call is the builtin append with a slice to append to.
func isAppend(pass *analysis.Pass, call *ast.CallExpr) bool {
	id, ok := call.Fun.(*ast.Ident)
	if !ok || len(call.Args) == 0 {
		return false
	}
	b, isBuiltin := pass.TypesInfo.Uses[id].(*types.Builtin)
	return isBuiltin && b.Name() == "append"
}

func leadingCallArgs(pass *analysis.Pass, call *ast.CallExpr) ([]argElem, bool) {
	if isAppend(pass, call) {
		elems, complete := leadingArgs(pass, call.Args[0])
		if !complete {
			return elems, false
		}
		if call.Ellipsis.IsValid() && len(call.Args) == 2 {
			more, ok := leadingArgs(pass, call.Args[1])
			return append(elems, more...), ok
		}
		for _, a := range call.Args[1:] {
			elems = append(elems, textOf(pass, a))
		}
		return elems, true
	}
	if isPkgFunc(calledFunc(pass, call), "slices", "Concat") {
		var elems []argElem
		for _, a := range call.Args {
			more, complete := leadingArgs(pass, a)
			elems = append(elems, more...)
			if !complete {
				return elems, false
			}
		}
		return elems, true
	}
	return nil, false
}

// textOf is the constant text of an argument, or the constant prefix of a concatenation.
func textOf(pass *analysis.Pass, expr ast.Expr) argElem {
	if tv, ok := pass.TypesInfo.Types[expr]; ok && tv.Value != nil && tv.Value.Kind() == constant.String {
		return argElem{text: constant.StringVal(tv.Value), full: true}
	}
	if b, ok := ast.Unparen(expr).(*ast.BinaryExpr); ok && b.Op.String() == "+" {
		left := textOf(pass, b.X)
		if !left.full {
			return left
		}
		right := textOf(pass, b.Y)
		return argElem{text: left.text + right.text, full: right.full}
	}
	return argElem{}
}
