package ui

import (
	"slices"
	"testing"
)

// Grapheme clusters the tests use: a ZWJ family (five runes, two cells) and a flag (two
// regional indicators, two cells).
const (
	family = "\U0001F468\u200D\U0001F469\u200D\U0001F467"
	flag   = "\U0001F1EC\U0001F1E7"
)

func TestClustersSplitsTextIntoWhatATerminalDrawsAsOneWithItsCells(t *testing.T) {
	got := Clusters("a" + family + "b" + flag + "é")
	want := []Cluster{{"a", 1}, {family, 2}, {"b", 1}, {flag, 2}, {"é", 1}}
	if !slices.Equal(got, want) {
		t.Fatalf("Clusters = %+v, want %+v", got, want)
	}
	if Clusters("") != nil {
		t.Fatal("no text has no clusters")
	}
	for _, c := range got {
		if c.Cells != Width(c.Text) {
			t.Fatalf("%q: %d cells, Width says %d", c.Text, c.Cells, Width(c.Text))
		}
	}
}
