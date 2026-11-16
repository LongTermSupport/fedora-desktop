package rules

import (
	"go/ast"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// stdinSeams are the packages that may read stdin: the shared reader; the prompt seam, which
// bounds a piped answer with the shared reader's wait; the process seam, which hands a child
// the stdin it is given; the relay of the terminal to a child (internal/keyrelay), which ends on a
// stop its owner controls and reads only once poll finds a key, so it never blocks on a read
// alone; the terminal test and the terminal echo, which ask about the file and never read it. They are matched by path suffix, so the fixtures and the real modules share them.
var stdinSeams = []string{"internal/stdinread", uiPkg, "internal/execx", "internal/keyrelay", "internal/term", "internal/ttyecho"}

// bufioPkg is the buffered-reader package's path.
const bufioPkg = "bufio"

// stdinReaders are the functions that read a reader they are given, by package path and name,
// with the index of the argument read (-1: every argument).
var stdinReaders = map[[2]string]int{
	{"io", "ReadAll"}: 0, {"io", "ReadFull"}: 0, {"io", "ReadAtLeast"}: 0,
	{"io", "Copy"}: 1, {"io", "CopyN"}: 1, {"io", "CopyBuffer"}: 1,
	{"io", "LimitReader"}: 0, {"io", "TeeReader"}: 0, {"io", "MultiReader"}: -1,
	{bufioPkg, "NewReader"}: 0, {bufioPkg, "NewReaderSize"}: 0, {bufioPkg, "NewScanner"}: 0,
	{"encoding/json", "NewDecoder"}: 0, {"encoding/csv", "NewReader"}: 0, {"encoding/xml", "NewDecoder"}: 0,
}

// stdinReadMethods are the methods that read their receiver.
var stdinReadMethods = []string{"Read", "ReadString", "ReadByte", "ReadBytes", "ReadRune", "ReadLine", "WriteTo"}

const stdinSeamFix = " (project_stdin_seam): read it with stdinread.Read, which says when it is waiting and takes a bound, or prompt through internal/ui, which bounds a piped answer the same way; hand stdin on only as stdin"

// StdinSeam holds every read of a process's stdin to the seams that bound it.
var StdinSeam = &analysis.Analyzer{
	Name: "project_stdin_seam",
	Doc: `project_stdin_seam flags a read of stdin outside internal/stdinread and the other stdin
seams (internal/ui, internal/execx, internal/keyrelay, internal/term, internal/ttyecho), in every package of both
modules but a test double's (a package whose name ends in "test"); _test.go files are not judged.

Stdin is os.Stdin, or a variable, parameter or struct field named stdin or Stdin whose type reads
(has Read([]byte) (int, error)); a Stdin that is a string is not. Flagged:

- a read of it: io.ReadAll, ReadFull, ReadAtLeast, Copy, CopyN, CopyBuffer, LimitReader,
  TeeReader and MultiReader; bufio.NewReader, NewReaderSize and NewScanner; json.NewDecoder,
  csv.NewReader and xml.NewDecoder; and its own Read, ReadString, ReadByte, ReadBytes, ReadRune,
  ReadLine or WriteTo;
- stdin handed on under another name, which would hide the read from this rule: an argument to a
  parameter of a reader type not named stdin or Stdin (outside the seams), a variable or field of
  another name assigned from it, and a struct literal's field of another name (but a seam's own
  type, such as ui.Lines' In).

Passing stdin along as stdin (a run(args, stdin) parameter, an Env's Stdin, an exec.Cmd's Stdin),
asking whether it is a terminal, comparing it with nil and taking its descriptor are not reads.

A read of stdin returns at end of input, and an open pipe that nothing writes to and nothing
closes, which is what an agent's shell gives a command run with no input, never ends: the
command waits for ever and says nothing (issue #260: pingbus thread --close waited minutes).
stdinread says when it is waiting and refuses after a bound; a prompt through internal/ui waits
on a piped answer with the same notice and bound (stdinread.Await), a line at a time, and on a
terminal for as long as the person needs.

Not caught: stdin returned from a function or kept in a slice or map and read elsewhere, an
unkeyed struct literal, a reader built by reflection, and Python (the semgrep rule
stdin-read-unbounded covers it).

How to fix: data, err := stdinread.Read(stdin, stdinread.Options{Prog: "tool", Limit: n,
GiveUpAfter: time.Minute}) for piped input, handing bytes.NewReader(data) on if a callee wants a
reader; a prompt through ui.Lines, which bounds a piped answer.`,
	Run:        perPass(runStdinSeam),
	ResultType: noResultType,
}

func runStdinSeam(pass *analysis.Pass) {
	if inStdinSeam(pass.Pkg.Path()) || stdinTestDouble(pass.Pkg.Path()) {
		return
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			stdinNode(pass, n)
			return true
		})
	}
}

// stdinNode judges one node: a call, an assignment, a declaration or a struct literal.
func stdinNode(pass *analysis.Pass, n ast.Node) {
	switch n := n.(type) {
	case *ast.CallExpr:
		stdinCall(pass, n)
	case *ast.AssignStmt:
		if len(n.Lhs) == len(n.Rhs) {
			for i, rhs := range n.Rhs {
				stdinHeld(pass, n.Lhs[i], rhs)
			}
		}
	case *ast.ValueSpec:
		if len(n.Names) == len(n.Values) {
			for i, v := range n.Values {
				stdinHeld(pass, n.Names[i], v)
			}
		}
	case *ast.CompositeLit:
		stdinLiteral(pass, n)
	}
}

// inStdinSeam is whether the package path is one of the stdin seams.
func inStdinSeam(pkgPath string) bool {
	return slices.ContainsFunc(stdinSeams, func(seam string) bool { return inTree(pkgPath, seam) })
}

// stdinTestDouble is whether the package is a test double: its last element ends in "test".
func stdinTestDouble(pkgPath string) bool {
	path := strings.TrimSuffix(strings.Fields(pkgPath + " ")[0], "_test")
	return strings.HasSuffix(path[strings.LastIndexByte(path, '/')+1:], "test")
}

// stdinCall reports a call that reads stdin, or hands it to a reader parameter of another name.
func stdinCall(pass *analysis.Pass, call *ast.CallExpr) {
	if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok && slices.Contains(stdinReadMethods, sel.Sel.Name) && isStdin(pass, sel.X) {
		if _, isMethod := pass.TypesInfo.Selections[sel]; isMethod {
			pass.Reportf(call.Pos(), "%s reads stdin outside internal/stdinread%s", sel.Sel.Name, stdinSeamFix)
			return
		}
	}
	fn := calledFunc(pass, call)
	if fn == nil || fn.Pkg() == nil {
		stdinHanded(pass, call)
		return
	}
	if at, ok := stdinReaders[[2]string{fn.Pkg().Path(), fn.Name()}]; ok {
		stdinReaderCall(pass, call, fn, at)
		return
	}
	if !inStdinSeam(fn.Pkg().Path()) {
		stdinHanded(pass, call)
	}
}

// stdinReaderCall reports a call of one of stdinReaders whose read argument (at, or any) is stdin.
func stdinReaderCall(pass *analysis.Pass, call *ast.CallExpr, fn *types.Func, at int) {
	for i, arg := range call.Args {
		if (at < 0 || i == at) && isStdin(pass, arg) {
			pass.Reportf(call.Pos(), "%s.%s reads stdin outside internal/stdinread%s", fn.Pkg().Name(), fn.Name(), stdinSeamFix)
			return
		}
	}
}

// stdinHanded reports stdin given to a parameter of a reader type under another name.
func stdinHanded(pass *analysis.Pass, call *ast.CallExpr) {
	if tv, ok := pass.TypesInfo.Types[call.Fun]; ok && tv.IsType() {
		return
	}
	sig, ok := pass.TypesInfo.TypeOf(call.Fun).(*types.Signature)
	if !ok {
		return
	}
	params := sig.Params()
	for i, arg := range call.Args {
		if params.Len() == 0 || !isStdin(pass, arg) {
			continue
		}
		param := params.At(min(i, params.Len()-1))
		if !readsBytes(param.Type()) || stdinName(param.Name()) {
			continue
		}
		pass.Reportf(arg.Pos(), "stdin handed to %s as its %s, a reader of another name, which hides its read%s", calleeName(call), orBlank(param.Name()), stdinSeamFix)
	}
}

// stdinHeld reports stdin assigned to a variable or field of another name.
func stdinHeld(pass *analysis.Pass, lhs, rhs ast.Expr) {
	if !isStdin(pass, rhs) {
		return
	}
	var name string
	switch l := ast.Unparen(lhs).(type) {
	case *ast.Ident:
		name = l.Name
	case *ast.SelectorExpr:
		if stdinSeamField(pass, l) {
			return
		}
		name = l.Sel.Name
	default:
		return
	}
	if name == "_" || stdinName(name) {
		return
	}
	pass.Reportf(lhs.Pos(), "stdin held as %s, a reader of another name, which hides its read%s", name, stdinSeamFix)
}

// stdinLiteral reports a struct literal that holds stdin in a field of another name, unless the
// struct is a seam's own.
func stdinLiteral(pass *analysis.Pass, lit *ast.CompositeLit) {
	t := pass.TypesInfo.TypeOf(lit)
	if t == nil || stdinSeamType(t) {
		return
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || stdinName(key.Name) || !isStdin(pass, kv.Value) {
			continue
		}
		pass.Reportf(kv.Key.Pos(), "stdin held as %s, a reader of another name, which hides its read%s", key.Name, stdinSeamFix)
	}
}

// stdinSeamField is whether a selected field belongs to a type of a stdin seam.
func stdinSeamField(pass *analysis.Pass, sel *ast.SelectorExpr) bool {
	s, ok := pass.TypesInfo.Selections[sel]
	return ok && s.Kind() == types.FieldVal && stdinSeamType(s.Recv())
}

// stdinSeamType is whether t (or what it points to) is a named type of a stdin seam.
func stdinSeamType(t types.Type) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	return inStdinSeam(named.Obj().Pkg().Path())
}

// isStdin is whether e is the process's stdin: os.Stdin, or a variable, parameter or field named
// stdin or Stdin whose type reads.
func isStdin(pass *analysis.Pass, e ast.Expr) bool {
	switch x := ast.Unparen(e).(type) {
	case *ast.SelectorExpr:
		if v, ok := pass.TypesInfo.Uses[x.Sel].(*types.Var); ok && v.Pkg() != nil && v.Pkg().Path() == "os" && v.Name() == "Stdin" && !v.IsField() {
			return true
		}
		s, ok := pass.TypesInfo.Selections[x]
		return ok && s.Kind() == types.FieldVal && stdinName(x.Sel.Name) && readsBytes(s.Type())
	case *ast.Ident:
		v, ok := pass.TypesInfo.Uses[x].(*types.Var)
		return ok && !v.IsField() && stdinName(v.Name()) && readsBytes(v.Type())
	}
	return false
}

// stdinName is whether a name says it is stdin.
func stdinName(name string) bool { return name == "stdin" || name == "Stdin" }

// readsBytes is whether a value of type t, or a pointer to one, has Read([]byte) (int, error).
func readsBytes(t types.Type) bool {
	sets := []*types.MethodSet{types.NewMethodSet(t)}
	if _, isPointer := t.(*types.Pointer); !isPointer && !types.IsInterface(t) {
		sets = append(sets, types.NewMethodSet(types.NewPointer(t)))
	}
	return slices.ContainsFunc(sets, func(ms *types.MethodSet) bool {
		sel := ms.Lookup(nil, "Read")
		return sel != nil && isByteRead(sel.Type())
	})
}

// isByteRead is whether a method's type is func([]byte) (int, error).
func isByteRead(t types.Type) bool {
	sig, ok := t.(*types.Signature)
	if !ok || sig.Params().Len() != 1 || sig.Results().Len() != 2 {
		return false
	}
	slice, ok := sig.Params().At(0).Type().(*types.Slice)
	return ok && types.Identical(slice.Elem(), types.Typ[types.Byte])
}

// orBlank is a parameter's name, or "unnamed parameter".
func orBlank(name string) string {
	if name == "" || name == "_" {
		return "unnamed parameter"
	}
	return name
}
