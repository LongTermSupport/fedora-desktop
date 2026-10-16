package find

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func nul(args ...string) string { return strings.Join(args, "\x00") + "\x00" }

func TestReplaceTeams(t *testing.T) {
	cases := []struct {
		name, teams string
		in, want    []string
	}{
		{"replaced in place", "1@a,2@b", []string{"--token", "t", "--teams", "x@a", "--resume", "id"}, []string{"--token", "t", "--teams", "1@a,2@b", "--resume", "id"}},
		{"added in front", "1@a", []string{"--no-ssh", "--resume", "id"}, []string{"--teams", "1@a", "--no-ssh", "--resume", "id"}},
		{"dropped", "", []string{"--teams=x@a", "--no-ssh"}, []string{"--no-ssh"}},
		{"after -- is claude's", "1@a", []string{"--teams", "x@a", "--", "--teams", "y"}, []string{"--teams", "1@a", "--", "--teams", "y"}},
		{"a value-taking flag's value is not a flag", "1@a", []string{"--token", "--teams", "--no-ssh"}, []string{"--teams", "1@a", "--token", "--teams", "--no-ssh"}},
	}
	for _, c := range cases {
		if got := ReplaceTeams(c.in, c.teams); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestRestartArgsTakesTheTeamsFromTheNamedSessionsRecord(t *testing.T) {
	h := recordRig(t)
	for _, rec := range []registry.Record{
		{Name: "ccy-proj", Dir: "/x", Launcher: "/x/ccy", Prefix: "ccy", Restore: true, Args: []string{"--no-ssh", "--teams", "3@alpha"}},
		{Name: "cc-outer", Dir: "/x", Launcher: "/x/ccy", Prefix: "ccy", Restore: true, Args: []string{"--no-ssh", "--teams", "9@other"}},
	} {
		if err := h.store.Write(rec); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if code := h.f.RestartArgs(t.Context(), []string{"ccy-proj"}, strings.NewReader(nul("--token", "t", "--resume", "id")), &out); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if out.String() != nul("--teams", "3@alpha", "--token", "t", "--resume", "id") {
		t.Fatalf("the named session's record, never the TMUX it inherited: got %q", out.String())
	}
}

func TestRestartArgsPassesThroughForAnUnnamedLaunchOrNoRecord(t *testing.T) {
	h := recordRig(t)
	in := nul("--token", "t", "--teams", "1@a")
	var out bytes.Buffer
	if code := h.f.RestartArgs(t.Context(), []string{"ccy-proj"}, strings.NewReader(in), &out); code != term.ExitOK || out.String() != in {
		t.Fatalf("no record: exit %d out %q", code, out.String())
	}
	out.Reset()
	if code := h.f.RestartArgs(t.Context(), []string{""}, strings.NewReader(in), &out); code != term.ExitOK || out.String() != in {
		t.Fatalf("an unnamed launch: exit %d out %q", code, out.String())
	}
}

func TestRestartArgsRefusesAWrongArgumentCountAndAnUnterminatedList(t *testing.T) {
	h := recordRig(t)
	var out bytes.Buffer
	for _, args := range [][]string{nil, {"a", "b"}} {
		if code := h.f.RestartArgs(t.Context(), args, strings.NewReader(""), &out); code != term.ExitUsage {
			t.Fatalf("%q: exit %d", args, code)
		}
	}
	if code := h.f.RestartArgs(t.Context(), []string{"ccy-proj"}, strings.NewReader("--token"), &out); code != term.ExitFailure || out.Len() != 0 {
		t.Fatalf("unterminated: exit %d out %q", code, out.String())
	}
}
