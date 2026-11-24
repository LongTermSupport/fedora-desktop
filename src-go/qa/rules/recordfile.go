package rules

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

const (
	// registryStore is the registry's store type.
	registryStore = "Store"
	// registryDirName is both the store's directory method and the package's locator.
	registryDirName = "Dir"
	// registryNameRemove is the store's name-keyed removal.
	registryNameRemove = "Remove"
)

const recordFileFix = "a session record's file named from its name outside internal/registry (project_record_file_by_name): " +
	"a record's name= is not held to its file's name, so use the file the registry's reading carries " +
	"(Reading.Files, Reading.FilesOf, Store.Find) and remove it with Store.RemoveFile or Store.RemoveSession"

// RecordFileByName keeps the naming of a session record's file in the registry.
var RecordFileByName = &analysis.Analyzer{
	Name: "project_record_file_by_name",
	Doc: `project_record_file_by_name flags, in the non-test files of every package but
internal/registry, a session record's file found by the record's name rather than by the reading
that found it:

- a use of internal/registry's Store.Remove (the name-keyed removal): a call, a method value or a
  method expression;
- a path built in the registry directory: filepath.Join or path.Join with two or more elements
  whose first is the registry directory, or a + whose leftmost operand is. The registry directory
  is a call of Store.Dir or of the package's Dir, or a variable, package variable or struct field
  assigned one (by :=, =, var or a struct literal), followed through further assignments.

The registry's writer names a record's file after its name=, but nothing holds a record to that:
a record edited or renamed by hand lives in a file of another name. A record found by reading
the registry carries its file (Reading.Files); rebuilding <registry>/<name> from the record left
the real record behind as a stale record that failed every later restore (ending a session in
the picker, fedora-desktop#396, #398), named the wrong file in a refusal, and handed the wrong
file to the trampoline that removes a session's record when it ends.

Tests are not judged: a test plants a record where the writer would.

Not caught: the directory passed as a parameter (judged where the parameter is set, not where it
is joined); a path built by fmt.Sprintf or strings.Join; a name-keyed lookup (Store.Get, which
reads the file named after the name and is a reading of its own).

How to fix: read the registry (Store.ReadAll) and use the file each record carries, Reading.FilesOf
for the files answering to a session's name, Store.Find for the one record of a session and its
file; remove by Store.RemoveFile, or Store.RemoveSession for every record of a session.`,
	Run:        perPass(runRecordFileByName),
	ResultType: noResultType,
}

func runRecordFileByName(pass *analysis.Pass) {
	if ownedBy(pass.Pkg.Path(), registryPkg) {
		return
	}
	held := registryDirHolders(pass)
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if namesRecordFile(pass, n, held) {
				pass.Reportf(n.Pos(), "%s", recordFileFix)
				return false
			}
			return true
		})
	}
}

// namesRecordFile is whether n is a use of the name-keyed Remove, or a path built in the
// registry directory.
func namesRecordFile(pass *analysis.Pass, n ast.Node, held map[types.Object]bool) bool {
	switch e := n.(type) {
	case *ast.SelectorExpr:
		f, ok := pass.TypesInfo.Uses[e.Sel].(*types.Func)
		return ok && isRegistryStoreMethod(f, registryNameRemove)
	case *ast.CallExpr:
		f := calledFunc(pass, e)
		joins := isPkgFunc(f, "path/filepath", "Join") || isPkgFunc(f, "path", "Join")
		return joins && len(e.Args) >= 2 && isRegistryDir(pass, e.Args[0], held)
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return false
		}
		left := e.X
		for {
			inner, ok := ast.Unparen(left).(*ast.BinaryExpr)
			if !ok || inner.Op != token.ADD {
				break
			}
			left = inner.X
		}
		return isRegistryDir(pass, left, held)
	}
	return false
}

// isRegistryStoreMethod is whether f is the method name of internal/registry's Store.
func isRegistryStoreMethod(f *types.Func, name string) bool {
	named := receiverNamed(f)
	return f.Name() == name && named != nil && named.Obj().Name() == registryStore &&
		f.Pkg() != nil && ownedBy(f.Pkg().Path(), registryPkg)
}

// isRegistryDirCall is whether e calls Store.Dir or the registry package's Dir.
func isRegistryDirCall(pass *analysis.Pass, e ast.Expr) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return false
	}
	f := calledFunc(pass, call)
	if f == nil || f.Name() != registryDirName || f.Pkg() == nil || !ownedBy(f.Pkg().Path(), registryPkg) {
		return false
	}
	return receiverNamed(f) == nil || isRegistryStoreMethod(f, registryDirName)
}

// isRegistryDir is whether e is the registry directory: a call naming it, or a variable or field
// that holds it.
func isRegistryDir(pass *analysis.Pass, e ast.Expr, held map[types.Object]bool) bool {
	if isRegistryDirCall(pass, e) {
		return true
	}
	obj := exprObject(pass, e)
	return obj != nil && held[obj]
}

// exprObject is the variable or field an identifier or selector names, nil for anything else.
func exprObject(pass *analysis.Pass, e ast.Expr) types.Object {
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		return pass.TypesInfo.ObjectOf(x)
	case *ast.SelectorExpr:
		if v, ok := pass.TypesInfo.Uses[x.Sel].(*types.Var); ok {
			return v
		}
	}
	return nil
}

// registryDirHolders is every variable and field of the package assigned the registry directory,
// to a fixed point, so the directory is followed through a relay of assignments.
func registryDirHolders(pass *analysis.Pass) map[types.Object]bool {
	held := map[types.Object]bool{}
	for changed := true; changed; {
		changed = false
		hold := func(target ast.Expr, value ast.Expr) {
			obj := exprObject(pass, target)
			if obj != nil && !held[obj] && isRegistryDir(pass, value, held) {
				held[obj] = true
				changed = true
			}
		}
		for _, f := range pass.Files {
			ast.Inspect(f, func(n ast.Node) bool {
				holdAssigned(pass, n, hold)
				return true
			})
		}
	}
	return held
}

// holdAssigned hands hold each target n assigns with its value: an assignment, a var
// declaration, or a field of a struct literal.
func holdAssigned(pass *analysis.Pass, n ast.Node, hold func(target, value ast.Expr)) {
	switch s := n.(type) {
	case *ast.AssignStmt:
		holdPairs(s.Lhs, s.Rhs, hold)
	case *ast.ValueSpec:
		targets := make([]ast.Expr, len(s.Names))
		for i, id := range s.Names {
			targets[i] = id
		}
		holdPairs(targets, s.Values, hold)
	case *ast.KeyValueExpr:
		key, ok := s.Key.(*ast.Ident)
		if !ok {
			return
		}
		if _, isField := pass.TypesInfo.Uses[key].(*types.Var); isField {
			hold(key, s.Value)
		}
	}
}

// holdPairs hands hold each target with its value; a single call assigned to several targets
// (regdir, err := registry.Dir(getenv)) gives its first target the call.
func holdPairs(targets, values []ast.Expr, hold func(target, value ast.Expr)) {
	switch {
	case len(targets) == len(values):
		for i := range targets {
			hold(targets[i], values[i])
		}
	case len(values) == 1 && len(targets) > 1:
		hold(targets[0], values[0])
	}
}
