package teamchatseparate

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestParseAllowedReadsRows(t *testing.T) {
	rows, err := ParseAllowed("# path\tliteral\treason\n\na/b.go\tBefore=agent-bus-hs@\tthe fence orders before v1's server\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []Allowed{{Line: 3, Path: "a/b.go", Literal: "Before=agent-bus-hs@", Reason: "the fence orders before v1's server"}}
	if !slices.Equal(rows, want) {
		t.Fatalf("got %+v want %+v", rows, want)
	}
}

// An empty allowlist is valid: no remaining reference is the goal.
func TestParseAllowedAcceptsNoRows(t *testing.T) {
	rows, err := ParseAllowed("# header only\n")
	if err != nil || len(rows) != 0 {
		t.Fatalf("got %+v, %v", rows, err)
	}
}

func TestParseAllowedRefusesABadRow(t *testing.T) {
	for _, c := range []struct{ data, want string }{
		{"a/b.go\tlit\n", "line 1: want 3 tab-separated fields"},
		{"a/b.go\tlit\twhy\textra\n", "line 1: want 3 tab-separated fields"},
		{"\tlit\twhy\n", "line 1: an empty field"},
		{"a/b.go\t\twhy\n", "line 1: an empty field"},
		{"a/b.go\tlit\t \n", "line 1: an empty field"},
		{"a/b.go\tlit\twhy\na/b.go\tlit\tagain\n", "line 2: a/b.go lit is listed twice"},
	} {
		_, err := ParseAllowed(c.data)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("ParseAllowed(%q) = %v, want an error containing %q", c.data, err, c.want)
		}
	}
}

// The shipped allowlist parses.
func TestTheShippedAllowlistParses(t *testing.T) {
	data, err := os.ReadFile(allowedFile[len(packageDir):])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseAllowed(string(data)); err != nil {
		t.Fatal(err)
	}
}
