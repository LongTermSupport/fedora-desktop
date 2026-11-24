package rules

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
)

// TempNotExclusive forbids creating a temporary that is renamed or linked into place without
// O_EXCL.
var TempNotExclusive = &analysis.Analyzer{
	Name: "project_temp_not_exclusive",
	Doc: `project_temp_not_exclusive flags, in non-test files, a file created without O_EXCL at a
path the same function then renames or links into place.

A create is os.WriteFile, os.Create, os.OpenFile, or os.Root's WriteFile, Create or OpenFile, or
unix.Open or Openat; OpenFile and the unix calls are clean when their flags (a constant
expression, or an expression naming a constant) hold O_EXCL. A put in place is os.Rename,
os.Link, os.Root's Rename or Link, or unix.Rename, Renameat, Renameat2, Link or Linkat. The path
created and the path moved are the same when they are the same variable or the same expression
as written.

A create without O_EXCL opens what is already at the path: a link planted there is followed, and
the write lands wherever it points. A temporary's name is often predictable (the target's name
with a pid or a fixed suffix), so anyone who can write the directory (a container that shares
it) can plant one first. fsx.WriteAtomic wrote <path>.tmp.<pid> with os.WriteFile (the
fedora-desktop#417 review).

Not caught: a temporary created in one function and moved in another; a move through a
wrapper (an fs interface's Rename); a path rebuilt differently between the two calls; a file
written in place that is never moved (the target's own name, which this rule does not judge).

How to fix: create the temporary with os.CreateTemp in the target's directory (a random name,
opened O_CREATE|O_EXCL) and rename f.Name(); or open it with O_CREATE|O_EXCL (and O_NOFOLLOW
for the raw call) and remove a leftover only after the exclusive create has failed.`,
	Run:        perPass(runTempNotExclusive),
	ResultType: noResultType,
	FactTypes:  []analysis.Fact{new(tempCreatesParamFact)},
}

const tempNotExclusiveMsg = "%s created without O_EXCL at a path this function then renames or links into place: " +
	"a link planted at the name is followed and the write lands where it points (project_temp_not_exclusive): " +
	"create it with os.CreateTemp in the target's directory, or with O_CREATE|O_EXCL"

// oExcl and oCreat are O_EXCL and O_CREAT as Linux numbers them (os, syscall and unix alike).
const (
	oExcl  = 0x80
	oCreat = 0x40
)

// tempCall is a create or a put in place: the function, the index of its path argument, and of
// its flags (-1 for none).
type tempCall struct {
	pkg, recv, name string
	path, flags     int
}

// tempCreates are the creates the rule judges.
var tempCreates = []tempCall{
	{"os", "", "WriteFile", 0, -1},
	{"os", "", "Create", 0, -1},
	{"os", "", "OpenFile", 0, 1},
	{"os", "Root", "WriteFile", 0, -1},
	{"os", "Root", "Create", 0, -1},
	{"os", "Root", "OpenFile", 0, 1},
	{unixPkg, "", "Open", 0, 1},
	{unixPkg, "", "Openat", 1, 2},
	{"syscall", "", "Open", 0, 1},
}

// tempMoves are the calls that put a file in place under another name.
var tempMoves = []tempCall{
	{"os", "", "Rename", 0, -1},
	{"os", "", "Link", 0, -1},
	{"os", "Root", "Rename", 0, -1},
	{"os", "Root", "Link", 0, -1},
	{unixPkg, "", "Rename", 0, -1},
	{unixPkg, "", "Link", 0, -1},
	{unixPkg, "", "Renameat", 1, -1},
	{unixPkg, "", "Renameat2", 1, -1},
	{unixPkg, "", "Linkat", 1, -1},
	{"syscall", "", "Rename", 0, -1},
	{"syscall", "", "Link", 0, -1},
}

// tempCreatesParamFact marks a function that creates the paths its parameters at Params name
// without O_EXCL.
type tempCreatesParamFact struct{ Params []int }

func (*tempCreatesParamFact) AFact() {}

func (f *tempCreatesParamFact) String() string {
	return fmt.Sprintf("creates its parameters %v without O_EXCL", f.Params)
}

// tempPass is one package's judgement: the functions of the package known to create a
// parameter's path without O_EXCL.
type tempPass struct {
	pass  *analysis.Pass
	local map[*types.Func][]int
}

func runTempNotExclusive(pass *analysis.Pass) {
	t := &tempPass{pass: pass, local: map[*types.Func][]int{}}
	var decls []*ast.FuncDecl
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				decls = append(decls, fd)
			}
		}
	}
	// A helper of the package can call another: settle them to a fixed point.
	for changed := true; changed; {
		changed = false
		for _, fd := range decls {
			fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func)
			if !ok {
				continue
			}
			if params := t.createdParams(fd); len(params) > len(t.local[fn]) {
				t.local[fn], changed = params, true
			}
		}
	}
	for fn, params := range t.local {
		pass.ExportObjectFact(fn, &tempCreatesParamFact{Params: params})
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch fn := n.(type) {
			case *ast.FuncDecl:
				if fn.Body != nil {
					t.checkBody(fn.Body)
				}
			case *ast.FuncLit:
				t.checkBody(fn.Body)
			}
			return true
		})
	}
}

// createdParams is the indexes of fd's parameters whose path its body creates without O_EXCL.
func (t *tempPass) createdParams(fd *ast.FuncDecl) []int {
	keys := map[string]bool{}
	for _, c := range t.creates(fd.Body) {
		// A child program's arguments are not a helper's create: the program may only read them.
		if _, child := c.at.(*ast.CompositeLit); !child {
			keys[c.key] = true
		}
	}
	var out []int
	i := 0
	for _, field := range fd.Type.Params.List {
		for _, name := range field.Names {
			if keys[tempPathKey(t.pass, name)] {
				out = append(out, i)
			}
			i++
		}
		if len(field.Names) == 0 {
			i++
		}
	}
	return out
}

// paramsCreated is the parameters fn creates without O_EXCL, from this package's settling or
// another's fact.
func (t *tempPass) paramsCreated(fn *types.Func) []int {
	if fn == nil {
		return nil
	}
	if fn.Pkg() == t.pass.Pkg {
		return t.local[fn]
	}
	var fact tempCreatesParamFact
	if t.pass.ImportObjectFact(fn, &fact) {
		return fact.Params
	}
	return nil
}

// tempCreate is one create found in a body.
type tempCreate struct {
	at   ast.Node
	name string
	key  string
	path ast.Expr
}

// creates is every create in body without O_EXCL (its nested function literals aside): a call
// of tempCreates, a call of a function that creates a parameter's path, and a path among the
// arguments of an execx.Cmd, which a child program may write.
func (t *tempPass) creates(body *ast.BlockStmt) []tempCreate {
	var out []tempCreate
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CompositeLit:
			out = append(out, t.childWrites(body, n)...)
		case *ast.CallExpr:
			fn := calledFunc(t.pass, n)
			if c, found := matchTempCall(fn, tempCreates); found {
				if c.path < len(n.Args) && t.nonExclusive(body, c, n) {
					out = append(out, tempCreate{at: n, name: tempCallName(c), key: tempPathKey(t.pass, n.Args[c.path]), path: n.Args[c.path]})
				}
				return true
			}
			for _, i := range t.paramsCreated(fn) {
				if i < len(n.Args) {
					out = append(out, tempCreate{at: n, name: fn.Name(), key: tempPathKey(t.pass, n.Args[i]), path: n.Args[i]})
				}
			}
		}
		return true
	})
	// A path under a directory or name only this process made carries no planted link.
	return slices.DeleteFunc(out, func(c tempCreate) bool { return freshPath(t.pass, body, c.path) })
}

// nonExclusive is whether the create call c (a row of tempCreates) creates without O_EXCL: an
// open whose flags hold O_CREAT and not O_EXCL, or a call that always creates.
func (t *tempPass) nonExclusive(body *ast.BlockStmt, c tempCall, call *ast.CallExpr) bool {
	if c.flags < 0 {
		return true
	}
	if c.flags >= len(call.Args) {
		return false
	}
	flags := call.Args[c.flags]
	return holdsBit(t.pass, body, flags, oCreat) && !holdsBit(t.pass, body, flags, oExcl)
}

// childWrites is a path named among the Args of an execx.Cmd literal (directly, or in a local
// slice the body builds), each a path the child program may create.
func (t *tempPass) childWrites(body *ast.BlockStmt, lit *ast.CompositeLit) []tempCreate {
	named, ok := types.Unalias(t.pass.TypesInfo.TypeOf(lit)).(*types.Named)
	if !ok || named.Obj().Name() != "Cmd" || named.Obj().Pkg() == nil || !ownedBy(named.Obj().Pkg().Path(), "internal/execx") {
		return nil
	}
	var out []tempCreate
	for _, el := range lit.Elts {
		kv, isKV := el.(*ast.KeyValueExpr)
		if !isKV {
			continue
		}
		if id, isIdent := kv.Key.(*ast.Ident); !isIdent || id.Name != "Args" {
			continue
		}
		for _, e := range argExprs(t.pass, body, kv.Value) {
			out = append(out, tempCreate{at: lit, name: "a child program (execx.Cmd)", key: tempPathKey(t.pass, e), path: e})
		}
	}
	return out
}

// argExprs is every expression an argument list holds: a slice literal's elements, an append's
// arguments, and for a local slice, what the body assigns it.
func argExprs(pass *analysis.Pass, body *ast.BlockStmt, e ast.Expr) []ast.Expr {
	var out []ast.Expr
	seen := map[types.Object]bool{}
	var walk func(ast.Expr)
	walk = func(e ast.Expr) {
		switch e := ast.Unparen(e).(type) {
		case *ast.CompositeLit:
			for _, el := range e.Elts {
				walk(el)
			}
		case *ast.CallExpr:
			if id, ok := e.Fun.(*ast.Ident); ok && id.Name == "append" {
				for _, a := range e.Args {
					walk(a)
				}
				return
			}
			out = append(out, e)
		case *ast.Ident:
			out = append(out, e)
			v, ok := pass.TypesInfo.ObjectOf(e).(*types.Var)
			if !ok || seen[v] {
				return
			}
			if _, isSlice := v.Type().Underlying().(*types.Slice); !isSlice {
				return
			}
			seen[v] = true
			ast.Inspect(body, func(n ast.Node) bool {
				as, isAssign := n.(*ast.AssignStmt)
				if !isAssign || len(as.Lhs) != len(as.Rhs) {
					return true
				}
				for i, l := range as.Lhs {
					if id, isIdent := l.(*ast.Ident); isIdent && pass.TypesInfo.ObjectOf(id) == v {
						walk(as.Rhs[i])
					}
				}
				return true
			})
		default:
			out = append(out, e)
		}
	}
	walk(e)
	return out
}

// checkBody reports each create in body made without O_EXCL whose path the body moves after it
// (a file renamed aside before the target is written is a rotation, not a temporary).
func (t *tempPass) checkBody(body *ast.BlockStmt) {
	creates := t.creates(body)
	moved := map[string][]token.Pos{}
	ast.Inspect(body, func(n ast.Node) bool {
		if _, isLit := n.(*ast.FuncLit); isLit {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if c, found := matchTempCall(calledFunc(t.pass, call), tempMoves); found && c.path < len(call.Args) {
			key := tempPathKey(t.pass, call.Args[c.path])
			moved[key] = append(moved[key], call.Pos())
		}
		return true
	})
	for _, c := range creates {
		if slices.ContainsFunc(moved[c.key], func(at token.Pos) bool { return at > c.at.Pos() }) {
			t.pass.Reportf(c.at.Pos(), tempNotExclusiveMsg, c.name)
		}
	}
}

// matchTempCall is the row of table fn is.
func matchTempCall(fn *types.Func, table []tempCall) (tempCall, bool) {
	if fn == nil || fn.Pkg() == nil {
		return tempCall{}, false
	}
	recv := ""
	if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
		t := sig.Recv().Type()
		if ptr, isPtr := t.(*types.Pointer); isPtr {
			t = ptr.Elem()
		}
		named, isNamed := t.(*types.Named)
		if !isNamed {
			return tempCall{}, false
		}
		recv = named.Obj().Name()
	}
	for _, c := range table {
		if c.pkg == fn.Pkg().Path() && c.recv == recv && c.name == fn.Name() {
			return c, true
		}
	}
	return tempCall{}, false
}

func tempCallName(c tempCall) string {
	pkg := c.pkg
	if pkg == unixPkg {
		pkg = "unix"
	}
	if c.recv != "" {
		return pkg + "." + c.recv + "." + c.name
	}
	return pkg + "." + c.name
}

// tempPathKey is what makes two path expressions the same path: the variable, or the expression
// as written, with a filepath.Clean around it taken off.
func tempPathKey(pass *analysis.Pass, e ast.Expr) string {
	e = ast.Unparen(e)
	if call, ok := e.(*ast.CallExpr); ok && len(call.Args) == 1 && isPkgFunc(calledFunc(pass, call), filepathPkg, "Clean") {
		return tempPathKey(pass, call.Args[0])
	}
	if id, ok := e.(*ast.Ident); ok {
		if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
			return "var " + obj.Name() + "@" + pass.Fset.Position(obj.Pos()).String()
		}
	}
	return types.ExprString(e)
}

// holdsBit is whether the flags expression holds bit: a constant with the bit, any part of it
// that is one, or a local variable of body assigned one.
func holdsBit(pass *analysis.Pass, body *ast.BlockStmt, e ast.Expr, bit int64) bool {
	return anyAssigned(pass, body, e, 0, func(expr ast.Expr) bool {
		tv, has := pass.TypesInfo.Types[expr]
		if !has || tv.Value == nil {
			return false
		}
		v, exact := constant.Int64Val(constant.ToInt(tv.Value))
		return exact && v&bit != 0
	})
}

// freshPath is whether the path expression is built from a name os.MkdirTemp or os.CreateTemp
// chose (directly, or through the body's locals): a directory or file only this process made.
func freshPath(pass *analysis.Pass, body *ast.BlockStmt, e ast.Expr) bool {
	return anyAssigned(pass, body, e, 0, func(expr ast.Expr) bool {
		call, ok := expr.(*ast.CallExpr)
		if !ok {
			return false
		}
		fn := calledFunc(pass, call)
		return isPkgFunc(fn, "os", "MkdirTemp") || isPkgFunc(fn, "os", "CreateTemp")
	})
}

// anyAssigned is whether match holds for a part of e, or of what the body assigns a local
// variable e names, followed a few assignments deep. A constant is not looked inside.
func anyAssigned(pass *analysis.Pass, body *ast.BlockStmt, e ast.Expr, depth int, match func(ast.Expr) bool) bool {
	const maxDepth = 4
	if depth > maxDepth {
		return false
	}
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if found {
			return false
		}
		expr, ok := n.(ast.Expr)
		if !ok {
			return true
		}
		if match(expr) {
			found = true
			return false
		}
		if tv, has := pass.TypesInfo.Types[expr]; has && tv.Value != nil {
			return false
		}
		if id, isIdent := expr.(*ast.Ident); isIdent {
			if v, isVar := pass.TypesInfo.ObjectOf(id).(*types.Var); isVar {
				found = assignedMatch(pass, body, v, depth, match)
			}
		}
		return true
	})
	return found
}

// assignedMatch is whether any assignment of v in body matches (anyAssigned).
func assignedMatch(pass *analysis.Pass, body *ast.BlockStmt, v *types.Var, depth int, match func(ast.Expr) bool) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, l := range as.Lhs {
			id, isIdent := l.(*ast.Ident)
			if !isIdent || pass.TypesInfo.ObjectOf(id) != v {
				continue
			}
			rhs := as.Rhs[0]
			if len(as.Lhs) == len(as.Rhs) {
				rhs = as.Rhs[i]
			}
			if anyAssigned(pass, body, rhs, depth+1, match) {
				found = true
			}
		}
		return !found
	})
	return found
}
