package rules

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slices"
	"strings"

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

// agentBranchTrees are what the rule judges: teamchat's packages and its two commands.
var agentBranchTrees = []string{teamchatTree, "cmd/teamchat", "cmd/teamchat-admin"}

// launchListSeparators split a string that spells a list of launch values ("claude,plain").
const launchListSeparators = ",|;"

const agentBranchFix = " outside package agent (project_agent_branch): ask the run's agent.Adapter, or add the agent's adapter there"

// AgentBranch holds every branch on a run's launch value to package agent.
var AgentBranch = newAgentBranch(agentBranches)

func newAgentBranch(allow []allowance) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "project_agent_branch",
		Doc: `project_agent_branch flags, in the non-test files of internal/teamchat, cmd/teamchat and
cmd/teamchat-admin and below, a branch on the run's launch value outside the agent seam
(internal/teamchat/member/agent), which owns it, and outside the agentBranches table of the
rules package:

- a use of state.LaunchClaude, state.LaunchPlain, state.Launches, cli.LaunchClaude,
  cli.LaunchPlain, agent.Claude or agent.Plain (a types.Info use, so an unqualified use in the
  declaring package counts; a declaration is not a use);
- an == or !=, a switch tag or a map key type whose type is agent.Agent;
- an == or !=, a call, an index expression, or a switch tag and its cases, whose operands hold
  both the launch value and a launch constant. The launch value is the field wire.Launch.Launch
  or state.SessionPID.Launch or a conversion to a string of an agent.Agent, as an operand or as
  an argument of a call that is one (strings.ToLower(l.Launch)), through any depth of calls. A
  launch constant is a constant expression whose value is "claude" or "plain", or a list of
  words split on , | or ; one of which is ("plain,x"); in a call's arguments or an index's map
  it may also be a composite literal holding one (slices.Contains([]string{"plain"}, l.Launch),
  map[string]bool{"plain": true}[l.Launch]).

A run's agent (Claude Code, or a plain command) is asked what it can do through one value, its
agent.Adapter, so that another agent arrives as an adapter and not as a refactor of every
comparison (SPEC-n3 6.1, Plan 00215). Before the seam the keeper and the link each compared the
launch value wherever their behaviour differed by agent.

Not caught: a value that reaches the field through a function's result (isPlain(l.Launch)
with a bare literal inside isPlain) is caught only when that function names a constant; a
copy of the field held in a variable and compared; a list or map of launch values held in a
variable other than state.Launches; a part of a launch value (strings.HasPrefix(l.Launch,
"pla")); a launch value inside a sentence, which is not a list ("is not claude or plain").

How to fix: ask the run's agent.Adapter (Capabilities, Preparer); a new agent is its constant,
its case in agent.For and its adapter in agent, and one entry in state.Launches.`,
		Run: perPass(func(pass *analysis.Pass) {
			runAgentBranch(pass, allow)
		}),
		ResultType: noResultType,
	}
}

func runAgentBranch(pass *analysis.Pass, allow []allowance) {
	inScope := slices.ContainsFunc(agentBranchTrees, func(tree string) bool { return inTree(pass.Pkg.Path(), tree) })
	if !inScope || ownedBy(pass.Pkg.Path(), teamchatAgentPkg) {
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
		checkLaunchBinary(pass, n, report)
	case *ast.SwitchStmt:
		checkLaunchSwitch(pass, n, report)
	case *ast.MapType:
		if isAgentType(pass.TypesInfo.TypeOf(n.Key)) {
			report(n.Pos(), "a map keyed by agent.Agent")
		}
	case *ast.CallExpr:
		if what, ok := launchCall(pass, n); ok {
			report(n.Pos(), what)
		}
	case *ast.IndexExpr:
		if what, ok := launchOperand(pass, n.Index); ok && isLaunchLiteral(pass, n.X) {
			report(n.Pos(), what+" used as an index into a literal holding a launch value")
		}
	}
}

func checkLaunchBinary(pass *analysis.Pass, be *ast.BinaryExpr, report func(token.Pos, string)) {
	if be.Op != token.EQL && be.Op != token.NEQ {
		return
	}
	if isAgentExpr(pass, be.X) || isAgentExpr(pass, be.Y) {
		report(be.Pos(), "an agent.Agent compared")
		return
	}
	if what, ok := launchCompare(pass, be.X, be.Y); ok {
		report(be.Pos(), what)
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

// launchCompare is what a comparison of a and b is when one is a launch constant and the other
// a launch operand.
func launchCompare(pass *analysis.Pass, a, b ast.Expr) (string, bool) {
	for _, pair := range [][2]ast.Expr{{a, b}, {b, a}} {
		value, isValue := launchConst(pass, pair[0])
		if !isValue {
			continue
		}
		if what, isOperand := launchOperand(pass, pair[1]); isOperand {
			return what + " compared with " + value, true
		}
	}
	return "", false
}

// launchCall is what call is when it is given a launch operand and, as another argument, a
// launch constant or a composite literal holding one.
func launchCall(pass *analysis.Pass, call *ast.CallExpr) (string, bool) {
	for i, arg := range call.Args {
		what, isOperand := launchOperand(pass, arg)
		if !isOperand {
			continue
		}
		for j, other := range call.Args {
			if j == i {
				continue
			}
			if value, isValue := launchConst(pass, other); isValue {
				return what + " given to a call with " + value, true
			}
			if isLaunchLiteral(pass, other) {
				return what + " checked against a list of launch values", true
			}
		}
	}
	return "", false
}

// launchOperand is what e is when it is the launch field or a string of an agent.Agent, or a
// call given one of them as an argument, through any depth of calls.
func launchOperand(pass *analysis.Pass, e ast.Expr) (string, bool) {
	switch {
	case isLaunchField(pass, e):
		return "the launch field", true
	case isAgentString(pass, e):
		return "a string of an agent.Agent", true
	}
	call, isCall := ast.Unparen(e).(*ast.CallExpr)
	if !isCall {
		return "", false
	}
	for _, arg := range call.Args {
		if what, isOperand := launchOperand(pass, arg); isOperand {
			return "a call over " + strings.TrimPrefix(what, "a call over "), true
		}
	}
	return "", false
}

// launchConst is e's value, quoted, when e is a constant whose value is a launch value or a
// list of words one of which is.
func launchConst(pass *analysis.Pass, e ast.Expr) (string, bool) {
	v := pass.TypesInfo.Types[e].Value
	if v == nil || v.Kind() != constant.String || !namesLaunchValue(constant.StringVal(v)) {
		return "", false
	}
	return v.ExactString(), true
}

// namesLaunchValue is whether s is a launch value, or a list split on launchListSeparators with
// one as a word; a sentence that names a value is not a list.
func namesLaunchValue(s string) bool {
	words := strings.FieldsFunc(s, func(r rune) bool { return strings.ContainsRune(launchListSeparators, r) })
	return slices.ContainsFunc(words, func(w string) bool { return slices.Contains(launchValues, strings.TrimSpace(w)) })
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

// isLaunchLiteral is whether e is a composite literal holding a launch constant: a list or a
// map of the values.
func isLaunchLiteral(pass *analysis.Pass, e ast.Expr) bool {
	lit, isLit := ast.Unparen(e).(*ast.CompositeLit)
	return isLit && holdsLaunchValue(pass, lit)
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
