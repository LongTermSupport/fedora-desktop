package rules

import (
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"slices"

	"golang.org/x/tools/go/analysis"
)

// LeakcheckOnce forbids a second leakcheck.Check on one test, which judges too early.
var LeakcheckOnce = &analysis.Analyzer{
	Name: "project_leakcheck_once",
	Doc: `project_leakcheck_once flags a call that registers leakcheck.Check again on a test it is
already registered on.

A call registers the check on a test when it is a leakcheck package's Check, or a function or
method of the same package that passes one of its parameters on to such a call (directly, or
through another such function); the test is the identifier given at that parameter. Flagged:
within one function body (a function literal is a body of its own), the second such call on the
same identifier, and such a call inside a for or range loop of the body.

A test's cleanups run last in, first out. Check judges in a cleanup, so only the first Check on
a test runs after every cleanup registered later; a second one judges before the cleanups
registered between the two have run, and reports the goroutines of a world those cleanups stop
as a leak. TestLiveNeedsEveryFact built two engine worlds, each registering the check: the
second world's check failed, by the shuffle's order and the scheduler, on the first world's
transcript watcher, which that world's teardown closes and waits for.

Not caught: a test passed in a struct field or through a function value; a cleanup that stops
goroutines registered before the test's first Check.

How to fix: register the check once per test, first: build a second world from the first (a
method that builds without Check), or give each its own test with t.Run.`,
	Run:        perPass(runLeakcheckOnce),
	ResultType: noResultType,
}

// checkers is, for each function of the package that registers the check, the indices of the
// parameters that carry the test to it.
type checkers map[*types.Func]map[int]bool

func runLeakcheckOnce(pass *analysis.Pass) {
	decls := map[*types.Func]*ast.FuncDecl{}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				if fn, isFunc := pass.TypesInfo.Defs[fd.Name].(*types.Func); isFunc {
					decls[fn] = fd
				}
			}
		}
	}
	c := findCheckers(pass, decls)
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncDecl:
				if n.Body != nil {
					c.judge(pass, n.Body)
				}
			case *ast.FuncLit:
				c.judge(pass, n.Body)
			}
			return true
		})
	}
}

// findCheckers grows the set from leakcheck.Check until no function of the package adds one.
func findCheckers(pass *analysis.Pass, decls map[*types.Func]*ast.FuncDecl) checkers {
	c := checkers{}
	for grew := true; grew; {
		grew = false
		for fn, fd := range decls {
			params := paramIndices(pass, fd)
			ownCalls(fd.Body, func(call *ast.CallExpr, _ bool) {
				for _, arg := range c.tests(pass, call) {
					i, isParam := params[pass.TypesInfo.ObjectOf(arg)]
					if isParam && !c[fn][i] {
						if c[fn] == nil {
							c[fn] = map[int]bool{}
						}
						c[fn][i], grew = true, true
					}
				}
			})
		}
	}
	return c
}

// paramIndices is each named parameter of fd's object, by its index.
func paramIndices(pass *analysis.Pass, fd *ast.FuncDecl) map[types.Object]int {
	out := map[types.Object]int{}
	i := 0
	for _, field := range fd.Type.Params.List {
		if len(field.Names) == 0 {
			i++
			continue
		}
		for _, name := range field.Names {
			if obj := pass.TypesInfo.Defs[name]; obj != nil {
				out[obj] = i
			}
			i++
		}
	}
	return out
}

// tests is the identifiers call registers the check on: none when it does not.
func (c checkers) tests(pass *analysis.Pass, call *ast.CallExpr) []*ast.Ident {
	fn := calledFunc(pass, call)
	if fn == nil {
		return nil
	}
	fn = fn.Origin()
	indices := c[fn]
	if isLeakcheckCheck(fn) {
		indices = map[int]bool{0: true}
	}
	var out []*ast.Ident
	for _, i := range slices.Sorted(maps.Keys(indices)) {
		if i < len(call.Args) {
			if id, ok := ast.Unparen(call.Args[i]).(*ast.Ident); ok {
				out = append(out, id)
			}
		}
	}
	return out
}

func isLeakcheckCheck(fn *types.Func) bool {
	sig, ok := fn.Type().(*types.Signature)
	return ok && sig.Recv() == nil && fn.Name() == "Check" && fn.Pkg() != nil && fn.Pkg().Name() == "leakcheck"
}

// judge reports, in body, a registration on a test already registered on, or one in a loop.
func (c checkers) judge(pass *analysis.Pass, body *ast.BlockStmt) {
	seen := map[types.Object]bool{}
	ownCalls(body, func(call *ast.CallExpr, inLoop bool) {
		for _, id := range c.tests(pass, call) {
			obj := pass.TypesInfo.ObjectOf(id)
			if obj == nil {
				continue
			}
			if seen[obj] || inLoop {
				pass.Reportf(call.Pos(), "leakcheck.Check registered again on %s, through %s: the later check judges before the cleanups registered between them run; register it once per test, first (build a second world from the first, or use t.Run)", id.Name, calleeName(call))
			}
			seen[obj] = true
		}
	})
}

// calleeName is how call names its function: f, pkg.F or x.Method.
func calleeName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		if x, ok := fn.X.(*ast.Ident); ok {
			return x.Name + "." + fn.Sel.Name
		}
		return fn.Sel.Name
	case *ast.Ident:
		return fn.Name
	}
	return "a call"
}

// ownCalls visits, in source order, every call of body's own (not a nested function
// literal's), with whether it is inside a for or range loop of body.
func ownCalls(body *ast.BlockStmt, visit func(call *ast.CallExpr, inLoop bool)) {
	var loops []ast.Node
	inLoop := func(p token.Pos) bool {
		for _, l := range loops {
			if l.Pos() <= p && p < l.End() {
				return true
			}
		}
		return false
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ForStmt:
			loops = append(loops, n.Body)
		case *ast.RangeStmt:
			loops = append(loops, n.Body)
		case *ast.CallExpr:
			visit(n, inLoop(n.Pos()))
		}
		return true
	})
}
