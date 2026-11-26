package rules

import (
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// teamsTree is ccy-teams' package tree, the one whose runs are given a teamchat root.
const teamsTree = "internal/teams"

// teamchatRootName is the name a root the machine's teamchat records are read under is held
// in: config.Config's and host.Host's TeamchatRoot field, or a parameter named for it.
const teamchatRootName = "teamchatroot"

// machineRecordNames are teamchat's paths names that are a fixed place on the machine: the
// status root and what is under it.
var machineRecordNames = []string{
	"StatusRoot", "StatusInstalledFile", "LXCRecordDir", "StatusTeamDir", "StatusFile", "StampFile",
	"ApprovedFile", "LXCOnlineFile", "SetRecord",
}

// messagePkgs are the packages whose calls only word a message, which reads nothing.
var messagePkgs = []string{"fmt", "errors"}

const teamchatRootFix = "a teamchat machine record read at its fixed path in ccy-teams (project_teamchat_root_read): " +
	"a test run reads the machine's own teams; read it under the run's root (TeamchatRoot + paths.StatusFile(id)), " +
	"as board does"

// TeamchatRootRead keeps ccy-teams' reads of the machine's teamchat records under the run's root.
var TeamchatRootRead = &analysis.Analyzer{
	Name: "project_teamchat_root_read",
	Doc: `project_teamchat_root_read flags, in the non-test files of internal/teams and the
packages under it, a call argument that names one of teamchat's fixed machine records
(internal/teamchat/paths' StatusRoot, StatusInstalledFile, LXCRecordDir, StatusTeamDir,
StatusFile, StampFile, ApprovedFile, LXCOnlineFile or SetRecord) without a root in front: the
argument is not a + chain whose leftmost operand is a name or field called TeamchatRoot (any
case, so a parameter teamchatRoot counts). Calls of fmt and errors are not judged: they word a
message and read nothing.

ccy-teams' configuration carries TeamchatRoot, the root the v2 records are read under: "" on a
host, a scratch directory in a test world (teamtest), so a test never reads the machine's own
v2 teams. A record read at its fixed path ignores it: the session-name wiring's role lookup read
/var/lib/teamchat-status through system.StatusRecord, so the teams unit tests read whatever
teams the machine running them has installed (Plan 00265, the batch 26 review).

Not caught: a fixed path built from a literal rather than from paths' names; a record path held
in a variable first and passed later (the variable is not followed); a read outside
internal/teams, whose commands are given no root; lxcstate's readers, which take the root as
an argument already.

How to fix: read the record under the root the run was given (h.TeamchatRoot +
paths.StatusFile(id), or system.StatusUnder(h.TeamchatRoot) for a Status func).`,
	Run:        perPass(runTeamchatRootRead),
	ResultType: noResultType,
}

func runTeamchatRootRead(pass *analysis.Pass) {
	if !inTree(pass.Pkg.Path(), teamsTree) {
		return
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || wordsAMessage(pass, call) {
				return true
			}
			for _, arg := range call.Args {
				if namesMachineRecord(pass, arg) && !rootedArg(arg) {
					pass.Reportf(arg.Pos(), "%s", teamchatRootFix)
				}
			}
			return true
		})
	}
}

// wordsAMessage is whether call is a function of fmt or errors.
func wordsAMessage(pass *analysis.Pass, call *ast.CallExpr) bool {
	fn := calledFunc(pass, call)
	if fn == nil || fn.Pkg() == nil {
		return false
	}
	for _, p := range messagePkgs {
		if fn.Pkg().Path() == p {
			return true
		}
	}
	return false
}

// namesMachineRecord is whether e names one of teamchat's fixed machine records, outside a
// nested function literal or call (a call inside is judged on its own).
func namesMachineRecord(pass *analysis.Pass, e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if found {
			return false
		}
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			if !isMachineRecordRef(pass, x.Fun) {
				return false
			}
			found = true
		case *ast.SelectorExpr:
			found = isMachineRecordRef(pass, x)
		}
		return !found
	})
	return found
}

// isMachineRecordRef is whether e is a selector naming one of paths' machine record names.
func isMachineRecordRef(pass *analysis.Pass, e ast.Expr) bool {
	sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	obj := pass.TypesInfo.Uses[sel.Sel]
	if obj == nil || obj.Pkg() == nil || !ownedBy(obj.Pkg().Path(), teamchatPathsPkg) {
		return false
	}
	for _, name := range machineRecordNames {
		if obj.Name() == name {
			return true
		}
	}
	return false
}

// rootedArg is whether arg is a + chain whose leftmost operand is named for the teamchat root.
func rootedArg(arg ast.Expr) bool {
	e := ast.Unparen(arg)
	bin, ok := e.(*ast.BinaryExpr)
	if !ok || bin.Op != token.ADD {
		return false
	}
	for {
		left, isBin := ast.Unparen(bin.X).(*ast.BinaryExpr)
		if !isBin || left.Op != token.ADD {
			break
		}
		bin = left
	}
	switch x := ast.Unparen(bin.X).(type) {
	case *ast.Ident:
		return strings.EqualFold(x.Name, teamchatRootName)
	case *ast.SelectorExpr:
		return strings.EqualFold(x.Sel.Name, teamchatRootName)
	}
	return false
}
