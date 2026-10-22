package admin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// hostFile writes content to root+path with mode, its parents made.
func hostFile(t *testing.T, root, path, content string, mode os.FileMode) string {
	t.Helper()
	full := root + path
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(full, mode); err != nil {
		t.Fatal(err)
	}
	return full
}

// self is the owner of the test's files.
func self(t *testing.T) uint32 {
	t.Helper()
	uid, err := rootOwner(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return uid
}

func groups(name string) (int, error) {
	if name == "teamchat-owners" {
		return 1234, nil
	}
	return 0, errors.New("unknown group " + name)
}

// TestOwnerGroupFileIsOneGroupName is HF1.
func TestOwnerGroupFileIsOneGroupName(t *testing.T) {
	for _, content := range []string{"teamchat-owners", "teamchat-owners\n"} {
		root := t.TempDir()
		hostFile(t, root, paths.OwnerGroupFile, content, 0o644)
		gid, err := ownerGroupFile(root, groups, self(t))()
		if err != nil || gid != 1234 {
			t.Fatalf("%q: %d, %v", content, gid, err)
		}
	}
	cases := map[string]struct {
		content string
		mode    os.FileMode
	}{
		"two newlines":     {"teamchat-owners\n\n", 0o644},
		"two names":        {"teamchat-owners\nother\n", 0o644},
		"empty":            {"", 0o644},
		"a space":          {"teamchat owners", 0o644},
		"unknown":          {"nobody-here", 0o644},
		"group writable":   {"teamchat-owners", 0o664},
		"other writable":   {"teamchat-owners", 0o646},
		"over the cap":     {strings.Repeat("a", maxHostFile+1), 0o644},
		"carriage return":  {"teamchat-owners\r\n", 0o644},
		"a leading hyphen": {"-teamchat", 0o644},
	}
	for name, c := range cases {
		root := t.TempDir()
		hostFile(t, root, paths.OwnerGroupFile, c.content, c.mode)
		if _, err := ownerGroupFile(root, groups, self(t))(); ExitCodeOf(err) != ExitConfig {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := ownerGroupFile(t.TempDir(), groups, self(t))(); ExitCodeOf(err) != ExitConfig || !strings.Contains(err.Error(), paths.OwnerGroupFile) {
		t.Fatalf("missing: %v", err)
	}
	root := t.TempDir()
	hostFile(t, root, paths.OwnerGroupFile, "teamchat-owners", 0o644)
	if _, err := ownerGroupFile(root, groups, self(t)+1)(); ExitCodeOf(err) != ExitConfig {
		t.Fatalf("another owner: %v", err)
	}
}

// TestHostFilesAreReadByTheTrustRule: a link, a FIFO (which must not block the open) and a
// directory are refused.
func TestHostFilesAreReadByTheTrustRule(t *testing.T) {
	root := t.TempDir()
	target := hostFile(t, root, "/elsewhere", "teamchat-owners", 0o644)
	full := root + paths.OwnerGroupFile
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, full); err != nil {
		t.Fatal(err)
	}
	if _, err := ownerGroupFile(root, groups, self(t))(); ExitCodeOf(err) != ExitConfig {
		t.Fatalf("link: %v", err)
	}
	if err := os.Remove(full); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(full, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ownerGroupFile(root, groups, self(t))(); ExitCodeOf(err) != ExitConfig {
		t.Fatalf("fifo: %v", err)
	}
	if err := os.Remove(full); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(full, 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := ownerGroupFile(root, groups, self(t))(); ExitCodeOf(err) != ExitConfig {
		t.Fatalf("directory: %v", err)
	}
}

// TestTheExportedReadersWantRoot: the production readers want root's files.
func TestTheExportedReadersWantRoot(t *testing.T) {
	root := t.TempDir()
	group := hostFile(t, root, paths.OwnerGroupFile, "teamchat-owners", 0o644)
	pin := hostFile(t, root, paths.ServerPinFile, goodPin, 0o644)
	if os.Geteuid() == 0 {
		if gid, err := OwnerGroupFile(root, groups)(); err != nil || gid != 1234 {
			t.Fatalf("root's group file: %d, %v", gid, err)
		}
		if _, err := ReadPin(root); err != nil {
			t.Fatalf("root's pin: %v", err)
		}
		for _, f := range []string{group, pin} {
			if err := os.Chown(f, 4242, 4242); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := OwnerGroupFile(root, groups)(); ExitCodeOf(err) != ExitConfig {
		t.Fatalf("a group file root does not own: %v", err)
	}
	if _, err := ReadPin(root); ExitCodeOf(err) != ExitConfig {
		t.Fatalf("a pin root does not own: %v", err)
	}
}

func TestLookupGroupFindsTheSystemsGroups(t *testing.T) {
	if gid, err := LookupGroup("root"); err != nil || gid != 0 {
		t.Fatalf("root: %d, %v", gid, err)
	}
	if _, err := LookupGroup("teamchat-no-such-group"); err == nil {
		t.Fatal("an unknown group was found")
	}
}

const goodPin = `{"v":1,"version":"1.9.4","memory_high":"512M","cache_capacity_modifier":"0.5"}`

// TestReadPinIsStrict is HF2.
func TestReadPinIsStrict(t *testing.T) {
	root := t.TempDir()
	hostFile(t, root, paths.ServerPinFile, goodPin+"\n", 0o644)
	pin, err := readPin(root, self(t))
	if err != nil {
		t.Fatal(err)
	}
	if pin.Version.String() != "1.9.4" || pin.MemoryHigh != "512M" || pin.CacheCapacityModifier != "0.5" {
		t.Fatalf("pin %+v", pin)
	}
	for name, content := range map[string]string{
		"v 2":           `{"v":2,"version":"1.9.4","memory_high":"512M","cache_capacity_modifier":"0.5"}`,
		"extra key":     `{"v":1,"version":"1.9.4","memory_high":"512M","cache_capacity_modifier":"0.5","x":1}`,
		"missing key":   `{"v":1,"version":"1.9.4","memory_high":"512M"}`,
		"version":       `{"v":1,"version":"1.9 4","memory_high":"512M","cache_capacity_modifier":"0.5"}`,
		"memory number": `{"v":1,"version":"1.9.4","memory_high":512,"cache_capacity_modifier":"0.5"}`,
		"modifier":      `{"v":1,"version":"1.9.4","memory_high":"512M","cache_capacity_modifier":0.5}`,
		"version num":   `{"v":1,"version":1,"memory_high":"512M","cache_capacity_modifier":"0.5"}`,
		"duplicate":     `{"v":1,"v":1,"version":"1.9.4","memory_high":"512M","cache_capacity_modifier":"0.5"}`,
		"not json":      `v=1`,
	} {
		hostFile(t, root, paths.ServerPinFile, content, 0o644)
		if _, err := readPin(root, self(t)); ExitCodeOf(err) != ExitConfig {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := readPin(t.TempDir(), self(t)); ExitCodeOf(err) != ExitConfig {
		t.Fatalf("missing: %v", err)
	}
	hostFile(t, root, paths.ServerPinFile, goodPin, 0o664)
	if _, err := readPin(root, self(t)); ExitCodeOf(err) != ExitConfig {
		t.Fatalf("group writable: %v", err)
	}
}

// TestIsFedoraReadsOSRelease is HF3's reading: ID=fedora, quoted or not.
func TestIsFedoraReadsOSRelease(t *testing.T) {
	for _, content := range []string{"NAME=Fedora\nID=fedora\n", "ID=\"fedora\"\n", "ID='fedora'", "VERSION_ID=44\nID=fedora"} {
		root := t.TempDir()
		hostFile(t, root, osRelease, content, 0o644)
		if err := checkFedora(root, self(t)); err != nil {
			t.Errorf("%q: %v", content, err)
		}
	}
	for _, content := range []string{"ID=ubuntu\n", "ID_LIKE=fedora\n", "ID=fedora-ish\n", "", "ID=\"fedora\n", "# ID=fedora\n"} {
		root := t.TempDir()
		hostFile(t, root, osRelease, content, 0o644)
		if err := checkFedora(root, self(t)); ExitCodeOf(err) != ExitConfig {
			t.Errorf("%q: %v", content, err)
		}
	}
	if err := checkFedora(t.TempDir(), self(t)); ExitCodeOf(err) != ExitConfig {
		t.Fatalf("missing: %v", err)
	}
}

// TestOSReleaseIsRootsFile: group or other writable, or another owner, is refused; the owner is
// root on the host and the test root's owner under one.
func TestOSReleaseIsRootsFile(t *testing.T) {
	group := t.TempDir()
	hostFile(t, group, osRelease, "ID=fedora\n", 0o664)
	if err := checkFedora(group, self(t)); ExitCodeOf(err) != ExitConfig {
		t.Fatalf("group writable: %v", err)
	}
	if err := checkFedora(group, self(t)+1); ExitCodeOf(err) != ExitConfig {
		t.Fatalf("another owner: %v", err)
	}
	if uid, err := rootOwner(""); err != nil || uid != 0 {
		t.Fatalf("the host's owner: %d, %v", uid, err)
	}
	if uid, err := rootOwner(group); err != nil || uid != self(t) {
		t.Fatalf("a test root's owner: %d, %v", uid, err)
	}
	if _, err := rootOwner(group + "/missing"); ExitCodeOf(err) != ExitConfig {
		t.Fatalf("a missing root: %v", err)
	}
	root := t.TempDir()
	target := hostFile(t, root, "/usr/lib/os-release", "ID=fedora\n", 0o644)
	if err := os.MkdirAll(root+"/etc", 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../usr/lib/os-release", root+osRelease); err != nil {
		t.Fatal(err)
	}
	if err := checkFedora(root, self(t)); err != nil || target == "" {
		t.Fatalf("the distribution's link: %v", err)
	}
}

// TestTeamApplyChecksFedoraFirst is VB5: team apply refuses with 78 before it reads its input.
func TestTeamApplyChecksFedoraFirst(t *testing.T) {
	w := newWorld(t)
	hostFile(t, w.root, osRelease, "ID=debian\n", 0o644)
	out := w.verb("not even the input", "team", "apply")
	if out.code != ExitConfig || !strings.Contains(out.stderr, "Fedora") || out.stdout != "" {
		t.Fatalf("%+v", out)
	}
	if out.code == term.ExitOK {
		t.Fatal("applied")
	}
}
