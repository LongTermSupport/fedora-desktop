package godeps

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// kind is what an allowlist row permits.
type kind string

// The kinds of row (SPEC-qa-go-deps.md, the allowlist).
const (
	kindModule  kind = "module"
	kindPseudo  kind = "pseudo"
	kindReplace kind = "replace"
	kindKeepout kind = "keepout"
)

// The number of tab-separated fields of a row: kind, module, reason; a keepout row has the
// package between the module and the reason.
const (
	allowlistFields = 3
	keepoutFields   = 4
)

// keepoutPackage is the package a keepout row may name: a command of src-go.
var keepoutPackage = regexp.MustCompile(`^\./cmd/[a-z0-9][a-z0-9-]*$`)

// row is one allowlist entry and the line it is on; pkg is a keepout row's package.
type row struct {
	kind   kind
	module string
	pkg    string
	reason string
	line   int
}

// allowlist is the parsed src-go/go-deps.allowlist.
type allowlist struct {
	rows []row
}

func (a allowlist) has(k kind, module string) bool {
	for _, r := range a.rows {
		if r.kind == k && r.module == module {
			return true
		}
	}
	return false
}

// parseAllowlist reads the allowlist strictly (G1, GD2): every row is three fields, a known
// kind, a module and a reason, or a keepout row of four with a ./cmd/<name> package; no row is
// listed twice, and a kept-out module has its own module row.
func parseAllowlist(text string) (allowlist, error) {
	var a allowlist
	for i, line := range strings.Split(text, "\n") {
		n := i + 1
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		r, err := parseRow(strings.Split(line, "\t"), n)
		if err != nil {
			return allowlist{}, err
		}
		if slices.ContainsFunc(a.rows, func(o row) bool { return o.kind == r.kind && o.module == r.module && o.pkg == r.pkg }) {
			return allowlist{}, fmt.Errorf("line %d: %s %s is listed twice", n, r.kind, strings.TrimSpace(r.module+" "+r.pkg))
		}
		a.rows = append(a.rows, r)
	}
	for _, r := range a.rows {
		if r.kind == kindKeepout && !a.has(kindModule, r.module) {
			return allowlist{}, fmt.Errorf("line %d: keepout %s has no module row: a module go.mod does not require needs no keepout", r.line, r.module)
		}
	}
	return a, nil
}

func parseRow(fields []string, n int) (row, error) {
	want := allowlistFields
	if kind(fields[0]) == kindKeepout {
		want = keepoutFields
	}
	if len(fields) != want {
		return row{}, fmt.Errorf("line %d: want kind<TAB>module<TAB>reason (keepout: kind<TAB>module<TAB>package<TAB>reason), got %d field(s)", n, len(fields))
	}
	r := row{kind: kind(fields[0]), module: strings.TrimSpace(fields[1]), reason: strings.TrimSpace(fields[len(fields)-1]), line: n}
	switch r.kind {
	case kindModule, kindPseudo, kindReplace:
	case kindKeepout:
		r.pkg = strings.TrimSpace(fields[2])
		if !keepoutPackage.MatchString(r.pkg) {
			return row{}, fmt.Errorf("line %d: keepout package %q is not ./cmd/<name>", n, r.pkg)
		}
	default:
		return row{}, fmt.Errorf("line %d: unknown kind %q (module, pseudo, replace or keepout)", n, r.kind)
	}
	if r.module == "" || r.reason == "" {
		return row{}, fmt.Errorf("line %d: a row needs a module and a reason", n)
	}
	return r, nil
}
