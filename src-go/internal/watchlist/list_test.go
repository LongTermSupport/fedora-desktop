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
)

func row(fields ...string) string { return strings.Join(fields, "\t") + "\n" }

// W4
func TestParseReadsRowsAndSkipsCommentsAndBlanks(t *testing.T) {
	data := "# header\n\n" + row("https://github.com/example-org/alpha", "main", shaA, "run-1") +
		row("https://github.com/example-org/beta", "trunk", "-", "-")
	got, err := watchlist.Parse([]byte(data), "list.tsv")
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v, %v", got, err)
	}
	want := watchlist.Entry{URL: "https://github.com/example-org/alpha", Branch: "main", LastSHA: shaA, Run: "run-1"}
	if got[0] != want {
		t.Fatalf("got %+v", got[0])
	}
	if got[1].LastSHA != "" || got[1].Run != "" || got[1].Branch != "trunk" {
		t.Fatalf("an unreviewed row should hold no sha or run: %+v", got[1])
	}
}

// W4, W5, W6, W7, W8: every malformed list is an error that names the line.
func TestParseRejectsMalformedRows(t *testing.T) {
	cases := map[string]string{
		"three fields":   row("https://github.com/example-org/a", "main", "-"),
		"five fields":    row("https://github.com/example-org/a", "main", "-", "-", "x"),
		"spaces not tab": "https://github.com/example-org/a main - -\n",
		"http":           row("http://github.com/example-org/a", "main", "-", "-"),
		"ssh":            row("git@github.com:example-org/a.git", "main", "-", "-"),
		"dot git":        row("https://github.com/example-org/a.git", "main", "-", "-"),
		"trailing slash": row("https://github.com/example-org/a/", "main", "-", "-"),
		"port":           row("https://github.com:443/example-org/a", "main", "-", "-"),
		"fragment":       row("https://github.com/example-org/a#x", "main", "-", "-"),
		"other host":     row("https://gitlab.com/example-org/a", "main", "-", "-"),
		"query":          row("https://github.com/example-org/a?x=1", "main", "-", "-"),
		"userinfo":       row("https://name@example.com/example-org/a", "main", "-", "-"),
		"no repo":        row("https://github.com/example-org", "main", "-", "-"),
		"deep path":      row("https://github.com/example-org/a/b", "main", "-", "-"),
		"empty branch":   row("https://github.com/example-org/a", "", "-", "-"),
		"branch space":   row("https://github.com/example-org/a", "my branch", "-", "-"),
		"short sha":      row("https://github.com/example-org/a", "main", "abc123", "run"),
		"upper sha":      row("https://github.com/example-org/a", "main", strings.ToUpper(shaA), "run"),
		"sha no run":     row("https://github.com/example-org/a", "main", shaA, "-"),
		"run no sha":     row("https://github.com/example-org/a", "main", "-", "run"),
		"run space":      row("https://github.com/example-org/a", "main", shaA, "two words"),
		"duplicate": row("https://github.com/example-org/a", "main", "-", "-") +
			row("https://github.com/Example-Org/A", "main", "-", "-"),
	}
	for name, data := range cases {
		_, err := watchlist.Parse([]byte(data), "list.tsv")
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if !strings.Contains(err.Error(), "list.tsv") {
			t.Errorf("%s: the error does not name the file: %v", name, err)
		}
	}
}

func TestParseErrorNamesTheLine(t *testing.T) {
	data := "# h\n" + row("https://github.com/example-org/a", "main", "-", "-") + "broken\n"
	_, err := watchlist.Parse([]byte(data), "list.tsv")
	if err == nil || !strings.Contains(err.Error(), "list.tsv:3") {
		t.Fatalf("got %v", err)
	}
}

// W9
func TestFormatRoundTripsAndKeepsTheHeader(t *testing.T) {
	entries := []watchlist.Entry{
		{URL: "https://github.com/example-org/alpha", Branch: "main", LastSHA: shaA, Run: "run-1"},
		{URL: "https://github.com/example-org/beta", Branch: "trunk"},
	}
	out := watchlist.Format(entries)
	if !strings.HasPrefix(string(out), "#") {
		t.Fatalf("no comment header: %q", out)
	}
	back, err := watchlist.Parse(out, "list.tsv")
	if err != nil || len(back) != 2 || back[0] != entries[0] || back[1] != entries[1] {
		t.Fatalf("round trip: %v, %v", back, err)
	}
}

func TestEntryNameAndMirrorDir(t *testing.T) {
	e := watchlist.Entry{URL: "https://github.com/Example-Org/Alpha", Branch: "main"}
	if e.Name() != "Example-Org/Alpha" {
		t.Fatalf("name %q", e.Name())
	}
	if got := e.MirrorDir("/srv/root"); got != "/srv/root/untracked/watchlist/Example-Org__Alpha" {
		t.Fatalf("dir %q", got)
	}
}

// W25: the list this repository ships is valid.
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
}
