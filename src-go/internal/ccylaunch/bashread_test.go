package ccylaunch

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// bashName is ${fn%.*.token}: the shortest suffix of the form .<anything>.token removed.
func TestBashNameStripsTheShortestDottedSuffix(t *testing.T) {
	cases := map[string]string{
		"work.2030-06-01.token":     "work",
		"a.b.2030-01-01.token":      "a.b",
		"work.token":                "work.token",
		"x..token":                  "x",
		"work2030-06-01.token":      "work2030-06-01.token",
		"a.2030-01-01.x.token":      "a.2030-01-01",
		"no-suffix.2030-01-01.toke": "no-suffix.2030-01-01.toke",
	}
	for in, want := range cases {
		if got := bashName(in); got != want {
			t.Errorf("bashName(%q) = %q, want %q", in, got, want)
		}
	}
}

// T1's departures: a name the bash reads as dated and the tokens package does not, or the
// other way round, is one the front hands to the bash.
func TestReadsAlike(t *testing.T) {
	alike := []string{"work.2030-06-01.token", "a.b.2030-01-01.token", "work.token", "a.2030-01-01.x.token"}
	differ := []string{"work2030-06-01.token", "work.2030-13-45.token", "x.2030-02-30.token"}
	for _, name := range alike {
		if !readsAlike(name) {
			t.Errorf("readsAlike(%q) = false, want true", name)
		}
	}
	for _, name := range differ {
		if readsAlike(name) {
			t.Errorf("readsAlike(%q) = true, want false", name)
		}
	}
}

func envOf(kv ...string) func(string) string {
	return Front{Environ: kv}.getenv
}

func TestCollationIsBytesOnlyInTheCLocale(t *testing.T) {
	bytesOrder := [][]string{
		nil,
		{"LANG=C.UTF-8"},
		{"LANG=C"},
		{"LANG=POSIX"},
		{"LANG=en_GB.UTF-8", "LC_ALL=C.utf8"},
		{"LANG=en_GB.UTF-8", "LC_COLLATE=C"},
		{"LC_ALL=", "LANG=C.UTF-8"},
	}
	for _, env := range bytesOrder {
		if !collationIsBytes(envOf(env...)) {
			t.Errorf("collationIsBytes(%q) = false, want true", env)
		}
	}
	other := [][]string{{"LANG=en_GB.UTF-8"}, {"LANG=C.UTF-8", "LC_COLLATE=en_US.UTF-8"}, {"LC_ALL=de_DE.UTF-8", "LANG=C"}}
	for _, env := range other {
		if collationIsBytes(envOf(env...)) {
			t.Errorf("collationIsBytes(%q) = true, want false", env)
		}
	}
}

// Under another collation only lower-case letters and a strict date sort as bytes do.
func TestOrderAlike(t *testing.T) {
	if !orderAlike([]string{"Work.x.token", "a-b.token"}, true) {
		t.Error("byte order refused names")
	}
	if !orderAlike([]string{"play.2030-12-31.token", "work.2030-06-01.token"}, false) {
		t.Error("plain names refused under a locale")
	}
	for _, name := range []string{"Work.2030-06-01.token", "work2.2030-06-01.token", "a-b.2030-06-01.token", "work.token"} {
		if orderAlike([]string{name}, false) {
			t.Errorf("orderAlike(%q) under a locale = true, want false", name)
		}
	}
}

func TestZoneReadsAlike(t *testing.T) {
	alike := [][]string{nil, {"TZ=UTC"}, {"TZ="}, {"TZ=Europe/London"}, {"TZ=:Europe/London"}, {"TZ=/usr/share/zoneinfo/Europe/London"}}
	for _, env := range alike {
		if !zoneReadsAlike(envOf(env...)) {
			t.Errorf("zoneReadsAlike(%q) = false, want true", env)
		}
	}
	differ := [][]string{{"TZ=UTC0"}, {"TZ=Nowhere/Atall"}, {"TZ=/nonexistent/zone"}, {"TZDIR=/x"}, {"ZONEINFO=/x"}}
	for _, env := range differ {
		if zoneReadsAlike(envOf(env...)) {
			t.Errorf("zoneReadsAlike(%q) = true, want false", env)
		}
	}
}

// The bash counts expiry days in seconds between two local midnights, truncated; a day on
// which the clocks change can move its colour. Only such a day differs.
func TestColourAlike(t *testing.T) {
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	utcNow := time.Date(2030, 1, 15, 12, 0, 0, 0, time.UTC)
	for _, date := range []string{"2030-01-14", "2030-01-15", "2030-01-16", "2030-01-29", "2030-01-30", "2031-01-01"} {
		if !colourAlike(date, utcNow) {
			t.Errorf("colourAlike(%s) in UTC = false", date)
		}
	}
	// London's clocks go forward on the last Sunday of March, so that day is 23 hours long:
	// the bash counts the next day as 0 days away.
	if colourAlike("2030-04-01", time.Date(2030, 3, 31, 12, 0, 0, 0, london)) {
		t.Error("the day the clocks change reads alike")
	}
	if !colourAlike("2030-04-30", time.Date(2030, 3, 31, 12, 0, 0, 0, london)) {
		t.Error("a date well past the change does not read alike")
	}
}

func TestTokenGlob(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	names, exists, why := tokenGlob(missing, isTokenFile)
	if exists || why != "" || names != nil {
		t.Fatalf("a missing directory: %q %v %q", names, exists, why)
	}
	for _, name := range []string{"work.2030-06-01.token", "a.token", ".hidden.token", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	names, exists, why = tokenGlob(dir, isTokenFile)
	if !exists || why != "" || !slices.Equal(names, []string{"a.token", "work.2030-06-01.token"}) {
		t.Fatalf("tokenGlob = %q %v %q", names, exists, why)
	}
	if err := os.Mkdir(filepath.Join(dir, "d.token"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, why = tokenGlob(dir, isTokenFile); why == "" {
		t.Fatal("a directory named like a token file was not refused")
	}
	if _, _, why = tokenGlob(filepath.Join(dir, "notes.txt"), isTokenFile); why == "" {
		t.Fatal("a token directory that is a file was not refused")
	}
}
