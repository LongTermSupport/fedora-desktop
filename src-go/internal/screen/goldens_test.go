package screen

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// capturesNamed is a testdata tree holding an empty file of each name given.
func capturesNamed(names ...string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for _, n := range names {
		fsys["testdata/"+n] = &fstest.MapFile{}
	}
	return fsys
}

func TestVersionsCompareNumericallyPerComponent(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2.1.331", "2.1.40", 1},
		{"2.1.40", "2.1.331", -1},
		{"2.1.331", "2.1.331", 0},
		{"10.0.0", "9.99.99", 1},
		{"2.1", "2.1.0", -1},
		{"2.1.0.1", "2.1.0", 1},
	}
	for _, tc := range cases {
		a, err := ParseVersion(tc.a)
		if err != nil {
			t.Fatalf("ParseVersion(%q): %v", tc.a, err)
		}
		b, err := ParseVersion(tc.b)
		if err != nil {
			t.Fatalf("ParseVersion(%q): %v", tc.b, err)
		}
		if got := a.Compare(b); got != tc.want {
			t.Errorf("%s vs %s: %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestAnUnparseableVersionIsAnError(t *testing.T) {
	for _, s := range []string{"", "2", "2.1.x", "v2.1.3", "2.1.3 (Claude Code)", "2..1", "2.1.", "99999999999999999999.1"} {
		if v, err := ParseVersion(s); err == nil {
			t.Errorf("ParseVersion(%q) = %v, want an error", s, v)
		}
	}
}

func TestNewestCapturedIsTheHighestVersionNamed(t *testing.T) {
	fsys := capturesNamed("idle-cc-2.1.40.txt", "mid-turn-cc-2.1.331.txt", "idle-cc-2.1.99.txt", "picker-60.txt", "idle-new-layout.txt")
	v, err := NewestCaptured(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if v.String() != "2.1.331" {
		t.Fatalf("newest %s, want 2.1.331", v)
	}
}

func TestNoVersionedCaptureIsAnError(t *testing.T) {
	for name, fsys := range map[string]fstest.MapFS{
		"none named for a version": capturesNamed("picker-60.txt", "idle-new-layout.txt"),
		"no testdata at all":       {},
	} {
		if v, err := NewestCaptured(fsys); err == nil {
			t.Errorf("%s: newest %s, want an error", name, v)
		}
	}
}

func TestACaptureNamedForABadVersionIsAnError(t *testing.T) {
	_, err := NewestCaptured(capturesNamed("idle-cc-2.1.331.txt", "idle-cc-2.x.txt"))
	if err == nil || !strings.Contains(err.Error(), "idle-cc-2.x.txt") {
		t.Fatalf("err %v, want one naming idle-cc-2.x.txt", err)
	}
}

func TestDriftIsReportedOnlyWhenTheInstalledVersionIsNewer(t *testing.T) {
	fsys := capturesNamed("idle-cc-2.1.331.txt", "mid-turn-cc-2.1.331.txt")
	for _, installed := range []string{"2.1.331", "2.1.40", "1.9.999"} {
		notice, err := layoutDrift(fsys, installed)
		if err != nil || notice != "" {
			t.Errorf("installed %s: notice %q, err %v, want silence", installed, notice, err)
		}
	}
	notice, err := layoutDrift(fsys, "2.1.400")
	if err != nil {
		t.Fatal(err)
	}
	for _, words := range []string{"2.1.400", "2.1.331", "tmux capture-pane -p -e", "mid-turn", "idle", "redact", "escape", "mid-turn-cc-2.1.400.txt", "idle-cc-2.1.400.txt", "src-go/internal/screen/testdata"} {
		if !strings.Contains(notice, words) {
			t.Errorf("notice %q does not say %q", notice, words)
		}
	}
}

func TestDriftOnAnUnreadableInputIsAnError(t *testing.T) {
	if _, err := layoutDrift(capturesNamed("idle-cc-2.1.331.txt"), "2.1.x"); err == nil {
		t.Error("an unparseable installed version was not an error")
	}
	if _, err := layoutDrift(capturesNamed("picker-60.txt"), "2.1.400"); err == nil {
		t.Error("a testdata tree with no versioned capture was not an error")
	}
}

// TestTheBuiltInCapturesAreTheTestdataOnes keeps the captures LayoutDrift compares with (built
// into the binary) the same set as testdata holds.
func TestTheBuiltInCapturesAreTheTestdataOnes(t *testing.T) {
	onDisk, err := filepath.Glob(filepath.Join("testdata", "*-cc-*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(onDisk) == 0 {
		t.Fatal("testdata holds no capture named for a Claude Code version")
	}
	newest, err := NewestCaptured(os.DirFS("."))
	if err != nil {
		t.Fatal(err)
	}
	builtIn, err := NewestCaptured(captures)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(newest, builtIn) {
		t.Fatalf("built in %s, testdata %s", builtIn, newest)
	}
	if notice, err := LayoutDrift(newest.String()); err != nil || notice != "" {
		t.Fatalf("the newest capture's own version: notice %q, err %v", notice, err)
	}
}
