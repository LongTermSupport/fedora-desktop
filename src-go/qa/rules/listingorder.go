package rules

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// ListingOrder forbids a test asserting the order of a directory listing nothing sorted.
var ListingOrder = &analysis.Analyzer{
	Name: "project_listing_order",
	Doc: `project_listing_order flags a _test.go comparison that asserts the order of a directory
listing nothing sorted: slices.Equal, slices.EqualFunc, slices.Compare, slices.CompareFunc or
reflect.DeepEqual given, as an argument, a call of a function that returns a listing in
directory order, or a variable last assigned from one with no sort of it between. Outside
_test.go files it flags each function that reads a directory itself and returns the listing in
directory order: the place to sort it, so no caller meets the filesystem's order.

A function returns a listing in directory order when its body calls (*os.File).ReadDir, Readdir
or Readdirnames (or fs.ReadDirFile's ReadDir), or a function that does, has a slice among its
results, and calls nothing that sorts (a function of package sort, slices.Sort, SortFunc,
SortStableFunc, Sorted, SortedFunc, SortedStableFunc, or a function of the same package whose body
sorts). os.ReadDir, fs.ReadDir, filepath.Glob and filepath.WalkDir are sorted by the standard
library and are not listing calls. The mark crosses packages as an analysis fact, so a test of one
package is judged against the function of another.

The order of a directory's entries is the filesystem's: ext4 hashes the names with a seed made at
mkfs, tmpfs lists the newest first, btrfs by index. A test that expects the order its author's
machine gave passes there and fails on the CI runner whose filesystem lists the same names
another way: restore's TestOSReadDir expected [2 5] and CI got [5 2] (fedora-desktop#140).

Not caught: an order asserted through an index (got[0]), a loop over the listing and the want
together, a value derived from the listing (the names of its entries, a joined string), a
listing read through an interface method (no body to mark), a comparison helper of the test's own
that wraps slices.Equal, a production function that uses a directory order without returning it
(a message that names the entries as read), a sort through another package's helper (not counted
as a sort, so its listing is flagged), and a sort of something other than the listing in the same
function (counted as one).

How to fix: sort the listing before comparing (slices.SortFunc on the entries' names), or
compare it as a set (a map or a sorted copy); a function whose callers need an order sorts at its
return, and then it is not marked.`,
	Run:        perPass(runListingOrder),
	ResultType: noResultType,
	FactTypes:  []analysis.Fact{new(unsortedListingFact)},
}

// unsortedListingFact marks a function that returns a directory listing in directory order.
type unsortedListingFact struct{}

func (*unsortedListingFact) AFact() {}

func (*unsortedListingFact) String() string { return "returns a listing in directory order" }

const listingOrderMsg = "%s is a directory listing in directory order, which is the filesystem's and differs between " +
	"machines (project_listing_order): sort it before comparing, or compare it as a set"

// orderedComparisons are the standard library functions that judge two values by element order.
var orderedComparisons = []struct{ pkg, name string }{
	{slicesPkg, "Equal"},
	{slicesPkg, "EqualFunc"},
	{slicesPkg, "Compare"},
	{slicesPkg, "CompareFunc"},
	{"reflect", "DeepEqual"},
}

// slicesPkg is the import path of the slices package, which the comparisons and the sorts share.
const slicesPkg = "slices"

func runListingOrder(pass *analysis.Pass) {
	s := localSorters(pass)
	markListings(pass, s)
	for _, f := range pass.Files {
		if !isTestFile(pass, f) {
			reportUnsortedReturns(pass, f)
			continue
		}
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Body != nil {
				checkListingComparisons(pass, s, fd.Body)
			}
		}
	}
}

// sorters are the functions of the package under analysis whose body sorts: a library sort, or
// another of them. Only the package's own count, so a listing sorted through a helper of its own
// (seat's sortNames) is not marked, while a function of another package that sorts something
// inside (testing's) does not pass for a sort of what it is given.
type sorters map[*types.Func]bool

// sorts is whether fn orders what it is given: a library sort or one of the package's sorters.
func (s sorters) sorts(fn *types.Func) bool {
	return fn != nil && (s[fn.Origin()] || isSortCall(fn))
}

// localSorters finds the package's sorters, to a fixed point.
func localSorters(pass *analysis.Pass) sorters {
	s := sorters{}
	for changed := true; changed; {
		changed = false
		for _, f := range pass.Files {
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func)
				if !ok || s[fn] || !s.calledIn(pass, fd.Body) {
					continue
				}
				s[fn] = true
				changed = true
			}
		}
	}
	return s
}

// calledIn is whether body calls something that sorts.
func (s sorters) calledIn(pass *analysis.Pass, body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && s.sorts(calledFunc(pass, call)) {
			found = true
		}
		return !found
	})
	return found
}

// markListings exports the fact for each function of the package that returns a listing in
// directory order, to a fixed point so a wrapper of such a function is marked too.
func markListings(pass *analysis.Pass, s sorters) {
	for changed := true; changed; {
		changed = false
		for _, f := range pass.Files {
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func)
				if !ok || !returnsSlice(fn) || isMarkedListing(pass, fn) || !listsUnsorted(pass, s, fd.Body) {
					continue
				}
				pass.ExportObjectFact(fn, new(unsortedListingFact))
				changed = true
			}
		}
	}
}

// reportUnsortedReturns reports each function of f, a file that is not a test, that reads a
// directory itself (one of the unsorted listing methods) and returns the listing in directory
// order. A wrapper of such a function is marked but not reported: sorting at the read unmarks it.
func reportUnsortedReturns(pass *analysis.Pass, f *ast.File) {
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func)
		if ok && isMarkedListing(pass, fn) && readsDirectory(pass, fd.Body) {
			pass.Reportf(fd.Name.Pos(), listingReturnMsg, fd.Name.Name)
		}
	}
}

const listingReturnMsg = "%s returns a directory listing in directory order, which is the filesystem's and differs " +
	"between machines (project_listing_order): sort it at its return, as homefs.Root.Entries does"

// readsDirectory is whether body calls one of the unsorted listing methods itself.
func readsDirectory(pass *analysis.Pass, body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if fn := calledFunc(pass, call); fn != nil && isUnsortedListingMethod(fn) {
				found = true
			}
		}
		return !found
	})
	return found
}

// returnsSlice is whether one of fn's results is a slice.
func returnsSlice(fn *types.Func) bool {
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return false
	}
	for v := range sig.Results().Variables() {
		if _, isSlice := v.Type().Underlying().(*types.Slice); isSlice {
			return true
		}
	}
	return false
}

// isMarkedListing is whether fn is a function marked as returning a listing in directory order.
func isMarkedListing(pass *analysis.Pass, fn *types.Func) bool {
	return fn != nil && pass.ImportObjectFact(fn.Origin(), new(unsortedListingFact))
}

// listsUnsorted is whether body reads a listing in directory order and sorts nothing.
func listsUnsorted(pass *analysis.Pass, s sorters, body *ast.BlockStmt) bool {
	lists, sorts := false, false
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			fn := calledFunc(pass, call)
			lists = lists || isListingCall(pass, fn)
			sorts = sorts || s.sorts(fn)
		}
		return true
	})
	return lists && !sorts
}

// isListingCall is whether fn is a call that returns a directory's entries in directory order:
// one of the three listing methods of os.File or fs.ReadDirFile, or a marked function.
func isListingCall(pass *analysis.Pass, fn *types.Func) bool {
	if fn == nil || fn.Pkg() == nil {
		return false
	}
	return isUnsortedListingMethod(fn) || isMarkedListing(pass, fn)
}

// isUnsortedListingMethod is whether fn is ReadDir, Readdir or Readdirnames of os.File or
// fs.ReadDirFile.
func isUnsortedListingMethod(fn *types.Func) bool {
	switch fn.Name() {
	case "ReadDir", "Readdir", "Readdirnames":
	default:
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return false
	}
	recv := sig.Recv().Type()
	if ptr, isPtr := recv.(*types.Pointer); isPtr {
		recv = ptr.Elem()
	}
	named, isNamed := recv.(*types.Named)
	if !isNamed || named.Obj().Pkg() == nil {
		return false
	}
	path, name := named.Obj().Pkg().Path(), named.Obj().Name()
	return path == "os" && name == "File" || path == "io/fs" && name == "ReadDirFile"
}

// isSortCall is whether fn orders what it is given: a function of package sort that sorts, or
// slices.Sort, SortFunc, SortStableFunc, Sorted, SortedFunc and SortedStableFunc.
func isSortCall(fn *types.Func) bool {
	if fn == nil || fn.Pkg() == nil {
		return false
	}
	switch fn.Pkg().Path() {
	case "sort":
		return !strings.HasPrefix(fn.Name(), "Is") && !strings.HasPrefix(fn.Name(), "Search")
	case slicesPkg:
		return strings.HasPrefix(fn.Name(), "Sort")
	}
	return false
}

// checkListingComparisons reports each ordered comparison in body of a listing nothing sorted.
func checkListingComparisons(pass *analysis.Pass, s sorters, body *ast.BlockStmt) {
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isOrderedComparison(calledFunc(pass, call)) {
			return true
		}
		for _, arg := range call.Args {
			if what, unsorted := unsortedListing(pass, s, body, arg, call.Pos()); unsorted {
				pass.Reportf(arg.Pos(), listingOrderMsg, what)
				break
			}
		}
		return true
	})
}

// isOrderedComparison is whether fn is one of the comparisons that judge element order.
func isOrderedComparison(fn *types.Func) bool {
	for _, c := range orderedComparisons {
		if isPkgFunc(fn, c.pkg, c.name) {
			return true
		}
	}
	return false
}

// unsortedListing is what arg is, when it is a listing in directory order at the comparison at
// pos: a call of a listing function, or a variable last assigned from one with no sort of it
// since.
func unsortedListing(pass *analysis.Pass, s sorters, body *ast.BlockStmt, arg ast.Expr, pos token.Pos) (string, bool) {
	arg = ast.Unparen(arg)
	switch e := arg.(type) {
	case *ast.CallExpr:
		fn := calledFunc(pass, e)
		if !isListingCall(pass, fn) {
			return "", false
		}
		return "the result of " + fn.Name(), true
	case *ast.Ident:
		v, ok := pass.TypesInfo.ObjectOf(e).(*types.Var)
		if !ok {
			return "", false
		}
		from, call := lastListingAssign(pass, body, v, pos)
		if call == nil || sortedBetween(pass, s, body, v, from, pos) {
			return "", false
		}
		return e.Name, true
	}
	return "", false
}

// lastListingAssign is the end of the latest assignment to v before pos in body and the call it
// took its value from, when that is a listing call; a later assignment of anything else ends the
// listing, so it answers nil.
func lastListingAssign(pass *analysis.Pass, body *ast.BlockStmt, v *types.Var, pos token.Pos) (token.Pos, *ast.CallExpr) {
	var at token.Pos
	var from *ast.CallExpr
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || as.End() > pos || as.End() < at || !assigns(pass, as, v) {
			return true
		}
		at, from = as.End(), nil
		if len(as.Rhs) == 1 {
			if call, isCall := ast.Unparen(as.Rhs[0]).(*ast.CallExpr); isCall && isListingCall(pass, calledFunc(pass, call)) {
				from = call
			}
		}
		return true
	})
	return at, from
}

// assigns is whether as assigns to v.
func assigns(pass *analysis.Pass, as *ast.AssignStmt, v *types.Var) bool {
	for _, lhs := range as.Lhs {
		if id, ok := lhs.(*ast.Ident); ok && pass.TypesInfo.ObjectOf(id) == v {
			return true
		}
	}
	return false
}

// sortedBetween is whether body sorts v between from and to: a sort call given v.
func sortedBetween(pass *analysis.Pass, s sorters, body *ast.BlockStmt, v *types.Var, from, to token.Pos) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || call.Pos() < from || call.End() > to || !s.sorts(calledFunc(pass, call)) {
			return !found
		}
		for _, arg := range call.Args {
			if id, isIdent := ast.Unparen(arg).(*ast.Ident); isIdent && pass.TypesInfo.ObjectOf(id) == v {
				found = true
			}
		}
		return !found
	})
	return found
}
