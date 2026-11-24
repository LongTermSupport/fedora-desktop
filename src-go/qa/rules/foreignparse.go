package rules

import (
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
)

const (
	stdinreadPkg = "internal/stdinread"
	// foreignRemedy is proto's tolerant decoder of a foreign object: it parses the keys it is
	// asked for strictly and skips the rest, so its own parse of a key's value is not a sink.
	foreignRemedy = "ForeignObject"
	fromStdin     = "stdin"
	fromEnv       = "an environment variable"
	// foreignRounds bounds the package's fixed point of wrappers and sources.
	foreignRounds = 16
)

// protocolStdin are the packages whose stdin is a document teamchat itself specifies and
// teamchat (or its own play) writes, each decoded to its every key: a strict parse there is the
// protocol's own rule, not a foreign producer's field refusing the rest. The producer is the
// reason.
var protocolStdin = []struct{ pkg, producer string }{
	{pkg: "internal/teamchat/element", producer: "the Element play writes teamchat-admin element's EL1 team list"},
	{pkg: "internal/teamchat/member/guest", producer: "the host's teamchat writes the guest ops' facts, issued bundle and desired membership over lxc-attach (GU1-GU5)"},
	{pkg: "internal/teamchat/member/placeissued", producer: "teamchat-admin member add, return or reissue writes the issued seat a person pipes in"},
	{pkg: teamchatAdminPkg, producer: "teamchat-admin's root parent writes ctl-child's request (CC2)"},
}

const foreignParseFix = " parsed by teamchat's strict wire parser (project_foreign_strict_parse): a field the program never reads (a float, a large integer, a repeated key) refuses the fields it reads; decode a foreign object with proto.ForeignObject, naming the keys read"

// ForeignStrictParse keeps input from outside teamchat's protocol away from proto.Parse.
var ForeignStrictParse = &analysis.Analyzer{
	Name: "project_foreign_strict_parse",
	Doc: `project_foreign_strict_parse flags, in non-test files, input from outside teamchat's
protocol handed to teamchat's strict wire parser: a call of internal/teamchat/proto's Parse, or
of a function (of any package) that hands that parameter on to it, whose argument holds
stdin or an environment variable.

Stdin is the result of internal/stdinread's Read or ReadLine (project_stdin_seam holds every
read of stdin to that package). An environment variable is os.Getenv, os.LookupEnv, or a call of
a function value named Getenv or LookupEnv (an Env's). A value holds one when it is built from
one: a local assigned an expression that names it (a slice of it, a concatenation), a
conversion of it, a function of package bytes or strings given it, through a closure, to a
fixed point; a function whose result holds one (of the package, or another one by
a fact) is a source too. Only strings and byte slices carry it.

proto.Parse is the Matrix wire's parser: no repeated key, integers only within 2^53-1, valid
UTF-8, no lone surrogate escape, nothing after the value. A foreign producer (Claude Code's
hook payload) follows none of these rules for the fields teamchat never reads, so one of them
anywhere in the document refused the whole payload, and the fields read were lost: every
SessionStart was unusable, the link never said started, and every /exit ended 78
(fedora-desktop#394).

Not judged: proto.ForeignObject (the remedy), tests, and stdin in the packages whose stdin is a
document teamchat specifies and writes, decoded to its every key (protocolStdin in the rule); an
environment variable there is still judged.

Not caught: a file a foreign program writes (a path does not say who wrote it), a foreign
program's output read through execx, a socket's answer, a value kept in a struct field or a map
and parsed elsewhere, a value handed through a function of the program that returns it changed,
a function value.

How to fix: decode the object with proto.ForeignObject, naming the keys read; each key read is
parsed strictly and refused if repeated, and every other member is skipped whatever its form.`,
	Run:        perPass(runForeignStrictParse),
	ResultType: noResultType,
	FactTypes:  []analysis.Fact{new(foreignParseFact), new(foreignSourceFact)},
}

// foreignParseFact marks a function that hands its parameters at Params to proto.Parse.
type foreignParseFact struct{ Params []int }

func (*foreignParseFact) AFact() {}

func (f *foreignParseFact) String() string {
	return "strictly parses parameters " + indexWords(f.Params)
}

// foreignSourceFact marks a function whose result holds input from outside the protocol.
type foreignSourceFact struct{ From string }

func (*foreignSourceFact) AFact() {}

func (f *foreignSourceFact) String() string { return "returns " + f.From }

// origin is what a value is built from: the enclosing function's parameters, and the first
// foreign source.
type origin struct {
	params []int
	from   string
}

func (o *origin) add(p origin) {
	for _, i := range p.params {
		if !slices.Contains(o.params, i) {
			o.params = append(o.params, i)
		}
	}
	if o.from == "" {
		o.from = p.from
	}
}

func (o origin) empty() bool { return len(o.params) == 0 && o.from == "" }

type foreignRun struct {
	pass    *analysis.Pass
	parses  map[*types.Func][]int
	sources map[*types.Func]string
	// stdinOwned is a package whose stdin is a teamchat protocol: its stdin is not judged, an
	// environment variable still is.
	stdinOwned bool
}

func runForeignStrictParse(pass *analysis.Pass) {
	r := &foreignRun{pass: pass, parses: map[*types.Func][]int{}, sources: map[*types.Func]string{}}
	var decls []*ast.FuncDecl
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				decls = append(decls, fd)
			}
		}
	}
	for range foreignRounds {
		changed := false
		for _, fd := range decls {
			if r.judge(fd, false) {
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	r.stdinOwned = slices.ContainsFunc(protocolStdin, func(p struct{ pkg, producer string }) bool { return ownedBy(pass.Pkg.Path(), p.pkg) })
	for _, fd := range decls {
		r.judge(fd, true)
	}
	for fn, params := range r.parses {
		pass.ExportObjectFact(fn, &foreignParseFact{Params: params})
	}
	for fn, from := range r.sources {
		pass.ExportObjectFact(fn, &foreignSourceFact{From: from})
	}
}

// judge follows one function's values to a fixed point, records what it parses and returns,
// and reports a foreign value parsed when report is set; true is a summary that grew.
func (r *foreignRun) judge(fd *ast.FuncDecl, report bool) bool {
	fn, ok := r.pass.TypesInfo.Defs[fd.Name].(*types.Func)
	if !ok {
		return false
	}
	if fn.Name() == foreignRemedy && fn.Pkg() != nil && ownedBy(fn.Pkg().Path(), teamchatProtoPkg) {
		return false
	}
	taint := r.params(fd)
	r.spread(fd.Body, taint)
	grew := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if call, isCall := n.(*ast.CallExpr); isCall && r.sink(fn, call, taint, report) {
			grew = true
		}
		return true
	})
	if r.sources[fn] == "" {
		if from := r.returned(fd.Body, taint); from != "" {
			r.sources[fn] = from
			grew = true
		}
	}
	return grew
}

// sink judges one call in fn: a foreign value it hands the parser is reported when report is
// set, and a parameter of fn it hands on is recorded; true is a parameter newly recorded.
func (r *foreignRun) sink(fn *types.Func, call *ast.CallExpr, taint map[types.Object]origin, report bool) bool {
	grew := false
	for _, p := range r.sinkParams(call) {
		if p >= len(call.Args) {
			continue
		}
		o := r.originOf(call.Args[p], taint)
		if o.from != "" && report && (o.from != fromStdin || !r.stdinOwned) {
			r.pass.Reportf(call.Pos(), "%s%s", o.from, foreignParseFix)
		}
		for _, i := range o.params {
			if !slices.Contains(r.parses[fn], i) {
				r.parses[fn] = append(r.parses[fn], i)
				grew = true
			}
		}
	}
	return grew
}

// params is each carrying parameter's origin: itself.
func (r *foreignRun) params(fd *ast.FuncDecl) map[types.Object]origin {
	taint := map[types.Object]origin{}
	i := 0
	for _, field := range fd.Type.Params.List {
		names := field.Names
		if len(names) == 0 {
			i++
			continue
		}
		for _, name := range names {
			if obj := r.pass.TypesInfo.Defs[name]; obj != nil && carries(obj.Type()) {
				taint[obj] = origin{params: []int{i}}
			}
			i++
		}
	}
	return taint
}

// spread follows assignments in body (closures included) until no local gains an origin.
func (r *foreignRun) spread(body *ast.BlockStmt, taint map[types.Object]origin) {
	for range foreignRounds {
		changed := false
		assign := func(lhs ast.Expr, o origin) {
			if r.gain(lhs, o, taint) {
				changed = true
			}
		}
		ast.Inspect(body, func(n ast.Node) bool {
			switch s := n.(type) {
			case *ast.AssignStmt:
				r.pairs(s.Lhs, s.Rhs, taint, assign)
			case *ast.ValueSpec:
				lhs := make([]ast.Expr, len(s.Names))
				for i, name := range s.Names {
					lhs[i] = name
				}
				r.pairs(lhs, s.Values, taint, assign)
			}
			return true
		})
		if !changed {
			return
		}
	}
}

// gain adds o to the local lhs names when it carries a document; true is an origin it gained.
func (r *foreignRun) gain(lhs ast.Expr, o origin, taint map[types.Object]origin) bool {
	id, ok := lhs.(*ast.Ident)
	if !ok || o.empty() {
		return false
	}
	obj := r.pass.TypesInfo.ObjectOf(id)
	if obj == nil || !carries(obj.Type()) {
		return false
	}
	cur := taint[obj]
	before, hadFrom := len(cur.params), cur.from != ""
	cur.add(o)
	if len(cur.params) == before && (cur.from != "") == hadFrom {
		return false
	}
	taint[obj] = cur
	return true
}

// pairs hands each left side the origin of its right side; one call on the right gives every
// left side its origin.
func (r *foreignRun) pairs(lhs, rhs []ast.Expr, taint map[types.Object]origin, assign func(ast.Expr, origin)) {
	if len(lhs) == len(rhs) {
		for i := range lhs {
			assign(lhs[i], r.originOf(rhs[i], taint))
		}
		return
	}
	if len(rhs) == 1 {
		o := r.originOf(rhs[0], taint)
		for _, l := range lhs {
			assign(l, o)
		}
	}
}

// originOf is what e is built from: the origins of the names in it, and a source it calls. A
// call's result is built from its arguments only for a conversion or a function of package
// bytes or strings: a file read from a path is not the path.
func (r *foreignRun) originOf(e ast.Expr, taint map[types.Object]origin) origin {
	var o origin
	ast.Inspect(e, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.Ident:
			if obj := r.pass.TypesInfo.Uses[x]; obj != nil {
				o.add(taint[obj])
			}
		case *ast.CallExpr:
			if from := r.sourceOf(x); from != "" {
				o.add(origin{from: from})
				return false
			}
			return r.passesThrough(x)
		}
		return true
	})
	return o
}

// passesThrough is whether call's result is built from its arguments: a conversion, or a
// function of package bytes or strings.
func (r *foreignRun) passesThrough(call *ast.CallExpr) bool {
	if tv, ok := r.pass.TypesInfo.Types[call.Fun]; ok && tv.IsType() {
		return true
	}
	f := calledFunc(r.pass, call)
	return f != nil && f.Pkg() != nil && (f.Pkg().Path() == "bytes" || f.Pkg().Path() == stringsPkg)
}

// sourceOf is the foreign source a call returns, or "".
func (r *foreignRun) sourceOf(call *ast.CallExpr) string {
	f := calledFunc(r.pass, call)
	if f == nil {
		return envValueCall(r.pass, call)
	}
	switch {
	case isPkgFunc(f, "os", "Getenv"), isPkgFunc(f, "os", "LookupEnv"), isPkgFunc(f, "syscall", "Getenv"):
		return fromEnv
	case f.Pkg() != nil && ownedBy(f.Pkg().Path(), stdinreadPkg) && (f.Name() == "Read" || f.Name() == "ReadLine"):
		return fromStdin
	}
	if from := r.sources[f]; from != "" {
		return from
	}
	var fact foreignSourceFact
	if f.Pkg() != r.pass.Pkg && r.pass.ImportObjectFact(f, &fact) {
		return fact.From
	}
	return ""
}

// envValueCall is fromEnv for a call of a function value named Getenv or LookupEnv.
func envValueCall(pass *analysis.Pass, call *ast.CallExpr) string {
	var id *ast.Ident
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		id = fn.Sel
	case *ast.Ident:
		id = fn
	default:
		return ""
	}
	if _, isVar := pass.TypesInfo.Uses[id].(*types.Var); isVar && (id.Name == "Getenv" || id.Name == "LookupEnv") {
		return fromEnv
	}
	return ""
}

// sinkParams are the arguments of call that reach proto.Parse.
func (r *foreignRun) sinkParams(call *ast.CallExpr) []int {
	f := calledFunc(r.pass, call)
	if f == nil || f.Pkg() == nil {
		return nil
	}
	inProto := ownedBy(f.Pkg().Path(), teamchatProtoPkg)
	if inProto && f.Name() == foreignRemedy {
		return nil
	}
	if inProto && isPkgFunc(f, f.Pkg().Path(), "Parse") {
		return []int{0}
	}
	if params, ok := r.parses[f]; ok {
		return params
	}
	var fact foreignParseFact
	if f.Pkg() != r.pass.Pkg && r.pass.ImportObjectFact(f, &fact) {
		return fact.Params
	}
	return nil
}

// returned is the foreign source a return of body (not of a closure in it) hands back.
func (r *foreignRun) returned(body *ast.BlockStmt, taint map[types.Object]origin) string {
	from := ""
	ast.Inspect(body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			for _, res := range s.Results {
				if tv, ok := r.pass.TypesInfo.Types[res]; ok && !carries(tv.Type) {
					continue
				}
				if o := r.originOf(res, taint); o.from != "" && from == "" {
					from = o.from
				}
			}
		}
		return true
	})
	return from
}

// carries is whether a value of t can hold a document: a string or a byte slice.
func carries(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Info()&types.IsString != 0
	case *types.Slice:
		b, ok := u.Elem().Underlying().(*types.Basic)
		return ok && b.Kind() == types.Byte
	}
	return false
}
