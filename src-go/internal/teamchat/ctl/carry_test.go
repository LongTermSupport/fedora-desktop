package ctl

import (
	"os"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/admin"
)

const (
	guestFile = "guest/coord.token"
	lostFile  = "restore-2-lost.jsonl"
)

// carried are the files carry.place hands the placer, in its order.
func carried() []admin.PlacedFile {
	return []admin.PlacedFile{
		{Name: rosterName, Data: []byte("roster")},
		{Name: roomsName, Data: []byte("rooms")},
		{Name: tokenName, Data: []byte("carried-token")},
		{Name: guestFile, Data: []byte("guest-token")},
		{Name: lostFile, Data: []byte("{}\n")},
	}
}

func (w *world) place(files []admin.PlacedFile) ([]string, error) {
	w.t.Helper()
	w.hidden = append(w.hidden, "carried-token", "guest-token")
	return w.cfg.Placer()(w.team, files)
}

// TestPlaceWritesEachCarriedFile is SC-C2: each file with the atomic writer, 0600, the guest
// directory 0700, and the names written answered in order.
func TestPlaceWritesEachCarriedFile(t *testing.T) {
	w := newWorld(t)
	names, err := w.place(carried())
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	if !slices.Equal(names, []string{rosterName, roomsName, tokenName, guestFile, lostFile}) {
		t.Fatalf("names %v", names)
	}
	for _, f := range carried() {
		if got := w.text(f.Name); got != string(f.Data) {
			t.Errorf("%s = %q", f.Name, got)
		}
		if got := w.mode(f.Name); got != 0o600 {
			t.Errorf("%s mode %v", f.Name, got)
		}
	}
	if got := w.mode("guest"); got != os.ModeDir|0o700 {
		t.Errorf("guest mode %v", got)
	}
	if got := w.entries(); !slices.Equal(got, []string{tokenName, "guest", lostFile, roomsName, rosterName}) {
		t.Errorf("ctl/<team>/ = %v", got)
	}
	o := w.mustOpen("")
	if o.RosterPath != w.path(rosterName) {
		t.Errorf("the placed team does not open: %+v", o)
	}
}

// TestPlaceRefusesWhenAnyTargetIsThere is SC-C2's refusal: one target already there and
// nothing is written.
func TestPlaceRefusesWhenAnyTargetIsThere(t *testing.T) {
	for _, there := range []string{rosterName, tokenName, lostFile, "guest"} {
		t.Run(there, func(t *testing.T) {
			w := newWorld(t)
			if there == "guest" {
				w.mkdir(w.path("guest"), 0o700)
			} else {
				w.write(there, "old")
			}
			before := w.entries()
			if _, err := w.place(carried()); err == nil {
				t.Fatal("a place over an existing target was done")
			}
			if got := w.entries(); !slices.Equal(got, before) {
				t.Errorf("ctl/<team>/ = %v, was %v", got, before)
			}
		})
	}
}

// TestPlaceWithoutGuestsMakesNoGuestDirectory: no guest token, no guest directory.
func TestPlaceWithoutGuestsMakesNoGuestDirectory(t *testing.T) {
	w := newWorld(t)
	files := slices.DeleteFunc(carried(), func(f admin.PlacedFile) bool { return f.Name == guestFile || f.Name == lostFile })
	if _, err := w.place(files); err != nil {
		t.Fatal(err)
	}
	if got := w.entries(); !slices.Equal(got, []string{tokenName, roomsName, rosterName}) {
		t.Errorf("ctl/<team>/ = %v", got)
	}
}

// TestPlaceRefusesANameThatIsNotACtlFile: only a plain name or guest/<name>, each once.
func TestPlaceRefusesANameThatIsNotACtlFile(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../x", "/x", "guest/", "guest/a/b", "other/x", "guest/.."} {
		w := newWorld(t)
		if _, err := w.place([]admin.PlacedFile{{Name: name, Data: []byte("x")}}); err == nil {
			t.Errorf("%q was placed", name)
		}
		if got := w.entries(); len(got) != 0 {
			t.Errorf("%q: ctl/<team>/ = %v", name, got)
		}
	}
	w := newWorld(t)
	if _, err := w.place([]admin.PlacedFile{{Name: rosterName, Data: []byte("a")}, {Name: rosterName, Data: []byte("b")}}); err == nil {
		t.Error("one name was placed twice")
	}
}

// TestPlaceSweepsAStaleTemporaryAndNeedsItsDirectory: a temporary a killed place left is
// removed; a directory not the child's by OP1 is refused.
func TestPlaceSweepsAStaleTemporaryAndNeedsItsDirectory(t *testing.T) {
	w := newWorld(t)
	w.write("."+rosterName+".0011.tmp", "partial")
	if _, err := w.place(carried()); err != nil {
		t.Fatal(err)
	}
	if w.has("." + rosterName + ".0011.tmp") {
		t.Error("the stale temporary is still there")
	}
	w = newWorld(t)
	w.mkdir(w.dir, 0o755)
	if _, err := w.place(carried()); err == nil {
		t.Error("a directory of mode 0755 was written into")
	}
}
