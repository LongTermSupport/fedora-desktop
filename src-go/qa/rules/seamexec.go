package rules

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
)

// startsProcess lists the standard-library calls that start or replace a process.
var startsProcess = []struct{ pkg, name string }{
	{"os/exec", "Command"},
	{"os/exec", "CommandContext"},
	{"syscall", "Exec"},
	{"os", "StartProcess"},
}

// SeamExec confines process starts to the runner.
var SeamExec = &analysis.Analyzer{
	Name: "project_seam_exec",
	Doc: `project_seam_exec forbids starting a process outside internal/execx.

exec.Command, exec.CommandContext, syscall.Exec and os.StartProcess may be called only in
internal/execx. Every external command goes through the execx runner so the fake sees it and
a test can assert the argv. Spelling the command with sh -c to get round the gosec G204
finding is the same defect. exec.LookPath is allowed anywhere: finding a tool starts nothing.

How to fix: build an execx.Cmd and run it with the Runner (or the Execer for a process
replacement); add the capability to execx if it is missing.`,
	Run:        perPass(runSeamExec),
	ResultType: noResultType,
}

func runSeamExec(pass *analysis.Pass) {
	if ownedBy(pass.Pkg.Path(), execOwner) {
		return
	}
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn := calledFunc(pass, call)
			for _, s := range startsProcess {
				if isPkgFunc(fn, s.pkg, s.name) {
					pass.Reportf(call.Pos(), "%s.%s outside %s: run the command through the execx runner",
						fn.Pkg().Name(), s.name, execOwner)
				}
			}
			return true
		})
	}
}
