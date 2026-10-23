package rules

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// The package and type whose method names keeper.log.
const (
	teamchatPathsPkg = "internal/teamchat/paths"
	teamchatRunType  = "Run"
)

// keeperLogReaders are the keeper harness's functions that may name keeper.log: the one
// reader every barrier and assertion goes through, the copy each loop turn's end leaves, and
// the finding of the session's run by whether its keeper.log exists.
var keeperLogReaders = []allowance{
	{pkg: keeperPkg, fn: "world.keeperLog", reason: "the one reader: the file before the loop and after the keeper, the turn's copy between"},
	{pkg: keeperPkg, fn: "world.turned", reason: "the copy of keeper.log each of the loop's turns leaves as it ends"},
	{pkg: keeperPkg, fn: "world.runs", reason: "existence only: which runs of the run root are a keeper's"},
	{pkg: keeperPkg, fn: "logTap.StartTicks", reason: "the launch goldens' tap: opened while the run is made, read only once the keeper has ended"},
}

// KeeperLogBarrier holds every read of keeper.log in the keeper's tests to the harness's
// turn-aware reader.
var KeeperLogBarrier = newKeeperLogBarrier(keeperLogReaders)

func newKeeperLogBarrier(allow []allowance) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "project_keeper_log_barrier",
		Doc: `project_keeper_log_barrier flags a use of paths.Run's KeeperLog in the _test.go files of
teamchat's keeper (internal/teamchat/member/keeper) outside the harness functions named in the
keeperLogReaders table of the rules package.

A loop turn of the keeper may log a line and then do more: the owed notice's timer logs "not
posted in time" and only then lets the seat go. A test that waits for the line by reading the
file sees it while the turn is still under way, and an assertion of the turn's later effect
races it (Plan 00209 n3c: TestOwedNotice found the seat still held under load). The keeper
calls its tests' turned hook as each turn ends, and the harness's reader gives the log as
that left it while the loop runs, so a line shows only once its turn has run.

Any reference is flagged, not only a call: a method value, a method expression, a use in a
goroutine. The keeper's own code and other packages' tests are not judged; other files of
the run (link.log, keeper.json) are not keeper.log.

How to fix: wait with w.logged, and read the log with w.log or w.logOf, which go through the
reader; never open keeper.log in a test.`,
		Run: perPass(func(pass *analysis.Pass) {
			runKeeperLogBarrier(pass, allow)
		}),
		ResultType: noResultType,
	}
}

func runKeeperLogBarrier(pass *analysis.Pass, allow []allowance) {
	if !ownedBy(pass.Pkg.Path(), keeperPkg) {
		return
	}
	for _, f := range pass.Files {
		if !isTestFile(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			if fn := funcName(decl); fn != "" && allowedIn(pass, fn, allow) {
				continue
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				if id, isIdent := n.(*ast.Ident); isIdent && isKeeperLog(pass.TypesInfo.Uses[id]) {
					pass.Reportf(id.Pos(), "keeper.log read outside the harness's barrier (project_keeper_log_barrier): use w.logged, w.log or w.logOf")
				}
				return true
			})
		}
	}
}

// isKeeperLog is whether obj is paths.Run's KeeperLog method.
func isKeeperLog(obj types.Object) bool {
	fn, ok := obj.(*types.Func)
	if !ok || fn.Name() != "KeeperLog" || fn.Pkg() == nil || !ownedBy(fn.Pkg().Path(), teamchatPathsPkg) {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return false
	}
	named, isNamed := types.Unalias(sig.Recv().Type()).(*types.Named)
	return isNamed && named.Obj().Name() == teamchatRunType
}
