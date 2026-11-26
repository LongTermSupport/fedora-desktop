package rules

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
)

// RootCreateFollowsLink forbids a create through an os.Root without O_EXCL.
var RootCreateFollowsLink = &analysis.Analyzer{
	Name: "project_root_create_follows_link",
	Doc: `project_root_create_follows_link flags, in non-test files, a create through an os.Root that
is not exclusive: os.Root's Create or WriteFile, or its OpenFile with flags (a constant
expression, or a local the function assigns one) that hold O_CREATE and not O_EXCL.

An os.Root keeps every open inside its directory, but it follows a link at the last name that
stays inside, O_NOFOLLOW or not. A create there that is not exclusive opens the link's target,
and when the link dangles it makes that target: a file the caller never named. An os.Root is
chosen because its directory is not trusted (a checkout a container writes as its own), so a
link there is the case it is for. The seat-record lock was opened O_RDWR|O_CREATE through the
checkout's os.Root (the batch 22b review, Plan 00265 R36).

Not caught: flags the function does not spell (a parameter, a field); a create through a
wrapper of os.Root.

How to fix: create with O_CREATE|O_EXCL, which refuses any link at the name; when the file may
already be there, open it after fs.ErrExist without O_CREATE (a dangling link is then an error),
and check that what was opened is the plain file at the name (os.SameFile of f.Stat and the
root's Lstat).`,
	Run:        perPass(runRootCreateFollowsLink),
	ResultType: noResultType,
}

const rootCreateMsg = "%s creates without O_EXCL: an os.Root follows a link at the last name, O_NOFOLLOW or not, " +
	"so a dangling link there gets its target created (project_root_create_follows_link): create with " +
	"O_CREATE|O_EXCL, and open an existing file without O_CREATE"

// rootCreates are os.Root's creates; a flags index of -1 always creates.
var rootCreates = []tempCall{
	{"os", "Root", "Create", 0, -1},
	{"os", "Root", "WriteFile", 0, -1},
	{"os", "Root", "OpenFile", 0, 1},
}

func runRootCreateFollowsLink(pass *analysis.Pass) {
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, isCall := n.(*ast.CallExpr)
				if !isCall {
					return true
				}
				c, found := matchTempCall(calledFunc(pass, call), rootCreates)
				if found && rootCreateNonExclusive(pass, fd.Body, c, call) {
					pass.Reportf(call.Pos(), rootCreateMsg, tempCallName(c))
				}
				return true
			})
		}
	}
}

// rootCreateNonExclusive is whether call (a row of rootCreates) creates without O_EXCL: Create
// and WriteFile always do; OpenFile when its flags hold O_CREAT and not O_EXCL.
func rootCreateNonExclusive(pass *analysis.Pass, body *ast.BlockStmt, c tempCall, call *ast.CallExpr) bool {
	if c.flags < 0 {
		return true
	}
	if c.flags >= len(call.Args) {
		return false
	}
	flags := call.Args[c.flags]
	return holdsBit(pass, body, flags, oCreat) && !holdsBit(pass, body, flags, oExcl)
}
