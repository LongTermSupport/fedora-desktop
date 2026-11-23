package rules

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const (
	// floorStatePkg is the package whose Floor type is a room's floor as a (time, event IDs) pair.
	floorStatePkg = "internal/teamchat/member/state"
	floorType     = "Floor"
)

var (
	floorName = regexp.MustCompile(`(?i)floor`)
	sinceName = regexp.MustCompile(`(?i)since`)
	// timeName is an event's time: OriginTs, ts, SentMS, AtMS.
	timeName = regexp.MustCompile(`(?i)(ts|ms)$`)
)

// FloorByTime forbids an event-ordering cutoff held as a millisecond alone.
var FloorByTime = &analysis.Analyzer{
	Name: "project_floor_by_time",
	Doc: `project_floor_by_time flags, in internal/teamchat outside test files, an event-ordering
cutoff held as a millisecond alone:

- an ordered comparison (<, <=, >, >=), or an argument of the max or min builtin, that is an
  integer named for a floor (the identifier, field or called function whose name holds "floor",
  any case);
- an ordered comparison, or a max or min argument, that is the MS field of the state package's
  Floor, outside Floor's own methods;
- a comparison by <= or > (the two that drop the boundary millisecond) of an integer named for a
  "since" bound with an integer named for a time (ending in ts or ms, any case).

The floor checks judge internal/teamchat/member only: elsewhere in teamchat a "floor" is an age
threshold (health, status) or a question number (proto), which carry no event-ordering hazard. A
since compared with a stream position (fakehs's pos) is a position, the right form, and is not
judged.

Two Matrix events can share an origin_server_ts. A floor raised to the newest event's
millisecond and judged by "ts <= floor" drops every later event sent in that same millisecond as
already seen: the questions room's floor skipped an owner's answer posted in the millisecond of
the post before it (Plan 00209 R0 step 7). A "since" time that is a wall-clock bound with no
event behind it cannot name the events at its millisecond, so the cut that drops them loses them.

Not caught: a floor held under a name without "floor" and compared as a bare integer, and a
cutoff judged through a helper taking the integers apart from their names. An age threshold
(older than a limit) is not a floor and is not named one.

How to fix: hold the floor as a state.Floor, the millisecond and the event IDs at that
millisecond already taken, and judge an event by Floor.Covers(ts, id), raise it by Raised and
Max. A wall-clock "since" bound keeps its boundary millisecond: drop by ts < since.`,
	Run:        perPass(runFloorByTime),
	ResultType: noResultType,
}

func runFloorByTime(pass *analysis.Pass) {
	if !inTeamchat(pass.Pkg.Path()) {
		return
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			if isFloorMethod(pass, decl) {
				continue
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.BinaryExpr:
					reportFloorComparison(pass, n)
				case *ast.CallExpr:
					reportFloorMax(pass, n)
				}
				return true
			})
		}
	}
}

func reportFloorComparison(pass *analysis.Pass, be *ast.BinaryExpr) {
	if be.Op != token.LSS && be.Op != token.LEQ && be.Op != token.GTR && be.Op != token.GEQ {
		return
	}
	// A bound checked against a constant is a value's validation, not an event's cut.
	if pass.TypesInfo.Types[be.X].Value != nil || pass.TypesInfo.Types[be.Y].Value != nil {
		return
	}
	for _, x := range []ast.Expr{be.X, be.Y} {
		if isTimeFloor(pass, x) {
			pass.Reportf(be.Pos(), "a floor compared as a millisecond alone drops or repeats the events sharing its millisecond (Plan 00209): hold a state.Floor and judge by Covers (project_floor_by_time)")
			return
		}
	}
	if be.Op != token.LEQ && be.Op != token.GTR {
		return
	}
	for i, x := range []ast.Expr{be.X, be.Y} {
		other := []ast.Expr{be.Y, be.X}[i]
		if isIntegerExpr(pass, x) && sinceName.MatchString(lastName(x)) && timeName.MatchString(lastName(other)) {
			pass.Reportf(be.Pos(), "a since bound cut by <= or > drops the events of its own millisecond (Plan 00209): keep the boundary, drop by ts < since (project_floor_by_time)")
			return
		}
	}
}

func reportFloorMax(pass *analysis.Pass, call *ast.CallExpr) {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return
	}
	b, isBuiltin := pass.TypesInfo.Uses[id].(*types.Builtin)
	if !isBuiltin || (b.Name() != "max" && b.Name() != "min") {
		return
	}
	for _, arg := range call.Args {
		if isTimeFloor(pass, arg) {
			pass.Reportf(call.Pos(), "a floor set by %s as a millisecond alone covers every event of that millisecond (Plan 00209): hold a state.Floor and raise it by Raised or Max (project_floor_by_time)", b.Name())
			return
		}
	}
}

// isTimeFloor is whether e is an integer named for a floor, or a state.Floor's MS.
func isTimeFloor(pass *analysis.Pass, e ast.Expr) bool {
	e = ast.Unparen(e)
	if sel, ok := e.(*ast.SelectorExpr); ok && sel.Sel.Name == "MS" && isFloorType(pass.TypesInfo.TypeOf(sel.X)) {
		return true
	}
	return inMember(pass.Pkg.Path()) && isIntegerExpr(pass, e) && floorName.MatchString(lastName(e))
}

// inMember is whether pkgPath is teamchat's member tree, the one that judges event streams by
// floors; elsewhere in teamchat a "floor" is an age threshold or a question number.
func inMember(pkgPath string) bool {
	path := pkgPath
	if i := strings.IndexByte(path, ' '); i >= 0 {
		path = path[:i]
	}
	return strings.Contains("/"+path+"/", "/"+teamchatPkg+"/member/")
}

// lastName is the identifier an expression ends in: a variable, a field, or a called function.
func lastName(e ast.Expr) string {
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	case *ast.CallExpr:
		return lastName(e.Fun)
	case *ast.IndexExpr:
		return lastName(e.X)
	}
	return ""
}

// isIntegerExpr is whether e has an integer type.
func isIntegerExpr(pass *analysis.Pass, e ast.Expr) bool {
	t := pass.TypesInfo.TypeOf(e)
	return t != nil && isInteger(t)
}

// isFloorType is whether t is the state package's Floor, or a pointer to it.
func isFloorType(t types.Type) bool {
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Name() == floorType && obj.Pkg() != nil && ownedBy(obj.Pkg().Path(), floorStatePkg)
}

// isFloorMethod is whether decl is a method of Floor itself, the one place its time is judged.
func isFloorMethod(pass *analysis.Pass, decl ast.Decl) bool {
	fd, ok := decl.(*ast.FuncDecl)
	if !ok || fd.Recv == nil || len(fd.Recv.List) != 1 {
		return false
	}
	return isFloorType(pass.TypesInfo.TypeOf(fd.Recv.List[0].Type))
}
