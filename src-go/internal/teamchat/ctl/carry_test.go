package ctl

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/admin"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/carry"
)

const (
	guestFile = "guest/coord.token"
	lostFile  = "restore-2-lost.jsonl"
)

// carried are the files carry.place hands the placer in its request, in its order.
func carried() []admin.PlacedFile {
	return []admin.PlacedFile{
		{Name: rosterName, Data: []byte("roster")},
		{Name: roomsName, Data: []byte("rooms")},
		{Name: tokenName, Data: []byte("carried-token")},
		{Name: guestFile, Data: []byte("guest-token")},
	}
}

// lostData is the lost export's content place streams.
const lostData = "{}\n"

func lostOf(name, data string) carry.LostExport {
	sum := sha256.Sum256([]byte(data))
	return carry.LostExport{Name: name, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
}

// place is carry.place with one lost export, streamed.
func (w *world) place(files []admin.PlacedFile) ([]string, error) {
	w.t.Helper()
	return w.placeStream(files, []carry.LostExport{lostOf(lostFile, lostData)}, lostData)
}

func (w *world) placeStream(files []admin.PlacedFile, lost []carry.LostExport, stream string) ([]string, error) {
	w.t.Helper()
	w.hidden = append(w.hidden, "carried-token", "guest-token")
	return w.cfg.Placer()(w.team, files, admin.NewLostStream(lost, strings.NewReader(stream)))
}

// TestPlaceWritesEachCarriedFile is SC-C2: each file with the atomic writer, 0600, the guest
// directory 0700, the lost export from the stream, and the names written answered in order.
func TestPlaceWritesEachCarriedFile(t *testing.T) {
	w := newWorld(t)
	names, err := w.place(carried())
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	if !slices.Equal(names, []string{rosterName, roomsName, tokenName, guestFile, lostFile}) {
		t.Fatalf("names %v", names)
	}
	for _, f := range append(carried(), admin.PlacedFile{Name: lostFile, Data: []byte(lostData)}) {
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

// TestPlaceVerifiesEveryLostExportBeforeAnything is condition 2 of the ruling (sc-D11): the
// child checks each lost export's size and sha256 again as it streams it into a temporary in
// ctl/<team>/, and places nothing, leaving no temporary, unless every one verifies and the
// stream ends where the sizes say.
func TestPlaceVerifiesEveryLostExportBeforeAnything(t *testing.T) {
	two := []carry.LostExport{lostOf(lostFile, lostData), lostOf("restore-3-lost.jsonl", "[]\n")}
	for name, c := range map[string]struct {
		lost   []carry.LostExport
		stream string
	}{
		"the second's digest": {two, lostData + "{}\n"},
		"the first's digest":  {two, "[]\n[]\n"},
		"short":               {two, lostData + "[]"},
		"long":                {two, lostData + "[]\nx"},
		"none named":          {nil, "x"},
		"oversize":            {[]carry.LostExport{{Name: lostFile, Size: carry.LostCap + 1, SHA256: strings.Repeat("0", 64)}}, ""},
		"a traversal":         {[]carry.LostExport{lostOf("../"+lostFile, lostData)}, lostData},
		"not a lost name":     {[]carry.LostExport{lostOf("other.jsonl", lostData)}, lostData},
		"a carried file's":    {[]carry.LostExport{lostOf(rosterName, lostData)}, lostData},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			if _, err := w.placeStream(carried(), c.lost, c.stream); err == nil {
				t.Fatal("placed")
			}
			if got := w.entries(); len(got) != 0 {
				t.Errorf("ctl/<team>/ = %v", got)
			}
		})
	}
	w := newWorld(t)
	names, err := w.placeStream(carried(), two, lostData+"[]\n")
	if err != nil || !slices.Equal(names[4:], []string{lostFile, "restore-3-lost.jsonl"}) || w.text("restore-3-lost.jsonl") != "[]\n" {
		t.Fatalf("two lost exports: %v %v", names, err)
	}
}

// TestPlaceWithoutGuestsMakesNoGuestDirectory: no guest token, no guest directory.
func TestPlaceWithoutGuestsMakesNoGuestDirectory(t *testing.T) {
	w := newWorld(t)
	files := slices.DeleteFunc(carried(), func(f admin.PlacedFile) bool { return f.Name == guestFile })
	if _, err := w.placeStream(files, nil, ""); err != nil {
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
