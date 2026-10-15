package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/vmtestmanifest"
)

// repoRoot is the repository the tests run in.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// copyTree copies the inputs of the gate from the live repository into a fresh git repository and
// tracks them, so the real validator and the real guest scripts judge a tree the test may change.
func copyTree(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	copying := proc.Cmd{
		Argv: []string{
			"cp", "-r", "--parents", "helpers/vmtest", "vars/fedora-version.yml", "vars/vm-test-scenarios.yml",
			"files/home/.local/share/vmtest", "scripts/qa-trees.tsv", dst,
		},
		Dir: repoRoot(t),
	}
	steps := []proc.Cmd{
		copying,
		{Argv: []string{"find", dst, "-name", "__pycache__", "-prune", "-exec", "rm", "-rf", "{}", "+"}},
		{Argv: []string{"git", "init", "-q"}, Dir: dst},
		{Argv: []string{"git", "add", "-A", "-f"}, Dir: dst},
	}
	for _, step := range steps {
		if res, err := (proc.OS{}).Run(context.Background(), step); err != nil || res.Code != 0 {
			t.Fatalf("%v: %v %+v", step.Argv, err, res)
		}
	}
	return dst
}

// edit replaces the first occurrence of old in a file of the tree.
func edit(t *testing.T, tree, rel, old, repl string) {
	t.Helper()
	root, err := os.OpenRoot(tree)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	data, err := root.ReadFile(rel)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("%s lacks %q", rel, old)
	}
	if err := root.WriteFile(rel, []byte(strings.Replace(string(data), old, repl, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func needPython(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Fatalf("python3 is required to run the real validator: %v", err)
	}
}

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(vmtestmanifest.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(vmtestmanifest.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
}

// V6, V15: the real validator and the real guest scripts judge the repository's own manifest.
func TestTheLiveManifestPasses(t *testing.T) {
	needPython(t)
	if code := run([]string{"--root", repoRoot(t)}); code != int(vmtestmanifest.ExitOK) {
		t.Fatalf("the live tree: exit %d", code)
	}
}

// V6: the control the bash ran on every run, now proven here against the real validator: a
// manifest with no scenarios is rejected, with the validator's reason.
func TestARealManifestWithNoScenariosIsRejectedByTheRealValidator(t *testing.T) {
	needPython(t)
	root := copyTree(t)
	if err := os.WriteFile(filepath.Join(root, "vars/vm-test-scenarios.yml"), []byte("vm_test_ttl_upgrade_days: 7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--root", root}); code != int(vmtestmanifest.ExitFailed) {
		t.Fatalf("exit %d, want 1", code)
	}
}

// V10: the control the bash ran on every run: one more planned than the checker declares is a
// finding. The copy is judged by the real validator first, so this also proves the copy is valid.
func TestARealManifestWhoseCountDisagreesWithItsCheckerIsExitOne(t *testing.T) {
	needPython(t)
	root := copyTree(t)
	if code := run([]string{"--root", root}); code != int(vmtestmanifest.ExitOK) {
		t.Fatalf("the unchanged copy: exit %d, want 0", code)
	}
	edit(t, root, "files/home/.local/share/vmtest/guest-acceptance-server.bash", "PLANNED=13", "PLANNED=14")
	if code := run([]string{"--root", root}); code != int(vmtestmanifest.ExitFailed) {
		t.Fatalf("exit %d, want 1", code)
	}
}

// V11: the control the bash ran on every run: a fixture whose scenario does not reboot.
func TestARealFixtureWithoutItsRebootIsExitOne(t *testing.T) {
	needPython(t)
	root := copyTree(t)
	edit(t, root, "vars/vm-test-scenarios.yml",
		"    reboot_before_checks: true\n    run_env:\n      RUN_BASH_OPTIONAL_PLAYBOOKS: play-host-health-login-report.yml",
		"    reboot_before_checks: false\n    run_env:\n      RUN_BASH_OPTIONAL_PLAYBOOKS: play-host-health-login-report.yml")
	if code := run([]string{"--root", root}); code != int(vmtestmanifest.ExitFailed) {
		t.Fatalf("exit %d, want 1 (the validator accepts false; the fixture would never run)", code)
	}
}
