package prepush

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/stdinread"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/stdinread/stdinreadtest"
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

// Issue 260: the ref lines are read through stdinread, so a run with an open pipe nothing writes
// to (a hook run by hand in an agent's shell) is refused at the bound, naming it.
func TestReadStdinOnAnOpenPipeIsBounded(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := errors.Join(w.Close(), r.Close()); err != nil {
			t.Error(err)
		}
	})
	c := stdinreadtest.NewClock()
	var errs strings.Builder
	got := make(chan error, 1)
	go func() {
		_, err := ReadStdin(r, stdinread.Options{Prog: "hook", Err: &errs, GiveUpAfter: stdinread.PipedGiveUp, Clock: c})
		got <- err
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := c.Await(ctx, stdinread.NoticeAfter); err != nil {
		t.Fatal(err)
	}
	c.Fire(stdinread.NoticeAfter)
	c.Fire(stdinread.PipedGiveUp)
	if err := <-got; err == nil || !strings.Contains(err.Error(), "within 60 s") {
		t.Fatalf("%v", err)
	}
	if !strings.HasPrefix(errs.String(), "hook: waiting for input on stdin") {
		t.Fatalf("stderr %q", errs.String())
	}
}

func TestReadStdinParsesTheRefLinesAndRefusesNoStdin(t *testing.T) {
	refs, err := ReadStdin(strings.NewReader("refs/heads/F44 "+a+" refs/heads/F44 "+b+"\n"), stdinread.Options{Clock: stdinreadtest.NewClock()})
	if err != nil || len(refs) != 1 || refs[0].RemoteID != b {
		t.Fatalf("%v %v", refs, err)
	}
	if _, err := ReadStdin(nil, stdinread.Options{}); err == nil {
		t.Fatal("want the refusal of no stdin")
	}
}
