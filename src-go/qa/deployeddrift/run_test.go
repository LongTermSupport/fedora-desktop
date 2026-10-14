package deployeddrift

import (
	"bytes"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

const (
	testRoot = "/repo"
	testHome = "/home/user"
)

// world is one scenario: a repo, a host, and what git says about the checkout.
type world struct {
	repo     fstest.MapFS
	host     fstest.MapFS
	root     string
	home     string
	linked   bool
	gitErr   error
	unreadOn string            // a host path that fails to open
	links    map[string]string // a symlinked root and its target (the Resolve seam)
	resolve  error
}

type result struct {
	code        ExitCode
	out, errOut string
}

func newWorld() *world {
	return &world{repo: fstest.MapFS{}, host: fstest.MapFS{}, root: testRoot, home: testHome}
}

func file(text string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(text)} }

// withPlaybooks adds the plays every scenario needs (E6) plus any extra lines in one more play.
func (w *world) withPlaybooks(extra map[string]string) *world {
	w.repo["playbooks/imports/optional/common/play-vm-test-lab.yml"] = file("- hosts: all\n")
	w.repo["playbooks/imports/play-agent-bus.yml"] = file("- hosts: all\n")
	for p, text := range extra {
		w.repo[p] = file(text)
	}
	return w
}

// withPairFiles gives every extra-table glob one repo file (E6), none deployed.
func (w *world) withPairFiles() *world {
	for _, f := range []string{
		"files/home/.local/share/vmtest/a", "fedora-install/ks-vm-a.cfg",
		"files/home/.config/systemd/user/vmtest-a", "helpers/vmtest/a.py",
		"files/usr/local/sbin/agent-bus-install", "files/usr/local/bin/agent-bus",
		"files/usr/local/share/agent-bus/a", "files/etc/systemd/system/agent-bus-a",
		"files/opt/claude-yolo/optional/agent-bus/a",
		"files/opt/claude-yolo/optional/agent-bus/plugin/pingbus/.claude-plugin/a",
		"files/opt/claude-yolo/optional/agent-bus/plugin/pingbus/hooks/a",
		"files/opt/claude-yolo/optional/agent-bus/plugin/pingbus/skills/pingbus/a",
	} {
		w.repo[f] = file("x")
	}
	return w
}

// base is a deployable host checkout with the bin directory present on both sides.
func base() *world {
	w := newWorld().withPlaybooks(nil).withPairFiles()
	w.repo["files/home/.local/bin/.keep"] = file("")
	w.host["home/user/.local/bin/.keep"] = file("")
	return w
}

type hostFS struct {
	fs.FS

	unread string
}

func (h hostFS) Open(name string) (fs.File, error) {
	if name == h.unread {
		return nil, errors.New("permission denied")
	}
	return h.FS.Open(name)
}

func (w *world) run(args ...string) result {
	var out, errOut bytes.Buffer
	var host fs.FS = w.host
	if w.unreadOn != "" {
		host = hostFS{FS: w.host, unread: w.unreadOn}
	}
	env := Env{
		Stdout: &out, Stderr: &errOut, Cwd: w.root, Home: w.home,
		Open: func(string) fs.FS { return w.repo },
		Host: host,
		Resolve: func(p string) (string, error) {
			if w.resolve != nil {
				return "", w.resolve
			}
			if target, ok := w.links[p]; ok {
				return target, nil
			}
			return p, nil
		},
		LinkedWorktree: func(string) (bool, error) {
			return w.linked, w.gitErr
		},
	}
	return result{code: Main(args, env), out: out.String(), errOut: errOut.String()}
}

func (r result) expect(t *testing.T, code ExitCode) {
	t.Helper()
	if r.code != code {
		t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", r.code, code, r.out, r.errOut)
	}
}

func contains(t *testing.T, what, text, want string) {
	t.Helper()
	if !strings.Contains(text, want) {
		t.Errorf("%s lacks %q:\n%s", what, want, text)
	}
}

func lacks(t *testing.T, what, text, bad string) {
	t.Helper()
	if strings.Contains(text, bad) {
		t.Errorf("%s has %q:\n%s", what, bad, text)
	}
}

// E1
func TestE1HelpIsExitZeroOnStdout(t *testing.T) {
	r := base().run("--help")
	r.expect(t, ExitOK)
	contains(t, "stdout", r.out, "usage: qa-deployed-drift")
}

func TestE1UnknownArgumentAndMissingValueAreExitTwo(t *testing.T) {
	r := base().run("--nope")
	r.expect(t, ExitCannotRun)
	contains(t, "stderr", r.errOut, "ERROR: unknown argument '--nope'")
	contains(t, "stderr", r.errOut, "usage:")
	base().run("--root").expect(t, ExitCannotRun)
	base().run("--root", "--scripts", "x").expect(t, ExitCannotRun)
}

// E4
func TestE4MissingSourceDirectoryIsExitTwo(t *testing.T) {
	w := newWorld().withPlaybooks(nil)
	r := w.run()
	r.expect(t, ExitCannotRun)
	contains(t, "stderr", r.errOut, "ERROR: source directory not found: /repo/files/home/.local/bin")
}

func TestE4MissingPlaybooksIsExitTwo(t *testing.T) {
	w := newWorld()
	w.repo["files/home/.local/bin/tool"] = file("x")
	r := w.run()
	r.expect(t, ExitCannotRun)
	contains(t, "stderr", r.errOut, "playbooks")
}

// E5
func TestE5TemplateWithoutPlaybookDestIsExitTwoEvenInTheContainer(t *testing.T) {
	w := base()
	w.host["run/.containerenv"] = file("")
	w.repo["files/home/.local/bin/helper.j2"] = file("{{ x }}")
	w.repo["playbooks/play.yml"] = file("- dest: \"{{ h }}/.local/bin/other\"\n")
	r := w.run()
	r.expect(t, ExitCannotRun)
	contains(t, "stderr", r.errOut, "✗ deployed-drift: helper.j2 has no playbook dest: deploying it as 'helper'")
}

func TestE5DestMatchIsAnchoredAndDotIsLiteral(t *testing.T) {
	w := base()
	w.repo["files/home/.local/bin/a.b.j2"] = file("t")
	w.repo["playbooks/play.yml"] = file("    dest: /x/.local/bin/aXb\n")
	w.run().expect(t, ExitCannotRun)
	w.repo["playbooks/play.yml"] = file("    dest: /x/.local/bin/a.b-more\n")
	w.run().expect(t, ExitCannotRun)
	w.repo["playbooks/play.yml"] = file("    dest: \"{{ h }}/.local/bin/a.b\"\n")
	w.run().expect(t, ExitOK)
}

// E6
func TestE6MissingDeployingPlayIsExitTwo(t *testing.T) {
	w := base()
	delete(w.repo, "playbooks/imports/play-agent-bus.yml")
	w.repo["playbooks/other.yml"] = file("x")
	r := w.run()
	r.expect(t, ExitCannotRun)
	contains(t, "stderr", r.errOut, "playbooks/imports/play-agent-bus.yml")
}

func TestE6PairMatchingNothingIsExitTwo(t *testing.T) {
	w := base()
	delete(w.repo, "files/usr/local/bin/agent-bus")
	r := w.run()
	r.expect(t, ExitCannotRun)
	contains(t, "stderr", r.errOut, "files/usr/local/bin/agent-bus matches no repo file")
}

// E7
func TestE7ContainerSkipIsWarnedAndExitZero(t *testing.T) {
	w := base()
	w.host["run/.containerenv"] = file("")
	w.repo["files/home/.local/bin/t.j2"] = file("x")
	w.repo["playbooks/p.yml"] = file("dest: /.local/bin/t\n")
	r := w.run()
	r.expect(t, ExitOK)
	want := "⚠ deployed-drift: skipped (CCY container — no deployed copies to compare); 1 template(s) verified to map to a playbook dest:"
	contains(t, "stdout", r.out, want)
	lacks(t, "stdout", r.out, "✓")
}

// E8
func TestE8LinkedWorktreeSkipIsWarnedAndExitZero(t *testing.T) {
	w := base()
	w.linked = true
	r := w.run()
	r.expect(t, ExitOK)
	contains(t, "stdout", r.out, "⚠ deployed-drift: skipped (linked git worktree — not the deployed checkout); 0 template(s)")
}

func TestE8GitFailureIsExitTwoNotAComparison(t *testing.T) {
	w := base()
	w.gitErr = errors.New("not a git repository")
	r := w.run()
	r.expect(t, ExitCannotRun)
	contains(t, "stderr", r.errOut, "not a git repository")
}

// E9
func TestE9MissingDeployedDirectoryIsWarnedAndExitZero(t *testing.T) {
	w := base()
	w.host = fstest.MapFS{}
	r := w.run()
	r.expect(t, ExitOK)
	contains(t, "stdout", r.out, "⚠ deployed-drift: skipped (/home/user/.local/bin does not exist); 0 template(s)")
}

func TestE9HomeUnsetIsExitTwo(t *testing.T) {
	w := base()
	w.home = ""
	r := w.run()
	r.expect(t, ExitCannotRun)
	contains(t, "stderr", r.errOut, "ERROR: HOME is not set")
}

// E10 E11 E15
func TestE11NotInstalledIsCountedAndNeverAFailure(t *testing.T) {
	w := base()
	w.repo["files/home/.local/bin/one"] = file("a")
	w.repo["files/home/.local/bin/two"] = file("b")
	w.host["home/user/.local/bin/one"] = file("a")
	r := w.run()
	r.expect(t, ExitOK)
	// 1 bin match; 1 bin not installed + 12 extra-table files not installed.
	contains(t, "stdout", r.out, "✓ deployed-drift: 1 deployed script(s) match the repo; 13 not installed on this host; 0 template(s) not byte-comparable")
}

func TestE10ExtraPairIsComparedAgainstItsDirectory(t *testing.T) {
	w := base()
	w.repo["files/usr/local/bin/agent-bus"] = file("new")
	w.host["usr/local/bin/agent-bus"] = file("old")
	r := w.run()
	r.expect(t, ExitFailed)
	contains(t, "stderr", r.errOut, "  files/usr/local/bin/agent-bus\n")
	contains(t, "stderr", r.errOut, "    deployed: /usr/local/bin/agent-bus\n")
	contains(t, "stderr", r.errOut, "    ansible-playbook playbooks/imports/play-agent-bus.yml\n")
	contains(t, "stderr", r.errOut, "✗ deployed-drift: 1 of 1 deployed script(s) differ from the repo; 11 not installed on this host; 0 template(s) not byte-comparable")
}

func TestE10HomeRelativeDirectoriesFollowHome(t *testing.T) {
	w := base()
	w.repo["files/home/.local/share/vmtest/a"] = file("new")
	w.host["home/user/.local/share/vmtest/a"] = file("old")
	r := w.run()
	r.expect(t, ExitFailed)
	contains(t, "stderr", r.errOut, "deployed: /home/user/.local/share/vmtest/a")
	contains(t, "stderr", r.errOut, "play-vm-test-lab.yml")
}

func TestE10RequestPyIsExcludedFromHelpersOnly(t *testing.T) {
	w := base()
	w.repo["helpers/vmtest/request.py"] = file("repo")
	w.host["usr/local/lib/ccy-helpers/helpers/vmtest/request.py"] = file("other")
	w.run().expect(t, ExitOK)
	// the exclusion belongs to the helpers entry: the same name elsewhere is compared.
	w.repo["files/usr/local/share/agent-bus/request.py"] = file("repo")
	w.host["usr/local/share/agent-bus/request.py"] = file("other")
	w.run().expect(t, ExitFailed)
}

func TestE10GlobSkipsDirectoriesAndDotfiles(t *testing.T) {
	w := base()
	w.repo["files/home/.local/share/vmtest/sub/inner"] = file("x")
	w.repo["files/home/.local/share/vmtest/.hidden"] = file("new")
	w.host["home/user/.local/share/vmtest/.hidden"] = file("old")
	w.run().expect(t, ExitOK)
}

// E12
func TestE12ByteDifferenceIsDriftAndUnreadableIsExitTwo(t *testing.T) {
	w := base()
	w.repo["files/home/.local/bin/tool"] = file("abc")
	w.host["home/user/.local/bin/tool"] = file("abd")
	w.run().expect(t, ExitFailed)
	w.host["home/user/.local/bin/tool"] = file("abc")
	w.run().expect(t, ExitOK)
	w.unreadOn = "home/user/.local/bin/tool"
	r := w.run()
	r.expect(t, ExitCannotRun)
	contains(t, "stderr", r.errOut, "/home/user/.local/bin/tool")
	contains(t, "stderr", r.errOut, "permission denied")
}

// E13 E14
func TestE13TierOneNamesTheDeclaringPlayAndNotACommentOrANeighbour(t *testing.T) {
	w := base()
	w.repo["files/home/.local/bin/ftp-camera"] = file("new")
	w.host["home/user/.local/bin/ftp-camera"] = file("old")
	w.repo["playbooks/play-b.yml"] = file("- src: \"{{ root_dir }}/files/home/.local/bin/ftp-camera\"\n")
	w.repo["playbooks/play-a.yml"] = file("  src: files/home/.local/bin/ftp-camera\n")
	w.repo["playbooks/play-c.yml"] = file("# files/home/.local/bin/ftp-camera is deployed elsewhere\n")
	w.repo["playbooks/play-d.yml"] = file("  src: files/home/.local/bin/ftp-camera-foo\n")
	r := w.run()
	r.expect(t, ExitFailed)
	contains(t, "stderr", r.errOut, "    repo:     /repo/files/home/.local/bin/ftp-camera\n")
	contains(t, "stderr", r.errOut, "    deployed: /home/user/.local/bin/ftp-camera\n")
	contains(t, "stderr", r.errOut, "    ansible-playbook playbooks/play-a.yml\n    ansible-playbook playbooks/play-b.yml\n")
	lacks(t, "stderr", r.errOut, "play-c.yml")
	lacks(t, "stderr", r.errOut, "play-d.yml")
	contains(t, "stderr", r.errOut, "The repo and the host disagree")
	contains(t, "stderr", r.errOut, "CLAUDE/InfrastructureAsCode.md")
}

func TestE13TierTwoUsesTheTemplatedSrcPlayAndTheWholeWordName(t *testing.T) {
	w := base()
	w.repo["files/home/.local/bin/lan"] = file("new")
	w.host["home/user/.local/bin/lan"] = file("old")
	w.repo["playbooks/loop.yml"] = file("src: \"{{ root }}/files/home/.local/bin/{{ item }}\"\nloop: [lan-scan]\n")
	r := w.run()
	r.expect(t, ExitFailed)
	contains(t, "stderr", r.errOut, "(no playbook references this file — it may be orphaned)")
	w.repo["playbooks/loop.yml"] = file("src: \"{{ root }}/files/home/.local/bin/{{ item }}\"\nloop: [lan, qnap]\n")
	r = w.run()
	contains(t, "stderr", r.errOut, "ansible-playbook playbooks/loop.yml")
}

// E15 E16
func TestE15PassLineAlwaysStatesAllThreeCountsAndListsTemplates(t *testing.T) {
	w := base()
	w.repo["files/home/.local/bin/b.j2"] = file("t")
	w.repo["files/home/.local/bin/a.j2"] = file("t")
	w.repo["playbooks/p.yml"] = file("dest: /.local/bin/a\ndest: /.local/bin/b\n")
	r := w.run()
	r.expect(t, ExitOK)
	contains(t, "stdout", r.out, "✓ deployed-drift: 0 deployed script(s) match the repo; 12 not installed on this host; 2 template(s) not byte-comparable\n")
	contains(t, "stdout", r.out, "    template (rendered, so not compared): a.j2 -> a\n    template (rendered, so not compared): b.j2 -> b\n")
	if r.errOut != "" {
		t.Errorf("a pass wrote to stderr: %q", r.errOut)
	}
}

func TestE16TemplatesAreNeverByteCompared(t *testing.T) {
	w := base()
	w.repo["files/home/.local/bin/t.j2"] = file("{{ user }}")
	w.host["home/user/.local/bin/t.j2"] = file("joe")
	w.host["home/user/.local/bin/t"] = file("joe")
	w.repo["playbooks/p.yml"] = file("dest: /.local/bin/t\n")
	w.run().expect(t, ExitOK)
}

// E3
func TestE3DriftGoesToStderrOnly(t *testing.T) {
	w := base()
	w.repo["files/home/.local/bin/tool"] = file("a")
	w.host["home/user/.local/bin/tool"] = file("b")
	r := w.run()
	r.expect(t, ExitFailed)
	if r.out != "" {
		t.Errorf("drift wrote to stdout: %q", r.out)
	}
}
