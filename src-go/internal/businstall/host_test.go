package businstall

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestRequireFedora(t *testing.T) {
	cases := []struct {
		name, content, want string
	}{
		{"fedora", "NAME=\"Fedora Linux\"\nID=fedora\n", ""},
		{"quoted id", "ID=\"fedora\"\n", ""},
		{"debian", "ID=debian\n", "says ID=debian: only Fedora is supported"},
		{"no id", "NAME=x\n", "says ID=<none>: only Fedora is supported"},
		{"two ids", "ID=fedora\nID=fedora\n", "says ID=fedora\nfedora: only Fedora is supported"},
		{"id like", "ID_LIKE=fedora\n", "says ID=<none>: only Fedora is supported"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.write(h.rel("etc/os-release"), tc.content, 0o644)
			err := h.app.requireFedora()
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			f, ok := errors.AsType[*failure](err)
			if !ok || f.code != term.ExitConfig || !strings.Contains(f.msg, tc.want) || !strings.Contains(f.msg, h.rel("etc/os-release")) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestRequireFedoraNamesAMissingOSRelease(t *testing.T) {
	h := newHarness(t)
	if err := os.Remove(h.rel("etc/os-release")); err != nil {
		t.Fatal(err)
	}
	f, ok := errors.AsType[*failure](h.app.requireFedora())
	if !ok || f.code != term.ExitConfig || f.msg != h.rel("etc/os-release")+" is missing: only Fedora is supported" {
		t.Fatalf("got %v", f)
	}
}

func TestDirsOpenLazilyAndShareOneHandle(t *testing.T) {
	h := newHarness(t)
	first, err := h.app.fsAt("/usr/local")
	if err != nil {
		t.Fatal(err)
	}
	again, err := h.app.fsAt("/usr/local")
	if err != nil || first != again {
		t.Fatalf("a second open returned %p, want %p (%v)", again, first, err)
	}
	if err := h.app.finish(); err != nil {
		t.Fatal(err)
	}
	if len(h.app.dirs) != 0 {
		t.Fatal("finish left directories open")
	}
}

func TestVarLibIsMadeInATestPrefixOnly(t *testing.T) {
	h := newHarness(t)
	if h.exists("var/lib") {
		t.Fatal("the harness made /var/lib")
	}
	if _, err := h.app.fsAt("/var/lib"); err != nil {
		t.Fatal(err)
	}
	if !h.exists("var/lib") {
		t.Fatal("a test prefix has no /var/lib")
	}
}

func TestSubDirectoriesAreRootedThroughTheirParents(t *testing.T) {
	h := newHarness(t)
	h.mkdir("var/lib/agent-bus/alpha")
	teamFS, err := h.app.fsAt("/var/lib/agent-bus/alpha")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := teamFS.EnsureDir("db", 0o700, mustOwner(t)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.stdout(), "") || !h.exists("var/lib/agent-bus/alpha/db") {
		t.Fatal("the directory was not made")
	}
}

func TestAbsentDirectoriesAreReportedAsAbsentByTryFS(t *testing.T) {
	h := newHarness(t)
	got, found, err := h.app.tryFS("/var/lib/agent-bus/alpha")
	if err != nil || found || got != nil {
		t.Fatalf("%v, %v, %v", got, found, err)
	}
	h.mkdir("var/lib/agent-bus/alpha")
	got, found, err = h.app.tryFS("/var/lib/agent-bus/alpha")
	if err != nil || !found || got == nil {
		t.Fatalf("%v, %v, %v", got, found, err)
	}
}

func TestADirectoryOtherThanAbsenceIsAnError(t *testing.T) {
	outside := t.TempDir()
	h := newHarness(t)
	h.mkdir("var/lib/agent-bus")
	if err := os.Symlink(outside, h.rel("var/lib/agent-bus/alpha")); err != nil {
		t.Fatal(err)
	}
	if _, found, err := h.app.tryFS("/var/lib/agent-bus/alpha"); err == nil || found {
		t.Fatalf("a team directory that is a symlink out of the root was opened: %v %v", found, err)
	}
}

func TestOwnersAreLookedUpOnceTheyResolve(t *testing.T) {
	h := newHarness(t)
	calls := 0
	self := mustOwner(t)
	h.app.cfg.SvcOwner = func() (fsx.Owner, error) {
		calls++
		if calls == 1 {
			return fsx.Owner{}, errors.New("no such user yet")
		}
		return self, nil
	}
	if _, err := h.app.svcOwner(); err == nil {
		t.Fatal("the first lookup should fail")
	}
	for range 3 {
		if got, err := h.app.svcOwner(); err != nil || got != self {
			t.Fatalf("%v, %v", got, err)
		}
	}
	if calls != 2 {
		t.Fatalf("lookups: %d", calls)
	}
	group, err := h.app.groupOwner()
	if err != nil || group != self {
		t.Fatalf("%v, %v", group, err)
	}
	h.app.cfg.RootOwner = func() (fsx.Owner, error) { return fsx.Owner{}, errors.New("no root") }
	h.app.root = nil
	if _, err := h.app.rootOwner(); err == nil {
		t.Fatal("a failed root lookup returned no error")
	}
	if _, err := h.app.groupOwner(); err == nil {
		t.Fatal("a failed root lookup returned no error for the group owner")
	}
}

func TestGroupOwnerCombinesRootsUserWithTheServiceGroup(t *testing.T) {
	h := newHarness(t)
	h.app.cfg.RootOwner = func() (fsx.Owner, error) { return fsx.Owner{UID: 1, GID: 2}, nil }
	h.app.cfg.SvcOwner = func() (fsx.Owner, error) { return fsx.Owner{UID: 3, GID: 4}, nil }
	got, err := h.app.groupOwner()
	if err != nil || got != (fsx.Owner{UID: 1, GID: 4}) {
		t.Fatalf("%v, %v", got, err)
	}
}

func TestTheWorkDirectoryIsRootsOwnUnderTheInstallState(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	h := newHarness(t)
	dir, err := h.app.newWorkDir()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(dir) != h.rel("var/lib/agent-bus-install") || !strings.HasPrefix(filepath.Base(dir), ".work-") {
		t.Fatalf("work dir %s", dir)
	}
	if info, statErr := os.Stat(dir); statErr != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("%v, %v", info, statErr)
	}
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.app.finish(); err != nil {
		t.Fatal(err)
	}
	if h.exists("var/lib/agent-bus-install/" + filepath.Base(dir)) {
		t.Fatal("the work directory was left behind")
	}
	if entries, readErr := os.ReadDir(os.Getenv("TMPDIR")); readErr != nil || len(entries) != 0 {
		t.Fatalf("TMPDIR was used: %v %v", entries, readErr)
	}
}

func TestRequireTeamDirNeedsTheDirectoryAndItsTeamFile(t *testing.T) {
	h := newHarness(t)
	want := "team alpha is not installed here (no /var/lib/agent-bus/alpha/team.json)"
	for _, setup := range []func(){
		func() {},
		func() { h.mkdir("var/lib/agent-bus/alpha") },
		func() { h.mkdir("var/lib/agent-bus/alpha/team.json") },
	} {
		setup()
		f, ok := errors.AsType[*failure](requireTeam(h))
		if !ok || f.code != term.ExitConfig || f.msg != want {
			t.Fatalf("got %v", f)
		}
	}
	h.write(h.rel("var/lib/agent-bus/beta/team.json"), `{"team":"beta","port":8448,"state":"present","listen":["192.0.2.10"]}`, 0o640)
	teamFS, err := h.app.requireTeamDir("beta")
	if err != nil || teamFS == nil {
		t.Fatalf("%v, %v", teamFS, err)
	}
	doc, err := h.app.readTeamDoc(teamFS, "team.json")
	if err != nil || doc.Team != "beta" || doc.Port != 8448 || doc.Listen[0] != "192.0.2.10" {
		t.Fatalf("%+v, %v", doc, err)
	}
}

func requireTeam(h *harness) error {
	_, err := h.app.requireTeamDir("alpha")
	return err
}

func TestReadTeamDocRefusesWhatIsNotJSONOrHasNoPort(t *testing.T) {
	h := newHarness(t)
	h.mkdir("var/lib/agent-bus/alpha")
	teamFS, err := h.app.fsAt("/var/lib/agent-bus/alpha")
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"bad": "{not json", "noport": `{"team":"x"}`, "stringport": `{"port":"8448"}`} {
		h.write(h.rel("var/lib/agent-bus/alpha/"+name), content, 0o640)
		_, err := h.app.readTeamDoc(teamFS, name)
		f, ok := errors.AsType[*failure](err)
		if !ok || f.code != term.ExitConfig || !strings.Contains(f.msg, "/var/lib/agent-bus/alpha/"+name+": not readable JSON") && !strings.Contains(f.msg, "has no port") {
			t.Errorf("%s: got %v", name, err)
		}
	}
	if _, err := h.app.readTeamDoc(teamFS, "absent"); err == nil {
		t.Error("a missing file returned no error")
	}
}

func TestEnsureOSParentLeavesAnExistingDirectoryAlone(t *testing.T) {
	h := newHarness(t)
	h.mkdir("usr/local/lib")
	local, err := h.app.fsAt("/usr/local")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(h.rel("usr/local/lib"), fs.FileMode(0o711)); err != nil {
		t.Fatal(err)
	}
	if err := h.app.ensureOSParent(local, "lib"); err != nil {
		t.Fatal(err)
	}
	if err := h.app.ensureOSParent(local, "share"); err != nil {
		t.Fatal(err)
	}
	if h.mode("usr/local/lib") != 0o711 || h.mode("usr/local/share") != 0o755 {
		t.Fatalf("modes %o %o", h.mode("usr/local/lib"), h.mode("usr/local/share"))
	}
	if h.stdout() != "CHANGED\tdirectory /usr/local/share\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
}

func mustOwner(t *testing.T) fsx.Owner {
	t.Helper()
	o, err := fsx.CurrentOwner()
	if err != nil {
		t.Fatal(err)
	}
	return o
}
