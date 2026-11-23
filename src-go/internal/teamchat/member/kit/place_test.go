package kit

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// tempKit is a kit path in a fresh directory whose kit directory does not exist yet.
func tempKit(t *testing.T) paths.Kit {
	t.Helper()
	k, err := paths.KitAt(filepath.Join(t.TempDir(), "digest"))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// requireWhole fails unless every part of the kit is at k, of its kind.
func requireWhole(t *testing.T, k paths.Kit) {
	t.Helper()
	if missing := Missing(k); len(missing) > 0 {
		t.Fatalf("the kit at %s lacks %v", k, missing)
	}
}

func requireAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("%s exists (%v)", path, err)
	}
}

// Parts names every file and directory of the kit, the plugin's manifest among them.
func TestPartsNamesEveryEntryOfTheKit(t *testing.T) {
	k := hostKit(t)
	parts := Parts(k)
	wanted := []string{
		"bin/teamchat", "settings.json", "plugin/teamchat/.claude-plugin/plugin.json",
		"plugin/teamchat/hooks/hooks.json", "plugin/teamchat/skills/teamchat/SKILL.md",
	}
	for _, want := range wanted {
		if !slices.Contains(parts, want) {
			t.Errorf("parts %v lack %s", parts, want)
		}
	}
	if len(parts) != len(entries(k, strings.NewReader(""))) {
		t.Errorf("parts %v are not every entry", parts)
	}
}

// A kit is assembled beside its directory and renamed into place whole: no staging is left.
func TestPlaceAssemblesTheWholeKitAndLeavesNoStaging(t *testing.T) {
	k := tempKit(t)
	placed, err := Place(k, strings.NewReader("bin"))
	if err != nil || !placed {
		t.Fatalf("placed %v, %v", placed, err)
	}
	requireWhole(t, k)
	requireAbsent(t, Staging(k))
}

// A whole kit already in place is left as it is, and said so.
func TestPlaceLeavesAWholeKitAlone(t *testing.T) {
	k := tempKit(t)
	if _, err := Place(k, strings.NewReader("first")); err != nil {
		t.Fatal(err)
	}
	placed, err := Place(k, strings.NewReader("first"))
	if err != nil || placed {
		t.Fatalf("placed %v, %v; want a whole kit left alone", placed, err)
	}
	if data, err := os.ReadFile(k.Binary()); err != nil || string(data) != "first" {
		t.Errorf("the kit's binary is %q (%v), want the first kit's", data, err)
	}
}

// A kit whose parts are all there but whose binary is not the running program's bytes (the
// same source built by another Go toolchain) is not whole: it is assembled again.
func TestPlaceReplacesAKitWhoseBinaryDiffers(t *testing.T) {
	k := tempKit(t)
	if _, err := Place(k, strings.NewReader("first")); err != nil {
		t.Fatal(err)
	}
	placed, err := Place(k, strings.NewReader("second"))
	if err != nil || !placed {
		t.Fatalf("placed %v, %v; want a kit with another binary assembled again", placed, err)
	}
	if data, err := os.ReadFile(k.Binary()); err != nil || string(data) != "second" {
		t.Errorf("the kit's binary is %q (%v), want the running program's", data, err)
	}
	requireAbsent(t, Staging(k))
}

// A kit whose document differs from what this build writes (edited by hand) is assembled again.
func TestPlaceReplacesAKitWhoseDocumentDiffers(t *testing.T) {
	k := tempKit(t)
	if _, err := Place(k, strings.NewReader("bin")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(k.String(), "settings.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if placed, err := Place(k, strings.NewReader("bin")); err != nil || !placed {
		t.Fatalf("placed %v, %v; want a kit with another document assembled again", placed, err)
	}
}

// Failure point: the assembly fails part way. Nothing is at the kit's path, so the next run
// assembles it again and is not skipped; the staging it left is cleared by that run.
func TestPlaceAfterAFailedAssemblyLeavesNoKitAndTheNextRunRepairs(t *testing.T) {
	k := tempKit(t)
	if placed, err := Place(k, failReader{}); err == nil || placed {
		t.Fatalf("a failed assembly placed %v, %v", placed, err)
	}
	requireAbsent(t, k.String())
	placed, err := Place(k, strings.NewReader("bin"))
	if err != nil || !placed {
		t.Fatalf("the next run placed %v, %v", placed, err)
	}
	requireWhole(t, k)
	requireAbsent(t, Staging(k))
}

// Failure point: the rename into place fails. The kit's path stays empty and the next run
// clears the whole staging it left and places the kit.
func TestPlaceAfterAFailedRenameLeavesNoKitAndTheNextRunRepairs(t *testing.T) {
	k := tempKit(t)
	was := rename
	t.Cleanup(func() { rename = was })
	rename = func(string, string) error { return errors.New("rename refused") }
	if placed, err := Place(k, strings.NewReader("bin")); err == nil || placed {
		t.Fatalf("a failed rename placed %v, %v", placed, err)
	}
	requireAbsent(t, k.String())
	rename = was
	if placed, err := Place(k, strings.NewReader("bin")); err != nil || !placed {
		t.Fatalf("the next run placed %v, %v", placed, err)
	}
	requireWhole(t, k)
	requireAbsent(t, Staging(k))
}

// Failure point: a staging directory left by a run that was killed, with anything in it, is
// cleared by the next run, never by hand.
func TestPlaceClearsALeftoverStaging(t *testing.T) {
	k := tempKit(t)
	junk := filepath.Join(Staging(k), "bin")
	if err := os.MkdirAll(junk, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(junk, "teamchat"), []byte("half"), 0o600); err != nil {
		t.Fatal(err)
	}
	if placed, err := Place(k, strings.NewReader("bin")); err != nil || !placed {
		t.Fatalf("placed %v, %v", placed, err)
	}
	requireWhole(t, k)
	requireAbsent(t, Staging(k))
}

// Failure point: a kit directory that is not whole (an assembly made in place before the kit
// was placed atomically, or a part removed) is never taken as done: it is replaced whole.
func TestPlaceReplacesAKitThatIsNotWhole(t *testing.T) {
	for _, half := range []string{"", "settings.json", "plugin/teamchat/.claude-plugin/plugin.json"} {
		k := tempKit(t)
		if _, err := Place(k, strings.NewReader("old")); err != nil {
			t.Fatal(err)
		}
		cut := k.String()
		if half != "" {
			cut = filepath.Join(k.String(), half)
		}
		if err := os.RemoveAll(cut); err != nil {
			t.Fatal(err)
		}
		if half == "" {
			if err := os.Mkdir(k.String(), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if missing := Missing(k); len(missing) == 0 {
			t.Fatalf("%q: a kit with a part cut is reported whole", half)
		}
		placed, err := Place(k, strings.NewReader("new"))
		if err != nil || !placed {
			t.Fatalf("%q: placed %v, %v", half, placed, err)
		}
		requireWhole(t, k)
		if data, err := os.ReadFile(k.Binary()); err != nil || string(data) != "new" {
			t.Errorf("%q: the binary is %q (%v), want the new kit's", half, data, err)
		}
	}
}

// A part of the wrong kind (a link, or a file where a directory belongs) is not whole.
func TestMissingNamesAPartOfTheWrongKind(t *testing.T) {
	k := tempKit(t)
	if _, err := Place(k, strings.NewReader("bin")); err != nil {
		t.Fatal(err)
	}
	settings := k.Settings()
	if err := os.Remove(settings); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/null", settings); err != nil {
		t.Fatal(err)
	}
	if missing := Missing(k); !slices.Equal(missing, []string{"settings.json"}) {
		t.Errorf("missing %v, want the linked settings", missing)
	}
}
