package rules

import (
	"go/ast"
	"go/constant"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// checkoutPinPkg is the one package that may name a checkout file the host applies: it reads each
// through the approval a person recorded outside the checkout (Plan 00266 Task A.1).
const checkoutPinPkg = "internal/checkoutpin"

// checkoutPinNames are the checkout files the host launcher applies, which a container session
// can write: the project mounts (binds of the session's next launch) and the compose files and
// their overrides (`up -d`, run on the host).
var checkoutPinNames = []string{
	".claude/ccy/mounts",
	"docker-compose.yml", "docker-compose.yaml", "podman-compose.yml", "podman-compose.yaml", "compose.yml", "compose.yaml",
	"docker-compose.override.yml", "docker-compose.override.yaml", "podman-compose.override.yml",
	"podman-compose.override.yaml", "compose.override.yml", "compose.override.yaml",
}

const checkoutPinFix = " (project_checkout_config_pin): a container session can write this file, and the host applies it; read it only through internal/checkoutpin, which refuses a file no person approved"

// CheckoutConfigPin confines the naming of a checkout file the host applies to internal/checkoutpin.
var CheckoutConfigPin = &analysis.Analyzer{
	Name: "project_checkout_config_pin",
	Doc: `project_checkout_config_pin flags, in src-go's internal/ and cmd/ trees, every constant
string expression whose value names a checkout file the host launcher applies, outside
internal/checkoutpin:

- .claude/ccy/mounts, the project's extra binds, which the launcher adds to the session's next
  container;
- the compose files (docker-compose, podman-compose and compose, .yml and .yaml) and their
  .override files, which the launcher runs ` + "`up -d`" + ` on, as the host user.

A container session writes its checkout, so it writes these files, and a host that applies them
unasked lets the session widen its own next launch: bind a host path it could not reach, or run a
compose stack with any volume (Plan 00266 Phase A, "host launcher applies container-writable
checkout config"). internal/checkoutpin reads them only when their content is the content a
person approved, whose hash it keeps outside the checkout. The defect the rule exists for was
restore's own list of compose files.

A constant expression is judged whole, so a name spelt in pieces with + is the name; it is
reported where it is written, not at each use of a constant that holds it. Test files are not
judged: a test plants these files to prove the refusal. The nested qa module is not judged: its
gates scan these files and never apply them.

Not caught: a name built at run time (a loop over suffixes, fmt.Sprintf), a path joined from
pieces that are each only part of a name (".claude", "ccy", "mounts"), and a file read through a
directory listing.

How to fix: ask internal/checkoutpin (its names, its Mounts and Compose readers), never the file.`,
	Run:        perPass(runCheckoutConfigPin),
	ResultType: noResultType,
}

func runCheckoutConfigPin(pass *analysis.Pass) {
	path := pass.Pkg.Path()
	judged := inTree(path, "internal") || inTree(path, "cmd")
	if !judged || inTree(path, "qa") || ownedBy(path, checkoutPinPkg) {
		return
	}
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			e, ok := n.(ast.Expr)
			if !ok {
				return true
			}
			if _, ident := e.(*ast.Ident); ident {
				return true
			}
			tv, ok := pass.TypesInfo.Types[e]
			if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
				return true
			}
			if value := constant.StringVal(tv.Value); slices.Contains(checkoutPinNames, value) {
				pass.Reportf(e.Pos(), "%q, a checkout file the host applies, named outside internal/checkoutpin%s", value, checkoutPinFix)
			}
			return false
		})
	}
}
