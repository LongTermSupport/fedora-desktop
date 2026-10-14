// Package rules holds this module's project rules (Defence Before Fix): go/analysis analyzers
// for the defect classes the ccy-teams and ccy-sessions ports actually hit. Each is proven on
// a fixture reproducing the defect (testdata) and run over src-go by scripts/qa-go.bash.
// CLAUDE/GoProjectRules.md is the catalogue.
//
// The tables below are the rules' data: which package owns which seam, and the narrow,
// reasoned exceptions. Nothing is suppressed in the code under check.
package rules

import (
	"go/ast"
	"go/types"
	"path/filepath"
	"reflect"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// execOwner is the only package that may start a process: every external command goes
// through its runner so a fake sees it.
const execOwner = "internal/execx"

// homeOwner is the only package that may abbreviate the home directory.
const homeOwner = "internal/term"

const (
	sessprobePkg = "internal/sessprobe"
	podfreezePkg = "internal/podfreeze"
	lxcfreezePkg = "internal/lxcfreeze"
	teamsPkg     = "internal/teams"
	sudoPkg      = "internal/sudo"
	agentbusPkg  = "internal/agentbus"
)

// commandOwners maps a program to the packages that own every use of it, so its flags and
// target forms are decided once (the gum and fzf prompts, the sudo and tmux argument forms).
// A program with two owners is used for two different jobs: podman is read for a session's
// container by sessprobe and driven (pause, unpause, networks) by podfreeze. A program absent
// from the table is unowned: any package may run it.
var commandOwners = map[string][]string{
	"sudo":         {sudoPkg},
	"tmux":         {"internal/tmuxccy"},
	"gum":          {"internal/ui"},
	"fzf":          {"internal/ui"},
	"systemctl":    {"internal/systemd"},
	"busctl":       {"internal/systemd"},
	"git":          {"internal/gitx"},
	"ps":           {sessprobePkg},
	"podman":       {sessprobePkg, podfreezePkg},
	"docker":       {sessprobePkg},
	"lxc-ls":       {lxcfreezePkg},
	"lxc-info":     {lxcfreezePkg},
	"lxc-freeze":   {lxcfreezePkg},
	"lxc-unfreeze": {lxcfreezePkg},
	"lxc-attach":   {lxcfreezePkg},
	"nmcli":        {lxcfreezePkg},
}

// programRunner is a function or method that runs the program named by its arguments from
// argIndex on (the program, then its arguments) without the caller building an execx.Cmd, so
// the call is judged as the Cmd would be. recv is the receiver type name, "" for a package
// function.
type programRunner struct {
	pkg      string
	recv     string
	method   string
	argIndex int
}

// programRunners are the entry points that run a program from their arguments. sudo's three
// run any program as root; lxcfreeze's two helpers are the same shape one level up, so their
// callers are judged too.
var programRunners = []programRunner{
	{pkg: sudoPkg, recv: "Sudo", method: "Capture", argIndex: 1},
	{pkg: sudoPkg, recv: "Sudo", method: "AsRoot", argIndex: 1},
	{pkg: sudoPkg, recv: "Sudo", method: "Run", argIndex: 1},
	{pkg: lxcfreezePkg, recv: "Engine", method: "runAct", argIndex: 1},
	{pkg: lxcfreezePkg, recv: "Engine", method: "probe", argIndex: 1},
}

// shells are interpreters that run a string given with -c; handing one to the runner is the
// sh -c route round the argv the runner and its fake exist to show.
var shells = map[string]bool{"sh": true, "bash": true, "dash": true, "zsh": true}

// allowance excuses one function of one package from a rule. It is data, with the reason,
// never a comment in the code. pkg is a package path suffix, as for an owner.
type allowance struct {
	pkg    string
	fn     string // the enclosing function; "" is a package-level declaration
	reason string
}

// dynamicNames are the functions that may build an execx.Cmd whose Name is not a constant,
// because the program is chosen at run time. Every other Name is a constant, so the owner
// table can judge it. A dynamic Name is not judged against the owner table, so each row says
// what it can be. fn is "func" or "Type.method".
var dynamicNames = []allowance{
	{
		pkg:    sessprobePkg,
		fn:     "enginePS",
		reason: "the container engine (podman or docker) the caller found; sessprobe owns both",
	},
	{
		pkg:    sessprobePkg,
		fn:     "Probe.inspectRunning",
		reason: "the container engine (podman or docker) found in a session's process tree; sessprobe owns both",
	},
	{
		pkg:    "internal/sessions",
		fn:     "route.run",
		reason: "a project's daemon CLI path, or the container engine that execs it inside the project's container",
	},
	{
		pkg:    teamsPkg,
		fn:     "launcherVia",
		reason: "the ccy launcher path the process is replaced with, given by the environment",
	},
	{
		pkg:    teamsPkg,
		fn:     "teamOps.runPlay",
		reason: "the agent-bus play, run by its own shebang; its path is configuration",
	},
	{
		pkg:    teamsPkg,
		fn:     "teamOps.runElementPlay",
		reason: "the Element play, run by its own shebang; its path is configuration",
	},
	{
		pkg:    lxcfreezePkg,
		fn:     "Engine.runAct",
		reason: "runs the lxc command its caller names; every caller is itself judged as a program runner",
	},
	{
		pkg:    lxcfreezePkg,
		fn:     "Engine.probe",
		reason: "runs the read-only command its caller names; every caller is itself judged as a program runner",
	},
	{
		pkg:    agentbusPkg,
		fn:     "Client.rootRun",
		reason: "runs the root tool its caller names; the callers pass the sudo.AgentBus and sudo.InstallTool paths",
	},
	{
		pkg:    agentbusPkg,
		fn:     "Client.rootShow",
		reason: "streams the root tool its caller names; the callers pass the sudo.AgentBus and sudo.InstallTool paths",
	},
}

// clockAllowance excuses one test function from the fixed-clock rule.
type clockAllowance = allowance

var clockAllowances = []clockAllowance{
	{
		pkg:    "internal/tmuxccy",
		fn:     "TestTakeOverDefaultsToTheRealClockAndSleep",
		reason: "proves the default Sleep really sleeps; only the wall clock can show that",
	},
}

// noResult is what every rule hands the analysis framework: the rules only report.
type noResult struct{}

var noResultType = reflect.TypeFor[noResult]()

// perPass adapts a reporting function to an analyzer's Run.
func perPass(run func(*analysis.Pass)) func(*analysis.Pass) (any, error) {
	return func(pass *analysis.Pass) (any, error) {
		run(pass)
		return noResult{}, nil
	}
}

// All is every rule, in the order the driver registers them.
func All() []*analysis.Analyzer {
	return []*analysis.Analyzer{SeamExec, SeamCommands, TestClock, HomeAbbrev, TmuxTargets}
}

// ownedBy is whether the package path (the test variants of it included) is owner, matched
// as a path suffix on a whole element so the fixtures and the real module share the table.
func ownedBy(pkgPath, owner string) bool {
	path := pkgPath
	if i := strings.IndexByte(path, ' '); i >= 0 {
		path = path[:i]
	}
	path = strings.TrimSuffix(path, "_test")
	return path == owner || strings.HasSuffix(path, "/"+owner)
}

// funcName is the name an allowance row uses for a declaration: "func" or "Type.method",
// and "" for a package-level declaration that is not a function.
func funcName(decl ast.Decl) string {
	fd, ok := decl.(*ast.FuncDecl)
	if !ok {
		return ""
	}
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	recv := fd.Recv.List[0].Type
	if star, isStar := recv.(*ast.StarExpr); isStar {
		recv = star.X
	}
	if idx, isIdx := recv.(*ast.IndexExpr); isIdx {
		recv = idx.X
	}
	if id, isIdent := recv.(*ast.Ident); isIdent {
		return id.Name + "." + fd.Name.Name
	}
	return fd.Name.Name
}

// isTestFile is whether the file is a _test.go file.
func isTestFile(pass *analysis.Pass, f *ast.File) bool {
	return strings.HasSuffix(filepath.Base(pass.Fset.Position(f.Pos()).Filename), "_test.go")
}

// calledFunc is the function a call expression calls, or nil for a conversion, a builtin or
// a call through a value.
func calledFunc(pass *analysis.Pass, call *ast.CallExpr) *types.Func {
	var id *ast.Ident
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		id = fn.Sel
	case *ast.Ident:
		id = fn
	default:
		return nil
	}
	f, ok := pass.TypesInfo.Uses[id].(*types.Func)
	if !ok {
		return nil
	}
	return f
}

// isPkgFunc is whether f is the package-level function pkgPath.name (not a method).
func isPkgFunc(f *types.Func, pkgPath, name string) bool {
	if f == nil || f.Pkg() == nil || f.Pkg().Path() != pkgPath || f.Name() != name {
		return false
	}
	sig, ok := f.Type().(*types.Signature)
	return ok && sig.Recv() == nil
}
