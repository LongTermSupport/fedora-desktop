package godeps

import (
	"strings"
	"testing"
)

func TestAllowlistReadsRowsAndSkipsCommentsAndBlankLines(t *testing.T) {
	got, err := parseAllowlist("# a comment\n\nmodule\tex.com/a\tthe engine\npseudo\tex.com/a\tno tag yet\nreplace\tex.com/b\ta fork\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.rows) != 3 {
		t.Fatalf("rows = %v", got.rows)
	}
	if !got.has(kindModule, "ex.com/a") || !got.has(kindPseudo, "ex.com/a") || !got.has(kindReplace, "ex.com/b") {
		t.Fatalf("rows = %v", got.rows)
	}
	if got.has(kindModule, "ex.com/b") {
		t.Fatal("a replace row is not a module row")
	}
	if got.rows[0].line != 3 {
		t.Fatalf("first row on line %d, want 3", got.rows[0].line)
	}
}

// G1: the allowlist is read strictly, and the error names the line.
func TestAllowlistRefusesABadRow(t *testing.T) {
	for name, text := range map[string]string{
		"two fields":     "module\tex.com/a\n",
		"four fields":    "module\tex.com/a\twhy\textra\n",
		"unknown kind":   "allow\tex.com/a\twhy\n",
		"empty module":   "module\t\twhy\n",
		"empty reason":   "module\tex.com/a\t \n",
		"repeated entry": "module\tex.com/a\twhy\nmodule\tex.com/a\tagain\n",
	} {
		_, err := parseAllowlist("# header\n" + text)
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if !strings.Contains(err.Error(), "line ") {
			t.Errorf("%s: %v does not name the line", name, err)
		}
	}
}

func TestTheSameModuleMayHaveRowsOfDifferentKinds(t *testing.T) {
	if _, err := parseAllowlist("module\tex.com/a\twhy\npseudo\tex.com/a\twhy\n"); err != nil {
		t.Fatal(err)
	}
}
