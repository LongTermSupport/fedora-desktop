package ccylaunch

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
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
