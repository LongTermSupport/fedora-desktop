package bashlimits

import (
	"strings"
	"testing"
	"testing/fstest"
)

// maskedLines runs the rule over src and returns the line of each finding, with its shape.
func maskedLines(t *testing.T, src string) []MaskedSubstitution {
	t.Helper()
	got, err := MaskedSubstitutions([]byte(src))
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	return got
}

// Every shape here was run under bash 5.2 with errexit and continued past a failed
// substitution (untracked/scratch evidence in Plan 00201 Task 2.26's journal).
func TestMaskedSubstitutionsMatch(t *testing.T) {
	cases := []struct {
		src   string
		shape string
	}{
		{`x="$(false)$(true)"`, ShapeSeveral},
		{`x=$(false)$(true)`, ShapeSeveral},
		{`x=$(false) y=$(true)`, ShapeSeveral},
		{`x=$(false) y=$(true) z=1`, ShapeSeveral},
		{`a=("$(false)" "$(true)")`, ShapeSeveral},
		{"a=(\n    \"$(false)\"\n    \"$(true)\"\n)", ShapeSeveral},
		{`a+=("$(false)" "$(true)")`, ShapeSeveral},
		{`x=${u:-$(false)}$(true)`, ShapeSeveral},
		{"x=`false``true`", ShapeSeveral},
		{`x=$(( $(false) + $(true) ))`, ShapeSeveral},
		{`a[$(false)]=$(true)`, ShapeSeveral},
		{`x="$(false)" && y=1`, ""},
		{`f() { x="$(false)$(true)"; }`, ShapeSeveral},
		{`y=$(echo "$(x=$(false)$(true); echo "$x")")`, ShapeSeveral},
		{`if x=$(false)$(true); then :; fi`, ShapeSeveral},
		{`x=$(false)$(true) || exit 1`, ShapeSeveral},
		{`local x=$(false)`, ShapeDeclaration},
		{`local x="$(false)"`, ShapeDeclaration},
		{`local -a a=("$(false)")`, ShapeDeclaration},
		{`declare -r x=$(false)`, ShapeDeclaration},
		{`typeset x=$(false)`, ShapeDeclaration},
		{`export X=$(false)`, ShapeDeclaration},
		{`readonly X=$(false)`, ShapeDeclaration},
		{"readonly X=`false`", ShapeDeclaration},
		{`builtin local x=$(false)`, ShapeDeclaration},
		{`command export X=$(false)`, ShapeDeclaration},
		{`\local x=$(false)`, ShapeDeclaration},
		{`X=$(false) true`, ShapePrefix},
		{`X=$(false) Y=1 make all`, ShapePrefix},
	}
	for _, c := range cases {
		got := maskedLines(t, c.src)
		if c.shape == "" {
			if len(got) != 0 {
				t.Errorf("%q: found %v, want none", c.src, got)
			}
			continue
		}
		if len(got) != 1 || got[0].Shape != c.shape || got[0].Line != 1 {
			t.Errorf("%q: found %v, want one %s on line 1", c.src, got, c.shape)
		}
	}
}

// Each of these reports every substitution's failure, or none is held in an assignment.
func TestMaskedSubstitutionsNoMatch(t *testing.T) {
	for _, src := range []string{
		`x=$(false)`,
		`x="$(false)"`,
		`x="prefix-$(false)-suffix"`,
		`x=$(false) y=1`,
		`x=$(false) y=$((1 + 2))`,
		`a=("$(false)")`,
		`a+=("$(false)" literal "$y")`,
		`x=$(false); y=$(true)`,
		"x=$(false)\ny=$(true)",
		`x=$(echo "$(false)")`,
		`x=$(cat <(false) <(true))`,
		`x=$(false) y=<(true)`,
		`local x`,
		`local x=literal y="$z"`,
		`local x; x=$(false)`,
		`export X`,
		`readonly X=1`,
		`true "$(false)" "$(true)"`,
		`X=1 true "$(false)"`,
		`echo "$(false)$(true)"`,
		`builtin echo x=$(false)`,
		`command -v local`,
		`# x=$(false)$(true)`,
		"cat <<EOF\nx=$(false)$(true)\nEOF",
		`[[ -n "$(false)$(true)" ]]`,
		`for x in "$(false)" "$(true)"; do :; done`,
	} {
		if got := maskedLines(t, src); len(got) != 0 {
			t.Errorf("%q: found %v, want none", src, got)
		}
	}
}

func TestMaskedSubstitutionsLinesAndOrder(t *testing.T) {
	src := "#!/usr/bin/bash\nset -e\nx=$(a)$(b)\n\nf() {\n    local y=$(c)\n}\nZ=$(d) run\n"
	got := maskedLines(t, src)
	want := []MaskedSubstitution{{3, ShapeSeveral}, {6, ShapeDeclaration}, {8, ShapePrefix}}
	if len(got) != len(want) {
		t.Fatalf("found %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("finding %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestMaskedSubstitutionsUnparseable(t *testing.T) {
	if _, err := MaskedSubstitutions([]byte("x=$(\n")); err == nil {
		t.Fatal("an unparseable file gave no error")
	}
}

// A template is judged with its Jinja set aside, on the template's own line numbers.
func TestMaskedSubstitutionsTemplate(t *testing.T) {
	src := "{# a comment\nover two lines #}\nx={{ value }}$(a)\ny=$(b)$(c)\n"
	got, err := MaskedSubstitutionsTemplate([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != (MaskedSubstitution{4, ShapeSeveral}) {
		t.Errorf("found %v, want one %s on line 4", got, ShapeSeveral)
	}
}

// The rule judges every bash file it can fix that runs, the exempt ones too; only trees nobody
// edits here (vendored, daemon-owned) and the semgrep fixtures, which never run, are left out,
// and an unparseable exempt file is an error, as a measured one is.
func TestScanTreeFindsMaskedSubstitutions(t *testing.T) {
	bad := "#!/usr/bin/bash\nx=$(a)$(b)\n"
	paths := []string{
		"scripts/tool.bash", "scripts/test-suite.bash", "tests/fixtures/x.bash",
		"CLAUDE/Plan/00001-x/run.bash", "CLAUDE/Plan/Completed/00002-y/run.bash",
		"files/x.sh.j2", "src-go/vendor/m/x.bash", ".claude/skills/hooks-daemon/x.bash",
		"CLAUDE/Plan/mkplan.bash", ".semgrep/rule.bash", "scripts/clean.bash",
	}
	fsys := fstest.MapFS{}
	for _, p := range paths {
		fsys[p] = &fstest.MapFile{Data: []byte(bad)}
	}
	fsys["scripts/clean.bash"] = &fstest.MapFile{Data: []byte("#!/usr/bin/bash\nx=$(a)\n")}
	scan, err := ScanTree(fsys, paths)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"CLAUDE/Plan/00001-x/run.bash", "CLAUDE/Plan/Completed/00002-y/run.bash",
		"files/x.sh.j2", "scripts/test-suite.bash", "scripts/tool.bash", "tests/fixtures/x.bash",
	}
	findings := MaskedFindings(scan)
	if len(findings) != len(want) {
		t.Fatalf("findings = %v, want one in each of %v", findings, want)
	}
	for i, p := range want {
		f := findings[i]
		if f.Path != p || f.Kind != KindMasked || !strings.HasPrefix(f.Msg, p+":2: ") ||
			!strings.Contains(f.Msg, MaskedRuleID) {
			t.Errorf("finding %d = %+v, want %s:2 of kind %s citing %s", i, f, p, KindMasked, MaskedRuleID)
		}
	}

	fsys["tests/fixtures/x.bash"] = &fstest.MapFile{Data: []byte("x=$(\n")}
	if _, err := ScanTree(fsys, paths); err == nil || !strings.Contains(err.Error(), "tests/fixtures/x.bash") {
		t.Errorf("an unparseable exempt file: err = %v, want one naming it", err)
	}
}
