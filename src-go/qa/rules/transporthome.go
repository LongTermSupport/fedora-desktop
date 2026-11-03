package rules

import (
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
)

// pkiPkg is teamchat's certificate package.
const pkiPkg = "internal/teamchat/pki"

// transportOwners are the packages that may build a transport or a TLS configuration: matrix,
// which holds every client of a homeserver and decides whom it trusts, and pki, which parses and
// makes the certificates the configuration is built from. The terminator of Plan 00244 X18 will
// be the third, with its own package.
var transportOwners = []string{matrixPkg, pkiPkg}

// transportTrees are what the rule judges: teamchat's packages and its two commands.
var transportTrees = []string{teamchatTree, "cmd/teamchat", "cmd/teamchat-admin"}

// transportTypes are the types whose construction is a trust or transport decision, by package.
var transportTypes = map[string]string{"net/http": "Transport", "crypto/tls": "Config"}

const transportFix = " outside package matrix (project_transport_home): ask matrix for the client or the transport (matrix.NewMember, matrix.LoopbackClient, matrix.PublicTransport), or add the case there"

// TransportHome confines the building of a transport and of a TLS configuration to package
// matrix.
var TransportHome = &analysis.Analyzer{
	Name: "project_transport_home",
	Doc: `project_transport_home flags, in the non-test files of internal/teamchat, cmd/teamchat and
cmd/teamchat-admin and below, the building of an http.Transport or a tls.Config outside package
matrix (and pki, the certificate package):

- a composite literal of either type, by value or by pointer, through any alias, nested or with
  its type elided in a slice, array or map literal;
- new(http.Transport) and new(tls.Config);
- a variable declared with either type by value (var t http.Transport);
- a Clone of either (http.DefaultTransport.(*http.Transport).Clone(), cfg.Clone()).

Test files are not judged: a test builds the server side of a connection and the clients it
puts under test. Nothing is flagged for taking a *tls.Config or an http.RoundTripper from a
caller, or for a certificate pool, a key pair or a listener wrapped in a given configuration.

Whom a client trusts, which TLS versions it speaks and which name it verifies are decided where
the transport is made. A second place that makes one is a second place that can trust the system's
roots, or skip a name check, for a connection to a team's homeserver; teamchat pins the team's
own CA in one (Plan 00244 SPEC-cross-network X16, X19).

Not caught: an http.Client with no Transport, http.DefaultClient and http.Get (all use the
default transport, which trusts the system's roots), a transport built by a function of another
package that this rule does not judge, and reflect.New.

How to fix: ask package matrix for the client or the transport, as matrix.NewMember does for a
homeserver, matrix.LoopbackClient for a probe of the homeserver's own loopback listener and
matrix.PublicTransport for a public https service that is not a homeserver; add a constructor
there for a case it does not cover.`,
	Run:        perPass(runTransportHome),
	ResultType: noResultType,
}

func runTransportHome(pass *analysis.Pass) {
	inScope := slices.ContainsFunc(transportTrees, func(tree string) bool { return inTree(pass.Pkg.Path(), tree) })
	if !inScope || ownedByAny(pass.Pkg.Path(), transportOwners) {
		return
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if what := transportBuilt(pass, n); what != "" {
				pass.Reportf(n.Pos(), "%s built%s", what, transportFix)
			}
			return true
		})
	}
}

// transportBuilt names the type n builds ("http.Transport", "tls.Config"), or "" when n builds
// neither.
func transportBuilt(pass *analysis.Pass, n ast.Node) string {
	switch n := n.(type) {
	case *ast.CompositeLit:
		return transportName(pass.TypesInfo.TypeOf(n))
	case *ast.ValueSpec:
		if n.Type != nil && len(n.Values) == 0 {
			return transportName(pass.TypesInfo.TypeOf(n.Type))
		}
	case *ast.CallExpr:
		return transportCall(pass, n)
	}
	return ""
}

// transportCall is the type a call builds: new(T) or a Clone of one.
func transportCall(pass *analysis.Pass, call *ast.CallExpr) string {
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		if _, builtin := pass.TypesInfo.Uses[fun].(*types.Builtin); builtin && fun.Name == "new" && len(call.Args) == 1 {
			return transportName(pass.TypesInfo.TypeOf(call.Args[0]))
		}
	case *ast.SelectorExpr:
		if m, ok := pass.TypesInfo.Uses[fun.Sel].(*types.Func); ok && m.Name() == "Clone" {
			if recv := receiverNamed(m); recv != nil {
				return transportName(recv)
			}
		}
	}
	return ""
}

// transportName is "http.Transport" or "tls.Config" when t is that type by value, else "".
func transportName(t types.Type) string {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return ""
	}
	if want, found := transportTypes[named.Obj().Pkg().Path()]; found && named.Obj().Name() == want {
		return named.Obj().Pkg().Name() + "." + want
	}
	return ""
}
