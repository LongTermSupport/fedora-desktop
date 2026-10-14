package discovery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	bashShebang = "#!/usr/bin/bash\n"
	pyShebang   = "#!/usr/bin/env python3\n"
)

// D1
func TestNoListIsUsageError(t *testing.T) {
	got := newWorld(t).main()
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, "ERROR:", "usage: qa-discovery")
}

func TestBadCommandLinesAreUsageErrors(t *testing.T) {
	for name, args := range map[string][]string{
		"unknown list":  {"cobol"},
		"two lists":     {"shell", "python"},
		"unknown flag":  {"--frob", "shell"},
		"root no value": {"shell", "--root"},
	} {
		t.Run(name, func(t *testing.T) {
			got := newWorld(t).main(args...)
			wantExit(t, got, ExitCannotRun)
			wantStderr(t, got, "ERROR:", "usage: qa-discovery")
			if got.stdout != "" {
				t.Fatalf("stdout %q", got.stdout)
			}
		})
	}
}

func TestHelpIsStdoutExitZero(t *testing.T) {
	got := newWorld(t).main("--help")
	wantExit(t, got, ExitOK)
	if !strings.Contains(got.stdout, "usage: qa-discovery") || got.stderr != "" {
		t.Fatalf("%+v", got)
	}
}

func TestRootFromScriptsDirOrFlagOrCwd(t *testing.T) {
	w := newWorld(t)
	w.track("scripts/a.bash", bashShebang)
	want := w.root + "/scripts/a.bash"
	wantPaths(t, w.main("shell"), want)
	wantPaths(t, w.main("--root", w.root, "shell"), want)
	wantPaths(t, w.main("--scripts", filepath.Join(w.root, "scripts"), "shell"), want)
}

// D2
func TestPathsAreAbsoluteSortedNulTerminatedAndNothingElseIsPrinted(t *testing.T) {
	w := newWorld(t)
	w.track("b.sh", "")
	w.track("a/z.bash", "")
	w.track("a/a.bash", "")
	got := w.main("shell")
	want := w.root + "/a/a.bash\x00" + w.root + "/a/z.bash\x00" + w.root + "/b.sh\x00"
	if got.code != ExitOK || got.stdout != want || got.stderr != "" {
		t.Fatalf("%+v want %q", got, want)
	}
}

func TestTrackedListsArePrintedRepositoryRelative(t *testing.T) {
	w := newWorld(t)
	w.track("a/z.bash", "")
	w.track("a/a.bash", "")
	wantPaths(t, w.main("tracked-shell"), "a/a.bash", "a/z.bash")
}

// D3
func TestAnEmptyTemplateListIsNothingAndSuccess(t *testing.T) {
	w := newWorld(t)
	w.track("a.bash", "")
	got := w.main("python-templates")
	wantExit(t, got, ExitOK)
	if got.stdout != "" || got.stderr != "" {
		t.Fatalf("%+v", got)
	}
}

func TestAFailedRunPrintsNothingToStdout(t *testing.T) {
	w := newWorld(t)
	w.gitCode = 128
	got := w.main("shell")
	wantExit(t, got, ExitCannotRun)
	if got.stdout != "" {
		t.Fatalf("stdout %q", got.stdout)
	}
}

// D4
func TestPublishedMeansTrackedOrFreshButNeverIgnoredNorDeleted(t *testing.T) {
	w := newWorld(t)
	w.track("tracked.bash", "")
	w.fresh("fresh.bash", "")
	w.ignore("ignored.bash", "")
	w.track("gone.bash", "")
	w.deleted = []string{"gone.bash"}
	if err := os.Remove(filepath.Join(w.root, "gone.bash")); err != nil {
		t.Fatal(err)
	}
	wantPaths(t, w.main("shell"), w.root+"/fresh.bash", w.root+"/tracked.bash")
}

func TestTrackedListsLeaveOutUntrackedFiles(t *testing.T) {
	w := newWorld(t)
	w.track("tracked.bash", "")
	w.fresh("fresh.bash", "")
	wantPaths(t, w.main("tracked-shell"), "tracked.bash")
}

func TestGitFailingIsNamedAndExitTwo(t *testing.T) {
	w := newWorld(t)
	w.gitCode = 128
	got := w.main("shell")
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, "ERROR: cannot list the files git would publish under "+w.root, "not a git repository")
}

// D5
func TestExcludedTreesApplyAsWholeSegmentsAnywhereOnTheRelativePath(t *testing.T) {
	w := newWorld(t)
	w.track("ok.bash", "")
	w.track("untracked/x.bash", "")
	w.track("deep/untracked/x.bash", "")
	w.track("untrackedish/x.bash", "")
	wantPaths(t, w.main("shell"), w.root+"/ok.bash", w.root+"/untrackedish/x.bash")
}

func TestAnExcludedNameInAnAncestorOfTheRootDoesNotExclude(t *testing.T) {
	w := newWorld(t)
	nested := filepath.Join(w.root, "untracked", "checkout")
	if err := os.MkdirAll(filepath.Join(nested, "scripts"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "scripts", "qa-trees.tsv"), []byte(defaultTrees), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "a.bash"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	w.tracked = []string{"a.bash"}
	wantPaths(t, w.main("--root", nested, "shell"), nested+"/a.bash")
}

func TestEachListUsesItsOwnTrees(t *testing.T) {
	w := newWorld(t)
	w.track(".semgrep/a.bash", "")
	w.track(".venv/a.py", "")
	w.track("keep.bash", "")
	w.track("keep.py", "")
	wantPaths(t, w.main("shell"), w.root+"/keep.bash")
	wantPaths(t, w.main("python"), w.root+"/keep.py")
	wantPaths(t, w.main("tracked-shell"), "keep.bash")
	wantPaths(t, w.main("tracked-python"), "keep.py")
}

func TestYardsticksForPlaybooksAndHelperTestsExcludeNothing(t *testing.T) {
	w := newWorld(t)
	w.track("playbooks/untracked/p.yml", "")
	w.track("tests/helpers/untracked/test_x.py", "")
	wantPaths(t, w.main("tracked-playbooks"), "playbooks/untracked/p.yml")
	wantPaths(t, w.main("tracked-helper-tests"), "tests/helpers/untracked/test_x.py")
}

// The yaml list is the YAML files git would publish outside the shell trees; absolute.
func TestYamlListIsPublishedYamlOutsideTheExcludedTrees(t *testing.T) {
	w := newWorld(t)
	w.track("a.yml", "")
	w.track("sub/b.yaml", "")
	w.track("c.txt", "")
	w.track(".semgrep/x.yml", "")
	w.fresh("new.yml", "")
	wantPaths(t, w.main("yaml"), w.root+"/a.yml", w.root+"/new.yml", w.root+"/sub/b.yaml")
}

func TestABrokenTreesTableIsExitTwo(t *testing.T) {
	for name, table := range map[string]string{
		"missing":      "",
		"malformed":    "shell\n",
		"unknown list": "ruby\tx\nshell\ta\npython\tb\n",
		"no shell":     "python\tb\n",
		"no python":    "shell\ta\n",
		"empty dir":    "shell\t\npython\tb\n",
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.track("a.bash", "")
			if table == "" {
				if err := os.Remove(filepath.Join(w.root, "scripts", "qa-trees.tsv")); err != nil {
					t.Fatal(err)
				}
			} else {
				w.write("scripts/qa-trees.tsv", table, false)
			}
			got := w.main("shell")
			wantExit(t, got, ExitCannotRun)
			wantStderr(t, got, "ERROR:", "qa-trees.tsv")
		})
	}
}

// D6
func TestShellScriptIsByNameOrShebangNeverByMode(t *testing.T) {
	w := newWorld(t)
	w.track("a.sh", "")
	w.track("b.bash", "")
	w.track("tool", bashShebang)
	w.track("tool2", "#!/bin/sh\n")
	w.track("tool3", "#!/bin/sh -e\n")
	w.track("tool4", "#!/usr/bin/env -S bash -eu\n")
	w.track("tool5", "#!/usr/bin/env sh\n")
	w.track("tool6", "#!/bin/bash")
	w.track("plain", "echo hi\n")
	w.track("empty", "")
	w.track("py", pyShebang)
	w.track("bashful", "#!/usr/bin/bashful\n")
	w.track("inline", "#!/usr/bin/bash -c 'exec ansible-playbook \"$0\"'\n")
	w.track("inline-ec", "#!/usr/bin/env -S bash -ec 'exec ansible-playbook \"$0\"'\n")
	w.track("inline-xc", "#!/bin/bash -xc 'true'\n")
	w.track("combined-no-c", "#!/bin/bash -eu\n")
	w.track("tmpl.sh.j2", bashShebang)
	w.track("tmpl2.j2", bashShebang)
	got := w.main("shell")
	names := []string{"a.sh", "b.bash", "combined-no-c", "tool", "tool2", "tool3", "tool4", "tool5", "tool6"}
	want := make([]string, 0, len(names))
	for _, n := range names {
		want = append(want, w.root+"/"+n)
	}
	wantPaths(t, got, want...)
}

// D7
func TestPythonFileIsByNameOrInterpreter(t *testing.T) {
	w := newWorld(t)
	w.track("a.py", "")
	w.track("tool", "#!/usr/bin/python3\n")
	w.track("tool2", pyShebang)
	w.track("tool3", "#!/usr/bin/env python\n")
	w.track("tool4", "#!/usr/bin/env -S python3 -u\n")
	w.track("mention", "#!/bin/bash # python\n")
	w.track("sh", bashShebang)
	w.track("t.py.j2", pyShebang)
	w.track("t.j2", pyShebang)
	names := []string{"a.py", "tool", "tool2", "tool3", "tool4"}
	want := make([]string, 0, len(names))
	for _, n := range names {
		want = append(want, w.root+"/"+n)
	}
	wantPaths(t, w.main("python"), want...)
}

// D8
func TestPythonTemplatesArePublishedJ2FilesWithAPythonShebang(t *testing.T) {
	w := newWorld(t)
	w.track("files/helper.j2", pyShebang)
	w.fresh("files/new.j2", "#!/usr/bin/python3\n")
	w.ignore("files/ignored.j2", pyShebang)
	w.track("files/shell.j2", bashShebang)
	w.track("files/plain.j2", "{{ x }}\n")
	w.track(".venv/x.j2", pyShebang)
	wantPaths(t, w.main("python-templates"), w.root+"/files/helper.j2", w.root+"/files/new.j2")
}

// D16: the js list is the published .js and .mjs files outside the shell trees, by name only.
func TestJSFilesArePublishedJSAndMJSFilesOutsideTheShellTrees(t *testing.T) {
	w := newWorld(t)
	w.track("extensions/a/extension.js", "")
	w.track("tests/extensions/t.mjs", "")
	w.fresh("fresh.js", "")
	w.ignore("ignored.js", "")
	w.track("untracked/scratch.js", "")
	w.track("notes.json", "")
	w.track("bin/tool", "#!/usr/bin/env node\n")
	w.track("lib.cjs", "")
	w.track("dir.js/readme.txt", "")
	wantPaths(t, w.main("js"), w.root+"/extensions/a/extension.js", w.root+"/fresh.js", w.root+"/tests/extensions/t.mjs")
}

// D16: a js list with nothing in it is a broken discovery, like every list but the templates.
func TestAnEmptyJSListIsABrokenDiscovery(t *testing.T) {
	w := newWorld(t)
	w.track("a.bash", "")
	got := w.main("js")
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, "ERROR: found 0 JavaScript files under "+w.root)
}

// D9
func TestEveryTrackedFileDiscoveredIsAlsoInTheYardstickSource(t *testing.T) {
	w := newWorld(t)
	w.track("a.bash", "")
	w.track("tool", bashShebang)
	w.track("c.py", "")
	w.track("py", pyShebang)
	shell := w.main("shell").paths()
	for i, p := range shell {
		shell[i] = strings.TrimPrefix(p, w.root+"/")
	}
	if got := w.main("tracked-shell").paths(); strings.Join(got, ",") != strings.Join(shell, ",") {
		t.Fatalf("shell %v tracked %v", shell, got)
	}
	python := w.main("python").paths()
	for i, p := range python {
		python[i] = strings.TrimPrefix(p, w.root+"/")
	}
	if got := w.main("tracked-python").paths(); strings.Join(got, ",") != strings.Join(python, ",") {
		t.Fatalf("python %v tracked %v", python, got)
	}
}

// D10
func TestAnUnreadableFileIsNamedNotQuietlyClassified(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads everything")
	}
	w := newWorld(t)
	w.track("a.bash", "")
	w.track("locked", bashShebang)
	if err := os.Chmod(filepath.Join(w.root, "locked"), 0); err != nil {
		t.Fatal(err)
	}
	got := w.main("shell")
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, "ERROR: cannot read locked")
}

func TestAListedFileThatIsMissingAndNotDeletedIsNamed(t *testing.T) {
	w := newWorld(t)
	w.track("a.bash", "")
	w.tracked = append(w.tracked, "vanished")
	got := w.main("shell")
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, "ERROR: cannot inspect vanished")
}

// D11
func TestSymlinksAreJudgedByTargetAndListedByOwnPath(t *testing.T) {
	w := newWorld(t)
	w.track("real.bash", "")
	w.track("realtool", bashShebang)
	w.link("alias.bash", "real.bash")
	w.link("aliastool", "realtool")
	w.link("dir", "scripts")
	w.link("dangling", "nowhere")
	w.link("notscript", "scripts/qa-trees.tsv")
	got := w.main("shell")
	wantPaths(t, got, w.root+"/alias.bash", w.root+"/aliastool", w.root+"/real.bash", w.root+"/realtool")
	wantPaths(t, w.main("tracked-shell"), "alias.bash", "aliastool", "real.bash", "realtool")
}

func TestADanglingLinkNamedAsAScriptIsExitTwo(t *testing.T) {
	for _, name := range []string{"x.sh", "x.bash"} {
		w := newWorld(t)
		w.track("a.bash", "")
		w.link(name, "nowhere")
		got := w.main("shell")
		wantExit(t, got, ExitCannotRun)
		wantStderr(t, got, "ERROR: the symlink "+name+" cannot be followed")
	}
	w := newWorld(t)
	w.track("a.py", "")
	w.link("x.py", "nowhere")
	wantExit(t, w.main("python"), ExitCannotRun)
}

// D12
func TestPlaybooksAndHelperTestYardsticks(t *testing.T) {
	w := newWorld(t)
	w.track("playbooks/a.yml", "")
	w.track("playbooks/imports/b.yaml", "")
	w.track("playbooks/c.txt", "")
	w.track("roles/d.yml", "")
	w.track("tests/helpers/test_a.py", "")
	w.track("tests/helpers/sub/test_b.py", "")
	w.track("tests/helpers/conftest.py", "")
	w.track("tests/helpers/helper_test.py", "")
	w.track("tests/other/test_c.py", "")
	w.fresh("playbooks/new.yml", "")
	wantPaths(t, w.main("tracked-playbooks"), "playbooks/a.yml", "playbooks/imports/b.yaml")
	wantPaths(t, w.main("tracked-helper-tests"), "tests/helpers/sub/test_b.py", "tests/helpers/test_a.py")
}

// D13
func TestAnEmptyListIsABrokenDiscovery(t *testing.T) {
	for _, list := range []string{"shell", "python", "tracked-shell", "tracked-python", "tracked-playbooks", "tracked-helper-tests"} {
		t.Run(list, func(t *testing.T) {
			w := newWorld(t)
			w.track("README", "text\n")
			got := w.main(list)
			wantExit(t, got, ExitCannotRun)
			wantStderr(t, got, "ERROR:", "found 0", "discovery is broken")
			if got.stdout != "" {
				t.Fatalf("stdout %q", got.stdout)
			}
		})
	}
}

func TestGitIsAskedForTheSetsTheSpecNames(t *testing.T) {
	w := newWorld(t)
	w.track("a.bash", "")
	wantExit(t, w.main("shell"), ExitOK)
	wantExit(t, w.main("tracked-shell"), ExitOK)
	got := make([]string, 0, len(w.argvs))
	for _, argv := range w.argvs {
		got = append(got, strings.Join(argv, " "))
	}
	want := "git ls-files -z --cached --others --exclude-standard|git ls-files -z --deleted|git ls-files -z|git ls-files -z --deleted"
	if strings.Join(got, "|") != want {
		t.Fatalf("argv\n got  %s\n want %s", strings.Join(got, "|"), want)
	}
}
