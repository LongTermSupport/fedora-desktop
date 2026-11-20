package watchlist_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/watchlist"
)

const (
	shaA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	shaB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	shaC = "cccccccccccccccccccccccccccccccccccccccc"
	lsha = "0123456789abcdef"
)

func row(fields ...string) string { return strings.Join(fields, "\t") + "\n" }

// studyRow is a valid study row of repo in group, with an MIT LICENSE.
func studyRow(repo, group string) string {
	return row(repo, "star", "MIT", "lift", "study", group, "LICENSE", lsha)
}

// E1-E9
func TestParseReadsEveryColumn(t *testing.T) {
	data := "# header\n\n" + studyRow("example-org/alpha", "fleets") +
		row("example-org/beta", "added", "NOASSERTION", "clean-room", "new", "-", "-", "-")
	got, err := watchlist.Parse([]byte(data), "list.tsv")
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v, %v", got, err)
	}
	want := watchlist.Entry{
		Repo: "example-org/alpha", Source: "star", Licence: "MIT", Reuse: "lift", State: "study",
		Group: "fleets", LicenceFile: "LICENSE", LicenceSHA: lsha,
	}
	if got[0] != want {
		t.Fatalf("got %+v", got[0])
	}
}

// E5
func TestReuseIsDerivedFromTheLicence(t *testing.T) {
	for licence, want := range map[string]string{
		"MIT": "lift", "Apache-2.0": "lift", "ISC": "lift", "0BSD": "lift", "BSD-2-Clause": "lift",
		"BSD-3-Clause": "lift", "MPL-2.0": "whole-file", "GPL-3.0": "clean-room", "AGPL-3.0": "clean-room",
		"NONE": "clean-room", "NOASSERTION": "clean-room", "LicenseRef-Multica": "clean-room",
		"BUSL-1.1": "clean-room", "mit": "clean-room",
	} {
		if got := watchlist.Reuse(licence); got != want {
			t.Errorf("%s: %s, want %s", licence, got, want)
		}
	}
}

// E1-E9: every malformed list is an error that names the file and line.
func TestParseRejectsMalformedRows(t *testing.T) {
	cases := map[string]string{
		"seven fields":        row("example-org/a", "star", "MIT", "lift", "study", "fleets", "LICENSE"),
		"nine fields":         studyRow("example-org/a", "fleets")[:len(studyRow("example-org/a", "fleets"))-1] + "\tx\n",
		"spaces not tab":      "example-org/a star MIT lift study fleets LICENSE " + lsha + "\n",
		"E1 url":              studyRow("https://github.com/example-org/a", "fleets"),
		"E1 no owner":         studyRow("alpha", "fleets"),
		"E1 deep":             studyRow("example-org/a/b", "fleets"),
		"E2 source":           row("example-org/a", "fork", "MIT", "lift", "study", "g", "LICENSE", lsha),
		"E3 licence space":    row("example-org/a", "star", "M IT", "clean-room", "study", "g", "LICENSE", lsha),
		"E3 licence slash":    row("example-org/a", "star", "a/b", "clean-room", "study", "g", "LICENSE", lsha),
		"E3 licence empty":    row("example-org/a", "star", "", "clean-room", "study", "g", "LICENSE", lsha),
		"E4 reuse":            row("example-org/a", "star", "MIT", "free", "study", "g", "LICENSE", lsha),
		"E5 inconsistent":     row("example-org/a", "star", "GPL-3.0", "lift", "study", "g", "LICENSE", lsha),
		"E6 state":            row("example-org/a", "star", "MIT", "lift", "done", "g", "LICENSE", lsha),
		"E7 study no group":   row("example-org/a", "star", "MIT", "lift", "study", "-", "LICENSE", lsha),
		"E7 group case":       row("example-org/a", "star", "MIT", "lift", "study", "Fleets", "LICENSE", lsha),
		"E7 group space":      row("example-org/a", "star", "MIT", "lift", "study", "two words", "LICENSE", lsha),
		"E8 not a licence":    row("example-org/a", "star", "MIT", "lift", "study", "g", "README", lsha),
		"E8 a path":           row("example-org/a", "star", "MIT", "lift", "study", "g", "doc/LICENSE", lsha),
		"E8 file without sha": row("example-org/a", "star", "MIT", "lift", "study", "g", "LICENSE", "-"),
		"E9 sha without file": row("example-org/a", "star", "MIT", "lift", "study", "g", "-", lsha),
		"E9 short sha":        row("example-org/a", "star", "MIT", "lift", "study", "g", "LICENSE", "0123"),
		"E9 upper sha":        row("example-org/a", "star", "MIT", "lift", "study", "g", "LICENSE", strings.ToUpper(lsha)),
		"duplicate":           studyRow("example-org/a", "g") + studyRow("Example-Org/A", "g"),
	}
	for name, data := range cases {
		_, err := watchlist.Parse([]byte(data), "list.tsv")
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if !strings.Contains(err.Error(), "list.tsv:") {
			t.Errorf("%s: the error does not name the line: %v", name, err)
		}
	}
}

func TestParseErrorNamesTheLine(t *testing.T) {
	data := "# h\n" + studyRow("example-org/a", "g") + "broken\n"
	_, err := watchlist.Parse([]byte(data), "list.tsv")
	if err == nil || !strings.Contains(err.Error(), "list.tsv:3") {
		t.Fatalf("got %v", err)
	}
}

// E7: a group may stay on a row that is not in study.
func TestParseAcceptsAGroupOnARowNotInStudy(t *testing.T) {
	data := row("example-org/a", "star", "MIT", "lift", "unstarred", "fleets", "LICENSE", lsha)
	if _, err := watchlist.Parse([]byte(data), "list.tsv"); err != nil {
		t.Fatal(err)
	}
}

// The list's text: the documenting header, then the rows sorted by repo.
func TestFormatSortsAndRoundTrips(t *testing.T) {
	entries := []watchlist.Entry{
		{Repo: "example-org/beta", Source: "added", Licence: "GPL-3.0", Reuse: "clean-room", State: "new", Group: "-", LicenceFile: "-", LicenceSHA: "-"},
		{Repo: "example-org/alpha", Source: "star", Licence: "MIT", Reuse: "lift", State: "study", Group: "g", LicenceFile: "LICENSE", LicenceSHA: lsha},
	}
	out := string(watchlist.Format(entries))
	if !strings.HasPrefix(out, "#") || !strings.Contains(out, "licence_sha") {
		t.Fatalf("no documenting header: %q", out)
	}
	if strings.Index(out, "example-org/alpha") > strings.Index(out, "example-org/beta") {
		t.Fatalf("not sorted: %q", out)
	}
	back, err := watchlist.Parse([]byte(out), "list.tsv")
	if err != nil || len(back) != 2 || back[0] != entries[1] || back[1] != entries[0] {
		t.Fatalf("round trip: %v, %v", back, err)
	}
	if entries[0].Repo != "example-org/beta" {
		t.Fatal("Format reordered its argument")
	}
}

// Paths: study, new and unstarred under starred-repos, excluded under starred-repos-excluded.
func TestCloneDirFollowsLicenceAndState(t *testing.T) {
	for state, want := range map[string]string{
		"study":     "/r/untracked/starred-repos/MIT/example-org/alpha",
		"new":       "/r/untracked/starred-repos/MIT/example-org/alpha",
		"unstarred": "/r/untracked/starred-repos/MIT/example-org/alpha",
		"excluded":  "/r/untracked/starred-repos-excluded/MIT/example-org/alpha",
	} {
		e := watchlist.Entry{Repo: "example-org/alpha", Licence: "MIT", State: state}
		if got := e.CloneDir("/r"); got != want {
			t.Errorf("%s: %s", state, got)
		}
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// E8, E9: the first root file by name matching licence or copying, any case; its sha256's first
// 16 hex digits.
func TestLicenceOfPicksTheFirstMatchingRootFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "README.md"), "x")
	writeFile(t, filepath.Join(dir, "LICENSES", "MIT.txt"), "a directory is not the licence")
	writeFile(t, filepath.Join(dir, "LICENSE.md"), "md")
	writeFile(t, filepath.Join(dir, "COPYING"), "gpl")
	file, sha, err := watchlist.LicenceOf(dir)
	if err != nil {
		t.Fatal(err)
	}
	if file != "COPYING" || sha != "78633c5ba953d299" {
		t.Fatalf("sha %q", sha)
	}
	only := t.TempDir()
	writeFile(t, filepath.Join(only, "licence.txt"), "x")
	if file, _, err := watchlist.LicenceOf(only); err != nil || file != "licence.txt" {
		t.Fatalf("got %q, %v", file, err)
	}
}

func TestLicenceOfWithNoLicenceIsDashes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "README.md"), "x")
	file, sha, err := watchlist.LicenceOf(dir)
	if err != nil || file != "-" || sha != "-" {
		t.Fatalf("got %q %q %v", file, sha, err)
	}
	if _, _, err := watchlist.LicenceOf(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("a missing clone was read as no licence")
	}
}

// The list this repository ships is valid in the new format.
func TestTheShippedListLoads(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(watchlist.ListPath(root))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := watchlist.Parse(data, "Watchlist.tsv")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("the shipped list is empty")
	}
	if string(watchlist.Format(entries)) != string(data) {
		t.Fatal("the shipped list is not in the form Format writes")
	}
}
