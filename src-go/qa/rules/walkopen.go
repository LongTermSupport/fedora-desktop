package rules

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// WalkOpen forbids opening an entry a directory walk found without a regular-file check guarding
// the open.
var WalkOpen = &analysis.Analyzer{
	Name: "project_walk_open",
	Doc: `project_walk_open flags an open of a name a directory walk found, unless a regular-file
check of that entry guards the open.

A walk is os.ReadDir, fs.ReadDir, filepath.Glob, fs.Glob, (*os.File).ReadDir, Readdir or
Readdirnames, and the callback of filepath.WalkDir, fs.WalkDir and filepath.Walk (its path and
entry parameters). What a walk returns is followed through every value built from it (a name
joined to a directory, a slice appended to, the value of a range over it, a closure that captures
it). What a walked file says is not followed: a byte slice, and the result of a ReadFile,
parser.ParseFile, io.ReadAll or a function of the package that returns one, names no entry the
walk found. An open
is os.Open, os.OpenFile, os.ReadFile, ioutil.ReadFile, fs.ReadFile, an fs.FS's Open,
parser.ParseFile given no source, and a call of a function of the same package that opens the
parameter it is given unguarded. os.OpenFile with O_DIRECTORY is no open. With O_NONBLOCK the
open cannot wait but a read can (a named pipe with no writer reads as empty, one with a silent
writer still blocks, a device reads without end), so the open file's first use other than Stat,
Close, Fd, Name or SyscallConn is judged as the open: a method of it, or a call it is handed to
(a function of the package only if that reads the file unchecked).

A check is IsRegular() of the entry's mode (d.Type().IsRegular(), info.Mode().IsRegular(), on
a value built from the walked name, such as the FileInfo of os.Stat of it). It guards an open in
the branch it holds (if check { open }, also with more terms joined by &&), or an open after an
if whose failed check leaves the block (if !check { continue, break, return or panic }, also with
more terms joined by ||). A suffix, a name pattern, !IsDir() or a check that only warns and falls
through is no check.

A walk lists named pipes, sockets and devices as well as files, and opening a named pipe for
reading blocks until something writes to it: play_text in helpers/play_ledger/group_vars.py
opened every name os.walk listed and changed_plays hung on a named pipe an old plan run left
(fedora-desktop#191; the Go half, fedora-desktop#195).

Not caught: a walk callback that is a named function rather than a literal, an open through a
function of another package, an interface method or a function value, an open inside the
condition that checks (check && open), a check through a helper, and a file opened O_NONBLOCK
that is returned or stored before it is read.

How to fix: skip the entry unless its mode is regular (if !d.Type().IsRegular() { continue }),
or refuse it where a non-regular entry means something is wrong; never by moving the open out
of sight.`,
	Run:        perPass(runWalkOpen),
	ResultType: noResultType,
}

const walkOpenMsg = "%s opens an entry a directory walk found without a regular-file check guarding it, and opening a " +
	"named pipe blocks for ever (project_walk_open): skip the entry, or refuse it, unless its Type() or Mode() IsRegular()"

const walkReadMsg = "%s reads an entry a directory walk found, opened O_NONBLOCK, with no regular-file check of the open " +
	"file: a named pipe with no writer reads as empty, one with a silent writer still blocks, and a device reads without " +
	"end (project_walk_open): refuse it unless its Stat().Mode().IsRegular()"

// The import paths and names the walk and open tables share.
const (
	ioFSPkg      = "io/fs"
	filepathPkg  = "path/filepath"
	ioutilPkg    = "io/ioutil"
	readDirName  = "ReadDir"
	readFileName = "ReadFile"
)

// walkSources are the functions that list a directory's entries.
var walkSources = []struct{ pkg, name string }{
	{"os", readDirName}, {ioFSPkg, readDirName}, {filepathPkg, "Glob"}, {ioFSPkg, "Glob"}, {ioutilPkg, readDirName},
}

// walkFileMethods are the methods of *os.File that list a directory.
var walkFileMethods = []string{readDirName, "Readdir", "Readdirnames"}

// walkCallbacks are the walks that hand each entry to a function, with the index of that argument.
var walkCallbacks = []struct {
	pkg, name string
	arg       int
}{
	{filepathPkg, "WalkDir", 1}, {filepathPkg, "Walk", 1}, {ioFSPkg, "WalkDir", 2},
}

// openers are the functions that open a name, with the index of the name argument.
var openers = []struct {
	pkg, name string
	arg       int
}{
	{"os", "Open", 0}, {"os", "OpenFile", 0}, {"os", readFileName, 0}, {ioutilPkg, readFileName, 0}, {ioFSPkg, readFileName, 1},
}

func runWalkOpen(pass *analysis.Pass) {
	sum := summarise(pass)
	for _, f := range pass.Files {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			s := &walkScan{pass: pass, sum: sum, taint: map[types.Object]bool{}, report: true}
			s.block(fd.Body.List, false)
		}
	}
}

// walkSummary is what the package's own functions do with walked names: which parameters each
// opens unguarded, and which return a value built from a listing (a call of one is a walk).
type walkSummary struct {
	opens     map[*types.Func][]bool
	recvOpens map[*types.Func]bool
	lists     map[*types.Func]bool
	// reads are the *os.File parameters each function reads with no regular-file check of it.
	reads map[*types.Func][]bool
	// content are the functions that return what a file says (a call of one is a content read).
	content map[*types.Func]bool
}

// summarise finds the package's openers, readers, listers and content readers to a fixed point,
// so a function that hands its parameter to another opener, or returns what another lister
// returned, is one too.
func summarise(pass *analysis.Pass) walkSummary {
	sum := walkSummary{
		opens: map[*types.Func][]bool{}, recvOpens: map[*types.Func]bool{}, lists: map[*types.Func]bool{},
		reads: map[*types.Func][]bool{}, content: map[*types.Func]bool{},
	}
	decls := funcDecls(pass)
	for changed := true; changed; {
		changed = false
		for _, fd := range decls {
			fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func)
			if !ok {
				continue
			}
			changed = sum.markContent(pass, fd, fn) || changed
			changed = sum.markLister(pass, fd, fn) || changed
			changed = sum.markOpeners(pass, fd, fn) || changed
			changed = sum.markReaders(pass, fd, fn) || changed
			changed = sum.markReceiver(pass, fd, fn) || changed
		}
	}
	return sum
}

// markContent marks a function that returns, as a value that is not an error, what a file it
// read says, and says whether that was new.
func (sum walkSummary) markContent(pass *analysis.Pass, fd *ast.FuncDecl, fn *types.Func) bool {
	if sum.content[fn] {
		return false
	}
	c := contentScan{pass: pass, sum: sum, held: map[types.Object]bool{}}
	ast.Inspect(fd.Body, c.node)
	if c.returns {
		sum.content[fn] = true
	}
	return c.returns
}

// contentScan follows what a function read out of a file to its returns.
type contentScan struct {
	pass    *analysis.Pass
	sum     walkSummary
	held    map[types.Object]bool
	returns bool
}

func (c *contentScan) node(n ast.Node) bool {
	switch node := n.(type) {
	case *ast.FuncLit:
		return false
	case *ast.AssignStmt:
		if slices.ContainsFunc(node.Rhs, c.carries) {
			for _, lhs := range node.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && c.pass.TypesInfo.ObjectOf(id) != nil {
					c.held[c.pass.TypesInfo.ObjectOf(id)] = true
				}
			}
		}
	case *ast.ReturnStmt:
		for _, r := range node.Results {
			tv, ok := c.pass.TypesInfo.Types[r]
			c.returns = c.returns || ok && tv.Type != nil && !isErrorType(tv.Type) && c.carries(r)
		}
	}
	return true
}

// carries is whether e holds what a file says: a content read, or a variable assigned one.
func (c *contentScan) carries(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			found = found || isContentCall(c.pass, c.sum, node)
		case *ast.Ident:
			found = found || c.held[c.pass.TypesInfo.ObjectOf(node)]
		}
		return !found
	})
	return found
}

// markReaders marks each *os.File parameter of fn that its body reads with no regular-file check
// of it, and says whether any was new.
func (sum walkSummary) markReaders(pass *analysis.Pass, fd *ast.FuncDecl, fn *types.Func) bool {
	params := paramObjects(pass, fd)
	if sum.reads[fn] == nil {
		sum.reads[fn] = make([]bool, len(params))
	}
	changed := false
	checkable := entryParams(pass, fd)
	base := &walkScan{pass: pass, sum: sum, taint: map[types.Object]bool{}, checkable: checkable}
	base.block(fd.Body.List, false)
	for i, param := range params {
		if sum.reads[fn][i] || param == nil || !isOSFile(param.Type()) {
			continue
		}
		s := &walkScan{
			pass: pass, sum: sum, taint: map[types.Object]bool{param: true},
			pending: map[types.Object]bool{param: true}, checkable: checkable,
		}
		s.block(fd.Body.List, false)
		if s.addsTo(base) {
			sum.reads[fn][i] = true
			changed = true
		}
	}
	return changed
}

// isOSFile is whether t is *os.File.
func isOSFile(t types.Type) bool {
	return types.TypeString(t, nil) == "*os.File"
}

// funcDecls is every function declaration of the package that has a body.
func funcDecls(pass *analysis.Pass) []*ast.FuncDecl {
	var decls []*ast.FuncDecl
	for _, f := range pass.Files {
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Body != nil {
				decls = append(decls, fd)
			}
		}
	}
	return decls
}

// markLister marks a function that returns a value built from a listing, and says whether that
// was new.
func (sum walkSummary) markLister(pass *analysis.Pass, fd *ast.FuncDecl, fn *types.Func) bool {
	if sum.lists[fn] {
		return false
	}
	s := &walkScan{pass: pass, sum: sum, taint: map[types.Object]bool{}}
	s.block(fd.Body.List, false)
	if s.returns {
		sum.lists[fn] = true
	}
	return s.returns
}

// markOpeners marks each parameter of fn that its body opens unguarded, and says whether any was new.
func (sum walkSummary) markOpeners(pass *analysis.Pass, fd *ast.FuncDecl, fn *types.Func) bool {
	params := paramObjects(pass, fd)
	if sum.opens[fn] == nil {
		sum.opens[fn] = make([]bool, len(params))
	}
	changed := false
	// The body's own walks are found where it opens them; a parameter opens only what seeding it
	// adds to those.
	checkable := entryParams(pass, fd)
	base := &walkScan{pass: pass, sum: sum, taint: map[types.Object]bool{}, checkable: checkable}
	base.block(fd.Body.List, false)
	for i, param := range params {
		if sum.opens[fn][i] || param == nil {
			continue
		}
		s := &walkScan{pass: pass, sum: sum, taint: map[types.Object]bool{param: true}, checkable: checkable}
		s.block(fd.Body.List, false)
		if s.addsTo(base) {
			sum.opens[fn][i] = true
			changed = true
		}
	}
	return changed
}

// markReceiver marks a method whose body opens its receiver (a name or a record built from a
// walk) unguarded, and says whether that was new.
func (sum walkSummary) markReceiver(pass *analysis.Pass, fd *ast.FuncDecl, fn *types.Func) bool {
	if sum.recvOpens[fn] || fd.Recv == nil || len(fd.Recv.List) == 0 || len(fd.Recv.List[0].Names) == 0 {
		return false
	}
	recv := pass.TypesInfo.Defs[fd.Recv.List[0].Names[0]]
	if recv == nil {
		return false
	}
	checkable := entryParams(pass, fd)
	base := &walkScan{pass: pass, sum: sum, taint: map[types.Object]bool{}, checkable: checkable}
	base.block(fd.Body.List, false)
	s := &walkScan{pass: pass, sum: sum, taint: map[types.Object]bool{recv: true}, checkable: checkable}
	s.block(fd.Body.List, false)
	if !s.addsTo(base) {
		return false
	}
	sum.recvOpens[fn] = true
	return true
}

// addsTo is whether this scan found an open the scan base did not.
func (s *walkScan) addsTo(base *walkScan) bool {
	for pos := range s.opened {
		if !base.opened[pos] {
			return true
		}
	}
	return false
}

// entryParams is the receiver and parameters of a function: a caller hands it an entry with its
// name, so a check of any of them, in its body, is a check of what the caller walked.
func entryParams(pass *analysis.Pass, fd *ast.FuncDecl) map[types.Object]bool {
	objs := map[types.Object]bool{}
	for _, obj := range paramObjects(pass, fd) {
		if obj != nil {
			objs[obj] = true
		}
	}
	if fd.Recv != nil && len(fd.Recv.List) > 0 && len(fd.Recv.List[0].Names) > 0 {
		if recv := pass.TypesInfo.Defs[fd.Recv.List[0].Names[0]]; recv != nil {
			objs[recv] = true
		}
	}
	return objs
}

// paramObjects is the object of each parameter of the declaration, nil for an unnamed one.
func paramObjects(pass *analysis.Pass, fd *ast.FuncDecl) []types.Object {
	var params []types.Object
	for _, field := range fd.Type.Params.List {
		if len(field.Names) == 0 {
			params = append(params, nil)
			continue
		}
		for _, name := range field.Names {
			params = append(params, pass.TypesInfo.Defs[name])
		}
	}
	return params
}

// walkScan follows walked names through one function body.
type walkScan struct {
	pass  *analysis.Pass
	sum   walkSummary
	taint map[types.Object]bool
	// listings are the tainted variables that hold a whole listing, not one entry of it.
	listings map[types.Object]bool
	report   bool
	// checkable are the parameters whose check a summary counts (entryParams).
	checkable map[types.Object]bool
	// pending are the files opened O_NONBLOCK from a walked name: a read of one is the open.
	pending map[types.Object]bool
	// opened is where each unguarded open was found.
	opened  map[token.Pos]bool
	returns bool
}

// block scans a statement list; a failed check that leaves guards the statements after it.
func (s *walkScan) block(list []ast.Stmt, guarded bool) {
	for _, stmt := range list {
		s.stmt(stmt, guarded)
		if ifs, ok := stmt.(*ast.IfStmt); ok && s.leavingCheck(ifs) {
			guarded = true
		}
	}
}

func (s *walkScan) stmt(stmt ast.Stmt, guarded bool) {
	switch st := stmt.(type) {
	case *ast.AssignStmt:
		s.exprs(st.Rhs, guarded)
		if s.carriesUnchecked(st.Rhs, guarded) {
			listing := s.anyListing(st.Rhs)
			for _, lhs := range st.Lhs {
				s.taintRoot(lhs)
				s.markListing(lhs, listing)
			}
		}
		s.markPending(st.Lhs, st.Rhs, guarded)
	case *ast.DeclStmt:
		s.declStmt(st, guarded)
	case *ast.RangeStmt:
		s.rangeStmt(st, guarded)
	case *ast.IfStmt:
		s.ifStmt(st, guarded)
	case *ast.BlockStmt:
		s.block(st.List, guarded)
	case *ast.ReturnStmt:
		s.exprs(st.Results, guarded)
		for _, r := range st.Results {
			if s.isNameCarrier(r) && s.walked(r) {
				s.returns = true
			}
		}
	default:
		s.compound(stmt, guarded)
	}
}

// rangeStmt marks the variables of a range over a listing and scans its body.
func (s *walkScan) rangeStmt(st *ast.RangeStmt, guarded bool) {
	s.expr(st.X, guarded)
	if s.walked(st.X) {
		s.taintRoot(st.Key)
		s.taintRoot(st.Value)
	}
	s.block(st.Body.List, guarded)
}

// ifStmt scans an if; a check in its condition guards its body.
func (s *walkScan) ifStmt(st *ast.IfStmt, guarded bool) {
	s.optional(st.Init, guarded)
	s.expr(st.Cond, guarded)
	s.block(st.Body.List, guarded || s.positiveCheck(st.Cond))
	s.optional(st.Else, guarded)
}

// compound scans the statements that only nest others or hold expressions.
func (s *walkScan) compound(stmt ast.Stmt, guarded bool) {
	switch st := stmt.(type) {
	case *ast.ForStmt:
		s.optional(st.Init, guarded)
		s.expr(st.Cond, guarded)
		s.optional(st.Post, guarded)
		s.block(st.Body.List, guarded)
	case *ast.SwitchStmt:
		s.optional(st.Init, guarded)
		s.expr(st.Tag, guarded)
		if st.Tag != nil {
			s.block(st.Body.List, guarded)
			return
		}
		for _, clause := range st.Body.List {
			cc, ok := clause.(*ast.CaseClause)
			if !ok {
				continue
			}
			s.exprs(cc.List, guarded)
			s.block(cc.Body, guarded || s.positiveCase(cc))
		}
	case *ast.TypeSwitchStmt:
		s.optional(st.Init, guarded)
		s.stmt(st.Assign, guarded)
		s.block(st.Body.List, guarded)
	case *ast.SelectStmt:
		s.block(st.Body.List, guarded)
	case *ast.CaseClause:
		s.exprs(st.List, guarded)
		s.block(st.Body, guarded)
	case *ast.CommClause:
		s.optional(st.Comm, guarded)
		s.block(st.Body, guarded)
	case *ast.LabeledStmt:
		s.stmt(st.Stmt, guarded)
	default:
		ast.Inspect(stmt, func(n ast.Node) bool {
			if e, ok := n.(ast.Expr); ok {
				s.expr(e, guarded)
				return false
			}
			return true
		})
	}
}

func (s *walkScan) optional(stmt ast.Stmt, guarded bool) {
	if stmt != nil {
		s.stmt(stmt, guarded)
	}
}

func (s *walkScan) declStmt(st *ast.DeclStmt, guarded bool) {
	gen, ok := st.Decl.(*ast.GenDecl)
	if !ok {
		return
	}
	for _, spec := range gen.Specs {
		vs, isValue := spec.(*ast.ValueSpec)
		if !isValue {
			continue
		}
		s.exprs(vs.Values, guarded)
		if s.carriesUnchecked(vs.Values, guarded) {
			listing := s.anyListing(vs.Values)
			for _, name := range vs.Names {
				s.taintRoot(name)
				s.markListing(name, listing)
			}
		}
		lhs := make([]ast.Expr, len(vs.Names))
		for i, name := range vs.Names {
			lhs[i] = name
		}
		s.markPending(lhs, vs.Values, guarded)
	}
}

// markPending records the file an unguarded O_NONBLOCK open of a walked name assigns: the open
// itself cannot wait, but a read of what it opened can, so the read is judged as the open.
func (s *walkScan) markPending(lhs, rhs []ast.Expr, guarded bool) {
	if guarded || len(rhs) != 1 || len(lhs) == 0 {
		return
	}
	call, ok := ast.Unparen(rhs[0]).(*ast.CallExpr)
	if !ok || !isPkgFunc(calledFunc(s.pass, call), "os", "OpenFile") || len(call.Args) < 2 {
		return
	}
	if !hasFlag(s.pass, call.Args[1], "O_NONBLOCK") || hasFlag(s.pass, call.Args[1], "O_DIRECTORY") || !s.walked(call.Args[0]) {
		return
	}
	obj := s.rootObject(lhs[0])
	if obj == nil {
		return
	}
	if s.pending == nil {
		s.pending = map[types.Object]bool{}
	}
	s.pending[obj] = true
}

// fileOnlyMethods are the methods of *os.File that do not read or write what it opened.
var fileOnlyMethods = []string{"Stat", "Close", "Fd", "Name", "SyscallConn"}

// pendingRead is the name of a call that reads a pending file (a method of it, or a call it is
// handed to), or "".
func (s *walkScan) pendingRead(call *ast.CallExpr) string {
	if len(s.pending) == 0 {
		return ""
	}
	fn := calledFunc(s.pass, call)
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok && s.isPending(sel.X) && !slices.Contains(fileOnlyMethods, sel.Sel.Name) {
		return sel.Sel.Name
	}
	for i, arg := range call.Args {
		if !s.isPending(arg) {
			continue
		}
		if fn == nil {
			return calleeName(call)
		}
		if fn.Pkg() == s.pass.Pkg {
			// The package's own functions start as readers of nothing; the fixed point marks them.
			if reads := s.sum.reads[fn]; i < len(reads) && reads[i] {
				return fn.Name()
			}
			continue
		}
		if fn.Pkg() != nil {
			return fn.Pkg().Name() + "." + fn.Name()
		}
		return fn.Name()
	}
	return ""
}

func (s *walkScan) isPending(e ast.Expr) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	return ok && s.pending[s.pass.TypesInfo.ObjectOf(id)]
}

func (s *walkScan) exprs(list []ast.Expr, guarded bool) {
	for _, e := range list {
		s.expr(e, guarded)
	}
}

// expr reports the opens of walked names in an expression and scans the function literals in it.
func (s *walkScan) expr(e ast.Expr, guarded bool) {
	if e == nil {
		return
	}
	ast.Inspect(e, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncLit:
			s.block(node.Body.List, guarded)
			return false
		case *ast.CallExpr:
			s.callbackParams(node)
			s.call(node, guarded)
		}
		return true
	})
}

// callbackParams taints the path and entry parameters of a walk's callback literal.
func (s *walkScan) callbackParams(call *ast.CallExpr) {
	fn := calledFunc(s.pass, call)
	for _, cb := range walkCallbacks {
		if !isPkgFunc(fn, cb.pkg, cb.name) || len(call.Args) <= cb.arg {
			continue
		}
		lit, ok := call.Args[cb.arg].(*ast.FuncLit)
		if !ok {
			return
		}
		for i, field := range lit.Type.Params.List {
			if i > 1 {
				break
			}
			for _, name := range field.Names {
				s.taintRoot(name)
			}
		}
	}
}

// call reports an open of a walked name that no check guards.
func (s *walkScan) call(call *ast.CallExpr, guarded bool) {
	if guarded {
		return
	}
	if name := s.pendingRead(call); name != "" {
		s.found(call, walkReadMsg, name)
		return
	}
	name, arg := s.openedArg(call)
	if arg == nil || !s.walked(arg) {
		return
	}
	s.found(call, walkOpenMsg, name)
}

// found records an unguarded open (or read) at call, and reports it when this scan reports.
func (s *walkScan) found(call *ast.CallExpr, msg, name string) {
	if s.opened == nil {
		s.opened = map[token.Pos]bool{}
	}
	s.opened[call.Pos()] = true
	if s.report {
		s.pass.Reportf(call.Pos(), msg, name)
	}
}

// openedArg is the name of the opener a call calls and the argument it opens, or nil.
func (s *walkScan) openedArg(call *ast.CallExpr) (string, ast.Expr) {
	fn := calledFunc(s.pass, call)
	if fn == nil {
		return "", nil
	}
	if name, arg := stdOpenedArg(s.pass, fn, call); arg != nil {
		return name, arg
	}
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok && s.sum.recvOpens[fn] && s.walked(sel.X) {
		return fn.Name(), sel.X
	}
	for i, opens := range s.sum.opens[fn] {
		if opens && i < len(call.Args) && s.walked(call.Args[i]) {
			return fn.Name(), call.Args[i]
		}
	}
	return "", nil
}

// stdOpenedArg is the name of the standard library opener fn and the argument it opens, or nil.
func stdOpenedArg(pass *analysis.Pass, fn *types.Func, call *ast.CallExpr) (string, ast.Expr) {
	for _, o := range openers {
		if isPkgFunc(fn, o.pkg, o.name) && len(call.Args) > o.arg {
			if o.name == "OpenFile" && len(call.Args) > 1 && cannotWait(pass, call.Args[1]) {
				return "", nil
			}
			return fn.Pkg().Name() + "." + fn.Name(), call.Args[o.arg]
		}
	}
	if isPkgFunc(fn, "go/parser", "ParseFile") && len(call.Args) > 2 && isNil(pass, call.Args[2]) {
		return "parser.ParseFile", call.Args[1]
	}
	if isFSOpen(fn) && len(call.Args) == 1 {
		return fn.Name(), call.Args[0]
	}
	return "", nil
}

// cannotWait is whether an open's flags keep the open itself from waiting on a named pipe:
// O_DIRECTORY refuses what is not a directory first, and O_NONBLOCK returns at once from a pipe
// with no writer. A read of what O_NONBLOCK opened can still wait or lie, so markPending judges
// that read as the open.
func cannotWait(pass *analysis.Pass, flags ast.Expr) bool {
	return hasFlag(pass, flags, "O_NONBLOCK") || hasFlag(pass, flags, "O_DIRECTORY")
}

// hasFlag is whether an open's flags name the syscall or unix flag name.
func hasFlag(pass *analysis.Pass, flags ast.Expr, name string) bool {
	found := false
	ast.Inspect(flags, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || found {
			return !found
		}
		obj := pass.TypesInfo.Uses[sel.Sel]
		if obj != nil && obj.Pkg() != nil && (obj.Pkg().Path() == "syscall" || obj.Pkg().Path() == "golang.org/x/sys/unix") {
			found = obj.Name() == name
		}
		return !found
	})
	return found
}

// isContentCall is whether a call returns what a file says, which names no entry a walk found:
// a ReadFile, a parsed source file, io.ReadAll, or a function of the package that returns one.
func isContentCall(pass *analysis.Pass, sum walkSummary, call *ast.CallExpr) bool {
	fn := calledFunc(pass, call)
	if fn == nil {
		return false
	}
	if sum.content[fn] {
		return true
	}
	for _, o := range openers {
		if o.name == readFileName && isPkgFunc(fn, o.pkg, o.name) {
			return true
		}
	}
	return isPkgFunc(fn, "go/parser", "ParseFile") || isPkgFunc(fn, "io", "ReadAll")
}

// isFSOpen is whether f is an Open method returning an fs.File (an fs.FS's).
func isFSOpen(f *types.Func) bool {
	sig, ok := f.Type().(*types.Signature)
	if !ok || sig.Recv() == nil || f.Name() != "Open" || sig.Results().Len() == 0 {
		return false
	}
	return types.TypeString(sig.Results().At(0).Type(), nil) == "io/fs.File"
}

// isNameCarrier is whether a returned value can be the names a listing gave: a string or a slice
// (of names, entries or records built from them). A struct, a pointer, a number or an error is
// not followed, so a constructor that lists a directory on the way does not mark what it returns.
func (s *walkScan) isNameCarrier(e ast.Expr) bool {
	tv, ok := s.pass.TypesInfo.Types[e]
	if !ok || tv.Type == nil {
		return false
	}
	switch t := tv.Type.Underlying().(type) {
	case *types.Slice:
		return true
	case *types.Basic:
		return t.Info()&types.IsString != 0
	}
	return false
}

// walked is whether the expression lists a directory or names a value built from a listing.
func (s *walkScan) walked(e ast.Expr) bool {
	if e == nil {
		return false
	}
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if found {
			return false
		}
		switch node := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.Ident:
			if obj := s.pass.TypesInfo.ObjectOf(node); obj != nil && s.taint[obj] {
				found = true
			}
		case *ast.CallExpr:
			if s.isWalkSource(node) {
				found = true
			} else if isContentCall(s.pass, s.sum, node) {
				// What a walked file says is content: the names in it are not the walk's.
				return false
			}
		}
		return !found
	})
	return found
}

func (s *walkScan) anyWalked(list []ast.Expr) bool {
	return slices.ContainsFunc(list, s.walked)
}

// carriesUnchecked is whether an assignment of list carries walked names no check vouches for.
// Where a check guards it, a name taken from the entry checked is a regular file's, so the check
// travels with it; but a listing, taken there or before, is entries no check has seen.
func (s *walkScan) carriesUnchecked(list []ast.Expr, guarded bool) bool {
	if !guarded {
		return s.anyWalked(list)
	}
	return s.anyListing(list)
}

// anyListing is whether a value in list is a listing itself (a walk source's result, or a
// variable holding one), not one entry of it.
func (s *walkScan) anyListing(list []ast.Expr) bool {
	return slices.ContainsFunc(list, s.listingOf)
}

func (s *walkScan) listingOf(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if _, isLit := n.(*ast.FuncLit); isLit || found {
			return false
		}
		switch node := n.(type) {
		case *ast.CallExpr:
			found = s.isWalkSource(node)
		case *ast.Ident:
			obj := s.pass.TypesInfo.ObjectOf(node)
			found = obj != nil && s.listings[obj]
		}
		return !found
	})
	return found
}

// markListing records that the variable an assignment target names holds a listing.
func (s *walkScan) markListing(e ast.Expr, listing bool) {
	obj := s.rootObject(e)
	if !listing || obj == nil {
		return
	}
	if s.listings == nil {
		s.listings = map[types.Object]bool{}
	}
	s.listings[obj] = true
}

func (s *walkScan) isWalkSource(call *ast.CallExpr) bool {
	fn := calledFunc(s.pass, call)
	if fn == nil {
		return false
	}
	if s.sum.lists[fn] {
		return true
	}
	for _, src := range walkSources {
		if isPkgFunc(fn, src.pkg, src.name) {
			return true
		}
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil || fn.Pkg() == nil || fn.Pkg().Path() != "os" {
		return false
	}
	for _, m := range walkFileMethods {
		if fn.Name() == m {
			return true
		}
	}
	return false
}

// taintRoot marks the variable (or the field) an assignment target or range variable names.
func (s *walkScan) taintRoot(e ast.Expr) {
	if obj := s.rootObject(e); obj != nil && !isBytes(obj.Type()) {
		s.taint[obj] = true
	}
}

// isBytes is whether t is a byte slice: what a file says, never a name, whichever reader (one the
// rule cannot see into too) returned it.
func isBytes(t types.Type) bool {
	sl, ok := t.Underlying().(*types.Slice)
	if !ok {
		return false
	}
	b, ok := sl.Elem().Underlying().(*types.Basic)
	return ok && b.Kind() == types.Byte
}

// rootObject is the variable (or the field) an assignment target or range variable names, nil
// for the blank identifier, an error, or a target that names none.
func (s *walkScan) rootObject(e ast.Expr) types.Object {
	for e != nil {
		switch node := e.(type) {
		case *ast.Ident:
			if node.Name == "_" {
				return nil
			}
			if obj := s.pass.TypesInfo.ObjectOf(node); obj != nil && !isErrorType(obj.Type()) {
				return obj
			}
			return nil
		case *ast.IndexExpr:
			e = node.X
		case *ast.SelectorExpr:
			// A field assigned a walked value marks the field, not the whole value holding it.
			e = node.Sel
		case *ast.StarExpr:
			e = node.X
		case *ast.ParenExpr:
			e = node.X
		default:
			return nil
		}
	}
	return nil
}

// positiveCheck is whether a condition holds only when a walked entry is regular: a check, or a
// conjunction with one among its terms.
func (s *walkScan) positiveCheck(cond ast.Expr) bool {
	return slices.ContainsFunc(splitBinary(cond, token.LAND), s.regularCheck)
}

// positiveCase is whether a clause of a switch with no tag runs only when a walked entry is
// regular: every expression it lists is a positive check (the default clause lists none).
func (s *walkScan) positiveCase(cc *ast.CaseClause) bool {
	return len(cc.List) > 0 && !slices.ContainsFunc(cc.List, func(e ast.Expr) bool { return !s.positiveCheck(e) })
}

// leavingCheck is whether an if leaves its block whenever a walked entry is not regular: a
// negated check, or a disjunction with one among its terms, whose body ends by leaving.
func (s *walkScan) leavingCheck(ifs *ast.IfStmt) bool {
	if !leaves(ifs.Body) {
		return false
	}
	for _, term := range splitBinary(ifs.Cond, token.LOR) {
		not, ok := ast.Unparen(term).(*ast.UnaryExpr)
		if ok && not.Op == token.NOT && s.positiveCheck(not.X) {
			return true
		}
	}
	return false
}

// regularCheck is whether e is X.IsRegular() on the mode of a walked value.
func (s *walkScan) regularCheck(e ast.Expr) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "IsRegular" {
		return false
	}
	return s.walked(sel.X) || s.mentionsCheckable(sel.X)
}

// mentionsCheckable is whether e names a parameter a summary may take a check of.
func (s *walkScan) mentionsCheckable(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && s.checkable[s.pass.TypesInfo.ObjectOf(id)] {
			found = true
		}
		return !found
	})
	return found
}

// splitBinary is the terms of a chain of one boolean operator.
func splitBinary(e ast.Expr, op token.Token) []ast.Expr {
	bin, ok := ast.Unparen(e).(*ast.BinaryExpr)
	if !ok || bin.Op != op {
		return []ast.Expr{e}
	}
	return append(splitBinary(bin.X, op), splitBinary(bin.Y, op)...)
}

// leaves is whether a block's last statement leaves it: a return, continue, break or goto, or a
// call of panic, os.Exit, log.Fatal* or a test's Fatal* or Skip*.
func leaves(body *ast.BlockStmt) bool {
	if len(body.List) == 0 {
		return false
	}
	switch last := body.List[len(body.List)-1].(type) {
	case *ast.ReturnStmt, *ast.BranchStmt:
		return true
	case *ast.ExprStmt:
		call, ok := last.X.(*ast.CallExpr)
		if !ok {
			return false
		}
		name := calleeName(call)
		if i := strings.LastIndexByte(name, '.'); i >= 0 {
			name = name[i+1:]
		}
		switch name {
		case "panic", "Exit", "Fatal", "Fatalf", "Fatalln", "FailNow", "Skip", "Skipf", "SkipNow":
			return true
		}
	}
	return false
}
