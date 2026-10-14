package planscriptlogging

import (
	"io/fs"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

const okLine = "PLAN-SCRIPT-LOGGING-OK: 1 plan script(s) examined, no offences"

// L1
func TestL1CommandLine(t *testing.T) {
	for _, flag := range []string{"--help", "-h"} {
		w := newWorld(planScript("true\n"))
		if got := w.run(flag); got != ExitOK || !strings.Contains(w.out.String(), "usage: qa-plan-script-logging") || w.err.Len() != 0 {
			t.Fatalf("%s: %d %q %q", flag, got, w.out.String(), w.err.String())
		}
	}
	w := newWorld(planScript("true\n"))
	if got := w.run("--nope"); got != ExitCannotRun {
		t.Fatalf("unknown: %d", got)
	}
	mustContain(t, "unknown", w.err.String(), "ERROR: unknown argument '--nope'")
	mustContain(t, "unknown", w.err.String(), "usage: qa-plan-script-logging")
	for _, args := range [][]string{{"--root"}, {"--scripts"}, {"--root", "--scripts"}, {"stray"}} {
		w = newWorld(planScript("true\n"))
		if got := w.run(args...); got != ExitCannotRun || !strings.HasPrefix(w.err.String(), "ERROR:") {
			t.Errorf("%v: %d %q", args, got, w.err.String())
		}
	}
}

// L1: the root opened is the one named; --scripts names its parent.
func TestL1TheRootOpenedIsTheOneNamed(t *testing.T) {
	var opened []string
	w := newWorld(planScript("true\n"))
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

// L2, L3
func TestL2UnresolvableRootIsExitTwo(t *testing.T) {
	w := newWorld(planScript("true\n"))
	env := w.env()
	env.Resolve = func(string) (string, error) { return "", fs.ErrNotExist }
	if got := Main(nil, env); got != ExitCannotRun {
		t.Fatalf("exit %d", got)
	}
	mustContain(t, "root", w.err.String(), "ERROR: cannot resolve the repository root")
}

// L2
func TestL2AFailedWriteToStdoutIsExitTwo(t *testing.T) {
	w := newWorld(planScript("true\n"))
	env := w.env()
	env.Stdout = failWriter{}
	if got := Main(nil, env); got != ExitCannotRun {
		t.Fatalf("exit %d", got)
	}
	mustContain(t, "stdout", w.err.String(), "ERROR: cannot write to stdout: closed pipe")
}

// L4
func TestL4OnlyActivePlanFoldersAreExamined(t *testing.T) {
	offence := "#!/usr/bin/env bash\nexec > >(tee \"$LOG\") 2>&1\n"
	w := expectExit(t, "completed", map[string]string{
		"CLAUDE/Plan/00001-x/run.bash":           "#!/usr/bin/env bash\ntrue\n",
		"CLAUDE/Plan/Completed/00002-y/run.bash": offence,
		"CLAUDE/Plan/_planlib.inc.bash":          offence,
		"CLAUDE/Plan/README.md":                  "x",
		"CLAUDE/Plan/abc-notaplan/run.bash":      offence,
		"CLAUDE/Plan/1abc-nodigitsonly/run.bash": offence,
	}, ExitOK)
	mustContain(t, "ok", w.out.String(), okLine)
}

// L4
func TestL4NoPlanTreeOrNoActivePlanIsExitTwo(t *testing.T) {
	w := expectExit(t, "no tree", map[string]string{"README.md": "x"}, ExitCannotRun)
	mustContain(t, "no tree", w.err.String(), "ERROR: cannot read CLAUDE/Plan")
	w = expectExit(t, "no active", map[string]string{"CLAUDE/Plan/Completed/00002-y/run.bash": "#!/usr/bin/bash\n"}, ExitCannotRun)
	mustContain(t, "no active", w.err.String(), "ERROR: no active plan folder under CLAUDE/Plan; nothing was checked")
}

// L4: a link standing where a plan folder would be is not followed, so it cannot be a pass.
func TestL4ASymlinkedPlanFolderIsExitTwo(t *testing.T) {
	w := newWorld(planScript("true\n"))
	w.mem["CLAUDE/Plan/00009-linked"] = &fstest.MapFile{Mode: fs.ModeSymlink, Data: []byte("../../elsewhere")}
	if got := w.run(); got != ExitCannotRun {
		t.Fatalf("exit %d\n%s", got, w.err.String())
	}
	mustContain(t, "link", w.err.String(), "ERROR: CLAUDE/Plan/00009-linked is a symlink")
}

// L5
func TestL5TheWalkIsRecursiveAndSkipsLinksInside(t *testing.T) {
	offence := "#!/usr/bin/env bash\nexec > >(tee \"$LOG\") 2>&1\n"
	w := newWorld(map[string]string{"CLAUDE/Plan/00001-x/a/b/c/deep.bash": offence})
	w.mem["CLAUDE/Plan/00001-x/link.bash"] = &fstest.MapFile{Mode: fs.ModeSymlink, Data: []byte("a/b/c/deep.bash")}
	if got := w.run(); got != ExitFailed {
		t.Fatalf("exit %d", got)
	}
	mustContain(t, "deep", w.err.String(), "CLAUDE/Plan/00001-x/a/b/c/deep.bash:2:exec > >(tee \"$LOG\") 2>&1")
	if strings.Contains(w.err.String(), "link.bash") {
		t.Errorf("a link below a plan folder was judged:\n%s", w.err.String())
	}
	mustContain(t, "count", w.err.String(), "1 plan-script logging offence(s) across 1 script(s) examined")
}

// L5: a file that cannot be read is exit 2, and the rest was still judged.
func TestL5AnUnreadableScriptIsExitTwoNamingIt(t *testing.T) {
	w := newWorld(map[string]string{
		"CLAUDE/Plan/00001-x/a.bash": "#!/usr/bin/bash\ntrue\n",
		"CLAUDE/Plan/00001-x/b.bash": "#!/usr/bin/bash\nexec > >(tee x)\n",
	})
	env := w.env()
	env.Open = func(string) fs.FS { return unreadableFS{FS: w.mem, bad: "CLAUDE/Plan/00001-x/a.bash"} }
	if got := Main(nil, env); got != ExitCannotRun {
		t.Fatalf("exit %d", got)
	}
	mustContain(t, "unreadable", w.err.String(), "ERROR: cannot read CLAUDE/Plan/00001-x/a.bash")
	mustContain(t, "rest judged", w.err.String(), "CLAUDE/Plan/00001-x/b.bash:2:")
}

func TestL5AnUnreadableFolderIsExitTwoNamingIt(t *testing.T) {
	w := newWorld(planScript("true\n"))
	env := w.env()
	env.Open = func(string) fs.FS { return unreadableFS{FS: w.mem, bad: "CLAUDE/Plan/00001-x"} }
	if got := Main(nil, env); got != ExitCannotRun {
		t.Fatalf("exit %d", got)
	}
	mustContain(t, "folder", w.err.String(), "ERROR: cannot read CLAUDE/Plan/00001-x")
}

// L6
func TestL6ShellFilesAreFoundByNameOrShebang(t *testing.T) {
	offence := "exec > >(tee \"$LOG\") 2>&1\n"
	cases := map[string]struct {
		path, text string
		examined   bool
	}{
		"bash name":       {"CLAUDE/Plan/00001-x/a.bash", offence, true},
		"sh name":         {"CLAUDE/Plan/00001-x/a.sh", offence, true},
		"bash shebang":    {"CLAUDE/Plan/00001-x/tool", "#!/usr/bin/env bash\n" + offence, true},
		"sh shebang":      {"CLAUDE/Plan/00001-x/tool.run", "#!/bin/sh\n" + offence, true},
		"python shebang":  {"CLAUDE/Plan/00001-x/tool.py", "#!/usr/bin/env python3\n" + offence, false},
		"no shebang":      {"CLAUDE/Plan/00001-x/notes.txt", offence, false},
		"markdown":        {"CLAUDE/Plan/00001-x/PLAN.md", "# plan\n" + offence, false},
		"bash -c wrapper": {"CLAUDE/Plan/00001-x/inline.yml", "#!/bin/bash -c\n" + offence, false},
	}
	for name, c := range cases {
		w := newWorld(map[string]string{"CLAUDE/Plan/00001-x/anchor.bash": "#!/usr/bin/bash\ntrue\n", c.path: c.text})
		want := ExitOK
		if c.examined {
			want = ExitFailed
		}
		if got := w.run(); got != want {
			t.Errorf("%s: exit %d, want %d\n%s", name, got, want, w.err.String())
		}
	}
}

// L7
func TestL7NoScriptIsExitTwo(t *testing.T) {
	w := expectExit(t, "none", map[string]string{"CLAUDE/Plan/00001-x/PLAN.md": "# plan\n"}, ExitCannotRun)
	mustContain(t, "none", w.err.String(), "ERROR: examined 0 plan scripts; the search matched nothing, so this gate certified nothing")
	if w.out.Len() != 0 {
		t.Errorf("stdout: %q", w.out.String())
	}
}

// L8
func TestL8AScriptThatDoesNotParseIsExitTwoNamingIt(t *testing.T) {
	w := expectExit(t, "unparseable", map[string]string{
		"CLAUDE/Plan/00001-x/ok.bash":  "#!/usr/bin/bash\ntrue\n",
		"CLAUDE/Plan/00001-x/bad.bash": "#!/usr/bin/bash\nif then fi (\n",
	}, ExitCannotRun)
	mustContain(t, "unparseable", w.err.String(), "ERROR: cannot parse CLAUDE/Plan/00001-x/bad.bash:")
	if w.out.Len() != 0 {
		t.Errorf("stdout: %q", w.out.String())
	}
}

// L9: every form of the offence, each reported at the statement's first line.
func TestL9TheOffenceInEveryForm(t *testing.T) {
	cases := map[string]struct {
		body string
		line int
	}{
		"the classic":         {"LOG=/tmp/x\nexec > >(tee \"$LOG\") 2>&1\n", 3},
		"append":              {"exec >> >(tee -a x)\n", 2},
		"stderr only":         {"exec 2> >(tee x >&2)\n", 2},
		"both":                {"exec &> >(tee x)\n", 2},
		"high fd":             {"exec 3> >(tee x)\n", 2},
		"named fd":            {"exec {fd}> >(tee x)\n", 2},
		"not tee":             {"exec > >(cat > x)\n", 2},
		"a function":          {"setup() {\n  exec > >(ts '%F' | tee x)\n}\n", 3},
		"an if":               {"if true; then\n  exec > >(tee x)\nfi\n", 3},
		"a subshell":          {"( exec > >(tee x) )\n", 2},
		"a brace group":       {"{ exec > >(tee x); }\n", 2},
		"a prefix assignment": {"A=1 exec > >(tee x)\n", 2},
		"two redirections":    {"exec 2>&1 > >(tee x)\n", 2},
		"continued":           {"exec \\\n  > >(tee x) \\\n  2>&1\n", 2},
		"a case arm":          {"case $1 in\n  a) exec > >(tee x) ;;\nesac\n", 3},
	}
	for name, c := range cases {
		w := newWorld(planScript(c.body))
		if got := w.run(); got != ExitFailed {
			t.Errorf("%s: exit %d\n%s", name, got, w.err.String())
			continue
		}
		want := "CLAUDE/Plan/00001-x/run.bash:" + strconv.Itoa(c.line) + ":"
		if !strings.Contains(w.err.String(), want) {
			t.Errorf("%s: %q not in\n%s", name, want, w.err.String())
		}
	}
}

// L10: text and constructs that are not the offence.
func TestL10NotTheOffence(t *testing.T) {
	cases := map[string]string{
		"a comment":              "# exec > >(tee \"$LOG\") 2>&1\ntrue\n",
		"an end-of-line comment": "true # exec > >(tee x)\n",
		"a quoted string":        "echo 'exec > >(tee x)'\necho \"exec > >(tee x)\"\n",
		"a heredoc body":         "cat <<'EOF'\nexec > >(tee x)\nEOF\n",
		"a pipe to tee":          "run | tee /tmp/x\n",
		"a plain file":           "exec > \"$LOG\"\nexec >> /tmp/x 2>&1\n",
		"an input substitution":  "exec < <(cmd)\nexec 3< <(cmd)\n",
		"exec with a command":    "exec cmd > >(tee x)\n",
		"not exec":               "foo > >(tee x)\n",
		"the library":            "plan_start_log auto\n",
		"an exec of a program":   "exec \"$0\" \"$@\"\n",
	}
	for name, body := range cases {
		w := newWorld(planScript(body))
		if got := w.run(); got != ExitOK {
			t.Errorf("%s: exit %d\n%s", name, got, w.err.String())
		}
	}
}

// L11
func TestL11ALogsDirectoryAtAnyDepthIsAnOffence(t *testing.T) {
	w := newWorld(planScript("true\n"))
	w.mem["CLAUDE/Plan/00001-x/logs"] = &fstest.MapFile{Mode: fs.ModeDir}
	w.mem["CLAUDE/Plan/00001-x/deep/er/logs/run.txt"] = &fstest.MapFile{Data: []byte("x")}
	w.mem["CLAUDE/Plan/00001-x/blogs/x.txt"] = &fstest.MapFile{Data: []byte("x")}
	w.mem["CLAUDE/Plan/00001-x/Logs/x.txt"] = &fstest.MapFile{Data: []byte("x")}
	if got := w.run(); got != ExitFailed {
		t.Fatalf("exit %d\n%s", got, w.err.String())
	}
	mustContain(t, "top", w.err.String(), "CLAUDE/Plan/00001-x/logs: plan-local run-log directory")
	mustContain(t, "deep", w.err.String(), "CLAUDE/Plan/00001-x/deep/er/logs: plan-local run-log directory")
	mustContain(t, "count", w.err.String(), "2 plan-script logging offence(s) across 1 script(s) examined")
	if strings.Contains(w.err.String(), "blogs") || strings.Contains(w.err.String(), "Logs") {
		t.Errorf("a name that is not logs was reported:\n%s", w.err.String())
	}
}

func TestL11ALogsDirectoryInACompletedPlanIsNotExamined(t *testing.T) {
	files := planScript("true\n")
	files["CLAUDE/Plan/Completed/00002-y/logs/run.txt"] = "x"
	expectExit(t, "completed logs", files, ExitOK)
}

// L12
func TestL12FindingsAreSortedAndFollowedByTheRemedy(t *testing.T) {
	w := newWorld(map[string]string{
		"CLAUDE/Plan/00002-b/z.bash": "#!/usr/bin/bash\ntrue\nexec > >(tee x)\n",
		"CLAUDE/Plan/00001-a/m.bash": "#!/usr/bin/bash\nexec > >(tee y)\ntrue\nexec 2> >(tee z)\n",
		"CLAUDE/Plan/00003-c/logs/x": "x",
	})
	if got := w.run(); got != ExitFailed {
		t.Fatalf("exit %d", got)
	}
	want := strings.Join([]string{
		"ERROR: 4 plan-script logging offence(s) across 2 script(s) examined:",
		"  CLAUDE/Plan/00001-a/m.bash:2:exec > >(tee y)",
		"  CLAUDE/Plan/00001-a/m.bash:4:exec 2> >(tee z)",
		"  CLAUDE/Plan/00002-b/z.bash:3:exec > >(tee x)",
		"  CLAUDE/Plan/00003-c/logs: plan-local run-log directory",
		"",
		"  Convert to the plan library: source _planlib.inc.bash, plan_init, then",
		"  plan_start_log auto — and remove every consumer of the $LOG you delete.",
		"",
	}, "\n")
	if w.err.String() != want {
		t.Fatalf("stderr:\n%s\nwant:\n%s", w.err.String(), want)
	}
	if w.out.Len() != 0 {
		t.Errorf("stdout on failure: %q", w.out.String())
	}
}

// L13
func TestL13CleanPrintsTheCountedOKLine(t *testing.T) {
	files := planScript("plan_start_log auto\n")
	files["CLAUDE/Plan/00002-y/two.sh"] = "#!/bin/sh\ntrue\n"
	files["CLAUDE/Plan/00002-y/PLAN.md"] = "x"
	w := expectExit(t, "clean", files, ExitOK)
	if w.out.String() != "PLAN-SCRIPT-LOGGING-OK: 2 plan script(s) examined, no offences\n" || w.err.Len() != 0 {
		t.Fatalf("stdout %q stderr %q", w.out.String(), w.err.String())
	}
}

// L14: a converted script that documents its own conversion in a comment is clean.
func TestL14AConvertedScriptNamingThePatternInACommentIsClean(t *testing.T) {
	body := "# historical note: this script used to exec > >(tee \"$LOG\") 2>&1\nplan_start_log auto\n"
	w := expectExit(t, "converted", planScript(body), ExitOK)
	mustContain(t, "ok", w.out.String(), okLine)
}
