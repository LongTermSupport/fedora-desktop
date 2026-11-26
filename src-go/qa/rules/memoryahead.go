package rules

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// MemoryAheadOfStore forbids engine memory written inside a store write's change function: that
// function runs before the store is written, so a refused write leaves memory ahead of the disk.
var MemoryAheadOfStore = &analysis.Analyzer{
	Name: "project_memory_ahead_of_store",
	Doc: `project_memory_ahead_of_store flags, in the non-test files of teamchat's engine package
(internal/teamchat/member/engine), a write of engine memory made inside the change function of a
store write: the function literal passed to (*Engine).change or (*Engine).changeLocked, and every
function of the package it calls, followed through. Engine memory is whatever is reached through
a *Engine or a *held: an assignment, ++ or -- whose target is rooted at one, and a delete or
clear of a map rooted at one.

The change function edits a copy of the store, and the copy is written and kept only when the
write succeeds, so the store never runs ahead of the disk. Memory the function changes as it
goes is changed before the write: a refused write keeps the store as it was and the memory as
it is not. The settle of a recv forgot each delivered item's content inside its write, so a
refused write left the item pending with no content to render (Plan 00267, issue #443's sweep).

Not flagged: the store copy and the closure's own variables, which are read only once the write
returns; the link log (Engine.logf) and the failpoints (Engine.hit), which record nothing the
store holds; a write through a local alias of a type that is neither (a *pathMark read out of
the proof's map); memory moved in an earlier step, before the write is called (the proof's
marks, #443 itself: see "Defect classes that cannot be a rule").

How to fix: compute in the change function, into closure variables, and change memory after the
write returned nil.`,
	Run:        perPass(runMemoryAheadOfStore),
	ResultType: noResultType,
}

const (
	teamchatEnginePkg = "internal/teamchat/member/engine"
	memoryAheadMsg    = "engine memory written inside a store write's change function (project_memory_ahead_of_store): a refused write leaves memory ahead of the disk; change it after the write returns nil"
)

// memoryAheadExempt are the engine methods whose writes record nothing the store holds.
var memoryAheadExempt = map[string]string{
	"logf": "the link log's own lost-line count",
	"hit":  "a failpoint, armed only by the tests",
}

func runMemoryAheadOfStore(pass *analysis.Pass) {
	if !ownedBy(pass.Pkg.Path(), teamchatEnginePkg) {
		return
	}
	m := &memoryWrites{pass: pass, decls: declsByFunc(pass)}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, isCall := n.(*ast.CallExpr)
			if !isCall || !isStoreChange(pass, call) || len(call.Args) == 0 {
				return true
			}
			if lit, isLit := ast.Unparen(call.Args[len(call.Args)-1]).(*ast.FuncLit); isLit {
				m.judge(lit.Body)
			}
			return true
		})
	}
}

// declsByFunc is each function the package declares with a body, by its object.
func declsByFunc(pass *analysis.Pass) map[*types.Func]*ast.FuncDecl {
	decls := map[*types.Func]*ast.FuncDecl{}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			if fn, isFn := pass.TypesInfo.Defs[fd.Name].(*types.Func); isFn {
				decls[fn] = fd
			}
		}
	}
	return decls
}

// memoryWrites finds the writes of engine memory a change function makes.
type memoryWrites struct {
	pass  *analysis.Pass
	decls map[*types.Func]*ast.FuncDecl
}

// judge reports each memory write of body (its function literals included), and each call of a
// package function that makes one, at the call.
func (m *memoryWrites) judge(body *ast.BlockStmt) {
	ast.Inspect(body, func(n ast.Node) bool {
		if pos, ok := m.writes(n); ok {
			m.pass.Reportf(pos, "%s", memoryAheadMsg)
			return true
		}
		if call, isCall := n.(*ast.CallExpr); isCall && m.callWrites(call, map[*types.Func]bool{}) {
			m.pass.Reportf(call.Pos(), "%s", memoryAheadMsg)
		}
		return true
	})
}

// writes is whether n writes engine memory, and where.
func (m *memoryWrites) writes(n ast.Node) (token.Pos, bool) {
	switch x := n.(type) {
	case *ast.AssignStmt:
		if x.Tok == token.DEFINE {
			return token.NoPos, false
		}
		for _, l := range x.Lhs {
			if m.rootedInMemory(l) {
				return l.Pos(), true
			}
		}
	case *ast.IncDecStmt:
		if m.rootedInMemory(x.X) {
			return x.Pos(), true
		}
	case *ast.CallExpr:
		if id, isID := ast.Unparen(x.Fun).(*ast.Ident); isID && len(x.Args) > 0 {
			if b, isBuiltin := m.pass.TypesInfo.Uses[id].(*types.Builtin); isBuiltin &&
				(b.Name() == "delete" || b.Name() == "clear") && m.rootedInMemory(x.Args[0]) {
				return x.Pos(), true
			}
		}
	}
	return token.NoPos, false
}

// callWrites is whether call reaches a package function that writes engine memory.
func (m *memoryWrites) callWrites(call *ast.CallExpr, seen map[*types.Func]bool) bool {
	fn := calledFunc(m.pass, call)
	if fn == nil || seen[fn] {
		return false
	}
	if _, exempt := memoryAheadExempt[fn.Name()]; exempt && isEngineMethod(fn) {
		return false
	}
	fd := m.decls[fn]
	if fd == nil {
		return false
	}
	seen[fn] = true
	found := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := m.writes(n); ok {
			found = true
			return false
		}
		if c, isCall := n.(*ast.CallExpr); isCall && m.callWrites(c, seen) {
			found = true
			return false
		}
		return true
	})
	return found
}

// rootedInMemory is whether expression e names a place reached through a *Engine or a *held.
func (m *memoryWrites) rootedInMemory(e ast.Expr) bool {
	for {
		switch x := ast.Unparen(e).(type) {
		case *ast.SelectorExpr:
			if isEngineMemory(m.pass.TypesInfo.TypeOf(x.X)) {
				return true
			}
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.StarExpr:
			e = x.X
		case *ast.Ident:
			return false
		default:
			return false
		}
	}
}

// isEngineMemory is whether t is *Engine or *held of the engine package.
func isEngineMemory(t types.Type) bool {
	p, isPtr := t.(*types.Pointer)
	if !isPtr {
		return false
	}
	named, isNamed := p.Elem().(*types.Named)
	if !isNamed || named.Obj().Pkg() == nil || !ownedBy(named.Obj().Pkg().Path(), teamchatEnginePkg) {
		return false
	}
	return named.Obj().Name() == "Engine" || named.Obj().Name() == "held"
}

func isEngineMethod(fn *types.Func) bool {
	sig, isSig := fn.Type().(*types.Signature)
	return isSig && sig.Recv() != nil && isEngineMemory(sig.Recv().Type())
}

// isStoreChange is whether call is (*Engine).change or (*Engine).changeLocked.
func isStoreChange(pass *analysis.Pass, call *ast.CallExpr) bool {
	fn := calledFunc(pass, call)
	return fn != nil && (fn.Name() == "change" || fn.Name() == "changeLocked") && isEngineMethod(fn)
}
