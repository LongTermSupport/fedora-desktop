package gates

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeTable(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "qa-gates.tsv")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const goodTable = `# a comment

gate	toolchain	scripts/qa-toolchain.bash	QA tool versions do not match	detail:TOOLCHAIN-OK .+
stage	bash	scripts/qa-bash.bash	ERROR: Missing required tools.
drift	deployed-drift	scripts/qa-deployed-drift.bash	repo-owned scripts differ
helpers	helper-tests	scripts/qa-helper-tests.bash	the helper unit suite failed
gate	suite-a	scripts/test-a.bash	suite a failed	count
gate	compat	python3 -m helpers.gnome.check_extension_compat	extension drift	detail:All [0-9]+ ok	detail:two
`

func TestLoadReadsTheRowsInOrder(t *testing.T) {
	got, err := Load(writeTable(t, goodTable))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(got))
	for i, g := range got {
		names[i] = string(g.Kind) + ":" + g.Name
	}
	want := []string{"gate:toolchain", "stage:bash", "drift:deployed-drift", "helpers:helper-tests", "gate:suite-a", "gate:compat"}
	if !slices.Equal(names, want) {
		t.Fatalf("got %v", names)
	}
	if got[0].Text != "QA tool versions do not match" || len(got[0].Readers) != 1 || got[0].Readers[0].Detail == nil {
		t.Fatalf("toolchain row: %+v", got[0])
	}
	if len(got[4].Readers) != 1 || got[4].Readers[0].Detail != nil {
		t.Fatalf("a count reader has no pattern: %+v", got[4].Readers)
	}
	if len(got[5].Readers) != 2 || got[5].Line != 8 {
		t.Fatalf("two detail readers and the line number: %+v", got[5])
	}
}

func TestLoadRefusesAMalformedTableWithTheLineNumber(t *testing.T) {
	cases := []struct{ name, row, mentions string }{
		{"a short row", "gate\tx\tscripts/test-x.bash", "want"},
		{"an unknown kind", "check\tx\tscripts/test-x.bash\ttext\tcount", "unknown kind"},
		{"an uppercase name", "gate\tX\tscripts/test-x.bash\ttext\tcount", "name"},
		{"a name with a colon", "gate\tx:y\tscripts/test-x.bash\ttext\tcount", "name"},
		{"an empty command", "gate\tx\t \ttext\tcount", "command"},
		{"an empty text", "gate\tx\tscripts/test-x.bash\t \tcount", "text"},
		{"a gate with no reader", "gate\tx\tscripts/test-x.bash\ttext", "reader"},
		{"an unknown reader", "gate\tx\tscripts/test-x.bash\ttext\tsummary", "reader"},
		{"a pattern that does not compile", "gate\tx\tscripts/test-x.bash\ttext\tdetail:[0-9", "pattern"},
		{"an empty pattern", "gate\tx\tscripts/test-x.bash\ttext\tdetail:", "pattern"},
		{"a reader on a stage", "stage\tx\tscripts/qa-x.bash\ttext\tcount", "reader"},
		{"a reader on a drift gate", "drift\tx\tscripts/qa-x.bash\ttext\tcount", "reader"},
		{"a stage that is not a script", "stage\tx\tpython3 -m x\ttext", "script"},
		{"a helpers gate that is not a script", "helpers\tx\tpython3 -m x\ttext", "script"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Load(writeTable(t, "# header\n"+c.row+"\n"))
			if err == nil || !strings.Contains(err.Error(), c.mentions) || !strings.Contains(err.Error(), ":2:") {
				t.Fatalf("got %v; want a refusal mentioning %q at line 2", err, c.mentions)
			}
		})
	}
}

func TestLoadRefusesADuplicateNameAndAnEmptyTable(t *testing.T) {
	dup := "gate\tx\tscripts/test-x.bash\ttext\tcount\ngate\tx\tscripts/test-y.bash\ttext\tcount\n"
	if _, err := Load(writeTable(t, dup)); err == nil || !strings.Contains(err.Error(), "duplicate") || !strings.Contains(err.Error(), ":2:") {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := Load(writeTable(t, "# nothing\n")); err == nil || !strings.Contains(err.Error(), "no gates") {
		t.Fatalf("empty: %v", err)
	}
	if _, err := Load(filepath.Join(t.TempDir(), "absent.tsv")); err == nil {
		t.Fatal("a missing table must be an error")
	}
}

func TestCommandRunsAScriptThroughBashAndAModuleAsWritten(t *testing.T) {
	gs, err := Load(writeTable(t, goodTable))
	if err != nil {
		t.Fatal(err)
	}
	if got := gs[0].Command("/r"); !slices.Equal(got, []string{"bash", "/r/scripts/qa-toolchain.bash"}) {
		t.Fatalf("script: %v", got)
	}
	if got := gs[5].Command("/r"); !slices.Equal(got, []string{"python3", "-m", "helpers.gnome.check_extension_compat"}) {
		t.Fatalf("module: %v", got)
	}
}

func TestPopulationIsTheScriptRowsMinusThePerFileGatesInOrder(t *testing.T) {
	gs, err := Load(writeTable(t, goodTable+"gate\tsuite-b\tscripts/test-b.bash\tb\tcount\ngate\tsuite-a-again\tscripts/test-a.bash\ta\tcount\n"))
	if err != nil {
		t.Fatal(err)
	}
	got := Population(gs, []string{"qa-toolchain", "qa-bash", "qa-deployed-drift", "qa-helper-tests"})
	if want := []string{"test-a.bash", "test-b.bash"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCheckScriptsNamesTheGateWhoseScriptIsGone(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o750); err != nil {
		t.Fatal(err)
	}
	gs, err := Load(writeTable(t, "gate\tx\tscripts/test-x.bash\ttext\tcount\ngate\tc\tpython3 -m helpers.x\ttext\tcount\n"))
	if err != nil {
		t.Fatal(err)
	}
	err = CheckScripts(root, gs)
	if err == nil || !strings.Contains(err.Error(), "test-x.bash") || !strings.Contains(err.Error(), `"x"`) {
		t.Fatalf("got %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "scripts/test-x.bash"), []byte("#!/usr/bin/bash\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckScripts(root, gs); err != nil {
		t.Fatalf("every script exists now: %v", err)
	}
}

// The real table: every row is well formed, every script exists, and the gates the push gate
// depends on are present in the order that matters.
func TestTheRealTableLoadsAndItsScriptsExist(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	gs, err := Load(filepath.Join(root, "scripts", "qa-gates.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckScripts(root, gs); err != nil {
		t.Fatal(err)
	}
	index := map[string]int{}
	for i, g := range gs {
		index[g.Name] = i
	}
	for _, name := range []string{"toolchain", "go", "bash-limits", "bash", "docs", "helper-tests", "qa-changed-mapping", "version-pins"} {
		if _, ok := index[name]; !ok {
			t.Errorf("the real table has no %q gate", name)
		}
	}
	if index["toolchain"] != 0 || index["go"] >= index["bash"] || index["docs"] >= index["helper-tests"] {
		t.Errorf("the order the verdict depends on moved: %v", index)
	}
}
