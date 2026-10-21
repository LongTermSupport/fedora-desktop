package statusline

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const hostUser = "@myrepo.1+workstation.host:alpha.example.org"

// rolesWorld is a pingbus home with the bundle of team alpha kept elsewhere and linked in, or in
// place, and a passwd home for the Element checks.
type rolesWorld struct {
	t    *testing.T
	root string
}

func (rw rolesWorld) path(rel string) string { return filepath.Join(rw.root, rel) }

func (rw rolesWorld) mkdir(rel string, mode os.FileMode) {
	rw.t.Helper()
	if err := os.MkdirAll(rw.path(rel), 0o700); err != nil {
		rw.t.Fatal(err)
	}
	if err := os.Chmod(rw.path(rel), mode); err != nil {
		rw.t.Fatal(err)
	}
}

func (rw rolesWorld) write(rel, content string) {
	rw.t.Helper()
	if err := os.WriteFile(rw.path(rel), []byte(content), 0o600); err != nil {
		rw.t.Fatal(err)
	}
}

func (rw rolesWorld) link(target, rel string) {
	rw.t.Helper()
	if err := os.Symlink(target, rw.path(rel)); err != nil {
		rw.t.Fatal(err)
	}
}

// bundle writes a bundle of alpha for user at rel.
func (rw rolesWorld) bundle(rel, user string) {
	rw.t.Helper()
	rw.mkdir(rel+"/state", 0o700)
	rw.mkdir(rel, 0o700)
	rw.write(rel+"/member.json", strings.Replace(alphaMember, "@myrepo.1+workstation.podman:alpha.example.org", user, 1))
	rw.write(rel+"/token", "syt_token")
	record := strings.Replace(alphaRecord(roleWorker), "@myrepo.1+workstation.podman:alpha.example.org", user, 1)
	rw.write(rel+"/state/team.json", record)
}

func newRolesWorld(t *testing.T) rolesWorld {
	t.Helper()
	rw := rolesWorld{t: t, root: t.TempDir()}
	rw.mkdir("pingbus", 0o700)
	rw.mkdir("user", 0o700)
	return rw
}

// roles reads alpha's role with passwd home <root>/user, or homeErr when it is given.
func (rw rolesWorld) roles(homeErr error) (string, string) {
	rw.t.Helper()
	userHome := func(int) (string, error) { return rw.path("user"), homeErr }
	w := newWorld(rw.root, time.Unix(0, 0), userHome)
	roles, problem := w.readRoles(map[string]string{"PINGBUS_HOME": rw.path("pingbus")}, []string{"alpha"})
	return roles["alpha"], problem
}

// S21: a linked bundle is read through its link only when the target is a real directory of
// this user's with mode 0700.
func TestALinkedBundleNeedsAPrivateRealDirectory(t *testing.T) {
	rw := newRolesWorld(t)
	rw.bundle("kept/alpha", "@myrepo.1+workstation.podman:alpha.example.org")
	rw.link("../kept/alpha", "pingbus/alpha")
	if role, problem := rw.roles(nil); role != roleWorker || problem != "" {
		t.Fatalf("a good link: %q %q", role, problem)
	}
	rw.mkdir("kept/alpha", 0o750)
	if _, problem := rw.roles(nil); !strings.Contains(problem, "links to "+rw.root+"/pingbus/../kept/alpha: mode 0750, not 0700") {
		t.Errorf("a loose target: %q", problem)
	}
	rw.mkdir("kept/alpha", 0o700)
	rw.link("alpha", "kept/beta")
	if err := os.Remove(rw.path("pingbus/alpha")); err != nil {
		t.Fatal(err)
	}
	rw.link(rw.path("kept/beta"), "pingbus/alpha")
	if _, problem := rw.roles(nil); !strings.Contains(problem, "a link onto a link is refused") {
		t.Errorf("a link onto a link: %q", problem)
	}
	rw.write("kept/file", "")
	if err := os.Remove(rw.path("pingbus/alpha")); err != nil {
		t.Fatal(err)
	}
	rw.link(rw.path("kept/file"), "pingbus/alpha")
	if _, problem := rw.roles(nil); !strings.Contains(problem, ": not a directory") {
		t.Errorf("a link to a file: %q", problem)
	}
	if err := os.Remove(rw.path("pingbus/alpha")); err != nil {
		t.Fatal(err)
	}
	rw.link(rw.path("kept/none"), "pingbus/alpha")
	if _, problem := rw.roles(nil); !strings.Contains(problem, ": no such directory") {
		t.Errorf("a dangling link: %q", problem)
	}
}

// S21: a host member is refused beside an Element profile, unless its bundle is linked in and
// the user has accepted it; without a passwd home it is refused.
func TestAHostMemberBesideElement(t *testing.T) {
	rw := newRolesWorld(t)
	rw.bundle("pingbus/alpha", hostUser)
	if role, problem := rw.roles(nil); role != roleWorker || problem != "" {
		t.Fatalf("no Element: %q %q", role, problem)
	}
	if _, problem := rw.roles(errors.New("no passwd entry")); !strings.Contains(problem, "needs this user's home") {
		t.Errorf("no passwd home: %q", problem)
	}
	rw.mkdir("user/.config/Element-work", 0o700)
	if _, problem := rw.roles(nil); !strings.Contains(problem, "must not run as a user with an Element profile") {
		t.Errorf("a native profile: %q", problem)
	}
	if err := os.Remove(rw.path("user/.config/Element-work")); err != nil {
		t.Fatal(err)
	}
	rw.mkdir("user/.var/app", 0o700)
	rw.link("gone", "user/.var/app/im.riot.Riot")
	if _, problem := rw.roles(nil); !strings.Contains(problem, "must not run as a user with an Element profile") {
		t.Errorf("a dangling Flatpak link: %q", problem)
	}

	if err := os.Rename(rw.path("pingbus/alpha"), rw.path("kept")); err != nil {
		t.Fatal(err)
	}
	rw.link(rw.path("kept"), "pingbus/alpha")
	rw.mkdir("user/.config/pingbus", 0o700)
	rw.write("user/.config/pingbus/element-accepted", "")
	if role, problem := rw.roles(nil); role != roleWorker || problem != "" {
		t.Errorf("linked and accepted: %q %q", role, problem)
	}
	if err := os.Chmod(rw.path("user/.config/pingbus/element-accepted"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, problem := rw.roles(nil); !strings.Contains(problem, "Element profile") {
		t.Errorf("an acceptance others can read: %q", problem)
	}
}

// parentOf is pathlib's parent.
func TestParentOf(t *testing.T) {
	for in, want := range map[string]string{"/a/b": "/a", "/a": "/", "a": ".", "a/b": "a"} {
		if got := parentOf(in); got != want {
			t.Errorf("parentOf(%q) = %q, want %q", in, got, want)
		}
	}
}
