package rules

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"math"

	"golang.org/x/tools/go/analysis"
)

// durationOwner is the package that multiplies a duration by a number and checks the product.
const durationOwner = "internal/duration"

// maxBoundDepth stops a walk through held variables that refer to each other.
const maxBoundDepth = 16

// DurationOverflow flags a time.Duration built from a number with no bound that keeps it
// inside int64 nanoseconds.
var DurationOverflow = &analysis.Analyzer{
	Name: "project_duration_overflow",
	Doc: `project_duration_overflow flags a time.Duration built from a number that no constant
bound keeps inside int64 nanoseconds.

A Duration is an int64 count of nanoseconds, and Go's integer arithmetic wraps: 106752 days, or
292 years of seconds, is past it, and the product comes out negative. A number read from input
(an argument, the environment, a file, another program's output) has no size of its own, so a
duration built from it can mean the opposite of what was asked: vmtest prune-runs
--older-than 200000 made the cutoff negative, every failed run was older than it, and every
failed run's overlay was pruned. A negative wait or timeout fires at once.

Flagged, outside the _test.go files and the owner internal/duration: a multiplication of type
time.Duration (either operand order, a chain, *=) with a factor that is time.Duration(x) of a
number x that is not a constant, or a variable assigned such a conversion; and a conversion
of a float that is not a constant to time.Duration. Each is judged by the largest value its
factors can have: a constant, or a number the package refuses above a constant. A refusal is a
comparison x > K or x >= K (K < x, K <= x) in the condition of an if, alone or joined by ||,
whose block ends in a return; it bounds x (a variable or a field) everywhere in the package.
Sums, differences, products and conversions of bounded numbers are bounded. A factor with no
bound, or a bound whose product leaves int64, is a finding.

Not caught: a comparison that has the refusal's shape and is not one (a return that accepts);
a lower bound (a large negative number); a number held in a variable other than by a direct
conversion; a bound made in another package; a uint64 converted to a Duration; package-level
initialisers.

How to fix: refuse the number above a constant where it is read, in the same package (a usage
error or a failure naming the largest value taken), or build the duration with
duration.Times, which refuses a product that leaves int64.`,
	Run:        perPass(runDurationOverflow),
	ResultType: noResultType,
}

// durationJudge holds what one package says about its numbers: the bounds its refusals set,
// and the variables a conversion was assigned to.
type durationJudge struct {
	pass   *analysis.Pass
	bounds map[types.Object]constant.Value
	held   map[types.Object]ast.Expr
	seen   map[ast.Node]bool
}

func runDurationOverflow(pass *analysis.Pass) {
	if ownedBy(pass.Pkg.Path(), durationOwner) {
		return
	}
	j := &durationJudge{
		pass:   pass,
		bounds: map[types.Object]constant.Value{},
		held:   map[types.Object]ast.Expr{},
		seen:   map[ast.Node]bool{},
	}
	var files []*ast.File
	for _, f := range pass.Files {
		if !isTestFile(pass, f) {
			files = append(files, f)
		}
	}
	for _, f := range files {
		ast.Inspect(f, j.collect)
	}
	for _, f := range files {
		ast.Inspect(f, j.judge)
	}
}

// collect records the refusals and the held conversions of one node.
func (j *durationJudge) collect(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.IfStmt:
		if endsInReturn(n.Body) {
			for _, c := range orOperands(n.Cond) {
				j.refusal(c)
			}
		}
	case *ast.AssignStmt:
		if (n.Tok == token.DEFINE || n.Tok == token.ASSIGN) && len(n.Lhs) == 1 && len(n.Rhs) == 1 {
			j.hold(n.Lhs[0], n.Rhs[0])
		}
	case *ast.ValueSpec:
		if len(n.Names) == 1 && len(n.Values) == 1 {
			j.hold(n.Names[0], n.Values[0])
		}
	}
	return true
}

// refusal records the bound a comparison of a refusing if sets.
func (j *durationJudge) refusal(c ast.Expr) {
	b, ok := ast.Unparen(c).(*ast.BinaryExpr)
	if !ok {
		return
	}
	subject, limit := b.X, b.Y
	if b.Op == token.LSS || b.Op == token.LEQ {
		subject, limit = b.Y, b.X
	} else if b.Op != token.GTR && b.Op != token.GEQ {
		return
	}
	k := j.pass.TypesInfo.Types[limit].Value
	obj := j.object(subject)
	if k == nil || obj == nil || !isNumeric(k) {
		return
	}
	k = absValue(k)
	if was, had := j.bounds[obj]; !had || constant.Compare(k, token.GTR, was) {
		j.bounds[obj] = k
	}
}

// hold records a variable assigned a conversion of a number to a Duration.
func (j *durationJudge) hold(lhs, rhs ast.Expr) {
	arg, ok := j.numberConversion(rhs)
	if !ok {
		return
	}
	if obj := j.object(lhs); obj != nil {
		j.held[obj] = arg
	}
}

// judge reports each duration built from a number in one node.
func (j *durationJudge) judge(n ast.Node) bool {
	info := j.pass.TypesInfo
	switch n := n.(type) {
	case *ast.BinaryExpr:
		if n.Op == token.MUL && !j.seen[n] && isDuration(info.TypeOf(n)) && info.Types[n].Value == nil {
			j.product(n, j.factors(n))
		}
	case *ast.AssignStmt:
		if n.Tok == token.MUL_ASSIGN && len(n.Lhs) == 1 && len(n.Rhs) == 1 && isDuration(info.TypeOf(n.Lhs[0])) {
			j.product(n, append([]ast.Expr{n.Lhs[0]}, j.factors(n.Rhs[0])...))
		}
	case *ast.CallExpr:
		if arg, ok := j.conversion(n); ok && isFloat(info.TypeOf(arg)) && info.Types[arg].Value == nil {
			j.within(n, arg, []ast.Expr{arg})
		}
	}
	return true
}

// product judges a multiplication whose factors include a number converted to a Duration.
func (j *durationJudge) product(at ast.Node, factors []ast.Expr) {
	for _, f := range factors {
		if _, ok := j.numberConversion(f); ok || j.heldBy(f) {
			j.within(at, f, factors)
			return
		}
	}
}

// within reports at unless the product of the factors' largest values fits in int64.
func (j *durationJudge) within(at ast.Node, number ast.Expr, factors []ast.Expr) {
	total := constant.MakeInt64(1)
	var numberBound constant.Value
	for _, f := range factors {
		v, ok := j.largest(f, 0)
		if !ok {
			j.pass.Reportf(at.Pos(), "time.Duration from a number with no bound: refuse it above a constant where it is read, or build it with duration.Times")
			return
		}
		if f == number {
			numberBound = v
		}
		total = constant.BinaryOp(total, token.MUL, v)
	}
	if constant.Compare(total, token.GTR, constant.MakeInt64(math.MaxInt64)) {
		j.pass.Reportf(at.Pos(), "time.Duration from a number bounded by %s, which leaves int64 nanoseconds: lower the bound", numberBound.ExactString())
	}
}

// factors is the operands of a chain of Duration multiplications, each inner one marked seen.
func (j *durationJudge) factors(e ast.Expr) []ast.Expr {
	e = ast.Unparen(e)
	if b, ok := e.(*ast.BinaryExpr); ok && b.Op == token.MUL && isDuration(j.pass.TypesInfo.TypeOf(b)) {
		j.seen[b] = true
		return append(j.factors(b.X), j.factors(b.Y)...)
	}
	return []ast.Expr{e}
}

// largest is the largest magnitude e can have, when the package bounds every number in it.
func (j *durationJudge) largest(e ast.Expr, depth int) (constant.Value, bool) {
	if depth > maxBoundDepth {
		return nil, false
	}
	e = ast.Unparen(e)
	if v := j.pass.TypesInfo.Types[e].Value; v != nil {
		return absValue(v), isNumeric(v)
	}
	switch e := e.(type) {
	case *ast.CallExpr:
		if arg, ok := j.conversion(e); ok {
			return j.largest(arg, depth+1)
		}
		if arg, ok := j.numericConversion(e); ok {
			return j.largest(arg, depth+1)
		}
	case *ast.Ident, *ast.SelectorExpr:
		return j.largestNamed(e, depth)
	case *ast.UnaryExpr:
		if e.Op == token.SUB || e.Op == token.ADD {
			return j.largest(e.X, depth+1)
		}
	case *ast.BinaryExpr:
		return j.largestOf(e, depth)
	}
	return nil, false
}

// largestNamed is the bound of a variable or field: its refusal, or what it was assigned.
func (j *durationJudge) largestNamed(e ast.Expr, depth int) (constant.Value, bool) {
	obj := j.object(e)
	if obj == nil {
		return nil, false
	}
	if b, ok := j.bounds[obj]; ok {
		return b, true
	}
	if h, ok := j.held[obj]; ok {
		return j.largest(h, depth+1)
	}
	return nil, false
}

// largestOf is the largest magnitude of a sum, difference, product or quotient.
func (j *durationJudge) largestOf(e *ast.BinaryExpr, depth int) (constant.Value, bool) {
	x, okX := j.largest(e.X, depth+1)
	if !okX {
		return nil, false
	}
	if e.Op == token.QUO {
		d := j.pass.TypesInfo.Types[e.Y].Value
		return x, d != nil && isNumeric(d) && constant.Compare(absValue(d), token.GEQ, constant.MakeInt64(1))
	}
	if e.Op != token.ADD && e.Op != token.SUB && e.Op != token.MUL {
		return nil, false
	}
	y, okY := j.largest(e.Y, depth+1)
	if !okY {
		return nil, false
	}
	if e.Op == token.MUL {
		return constant.BinaryOp(x, token.MUL, y), true
	}
	return constant.BinaryOp(x, token.ADD, y), true
}

// numberConversion is the argument of a conversion to Duration of a number that is not a
// constant and not already a Duration.
func (j *durationJudge) numberConversion(e ast.Expr) (ast.Expr, bool) {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	arg, ok := j.conversion(call)
	if !ok || j.pass.TypesInfo.Types[arg].Value != nil || isDuration(j.pass.TypesInfo.TypeOf(arg)) {
		return nil, false
	}
	return arg, true
}

// conversion is the argument of a conversion to time.Duration.
func (j *durationJudge) conversion(call *ast.CallExpr) (ast.Expr, bool) {
	if len(call.Args) != 1 || !j.pass.TypesInfo.Types[call.Fun].IsType() || !isDuration(j.pass.TypesInfo.TypeOf(call.Fun)) {
		return nil, false
	}
	return call.Args[0], true
}

// numericConversion is the argument of a conversion to any other number type (float64(n)).
func (j *durationJudge) numericConversion(call *ast.CallExpr) (ast.Expr, bool) {
	if len(call.Args) != 1 || !j.pass.TypesInfo.Types[call.Fun].IsType() {
		return nil, false
	}
	b, ok := j.pass.TypesInfo.TypeOf(call.Fun).Underlying().(*types.Basic)
	if !ok || b.Info()&types.IsNumeric == 0 {
		return nil, false
	}
	return call.Args[0], true
}

// heldBy is whether e names a variable assigned a number's conversion.
func (j *durationJudge) heldBy(e ast.Expr) bool {
	obj := j.object(e)
	if obj == nil {
		return false
	}
	_, ok := j.held[obj]
	return ok
}

// object is the variable or field e names, or nil.
func (j *durationJudge) object(e ast.Expr) types.Object {
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		if v, ok := j.pass.TypesInfo.ObjectOf(e).(*types.Var); ok {
			return v
		}
	case *ast.SelectorExpr:
		if s, ok := j.pass.TypesInfo.Selections[e]; ok && s.Kind() == types.FieldVal {
			return s.Obj()
		}
		if v, ok := j.pass.TypesInfo.ObjectOf(e.Sel).(*types.Var); ok {
			return v
		}
	}
	return nil
}

// orOperands is a condition split at its top-level ||.
func orOperands(e ast.Expr) []ast.Expr {
	e = ast.Unparen(e)
	if b, ok := e.(*ast.BinaryExpr); ok && b.Op == token.LOR {
		return append(orOperands(b.X), orOperands(b.Y)...)
	}
	return []ast.Expr{e}
}

// endsInReturn is whether a block's last statement is a return.
func endsInReturn(b *ast.BlockStmt) bool {
	if len(b.List) == 0 {
		return false
	}
	_, ok := b.List[len(b.List)-1].(*ast.ReturnStmt)
	return ok
}

// isDuration is whether t is time.Duration.
func isDuration(t types.Type) bool {
	n, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := n.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == "time" && obj.Name() == "Duration"
}

// isFloat is whether t is a floating-point type.
func isFloat(t types.Type) bool {
	if t == nil {
		return false
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsFloat != 0
}

// isNumeric is whether a constant is an integer or a float.
func isNumeric(v constant.Value) bool {
	return v.Kind() == constant.Int || v.Kind() == constant.Float
}

// absValue is the magnitude of a numeric constant.
func absValue(v constant.Value) constant.Value {
	if constant.Sign(v) < 0 {
		return constant.UnaryOp(token.SUB, v, 0)
	}
	return v
}
