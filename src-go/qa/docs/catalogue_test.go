package docs

import (
	"strings"
	"testing"
)

// named is a document that mentions each name.
func named(names ...string) string {
	return "# Doc\n\n" + strings.Join(names, "\n") + "\n"
}

// D17
func TestD17TheImportsAreReadFromTheYAMLNotFromLines(t *testing.T) {
	w := newWorld(t, map[string]string{
		"playbooks/playbook-main.yml": "---\n# - import_playbook: imports/play-commented.yml\n" +
			"- hosts: localhost\n  tasks: []\n" +
			"- import_playbook: imports/optional/hw/play-b.yml\n" +
			"- name: quoted\n  import_playbook: \"imports/play-c.yml\"\n" +
			"- import_playbook: imports/other.yml\n",
		"docs/playbooks.md":    named("play-b.yml", "play-c.yml", "other.yml"),
		"docs/architecture.md": named("play-b.yml", "play-c.yml", "other.yml"),
	})
	w.wantFindings()
	w.add("docs/playbooks.md", named("play-b.yml", "play-c.yml"))
	w.wantFindings("docs/playbooks.md:0  other.yml  — core playbook is absent from this document")
}

func TestD17APlaybookMainThatCannotBeUsedIsExitTwoNamingIt(t *testing.T) {
	cases := map[string]string{
		"missing":             "",
		"does not parse":      "- import_playbook: [unclosed\n",
		"not a sequence":      "import_playbook: imports/play-a.yml\n",
		"not a string":        "- import_playbook: [a, b]\n",
		"an empty string":     "- import_playbook: \"\"\n",
		"no import at all":    "- hosts: all\n",
		"empty file":          "",
		"only comments":       "# nothing\n",
		"a scalar item":       "- just text\n",
		"a null import value": "- import_playbook:\n",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, map[string]string{"playbooks/playbook-main.yml": text})
			got := w.check()
			wantExit(t, got, ExitCannotRun)
			wantNamed(t, got, "playbooks/playbook-main.yml")
			w.noReport()
		})
	}
}

func TestD17NoImportIsSaidToBeABrokenDiscovery(t *testing.T) {
	w := newWorld(t, map[string]string{"playbooks/playbook-main.yml": "- hosts: all\n"})
	wantStderr(t, w.check(), "parsed 0 import_playbook entries")
}

// D18
func TestD18AnImportedNameMustBeAWholeTokenInBothDocuments(t *testing.T) {
	cases := []struct {
		name, text string
		ok         bool
	}{
		{"bare", "see play-a.yml here", true},
		{"backticks", "`play-a.yml`", true},
		{"in a path", "imports/play-a.yml", true},
		{"in a link", "[play-a.yml](../playbooks/imports/play-a.yml)", true},
		{"end of line", "x play-a.yml", true},
		{"a longer name before", "play-play-a.yml", false},
		{"a dotted name before", "x.play-a.yml", false},
		{"a longer name after", "play-a.ymlx", false},
		{"a hyphen after", "play-a.yml-old", false},
		{"nothing", "no mention", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, doc := range []string{"docs/playbooks.md", "docs/architecture.md"} {
				w := newWorld(t, map[string]string{doc: "# D\n\n" + c.text + "\n"})
				if c.ok {
					w.wantFindings()
				} else {
					w.wantFindings(doc + ":0  play-a.yml  — core playbook is absent from this document")
				}
			}
		})
	}
}

func TestD18BothDocumentsAreJudgedAndAMissingOneIsExitTwo(t *testing.T) {
	w := newWorld(t, map[string]string{"docs/playbooks.md": "# P\n", "docs/architecture.md": "# A\n"})
	w.wantFindings(
		"docs/playbooks.md:0  play-a.yml  — core playbook is absent from this document",
		"docs/architecture.md:0  play-a.yml  — core playbook is absent from this document",
	)
	for _, doc := range []string{"docs/playbooks.md", "docs/architecture.md"} {
		w := newWorld(t, map[string]string{doc: ""})
		got := w.check()
		wantExit(t, got, ExitCannotRun)
		wantNamed(t, got, doc)
	}
}

// D19
func TestD19ATopicFileMustBeAWholeTokenInClaudeMD(t *testing.T) {
	w := newWorld(t, nil)
	w.add("CLAUDE/Notes.md", "# N\n")
	w.add("CLAUDE/core/Daemon.md", "# not a topic file\n")
	w.add("CLAUDE.md", claudeText+"| `CLAUDE/AgentNotes.md` | x |\n")
	w.wantFindings("CLAUDE.md:0  CLAUDE/Notes.md  — topic file has no row in the index")
	w.add("CLAUDE.md", claudeText+"| [Notes](CLAUDE/Notes.md) | x |\n")
	w.wantFindings()
}

func TestD19TheTokenIsThePathUnderCLAUDE(t *testing.T) {
	w := newWorld(t, nil)
	w.add("CLAUDE/Other.md", "# O\n")
	w.add("CLAUDE.md", claudeText+"| `CLAUDE/core/Other.md` | x |\n| Other.md alone | x |\n")
	w.wantFindings("CLAUDE.md:0  CLAUDE/Other.md  — topic file has no row in the index")
	w.add("CLAUDE.md", claudeText+"| `CLAUDE/Other.md` | x |\n")
	w.wantFindings()
}

func TestD19AMissingClaudeMDIsExitTwoAndNoTopicFileIsExitTwo(t *testing.T) {
	w := newWorld(t, map[string]string{"CLAUDE.md": ""})
	got := w.check()
	wantExit(t, got, ExitCannotRun)
	wantNamed(t, got, "CLAUDE.md")
}

func TestD19NoTopicFileIsABrokenDiscovery(t *testing.T) {
	if _, err := topicFiles([]string{"CLAUDE.md", "docs/a.md", "CLAUDE/Plan/x/PLAN.md", "CLAUDE/core/D.md"}); err == nil ||
		!strings.Contains(err.Error(), "found 0 topic files") {
		t.Fatalf("%v", err)
	}
	got, err := topicFiles([]string{"CLAUDE/B.md", "CLAUDE/A.md", "CLAUDE/sub/C.md", "README.md"})
	if err != nil || strings.Join(got, ",") != "A.md,B.md" {
		t.Fatalf("%v %v", got, err)
	}
}

// D20
func TestD20AGateIsNamedByItsScriptItsModuleOrItsTool(t *testing.T) {
	w := newWorld(t, map[string]string{
		"scripts/qa-gates.tsv": gatesText +
			"gate\tmod\tpython3 -m helpers.gnome.check_panel_contract .\ttext\tcount\n" +
			"stage\tst\tscripts/qa-st.bash\ttext\n" +
			"gate\targs\tscripts/with-args.bash --flag x\ttext\tcount\n" +
			"gate\ttool\tgo-qa --root {root}/src-go run\ttext\tdetail:passed\n",
		"CLAUDE/QA.md": "# QA\n\n| a | b |\n| - | - |\n| `g1.bash` | x |\n| `helpers.gnome.check_panel_contract` | x |\n| `qa-st.bash` | x |\n| `with-args.bash` | x |\n| `go-qa` | x |\n",
	})
	w.wantFindings()
}

func TestD20ARefusedTableIsExitTwoNamingTheLine(t *testing.T) {
	cases := map[string]string{
		"a short row":        "gate\tx\n",
		"an unknown kind":    "wat\tx\tscripts/x.bash\ttext\tcount\n",
		"a duplicate":        gatesText + gatesText,
		"no gates":           "# nothing\n",
		"no reader":          "gate\tx\tscripts/x.bash\ttext\n",
		"unnameable command": "gate\tx\ttools/x.py\ttext\tcount\n",
	}
	for name, table := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, map[string]string{"scripts/qa-gates.tsv": table})
			got := w.check()
			wantExit(t, got, ExitCannotRun)
			wantNamed(t, got, "scripts/qa-gates.tsv")
			w.noReport()
		})
	}
	w := newWorld(t, map[string]string{"scripts/qa-gates.tsv": "gate\tx\ttools/x.py\ttext\tcount\n"})
	wantStderr(t, w.check(), `cannot name the gate run by "tools/x.py"`)
	w = newWorld(t, map[string]string{"scripts/qa-gates.tsv": gatesText + "\nwat\n"})
	wantStderr(t, w.check(), "scripts/qa-gates.tsv:3")
	w = newWorld(t, map[string]string{"scripts/qa-gates.tsv": ""})
	got := w.check()
	wantExit(t, got, ExitCannotRun)
	wantNamed(t, got, "scripts/qa-gates.tsv")
}

// D21
func TestD21BothDirectionsAreChecked(t *testing.T) {
	w := newWorld(t, map[string]string{
		"scripts/qa-gates.tsv": gatesText + "gate\tg2\tscripts/g2.bash\ttext\tcount\n",
		"CLAUDE/QA.md":         "# QA\n\n| Script | What |\n| --- | --- |\n| `g1.bash` | x |\n| `old.bash` | x |\n",
	})
	w.wantFindings(
		"CLAUDE/QA.md:0  g2.bash  — gate is run by qa-all.bash but has no row in this document",
		"CLAUDE/QA.md:6  old.bash  — this document lists the gate but qa-all.bash does not run it",
	)
}

func TestD21ARowListedTwiceIsReportedAtItsFirstLine(t *testing.T) {
	w := newWorld(t, map[string]string{
		"CLAUDE/QA.md": "# QA\n\n| Script | What |\n| --- | --- |\n| `g1.bash` | x |\n| `old.bash` | x |\n| `old.bash` | again |\n",
	})
	w.wantFindings("CLAUDE/QA.md:6  old.bash  — this document lists the gate but qa-all.bash does not run it")
}

func TestD21OnlyTableRowsCountNotFencesNorProse(t *testing.T) {
	w := newWorld(t, map[string]string{
		"scripts/qa-gates.tsv": gatesText + "gate\tg2\tscripts/g2.bash\ttext\tcount\n",
		"CLAUDE/QA.md": "# QA\n\n| Script | What |\n| --- | --- |\n| `g1.bash` | x |\n\n" +
			"Prose naming `g2.bash` in a sentence.\n\n```\n| `g2.bash` | in a fence |\n```\n\n    | `g2.bash` | indented |\n",
	})
	w.wantFindings("CLAUDE/QA.md:0  g2.bash  — gate is run by qa-all.bash but has no row in this document")
}

func TestD21ARowIsTheFirstCellBeingExactlyOneCodeSpan(t *testing.T) {
	w := newWorld(t, map[string]string{
		"CLAUDE/QA.md": "# QA\n\n| Script | What |\n| --- | --- |\n| `g1.bash` | x |\n" +
			"| see `not-a-gate.bash` | x |\n| `two` `spans` | x |\n| plain | `g2.bash` |\n| `has space.bash` | x |\n",
	})
	w.wantFindings()
}

func TestD21OnlyTheShapeOfAGateNameIsARow(t *testing.T) {
	w := newWorld(t, map[string]string{
		"CLAUDE/QA.md": "# QA\n\n| Script | What |\n| --- | --- |\n| `g1.bash` | x |\n\n" +
			"| Variable | What |\n| --- | --- |\n| `QA_JSON_OUT` | report |\n| `ruff.toml` | config |\n| `helpers.a.b` | module not run |\n",
	})
	w.wantFindings("CLAUDE/QA.md:11  helpers.a.b  — this document lists the gate but qa-all.bash does not run it")
}

func TestD21ASeveralTablesAreAllRead(t *testing.T) {
	w := newWorld(t, map[string]string{
		"scripts/qa-gates.tsv": gatesText + "gate\tg2\tscripts/g2.bash\ttext\tcount\n",
		"CLAUDE/QA.md":         "# QA\n\n| S | W |\n| - | - |\n| `g1.bash` | x |\n\nBetween.\n\n| S | W |\n| - | - |\n| `g2.bash` | x |\n",
	})
	w.wantFindings()
}

func TestD21ADocumentWithNoRowIsExitTwo(t *testing.T) {
	w := newWorld(t, map[string]string{"CLAUDE/QA.md": "# QA\n\nNo tables here, `g1.bash`.\n"})
	got := w.check()
	wantExit(t, got, ExitCannotRun)
	wantNamed(t, got, "CLAUDE/QA.md")
}

func TestD21AMissingQADocumentIsExitTwo(t *testing.T) {
	w := newWorld(t, map[string]string{"CLAUDE/QA.md": "", "CLAUDE/Other.md": "# O\n"})
	w.add("CLAUDE.md", claudeText+"`CLAUDE/Other.md`\n")
	got := w.check()
	wantExit(t, got, ExitCannotRun)
	wantNamed(t, got, "CLAUDE/QA.md")
}
