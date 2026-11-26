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

// CheckoutPathSeam confines the opening of a path in a checkout's .claude/ccy to an os.Root of the
// checkout, or to internal/checkoutpin.
var CheckoutPathSeam = &analysis.Analyzer{
	Name: "project_checkout_path_seam",
	Doc: `project_checkout_path_seam flags, in src-go's internal/ and cmd/ trees, a call that opens,
reads, writes, makes, removes, renames, changes the mode of or walks a path under a checkout's
.claude/ccy by its path name, outside internal/checkoutpin: os's ReadFile, WriteFile, Open,
OpenFile, Create, Mkdir, MkdirAll, Remove, RemoveAll, Rename, ReadDir, Chmod, Chown, Lchown,
Truncate, Symlink, Link, Chtimes, DirFS and OpenRoot, filepath's Walk and WalkDir, and the
module's fsx.WriteAtomic.

A ccy container writes its checkout, .claude/ccy included, so it can put a link at any name
there, or make .claude/ccy itself a link. A host call that opens such a path by name follows
the link: the host then reads, binds, truncates, creates or deletes a host file the container
chose (Plan 00266 review findings 1, 6 and 7: the ccy.env.local bind, the .gitignore and
ccy.env.local.dist writes, the custom Dockerfile build). The class is "host code acts on a path
in a container-writable checkout without going through the checkoutpin or os.Root seam"; its
bash half is the semgrep rule ccy-host-applies-checkout-config, and the naming of the files the
host applies is project_checkout_config_pin.

A call's path is followed through constants, filepath.Join and path.Join (".claude", "ccy" as
two pieces too), string concatenation, a local the function assigns it, a function of the
package that returns it, and a helper it is passed to (f.at(rel)). The user's own ~/.claude is
not a checkout path. Test files are not judged (a test plants the links), nor a function that takes a
*testing.T, *B or *F or a testing.TB (a test helper in a fake machine's package, such as
teamtest), nor the nested qa module (its gates read the checkout they were run in).

Not caught: a path built at run time from words that are each not a piece of the name, one
held in a field or a parameter, and a call through a wrapper of another package.

How to fix: open the checkout with os.OpenRoot and act through the *os.Root (Lstat to refuse a
link at the name, an exclusive temporary renamed into place for a write), or use
internal/checkoutpin's ReadFile, WriteFile and CopyOut.`,
	Run:        perPass(runCheckoutPathSeam),
	ResultType: noResultType,
}

const checkoutPathFix = "%s acts on %s, a path in a checkout a container writes, by its name, so it follows a link the container put there " +
	"(project_checkout_path_seam): open the checkout with os.OpenRoot and act through the *os.Root, or use internal/checkoutpin's ReadFile, WriteFile and CopyOut"

// checkoutPathMark is the directory whose paths a container session writes.
const checkoutPathMark = ".claude/ccy"

// checkoutPathCalls are the calls that act on a path by name: package, function.
var checkoutPathCalls = [][2]string{
	{"os", "ReadFile"},
	{"os", "WriteFile"},
	{"os", "Open"},
	{"os", "OpenFile"},
	{"os", "Create"},
	{"os", "Mkdir"},
	{"os", "MkdirAll"},
	{"os", "Remove"},
	{"os", "RemoveAll"},
	{"os", "Rename"},
	{"os", "ReadDir"},
	{"os", "Chmod"},
	{"os", "Chown"},
	{"os", "Lchown"},
	{"os", "Truncate"},
	{"os", "Symlink"},
	{"os", "Link"},
	{"os", "Chtimes"},
	{"os", "DirFS"},
	{"os", "OpenRoot"},
	{"path/filepath", "Walk"},
	{"path/filepath", "WalkDir"},
	{"internal/fsx", "WriteAtomic"},
}

// checkoutPathDepth bounds how far a path is followed through locals and functions.
const checkoutPathDepth = 6

func runCheckoutPathSeam(pass *analysis.Pass) {
	pkg := pass.Pkg.Path()
	judged := inTree(pkg, "internal") || inTree(pkg, "cmd")
	if !judged || inTree(pkg, "qa") || ownedBy(pkg, checkoutPinPkg) {
		return
	}
	funcs := packageFuncs(pass)
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil || takesTesting(pass, fd) {
				continue
			}
			w := pathWalk{pass: pass, body: fd.Body, funcs: funcs}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if call, isCall := n.(*ast.CallExpr); isCall {
					w.judge(call)
				}
				return true
			})
		}
	}
}

// takesTesting is whether fd is a test helper: one of its parameters is a *testing.T, *B or *F,
// or a testing.TB, so it plants a test's own world (a fake machine's package outside a _test.go
// file), never acts for the host.
func takesTesting(pass *analysis.Pass, fd *ast.FuncDecl) bool {
	for _, field := range fd.Type.Params.List {
		t := pass.TypesInfo.TypeOf(field.Type)
		if ptr, isPtr := t.(*types.Pointer); isPtr {
			t = ptr.Elem()
		}
		if named, isNamed := t.(*types.Named); isNamed && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "testing" {
			return true
		}
	}
	return false
}

// judge reports a call that acts by name on a checkout path one of its arguments names.
func (w pathWalk) judge(call *ast.CallExpr) {
	name, acts := checkoutPathCall(calledFunc(w.pass, call))
	if !acts {
		return
	}
	for _, arg := range call.Args {
		if rel, found := w.rel(arg, 0); found {
			w.pass.Reportf(call.Pos(), checkoutPathFix, name, rel)
			return
		}
	}
}

// checkoutPathCall is the name of a call that acts on a path by name, as a report says it.
func checkoutPathCall(fn *types.Func) (string, bool) {
	if fn == nil || fn.Pkg() == nil {
		return "", false
	}
	if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
		return "", false
	}
	for _, c := range checkoutPathCalls {
		if fn.Name() == c[1] && (fn.Pkg().Path() == c[0] || strings.HasSuffix(fn.Pkg().Path(), "/"+c[0])) {
			return fn.Pkg().Name() + "." + fn.Name(), true
		}
	}
	return "", false
}

// packageFuncs are the package's function declarations, by their object.
func packageFuncs(pass *analysis.Pass) map[*types.Func]*ast.FuncDecl {
	out := map[*types.Func]*ast.FuncDecl{}
	for _, f := range pass.Files {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			if obj, isFunc := pass.TypesInfo.Defs[fd.Name].(*types.Func); isFunc {
				out[obj] = fd
			}
		}
	}
	return out
}

// pathWalk follows an expression to the checkout path it names, inside one function.
type pathWalk struct {
	pass  *analysis.Pass
	body  *ast.BlockStmt
	funcs map[*types.Func]*ast.FuncDecl
}

// rel is the .claude/ccy path the expression names, from the mark on, or false.
func (w pathWalk) rel(e ast.Expr, depth int) (string, bool) {
	if depth > checkoutPathDepth || e == nil {
		return "", false
	}
	if s, ok := w.constString(e); ok {
		return markedPath([]string{s})
	}
	switch x := e.(type) {
	case *ast.ParenExpr:
		return w.rel(x.X, depth+1)
	case *ast.BinaryExpr:
		if left, ok := w.rel(x.X, depth+1); ok {
			if right, isConst := w.constString(x.Y); isConst {
				return left + right, true
			}
			return left, true
		}
		return w.rel(x.Y, depth+1)
	case *ast.Ident:
		return w.local(x, depth)
	case *ast.CallExpr:
		return w.call(x, depth)
	}
	return "", false
}

// call follows a join's pieces, a package function's result, then the arguments of any helper.
func (w pathWalk) call(c *ast.CallExpr, depth int) (string, bool) {
	fn := calledFunc(w.pass, c)
	if fn != nil && fn.Pkg() != nil && fn.Name() == "Join" && (fn.Pkg().Path() == "path/filepath" || fn.Pkg().Path() == "path") {
		return w.join(c, depth)
	}
	if fd, ok := w.funcs[fn]; ok {
		if rel, found := w.returned(fd, depth); found {
			return rel, true
		}
	}
	for _, arg := range c.Args {
		if rel, ok := w.rel(arg, depth+1); ok {
			return rel, true
		}
	}
	return "", false
}

// join is the path a join's pieces name: its constant pieces, and a piece that names one.
func (w pathWalk) join(c *ast.CallExpr, depth int) (string, bool) {
	var pieces []string
	for _, arg := range c.Args {
		if s, ok := w.constString(arg); ok {
			pieces = append(pieces, s)
			continue
		}
		if inner, ok := w.rel(arg, depth+1); ok {
			pieces = []string{inner}
			continue
		}
		if rel, ok := markedPath(pieces); ok {
			return rel, true
		}
		pieces = nil
	}
	return markedPath(pieces)
}

// returned is the path a package function returns.
func (w pathWalk) returned(fd *ast.FuncDecl, depth int) (string, bool) {
	inner := pathWalk{pass: w.pass, body: fd.Body, funcs: w.funcs}
	var rel string
	var found bool
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		ret, isRet := n.(*ast.ReturnStmt)
		if found || !isRet {
			return !found
		}
		for _, r := range ret.Results {
			if rel, found = inner.rel(r, depth+1); found {
				return false
			}
		}
		return false
	})
	return rel, found
}

// local follows a variable to what the function assigns it.
func (w pathWalk) local(id *ast.Ident, depth int) (string, bool) {
	v, ok := w.pass.TypesInfo.Uses[id].(*types.Var)
	if !ok || v.IsField() {
		return "", false
	}
	var rel string
	var found bool
	ast.Inspect(w.body, func(n ast.Node) bool {
		as, isAssign := n.(*ast.AssignStmt)
		if found || !isAssign || len(as.Lhs) != len(as.Rhs) {
			return !found
		}
		for i, lhs := range as.Lhs {
			l, isIdent := lhs.(*ast.Ident)
			if !isIdent || l.Pos() >= id.Pos() {
				continue
			}
			obj := w.pass.TypesInfo.Defs[l]
			if obj == nil {
				obj = w.pass.TypesInfo.Uses[l]
			}
			if obj == v {
				rel, found = w.rel(as.Rhs[i], depth+1)
			}
		}
		return !found
	})
	return rel, found
}

// constString is the expression's constant string value.
func (w pathWalk) constString(e ast.Expr) (string, bool) {
	tv, ok := w.pass.TypesInfo.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(tv.Value), true
}

// markedPath is the joined pieces from the mark on (".claude/ccy" in one piece, or ".claude"
// then "ccy"), or false when they hold no mark.
func markedPath(pieces []string) (string, bool) {
	for i, p := range pieces {
		if at := strings.Index(p, checkoutPathMark); at >= 0 && (at == 0 || p[at-1] == '/') {
			return path.Join(slices.Concat([]string{p[at:]}, pieces[i+1:])...), true
		}
		if path.Clean(p) == ".claude" && i+1 < len(pieces) && strings.HasPrefix(pieces[i+1], "ccy") {
			return path.Join(slices.Concat([]string{".claude"}, pieces[i+1:])...), true
		}
	}
	return "", false
}
