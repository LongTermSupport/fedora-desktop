package sessionname

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

func TestASeatSessionNamesLauncherTeamProjectAndSeat(t *testing.T) {
	for _, tc := range []struct{ launcher, project, seats, want string }{
		{"ccy", "fedora-desktop", "4@team-a", "ccy-team-a-fedora-desktop-4"},
		{"cc", "fedora-desktop", "dev@team-b", "cc-team-b-fedora-desktop-dev"},
		{"ccy", "proj", "1@alpha,3@beta", "ccy-alpha-proj-1_beta-proj-3"},
	} {
		got, note, err := Name(tc.launcher, tc.project, tc.seats)
		if err != nil || got != tc.want || note != "" {
			t.Fatalf("%s %q: got %q note %q err %v, want %q", tc.launcher, tc.seats, got, note, err, tc.want)
		}
		if _, err := paths.ParseSessionName(got); err != nil {
			t.Fatalf("%q is not a session name the seat store takes: %v", got, err)
		}
	}
}

// cc names its session from the --teams list as typed; ccy from the seat check's canonical
// list (whitespace removed, each item <seat>@<team>). Every spelling the check accepts must
// give the name its canonical list gives.
func TestCCsTypedListNamesAsCCYsCanonicalOne(t *testing.T) {
	for typed, canonical := range map[string]string{
		"1@alpha, 3@beta": "1@alpha,3@beta",
		" 1@alpha":        "1@alpha",
		"1 @alpha":        "1@alpha",
	} {
		cc, _, ccErr := Name("cc", "proj", typed)
		ccy, _, ccyErr := Name("ccy", "proj", canonical)
		if ccErr != nil || ccyErr != nil || cc == "cc-proj" || strings.TrimPrefix(cc, "cc-") != strings.TrimPrefix(ccy, "ccy-") {
			t.Fatalf("%q: cc %q (%v), ccy %q (%v)", typed, cc, ccErr, ccy, ccyErr)
		}
	}
}

func TestAListTheSeatCheckRefusesKeepsTheProjectName(t *testing.T) {
	for _, seats := range []string{"", "team-a", "1@alpha,beta", "@alpha", "1@", "A@alpha", "1@Alpha", "1@alpha,2@alpha", "a@b@c", "app.1@alpha"} {
		got, note, err := Name("cc", "parent-proj", seats)
		if err != nil || got != "cc-parent-proj" || note != "" {
			t.Fatalf("%q: got %q note %q err %v", seats, got, note, err)
		}
	}
}

func TestATooLongNameKeepsTheProjectNameAndSaysSo(t *testing.T) {
	project := strings.Repeat("p", 110)
	got, note, err := Name("ccy", project, "1@alpha")
	if err != nil || got != "ccy-"+project || !strings.Contains(note, "longer than") {
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
		{nil, []string{"ccy", "proj", "1@alpha"}, "ccy-alpha-proj-1\n"},
		{[]string{"ccy-alpha-proj-1", "ccy-alpha-proj-1-2"}, []string{"ccy", "proj", "1@alpha"}, "ccy-alpha-proj-1-3\n"},
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
	long := strings.Repeat("p", 120)
	code, out, errOut := run(t, fakeServer{}, "cc", long, "1@alpha")
	if code != term.ExitOK || out != "cc-"+long+"\n" || !strings.Contains(errOut, "longer than") {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
}

func TestRunFailsWhenTheServerCannotBeListed(t *testing.T) {
	code, out, errOut := run(t, fakeServer{err: errors.New("tmux list-sessions failed: boom")}, "ccy", "proj", "")
	if code != term.ExitFailure || out != "" || !strings.Contains(errOut, "boom") {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
}

// failingWriter is a stream that cannot be written: a closed stdout or stderr.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestRunFailsWhenItCannotWrite(t *testing.T) {
	long := strings.Repeat("p", 120)
	for _, tc := range []struct {
		name           string
		stdout, stderr io.Writer
		args           []string
	}{
		{"the name", failingWriter{}, io.Discard, []string{"ccy", "proj", "1@alpha"}},
		{"the note", io.Discard, failingWriter{}, []string{"ccy", long, "1@alpha"}},
		{"a refusal", io.Discard, failingWriter{}, []string{"ccy"}},
	} {
		code := Command{Server: fakeServer{}, Stdout: tc.stdout, Stderr: tc.stderr}.Run(t.Context(), tc.args)
		if code != term.ExitFailure {
			t.Fatalf("%s: exit %d", tc.name, code)
		}
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
