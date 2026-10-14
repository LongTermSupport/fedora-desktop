package rules

import (
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
)

// startsProcess lists the standard-library functions that start or replace a process.
var startsProcess = map[string][]string{
	"os/exec": {"Command", "CommandContext"},
	"syscall": {"Exec", "ForkExec", "StartProcess"},
	"os":      {"StartProcess"},
}

// cmdRunners are the methods of os/exec.Cmd that start the process it describes.
var cmdRunners = []string{"Run", "Start", "Output", "CombinedOutput"}

// SeamExec confines process starts to the runner.
var SeamExec = &analysis.Analyzer{
	Name: "project_seam_exec",
	Doc: `project_seam_exec forbids starting a process outside internal/execx.

Any reference, not only a call, to exec.Command, exec.CommandContext, syscall.Exec,
syscall.ForkExec, syscall.StartProcess or os.StartProcess is flagged outside internal/execx: a
function value (f := exec.Command), an argument, an assignment. So is any use of the os/exec.Cmd
type (a composite literal, new(exec.Cmd), a parameter) and the Run, Start, Output and
CombinedOutput methods of one. Every external command goes through the execx runner so the fake
sees it and a test can assert the argv. exec.LookPath is allowed anywhere: finding a tool starts
nothing.

Not caught: raw syscall.Syscall(SYS_EXECVE), cgo, and a process started by code outside this
module (no dependency of src-go starts one). Spelling a command as sh -c through the runner is
seam_commands' business.

How to fix: build an execx.Cmd and run it with the Runner (or the Execer for a process
replacement); add the capability to execx if it is missing.`,
	Run:        perPass(runSeamExec),
	ResultType: noResultType,
}

func runSeamExec(pass *analysis.Pass) {
	if ownedByAny(pass.Pkg.Path(), execOwners) {
		return
	}
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				checkExecUse(pass, id)
			}
			return true
		})
	}
}

func checkExecUse(pass *analysis.Pass, id *ast.Ident) {
	switch obj := pass.TypesInfo.Uses[id].(type) {
	case *types.Func:
		if recv := receiverNamed(obj); recv != nil {
			if isOSExecCmd(recv) && slices.Contains(cmdRunners, obj.Name()) {
				pass.Reportf(id.Pos(), "(*exec.Cmd).%s outside %s: run the command through the execx runner", obj.Name(), execOwnerNames)
			}
			return
		}
		if obj.Pkg() != nil && slices.Contains(startsProcess[obj.Pkg().Path()], obj.Name()) {
			pass.Reportf(id.Pos(), "%s.%s outside %s: run the command through the execx runner",
				obj.Pkg().Name(), obj.Name(), execOwnerNames)
		}
	case *types.TypeName:
		if isOSExecCmd(obj.Type()) {
			pass.Reportf(id.Pos(), "exec.Cmd outside %s: run the command through the execx runner", execOwnerNames)
		}
	}
}

// receiverNamed is the named type a method is declared on, nil for a plain function.
func receiverNamed(f *types.Func) *types.Named {
	sig, ok := f.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return nil
	}
	t := sig.Recv().Type()
	if p, isPtr := t.(*types.Pointer); isPtr {
		t = p.Elem()
	}
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return nil
	}
	return named
}

// isOSExecCmd is whether t is os/exec.Cmd (or a pointer to it).
func isOSExecCmd(t types.Type) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := types.Unalias(t).(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Pkg().Path() == "os/exec" && named.Obj().Name() == "Cmd"
}
