package ftpcamera

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	return err == nil
}

// E17-E22: the sort pass files each kind, leaves fragments and the undated, sweeps empty dirs.
func TestSortPass(t *testing.T) {
	k := newKit(t)
	k.path("exiftool")
	k.realRoot()
	k.exifDates(map[string]string{
		"DSC1.JPG": "2026/10/14\n",
		"DSC1.ARW": "\n2026/10/14\n",
		"C1.MP4":   "2026/10/13",
	})
	strips := []string{"exiftool", "-s3", "-a", "-FileSize#"}
	k.fake.On(strips, execx.Result{Stdout: []byte("3\n")})
	k.fake.On([]string{"exiftool", "-validate"}, execx.Result{})
	k.put("DSC1.JPG", jpegBytes(), testStart)
	k.put("DSC1.ARW", []byte("raw"), testStart)
	k.put("C1.MP4", []byte("mp4"), testStart)
	k.put("DSC2.JPG", []byte{0xFF, 0xD8, 0x00}, testStart)  // truncated
	k.put("DSC3.JPG", jpegBytes(), testStart)               // no date
	k.put("notes.txt", []byte("x"), testStart)              // not media
	k.put("DCIM/100MSDCF/DSC9.JPG", jpegBytes(), testStart) // in a subdirectory
	k.wantCode(k.run("--sort"), term.ExitOK)

	for _, rel := range []string{"photos/2026/10/14/JPG/DSC1.JPG", "photos/2026/10/14/RAW/DSC1.ARW", "videos/2026/10/13/MP4/C1.MP4", "DSC2.JPG", "DSC3.JPG", "notes.txt", "DCIM/100MSDCF/DSC9.JPG"} {
		if !exists(t, filepath.Join(k.upload, rel)) {
			t.Errorf("%s is not where it belongs", rel)
		}
	}
	k.wantOut(
		"Sorting uploads by EXIF date...\n",
		"  move  C1.MP4  → videos/2026/10/13/MP4/\n",
		"  move  DSC1.ARW  → photos/2026/10/14/RAW/\n",
		"  skip  DSC3.JPG  (no EXIF date)\n",
		"1 file(s) were TRUNCATED in transfer and left in place.\n",
		"Sort complete: 3 moved, 1 skipped\n",
	)
	k.wantErr("  partial  DSC2.JPG  (truncated at 3 bytes — left in place)\n",
		"Detected: 1 file(s) arrived truncated during this session")
	k.wantRan(
		"exiftool -s3 -d %Y/%m/%d -DateTimeOriginal -CreateDate "+filepath.Join(k.upload, "DSC1.JPG"),
		"exiftool -s3 -d %Y/%m/%d -CreateDate -MediaCreateDate "+filepath.Join(k.upload, "C1.MP4"),
		"sudo -n mv "+filepath.Join(k.upload, "C1.MP4")+" "+filepath.Join(k.upload, "videos/2026/10/13/MP4")+"/",
		"sudo -n find "+k.upload+" -mindepth 1 -type d -empty -delete",
	)
	// Name order (B20): C1 before DSC1.
	if strings.Index(k.out.String(), "C1.MP4") > strings.Index(k.out.String(), "DSC1.ARW") {
		t.Error("not in name order")
	}
}

func TestSortPassPrerequisites(t *testing.T) {
	k := newKit(t)
	k.wantCode(k.run("--sort"), term.ExitFailure)
	k.wantErr("ERROR: exiftool not found — run play-photography.yml first\n")
	k.path("exiftool")
	if err := os.RemoveAll(k.upload); err != nil {
		t.Fatal(err)
	}
	k.wantCode(k.run("--sort"), term.ExitFailure)
	k.wantErr("ERROR: Upload directory not found: " + k.upload + "\n")
}

// E21, E22: a failed move or sweep ends the pass with an error.
func TestSortPassRootFailures(t *testing.T) {
	k := newKit(t)
	k.path("exiftool")
	k.exifDates(map[string]string{"A.JPG": "2026/01/02"})
	k.put("A.JPG", jpegBytes(), testStart)
	k.fake.On([]string{"sudo", "-n", "mkdir"}, execx.Result{Stderr: []byte("mkdir: denied\n"), ExitCode: 1})
	k.wantCode(k.run("--sort"), term.ExitFailure)
	k.wantErr("ERROR: could not move A.JPG: mkdir -p ")

	k2 := newKit(t)
	k2.path("exiftool")
	k2.fake.On([]string{"sudo", "-n", "find"}, execx.Result{Stderr: []byte("find: busy\n"), ExitCode: 1})
	k2.wantCode(k2.run("--sort"), term.ExitFailure)
	k2.wantErr("ERROR: could not remove the empty directories under " + k2.upload + ": find: busy\n")
}

// differentJPEG is a complete JPEG whose bytes differ from jpegBytes.
func differentJPEG() []byte { return []byte{0xFF, 0xD8, 0x07, 0xFF, 0xD9} }

// E21, B24: a sorted file of the same name is never replaced. A different file is refused for
// itself and the pass goes on, ending with exit 1; an identical one is a duplicate, reported
// and left in place, not an error.
func TestSortPassNameCollisions(t *testing.T) {
	k := newKit(t)
	k.path("exiftool")
	k.realRoot()
	k.exifDates(map[string]string{"DSC00001.JPG": "2026/01/02", "DSC00002.JPG": "2026/01/02", "DSC00003.JPG": "2026/01/02"})
	k.put("DSC00001.JPG", differentJPEG(), testStart)
	k.put("photos/2026/01/02/JPG/DSC00001.JPG", jpegBytes(), testStart)
	k.put("DSC00002.JPG", jpegBytes(), testStart)
	k.put("photos/2026/01/02/JPG/DSC00002.JPG", jpegBytes(), testStart)
	k.put("DSC00003.JPG", jpegBytes(), testStart)
	k.wantCode(k.run("--sort"), term.ExitFailure)
	k.wantErr(
		"  fail  DSC00001.JPG  (photos/2026/01/02/JPG/DSC00001.JPG is a different file of the same name — left in place)\n",
		"ERROR: 1 file(s) left in place: a different file of the same name is already in the library — rename the upload, then sort again\n",
	)
	k.wantOut(
		"  dup  DSC00002.JPG  (already in the library as photos/2026/01/02/JPG/DSC00002.JPG — left in place)\n",
		"  move  DSC00003.JPG  → photos/2026/01/02/JPG/\n",
		"Sort complete: 1 moved, 0 skipped, 1 duplicate(s), 1 refused\n",
	)
	k.wantRan("sudo -n find " + k.upload + " -mindepth 1 -type d -empty -delete")
	if data, err := os.ReadFile(filepath.Join(k.upload, "photos/2026/01/02/JPG/DSC00001.JPG")); err != nil || string(data) != string(jpegBytes()) {
		t.Errorf("the sorted file was touched: %q %v", data, err)
	}
	for _, n := range []string{"DSC00001.JPG", "DSC00002.JPG"} {
		if !exists(t, filepath.Join(k.upload, n)) {
			t.Errorf("%s was not left in place", n)
		}
	}
	if strings.Contains(k.err.String(), "DSC00002") {
		t.Errorf("a duplicate was reported as an error:\n%s", k.err.String())
	}
}

// E21: a pass whose only collisions are duplicates succeeds.
func TestSortPassDuplicatesOnly(t *testing.T) {
	k := newKit(t)
	k.path("exiftool")
	k.realRoot()
	k.exifDates(map[string]string{"DSC00002.JPG": "2026/01/02"})
	k.put("DSC00002.JPG", jpegBytes(), testStart)
	k.put("photos/2026/01/02/JPG/DSC00002.JPG", jpegBytes(), testStart)
	k.wantCode(k.run("--sort"), term.ExitOK)
	k.wantOut("Sort complete: 0 moved, 0 skipped, 1 duplicate(s)\n")
}

// E62, B24: in async mode a different file of the same name is a sort error and an identical
// one a duplicate; the session goes on.
func TestAsyncNameCollisions(t *testing.T) {
	k := serverKit(t)
	k.exifDates(map[string]string{"DSC00001.JPG": "2026/10/15", "DSC00002.JPG": "2026/10/15"})
	k.put("DSC00001.JPG", differentJPEG(), testStart)
	k.put("photos/2026/10/15/JPG/DSC00001.JPG", jpegBytes(), testStart)
	k.put("DSC00002.JPG", jpegBytes(), testStart)
	k.put("photos/2026/10/15/JPG/DSC00002.JPG", jpegBytes(), testStart)
	k.feedOn(false, tailArgv, okUpload("DSC00001.JPG", 6, "900"), okUpload("DSC00002.JPG", 6, "900"))
	k.wantCode(k.runCtx(interruptAt(k), "--async"), term.ExitOK)
	k.wantErr("  [09:30:00] fail  DSC00001.JPG  (sort error, left in place)\n")
	k.wantOut(
		"  [09:30:00] dup  DSC00002.JPG  (already in the library — left in place)\n",
		"Sorted: 0  Skipped: 1  Sort errors: 1\n",
	)
	k.wantNotRan("sudo -n mv")
}

// D5: sudo not ready is a failure after sudo's own explanation.
func TestSortPassSudoRefused(t *testing.T) {
	k := newKit(t)
	k.path("exiftool")
	k.exifDates(map[string]string{"A.JPG": "2026/01/02"})
	k.put("A.JPG", jpegBytes(), testStart)
	k.fake.On([]string{"sudo", "-n", "-v"}, execx.Result{ExitCode: 1})
	k.fake.On([]string{"sudo", "-n", "true"}, execx.Result{ExitCode: 1})
	k.fake.On([]string{"sudo", "-v"}, execx.Result{ExitCode: 1})
	k.wantCode(k.run("--sort"), term.ExitFailure)
	k.wantErr("sudo was not given your password")
}

func TestExifDateIgnoresExiftoolsStatusButNotItsAbsence(t *testing.T) {
	k := newKit(t)
	c := newCLI(k.app().Env)
	k.fake.On([]string{"exiftool"}, execx.Result{Stdout: []byte("  \n2026/05/06\n"), ExitCode: 1})
	if got := c.exifDate(t.Context(), "/x/a.arw", "photo"); got != "2026/05/06" {
		t.Errorf("got %q", got)
	}
	k.fake.OnError([]string{"exiftool"}, &execx.NotInstalledError{Name: "exiftool"})
	if got := c.exifDate(t.Context(), "/x/a.arw", "photo"); got != "" {
		t.Errorf("got %q", got)
	}
}

// E21: the duplicate check streams both files in chunks, whatever sizes the reads return, so
// a large video is never held in memory whole.
func TestSameReaders(t *testing.T) {
	long := strings.Repeat("0123456789", 20000)
	diffAtEnd := long[:len(long)-1] + "X"
	cases := []struct {
		name string
		a, b io.Reader
		want bool
	}{
		{"equal, uneven reads", iotest.OneByteReader(strings.NewReader(long)), iotest.HalfReader(strings.NewReader(long)), true},
		{"differ in the last byte", strings.NewReader(long), iotest.HalfReader(strings.NewReader(diffAtEnd)), false},
		{"one is a prefix", strings.NewReader(long), strings.NewReader(long[:100]), false},
		{"both empty", strings.NewReader(""), strings.NewReader(""), true},
	}
	for _, tc := range cases {
		got, err := sameReaders(tc.a, tc.b)
		if err != nil || got != tc.want {
			t.Errorf("%s: %v %v, want %v", tc.name, got, err, tc.want)
		}
	}
	if _, err := sameReaders(iotest.ErrReader(os.ErrPermission), strings.NewReader("x")); !errors.Is(err, os.ErrPermission) {
		t.Errorf("a read error: %v", err)
	}
}

// E21: files of different sizes are not identical; equal ones are.
func TestSameBytes(t *testing.T) {
	dir := t.TempDir()
	a, b, c := filepath.Join(dir, "a"), filepath.Join(dir, "b"), filepath.Join(dir, "c")
	writeFile(t, a, "same bytes")
	writeFile(t, b, "same bytes")
	writeFile(t, c, "same bytes, longer")
	if same, err := sameBytes(a, b); err != nil || !same {
		t.Errorf("a, b: %v %v", same, err)
	}
	if same, err := sameBytes(a, c); err != nil || same {
		t.Errorf("a, c: %v %v", same, err)
	}
	if _, err := sameBytes(a, filepath.Join(dir, "gone")); err == nil {
		t.Error("a missing file compared")
	}
}

func TestTypeAndClassDirs(t *testing.T) {
	for ext, want := range map[string][2]string{"arw": {"photos", "RAW"}, "Jpeg": {"photos", "JPEG"}, "mov": {"videos", "MOV"}} {
		class := mediaClass[strings.ToLower(ext)]
		if classDir(class) != want[0] || typeDir(ext) != want[1] {
			t.Errorf("%s: %s %s", ext, classDir(class), typeDir(ext))
		}
	}
}
