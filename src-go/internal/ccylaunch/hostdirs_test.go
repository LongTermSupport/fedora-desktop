package ccylaunch

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// repoRoot is the checkout this package is in.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func checkoutFile(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// The table is lib/host-paths.bash's three calls, in order, with the same defaults: a new
// override there that the front did not check would let it answer where the bash refuses.
func TestHostDirsAreTheLibrarysCalls(t *testing.T) {
	text := checkoutFile(t, "files/var/local/claude-yolo/lib/host-paths.bash")
	call := regexp.MustCompile(`(?m)^ccy_host_dir (\S+) (\S+) (\S+)( absent-ok)? \|\| exit 1$`)
	found := call.FindAllStringSubmatch(text, -1)
	if len(found) != len(hostDirs) {
		t.Fatalf("host-paths.bash makes %d ccy_host_dir calls, the front checks %d", len(found), len(hostDirs))
	}
	for i, m := range found {
		d := hostDirs[i]
		if m[2] != d.env || m[3] != d.def || (m[4] != "") != d.absentOK {
			t.Errorf("call %d is %q; the front has %+v", i, m[0], d)
		}
	}
}

// hostDirOK says yes only where ccy_host_dir accepts: unset or empty, or an absolute path whose
// resolution is the default or outside the system trees and is a directory (its parent, for
// absent-ok).
func TestHostDirOK(t *testing.T) {
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := filepath.Join(tmp, "ctx")
	if err := os.Mkdir(ctx, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(ctx, filepath.Join(tmp, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev", filepath.Join(tmp, "to-dev")); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		value    string
		def      string
		absentOK bool
		want     bool
	}{
		{"", "/opt/claude-yolo", false, true},
		{"relative", "/opt/claude-yolo", false, false},
		{ctx, "/opt/claude-yolo", false, true},
		{ctx + "/", "/opt/claude-yolo", false, true},
		{filepath.Join(tmp, "link"), "/opt/claude-yolo", false, true},
		{filepath.Join(tmp, "missing"), "/opt/claude-yolo", false, false},
		{filepath.Join(tmp, "missing"), "/dev/dri", true, true},
		{filepath.Join(tmp, "missing", "dri"), "/dev/dri", true, false},
		{"/", "/opt/claude-yolo", false, false},
		{"/dev", "/dev/dri", true, false},
		{"/dev/shm", "/dev/dri", true, false},
		{"/dev/dri", "/dev/dri", true, true},
		{"/proc/self", "/opt/claude-yolo", false, false},
		{"/sys", "/opt/claude-yolo", false, false},
		{filepath.Join(tmp, "to-dev", "shm"), "/dev/dri", true, false},
		{ctx + "/../ctx", "/opt/claude-yolo", false, true},
	}
	for _, c := range cases {
		if got := hostDirOK(c.value, c.def, c.absentOK); got != c.want {
			t.Errorf("hostDirOK(%q, %q, %v) = %v, want %v", c.value, c.def, c.absentOK, got, c.want)
		}
	}
}

// realpathM resolves as `realpath -m` does: every symbolic link in the part that exists, the
// rest lexically. The real realpath is the oracle.
func TestRealpathMIsRealpath(t *testing.T) {
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"a/b", "c"} {
		if err := os.MkdirAll(filepath.Join(tmp, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(tmp, "file"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	links := map[string]string{"abs": filepath.Join(tmp, "a", "b"), "rel": "a/b", "up": "../" + filepath.Base(tmp) + "/c", "chain": "rel", "dangling": "nowhere/x"}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(tmp, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{
		"/", "/.", tmp, tmp + "/a/b/", tmp + "/abs", tmp + "/rel/../c", tmp + "/up", tmp + "/chain/x/y",
		tmp + "/dangling", tmp + "/dangling/../z", tmp + "/missing/../a", tmp + "/file/x", tmp + "//a/./b",
	} {
		res, err := execx.OS{}.Run(context.Background(), execx.Cmd{Name: "realpath", Args: []string{"-m", "--", p}})
		if err != nil {
			t.Fatalf("realpath -m %s: %v", p, err)
		}
		want := strings.TrimSuffix(string(res.Stdout), "\n")
		got, err := realpathM(p)
		if err != nil || got != want {
			t.Errorf("realpathM(%q) = %q, %v; realpath -m says %q", p, got, err, want)
		}
	}
}

func TestRealpathMRefusesALinkLoop(t *testing.T) {
	tmp := t.TempDir()
	if err := os.Symlink("loop", filepath.Join(tmp, "loop")); err != nil {
		t.Fatal(err)
	}
	if got, err := realpathM(filepath.Join(tmp, "loop")); err == nil {
		t.Fatalf("a link to itself resolved to %q", got)
	}
}
