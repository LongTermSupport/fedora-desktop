package bash

import (
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

const allowlistPath = "scripts/shellcheck-disable.allowlist"

// list is an allowlist file's text from records written "path count codes".
func list(records ...string) string {
	var b strings.Builder
	b.WriteString("# a comment\n")
	for _, r := range records {
		b.WriteString(strings.Join(strings.Fields(r), "\t") + "\n")
	}
	return b.String()
}

// withDisable is a script holding one disable directive after a first line.
func withDisable(codes string) string {
	return "#!/usr/bin/bash\n# shellcheck disable=" + codes + "\necho $x\n"
}

// S16, S19: an unlisted directive fails its file with the line, on stdout and in the report.
func TestAnUnlistedDisableDirectiveFailsItsFile(t *testing.T) {
	w := newWorld(t, map[string]string{"x.bash": withDisable("SC2086")})
	got := w.main()
	expect(t, got, ExitFailed)
	mustContain(t, "stdout", got.stdout, "✗ suppression: x.bash:2 ", "not recorded in "+allowlistPath, "✗ bash: 1/2 files failed")
	rep := w.report()
	if len(rep.Failures) != 1 || rep.Failures[0].File != "x.bash" || !strings.HasPrefix(rep.Failures[0].Error, "directive:2 ") {
		t.Fatalf("failures %+v", rep.Failures)
	}
}

// S16: every placement and spelling shellcheck accepts is found.
func TestDirectivesAreFoundInEveryPlacementAndSpelling(t *testing.T) {
	cases := map[string]string{
		"own line":        "echo\n# shellcheck disable=SC2086\n",
		"indented":        "f() {\n\t  # shellcheck disable=SC2086\n}\n",
		"trailing":        "echo $x # shellcheck disable=SC2086\n",
		"after a command": "true; # shellcheck disable=SC2086\n",
		"no space":        "#shellcheck disable=SC2086\n",
		"wide space":      "#   shellcheck    disable=SC2086\n",
		"other key first": "# shellcheck source=/dev/null disable=SC2086\n",
		"bare number":     "# shellcheck disable=2086\n",
		"lower case":      "# shellcheck disable=sc2086\n",
		"a range":         "# shellcheck disable=SC2000-SC2100\n",
		"several codes":   "# shellcheck disable=SC2086,SC2046\n",
		"reason after":    "# shellcheck disable=SC2086  # because\n",
	}
	for name, body := range cases {
		w := newWorld(t, map[string]string{"x.bash": "#!/usr/bin/bash\n" + body})
		got := w.main()
		if got.code != ExitFailed || !strings.Contains(got.stdout, "✗ suppression: x.bash:") {
			t.Fatalf("%s: exit %d\n%s", name, got.code, got.stdout)
		}
	}
}

// S16: text that is not a directive is not counted.
func TestWhatIsNotADirectiveIsNotFound(t *testing.T) {
	cases := map[string]string{
		"hash inside a word":   "echo foo#shellcheck disable=SC2086\n",
		"a length expansion":   "echo ${#shellcheck} disable=SC2086\n",
		"another word":         "# shellchecker disable=SC2086\n",
		"a sentence":           "# see shellcheck disable=SC2086 for why\n",
		"enable only":          "# shellcheck enable=require-variable-braces\n",
		"source only":          "# shellcheck source=/dev/null\n",
		"no directive at all":  "echo hi\n",
		"disable word, no key": "# shellcheck disable\n",
	}
	for name, body := range cases {
		w := newWorld(t, map[string]string{"x.bash": "#!/usr/bin/bash\n" + body})
		if got := w.main(); got.code != ExitOK || strings.Contains(got.stdout, "suppression") {
			t.Fatalf("%s: exit %d\n%s", name, got.code, got.stdout)
		}
	}
}

// S17: a listed directive passes.
func TestAListedDirectivePasses(t *testing.T) {
	body := list("x.bash 1 SC2086")
	w := newWorld(t, map[string]string{"x.bash": withDisable("SC2086"), allowlistPath: body})
	w.run.baseList = &body
	got := w.main()
	expect(t, got, ExitOK)
	if got.stdout != "✓ bash: 2 files OK\n" {
		t.Fatalf("stdout %q", got.stdout)
	}
}

// S17: the ratchet, per file, against the record.
func TestTheRecordIsAPerFileRatchet(t *testing.T) {
	two := "#!/usr/bin/bash\n# shellcheck disable=SC2086\n# shellcheck disable=SC2086\n"
	cases := []struct {
		name, file, record, want string
	}{
		{"more directives than recorded", two, "x.bash 1 SC2086", "grew"},
		{"a code outside the record", withDisable("SC2046"), "x.bash 1 SC2086", "SC2046"},
		{"fewer directives than recorded", withDisable("SC2086"), "x.bash 2 SC2086", "tighten"},
		{"fewer codes than recorded", withDisable("SC2086"), "x.bash 1 SC2046,SC2086", "tighten"},
		{"no directive left", "echo\n", "x.bash 1 SC2086", "stale"},
		{"the file is gone from the gate", "", "gone.bash 1 SC2086", "stale"},
	}
	for _, c := range cases {
		w := newWorld(t, map[string]string{"x.bash": c.file, allowlistPath: list(c.record)})
		base := list(c.record)
		w.run.baseList = &base
		got := w.main()
		if got.code != ExitFailed || !strings.Contains(got.stdout, "✗ suppression: ") || !strings.Contains(got.stdout, c.want) {
			t.Fatalf("%s: exit %d\n%s", c.name, got.code, got.stdout)
		}
	}
}

// S19: a record for a path that is not a gated file is its own entry, counted in the totals.
func TestAStaleRecordIsItsOwnFailedEntry(t *testing.T) {
	w := newWorld(t, map[string]string{allowlistPath: list("gone.bash 1 SC2086")})
	base := list("gone.bash 1 SC2086")
	w.run.baseList = &base
	got := w.main()
	expect(t, got, ExitFailed)
	mustContain(t, "stdout", got.stdout, "✗ suppression: gone.bash: ", "✗ bash: 1/2 files failed")
	rep := w.report()
	if rep.Summary != (summary{Total: 2, Passed: 1, Failed: 1}) || len(rep.Failures) != 1 || rep.Failures[0].File != "gone.bash" {
		t.Fatalf("report %+v", rep)
	}
}

// S16: disable=all is never allowable, listed or not, alone or among codes.
func TestDisableAllIsNeverAllowed(t *testing.T) {
	for _, codes := range []string{"all", "SC2086,all", "ALL"} {
		w := newWorld(t, map[string]string{"x.bash": withDisable(codes)})
		got := w.main()
		if got.code != ExitFailed || !strings.Contains(got.stdout, "disable=all") {
			t.Fatalf("%s: exit %d\n%s", codes, got.code, got.stdout)
		}
	}
	body := list("x.bash 1 SC2086")
	w := newWorld(t, map[string]string{"x.bash": withDisable("SC2086,all"), allowlistPath: body})
	w.run.baseList = &body
	if got := w.main(); got.code != ExitFailed || !strings.Contains(got.stdout, "disable=all") {
		t.Fatalf("listed: exit %d\n%s", got.code, got.stdout)
	}
}

// S17: an allowlist that is not readable as one is exit 2 and writes nothing.
func TestAMalformedAllowlistIsExitTwo(t *testing.T) {
	for name, body := range map[string]string{
		"two fields":      "x.bash\t1\n",
		"four fields":     "x.bash\t1\tSC2086\textra\n",
		"count not int":   "x.bash\tmany\tSC2086\n",
		"count zero":      "x.bash\t0\tSC2086\n",
		"code malformed":  "x.bash\t1\tfoo\n",
		"all is listed":   "x.bash\t1\tall\n",
		"duplicate":       "x.bash\t1\tSC2086\nx.bash\t1\tSC2086\n",
		"no path":         "\t1\tSC2086\n",
		"codes unsorted":  "x.bash\t1\tSC2086,SC2046\n",
		"codes duplicate": "x.bash\t1\tSC2086,SC2086\n",
	} {
		w := newWorld(t, map[string]string{allowlistPath: body})
		got := w.main()
		if got.code != ExitCannotRun || !strings.HasPrefix(got.stderr, "ERROR: ") || !strings.Contains(got.stderr, "shellcheck-disable.allowlist") {
			t.Fatalf("%s: exit %d\nstderr: %s", name, got.code, got.stderr)
		}
		w.noReport()
	}
}

// S17: the list is compared with the merge-base copy: nothing added, nothing raised.
func TestTheListIsComparedWithTheBase(t *testing.T) {
	body := list("x.bash 2 SC2046,SC2086")
	file := "#!/usr/bin/bash\n# shellcheck disable=SC2046\n# shellcheck disable=SC2086\n"
	cases := map[string]struct{ base, want string }{
		"an entry the base lacks": {list("y.bash 1 SC2086"), "added"},
		"a higher count":          {list("x.bash 1 SC2046,SC2086"), "raised"},
		"a code the base lacks":   {list("x.bash 2 SC2086"), "raised"},
	}
	for name, c := range cases {
		w := newWorld(t, map[string]string{"x.bash": file, "y.bash": withDisable("SC2086"), allowlistPath: body + "y.bash\t1\tSC2086\n"})
		w.run.baseList = &c.base
		got := w.main()
		if got.code != ExitFailed || !strings.Contains(got.stdout, c.want) {
			t.Fatalf("%s: exit %d\n%s", name, got.code, got.stdout)
		}
	}
	w := newWorld(t, map[string]string{"x.bash": file, allowlistPath: body})
	same := body
	w.run.baseList = &same
	expect(t, w.main(), ExitOK)
}

// S17: a base with no list is the commit that introduces it; an unknown default branch is exit 2;
// an empty list needs no base.
func TestTheBaseIsOnlyAskedForWhenTheListHoldsRecords(t *testing.T) {
	body := list("x.bash 1 SC2086")
	w := newWorld(t, map[string]string{"x.bash": withDisable("SC2086"), allowlistPath: body})
	expect(t, w.main(), ExitOK)

	w = newWorld(t, map[string]string{"x.bash": withDisable("SC2086"), allowlistPath: body})
	w.run.noOriginHead = true
	got := w.main()
	expect(t, got, ExitCannotRun)
	mustContain(t, "stderr", got.stderr, "ERROR: ", "git remote set-head origin --auto")
	w.noReport()

	w = newWorld(t, nil)
	w.run.noOriginHead = true
	expect(t, w.main(), ExitOK)
	for _, c := range w.run.cmds("git") {
		if slices.Contains(c.Argv, "symbolic-ref") {
			t.Fatalf("an empty list asked for the base: %v", c.Argv)
		}
	}
}

// S18: a shell= that contradicts the shebang fails the file, and cannot be listed.
func TestShellDirectiveThatContradictsTheShebangFails(t *testing.T) {
	bad := map[string]string{
		"bash on sh":      "#!/bin/sh\n# shellcheck shell=bash\n",
		"sh on bash":      "#!/usr/bin/bash\n# shellcheck shell=sh\n",
		"env bash on ksh": "#!/usr/bin/env ksh\n# shellcheck shell=bash\n",
		"after the code":  "#!/bin/sh\necho hi # shellcheck shell=bash\n",
	}
	for name, text := range bad {
		w := newWorld(t, map[string]string{"x.bash": text})
		got := w.main()
		if got.code != ExitFailed || !strings.Contains(got.stdout, "✗ suppression: x.bash:") || !strings.Contains(got.stdout, "shell=") {
			t.Fatalf("%s: exit %d\n%s", name, got.code, got.stdout)
		}
	}
}

// S18: agreeing, and without a shebang, are fine.
func TestShellDirectiveThatAgreesOrHasNoShebangPasses(t *testing.T) {
	good := map[string]string{
		"bash on bash":         "#!/usr/bin/bash\n# shellcheck shell=bash\n",
		"sh on sh":             "#!/bin/sh\n# shellcheck shell=sh\n",
		"env bash":             "#!/usr/bin/env bash\n# shellcheck shell=bash\n",
		"env with an option":   "#!/usr/bin/env -S bash -e\n# shellcheck shell=bash\n",
		"no shebang":           "# shellcheck shell=bash\necho hi\n",
		"a comment, not a #!":  "# a note\n# shellcheck shell=bash\n",
		"a shebang with flags": "#!/usr/bin/bash -e\n# shellcheck shell=bash\n",
	}
	for name, text := range good {
		w := newWorld(t, map[string]string{"x.bash": text})
		if got := w.main(); got.code != ExitOK {
			t.Fatalf("%s: exit %d\n%s", name, got.code, got.stdout)
		}
	}
}

// S19: stdout order and the file's error string.
func TestDirectiveFindingsFollowTheShellcheckLinesAndJoinTheError(t *testing.T) {
	w := newWorld(t, map[string]string{"x.bash": "#!/bin/sh\n# shellcheck shell=bash\n# shellcheck disable=SC2086\n"})
	w.run.lint = func([]string) proc.Result {
		return lintAnswer(finding("x.bash", 9, "warning", 2155, "Declare"))
	}
	got := w.main()
	expect(t, got, ExitFailed)
	iGate := strings.Index(got.stdout, "✗ shellcheck:")
	iShell := strings.Index(got.stdout, "x.bash:2 ")
	iDisable := strings.Index(got.stdout, "x.bash:3 ")
	iEnd := strings.Index(got.stdout, "✗ bash: 1/")
	if iGate < 0 || iShell < iGate || iDisable < iShell || iEnd < iDisable {
		t.Fatalf("order:\n%s", got.stdout)
	}
	errText := w.report().Failures[0].Error
	if !strings.HasPrefix(errText, "SC2155:9 (warning) Declare; directive:2 ") || !strings.Contains(errText, "; directive:3 ") {
		t.Fatalf("error %q", errText)
	}
}

// S20: the changed-files mode judges only the named files and starts no tool.
func TestDirectivesModeJudgesTheNamedFilesAlone(t *testing.T) {
	w := newWorld(t, map[string]string{"x.bash": withDisable("SC2086"), "y.bash": withDisable("SC2086")})
	got := w.main("--directives", "x.bash")
	expect(t, got, ExitFailed)
	mustContain(t, "stdout", got.stdout, "✗ suppression: x.bash:2 ")
	if strings.Contains(got.stdout, "y.bash") || len(w.run.calls) != 0 {
		t.Fatalf("stdout %q calls %v", got.stdout, w.run.calls)
	}
	w.noReport()

	w = newWorld(t, map[string]string{"x.bash": withDisable("SC2086"), allowlistPath: list("x.bash 1 SC2086", "gone.bash 1 SC2086")})
	got = w.main("--directives", "x.bash")
	if got.code != ExitOK || got.stdout != "" {
		t.Fatalf("a listed file, an unrelated stale record: exit %d\n%s", got.code, got.stdout)
	}

	w = newWorld(t, map[string]string{"x.bash": "echo\n", allowlistPath: list("x.bash 1 SC2086")})
	if got = w.main("--directives", "x.bash"); got.code != ExitFailed || !strings.Contains(got.stdout, "stale") {
		t.Fatalf("a record with no directive left: exit %d\n%s", got.code, got.stdout)
	}

	w = newWorld(t, map[string]string{"x.bash": "#!/bin/sh\n# shellcheck shell=bash\n"})
	if got = w.main("--directives", "x.bash"); got.code != ExitFailed {
		t.Fatalf("shell=: exit %d\n%s", got.code, got.stdout)
	}
}

// S20: usage and unreadable files.
func TestDirectivesModeUsageAndUnreadableFiles(t *testing.T) {
	w := newWorld(t, nil)
	got := w.main("--directives")
	expect(t, got, ExitCannotRun)
	mustContain(t, "stderr", got.stderr, "ERROR: ", "usage: qa-bash")

	w = newWorld(t, nil)
	got = w.main("--directives", "missing.bash")
	expect(t, got, ExitCannotRun)
	mustContain(t, "stderr", got.stderr, "ERROR: ", "missing.bash")
}
