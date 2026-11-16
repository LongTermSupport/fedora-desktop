package rules

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// TestConnRead forbids a test's read of a connection, or accept on a listener, that nothing bounds.
var TestConnRead = &analysis.Analyzer{
	Name: "project_test_conn_read",
	Doc: `project_test_conn_read flags, in a _test.go file or a package whose name starts with
"fake" (code only tests run), a read that blocks until a network peer acts: a Read on a value
of a connection's shape (methods Read, SetReadDeadline and RemoteAddr: net.Conn,
*net.TCPConn, *net.UnixConn, *tls.Conn, a net.Pipe end), an Accept, AcceptTCP or AcceptUnix on
a value of a listener's shape (that method returning such a connection, and Addr), or such a
connection handed to a parameter that is a reader but not a connection (io.ReadAll,
io.ReadFull, io.Copy's source, bufio.NewReader, bufio.NewScanner, json.NewDecoder, a helper
taking an io.Reader). A read in a goroutine is judged too: the test that waits for the
goroutine, or a cleanup that does, blocks as long as the read does.

The read is bounded, and not flagged, when the function declaration holding it (its function
literals included) does one of these to the same expression: before the read, calls
testconn.Bound on it, sets a deadline on it (SetDeadline, SetReadDeadline), or arms
time.AfterFunc with a function literal that closes it; anywhere, closes it in a select case
that receives from a timer (time.After, a Timer's or Ticker's C); or, for a read in a go
statement, closes it in a function literal handed to Cleanup (an accept loop the test's end
stops). An accept on a listener field x.f is bounded too when x's type has a Close that closes
f and the package hands that Close to a Cleanup (a fake started with the test's TB): the loop
ends with the test. Only an accept: a read a test body waits on blocks before any cleanup runs,
so a conn closed by a registered Close is still judged.

A read with no bound ends only when the peer writes or closes. When the code under test never
does (the defect the test exists to catch), the test blocks until the package's timeout:
tlsterm's closed(c) called c.Read and CI hung for 30 minutes (fedora-desktop#300), with a
timeout panic that names no assertion.

Not caught: a read in a non-test helper whose package name does not start with "fake", a
bound set in another function (a constructor) or on another expression naming the same
connection, a read through a wrapper whose own type is not of a connection's shape
(wire.Conn's Recv, a decoder over a bufio.Reader built elsewhere), and a goroutine's read that
only a cleanup's close ends while the test body waits for that goroutine.

How to fix: call testconn.Bound(t, c) in the function that waits on c, before the wait (tests
read no wall clock, so a deadline is not available to them); or run the read in a goroutine and
wait for it in a select whose timer case closes the connection (tlsterm's awaitClose).`,
	Run:        perPass(runTestConnRead),
	ResultType: noResultType,
}

const connReadMessage = "unbounded connection read (project_test_conn_read): nothing bounds this wait on the peer; call testconn.Bound(t, <it>) first, or close it from a timer case of a select"

func runTestConnRead(pass *analysis.Pass) {
	owned := cleanupOwners(pass)
	for _, f := range pass.Files {
		if !isTestFile(pass, f) && !strings.HasPrefix(pass.Pkg.Name(), "fake") {
			continue
		}
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
				checkConnReads(pass, fn.Body, owned)
			}
		}
	}
}

// checkConnReads reports every unbounded read in body.
func checkConnReads(pass *analysis.Pass, body *ast.BlockStmt, owned map[*types.TypeName]map[string]bool) {
	var goStmts []*ast.GoStmt
	ast.Inspect(body, func(n ast.Node) bool {
		if g, ok := n.(*ast.GoStmt); ok {
			goStmts = append(goStmts, g)
		}
		return true
	})
	inGo := func(pos token.Pos) bool {
		for _, g := range goStmts {
			if g.Pos() <= pos && pos < g.End() {
				return true
			}
		}
		return false
	}
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			read, at := connReadOf(pass, call)
			if read != nil && !bounded(pass, body, read, at) && !(inGo(at) && closedInCleanup(body, types.ExprString(read))) && !ownerCleanedUp(pass, call, read, owned) {
				pass.Reportf(call.Pos(), "%s", connReadMessage)
			}
		}
		return true
	})
}

// connReadOf is the expression a call reads from, when the call is a read of a connection, an
// accept on a listener, or a connection handed on as a reader, and where it is.
func connReadOf(pass *analysis.Pass, call *ast.CallExpr) (ast.Expr, token.Pos) {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		if fn, isFunc := pass.TypesInfo.Uses[sel.Sel].(*types.Func); isFunc && fn.Signature().Recv() != nil {
			recv := pass.TypesInfo.TypeOf(sel.X)
			switch {
			case sel.Sel.Name == "Read" && connShaped(recv):
				return sel.X, call.Pos()
			case strings.HasPrefix(sel.Sel.Name, "Accept") && listenerShaped(recv, sel.Sel.Name):
				return sel.X, call.Pos()
			}
		}
	}
	sig, ok := pass.TypesInfo.TypeOf(call.Fun).(*types.Signature)
	if !ok {
		return nil, token.NoPos
	}
	for i, arg := range call.Args {
		if param := paramAt(sig, i); param != nil && readerNotConn(param) && connShaped(pass.TypesInfo.TypeOf(arg)) {
			return arg, call.Pos()
		}
	}
	return nil, token.NoPos
}

// paramAt is the type of the parameter argument i is passed to, the variadic element for the rest.
func paramAt(sig *types.Signature, i int) types.Type {
	params := sig.Params()
	switch {
	case i < params.Len()-1 || i < params.Len() && !sig.Variadic():
		return params.At(i).Type()
	case sig.Variadic() && params.Len() > 0:
		if s, ok := params.At(params.Len() - 1).Type().(*types.Slice); ok {
			return s.Elem()
		}
	}
	return nil
}

// connShaped is whether t has a connection's methods: Read, SetReadDeadline and RemoteAddr.
func connShaped(t types.Type) bool {
	return t != nil && methodNamed(t, "Read") != nil && methodNamed(t, "SetReadDeadline") != nil && methodNamed(t, "RemoteAddr") != nil
}

// listenerShaped is whether t has a listener's methods: the accept method named (Accept,
// AcceptTCP, AcceptUnix) with a connection as its first result, and Addr.
func listenerShaped(t types.Type, acceptName string) bool {
	accept := methodNamed(t, acceptName)
	if t == nil || accept == nil || methodNamed(t, "Addr") == nil {
		return false
	}
	results := accept.Signature().Results()
	return results.Len() > 0 && connShaped(results.At(0).Type())
}

// readerNotConn is whether t is an interface a reader satisfies that a connection's own
// handlers do not ask for: it has Read and no SetReadDeadline.
func readerNotConn(t types.Type) bool {
	return types.IsInterface(t) && methodNamed(t, "Read") != nil && methodNamed(t, "SetReadDeadline") == nil
}

// methodNamed is the exported method name of t, or of *t (a method an addressable value has).
func methodNamed(t types.Type, name string) *types.Func {
	if t == nil {
		return nil
	}
	for _, set := range []*types.MethodSet{types.NewMethodSet(t), types.NewMethodSet(types.NewPointer(t))} {
		if sel := set.Lookup(nil, name); sel != nil {
			if fn, ok := sel.Obj().(*types.Func); ok {
				return fn
			}
		}
	}
	return nil
}

// bounded is whether body bounds the read of read at at: before it, a testconn.Bound of it, a
// deadline set on it or a time.AfterFunc that closes it; anywhere, a close of it in a select case
// that receives from a timer.
func bounded(pass *analysis.Pass, body *ast.BlockStmt, read ast.Expr, at token.Pos) bool {
	target := types.ExprString(read)
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		switch n := n.(type) {
		case *ast.CommClause:
			found = timerCase(pass, n) && closesIn(n.Body, target)
		case *ast.CallExpr:
			found = n.Pos() < at && (boundCall(pass, n, target) || deadlineCall(n, target) || closesAfter(pass, n, target))
		}
		return !found
	})
	return found
}

// boundCall is whether call is testconn.Bound with target as the value bounded.
func boundCall(pass *analysis.Pass, call *ast.CallExpr, target string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Bound" || len(call.Args) != 2 || types.ExprString(call.Args[1]) != target {
		return false
	}
	fn, isFunc := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
	return isFunc && fn.Pkg() != nil && fn.Pkg().Name() == "testconn"
}

// deadlineCall is whether call sets a deadline on target.
func deadlineCall(call *ast.CallExpr, target string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && (sel.Sel.Name == "SetDeadline" || sel.Sel.Name == "SetReadDeadline") && types.ExprString(sel.X) == target
}

// timerCase is whether a select case receives from a timer: time.After or time.Tick, or the C
// of a time.Timer or time.Ticker.
func timerCase(pass *analysis.Pass, clause *ast.CommClause) bool {
	var recv ast.Expr
	switch comm := clause.Comm.(type) {
	case *ast.ExprStmt:
		recv = comm.X
	case *ast.AssignStmt:
		if len(comm.Rhs) == 1 {
			recv = comm.Rhs[0]
		}
	}
	unary, ok := recv.(*ast.UnaryExpr)
	if !ok || unary.Op != token.ARROW {
		return false
	}
	switch ch := unary.X.(type) {
	case *ast.CallExpr:
		if sel, isSel := ch.Fun.(*ast.SelectorExpr); isSel {
			fn, isFunc := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
			return isFunc && (isPkgFunc(fn, "time", "After") || isPkgFunc(fn, "time", "Tick"))
		}
	case *ast.SelectorExpr:
		if ch.Sel.Name == "C" {
			t := pass.TypesInfo.TypeOf(ch.X)
			return t != nil && (strings.HasSuffix(t.String(), "time.Timer") || strings.HasSuffix(t.String(), "time.Ticker"))
		}
	}
	return false
}

// closesIn is whether any of stmts, or code inside them, closes target.
func closesIn(stmts []ast.Stmt, target string) bool {
	closes := false
	for _, st := range stmts {
		ast.Inspect(st, func(n ast.Node) bool {
			if c, isCall := n.(*ast.CallExpr); isCall {
				if s, isSel := c.Fun.(*ast.SelectorExpr); isSel && s.Sel.Name == "Close" && types.ExprString(s.X) == target {
					closes = true
				}
			}
			return !closes
		})
	}
	return closes
}

// closedInCleanup is whether body hands Cleanup a function literal that closes target.
func closedInCleanup(body *ast.BlockStmt, target string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if found || !ok {
			return !found
		}
		if sel, isSel := call.Fun.(*ast.SelectorExpr); isSel && sel.Sel.Name == "Cleanup" && len(call.Args) == 1 {
			if lit, isLit := call.Args[0].(*ast.FuncLit); isLit && closesIn(lit.Body.List, target) {
				found = true
			}
		}
		return !found
	})
	return found
}

// closesAfter is whether call is time.AfterFunc with a function literal that closes target.
func closesAfter(pass *analysis.Pass, call *ast.CallExpr, target string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || len(call.Args) != 2 {
		return false
	}
	fn, isFunc := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
	lit, isLit := call.Args[1].(*ast.FuncLit)
	if !isFunc || !isPkgFunc(fn, "time", "AfterFunc") || !isLit {
		return false
	}
	return closesIn(lit.Body.List, target)
}

// ownerCleanedUp is whether call is an accept on a listener field x.f whose owner's type has a
// Close, closing f, that the package hands to Cleanup: the accept loop then ends with the test.
// Only an accept: a read a test body waits on blocks before any cleanup runs.
func ownerCleanedUp(pass *analysis.Pass, call *ast.CallExpr, read ast.Expr, owned map[*types.TypeName]map[string]bool) bool {
	method, ok := call.Fun.(*ast.SelectorExpr)
	field, isField := read.(*ast.SelectorExpr)
	if !ok || !strings.HasPrefix(method.Sel.Name, "Accept") || !isField {
		return false
	}
	owner := namedOf(pass.TypesInfo.TypeOf(field.X))
	return owner != nil && owned[owner][field.Sel.Name]
}

// cleanupOwners is, for each named type of the package whose Close the package hands to a
// Cleanup (as a method value, or called in a function literal), the fields that Close closes.
func cleanupOwners(pass *analysis.Pass) map[*types.TypeName]map[string]bool {
	registered := map[*types.TypeName]bool{}
	closes := map[*types.TypeName]map[string]bool{}
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				if sel, ok := n.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Cleanup" && len(n.Args) == 1 {
					for _, owner := range closedOwners(pass, n.Args[0]) {
						registered[owner] = true
					}
				}
			case *ast.FuncDecl:
				if owner, fields := closeMethodFields(pass, n); owner != nil {
					closes[owner] = fields
				}
			}
			return true
		})
	}
	owned := map[*types.TypeName]map[string]bool{}
	for owner := range registered {
		if fields := closes[owner]; fields != nil {
			owned[owner] = fields
		}
	}
	return owned
}

// closedOwners is the named types whose Close arg hands on: a method value v.Close, or a function
// literal that calls v.Close().
func closedOwners(pass *analysis.Pass, arg ast.Expr) []*types.TypeName {
	var owners []*types.TypeName
	add := func(sel *ast.SelectorExpr) {
		if sel.Sel.Name == "Close" {
			if owner := namedOf(pass.TypesInfo.TypeOf(sel.X)); owner != nil {
				owners = append(owners, owner)
			}
		}
	}
	switch a := arg.(type) {
	case *ast.SelectorExpr:
		add(a)
	case *ast.FuncLit:
		ast.Inspect(a.Body, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if sel, isSel := c.Fun.(*ast.SelectorExpr); isSel {
					add(sel)
				}
			}
			return true
		})
	}
	return owners
}

// closeMethodFields is, for a method named Close, its receiver's named type and the fields of
// the receiver it closes.
func closeMethodFields(pass *analysis.Pass, fn *ast.FuncDecl) (*types.TypeName, map[string]bool) {
	if fn.Name.Name != "Close" || fn.Recv == nil || len(fn.Recv.List) != 1 || len(fn.Recv.List[0].Names) != 1 || fn.Body == nil {
		return nil, nil
	}
	recv := fn.Recv.List[0].Names[0].Name
	owner := namedOf(pass.TypesInfo.TypeOf(fn.Recv.List[0].Type))
	fields := map[string]bool{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if sel, isSel := c.Fun.(*ast.SelectorExpr); isSel && sel.Sel.Name == "Close" {
				if f, isField := sel.X.(*ast.SelectorExpr); isField {
					if id, isIdent := f.X.(*ast.Ident); isIdent && id.Name == recv {
						fields[f.Sel.Name] = true
					}
				}
			}
		}
		return true
	})
	return owner, fields
}

// namedOf is the named type of t, through one pointer.
func namedOf(t types.Type) *types.TypeName {
	if t == nil {
		return nil
	}
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := t.(*types.Named); ok {
		return n.Obj()
	}
	return nil
}
