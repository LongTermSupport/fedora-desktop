package checkoutpin

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// realSharedTmp is sharedTmpDirs as the package has it; TestMain moves the list out of the way,
// since every test tree is made under /tmp.
var realSharedTmp []string

func TestMain(m *testing.M) {
	realSharedTmp = sharedTmpDirs
	sharedTmpDirs = []string{"/nonexistent-shared-tmp"}
	os.Exit(m.Run())
}

// mkdirs makes each directory under base.
func mkdirs(t *testing.T, base string, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(base, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

// approve approves the mounts file at r.dir, or fails the test.
func (r *rig) approve(t *testing.T) {
	t.Helper()
	r.answer = yes()
	if code := r.cmd().Approve(context.Background(), []string{"mounts", r.dir}); code != term.ExitOK {
		t.Fatalf("approve: %v %s", code, r.err.String())
	}
}

// M1 (finding 4): the temporary directories every process shares, the user's runtime
// directory and tmux's socket directory are denied: tmux's socket there runs any command on the
// host, and podman's socket is a container engine.
func TestSharedTmpIsDenied(t *testing.T) {
	for _, d := range []string{"/tmp", "/var/tmp"} {
		if !slices.Contains(realSharedTmp, d) {
			t.Errorf("sharedTmpDirs %v lack %s", realSharedTmp, d)
		}
	}
	r := newRig(t)
	runtime, tmux := filepath.Join(r.home, "run-user"), filepath.Join(r.home, "tmux-dir")
	mkdirs(t, r.home, "run-user/podman", "tmux-dir/tmux-1000")
	r.env["XDG_RUNTIME_DIR"], r.env["TMUX_TMPDIR"] = runtime, tmux
	for _, line := range []string{"~/run-user/podman:/p\n", "~/tmux-dir/tmux-1000:/t\n"} {
		write(t, r.dir, Mounts, line)
		if code := r.cmd().ProjectMounts(nil); code != term.ExitFailure || !strings.Contains(r.err.String(), "off limits") {
			t.Errorf("%q: %v %q", line, code, r.err.String())
		}
	}
	sharedTmpDirs = realSharedTmp
	defer func() { sharedTmpDirs = []string{"/nonexistent-shared-tmp"} }()
	if _, problems := ParseMounts([]byte("/tmp/tmux-1000:/t\n/var/tmp/x:/v\n/dev/shm/x:/s\n"), "/home/u", "/home/u/p", nil); len(problems) != 3 {
		t.Errorf("a shared temporary source passed: %v", problems)
	}
}

// M2 (finding 4, the hazard): a source a container session can write a part of is refused,
// since it could make that part a link to any host path: one inside the checkout, and one
// inside (or at) another read-write source of the same launch, the file's own lines and
// CCY_EXTRA_MOUNTS alike. Read-only sources may nest.
func TestMountsRefuseContainerWritableSources(t *testing.T) {
	r := newRig(t)
	mkdirs(t, r.home, "shared/models", "lib/sub", "extra/in")
	mkdirs(t, r.dir, "sub")
	for content, want := range map[string]string{
		r.dir + "/sub:/s\n":                       "inside the checkout",
		"~/shared:/s\n~/shared/models:/m:ro\n":    "inside " + filepath.Join(r.home, "shared"),
		"~/shared/models:/m:ro\n~/shared:/s:rw\n": "inside " + filepath.Join(r.home, "shared"),
		"~/shared:/s\n~/shared:/t:ro\n":           "inside " + filepath.Join(r.home, "shared"),
		"~/extra/in:/x:ro\n":                      "inside " + filepath.Join(r.home, "extra"),
	} {
		r.env["CCY_EXTRA_MOUNTS"] = "-v " + filepath.Join(r.home, "extra") + ":/e:rw -v /srv/ro:/r:ro"
		write(t, r.dir, Mounts, content)
		r.answer = yes()
		if code := r.cmd().Approve(context.Background(), []string{"mounts", r.dir}); code != term.ExitFailure || !strings.Contains(r.err.String(), want) {
			t.Errorf("%q approved, or not for %q: %v %q", content, want, code, r.err.String())
		}
	}
	write(t, r.dir, Mounts, "~/lib:/l:ro\n~/lib/sub:/s:ro\n")
	r.approve(t)
	if code := r.cmd().ProjectMounts(nil); code != term.ExitOK {
		t.Errorf("nested read-only sources refused: %s", r.err.String())
	}
}

// M3 (finding 4, the hazard): the approval covers where each source resolves, not only the
// file's text: a link on the way retargeted after the approval, or a source absent then and a
// link now, is refused at the next launch, unattended or not; the same target is not.
func TestMountsApprovalCoversWhereSourcesResolve(t *testing.T) {
	r := newRig(t)
	mkdirs(t, r.home, "a", "b", "later-target")
	link := filepath.Join(r.home, "data")
	if err := os.Symlink(filepath.Join(r.home, "a"), link); err != nil {
		t.Fatal(err)
	}
	write(t, r.dir, Mounts, "~/data:/d:ro\n~/later:/l:ro\n")
	r.approve(t)
	if !strings.Contains(r.err.String(), filepath.Join(r.home, "a")+" → /d (ro)") || !strings.Contains(r.err.String(), filepath.Join(r.home, "later")+" → /l (ro), not there yet") {
		t.Fatalf("approval did not show the resolved sources: %q", r.err.String())
	}
	mkdirs(t, r.home, "later")
	if code := r.cmd().ProjectMounts(nil); code != term.ExitOK || !strings.Contains(r.out.String(), filepath.Join(r.home, "a")+":/d:ro") {
		t.Fatalf("approved, same targets: %v %q %s", code, r.out.String(), r.err.String())
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(r.home, "b"), link); err != nil {
		t.Fatal(err)
	}
	if code := r.cmd().ProjectMounts(nil); code != term.ExitFailure || r.out.Len() != 0 || !strings.Contains(r.err.String(), "resolve") {
		t.Errorf("retargeted link: %v %q %q", code, r.out.String(), r.err.String())
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(r.home, "a"), link); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(r.home, "later")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(r.home, "later-target"), filepath.Join(r.home, "later")); err != nil {
		t.Fatal(err)
	}
	if code := r.cmd().ProjectMounts(nil); code != term.ExitFailure || r.out.Len() != 0 {
		t.Errorf("an absent source that became a link: %v %q %q", code, r.out.String(), r.err.String())
	}
}
