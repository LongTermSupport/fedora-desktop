package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/planscriptlogging"
)

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(planscriptlogging.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(planscriptlogging.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
}

// realRun runs the gate over a real work tree holding the files.
func realRun(t *testing.T, root string, files map[string]string) (planscriptlogging.ExitCode, string, string) {
	t.Helper()
	for rel, text := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	env := planscriptlogging.Env{Stdout: &stdout, Stderr: &stderr, Cwd: root, Resolve: filepath.EvalSymlinks, Open: os.DirFS}
	return planscriptlogging.Main([]string{"--root", root}, env), stdout.String(), stderr.String()
}

// L5, L6, L9, L11 on the real file system, where the plan folders hold an ignored logs directory
// and a link.
func TestTheRealFileSystemIsWalkedIncludingIgnoredAndLinkedFiles(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"CLAUDE/Plan/00001-x/clean.bash":              "#!/usr/bin/env bash\nplan_start_log auto\n",
		"CLAUDE/Plan/00001-x/sub/dirty.bash":          "#!/usr/bin/env bash\nexec > >(tee \"$LOG\") 2>&1\n",
		"CLAUDE/Plan/00001-x/logs/run.txt":            "x",
		"CLAUDE/Plan/00001-x/.gitignore":              "logs/\n",
		"CLAUDE/Plan/Completed/00002-y/dirty.bash":    "#!/usr/bin/env bash\nexec > >(tee x)\n",
		"CLAUDE/Plan/00003-z/tool":                    "#!/usr/bin/env bash\nexec 2> >(tee x)\n",
		"CLAUDE/Plan/00003-z/notes.md":                "exec > >(tee x)\n",
		"elsewhere/target.bash":                       "#!/usr/bin/env bash\nexec > >(tee x)\n",
		"CLAUDE/Plan/00004-w/ok.bash":                 "#!/usr/bin/env bash\ntrue\n",
		"CLAUDE/Plan/00004-w/not-a-plan-script.json":  "{}",
		"CLAUDE/Plan/00004-w/not-a-plan-script.json2": "#!/usr/bin/python3\n",
	}
	if err := os.MkdirAll(filepath.Join(root, "CLAUDE/Plan/00004-w"), 0o750); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := realRun(t, root, files)
	if err := os.Symlink(filepath.Join(root, "elsewhere/target.bash"), filepath.Join(root, "CLAUDE/Plan/00004-w/link.bash")); err != nil {
		t.Fatal(err)
	}
	code2, _, stderr2 := realRun(t, root, nil)
	if code != planscriptlogging.ExitFailed || code2 != planscriptlogging.ExitFailed || stdout != "" {
		t.Fatalf("exit %d / %d stdout %q\n%s", code, code2, stdout, stderr)
	}
	for _, want := range []string{
		"3 plan-script logging offence(s) across 4 script(s) examined",
		"CLAUDE/Plan/00001-x/sub/dirty.bash:2:exec > >(tee \"$LOG\") 2>&1",
		"CLAUDE/Plan/00003-z/tool:2:exec 2> >(tee x)",
		"CLAUDE/Plan/00001-x/logs: plan-local run-log directory",
	} {
		if !strings.Contains(stderr, want) || !strings.Contains(stderr2, want) {
			t.Errorf("%q missing:\n%s\n%s", want, stderr, stderr2)
		}
	}
	for _, unwanted := range []string{"Completed", "link.bash", "elsewhere", "notes.md", root} {
		if strings.Contains(stderr2, unwanted) {
			t.Errorf("%q must not appear:\n%s", unwanted, stderr2)
		}
	}
}

// L3: a root reached through a symlink never leaks the checkout path, and a symlinked plan folder
// is exit 2.
func TestASymlinkedRootAndASymlinkedPlanFolder(t *testing.T) {
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "checkout")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := realRun(t, link, map[string]string{"CLAUDE/Plan/00001-x/ok.bash": "#!/usr/bin/env bash\ntrue\n"})
	if code != planscriptlogging.ExitOK || !strings.Contains(stdout, "PLAN-SCRIPT-LOGGING-OK: 1 plan script(s) examined, no offences") || strings.Contains(stdout+stderr, link) {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
	}
	if err := os.Symlink(filepath.Join(target, "CLAUDE/Plan/00001-x"), filepath.Join(target, "CLAUDE/Plan/00002-linked")); err != nil {
		t.Fatal(err)
	}
	code, _, stderr = realRun(t, link, nil)
	if code != planscriptlogging.ExitCannotRun || !strings.Contains(stderr, "CLAUDE/Plan/00002-linked is a symlink") {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
}
