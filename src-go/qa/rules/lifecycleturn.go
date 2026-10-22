package rules

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// The package the rule judges, and the package whose client and calls read and post a
// member's lifecycle notices.
const (
	lifecycleEndsPkg   = "internal/teamchat/member/ends"
	lifecycleNoticePkg = "internal/teamchat/notice"
)

// LifecycleTurn holds every lifecycle notice read and post of teamchat's ends inside the
// team's turn.
var LifecycleTurn = &analysis.Analyzer{
	Name: "project_lifecycle_turn",
	Doc: `project_lifecycle_turn flags a read or post of a member's lifecycle notice in teamchat's
ends package (internal/teamchat/member/ends) that is not inside the team's turn.

A lifecycle notice (left, disconnected, back) is decided from what main says and posted later.
The ends of one run post from several goroutines: back from the team's goroutine at the run's
start, the end from the link's finish, a leave from a removal. A decision read outside the
turn can be overtaken by another post between the read and the send: back read the run's own
disconnected, posted after it, and main's latest notice said the member was back when it had
gone (Plan 00209 n3c: TestE2ECmdKilled and TestE2EKeeperKilled under load). The turn
((*ends).turn(ctx, team, fn)) runs one lifecycle step per team at a time, and an end refuses
a back still to come.

Flagged, outside a function argument of a call to the package's turn method: a use (a call
or a method value) of notice.LatestOwn, notice.Disconnected, or the Messages or Send method
of notice.Client (and so of team.Notices, which embeds it); and of a function of the package
that reaches one outside a turn. A helper is judged where it is used, so one called only
inside turns is clean. A function literal started by a go statement, or handed to a method
named Go (sync.WaitGroup.Go, errgroup), outlives the turn and is outside it, even when
written inside one. An exported function, and one nothing in the package uses, is where a
use is reported. Test files are not judged.

Not caught: that turn itself serialises per team and refuses a back after an end (the ends
package's tests hold that); a read or post reached through another package's function other
than the named ones; and the host's posts, which run in other processes and are ordered by
their transaction ids, not by a turn.

How to fix: move the read and the post it decides into one function literal passed to turn,
and start no goroutine from inside it.`,
	Run:        perPass(runLifecycleTurn),
	ResultType: noResultType,
}

// lifecycleRef is one use of a primitive or of a function of the package.
type lifecycleRef struct {
	pos     token.Pos
	target  *types.Func
	guarded bool
}

// lifecycleFunc is one function of the package and its uses.
type lifecycleFunc struct {
	fn       *types.Func
	exported bool
	refs     []lifecycleRef
}

func runLifecycleTurn(pass *analysis.Pass) {
	if !ownedBy(pass.Pkg.Path(), lifecycleEndsPkg) {
		return
	}
	funcs := lifecycleFuncs(pass)
	refs := map[*types.Func][]lifecycleRef{}
	used := map[*types.Func]bool{}
	for _, f := range funcs {
		refs[f.fn] = f.refs
		for _, r := range f.refs {
			used[r.target] = true
		}
	}
	hazardous := lifecycleHazards(refs)
	for _, f := range funcs {
		if !f.exported && used[f.fn] {
			continue
		}
		for _, r := range f.refs {
			if !r.guarded && (isLifecyclePrimitive(r.target) || hazardous[r.target]) {
				pass.Reportf(r.pos, "lifecycle notice read or posted outside the team's turn: %s", lifecycleName(pass, r.target))
			}
		}
	}
}

// lifecycleFuncs is every function the package's non-test files declare, in source order.
func lifecycleFuncs(pass *analysis.Pass) []lifecycleFunc {
	var out []lifecycleFunc
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			fn, isFunc := pass.TypesInfo.Defs[fd.Name].(*types.Func)
			if !isFunc {
				continue
			}
			w := &lifecycleWalker{pass: pass}
			w.visit(fd.Body, false)
			out = append(out, lifecycleFunc{fn: fn, exported: fd.Name.IsExported(), refs: w.refs})
		}
	}
	return out
}

// lifecycleHazards is the package's functions that reach a primitive outside a turn, directly
// or through another such function.
func lifecycleHazards(refs map[*types.Func][]lifecycleRef) map[*types.Func]bool {
	hazardous := map[*types.Func]bool{}
	for changed := true; changed; {
		changed = false
		for fn, rs := range refs {
			if hazardous[fn] {
				continue
			}
			for _, r := range rs {
				if !r.guarded && (isLifecyclePrimitive(r.target) || hazardous[r.target]) {
					hazardous[fn], changed = true, true
					break
				}
			}
		}
	}
	return hazardous
}

// lifecycleWalker collects a body's uses, each marked by whether a turn holds it.
type lifecycleWalker struct {
	pass *analysis.Pass
	refs []lifecycleRef
}

func (w *lifecycleWalker) visit(n ast.Node, guarded bool) {
	ast.Inspect(n, func(m ast.Node) bool {
		switch m := m.(type) {
		case *ast.GoStmt:
			w.visit(m.Call, false)
			return false
		case *ast.CallExpr:
			callee := calledFunc(w.pass, m)
			argsGuarded, special := guarded, false
			switch {
			case w.isTurn(callee):
				argsGuarded, special = true, true
			case isGoMethod(callee):
				argsGuarded, special = false, true
			}
			if !special {
				return true
			}
			w.visit(m.Fun, guarded)
			for _, arg := range m.Args {
				w.visit(arg, argsGuarded)
			}
			return false
		case *ast.Ident:
			if fn, ok := w.pass.TypesInfo.Uses[m].(*types.Func); ok && (isLifecyclePrimitive(fn) || fn.Pkg() == w.pass.Pkg) {
				w.refs = append(w.refs, lifecycleRef{pos: m.Pos(), target: fn, guarded: guarded})
			}
		}
		return true
	})
}

// isTurn is whether fn is the package's turn method.
func (w *lifecycleWalker) isTurn(fn *types.Func) bool {
	if fn == nil || fn.Pkg() != w.pass.Pkg || fn.Name() != "turn" {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	return ok && sig.Recv() != nil
}

// isGoMethod is whether fn is a method named Go, which starts its argument as a goroutine.
func isGoMethod(fn *types.Func) bool {
	if fn == nil || fn.Name() != "Go" {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	return ok && sig.Recv() != nil
}

// isLifecyclePrimitive is whether fn reads or posts main's lifecycle notices: notice.LatestOwn,
// notice.Disconnected, or notice.Client's Messages or Send.
func isLifecyclePrimitive(fn *types.Func) bool {
	if fn == nil || fn.Pkg() == nil || !ownedBy(fn.Pkg().Path(), lifecycleNoticePkg) {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return false
	}
	if sig.Recv() == nil {
		return fn.Name() == "LatestOwn" || fn.Name() == "Disconnected"
	}
	return fn.Name() == "Messages" || fn.Name() == "Send"
}

// lifecycleName is how a finding names its target: notice.F for the notice package's
// functions, the bare name for a method or a function of the package.
func lifecycleName(pass *analysis.Pass, fn *types.Func) string {
	if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() == nil && fn.Pkg() != pass.Pkg {
		return fn.Pkg().Name() + "." + fn.Name()
	}
	return fn.Name()
}
