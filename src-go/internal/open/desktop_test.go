package open

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const gioWithDefault = "Default application for “image/png”: org.gnome.Loupe.desktop\n" +
	"Registered applications:\n\torg.gimp.GIMP.desktop\n\torg.gnome.Loupe.desktop\n" +
	"Recommended applications:\n\torg.gnome.Loupe.desktop\n\tdisplay-im7.q16.desktop\n"

const gioNoDefault = "No default applications for “image/png”\n" +
	"Registered applications:\n\torg.gimp.GIMP.desktop\n" +
	"No recommended applications\n"

// E8: the default first, then the registered and recommended IDs in order, each once.
func TestParseGioMime(t *testing.T) {
	def, ids := parseGioMime(gioWithDefault)
	if def != "org.gnome.Loupe.desktop" {
		t.Fatalf("default %q", def)
	}
	want := []string{"org.gnome.Loupe.desktop", "org.gimp.GIMP.desktop", "display-im7.q16.desktop"}
	if !slices.Equal(ids, want) {
		t.Fatalf("ids %q", ids)
	}
	def, ids = parseGioMime(gioNoDefault)
	if def != "" || !slices.Equal(ids, []string{"org.gimp.GIMP.desktop"}) {
		t.Fatalf("no default: %q %q", def, ids)
	}
	if def, ids = parseGioMime("No default applications for “x/y”\nNo registered applications\nNo recommended applications\n"); def != "" || len(ids) != 0 {
		t.Fatalf("nothing: %q %q", def, ids)
	}
}

// E8: gio mime runs in the C locale, so the headings are the untranslated ones.
func TestGioMimeRunsInTheCLocale(t *testing.T) {
	w := newWorld(t)
	p := w.file("a.png", "x")
	w.types("image/png")
	w.gui("image/png", gioWithDefault)
	w.desktop("org.gnome.Loupe.desktop", "[Desktop Entry]\nName=Image Viewer\n")
	if code := w.run("--list", p); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	c := w.cmd("gio mime image/png")
	if !slices.Contains(c.Env, "LC_ALL=C.UTF-8") {
		t.Fatalf("gio env %q", c.Env)
	}
}

// E8, B7: a graphical session needs gio.
func TestGraphicalSessionWithoutGioFails(t *testing.T) {
	w := newWorld(t)
	p := w.file("a.png", "x")
	w.types("image/png")
	w.env["DISPLAY"] = ":0"
	if code := w.run(p); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	w.wantStderr("open: gio (glib2) is not installed; it lists and starts desktop apps. 'open -t' offers terminal handlers only.\n")
}

// E8, B5: a failing gio mime is exit 1, never "no applications".
func TestFailingGioMimeFails(t *testing.T) {
	w := newWorld(t)
	p := w.file("a.png", "x")
	w.types("image/png")
	w.env["DISPLAY"] = ":0"
	w.install("gio")
	w.runner.On([]string{"gio", "mime"}, execx.Result{ExitCode: 1, Stderr: []byte("bad type")})
	if code := w.run(p); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	w.wantStderr("open: gio mime image/png failed: gio exited 1: bad type\n")
}

// E7: -t, or no display, means no desktop applications and no gio.
func TestNoGraphicalSessionAsksNoGio(t *testing.T) {
	w := newWorld(t)
	p := w.file("a.txt", "x")
	w.types("text/plain")
	w.gui("text/plain", "Default application for “text/plain”: gedit.desktop\n")
	w.install("less")
	if code := w.run("-t", "--list", p); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	if w.runner.Called("gio") {
		t.Fatalf("gio asked under -t: %q", w.ran())
	}
}

// E9: the user's directory before the system's; a missing directory is skipped.
func TestDesktopEntrySearchOrder(t *testing.T) {
	w := newWorld(t)
	sys := w.desktop("foo.desktop", "[Desktop Entry]\nName=System\n")
	idx, err := w.app().newRun().desktopIndex()
	if err != nil {
		t.Fatal(err)
	}
	if got := idx["foo.desktop"]; got != sys {
		t.Fatalf("system entry %q", got)
	}
	user := filepath.Join(w.home, ".local", "share", "applications", "foo.desktop")
	if err = os.MkdirAll(filepath.Dir(user), 0o755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(user, []byte("[Desktop Entry]\nName=User\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w.env["XDG_DATA_DIRS"] = filepath.Join(w.home, "missing") + ":" + w.dataDirs
	idx, err = w.app().newRun().desktopIndex()
	if err != nil || idx["foo.desktop"] != user {
		t.Fatalf("user entry: %q, %v", idx["foo.desktop"], err)
	}
	w.env["XDG_DATA_HOME"] = filepath.Join(w.home, "elsewhere")
	idx, err = w.app().newRun().desktopIndex()
	if err != nil || idx["foo.desktop"] != sys {
		t.Fatalf("XDG_DATA_HOME moves the user directory: %q, %v", idx["foo.desktop"], err)
	}
}

// E9, B3: an ID is the path below the directory with / read as -.
func TestDesktopIDIncludesTheSubdirectory(t *testing.T) {
	w := newWorld(t)
	p := w.desktop("kde/foo.desktop", "[Desktop Entry]\nName=K\n")
	idx, err := w.app().newRun().desktopIndex()
	if err != nil {
		t.Fatal(err)
	}
	if idx["kde-foo.desktop"] != p {
		t.Fatalf("index %q", idx)
	}
	if _, found := idx["foo.desktop"]; found {
		t.Fatal("foo.desktop is not the ID of kde/foo.desktop")
	}
}

// E9, B2: a symlinked entry is followed; a dangling one or a directory is not an entry.
func TestDesktopEntrySymlinksAreFollowed(t *testing.T) {
	w := newWorld(t)
	exported := w.file("exports/org.flat.App.desktop", "[Desktop Entry]\nName=Flat\n")
	apps := filepath.Join(w.dataDirs, "applications")
	mustSymlink(t, exported, filepath.Join(apps, "org.flat.App.desktop"))
	mustSymlink(t, filepath.Join(w.home, "gone"), filepath.Join(apps, "dangling.desktop"))
	if err := os.Mkdir(filepath.Join(apps, "dir.desktop"), 0o755); err != nil {
		t.Fatal(err)
	}
	idx, err := w.app().newRun().desktopIndex()
	if err != nil {
		t.Fatal(err)
	}
	if idx["org.flat.App.desktop"] != filepath.Join(apps, "org.flat.App.desktop") {
		t.Fatalf("index %q", idx)
	}
	for _, id := range []string{"dangling.desktop", "dir.desktop"} {
		if _, found := idx[id]; found {
			t.Fatalf("%s indexed", id)
		}
	}
}

// E9: a directory that cannot be read is a failure, not an empty one.
func TestUnreadableApplicationsDirectoryFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads any directory")
	}
	w := newWorld(t)
	apps := filepath.Join(w.dataDirs, "applications")
	if err := os.Chmod(apps, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(apps, 0o755); err != nil {
			t.Error(err)
		}
	})
	if _, err := w.app().newRun().desktopIndex(); err == nil || !strings.Contains(err.Error(), "cannot read") {
		t.Fatalf("err %v", err)
	}
}

// E10, B4: the [Desktop Entry] group's unlocalised Name, else the ID.
func TestDesktopName(t *testing.T) {
	w := newWorld(t)
	for content, want := range map[string]string{
		"[Desktop Entry]\nName[de]=Bild\nName = Image Viewer\n":                       "Image Viewer",
		"[Desktop Action new]\nName=New Window\n[Desktop Entry]\nName=Real\n":         "Real",
		"# c\n[Desktop Entry]\nExec=x\n[Desktop Action a]\nName=Not this\n":           "foo",
		"[Desktop Entry]\nGenericName=Viewer\nName=Viewer App\nName=Second ignored\n": "Viewer App",
	} {
		p := w.desktop("foo.desktop", content)
		got, err := desktopName(p, "foo.desktop")
		if err != nil || got != want {
			t.Fatalf("%q: got %q, %v, want %q", content, got, err, want)
		}
	}
}

// E8-E10, B1: desktop candidates are labelled with [GUI], the default first; an ID with no entry
// is not offered.
func TestDesktopCandidatesInTheList(t *testing.T) {
	w := newWorld(t)
	p := w.file("a.png", "x")
	w.types("image/png")
	w.gui("image/png", gioWithDefault)
	w.desktop("org.gnome.Loupe.desktop", "[Desktop Entry]\nName=Image Viewer\n")
	w.desktop("org.gimp.GIMP.desktop", "[Desktop Entry]\nName=GNU Image Manipulation Program\n")
	w.install("chafa")
	if code := w.run("--list", p); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	got := w.stdout.String()
	for _, want := range []string{
		"   1) Image Viewer [GUI]",
		"   2) GNU Image Manipulation Program [GUI]",
		"   3) chafa (terminal image)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("list lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "display-im7") {
		t.Fatalf("an ID with no entry was offered:\n%s", got)
	}
}
