package deployeddrift

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

func symlink(target string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(target), Mode: fs.ModeSymlink}
}

func withMode(text string, mode fs.FileMode) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(text), Mode: mode}
}

// drifting is a world whose one bin script differs from its deployed copy.
func drifting() *world {
	w := base()
	w.repo["files/home/.local/bin/tool"] = file("a")
	w.host["home/user/.local/bin/tool"] = file("b")
	return w
}

// E1: the root is made absolute, cleaned and symlink-resolved once.
func TestE1RootIsMadeAbsoluteAndCleaned(t *testing.T) {
	for _, args := range [][]string{
		{"--root", "/repo/"},
		{"--root", "."},
		{"--root", "./"},
		{"--scripts", "/repo/scripts/"},
		{"--scripts", "scripts"},
		{},
	} {
		r := drifting().run(args...)
		r.expect(t, ExitFailed)
		contains(t, "stderr "+strings.Join(args, " "), r.errOut, "    repo:     /repo/files/home/.local/bin/tool\n")
	}
}

func TestE1SymlinkedRootIsResolved(t *testing.T) {
	w := drifting()
	w.links = map[string]string{"/link": "/repo"}
	r := w.run("--root", "/link")
	r.expect(t, ExitFailed)
	contains(t, "stderr", r.errOut, "    repo:     /repo/files/home/.local/bin/tool\n")
}

func TestE1UnresolvableRootIsExitTwoNamingIt(t *testing.T) {
	w := base()
	w.resolve = errors.New("no such file or directory")
	r := w.run("--root", "/gone")
	r.expect(t, ExitCannotRun)
	contains(t, "stderr", r.errOut, "/gone")
}

// E7: the container is the marker, not the spelling of the path.
func TestE7ContainerIsDetectedByMarkerWhateverTheRootSpelling(t *testing.T) {
	for _, marker := range []string{"run/.containerenv", ".dockerenv"} {
		for _, args := range [][]string{{"--root", "/workspace/"}, {"--root", "."}, {"--scripts", "scripts"}, {"--root", "/link"}} {
			w := drifting()
			w.root = "/workspace"
			w.links = map[string]string{"/link": "/workspace"}
			w.host[marker] = file("")
			r := w.run(args...)
			r.expect(t, ExitOK)
			contains(t, marker+" "+strings.Join(args, " "), r.out, "skipped (CCY container")
		}
	}
}

func TestE7HostCheckoutAtWorkspaceIsNotSkipped(t *testing.T) {
	w := drifting()
	w.root = "/workspace"
	w.run().expect(t, ExitFailed)
}

// E9: an unclean HOME is cleaned; a relative one is refused.
func TestE9HomeWithTrailingSlashIsAccepted(t *testing.T) {
	for _, home := range []string{"/home/user/", "/home//user", "/home/user/."} {
		w := base()
		w.home = home
		w.repo["files/home/.local/bin/tool"] = file("a")
		w.host["home/user/.local/bin/tool"] = file("a")
		r := w.run()
		r.expect(t, ExitOK)
		contains(t, home, r.out, "✓ deployed-drift: 1 deployed script(s) match")
	}
}

func TestE9RelativeHomeIsExitTwo(t *testing.T) {
	w := base()
	w.home = "user"
	r := w.run()
	r.expect(t, ExitCannotRun)
	contains(t, "stderr", r.errOut, "HOME is not an absolute path")
}

func TestE9UnsetHomeInAWorktreeIsASkipNotAnError(t *testing.T) {
	w := base()
	w.home = ""
	w.linked = true
	w.run().expect(t, ExitOK)
}

// E4: an unstat-able repo entry is exit 2 naming it.
func TestE4DanglingSymlinkInTheBinDirectoryIsExitTwoNamingIt(t *testing.T) {
	w := base()
	w.repo["files/home/.local/bin/zz-dangling"] = symlink("/nonexistent")
	r := w.run()
	r.expect(t, ExitCannotRun)
	contains(t, "stderr", r.errOut, "files/home/.local/bin/zz-dangling is a dangling symlink")
}

func TestE4DanglingSymlinkInAPairDirectoryIsExitTwoNamingIt(t *testing.T) {
	w := base()
	w.repo["files/home/.local/share/vmtest/zz"] = symlink("/nonexistent")
	r := w.run()
	r.expect(t, ExitCannotRun)
	contains(t, "stderr", r.errOut, "files/home/.local/share/vmtest/zz is a dangling symlink")
}

// E17: a deployed path that exists but is not a regular file is drift.
func TestE17DeployedDirectoryIsDriftNotNotInstalled(t *testing.T) {
	w := base()
	w.repo["files/home/.local/bin/tool"] = file("a")
	w.host["home/user/.local/bin/tool/inner"] = file("x")
	r := w.run()
	r.expect(t, ExitFailed)
	contains(t, "stderr", r.errOut, "  files/home/.local/bin/tool\n")
	contains(t, "stderr", r.errOut, "deployed path is a directory, not a file")
}

func TestE17DeployedDanglingSymlinkIsDriftNotNotInstalled(t *testing.T) {
	w := base()
	w.repo["files/home/.local/bin/tool"] = file("a")
	w.host["home/user/.local/bin/tool"] = symlink("/nonexistent")
	r := w.run()
	r.expect(t, ExitFailed)
	contains(t, "stderr", r.errOut, "deployed path is a dangling symlink")
}

func TestE17AbsentDeployedPathStaysNotInstalled(t *testing.T) {
	w := base()
	w.repo["files/home/.local/bin/tool"] = file("a")
	w.run().expect(t, ExitOK)
}

// execPlay is a play that deploys tool with the given mode, followed by another task.
func execPlay(mode string) string {
	return "- name: deploy\n  ansible.builtin.copy:\n    src: \"{{ root_dir }}/files/home/.local/bin/tool\"\n" +
		"    dest: \"{{ h }}/.local/bin/tool\"\n    mode: \"" + mode + "\"\n- name: next\n  ansible.builtin.copy:\n    mode: \"0600\"\n"
}

// E18: the owner execute bit of a script, against the mode its play sets.
func TestE18ScriptThatLostItsExecBitIsDrift(t *testing.T) {
	w := base()
	w.repo["files/home/.local/bin/tool"] = file("a")
	w.repo["playbooks/p.yml"] = file(execPlay("0755"))
	w.host["home/user/.local/bin/tool"] = withMode("a", 0o600)
	r := w.run()
	r.expect(t, ExitFailed)
	contains(t, "stderr", r.errOut, "deployed copy is not executable but playbooks/p.yml sets mode 0755")
	w.host["home/user/.local/bin/tool"] = withMode("a", 0o755)
	w.run().expect(t, ExitOK)
}

func TestE18ScriptThePlayDeploysNonExecutableIsNotDrift(t *testing.T) {
	w := base()
	w.repo["files/home/.local/bin/tool"] = file("a")
	w.repo["playbooks/p.yml"] = file(execPlay("0644"))
	w.host["home/user/.local/bin/tool"] = withMode("a", 0o644)
	w.run().expect(t, ExitOK)
	w.host["home/user/.local/bin/tool"] = withMode("a", 0o755)
	r := w.run()
	r.expect(t, ExitFailed)
	contains(t, "stderr", r.errOut, "deployed copy is executable but playbooks/p.yml sets mode 0644")
}

func TestE18NoDeclaredModeMeansNoModeCheck(t *testing.T) {
	w := base()
	w.repo["files/home/.local/bin/tool"] = file("a")
	w.repo["playbooks/p.yml"] = file("- src: files/home/.local/bin/tool\n")
	w.host["home/user/.local/bin/tool"] = withMode("a", 0o600)
	w.run().expect(t, ExitOK)
}

func TestE18ModeIsNotCheckedForTheExtraTable(t *testing.T) {
	w := base()
	w.repo["files/usr/local/bin/agent-bus"] = file("x")
	w.host["usr/local/bin/agent-bus"] = withMode("x", 0o600)
	w.run().expect(t, ExitOK)
}

// The real plays: an executable script and a deliberately non-executable sourced library.
func TestE18ReadsTheModesTheRealPlaysSet(t *testing.T) {
	repo, err := scanRepo("../../..", Env{Open: os.DirFS})
	if err != nil {
		t.Fatal(err)
	}
	for name, wantExec := range map[string]bool{"nord": true, "rclone-rc-auth.bash": false} {
		got, ok := repo.declaredMode(name)
		if !ok || got.exec != wantExec {
			t.Errorf("declaredMode(%s) = %+v, %v; want exec %v", name, got, ok, wantExec)
		}
	}
}
