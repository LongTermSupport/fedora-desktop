package rules

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// TestChown forbids a test giving a file to another user without saying it needs root.
var TestChown = &analysis.Analyzer{
	Name: "project_test_chown",
	Doc: `project_test_chown flags a call in a _test.go file that changes a file's owner to a user
other than the caller: os.Chown and os.Lchown, (*os.File).Chown, syscall.Chown, Lchown and
Fchown, unix.Chown, Lchown, Fchown and Fchownat. An argument is the caller's own when it is the
constant -1 or a call of os.Geteuid or os.Getuid (the uid) or os.Getegid or os.Getgid (the gid);
any other uid or gid is another user's.

Giving a file away needs CAP_CHOWN. Every agent container runs as root, so a test that makes
"a file another user owns" passes there and fails for CI's unprivileged user with "operation
not permitted"; the first CI run of the Go tests found six packages that way (a seat bundle,
the member home, the transcript scan, the state writer, the guest root and the LXC host).

Not flagged, because each states what it needs or cannot fail: a call inside an if that tests
the user is root (os.Geteuid() == 0); a call in a function that skips when the user is not root
(an if testing os.Geteuid() != 0 whose body calls Skip, Skipf or SkipNow, and its else); a call
that is the init of an if whose body skips. Those tests run nowhere CI runs, which is the other
half of the same hazard and the owner's to decide (a rule cannot say which tests matter). A chown
in a non-test file is production code and is not judged here.

How to fix: make the code under test take the owner it expects through a seam (a parameter
or a field, defaulting to the caller's effective uid), and let the test expect a different owner
than the one the file has, with a positive twin that expects the real one. The check then runs
unprivileged, and with the same comparison production makes.`,
	Run:        perPass(runTestChown),
	ResultType: noResultType,
}

const (
	syscallPkg = "syscall"
	unixPkg    = "golang.org/x/sys/unix"
	chownFn    = "Chown"
	lchownFn   = "Lchown"
	fchownFn   = "Fchown"
)

// chownPkg is the functions of one package that change an owner, and where their uid and gid
// arguments are.
type chownPkg struct {
	path     string
	names    []string
	uid, gid int
}

// chownPkgs are the package-level functions that change an owner.
var chownPkgs = []chownPkg{
	{"os", []string{chownFn, lchownFn}, 1, 2},
	{syscallPkg, []string{chownFn, lchownFn, fchownFn}, 1, 2},
	{unixPkg, []string{chownFn, lchownFn, fchownFn}, 1, 2},
	{unixPkg, []string{"Fchownat"}, 2, 3},
}

// skipMethods are the testing methods that end a test as skipped.
var skipMethods = []string{"Skip", "Skipf", "SkipNow"}

func runTestChown(pass *analysis.Pass) {
	for _, f := range pass.Files {
		if !isTestFile(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			checkChownsIn(pass, fd)
		}
	}
}

// checkChownsIn reports each call in fd that gives a file away, unless fd or an enclosing
// statement says it needs root.
func checkChownsIn(pass *analysis.Pass, fd *ast.FuncDecl) {
	var guarded []ast.Node
	whole := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		is, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		test := userIsRootTest(pass, is.Cond)
		switch {
		case test.equal:
			guarded = append(guarded, is.Body)
		case test.notEqual && skips(pass, is.Body):
			whole = true
		case test.notEqual && is.Else != nil:
			guarded = append(guarded, is.Else)
		}
		if is.Init != nil && skips(pass, is.Body) {
			guarded = append(guarded, is.Init)
		}
		return true
	})
	if whole {
		return
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		label, gives := givesAFileAway(pass, call)
		if gives && !within(call.Pos(), guarded) {
			pass.Reportf(call.Pos(), "%s gives a file to another user, which fails for an unprivileged user: "+
				"make the code under test take the owner it expects through a seam, and expect a different one here", label)
		}
		return true
	})
}

// givesAFileAway is the name of the chown call and whether it names an owner other than the
// caller.
func givesAFileAway(pass *analysis.Pass, call *ast.CallExpr) (string, bool) {
	fn := calledFunc(pass, call)
	if fn == nil || fn.Pkg() == nil {
		return "", false
	}
	if isFileChown(fn) {
		return "File.Chown", len(call.Args) == 2 && !callersOwn(pass, call.Args[0], call.Args[1])
	}
	for _, c := range chownPkgs {
		for _, name := range c.names {
			if isPkgFunc(fn, c.path, name) && len(call.Args) > c.gid {
				return fn.Pkg().Name() + "." + name, !callersOwn(pass, call.Args[c.uid], call.Args[c.gid])
			}
		}
	}
	return "", false
}

// isFileChown is whether fn is the method (*os.File).Chown.
func isFileChown(fn *types.Func) bool {
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil || fn.Pkg().Path() != "os" || fn.Name() != chownFn {
		return false
	}
	recv := sig.Recv().Type()
	if ptr, isPtr := recv.(*types.Pointer); isPtr {
		recv = ptr.Elem()
	}
	named, isNamed := recv.(*types.Named)
	return isNamed && named.Obj().Name() == "File"
}

// callersOwn is whether both arguments leave the owner or group as the caller's: -1, or the
// caller's own id.
func callersOwn(pass *analysis.Pass, uid, gid ast.Expr) bool {
	return isOwnID(pass, uid, "Geteuid", "Getuid") && isOwnID(pass, gid, "Getegid", "Getgid")
}

// isOwnID is whether arg is the constant -1 or a call of one of the os functions names.
func isOwnID(pass *analysis.Pass, arg ast.Expr, names ...string) bool {
	if tv, ok := pass.TypesInfo.Types[arg]; ok && tv.Value != nil {
		id, exact := constant.Int64Val(tv.Value)
		return exact && id == -1
	}
	call, ok := ast.Unparen(arg).(*ast.CallExpr)
	if !ok {
		return false
	}
	fn := calledFunc(pass, call)
	for _, name := range names {
		if isPkgFunc(fn, "os", name) {
			return true
		}
	}
	return false
}

// rootTest is how a condition compares the caller's uid with 0.
type rootTest struct{ equal, notEqual bool }

// userIsRootTest is how cond compares the caller's uid with 0, anywhere in the condition (so
// `os.Geteuid() != 0 && flag` is a test of not-root).
func userIsRootTest(pass *analysis.Pass, cond ast.Expr) rootTest {
	var found rootTest
	ast.Inspect(cond, func(n ast.Node) bool {
		bin, ok := n.(*ast.BinaryExpr)
		if !ok || !comparesUIDWithZero(pass, bin) {
			return true
		}
		found.equal = found.equal || bin.Op == token.EQL
		found.notEqual = found.notEqual || bin.Op == token.NEQ
		return true
	})
	return found
}

// comparesUIDWithZero is whether bin is == or != between a call of os.Geteuid or os.Getuid and
// the constant 0, in either order.
func comparesUIDWithZero(pass *analysis.Pass, bin *ast.BinaryExpr) bool {
	if bin.Op != token.EQL && bin.Op != token.NEQ {
		return false
	}
	return callsOwnUID(pass, bin.X) && isZero(pass, bin.Y) || callsOwnUID(pass, bin.Y) && isZero(pass, bin.X)
}

// callsOwnUID is whether e is a call of os.Geteuid or os.Getuid.
func callsOwnUID(pass *analysis.Pass, e ast.Expr) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return false
	}
	fn := calledFunc(pass, call)
	return isPkgFunc(fn, "os", "Geteuid") || isPkgFunc(fn, "os", "Getuid")
}

// isZero is whether e is the constant 0.
func isZero(pass *analysis.Pass, e ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok || tv.Value == nil {
		return false
	}
	id, exact := constant.Int64Val(tv.Value)
	return exact && id == 0
}

// skips is whether body calls Skip, Skipf or SkipNow of the testing package.
func skips(pass *analysis.Pass, body ast.Node) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return !found
		}
		fn := calledFunc(pass, call)
		if fn != nil && fn.Pkg() != nil && fn.Pkg().Path() == "testing" {
			for _, name := range skipMethods {
				found = found || fn.Name() == name
			}
		}
		return !found
	})
	return found
}
