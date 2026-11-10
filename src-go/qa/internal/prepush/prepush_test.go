package prepush

import (
	"slices"
	"strings"
	"testing"
)

const (
	a = "1111111111111111111111111111111111111111"
	b = "2222222222222222222222222222222222222222"
)

func TestParseReadsEveryRefLineAndSkipsBlankOnes(t *testing.T) {
	refs, err := Parse(strings.NewReader("refs/heads/F44 " + a + " refs/heads/F44 " + b + "\n\nrefs/heads/x " + ZeroID + " refs/heads/x " + a + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []Ref{
		{LocalRef: "refs/heads/F44", LocalID: a, RemoteRef: "refs/heads/F44", RemoteID: b},
		{LocalRef: "refs/heads/x", LocalID: ZeroID, RemoteRef: "refs/heads/x", RemoteID: a},
	}
	if !slices.Equal(refs, want) {
		t.Fatalf("got %+v", refs)
	}
	if !refs[1].Deletes() || refs[0].Deletes() {
		t.Fatal("Deletes does not tell a deletion")
	}
}

func TestParseRefusesALineWithoutFourFields(t *testing.T) {
	if _, err := Parse(strings.NewReader("refs/heads/F44 " + a + " refs/heads/F44\n")); err == nil {
		t.Fatal("parsed a three-field line")
	}
}

func TestSelection(t *testing.T) {
	for _, tc := range []struct {
		name, remote, remoteID string
		want                   []string
	}{
		{"named remote, existing branch", "origin", b, []string{a, "--not", "--remotes=origin", b}},
		{"named remote, new branch", "origin", ZeroID, []string{a, "--not", "--remotes=origin"}},
		{"url, existing branch", "git@example.com:o/r.git", b, []string{a, "--not", b}},
		{"url, new branch", "https://example.com/o/r", ZeroID, []string{a, "--not", "--remotes"}},
	} {
		got := Ref{LocalID: a, RemoteID: tc.remoteID}.Selection(tc.remote)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
