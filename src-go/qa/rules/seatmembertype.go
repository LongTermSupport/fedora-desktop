package rules

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// memberTypeParam is the agentbus client's parameter that says which kind of launcher a seat is for.
const memberTypeParam = "memberType"

// SeatMemberType forbids asking agent-bus about a session's seats with a member type fixed in the
// code.
var SeatMemberType = &analysis.Analyzer{
	Name: "project_seat_member_type",
	Doc: `project_seat_member_type flags a call to the agentbus client (internal/agentbus, matched
through types.Info) whose memberType argument is a compile-time constant ("podman", "host", "",
a named constant), outside the agentbus package itself.

A checkout's seat is a ccy container's (member type podman) or a cc session's on the desktop
(member type host), and agent-bus offers, creates and refuses seats by that type. The teams
picker's Add to a team asked seat choose with --type=podman for every session, so a cc session
was offered ccy's seats, which session add then refuses, and never its own.

Not caught: a type computed by a function that ignores the session's kind, and a constant
passed through a variable first. Test files, and the client's own wrappers (whose "" leaves the
type to agent-bus's default), are not judged.

How to fix: pass the type of the session's kind (hot.MemberType(kind)), or the launcher's own
type passed in by its caller.`,
	Run:        perPass(runSeatMemberType),
	ResultType: noResultType,
}

func runSeatMemberType(pass *analysis.Pass) {
	if ownedBy(pass.Pkg.Path(), agentbusPkg) {
		return
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if arg := constantMemberType(pass, call); arg != nil {
					pass.Reportf(arg.Pos(), "the member type of a seat is the session's kind: pass hot.MemberType(kind) or the launcher's own type, not a constant (project_seat_member_type)")
				}
			}
			return true
		})
	}
}

// constantMemberType is the memberType argument of a call to the agentbus client when it is a
// compile-time constant, or nil.
func constantMemberType(pass *analysis.Pass, call *ast.CallExpr) ast.Expr {
	fn := calledFunc(pass, call)
	if fn == nil || fn.Pkg() == nil || !ownedBy(fn.Pkg().Path(), agentbusPkg) {
		return nil
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return nil
	}
	for i := range sig.Params().Len() {
		if sig.Params().At(i).Name() != memberTypeParam || i >= len(call.Args) {
			continue
		}
		if tv, known := pass.TypesInfo.Types[call.Args[i]]; known && tv.Value != nil {
			return call.Args[i]
		}
	}
	return nil
}
