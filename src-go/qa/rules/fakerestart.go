package rules

import (
	"go/ast"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// FakeRestart forbids a test stopping or restarting a fake server from a goroutine other than
// the one that then uses it.
var FakeRestart = &analysis.Analyzer{
	Name: "project_fake_restart",
	Doc: `project_fake_restart flags, in a _test.go file, a call of Stop or Restart on a type named
Server of a package whose name starts with fake (fakehs) made inside a function literal, or as
the call of a go statement. A literal handed straight to a Cleanup or a Run (a subtest's body),
or deferred, is not judged: it runs on the goroutine that then owns the server, or after its
last use. A function literal nested in one of those is judged.

A hook, a callback or a goroutine runs while the test's own goroutine can be using the server:
a restart made there overlaps the test's request, whose tries meet the stopped server and get
no answer, and nothing the test does waits for the restart to finish. teamsync's
TestARestartedServerBetweenPolls restarted fakehs from the engine's persist hook, on the team's
goroutine, while the test posted (fedora-desktop#149).

Not caught: a stop reached through a helper called in a literal, a server held in an interface,
a fake whose package or type is named otherwise, and a literal stored and later called on the
test's own goroutine (a false positive the fix below removes as well).

How to fix: the literal tells the test it has reached the point (it closes a channel and waits
for another), and the test stops or restarts the server on its own goroutine, at a point after
its own requests are answered.`,
	Run:        perPass(runFakeRestart),
	ResultType: noResultType,
}

// fakeServerStops are the methods of a fake server that take it down.
var fakeServerStops = []string{"Stop", "Restart"}

// testingPkg is the standard library's testing package.
const testingPkg = "testing"

const fakeRestartMsg = "%s in a %s (project_fake_restart): it runs on another goroutine than the test's, " +
	"which can be using the server; signal the test and restart it on the test's goroutine once its requests are answered"

func runFakeRestart(pass *analysis.Pass) {
	for _, f := range pass.Files {
		if !isTestFile(pass, f) {
			continue
		}
		exempt := ownerLiterals(pass, f)
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.GoStmt:
				if what := fakeServerStop(pass, n.Call); what != "" {
					pass.Reportf(n.Call.Pos(), fakeRestartMsg, what, "go statement")
				}
			case *ast.FuncLit:
				if !exempt[n] {
					reportStopsIn(pass, n)
					return false
				}
			}
			return true
		})
	}
}

// ownerLiterals are the function literals that run on the goroutine owning the server, or after
// its last use: one handed straight to a method of package testing (Cleanup, a subtest's Run),
// or deferred.
func ownerLiterals(pass *analysis.Pass, f *ast.File) map[*ast.FuncLit]bool {
	exempt := map[*ast.FuncLit]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.DeferStmt:
			if lit, ok := ast.Unparen(n.Call.Fun).(*ast.FuncLit); ok {
				exempt[lit] = true
			}
		case *ast.CallExpr:
			fn := calledFunc(pass, n)
			if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != testingPkg {
				return true
			}
			for _, arg := range n.Args {
				if lit, ok := ast.Unparen(arg).(*ast.FuncLit); ok {
					exempt[lit] = true
				}
			}
		}
		return true
	})
	return exempt
}

// reportStopsIn reports each fake server's stop in lit, nested literals included.
func reportStopsIn(pass *analysis.Pass, lit *ast.FuncLit) {
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if what := fakeServerStop(pass, call); what != "" {
				pass.Reportf(call.Pos(), fakeRestartMsg, what, "function literal")
			}
		}
		return true
	})
}

// fakeServerStop names call when it is Stop or Restart of a fake package's Server, else "".
func fakeServerStop(pass *analysis.Pass, call *ast.CallExpr) string {
	fn := calledFunc(pass, call)
	if fn == nil || !slices.Contains(fakeServerStops, fn.Name()) {
		return ""
	}
	sig, isSig := fn.Type().(*types.Signature)
	if !isSig || sig.Recv() == nil {
		return ""
	}
	recv := sig.Recv().Type()
	if p, isPtr := recv.(*types.Pointer); isPtr {
		recv = p.Elem()
	}
	named, isNamed := recv.(*types.Named)
	if !isNamed || named.Obj().Name() != "Server" || named.Obj().Pkg() == nil {
		return ""
	}
	pkg := named.Obj().Pkg().Name()
	if !strings.HasPrefix(pkg, "fake") {
		return ""
	}
	return pkg + ".Server." + fn.Name()
}
