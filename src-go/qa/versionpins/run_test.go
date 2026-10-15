package versionpins

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

const okLine = "vars/version-pins.yml: VERSION-PINS-OK 1 pin(s), 1 with install state tracked, 0 declared untracked — " +
	"COVERAGE: 1 of 1 resolve to a live playbook var"

// V1, V23
func TestV1CommandLineAndTheCleanLine(t *testing.T) {
	for _, flag := range []string{"--help", "-h"} {
		w := newWorld(goodFiles())
		if got := w.run(flag); got != ExitOK || !strings.Contains(w.out.String(), "usage: qa-version-pins") || w.err.Len() != 0 {
			t.Fatalf("%s: %d %q %q", flag, got, w.out.String(), w.err.String())
		}
	}
	w := newWorld(goodFiles())
	if got := w.run("--nope"); got != ExitCannotRun {
		t.Fatalf("unknown: %d", got)
	}
	mustContain(t, "unknown", w.err.String(), "ERROR: unknown argument '--nope'")
	mustContain(t, "unknown", w.err.String(), "usage: qa-version-pins")
	for _, args := range [][]string{{"--root"}, {"--scripts"}, {"--root", "--scripts"}, {"stray"}} {
		w = newWorld(goodFiles())
		if got := w.run(args...); got != ExitCannotRun || !strings.HasPrefix(w.err.String(), "ERROR:") {
			t.Errorf("%v: %d %q", args, got, w.err.String())
		}
	}
	w = expectExit(t, "good", goodFiles(), ExitOK)
	if w.out.String() != okLine+"\n" || w.err.Len() != 0 {
		t.Errorf("clean output:\n%q\n%q", w.out.String(), w.err.String())
	}
}

// V1: the root opened is the one named; --scripts names its parent.
func TestV1TheRootOpenedIsTheOneNamed(t *testing.T) {
	var opened []string
	w := newWorld(goodFiles())
	env := w.env()
	env.Open = func(root string) fs.FS { opened = append(opened, root); return w.mem }
	for _, args := range [][]string{{"--scripts", "/elsewhere/scripts"}, {"--root", "/direct"}, nil} {
		if got := Main(args, env); got != ExitOK {
			t.Fatalf("%v: exit %d\n%s", args, got, w.err.String())
		}
	}
	if strings.Join(opened, " ") != "/elsewhere /direct /repo" {
		t.Fatalf("opened %v", opened)
	}
}

// V2, V3
func TestV2V3StreamsAndTheRoot(t *testing.T) {
	w := newWorld(goodFiles())
	env := w.env()
	env.Stdout = failWriter{}
	if got := Main(nil, env); got != ExitCannotRun {
		t.Fatalf("exit %d", got)
	}
	mustContain(t, "stdout", w.err.String(), "ERROR: cannot write to stdout: closed pipe")

	w = newWorld(goodFiles())
	env = w.env()
	env.Resolve = func(string) (string, error) { return "", fs.ErrNotExist }
	if got := Main(nil, env); got != ExitCannotRun {
		t.Fatalf("exit %d", got)
	}
	mustContain(t, "root", w.err.String(), "ERROR: cannot resolve the repository root")

	w = expectExit(t, "finding", withPlaybook("other: 1\n"), ExitFailed)
	if w.out.Len() != 0 {
		t.Errorf("a finding printed on stdout: %q", w.out.String())
	}
	if strings.Contains(w.err.String(), "/repo") {
		t.Errorf("the checkout path reached stderr:\n%s", w.err.String())
	}
}

// V4
func TestV4TheManifestMustBeAReadableRegularFile(t *testing.T) {
	w := expectExit(t, "absent", map[string]string{"playbooks/a.yml": "a_version: 1\n"}, ExitCannotRun)
	mustContain(t, "absent", w.err.String(), "ERROR: cannot read vars/version-pins.yml")

	files := goodFiles()
	delete(files, manifestPath)
	files["vars/version-pins.yml/inner"] = "x"
	w = expectExit(t, "directory", files, ExitCannotRun)
	mustContain(t, "directory", w.err.String(), "vars/version-pins.yml is not a regular file")

	w = newWorld(goodFiles())
	w.mem[manifestPath] = &fstest.MapFile{Mode: fs.ModeSymlink, Data: []byte("elsewhere.yml")}
	if got := w.run(); got != ExitCannotRun {
		t.Fatalf("symlink: exit %d", got)
	}
	mustContain(t, "symlink", w.err.String(), "vars/version-pins.yml is a symlink")

	w = newWorld(goodFiles())
	w.open = func(f fs.FS) fs.FS { return unreadableFS{FS: f, bad: manifestPath} }
	if got := w.run(); got != ExitCannotRun {
		t.Fatalf("unreadable: exit %d", got)
	}
	mustContain(t, "unreadable", w.err.String(), "ERROR: cannot read vars/version-pins.yml")
}

// V5
func TestV5ABrokenOrMultiDocumentManifestIsExitTwo(t *testing.T) {
	for name, text := range map[string]string{
		"not yaml":       "version_pins: [unclosed\n",
		"tab indent":     "version_pins:\n\t- x\n",
		"two documents":  manifestOf(trackedRow) + "---\n" + manifestOf(trackedRow),
		"binary garbage": "\x00\x01\x02: : :",
	} {
		w := expectExit(t, name, withManifest(text), ExitCannotRun)
		mustContain(t, name, w.err.String(), "ERROR: vars/version-pins.yml is not valid YAML")
		if w.out.Len() != 0 {
			t.Errorf("%s: stdout %q", name, w.out.String())
		}
	}
}

// V6, V25: emptiness is rejected for being empty, not for some other reason.
func TestV6NothingToJudgeIsExitTwoForThatReason(t *testing.T) {
	for name, text := range map[string]string{
		"empty file":      "",
		"comments only":   "# nothing\n",
		"no key":          "other: 1\n",
		"null":            "version_pins:\n",
		"explicit null":   "version_pins: null\n",
		"empty list":      "version_pins: []\n",
		"empty list lazy": "version_pins:\n  []\n",
	} {
		w := expectExit(t, name, withManifest(text), ExitCannotRun)
		mustContain(t, name, w.err.String(), "ERROR: vars/version-pins.yml declares no pins; nothing was checked")
		if strings.Contains(w.err.String(), "finding") {
			t.Errorf("%s: reported as a finding:\n%s", name, w.err.String())
		}
	}
}

// V7
func TestV7ShapeOfTheDocument(t *testing.T) {
	for name, tc := range map[string]struct{ text, want string }{
		"top level list":     {"- a\n- b\n", "vars/version-pins.yml:1: the top level must be a mapping"},
		"top level scalar":   {"hello\n", "the top level must be a mapping"},
		"extra top key":      {manifestOf(trackedRow) + "extra: 1\n", "vars/version-pins.yml:8: unknown top-level key 'extra'"},
		"pins a mapping":     {"version_pins:\n  a: 1\n", "version_pins must be a list, got !!map"},
		"pins a scalar":      {"version_pins: nope\n", "version_pins must be a list, got !!str"},
		"row a scalar":       {"version_pins:\n  - nope\n", "version_pins[0]: each pin must be a mapping, got !!str"},
		"row null":           {"version_pins:\n  -\n", "version_pins[0]: each pin must be a mapping, got !!null"},
		"second row a list":  {manifestOf(trackedRow) + "  - [a]\n", "version_pins[1]: each pin must be a mapping, got !!seq"},
		"row after good one": {manifestOf(trackedRow) + "  - 3\n", "version_pins[1]: each pin must be a mapping, got !!int"},
	} {
		w := expectExit(t, name, withManifest(tc.text), ExitFailed)
		mustContain(t, name, w.err.String(), tc.want)
	}
}

// V8
func TestV8RepeatedKeysAndMergeKeys(t *testing.T) {
	dup := "  - playbook: playbooks/a.yml\n    var: a_version\n    var: other\n    github: o/r\n" +
		"    installed: {kind: command, name: a}\n"
	w := expectExit(t, "row dup", withManifest(manifestOf(dup)), ExitFailed)
	mustContain(t, "row dup", w.err.String(), "vars/version-pins.yml:4: key 'var' is repeated")
	dupInstalled := "  - playbook: playbooks/a.yml\n    var: a_version\n    github: o/r\n" +
		"    installed: {kind: command, name: a, name: b}\n"
	w = expectExit(t, "installed dup", withManifest(manifestOf(dupInstalled)), ExitFailed)
	mustContain(t, "installed dup", w.err.String(), "key 'name' is repeated")
	w = expectExit(t, "top dup", withManifest(manifestOf(trackedRow)+"version_pins: []\n"), ExitFailed)
	mustContain(t, "top dup", w.err.String(), "key 'version_pins' is repeated")
	merged := "  - <<: {github: o/r}\n    playbook: playbooks/a.yml\n    var: a_version\n" +
		"    installed: {kind: command, name: a}\n"
	w = expectExit(t, "merge", withManifest(manifestOf(merged)), ExitFailed)
	mustContain(t, "merge", w.err.String(), "unknown key '<<'")
}

// V8: an alias is followed, not skipped.
func TestV8AnAliasIsFollowed(t *testing.T) {
	text := "x-installed: &inst\n  kind: command\n  name: a\n" +
		"version_pins:\n  - playbook: playbooks/a.yml\n    var: a_version\n    github: o/r\n    installed: *inst\n"
	w := expectExit(t, "alias", withManifest(text), ExitFailed)
	mustContain(t, "alias", w.err.String(), "unknown top-level key 'x-installed'")
	if strings.Contains(w.err.String(), "installed must be a mapping") || strings.Contains(w.err.String(), "no installed") {
		t.Errorf("the alias was not followed:\n%s", w.err.String())
	}
	aliased := "version_pins:\n  - playbook: playbooks/a.yml\n    var: a_version\n    github: &gh o/r\n" +
		"    installed: {kind: command, name: a}\n  - playbook: playbooks/a.yml\n    var: b_version\n    github: *gh\n" +
		"    installed: {kind: untracked, why: because}\n"
	files := withManifest(aliased)
	files["playbooks/a.yml"] = "a_version: 1\nb_version: 2\n"
	w = expectExit(t, "alias ok", files, ExitOK)
	mustContain(t, "alias ok", w.out.String(), "VERSION-PINS-OK 2 pin(s)")
}

// V9
func TestV9AnUnknownRowKeyIsAFinding(t *testing.T) {
	row := strings.Replace(trackedRow, "github: o/r", "githug: o/r", 1)
	w := expectExit(t, "typo", withManifest(manifestOf(row)), ExitFailed)
	mustContain(t, "typo", w.err.String(), "version_pins[0]: unknown key 'githug'")
	mustContain(t, "typo", w.err.String(), "needs a note")
}

// V10
func TestV10FieldTypesAndRequiredFields(t *testing.T) {
	cases := map[string]struct{ row, want string }{
		"playbook empty": {strings.Replace(trackedRow, "playbooks/a.yml", `"  "`, 1), "version_pins[0]: playbook is required"},
		"var empty":      {strings.Replace(trackedRow, "a_version", `""`, 1), "version_pins[0]: var is required"},
		"var null":       {strings.Replace(trackedRow, "var: a_version", "var:", 1), "version_pins[0]: var is required"},
		"playbook int":   {strings.Replace(trackedRow, "playbooks/a.yml", "12", 1), "playbook must be a string, got !!int"},
		"var bool":       {strings.Replace(trackedRow, "a_version", "true", 1), "var must be a string, got !!bool"},
		"github list":    {strings.Replace(trackedRow, "github: o/r", "github: [o/r]", 1), "github must be a string, got !!seq"},
		"note float":     {trackedRow + "    note: 1.5\n", "note must be a string, got !!float"},
		"tag_prefix map": {trackedRow + "    tag_prefix: {a: b}\n", "tag_prefix must be a string, got !!map"},
	}
	for name, tc := range cases {
		w := expectExit(t, name, withManifest(manifestOf(tc.row)), ExitFailed)
		mustContain(t, name, w.err.String(), tc.want)
	}
	// null optional fields read as empty, and values are trimmed.
	ok := strings.Replace(trackedRow, "github: o/r", `github: "  o/r  "`, 1) + "    note:\n    tag_prefix: ~\n"
	expectExit(t, "null optional", withManifest(manifestOf(ok)), ExitOK)
}

// V11
func TestV11RowDelimitersAreRejected(t *testing.T) {
	for name, field := range map[string]string{
		"pipe":    `note: "a|b"`,
		"newline": `note: "a\nb"`,
		"cr":      `tag_prefix: "a\rb"`,
	} {
		w := expectExit(t, name, withManifest(manifestOf(trackedRow+"    "+field+"\n")), ExitFailed)
		mustContain(t, name, w.err.String(), "contains a row delimiter")
	}
	row := strings.Replace(trackedRow, "playbooks/a.yml", `"playbooks/a|b.yml"`, 1)
	w := expectExit(t, "playbook pipe", withManifest(manifestOf(row)), ExitFailed)
	mustContain(t, "playbook pipe", w.err.String(), "playbook contains a row delimiter")
}

// V12
func TestV12GithubIsOwnerSlashRepo(t *testing.T) {
	for _, bad := range []string{"https://github.com/o/r", "justname", "a/b/c", "o/ r", "/r", "o/", "o/r?x"} {
		row := strings.Replace(trackedRow, "github: o/r", "github: '"+bad+"'", 1)
		w := expectExit(t, bad, withManifest(manifestOf(row)), ExitFailed)
		mustContain(t, bad, w.err.String(), "github must be owner/repo")
	}
	row := strings.Replace(trackedRow, "github: o/r", "github: Some-Org.x_y/repo.name-1", 1)
	expectExit(t, "good", withManifest(manifestOf(row)), ExitOK)
}

// V13
func TestV13NoGithubNeedsANote(t *testing.T) {
	bare := strings.Replace(trackedRow, "    github: o/r\n", "", 1)
	w := expectExit(t, "bare", withManifest(manifestOf(bare)), ExitFailed)
	mustContain(t, "bare", w.err.String(), "version_pins[0]: a pin with no github: needs a note")
	expectExit(t, "noted", withManifest(manifestOf(bare+"    note: check by hand\n")), ExitOK)
	w = expectExit(t, "blank note", withManifest(manifestOf(bare+"    note: '   '\n")), ExitFailed)
	mustContain(t, "blank note", w.err.String(), "needs a note")
}

// V14
func TestV14APairIsDeclaredOnce(t *testing.T) {
	w := expectExit(t, "dup", withManifest(manifestOf(trackedRow, trackedRow)), ExitFailed)
	mustContain(t, "dup", w.err.String(), "version_pins[1]: playbooks/a.yml a_version is declared twice (first at version_pins[0])")
	files := withManifest(manifestOf(trackedRow, untrackedRow("playbooks/a.yml", "b_version")))
	files["playbooks/a.yml"] = "a_version: 1\nb_version: 2\n"
	expectExit(t, "same file two vars", files, ExitOK)
}

// V15
func TestV15InstalledBlock(t *testing.T) {
	row := func(installed string) string {
		return "  - playbook: playbooks/a.yml\n    var: a_version\n    github: o/r\n" + installed
	}
	cases := map[string]struct{ installed, want string }{
		"missing":       {"", "version_pins[0]: no installed: block"},
		"scalar":        {"    installed: command\n", "installed must be a mapping, got !!str"},
		"unknown key":   {"    installed: {kind: command, name: a, nam: b}\n", "unknown installed key 'nam'"},
		"bad kind":      {"    installed: {kind: apt, name: a}\n", "installed.kind must be one of [command dkms rpm untracked], got 'apt'"},
		"no kind":       {"    installed: {name: a}\n", "installed.kind must be one of"},
		"untracked why": {"    installed: {kind: untracked}\n", "installed.kind=untracked needs a why"},
		"dkms name":     {"    installed: {kind: dkms}\n", "installed.kind=dkms needs a name"},
		"rpm name":      {"    installed: {kind: rpm, name: ' '}\n", "installed.kind=rpm needs a name"},
		"command name":  {"    installed: {kind: command}\n", "installed.kind=command needs a name"},
		"name int":      {"    installed: {kind: rpm, name: 5}\n", "name must be a string, got !!int"},
	}
	for name, tc := range cases {
		w := expectExit(t, name, withManifest(manifestOf(row(tc.installed))), ExitFailed)
		mustContain(t, name, w.err.String(), tc.want)
	}
	for _, kind := range []string{"dkms", "rpm", "command"} {
		expectExit(t, kind, withManifest(manifestOf(row("    installed: {kind: "+kind+", name: x}\n"))), ExitOK)
	}
}

// V16
func TestV16AtLeastOnePinMustBeTracked(t *testing.T) {
	files := withManifest(manifestOf(untrackedRow("playbooks/a.yml", "a_version")))
	w := expectExit(t, "none tracked", files, ExitFailed)
	mustContain(t, "none tracked", w.err.String(), "no pin tracks the installed version: 1 pin(s) declared, 0 tracked")
	files = withManifest(manifestOf(untrackedRow("playbooks/a.yml", "a_version"), strings.Replace(
		trackedRow, "a_version", "b_version", 1)))
	files["playbooks/a.yml"] = "a_version: 1\nb_version: 2\n"
	w = expectExit(t, "one tracked", files, ExitOK)
	mustContain(t, "one tracked", w.out.String(), "2 pin(s), 1 with install state tracked, 1 declared untracked")
}

// V16, V24: the floor is reported alongside any other finding, not held back until it is fixed.
func TestV16TheFloorIsReportedAlongsideOtherFindings(t *testing.T) {
	typo := strings.Replace(untrackedRow("playbooks/a.yml", "a_version"), "github:", "githug:", 1)
	w := expectExit(t, "typo and no tracked pin", withManifest(manifestOf(typo)), ExitFailed)
	mustContain(t, "typo", w.err.String(), "unknown key 'githug'")
	mustContain(t, "floor", w.err.String(), "no pin tracks the installed version")
	// A tracked pin whose row has another finding still counts as tracked.
	broken := strings.Replace(trackedRow, "github:", "githug:", 1)
	w = expectExit(t, "tracked but typo", withManifest(manifestOf(broken)), ExitFailed)
	if strings.Contains(w.err.String(), "no pin tracks") {
		t.Errorf("a tracked pin was not counted:\n%s", w.err.String())
	}
}

// V17
func TestV17PlaybookPathsStayInsideTheRepository(t *testing.T) {
	for _, bad := range []string{"/etc/passwd", "../outside.yml", "playbooks/../../x", "./playbooks/a.yml", "playbooks//a.yml", "playbooks/", "."} {
		row := strings.Replace(trackedRow, "playbooks/a.yml", "'"+bad+"'", 1)
		files := withManifest(manifestOf(row))
		files["outside.yml"] = "a_version: 1\n"
		w := expectExit(t, bad, files, ExitFailed)
		mustContain(t, bad, w.err.String(), "must be a repository-relative path")
		if strings.Contains(w.err.String(), "does not exist") {
			t.Errorf("%s: the path was opened:\n%s", bad, w.err.String())
		}
	}
}

// V18
func TestV18ThePlaybookMustExistBeRegularAndReadable(t *testing.T) {
	files := goodFiles()
	delete(files, "playbooks/a.yml")
	w := expectExit(t, "missing", files, ExitFailed)
	mustContain(t, "missing", w.err.String(), "version_pins[0]: names a playbook that does not exist: playbooks/a.yml")

	files = goodFiles()
	delete(files, "playbooks/a.yml")
	files["playbooks/a.yml/inner"] = "x"
	w = expectExit(t, "directory", files, ExitFailed)
	mustContain(t, "directory", w.err.String(), "playbooks/a.yml is not a regular file")

	w = newWorld(goodFiles())
	w.mem["playbooks/a.yml"] = &fstest.MapFile{Mode: fs.ModeSymlink, Data: []byte("../elsewhere")}
	if got := w.run(); got != ExitFailed {
		t.Fatalf("symlink: exit %d\n%s", got, w.err.String())
	}
	mustContain(t, "symlink", w.err.String(), "playbooks/a.yml is not a regular file")

	w = newWorld(goodFiles())
	w.open = func(f fs.FS) fs.FS { return unreadableFS{FS: f, bad: "playbooks/a.yml"} }
	if got := w.run(); got != ExitCannotRun {
		t.Fatalf("unreadable: exit %d\n%s", got, w.err.String())
	}
	mustContain(t, "unreadable", w.err.String(), "ERROR: cannot read playbooks/a.yml")
}

// V19, V25: a missing file and an undeclared var fail for different reasons.
func TestV19TheVarMustBeDeclaredWithAValue(t *testing.T) {
	missing := goodFiles()
	delete(missing, "playbooks/a.yml")
	wm := expectExit(t, "missing file", missing, ExitFailed)
	wv := expectExit(t, "undeclared var", withPlaybook("other_version: 1\n"), ExitFailed)
	mustContain(t, "undeclared", wv.err.String(), "version_pins[0]: names 'a_version', which playbooks/a.yml no longer declares")
	if strings.Contains(wv.err.String(), "does not exist") || strings.Contains(wm.err.String(), "no longer declares") {
		t.Errorf("the two failures read alike:\n%s\n%s", wm.err.String(), wv.err.String())
	}
	for name, text := range map[string]string{
		"comment only":   "# a_version: 1\nother: 2\n",
		"prefix of var":  "a_version_extra: 1\n",
		"suffix of var":  "xa_version: 1\n",
		"no colon":       "a_version 1\n",
		"as a value":     "other: a_version: 1\n",
		"after text":     "key: a_version: 1\n",
		"hash after var": "#a_version: 1\n",
	} {
		w := expectExit(t, name, withPlaybook(text), ExitFailed)
		mustContain(t, name, w.err.String(), "no longer declares")
	}
	for name, text := range map[string]string{
		"indented":   "vars:\n  a_version: \"1\"\n",
		"spaced":     "a_version   :   1\n",
		"single":     "a_version: '1'\n",
		"bare":       "a_version: 1.2.3\n",
		"after junk": "x: 1\n\na_version: 4\n",
		"tabbed":     "vars:\n  a_version:\t5\n",
	} {
		expectExit(t, name, withPlaybook(text), ExitOK)
	}
}

// V19: the var is literal text, never a pattern.
func TestV19TheVarIsLiteral(t *testing.T) {
	row := strings.Replace(trackedRow, "a_version", "a.version", 1)
	files := withManifest(manifestOf(row))
	files["playbooks/a.yml"] = "axversion: 2\n"
	w := expectExit(t, "dot as wildcard", files, ExitFailed)
	mustContain(t, "dot", w.err.String(), "names 'a.version', which playbooks/a.yml no longer declares")
	for _, meta := range []string{"a+", "a[", "(a", "a*b", `a\`} {
		row = strings.Replace(trackedRow, "a_version", "'"+meta+"'", 1)
		files = withManifest(manifestOf(row))
		files["playbooks/a.yml"] = "aaaa: 1\n"
		w = expectExit(t, meta, files, ExitFailed)
		mustContain(t, meta, w.err.String(), "which playbooks/a.yml no longer declares")
	}
	files = withManifest(manifestOf(row))
	files["playbooks/a.yml"] = "a\\: 1\n"
	expectExit(t, "literal backslash", files, ExitOK)
}

// V19: an empty value is a finding; the first declaration is the one read.
func TestV19AnEmptyValueIsAFindingAndTheFirstLineWins(t *testing.T) {
	for name, text := range map[string]string{
		"nothing":      "a_version:\n",
		"blank":        "a_version:    \n",
		"double quote": "a_version: \"\"\n",
		"single quote": "a_version: ''\n",
		"quotes space": "a_version: ' '\n",
	} {
		w := expectExit(t, name, withPlaybook(text), ExitFailed)
		mustContain(t, name, w.err.String(), "version_pins[0]: declares 'a_version' with no value in playbooks/a.yml")
	}
	// The consumer reads the first line, so an empty first line fails even if a later one has a value.
	w := expectExit(t, "first empty", withPlaybook("a_version:\nvars:\n  a_version: 1\n"), ExitFailed)
	mustContain(t, "first empty", w.err.String(), "declares 'a_version' with no value")
	expectExit(t, "first valued", withPlaybook("a_version: 1\nvars:\n  a_version:\n"), ExitOK)
}

// V20
func TestV20AYamlTargetMustHoldTheVarAsAParsedKey(t *testing.T) {
	for name, text := range map[string]string{
		"block scalar":   "notes: |\n  a_version: 1\nother: 2\n",
		"folded scalar":  "notes: >-\n  a_version: 1\n",
		"quoted lines":   "notes: \"first\n  a_version: 1\"\n",
		"collection val": "a_version: [1, 2]\n",
		"mapping val":    "a_version: {x: 1}\n",
	} {
		w := expectExit(t, name, withPlaybook(text), ExitFailed)
		mustContain(t, name, w.err.String(), "version_pins[0]: a_version is not a key with a scalar value in playbooks/a.yml")
	}
	for name, text := range map[string]string{
		"top":          "a_version: 1\n",
		"nested":       "- hosts: all\n  vars:\n    a_version: \"2\"\n",
		"in a task":    "- name: x\n  ansible.builtin.set_fact:\n    a_version: 3\n",
		"also in text": "notes: |\n  a_version: 0\nvars:\n  a_version: 1\n",
		"yaml ext":     "a_version: 1\n",
	} {
		expectExit(t, name, withPlaybook(text), ExitOK)
	}
}

// V20: a yml target that does not parse cannot be judged; a line-form file need not be YAML.
func TestV20AnUnparsedYamlTargetIsExitTwoAndOtherNamesAreLineForm(t *testing.T) {
	w := expectExit(t, "broken yml", withPlaybook("a_version: 1\nbad: [unclosed\n"), ExitCannotRun)
	mustContain(t, "broken yml", w.err.String(), "ERROR: playbooks/a.yml is not valid YAML")

	row := strings.Replace(trackedRow, "playbooks/a.yml", "files/share/x.pin", 1)
	files := withManifest(manifestOf(row))
	files["files/share/x.pin"] = "# sha256 lines follow\na_version: 1.4.0\nsha256_x86_64: abc\n  garbage: [unclosed\n"
	expectExit(t, "pin file", files, ExitOK)

	row = strings.Replace(trackedRow, "playbooks/a.yml", "playbooks/a.yaml", 1)
	files = withManifest(manifestOf(row))
	files["playbooks/a.yaml"] = "notes: |\n  a_version: 1\n"
	w = expectExit(t, ".yaml", files, ExitFailed)
	mustContain(t, ".yaml", w.err.String(), "is not a key with a scalar value")
}

// V21
func TestV21AFileIsReadOnceHoweverManyRowsNameIt(t *testing.T) {
	files := withManifest(manifestOf(trackedRow, untrackedRow("playbooks/a.yml", "b_version"), untrackedRow("playbooks/a.yml", "c_version")))
	files["playbooks/a.yml"] = "a_version: 1\nb_version: 2\nc_version: 3\n"
	w := newWorld(files)
	counter := &countingFS{reads: map[string]int{}}
	w.open = func(f fs.FS) fs.FS { counter.FS = f; return counter }
	if got := w.run(); got != ExitOK {
		t.Fatalf("exit %d\n%s", got, w.err.String())
	}
	if counter.reads["playbooks/a.yml"] != 1 {
		t.Errorf("playbooks/a.yml read %d times", counter.reads["playbooks/a.yml"])
	}
	mustContain(t, "count", w.out.String(), "3 pin(s), 1 with install state tracked, 2 declared untracked")
	mustContain(t, "coverage", w.out.String(), "COVERAGE: 3 of 3")
}

// V22: a row that never reached the on-disk check is exit 2, from the counts of the run.
func TestV22CoverageIsCountedNotDeclared(t *testing.T) {
	if err := coverage(3, 3); err != nil {
		t.Errorf("3 of 3: %v", err)
	}
	for _, c := range [][2]int{{3, 2}, {1, 0}, {2, 3}} {
		err := coverage(c[0], c[1])
		if err == nil || !strings.Contains(err.Error(), "declared but") {
			t.Errorf("%v: %v", c, err)
		}
	}
	// Through the whole tool: a pin that fails is still counted as having reached the check.
	files := withManifest(manifestOf(trackedRow, untrackedRow("playbooks/gone.yml", "b_version")))
	w := expectExit(t, "one fails", files, ExitFailed)
	mustContain(t, "one fails", w.err.String(), "ERROR: 1 version-pin finding(s) across 2 pin(s):")
}

// V24: every finding is reported, sorted by line; a problem makes the exit 2 and the findings
// are still printed.
func TestV24AllFindingsAreReportedSortedAndAProblemWinsTheExit(t *testing.T) {
	bad1 := strings.Replace(trackedRow, "var: a_version", "var: x_version", 1)
	bad2 := strings.Replace(trackedRow, "playbooks/a.yml", "playbooks/gone.yml", 1)
	bad2 = strings.Replace(bad2, "a_version", "y_version", 1)
	w := expectExit(t, "two bad", withManifest(manifestOf(bad1, bad2)), ExitFailed)
	e := w.err.String()
	mustContain(t, "header", e, "ERROR: 2 version-pin finding(s) across 2 pin(s):")
	i1 := strings.Index(e, "  vars/version-pins.yml:2: version_pins[0]: names 'x_version'")
	i2 := strings.Index(e, "  vars/version-pins.yml:8: version_pins[1]: names a playbook that does not exist: playbooks/gone.yml")
	if i1 < 0 || i2 < 0 || i1 > i2 {
		t.Errorf("findings missing or out of order:\n%s", e)
	}

	files := withManifest(manifestOf(bad1, trackedRow))
	files["playbooks/a.yml"] = "a_version: 1\nbad: [unclosed\n"
	w = expectExit(t, "problem and finding", files, ExitCannotRun)
	mustContain(t, "problem", w.err.String(), "ERROR: playbooks/a.yml is not valid YAML")
	mustContain(t, "finding", w.err.String(), "names 'x_version', which playbooks/a.yml no longer declares")
}

// The gate is read-only on the tree and starts nothing: the same tree gives the same answer.
func TestRunsAreRepeatable(t *testing.T) {
	a := expectExit(t, "a", goodFiles(), ExitOK)
	b := expectExit(t, "b", goodFiles(), ExitOK)
	if a.out.String() != b.out.String() {
		t.Errorf("%q != %q", a.out.String(), b.out.String())
	}
}
