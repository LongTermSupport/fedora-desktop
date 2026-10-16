package godeps

import (
	"slices"
	"strings"
	"testing"
)

const pseudoVersion = "v0.0.0-20261008173134-6b8d4baf91b4"

func goModOf(body string) string {
	return "module ex.com/me\n\ngo 1.26.0\n\n" + body
}

func mustAllow(t *testing.T, text string) allowlist {
	t.Helper()
	a, err := parseAllowlist(text)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func judged(t *testing.T, goMod, allow string) verdict {
	t.Helper()
	v, err := judge([]byte(goMod), mustAllow(t, allow))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func hasFinding(v verdict, part string) bool {
	return slices.ContainsFunc(v.findings, func(f string) bool { return strings.Contains(f, part) })
}

func TestEveryRequirementAllowlistedPasses(t *testing.T) {
	v := judged(t, goModOf("require (\n\tex.com/a v1.2.3\n\tex.com/b v0.4.0 // indirect\n)\n"),
		"module\tex.com/a\tdirect\nmodule\tex.com/b\tindirect\n")
	if len(v.findings) != 0 {
		t.Fatalf("findings = %v", v.findings)
	}
	if v.modules != 2 || v.pseudo != 0 || v.replaced != 0 {
		t.Fatalf("counts = %+v", v)
	}
}

// G3: a direct or an indirect requirement with no module row is a finding.
func TestARequirementNotAllowlistedIsAFinding(t *testing.T) {
	v := judged(t, goModOf("require (\n\tex.com/a v1.2.3\n\tex.com/b v0.4.0 // indirect\n)\n"), "module\tex.com/a\tdirect\n")
	if len(v.findings) != 1 || !hasFinding(v, "ex.com/b") || !hasFinding(v, "not on the allowlist") {
		t.Fatalf("findings = %v", v.findings)
	}
}

// G4: a pseudo-version needs its own row.
func TestAPseudoVersionNeedsAPseudoRow(t *testing.T) {
	mod := goModOf("require ex.com/a " + pseudoVersion + "\n")
	v := judged(t, mod, "module\tex.com/a\twhy\n")
	if len(v.findings) != 1 || !hasFinding(v, "pseudo-version") || !hasFinding(v, "ex.com/a") {
		t.Fatalf("findings = %v", v.findings)
	}
	v = judged(t, mod, "module\tex.com/a\twhy\npseudo\tex.com/a\tno tag has the fix\n")
	if len(v.findings) != 0 || v.pseudo != 1 {
		t.Fatalf("findings = %v, pseudo = %d", v.findings, v.pseudo)
	}
}

// G5: a replace directive needs its own row.
func TestAReplaceNeedsAReplaceRow(t *testing.T) {
	mod := goModOf("require ex.com/a v1.0.0\n\nreplace ex.com/a => ex.com/fork v1.0.1\n")
	v := judged(t, mod, "module\tex.com/a\twhy\n")
	if len(v.findings) != 1 || !hasFinding(v, "replace") || !hasFinding(v, "ex.com/a") {
		t.Fatalf("findings = %v", v.findings)
	}
	v = judged(t, mod, "module\tex.com/a\twhy\nreplace\tex.com/a\ta fix upstream has not released\n")
	if len(v.findings) != 0 || v.replaced != 1 {
		t.Fatalf("findings = %v, replaced = %d", v.findings, v.replaced)
	}
}

// G6: a row go.mod does not need is a finding, whatever its kind.
func TestARowGoModDoesNotNeedIsAFinding(t *testing.T) {
	v := judged(t, goModOf("require ex.com/a v1.0.0\n"),
		"module\tex.com/a\twhy\nmodule\tex.com/gone\twhy\npseudo\tex.com/a\twhy\nreplace\tex.com/a\twhy\n")
	for _, want := range []string{"module ex.com/gone", "pseudo ex.com/a", "replace ex.com/a"} {
		if !hasFinding(v, want) {
			t.Errorf("no finding for %q in %v", want, v.findings)
		}
	}
	if len(v.findings) != 3 {
		t.Fatalf("findings = %v", v.findings)
	}
}

func TestNoRequirementsAndAnEmptyAllowlistPass(t *testing.T) {
	v := judged(t, goModOf(""), "# nothing allowed\n")
	if len(v.findings) != 0 || v.modules != 0 {
		t.Fatalf("verdict = %+v", v)
	}
}

// G2: go.mod is parsed, and one that does not parse is an error, not a finding.
func TestAnUnparseableGoModIsAnError(t *testing.T) {
	if _, err := judge([]byte("module\nrequire (\n"), allowlist{}); err == nil {
		t.Fatal("accepted")
	}
}
