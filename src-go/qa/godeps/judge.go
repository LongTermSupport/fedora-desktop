package godeps

import (
	"fmt"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

// verdict is what go.mod and the allowlist say together: the findings, and the counts the pass
// line reports.
type verdict struct {
	findings []string
	modules  int
	pseudo   int
	replaced int
}

// judge holds go.mod's requirements and replacements to the allowlist (G2-G6).
func judge(goMod []byte, allow allowlist) (verdict, error) {
	file, err := modfile.Parse(goModFile, goMod, nil)
	if err != nil {
		return verdict{}, err
	}
	var v verdict
	needed := map[row]bool{}
	need := func(k kind, mod, finding string) {
		if allow.has(k, mod) {
			needed[row{kind: k, module: mod}] = true
			return
		}
		v.findings = append(v.findings, finding)
	}
	for _, req := range file.Require {
		mod, version := req.Mod.Path, req.Mod.Version
		v.modules++
		need(kindModule, mod, fmt.Sprintf("%s %s is required but not on the allowlist: add a module row with the reason it is needed, or drop it", mod, version))
		if module.IsPseudoVersion(version) {
			v.pseudo++
			need(kindPseudo, mod, fmt.Sprintf("%s is required at the pseudo-version %s, an untagged commit: require a tagged release, or add a pseudo row saying why none will do", mod, version))
		}
	}
	for _, rep := range file.Replace {
		v.replaced++
		need(kindReplace, rep.Old.Path, fmt.Sprintf("go.mod replaces %s with %s: remove the replace directive, or add a replace row saying why", rep.Old.Path, rep.New.Path))
	}
	for _, r := range allow.rows {
		if !needed[row{kind: r.kind, module: r.module}] {
			v.findings = append(v.findings, fmt.Sprintf("%s:%d: %s %s is allowed but go.mod does not need it: remove the row", allowlistFile, r.line, r.kind, r.module))
		}
	}
	return v, nil
}
