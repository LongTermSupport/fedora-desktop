package rules

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const (
	conversationPkg = "internal/conversation"
	registryPkg     = "internal/registry"
	// startPlanner is the one function that decides how a recorded session is started.
	startPlanner = "StartPlan"
)

// StartDecision keeps the decision how a recorded session is started (resume its recorded
// conversation, or start fresh) in conversation.StartPlan.
var StartDecision = &analysis.Analyzer{
	Name: "project_start_decision",
	Doc: `project_start_decision flags, outside test files, a use of internal/registry's ResumeArgs
or FreshArgs anywhere but conversation.StartPlan and the registry itself, and a use of an
exported error of internal/conversation outside that package (matched through types.Info, so a
call, a method value and a variable holding one are all seen).

A recorded session is started by a boot restore, judged by restore --check and declare, and
started again by a team restart. Each decided for itself whether to resume the recorded
conversation or start fresh, and they disagreed: the restore started a record marked
fresh-if-no-conversation fresh only when its conversation.json was missing, matched by
errors.Is on conversation.ErrNotRecorded, and refused one naming a conversation with no
transcript (a session that never took a turn), so the next reboot could not restore it; the
team restart never started such a record fresh at all (fedora-desktop#270).

Not caught: a start decided from conversation.For's error text, and arguments built by hand
without the registry's builders.

How to fix: ask conversation.StartPlan(rec, claudeHome), which returns the conversation to
resume (or fresh) and the arguments, and act on its answer.`,
	Run:        perPass(runStartDecision),
	ResultType: noResultType,
}

func runStartDecision(pass *analysis.Pass) {
	path := pass.Pkg.Path()
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, d := range f.Decls {
			if ownedBy(path, conversationPkg) && funcName(d) == startPlanner {
				continue
			}
			ast.Inspect(d, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && decidesStart(pass, path, id) {
					pass.Reportf(id.Pos(), "a recorded session's start is decided in conversation.StartPlan alone: ask it, do not build the arguments or match the lookup's errors (project_start_decision)")
				}
				return true
			})
		}
	}
}

// decidesStart is whether id names one of the start builders, outside the registry, or an
// exported error of the conversation package, outside it.
func decidesStart(pass *analysis.Pass, path string, id *ast.Ident) bool {
	switch obj := pass.TypesInfo.Uses[id].(type) {
	case *types.Func:
		return (obj.Name() == "ResumeArgs" || obj.Name() == "FreshArgs") && obj.Pkg() != nil &&
			ownedBy(obj.Pkg().Path(), registryPkg) && !ownedBy(path, registryPkg)
	case *types.Var:
		return strings.HasPrefix(obj.Name(), "Err") && obj.Exported() && obj.Pkg() != nil && !obj.IsField() &&
			ownedBy(obj.Pkg().Path(), conversationPkg) && !ownedBy(path, conversationPkg)
	}
	return false
}
