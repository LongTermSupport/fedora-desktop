package release_test

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/release"
)

const olderEntry = "## 44.0.0 - 2026-10-05\n\n- First release for Fedora 44.\n\n"

func TestAFirstEntryMakesTheChangelog(t *testing.T) {
	got, err := release.Prepend("", "44.1.0", "2026-11-04", []string{"feat: a thing", "fix: a bug"})
	if err != nil {
		t.Fatal(err)
	}
	want := "# Changelog\n\n## 44.1.0 - 2026-11-04\n\n- feat: a thing\n- fix: a bug\n\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestANewEntryGoesFirstAndTheRestIsKeptByteForByte(t *testing.T) {
	existing := "# Changelog\n\n" + olderEntry
	got, err := release.Prepend(existing, "44.1.0", "2026-11-04", []string{"feat: a thing"})
	if err != nil {
		t.Fatal(err)
	}
	want := "# Changelog\n\n## 44.1.0 - 2026-11-04\n\n- feat: a thing\n\n" + olderEntry
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAReleaseIsNeverListedTwice(t *testing.T) {
	existing := "# Changelog\n\n## 44.1.0 - 2026-11-01\n\n- feat: earlier\n\n"
	if _, err := release.Prepend(existing, "44.1.0", "2026-11-04", []string{"feat: a"}); err == nil {
		t.Fatal("want an error for a version that already has an entry")
	}
}

func TestAnEntryNeedsWhatItListsAndAVersionAndADate(t *testing.T) {
	cases := map[string]struct {
		version, date string
		items         []string
	}{
		"no items":        {"44.1.0", "2026-11-04", nil},
		"a blank item":    {"44.1.0", "2026-11-04", []string{"  "}},
		"a bad version":   {"44.1", "2026-11-04", []string{"feat: a"}},
		"a bad date":      {"44.1.0", "4 Nov", []string{"feat: a"}},
		"a date with day": {"44.1.0", "2026-11-04T10:00", []string{"feat: a"}},
	}
	for name, c := range cases {
		if _, err := release.Prepend("", c.version, c.date, c.items); err == nil {
			t.Fatalf("%s: want an error", name)
		}
	}
}

// An item is one line of a list; a newline in it would start something else.
func TestAnItemIsOneLine(t *testing.T) {
	got, err := release.Prepend("", "44.1.0", "2026-11-04", []string{"feat: a\n## 99.0.0 - forged"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "\n## 99.0.0") {
		t.Fatalf("an item started a heading of its own: %q", got)
	}
}

func TestNotesAreTheEntryOfTheExactVersion(t *testing.T) {
	changelog := "# Changelog\n\n## 44.1.0 - 2026-11-04\n\n- feat: a thing\n- fix: a bug\n\n## 44.1.0-rc1 - 2026-11-01\n\n- rc\n\n" + olderEntry
	got, err := release.Notes(changelog, "44.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if got != "- feat: a thing\n- fix: a bug" {
		t.Fatalf("got %q", got)
	}
	older, err := release.Notes(changelog, "44.0.0")
	if err != nil || older != "- First release for Fedora 44." {
		t.Fatalf("got %q, %v", older, err)
	}
}

func TestNotesOfAMissingOrEmptyEntryAreAnError(t *testing.T) {
	if _, err := release.Notes("# Changelog\n\n"+olderEntry, "44.1.0"); err == nil {
		t.Fatal("want an error for a version with no entry")
	}
	if _, err := release.Notes("# Changelog\n\n## 44.1.0 - 2026-11-04\n\n## 44.0.0 - x\n\n- a\n", "44.1.0"); err == nil {
		t.Fatal("want an error for an entry with nothing in it")
	}
	if _, err := release.Notes("", "not-a-version"); err == nil {
		t.Fatal("want an error for a version that is not one")
	}
}
