package rules

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// recordLauncherPkg owns the session record: it reads and writes its launcher= line, and its
// Launcher function is the fixed table a session is started by (Plan 00266 Task A.4).
const recordLauncherPkg = "internal/registry"

const recordLauncherFix = " (project_record_launcher_exec): any process of the user can write a session record, so the path it records is not a program to run; start the session by registry.Launcher, the fixed table's program for its prefix"

// RecordLauncherExec confines the reading of a session record's launcher= path to internal/registry.
var RecordLauncherExec = &analysis.Analyzer{
	Name: "project_record_launcher_exec",
	Doc: `project_record_launcher_exec flags, in src-go's internal/ and cmd/ trees, every read of the
Launcher field of internal/registry's Record outside internal/registry, except as an operand of
== or !=.

A session record is a file in the user's state directory that any process of that user can
write: the launcher itself, ccy-sessions declare, and a host cc session's claude. A restore and a
team restart started the session by the path its launcher= line holds, so a planted line was a
program the next boot ran unasked, outside every refusal a launch has (Plan 00266 Task A.4,
class "host executes a session record's launcher path"). registry.Launcher gives the fixed
table's program for the record's prefix (ccy, cc) and refuses any other prefix. The defect the
rule exists for was restore's start of a recorded session; the sweep found a team restart's
start of a stopped one.

A comparison reads the path to compare two records and runs nothing. A write (an assignment, a
composite literal's key) is not a read. Test files are not judged: they check what was
recorded. The nested qa module is not judged.

Not caught: the path read out of the record's file by other means (a line parsed by hand), or
through a copy of the field made inside internal/registry and handed out.

How to fix: registry.Launcher(getenv, rec).`,
	Run:        perPass(runRecordLauncherExec),
	ResultType: noResultType,
}

func runRecordLauncherExec(pass *analysis.Pass) {
	path := pass.Pkg.Path()
	judged := inTree(path, "internal") || inTree(path, "cmd")
	if !judged || inTree(path, "qa") || ownedBy(path, recordLauncherPkg) {
		return
	}
	for _, f := range pass.Files {
		if !isTestFile(pass, f) {
			judgeRecordLauncherReads(pass, f)
		}
	}
}

// judgeRecordLauncherReads reports each read of the field in f. A parent is visited before its
// children, so a comparison's operands and an assignment's left side are known when reached.
func judgeRecordLauncherReads(pass *analysis.Pass, f *ast.File) {
	exempt := map[ast.Expr]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BinaryExpr:
			if x.Op == token.EQL || x.Op == token.NEQ {
				exempt[ast.Unparen(x.X)], exempt[ast.Unparen(x.Y)] = true, true
			}
		case *ast.AssignStmt:
			for _, lhs := range x.Lhs {
				exempt[ast.Unparen(lhs)] = true
			}
		case *ast.SelectorExpr:
			if !exempt[x] && isRecordLauncher(pass, x) {
				pass.Reportf(x.Pos(), "the launcher= path of a session record read outside internal/registry%s", recordLauncherFix)
			}
		}
		return true
	})
}

// isRecordLauncher is whether sel selects the Launcher field of internal/registry's Record.
func isRecordLauncher(pass *analysis.Pass, sel *ast.SelectorExpr) bool {
	field, isVar := pass.TypesInfo.Uses[sel.Sel].(*types.Var)
	if !isVar || !field.IsField() || field.Name() != "Launcher" || field.Pkg() == nil {
		return false
	}
	return ownedBy(field.Pkg().Path(), recordLauncherPkg) && isFieldOf(field, "Record")
}
