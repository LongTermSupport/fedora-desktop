package rules

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
)

// WalkEntryType forbids using an entry a directory listing found as a directory without asking
// its type.
var WalkEntryType = &analysis.Analyzer{
	Name: "project_walk_entry_type",
	Doc: `project_walk_entry_type flags, in non-test files, a file system call on a path beneath an
entry a directory listing found, in a loop that never asks that entry's type.

A listing is a range over a slice of fs.DirEntry or fs.FileInfo (os.ReadDir, fs.ReadDir,
(*os.File).ReadDir or Readdir, or a function returning one). The entry is followed through every
value the loop body builds from it (a call of any package, a local, a + chain), but not into a
bool, a number, an error or a byte slice. The entry's own path is filepath.Join or path.Join
whose last element is its Name() and whose others are not built from it, or a + ending in its
Name(), or a local holding one. A path beneath the entry is any other value built from it.

Flagged: os.Open, OpenFile, ReadFile, Stat, Lstat, ReadDir, Readlink, Chdir or DirFS,
ioutil.ReadFile or ReadDir, fs.ReadFile, ReadDir or Stat, filepath.Walk or WalkDir given a path
beneath the entry, and the directory readers (ReadDir, DirFS, Chdir, Walk, WalkDir) given the
entry's own path, unless the loop body asks the entry's type: a call of its IsDir, Type, Info
or Mode, or os.Stat, os.Lstat or fs.Stat of its own path (to follow a symlink).

A listing holds regular files, symlinks, sockets and pipes beside directories: seats abandon
took every entry of the seat store's control/ for a session folder, so the launcher's
disconnect.log there was an unreadable session and every run refused (fedora-desktop#404).

Not caught: a range over names only (Readdirnames, Glob), the entry handed to a function of the
package that builds the path, a value built from the entry kept outside the loop, a loop that
asks the type but uses the answer wrongly, and an entry indexed (entries[i]) rather than ranged.
A path derived from the name but not beneath the entry (a sibling with another suffix) is flagged
too: asking the entry's type there costs one call.

How to fix: skip or refuse the entry unless its type is a directory (if !e.IsDir() { continue }),
or Stat its own path to follow a symlink and ask the target's type.`,
	Run:        perPass(runWalkEntryType),
	ResultType: noResultType,
}

const walkEntryMsg = "%s on a path beneath an entry a directory listing found, in a loop that never asks the entry's type: " +
	"a listing holds regular files, symlinks and sockets beside directories (project_walk_entry_type): " +
	"skip or refuse the entry unless e.IsDir(), e.Type(), or a Stat of its own path says it is a directory"

// entryFSCalls are the file system calls on a path, with the index of the path argument, and
// whether the call reads the path as a directory.
var entryFSCalls = []struct {
	pkg, name string
	arg       int
	dir       bool
}{
	{"os", "Open", 0, false},
	{"os", "OpenFile", 0, false},
	{"os", readFileName, 0, false},
	{"os", "Stat", 0, false},
	{"os", "Lstat", 0, false},
	{"os", readDirName, 0, true},
	{"os", "Readlink", 0, false},
	{"os", "Chdir", 0, true},
	{"os", "DirFS", 0, true},
	{ioutilPkg, readFileName, 0, false},
	{ioutilPkg, readDirName, 0, true},
	{ioFSPkg, readFileName, 1, false},
	{ioFSPkg, readDirName, 1, true},
	{ioFSPkg, "Stat", 1, false},
	{filepathPkg, "Walk", 0, true},
	{filepathPkg, "WalkDir", 0, true},
}

// entryTypeMethods are the methods of an entry that ask its type.
var entryTypeMethods = []string{"IsDir", "Type", "Info", "Mode"}

func runWalkEntryType(pass *analysis.Pass) {
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if st, ok := n.(*ast.RangeStmt); ok {
				checkEntryLoop(pass, st)
			}
			return true
		})
	}
}

// entryLoop follows one ranged entry through its loop body.
type entryLoop struct {
	pass  *analysis.Pass
	entry types.Object
	// built are the locals the body builds from the entry; own those holding its own path or name.
	built, own map[types.Object]bool
}

func checkEntryLoop(pass *analysis.Pass, st *ast.RangeStmt) {
	id, ok := st.Value.(*ast.Ident)
	if !ok || id.Name == "_" || !isListing(pass.TypesInfo.TypeOf(st.X)) {
		return
	}
	entry := pass.TypesInfo.ObjectOf(id)
	if entry == nil {
		return
	}
	l := &entryLoop{pass: pass, entry: entry, built: map[types.Object]bool{entry: true}, own: map[types.Object]bool{}}
	l.propagate(st.Body)
	if l.asksType(st.Body) {
		return
	}
	ast.Inspect(st.Body, func(n ast.Node) bool {
		call, isCall := n.(*ast.CallExpr)
		if !isCall {
			return true
		}
		if name, beneath := l.beneath(call); beneath {
			pass.Reportf(call.Pos(), walkEntryMsg, name)
		}
		return true
	})
}

// isListing is whether t is a slice of fs.DirEntry or fs.FileInfo.
func isListing(t types.Type) bool {
	if t == nil {
		return false
	}
	sl, ok := t.Underlying().(*types.Slice)
	if !ok {
		return false
	}
	named, ok := types.Unalias(sl.Elem()).(*types.Named)
	if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != ioFSPkg {
		return false
	}
	return named.Obj().Name() == "DirEntry" || named.Obj().Name() == "FileInfo"
}

// propagate marks, to a fixed point, the locals of the body assigned a value built from the
// entry, and those holding its own path or name.
func (l *entryLoop) propagate(body *ast.BlockStmt) {
	for changed := true; changed; {
		changed = false
		ast.Inspect(body, func(n ast.Node) bool {
			lhs, rhs := assignedPairs(n)
			for i, target := range lhs {
				changed = l.mark(target, rhs, i, len(lhs)) || changed
			}
			return true
		})
	}
}

// assignedPairs is the targets and values of an assignment or a var declaration.
func assignedPairs(n ast.Node) ([]ast.Expr, []ast.Expr) {
	switch s := n.(type) {
	case *ast.AssignStmt:
		return s.Lhs, s.Rhs
	case *ast.ValueSpec:
		lhs := make([]ast.Expr, len(s.Names))
		for i, name := range s.Names {
			lhs[i] = name
		}
		return lhs, s.Values
	}
	return nil, nil
}

// mark records target as built from the entry (and as its own path) when its value is; one
// value for several targets (a call's results) marks each of them.
func (l *entryLoop) mark(target ast.Expr, rhs []ast.Expr, i, n int) bool {
	id, ok := target.(*ast.Ident)
	if !ok || id.Name == "_" {
		return false
	}
	obj := l.pass.TypesInfo.ObjectOf(id)
	if obj == nil || !carriesPath(obj.Type()) {
		return false
	}
	var value ast.Expr
	switch {
	case len(rhs) == n:
		value = rhs[i]
	case len(rhs) == 1:
		value = rhs[0]
	default:
		return false
	}
	changed := false
	if !l.built[obj] && l.from(value) {
		l.built[obj], changed = true, true
	}
	if !l.own[obj] && len(rhs) == n && l.ownPath(value) {
		l.own[obj], changed = true, true
	}
	return changed
}

// carriesPath is whether a value of type t can name a path: not a bool, a number, an error or
// a byte slice.
func carriesPath(t types.Type) bool {
	if isErrorType(t) || isBytes(t) {
		return false
	}
	if b, ok := t.Underlying().(*types.Basic); ok {
		return b.Info()&types.IsString != 0
	}
	return true
}

// from is whether e is built from the entry.
func (l *entryLoop) from(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && l.built[l.pass.TypesInfo.ObjectOf(id)] {
			found = true
		}
		return !found
	})
	return found
}

// ownName is whether e is the entry's Name(), or a local holding it.
func (l *entryLoop) ownName(e ast.Expr) bool {
	e = ast.Unparen(e)
	if id, ok := e.(*ast.Ident); ok {
		return l.own[l.pass.TypesInfo.ObjectOf(id)]
	}
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Name" {
		return false
	}
	x, ok := ast.Unparen(sel.X).(*ast.Ident)
	return ok && l.pass.TypesInfo.ObjectOf(x) == l.entry
}

// ownPath is whether e names the entry itself: its name, a join whose last element is its name
// and whose others are not built from it, a + ending in it, or a local holding one.
func (l *entryLoop) ownPath(e ast.Expr) bool {
	e = ast.Unparen(e)
	if l.ownName(e) {
		return true
	}
	switch x := e.(type) {
	case *ast.CallExpr:
		fn := calledFunc(l.pass, x)
		if !isPkgFunc(fn, filepathPkg, "Join") && !isPkgFunc(fn, "path", "Join") || len(x.Args) < 2 {
			return false
		}
		last := len(x.Args) - 1
		return l.ownName(x.Args[last]) && !slices.ContainsFunc(x.Args[:last], l.from)
	case *ast.BinaryExpr:
		return x.Op == token.ADD && l.ownName(x.Y) && !l.from(x.X)
	}
	return false
}

// asksType is whether the body asks the entry's type: a type method of it, or a Stat of its
// own path.
func (l *entryLoop) asksType(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || found {
			return !found
		}
		if sel, isSel := call.Fun.(*ast.SelectorExpr); isSel && slices.Contains(entryTypeMethods, sel.Sel.Name) {
			if x, isIdent := ast.Unparen(sel.X).(*ast.Ident); isIdent && l.pass.TypesInfo.ObjectOf(x) == l.entry {
				found = true
			}
		}
		fn := calledFunc(l.pass, call)
		switch {
		case (isPkgFunc(fn, "os", "Stat") || isPkgFunc(fn, "os", "Lstat")) && len(call.Args) == 1:
			found = found || l.ownPath(call.Args[0])
		case isPkgFunc(fn, ioFSPkg, "Stat") && len(call.Args) == 2:
			found = found || l.ownPath(call.Args[1])
		}
		return !found
	})
	return found
}

// beneath is the name of a file system call and whether it is given a path beneath the entry,
// or the entry's own path to read as a directory.
func (l *entryLoop) beneath(call *ast.CallExpr) (string, bool) {
	fn := calledFunc(l.pass, call)
	for _, c := range entryFSCalls {
		if !isPkgFunc(fn, c.pkg, c.name) || len(call.Args) <= c.arg {
			continue
		}
		arg := call.Args[c.arg]
		if !l.from(arg) {
			return "", false
		}
		own := l.ownPath(arg)
		return fn.Pkg().Name() + "." + fn.Name(), !own || c.dir
	}
	return "", false
}
