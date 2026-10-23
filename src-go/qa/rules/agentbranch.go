package rules

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
)

// The packages of teamchat's launch values, and the agent seam that owns every branch on one.
const (
	teamchatAgentPkg = "internal/teamchat/member/agent"
	teamchatStatePkg = "internal/teamchat/member/state"
	teamchatWirePkg  = "internal/teamchat/member/wire"
	teamchatCLIPkg   = "internal/teamchat/member/cli"
	agentTypeName    = "Agent"
	launchFieldName  = "Launch"
)

// launchNames are the named launch values and their one list, by package.
var launchNames = map[string][]string{
	teamchatStatePkg: {"LaunchClaude", "LaunchPlain", "Launches"},
	teamchatCLIPkg:   {"LaunchClaude", "LaunchPlain"},
	teamchatAgentPkg: {"Claude", "Plain"},
}

// launchStructs are the types whose Launch field carries a launch value as a string: the
// launch frame and session.pid.
var launchStructs = map[string]string{teamchatWirePkg: "Launch", teamchatStatePkg: "SessionPID"}

// launchValues are the launch values as they are spelled.
var launchValues = []string{"claude", "plain"}

const agentBranchFix = " outside package agent (project_agent_branch): ask the run's agent.Adapter, or add the agent's adapter there"

// AgentBranch holds every branch on a run's launch value to package agent.
var AgentBranch = newAgentBranch(agentBranches)

func newAgentBranch(allow []allowance) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "project_agent_branch",
		Doc: `project_agent_branch flags, in the non-test files of internal/teamchat and below, a branch on
the run's launch value outside the agent seam (internal/teamchat/member/agent), which owns it,
and outside the agentBranches table of the rules package:

- a use of state.LaunchClaude, state.LaunchPlain, state.Launches, cli.LaunchClaude,
  cli.LaunchPlain, agent.Claude or agent.Plain (a types.Info use, so an unqualified use in the
  declaring package counts; a declaration is not a use);
- an == or !=, a switch tag or a map key type whose type is agent.Agent;
- an == or !=, or a switch case against a tag, of a constant whose value is "claude" or "plain"
  with the field wire.Launch.Launch or state.SessionPID.Launch, or with a conversion to a
  string of an agent.Agent;
- a call given that field and a composite literal holding a launch value.

A run's agent (Claude Code, or a plain command) is asked what it can do through one value, its
agent.Adapter, so that another agent arrives as an adapter and not as a refactor of every
comparison (SPEC-n3 6.1, Plan 00215). Before the seam the keeper and the link each compared the
launch value wherever their behaviour differed by agent.

Not caught: a value that reaches the field through a function's result (isPlain(l.Launch)
with a bare literal inside isPlain) is caught only when that function names a constant; a
copy of the field held in a variable and compared; a list of launch values held in a variable
other than state.Launches.

How to fix: ask the run's agent.Adapter (Capabilities, Preparer); a new agent is its constant,
its case in agent.For and its adapter in agent, and one entry in state.Launches.`,
		Run: perPass(func(pass *analysis.Pass) {
			runAgentBranch(pass, allow)
		}),
		ResultType: noResultType,
	}
}

func runAgentBranch(pass *analysis.Pass, allow []allowance) {
	if !inTree(pass.Pkg.Path(), teamchatTree) || ownedBy(pass.Pkg.Path(), teamchatAgentPkg) {
		return
	}
	lines := map[token.Position]bool{}
	report := func(pos token.Pos, what string) {
		p := pass.Fset.Position(pos)
		line := token.Position{Filename: p.Filename, Line: p.Line}
		if lines[line] {
			return
		}
		lines[line] = true
		pass.Reportf(pos, "%s%s", what, agentBranchFix)
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			if allowedIn(pass, funcName(decl), allow) {
				continue
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				checkAgentBranch(pass, n, report)
				return true
			})
		}
	}
}

func checkAgentBranch(pass *analysis.Pass, n ast.Node, report func(token.Pos, string)) {
	switch n := n.(type) {
	case *ast.Ident:
		if name, ok := launchName(pass.TypesInfo.Uses[n]); ok {
			report(n.Pos(), name+" named")
		}
	case *ast.BinaryExpr:
		if n.Op != token.EQL && n.Op != token.NEQ {
			return
		}
		if isAgentExpr(pass, n.X) || isAgentExpr(pass, n.Y) {
			report(n.Pos(), "an agent.Agent compared")
			return
		}
		if what, ok := launchCompare(pass, n.X, n.Y); ok {
			report(n.Pos(), what)
		}
	case *ast.SwitchStmt:
		checkLaunchSwitch(pass, n, report)
	case *ast.MapType:
		if isAgentType(pass.TypesInfo.TypeOf(n.Key)) {
			report(n.Pos(), "a map keyed by agent.Agent")
		}
	case *ast.CallExpr:
		if launchListCall(pass, n) {
			report(n.Pos(), "the launch field checked against a list of launch values")
		}
	}
}

func checkLaunchSwitch(pass *analysis.Pass, sw *ast.SwitchStmt, report func(token.Pos, string)) {
	if sw.Tag == nil {
		return
	}
	if isAgentExpr(pass, sw.Tag) {
		report(sw.Pos(), "a switch on an agent.Agent")
		return
	}
	for _, stmt := range sw.Body.List {
		clause, isClause := stmt.(*ast.CaseClause)
		if !isClause {
			continue
		}
		for _, e := range clause.List {
			if what, ok := launchCompare(pass, sw.Tag, e); ok {
				report(e.Pos(), what)
			}
		}
	}
}

// launchName is obj's name, qualified, when it is a named launch value or the list of them.
func launchName(obj types.Object) (string, bool) {
	if obj == nil || obj.Pkg() == nil || obj.Parent() != obj.Pkg().Scope() {
		return "", false
	}
	for pkg, names := range launchNames {
		if ownedBy(obj.Pkg().Path(), pkg) && slices.Contains(names, obj.Name()) {
			return obj.Pkg().Name() + "." + obj.Name(), true
		}
	}
	return "", false
}

func isAgentExpr(pass *analysis.Pass, e ast.Expr) bool {
	return isAgentType(pass.TypesInfo.TypeOf(e))
}

// isAgentType is whether t is agent.Agent, through any alias of it (cli.Mode).
func isAgentType(t types.Type) bool {
	if t == nil {
		return false
	}
	named, isNamed := types.Unalias(t).(*types.Named)
	if !isNamed || named.Obj().Name() != agentTypeName || named.Obj().Pkg() == nil {
		return false
	}
	return ownedBy(named.Obj().Pkg().Path(), teamchatAgentPkg)
}

// launchCompare is what a comparison of a and b is when one is a launch value and the other the
// launch field or a string of an agent.Agent.
func launchCompare(pass *analysis.Pass, a, b ast.Expr) (string, bool) {
	for _, pair := range [][2]ast.Expr{{a, b}, {b, a}} {
		value, isValue := launchConst(pass, pair[0])
		switch {
		case !isValue:
		case isLaunchField(pass, pair[1]):
			return "the launch field compared with " + value, true
		case isAgentString(pass, pair[1]):
			return "a string of an agent.Agent compared with " + value, true
		}
	}
	return "", false
}

// launchConst is e's value, quoted, when e is a constant whose value is a launch value.
func launchConst(pass *analysis.Pass, e ast.Expr) (string, bool) {
	v := pass.TypesInfo.Types[e].Value
	if v == nil || v.Kind() != constant.String || !slices.Contains(launchValues, constant.StringVal(v)) {
		return "", false
	}
	return v.ExactString(), true
}

// isLaunchField is whether e selects the Launch field of the launch frame or of session.pid.
func isLaunchField(pass *analysis.Pass, e ast.Expr) bool {
	sel, isSel := ast.Unparen(e).(*ast.SelectorExpr)
	if !isSel {
		return false
	}
	field, isVar := pass.TypesInfo.Uses[sel.Sel].(*types.Var)
	if !isVar || !field.IsField() || field.Name() != launchFieldName || field.Pkg() == nil {
		return false
	}
	for pkg, typeName := range launchStructs {
		if ownedBy(field.Pkg().Path(), pkg) && isFieldOf(field, typeName) {
			return true
		}
	}
	return false
}

// isFieldOf is whether field is one of the fields of the struct type typeName of its package.
func isFieldOf(field *types.Var, typeName string) bool {
	tn, isTypeName := field.Pkg().Scope().Lookup(typeName).(*types.TypeName)
	if !isTypeName {
		return false
	}
	st, isStruct := tn.Type().Underlying().(*types.Struct)
	if !isStruct {
		return false
	}
	for f := range st.Fields() {
		if f == field {
			return true
		}
	}
	return false
}

// isAgentString is whether e converts an agent.Agent to a string type.
func isAgentString(pass *analysis.Pass, e ast.Expr) bool {
	call, isCall := ast.Unparen(e).(*ast.CallExpr)
	if !isCall || len(call.Args) != 1 {
		return false
	}
	tv := pass.TypesInfo.Types[call.Fun]
	if !tv.IsType() {
		return false
	}
	basic, isBasic := tv.Type.Underlying().(*types.Basic)
	return isBasic && basic.Info()&types.IsString != 0 && isAgentExpr(pass, call.Args[0])
}

// launchListCall is whether call is given the launch field and a composite literal holding a
// launch value: a contains over a list of the values.
func launchListCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	field := slices.ContainsFunc(call.Args, func(a ast.Expr) bool { return isLaunchField(pass, a) })
	return field && slices.ContainsFunc(call.Args, func(a ast.Expr) bool {
		lit, isLit := ast.Unparen(a).(*ast.CompositeLit)
		return isLit && holdsLaunchValue(pass, lit)
	})
}

func holdsLaunchValue(pass *analysis.Pass, lit *ast.CompositeLit) bool {
	found := false
	ast.Inspect(lit, func(n ast.Node) bool {
		if e, isExpr := n.(ast.Expr); isExpr && !found {
			_, found = launchConst(pass, e)
		}
		return !found
	})
	return found
}
