package godeps

import (
	"fmt"
	"strings"
)

// kind is what an allowlist row permits.
type kind string

// The kinds of row (SPEC-qa-go-deps.md, the allowlist).
const (
	kindModule  kind = "module"
	kindPseudo  kind = "pseudo"
	kindReplace kind = "replace"
)

// allowlistFields is the number of tab-separated fields of a row: kind, module, reason.
const allowlistFields = 3

// row is one allowlist entry and the line it is on.
type row struct {
	kind   kind
	module string
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

// parseAllowlist reads the allowlist strictly (G1): every row is three fields, a known kind, a
// module and a reason, and no kind and module appear twice.
func parseAllowlist(text string) (allowlist, error) {
	var a allowlist
	for i, line := range strings.Split(text, "\n") {
		n := i + 1
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != allowlistFields {
			return allowlist{}, fmt.Errorf("line %d: want kind<TAB>module<TAB>reason, got %d field(s)", n, len(fields))
		}
		r := row{kind: kind(fields[0]), module: strings.TrimSpace(fields[1]), reason: strings.TrimSpace(fields[2]), line: n}
		switch r.kind {
		case kindModule, kindPseudo, kindReplace:
		default:
			return allowlist{}, fmt.Errorf("line %d: unknown kind %q (module, pseudo or replace)", n, r.kind)
		}
		if r.module == "" || r.reason == "" {
			return allowlist{}, fmt.Errorf("line %d: a row needs a module and a reason", n)
		}
		if a.has(r.kind, r.module) {
			return allowlist{}, fmt.Errorf("line %d: %s %s is listed twice", n, r.kind, r.module)
		}
		a.rows = append(a.rows, r)
	}
	return a, nil
}
