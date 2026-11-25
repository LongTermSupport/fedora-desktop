package sessionname

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

func TestASeatSessionNamesLauncherTeamAndSeat(t *testing.T) {
	for _, tc := range []struct{ launcher, seats, want string }{
		{"ccy", "fedora-desktop.4@team-a", "ccy-team-a-fedora-desktop-4"},
		{"cc", "fedora-desktop.2@team-b", "cc-team-b-fedora-desktop-2"},
		{"cc", "a+b.1@x", "cc-x-a-b-1"},
		{"ccy", "app.1@alpha,app.3@beta", "ccy-alpha-app-1_beta-app-3"},
	} {
		got, note, err := Name(tc.launcher, "proj", tc.seats)
		if err != nil || got != tc.want || note != "" {
			t.Fatalf("%s %q: got %q note %q err %v, want %q", tc.launcher, tc.seats, got, note, err, tc.want)
		}
		if _, err := paths.ParseSessionName(got); err != nil {
			t.Fatalf("%q is not a session name the seat store takes: %v", got, err)
		}
	}
}

func TestNoSeatKeepsTheProjectName(t *testing.T) {
	for _, seats := range []string{"", "team-a", "app.1@alpha,beta", "@alpha", "app.1@"} {
		got, note, err := Name("cc", "parent-proj", seats)
		if err != nil || got != "cc-parent-proj" || note != "" {
			t.Fatalf("%q: got %q note %q err %v", seats, got, note, err)
		}
	}
}

func TestATooLongNameKeepsTheProjectNameAndSaysSo(t *testing.T) {
	seats := strings.Repeat("s", 60) + ".1@alpha," + strings.Repeat("t", 60) + ".2@beta"
	got, note, err := Name("ccy", "proj", seats)
	if err != nil || got != "ccy-proj" || !strings.Contains(note, "longer than") {
		t.Fatalf("got %q note %q err %v", got, note, err)
	}
}

func TestWrongLauncherOrProjectIsRefused(t *testing.T) {
	for _, tc := range []struct{ launcher, project string }{
		{"podman", "proj"}, {"", "proj"}, {"ccy", ""}, {"ccy", "a.b"}, {"ccy", "-x"},
	} {
		if _, _, err := Name(tc.launcher, tc.project, ""); err == nil {
			t.Fatalf("%q %q was taken", tc.launcher, tc.project)
		}
	}
}

// fakeServer is ccy's tmux server: its sessions, or the error listing them gives.
type fakeServer struct {
	names []string
	err   error
}

func (f fakeServer) List(context.Context) ([]tmuxccy.Session, error) {
	sessions := make([]tmuxccy.Session, 0, len(f.names))
	for _, n := range f.names {
		sessions = append(sessions, tmuxccy.Session{Name: n, Path: "/elsewhere"})
	}
	return sessions, f.err
}

func run(t *testing.T, server fakeServer, args ...string) (term.ExitCode, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Command{Server: server, Stdout: &out, Stderr: &errOut}.Run(t.Context(), args)
	return code, out.String(), errOut.String()
}

func TestRunPrintsTheFirstFreeName(t *testing.T) {
	for _, tc := range []struct {
		taken []string
		args  []string
		want  string
	}{
		{nil, []string{"ccy", "proj", "app.1@alpha"}, "ccy-alpha-app-1\n"},
		{[]string{"ccy-alpha-app-1", "ccy-alpha-app-1-2"}, []string{"ccy", "proj", "app.1@alpha"}, "ccy-alpha-app-1-3\n"},
		{nil, []string{"cc", "proj", ""}, "cc-proj\n"},
		{[]string{"cc-proj", "cc-proj-2-x"}, []string{"cc", "proj", ""}, "cc-proj-2\n"},
		{[]string{"cc-proj-2"}, []string{"cc", "proj", ""}, "cc-proj\n"},
	} {
		code, out, errOut := run(t, fakeServer{names: tc.taken}, tc.args...)
		if code != term.ExitOK || out != tc.want {
			t.Fatalf("%v taken %v: exit %d out %q err %q, want %q", tc.args, tc.taken, code, out, errOut, tc.want)
		}
	}
}

func TestRunSaysWhenTheSeatsAreNotUsed(t *testing.T) {
	code, out, errOut := run(t, fakeServer{}, "cc", "proj", strings.Repeat("s", 130)+"@alpha")
	if code != term.ExitOK || out != "cc-proj\n" || !strings.Contains(errOut, "longer than") {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
}

func TestRunFailsWhenTheServerCannotBeListed(t *testing.T) {
	code, out, errOut := run(t, fakeServer{err: errors.New("tmux list-sessions failed: boom")}, "ccy", "proj", "")
	if code != term.ExitFailure || out != "" || !strings.Contains(errOut, "boom") {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
}

func TestRunRefusesWrongArguments(t *testing.T) {
	for _, args := range [][]string{{}, {"ccy", "proj"}, {"ccy", "proj", "a@b", "x"}, {"nope", "proj", ""}} {
		code, out, errOut := run(t, fakeServer{}, args...)
		if code != term.ExitUsage || out != "" || errOut == "" {
			t.Fatalf("%q: exit %d out %q err %q", args, code, out, errOut)
		}
	}
}
