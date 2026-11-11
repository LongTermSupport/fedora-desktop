package rules

import (
	"go/ast"
	"go/constant"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
)

// identityGrammars are proto's functions that judge a GitHub owner or OWNER/REPO by its
// lower-case canonical grammar (A 9.1), and so must be given a name already folded.
var identityGrammars = []string{"ValidOwner", "ValidOwnerRepo", "ParseRef", "DecodeForge"}

// identityFolds are proto's functions whose result is a name folded to its canonical form.
var identityFolds = []string{"LowercaseRefRepo"}

// identityFields are the fields of proto's types that carry a GitHub name, by type name.
var identityFields = map[string][]string{
	"Ping":      {"Ref"},
	"Forge":     {"Org", "Exclude"},
	"ForgeRepo": {"Repo"},
}

const identityFoldFix = " (project_identity_fold): GitHub matches names case-insensitively and a person writes one as GitHub shows it; fold it first (proto.DecodeForgeInput for a forge section, proto.LowercaseRefRepo for a reference)"

// IdentityFold holds a GitHub name from outside teamchat's proto to its fold before proto's
// lower-case grammar judges it.
var IdentityFold = newIdentityFold(identityFoldAllowances)

func newIdentityFold(allow []allowance) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "project_identity_fold",
		Doc: `project_identity_fold flags, in every non-test file outside teamchat's proto package
(internal/teamchat/proto) and outside the identityFoldAllowances table of the rules package:

- a use of proto.ValidOwner, proto.ValidOwnerRepo, proto.ParseRef or proto.DecodeForge, unless it
  is called with a first argument that is a call of proto.LowercaseRefRepo (a function value of
  one is flagged: what it is given cannot be seen);
- a value written to a field that carries a GitHub name (proto.Ping.Ref, proto.Forge.Org,
  proto.Forge.Exclude, proto.ForgeRepo.Repo), in a composite literal or an assignment, unless it
  is a call of proto.LowercaseRefRepo, the constant "" or nil.

A GitHub owner and an OWNER/REPO are stored and sent in one lower-case form (A 9.1), so proto's
grammars for them accept lower case only. GitHub shows names in mixed case and matches them
case-insensitively, so a person writes ExampleOrg; handed to the grammar unfolded, the name is
refused (teamchat-admin team approve refused a team's trust org, Plan 00209), or compared as
another owner. proto owns the fold and the input decoders that apply it; everything else reaches
the grammars through them.

Not flagged: proto's own uses (its canonical decoders of the roster, envelopes and verdicts read
what was folded when it was sent); test files, which state names in the form they mean; a
function in the allowance table, each row a read of the program's own canonical file. Not
caught: a name compared with == outside these functions and fields; a name folded with
strings.ToLower (Unicode, not the ASCII fold A 2.3 fixes) is still flagged, which is intended.

How to fix: fold the name where it enters. A forge section a person or a play wrote is read
with proto.DecodeForgeInput; a reference with proto.LowercaseRefRepo before it is written to a
ping or parsed. A canonical read of the program's own file is a row in identityFoldAllowances,
with its reason.`,
		Run:        perPass(func(pass *analysis.Pass) { runIdentityFold(pass, allow) }),
		ResultType: noResultType,
	}
}

func runIdentityFold(pass *analysis.Pass, allow []allowance) {
	if ownedBy(pass.Pkg.Path(), teamchatProtoPkg) {
		return
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			if allowedIn(pass, funcName(decl), allow) {
				continue
			}
			inspectIdentityFold(pass, decl)
		}
	}
}

func inspectIdentityFold(pass *analysis.Pass, decl ast.Decl) {
	folded := map[*ast.Ident]bool{}
	ast.Inspect(decl, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			if id := calleeIdent(n); id != nil && identityGrammar(pass, id) && len(n.Args) > 0 && isIdentityFold(pass, n.Args[0]) {
				folded[id] = true
			}
		case *ast.Ident:
			if identityGrammar(pass, n) && !folded[n] {
				pass.Reportf(n.Pos(), "proto.%s given a name that is not folded first%s", n.Name, identityFoldFix)
			}
		case *ast.CompositeLit:
			reportIdentityLit(pass, n)
		case *ast.AssignStmt:
			reportIdentityAssign(pass, n)
		}
		return true
	})
}

// calleeIdent is the identifier a call names, or nil.
func calleeIdent(call *ast.CallExpr) *ast.Ident {
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		return fn.Sel
	case *ast.Ident:
		return fn
	}
	return nil
}

// protoFunc is the proto package function an identifier uses, or nil.
func protoFunc(pass *analysis.Pass, id *ast.Ident) *types.Func {
	fn, ok := pass.TypesInfo.Uses[id].(*types.Func)
	if !ok || fn.Pkg() == nil || !ownedBy(fn.Pkg().Path(), teamchatProtoPkg) {
		return nil
	}
	if sig, isSig := fn.Type().(*types.Signature); !isSig || sig.Recv() != nil {
		return nil
	}
	return fn
}

func identityGrammar(pass *analysis.Pass, id *ast.Ident) bool {
	fn := protoFunc(pass, id)
	return fn != nil && slices.Contains(identityGrammars, fn.Name())
}

// isIdentityFold is whether e is a call of one of proto's folds.
func isIdentityFold(pass *analysis.Pass, e ast.Expr) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return false
	}
	id := calleeIdent(call)
	if id == nil {
		return false
	}
	fn := protoFunc(pass, id)
	return fn != nil && slices.Contains(identityFolds, fn.Name())
}

// identityValueOK is whether e may be written to a name field: a fold, "" or nil.
func identityValueOK(pass *analysis.Pass, e ast.Expr) bool {
	if isIdentityFold(pass, e) {
		return true
	}
	tv, ok := pass.TypesInfo.Types[e]
	if !ok {
		return false
	}
	if tv.IsNil() {
		return true
	}
	return tv.Value != nil && tv.Value.Kind() == constant.String && constant.StringVal(tv.Value) == ""
}

// identityType is the name of t when it is one of proto's types with a name field.
func identityType(t types.Type) string {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := types.Unalias(t).(*types.Named)
	if !ok || named.Obj().Pkg() == nil || !ownedBy(named.Obj().Pkg().Path(), teamchatProtoPkg) {
		return ""
	}
	if _, has := identityFields[named.Obj().Name()]; has {
		return named.Obj().Name()
	}
	return ""
}

func reportIdentityField(pass *analysis.Pass, typeName, field string, value ast.Expr) {
	if typeName == "" || !slices.Contains(identityFields[typeName], field) || identityValueOK(pass, value) {
		return
	}
	pass.Reportf(value.Pos(), "a name written to proto.%s.%s that is not folded first%s", typeName, field, identityFoldFix)
}

func reportIdentityLit(pass *analysis.Pass, lit *ast.CompositeLit) {
	typeName := identityType(pass.TypesInfo.TypeOf(lit))
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key, isIdent := kv.Key.(*ast.Ident); isIdent {
			reportIdentityField(pass, typeName, key.Name, kv.Value)
		}
	}
}

func reportIdentityAssign(pass *analysis.Pass, as *ast.AssignStmt) {
	if len(as.Lhs) != len(as.Rhs) {
		return
	}
	for i, lhs := range as.Lhs {
		sel, ok := lhs.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		s, ok := pass.TypesInfo.Selections[sel]
		if !ok || s.Kind() != types.FieldVal {
			continue
		}
		reportIdentityField(pass, identityType(s.Recv()), sel.Sel.Name, as.Rhs[i])
	}
}
