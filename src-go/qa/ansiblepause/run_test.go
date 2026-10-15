package ansiblepause

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

const (
	cleanTask = "- name: a\n  debug:\n    msg: hi\n"
	badTask   = "- name: a\n  pause:\n    seconds: 1\n"
)

func TestE1HelpUnknownAndMissingValue(t *testing.T) {
	w := newWorld(map[string]string{"playbooks/a.yml": cleanTask})
	if code := w.run("--help"); code != ExitOK || !strings.Contains(w.out.String(), "usage: qa-ansible-pause") {
		t.Fatalf("--help: %d %q", code, w.out.String())
	}
	w = newWorld(map[string]string{"playbooks/a.yml": cleanTask})
	if code := w.run("--bogus"); code != ExitCannotRun || !strings.Contains(w.err.String(), "ERROR: unknown argument '--bogus'") || !strings.Contains(w.err.String(), "usage:") {
		t.Fatalf("--bogus: %d %q", code, w.err.String())
	}
	w = newWorld(map[string]string{"playbooks/a.yml": cleanTask})
	if code := w.run("--root"); code != ExitCannotRun || !strings.HasPrefix(w.err.String(), "ERROR: ") {
		t.Fatalf("--root without value: %d %q", code, w.err.String())
	}
	w = newWorld(map[string]string{"playbooks/a.yml": cleanTask})
	if code := w.run("--scripts", "/repo/scripts"); code != ExitOK {
		t.Fatalf("--scripts: %d %q", code, w.err.String())
	}
}

func TestE1RootThroughASymlinkIsResolvedAndNeverLeaks(t *testing.T) {
	realRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "view")
	if err := symlink(realRoot, link); err != nil {
		t.Fatal(err)
	}
	w := newWorld(map[string]string{"playbooks/a.yml": badTask})
	env := w.env()
	env.Resolve = filepath.EvalSymlinks
	var asked string
	env.Yaml = func(root string) ([]string, error) { asked = root; return w.yaml, nil }
	if code := Main([]string{"--root", link}, env); code != ExitFailed {
		t.Fatalf("exit %d\n%s", code, w.err.String())
	}
	if asked != realRoot {
		t.Fatalf("discovery asked about %q, want %q", asked, realRoot)
	}
	if all := w.out.String() + w.err.String(); strings.Contains(all, realRoot) || strings.Contains(all, link) {
		t.Fatalf("a checkout path leaked:\n%s", all)
	}
	w = newWorld(map[string]string{"playbooks/a.yml": cleanTask})
	env = w.env()
	env.Resolve = filepath.EvalSymlinks
	if code := Main([]string{"--root", filepath.Join(t.TempDir(), "missing")}, env); code != ExitCannotRun {
		t.Fatalf("missing root: %d", code)
	}
}

func TestE2WriteFailuresAreExitTwo(t *testing.T) {
	w := newWorld(map[string]string{"playbooks/a.yml": cleanTask})
	env := w.env()
	env.Stdout = failWriter{}
	if code := Main(nil, env); code != ExitCannotRun || !strings.Contains(w.err.String(), "ERROR: cannot write to stdout: ") {
		t.Fatalf("exit %d, stderr %q", code, w.err.String())
	}
}

func TestE3PathArguments(t *testing.T) {
	w := newWorld(map[string]string{"playbooks/a.yml": cleanTask, "playbooks/b.yml": badTask})
	if code := w.run("playbooks/a.yml"); code != ExitOK || !strings.Contains(w.out.String(), "passed: 1 failed: 0") {
		t.Fatalf("a only: %d %q %q", code, w.out.String(), w.err.String())
	}
	w = newWorld(map[string]string{"playbooks/a.yml": cleanTask, "playbooks/b.yml": badTask})
	if code := w.run("/repo/playbooks/b.yml"); code != ExitFailed {
		t.Fatalf("absolute inside root: %d", code)
	}
	for _, p := range []string{"../outside.yml", "/elsewhere/x.yml", "playbooks/missing.yml"} {
		w = newWorld(map[string]string{"playbooks/a.yml": cleanTask})
		if code := w.run(p); code != ExitCannotRun || !strings.Contains(w.err.String(), "ERROR: ") || !strings.Contains(w.err.String(), filepath.Base(p)) {
			t.Fatalf("%s: %d %q", p, code, w.err.String())
		}
	}
}

func TestE4PopulationIsYamlUnderPlaybooksAndTasks(t *testing.T) {
	w := newWorld(map[string]string{
		"playbooks/imports/a.yml": cleanTask,
		"tasks/deep/b.yaml":       cleanTask,
		"vars/c.yml":              badTask,
		"playbooks/readme.txt":    badTask,
		"other/tasks/d.yml":       badTask,
		"playbooks_extra/e.yml":   badTask,
	})
	w.yaml = []string{"other/tasks/d.yml", "playbooks/imports/a.yml", "playbooks/readme.txt", "playbooks_extra/e.yml", "tasks/deep/b.yaml", "vars/c.yml"}
	if code := w.run(); code != ExitOK || !strings.Contains(w.out.String(), "passed: 2 failed: 0") {
		t.Fatalf("%d out %q err %q", code, w.out.String(), w.err.String())
	}
}

func TestE4DiscoveryFailingIsExitTwoWithItsReason(t *testing.T) {
	w := newWorld(map[string]string{"playbooks/a.yml": cleanTask})
	w.yamlErr = errors.New("git exploded")
	if code := w.run(); code != ExitCannotRun || !strings.Contains(w.err.String(), "ERROR: ") || !strings.Contains(w.err.String(), "git exploded") {
		t.Fatalf("%d %q", code, w.err.String())
	}
}

func TestE5EmptyPopulationIsExitTwo(t *testing.T) {
	w := newWorld(map[string]string{"vars/c.yml": badTask})
	if code := w.run(); code != ExitCannotRun || !strings.Contains(w.err.String(), "ERROR: no YAML found under playbooks/ or tasks/; nothing was checked") {
		t.Fatalf("%d %q", code, w.err.String())
	}
	if w.out.String() != "" {
		t.Fatalf("stdout %q", w.out.String())
	}
}

func TestE6UnreadableFileIsExitTwoNamingIt(t *testing.T) {
	w := newWorld(map[string]string{"playbooks/a.yml": cleanTask})
	w.yaml = append(w.yaml, "playbooks/gone.yml")
	if code := w.run(); code != ExitCannotRun || !strings.Contains(w.err.String(), "playbooks/gone.yml") {
		t.Fatalf("%d %q", code, w.err.String())
	}
}

func TestE14ToE16ReportAllFilesInOrder(t *testing.T) {
	w := newWorld(map[string]string{
		"playbooks/b.yml": badTask + badTask,
		"playbooks/a.yml": badTask,
		"tasks/c.yml":     cleanTask,
	})
	code := w.run()
	if code != ExitFailed {
		t.Fatalf("exit %d", code)
	}
	want := "playbooks/a.yml:2: pause-without-register\n" +
		"playbooks/b.yml:2: pause-without-register\n" +
		"playbooks/b.yml:5: pause-without-register\n" +
		"pause-without-register: 3 pause task(s) in 2 file(s) wait with no register and no '# PAUSE-OK: <reason>'. " +
		"Use ansible.builtin.debug for status. See CLAUDE/QA.md \"pause-without-register\".\n"
	if w.err.String() != want {
		t.Fatalf("stderr:\n%s\nwant:\n%s", w.err.String(), want)
	}
	if w.out.String() != "passed: 1 failed: 2\n" {
		t.Fatalf("stdout %q", w.out.String())
	}
}

func TestE16CleanRunIsOnlyTheCountLine(t *testing.T) {
	w := newWorld(map[string]string{"playbooks/a.yml": cleanTask, "tasks/b.yml": cleanTask})
	if code := w.run(); code != ExitOK || w.out.String() != "passed: 2 failed: 0\n" || w.err.String() != "" {
		t.Fatalf("%d out %q err %q", code, w.out.String(), w.err.String())
	}
}
