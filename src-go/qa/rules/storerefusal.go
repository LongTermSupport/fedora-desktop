package rules

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// StoreRefusalLost forbids losing a seat-store add's refusal: the error of a state.Store Add
// method that refuses when its list is full must reach a return of the function it is called in.
var StoreRefusalLost = &analysis.Analyzer{
	Name: "project_store_refusal_lost",
	Doc: `project_store_refusal_lost flags, outside teamchat's store package
(internal/teamchat/member/state) and outside test files, a call of a state.Store method named
Add... whose last result is an error, when that error reaches no return statement of the
function (or function literal) the call is in: the result discarded as a statement or into _,
held in a variable no return names (the if-init form with a log line in its body among them),
or passed anywhere but a return.

The store's capped lists refuse an entry when full (B 2.3.2: "refused loudly, never dropped").
The refusal is what makes the whole write fail, so nothing is lost and the work is tried again.
Answered by a log line, the write goes ahead without the entry, and what it stood for is gone:
the proof's read receipt for a HUMAN item was lost this way whenever the receipt queue was
full (Plan 00267 DESIGN C2), so the human never saw the message read.

Not flagged: package state, which owns the lists; an add with no error result (the ledger and
missed lists roll); a test file, which fills a store past its cap on purpose.

How to fix: return the refusal (wrapped or not), so the write is refused and tried again; or
make room in the list before the add, so the add cannot refuse.`,
	Run:        perPass(runStoreRefusalLost),
	ResultType: noResultType,
}

const storeRefusalMsg = "a seat-store add's refusal that reaches no return (project_store_refusal_lost): the write goes ahead without the entry; return the error"

func runStoreRefusalLost(pass *analysis.Pass) {
	if ownedBy(pass.Pkg.Path(), teamchatStatePkg) {
		return
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch fn := n.(type) {
			case *ast.FuncDecl:
				if fn.Body != nil {
					judgeStoreAdds(pass, fn.Body)
				}
			case *ast.FuncLit:
				judgeStoreAdds(pass, fn.Body)
			}
			return true
		})
	}
}

// judgeStoreAdds reports each store add in body, not in a nested function literal, whose
// error reaches no return of body.
func judgeStoreAdds(pass *analysis.Pass, body *ast.BlockStmt) {
	c := &storeAdds{pass: pass, returned: map[types.Object]bool{}, ok: map[*ast.CallExpr]bool{}}
	inspectOwn(body, c.visit)
	for _, h := range c.held {
		if h.obj != nil && c.returned[h.obj] {
			c.ok[h.call] = true
		}
	}
	for _, call := range c.adds {
		if !c.ok[call] {
			pass.Reportf(call.Pos(), "%s", storeRefusalMsg)
		}
	}
}

// storeAdds is one function body's store adds, the variables its returns name, and the adds
// whose result is held in a variable or returned.
type storeAdds struct {
	pass     *analysis.Pass
	returned map[types.Object]bool
	ok       map[*ast.CallExpr]bool
	adds     []*ast.CallExpr
	held     []heldAdd
}

func (c *storeAdds) visit(n ast.Node) {
	switch x := n.(type) {
	case *ast.ReturnStmt:
		for _, r := range x.Results {
			markReturned(c.pass, r, c.returned, c.ok)
		}
	case *ast.AssignStmt:
		c.assigned(x)
	case *ast.IfStmt:
		if refuses(x.Body) {
			markReturned(c.pass, x.Cond, c.returned, c.ok)
		}
	case *ast.CallExpr:
		if isStoreAdd(c.pass, x) {
			c.adds = append(c.adds, x)
		}
	}
}

// assigned records each store add of x whose result goes into a named variable.
func (c *storeAdds) assigned(x *ast.AssignStmt) {
	if len(x.Lhs) != len(x.Rhs) {
		return
	}
	for i, rhs := range x.Rhs {
		call, isCall := ast.Unparen(rhs).(*ast.CallExpr)
		if !isCall || !isStoreAdd(c.pass, call) {
			continue
		}
		if id, isID := x.Lhs[i].(*ast.Ident); isID && id.Name != "_" {
			c.held = append(c.held, heldAdd{call: call, obj: identObj(c.pass, id)})
		}
	}
}

// refuses is whether body (not a nested function literal) returns an error of its own: a return
// whose last result is not nil, so an if testing the add's error with this body translates the
// refusal rather than losing it.
func refuses(body *ast.BlockStmt) bool {
	found := false
	inspectOwn(body, func(n ast.Node) {
		r, isReturn := n.(*ast.ReturnStmt)
		if !isReturn || len(r.Results) == 0 {
			return
		}
		if id, isID := ast.Unparen(r.Results[len(r.Results)-1]).(*ast.Ident); !isID || id.Name != "nil" {
			found = true
		}
	})
	return found
}

// heldAdd is a store add whose result is assigned to a variable.
type heldAdd struct {
	call *ast.CallExpr
	obj  types.Object
}

// inspectOwn visits every node of body that is not inside a nested function literal.
func inspectOwn(body *ast.BlockStmt, visit func(ast.Node)) {
	ast.Inspect(body, func(n ast.Node) bool {
		if _, lit := n.(*ast.FuncLit); lit {
			return false
		}
		if n != nil {
			visit(n)
		}
		return true
	})
}

// markReturned records the variables a return's result names and the store adds it holds.
func markReturned(pass *analysis.Pass, result ast.Expr, returned map[types.Object]bool, ok map[*ast.CallExpr]bool) {
	ast.Inspect(result, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.Ident:
			if obj := pass.TypesInfo.Uses[x]; obj != nil {
				returned[obj] = true
			}
		case *ast.CallExpr:
			if isStoreAdd(pass, x) {
				ok[x] = true
			}
		}
		return true
	})
}

func identObj(pass *analysis.Pass, id *ast.Ident) types.Object {
	if obj := pass.TypesInfo.Defs[id]; obj != nil {
		return obj
	}
	return pass.TypesInfo.Uses[id]
}

// isStoreAdd is whether call is a state.Store method named Add... whose last result is an error.
func isStoreAdd(pass *analysis.Pass, call *ast.CallExpr) bool {
	fn := calledFunc(pass, call)
	if fn == nil || !strings.HasPrefix(fn.Name(), "Add") {
		return false
	}
	sig, isSig := fn.Type().(*types.Signature)
	if !isSig || sig.Recv() == nil || sig.Results().Len() == 0 {
		return false
	}
	last := sig.Results().At(sig.Results().Len() - 1).Type()
	if !types.Identical(last, types.Universe.Lookup("error").Type()) {
		return false
	}
	recv := sig.Recv().Type()
	if p, isPtr := recv.(*types.Pointer); isPtr {
		recv = p.Elem()
	}
	named, isNamed := recv.(*types.Named)
	if !isNamed {
		return false
	}
	obj := named.Obj()
	return obj.Name() == "Store" && obj.Pkg() != nil && ownedBy(obj.Pkg().Path(), teamchatStatePkg)
}
