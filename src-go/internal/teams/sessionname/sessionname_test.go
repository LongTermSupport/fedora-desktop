package sessionname

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
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

// here is the checkout every launch in these tests is made from.
const here = "/w/proj"

// fakeRecords is the session registry: its reading, or the error reading it gives.
type fakeRecords struct {
	reading registry.Reading
	err     error
}

func (f fakeRecords) read() (registry.Reading, error) { return f.reading, f.err }

// records is a reading of recs, each in the file its name names.
func records(recs ...registry.Record) fakeRecords {
	var r registry.Reading
	for _, rec := range recs {
		r.Records = append(r.Records, rec)
		r.Files = append(r.Files, "/reg/"+rec.Name)
	}
	return fakeRecords{reading: r}
}

func run(t *testing.T, server fakeServer, args ...string) (term.ExitCode, string, string) {
	t.Helper()
	return runWith(t, server, fakeRecords{}, args...)
}

func runWith(t *testing.T, server fakeServer, reg fakeRecords, args ...string) (term.ExitCode, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Command{Server: server, Records: reg.read, Stdout: &out, Stderr: &errOut}.Run(t.Context(), args)
	return code, out.String(), errOut.String()
}

func TestRunPrintsTheFirstFreeName(t *testing.T) {
	for _, tc := range []struct {
		taken []string
		args  []string
		want  string
	}{
		{nil, []string{"ccy", "proj", "1@alpha", here}, "ccy-alpha-proj-1\n"},
		{[]string{"ccy-alpha-proj-1", "ccy-alpha-proj-1-2"}, []string{"ccy", "proj", "1@alpha", here}, "ccy-alpha-proj-1-3\n"},
		{nil, []string{"cc", "proj", "", here}, "cc-proj\n"},
		{[]string{"cc-proj", "cc-proj-2-x"}, []string{"cc", "proj", "", here}, "cc-proj-2\n"},
		{[]string{"cc-proj-2"}, []string{"cc", "proj", "", here}, "cc-proj\n"},
	} {
		code, out, errOut := run(t, fakeServer{names: tc.taken}, tc.args...)
		if code != term.ExitOK || out != tc.want {
			t.Fatalf("%v taken %v: exit %d out %q err %q, want %q", tc.args, tc.taken, code, out, errOut, tc.want)
		}
	}
}

// N1 (Plan 00265 R90, Q7): a seat whose role is the only one of its kind among the checkout's
// pool seats of the team is named by the role; two workers keep their seats, to tell them apart.
// A team outside teamchat's grammar is not asked; a lookup that fails is exit 1.
func TestASeatWithAUniqueRoleIsNamedByIt(t *testing.T) {
	roles := map[string]map[string]string{
		"alpha": {"1": "coordinator", "2": "worker", "3": "worker"},
		"beta":  {"4": "worker"},
	}
	var asked []string
	lookup := func(checkout, team string) (map[string]string, error) {
		asked = append(asked, checkout+" "+team)
		return roles[team], nil
	}
	for seats, want := range map[string]string{
		"1@alpha":        "ccy-alpha-proj-coordinator",
		"2@alpha":        "ccy-alpha-proj-2",
		"4@beta":         "ccy-beta-proj-worker",
		"1@alpha,4@beta": "ccy-alpha-proj-coordinator_beta-proj-worker",
		"9@alpha":        "ccy-alpha-proj-9",
		"1@status":       "ccy-status-proj-1",
	} {
		var out, errOut bytes.Buffer
		code := Command{Server: fakeServer{}, Records: fakeRecords{}.read, Roles: lookup, Stdout: &out, Stderr: &errOut}.
			Run(t.Context(), []string{"ccy", "proj", seats, here})
		if code != term.ExitOK || out.String() != want+"\n" {
			t.Errorf("%s: exit %d out %q err %q, want %q", seats, code, out.String(), errOut.String(), want)
		}
	}
	if !slices.Contains(asked, here+" alpha") || slices.Contains(asked, here+" status") {
		t.Errorf("the roles were not asked of this checkout's teamchat teams alone: %v", asked)
	}
	var out, errOut bytes.Buffer
	failing := func(string, string) (map[string]string, error) { return nil, errors.New("status unreadable") }
	code := Command{Server: fakeServer{}, Records: fakeRecords{}.read, Roles: failing, Stdout: &out, Stderr: &errOut}.
		Run(t.Context(), []string{"ccy", "proj", "1@alpha", here})
	if code != term.ExitFailure || out.String() != "" || !strings.Contains(errOut.String(), "status unreadable") {
		t.Fatalf("a failed lookup: exit %d out %q err %q", code, out.String(), errOut.String())
	}
}

func TestRunSaysWhenTheSeatsAreNotUsed(t *testing.T) {
	long := strings.Repeat("p", 120)
	code, out, errOut := run(t, fakeServer{}, "cc", long, "1@alpha", here)
	if code != term.ExitOK || out != "cc-"+long+"\n" || !strings.Contains(errOut, "longer than") {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
}

func TestRunFailsWhenTheServerCannotBeListed(t *testing.T) {
	code, out, errOut := run(t, fakeServer{err: errors.New("tmux list-sessions failed: boom")}, "ccy", "proj", "", here)
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
		{"the name", failingWriter{}, io.Discard, []string{"ccy", "proj", "1@alpha", here}},
		{"the note", io.Discard, failingWriter{}, []string{"ccy", long, "1@alpha", here}},
		{"a refusal", io.Discard, failingWriter{}, []string{"ccy"}},
		{"the take-over", io.Discard, failingWriter{}, []string{"ccy", "proj", "4@alpha", here}},
	} {
		reg := records(registry.Record{Name: "ccy-fixed", Dir: here, Prefix: "ccy", Args: []string{"--teams", "4@alpha"}})
		code := Command{Server: fakeServer{}, Records: reg.read, Stdout: tc.stdout, Stderr: tc.stderr}.Run(t.Context(), tc.args)
		if code != term.ExitFailure {
			t.Fatalf("%s: exit %d", tc.name, code)
		}
	}
}

func TestRunRefusesWrongArguments(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"ccy", "proj"},
		{"ccy", "proj", "a@b"},
		{"ccy", "proj", "a@b", here, "x"},
		{"nope", "proj", "", here},
		{"ccy", "proj", "", "relative/dir"},
		{"ccy", "proj", "", ""},
	} {
		code, out, errOut := run(t, fakeServer{}, args...)
		if code != term.ExitUsage || out != "" || errOut == "" {
			t.Fatalf("%q: exit %d out %q err %q", args, code, out, errOut)
		}
	}
}

// declared is the record `ccy-sessions declare --name ccy-fixed --fresh-if-no-conversation` writes
// for a session in seat 4 of team alpha in this checkout.
func declared() registry.Record {
	return registry.Record{
		Name: "ccy-fixed", Dir: here, Launcher: "/l/claude-yolo", Prefix: "ccy", Restore: true,
		FreshIfNoConversation: true, Args: []string{"--teams", "4@alpha", "--token", "work"},
	}
}

// N7: a launch takes the name of the one stopped record of its checkout, launcher and seats, so
// its record replaces that one instead of standing beside it; the fresh mark rides on stdout.
func TestALaunchTakesTheNameOfItsCheckoutsRecordForTheSameSeats(t *testing.T) {
	code, out, errOut := runWith(t, fakeServer{}, records(declared()), "ccy", "proj", "4@alpha", here)
	if code != term.ExitOK || out != "ccy-fixed yes\n" || !strings.Contains(errOut, "ccy-fixed") {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
	plain := declared()
	plain.FreshIfNoConversation = false
	code, out, errOut = runWith(t, fakeServer{}, records(plain), "ccy", "proj", "4@alpha", here)
	if code != term.ExitOK || out != "ccy-fixed\n" {
		t.Fatalf("unmarked: exit %d out %q err %q", code, out, errOut)
	}
}

func TestTheSeatsMatchAsTheSeatCheckReadsThem(t *testing.T) {
	for _, tc := range []struct{ launcher, recorded, launched string }{
		{"ccy", "--teams=3@beta,1@alpha", "1@alpha,3@beta"},
		{"cc", "1@alpha,3@beta", "1@alpha, 3@beta"},
	} {
		rec := registry.Record{Name: tc.launcher + "-fixed", Dir: here, Prefix: tc.launcher, Args: []string{tc.recorded}}
		if !strings.HasPrefix(tc.recorded, "--teams=") {
			rec.Args = []string{"--teams", tc.recorded}
		}
		code, out, errOut := runWith(t, fakeServer{}, records(rec), tc.launcher, "proj", tc.launched, here)
		if code != term.ExitOK || out != tc.launcher+"-fixed\n" {
			t.Fatalf("%v: exit %d out %q err %q", tc, code, out, errOut)
		}
	}
}

func TestALaunchInNoSeatTakesARecordInNoSeat(t *testing.T) {
	rec := registry.Record{Name: "ccy-fixed", Dir: here, Prefix: "ccy", Args: []string{"--token", "work"}}
	code, out, errOut := runWith(t, fakeServer{}, records(rec), "ccy", "proj", "", here)
	if code != term.ExitOK || out != "ccy-fixed\n" {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
}

// A record nobody would call this launch's is never taken: the new form is printed, silently.
func TestARecordOfOtherSeatsCheckoutOrLauncherIsNotTaken(t *testing.T) {
	other := func(change func(*registry.Record)) registry.Record {
		rec := declared()
		change(&rec)
		return rec
	}
	for _, tc := range []struct {
		why     string
		reg     fakeRecords
		running []string
		seats   string
		want    string
	}{
		{"another checkout", records(other(func(r *registry.Record) { r.Dir = "/w/other" })), nil, "4@alpha", "ccy-alpha-proj-4\n"},
		{"the other launcher", records(other(func(r *registry.Record) { r.Prefix = "cc" })), nil, "4@alpha", "ccy-alpha-proj-4\n"},
		{"more seats", records(other(func(r *registry.Record) { r.Args = []string{"--teams", "4@alpha,1@beta"} })), nil, "4@alpha", "ccy-alpha-proj-4\n"},
		{"no seats", records(other(func(r *registry.Record) { r.Args = nil })), nil, "4@alpha", "ccy-alpha-proj-4\n"},
		{"seats, launched in none", records(declared()), nil, "", "ccy-proj\n"},
		{"seats the check refuses", records(other(func(r *registry.Record) { r.Args = []string{"--teams", "team-a"} })), nil, "team-a", "ccy-proj\n"},
		{"its session is running", records(declared()), []string{"ccy-fixed"}, "4@alpha", "ccy-alpha-proj-4\n"},
		{"in a file of another name", fakeRecords{reading: registry.Reading{Records: []registry.Record{declared()}, Files: []string{"/reg/old"}}}, nil, "4@alpha", "ccy-alpha-proj-4\n"},
	} {
		code, out, errOut := runWith(t, fakeServer{names: tc.running}, tc.reg, "ccy", "proj", tc.seats, here)
		if code != term.ExitOK || out != tc.want || errOut != "" {
			t.Fatalf("%s: exit %d out %q err %q, want %q", tc.why, code, out, errOut, tc.want)
		}
	}
}

func TestTwoMatchingRecordsAreNeitherTakenAndItIsSaid(t *testing.T) {
	second := declared()
	second.Name = "ccy-alpha-proj-4"
	code, out, errOut := runWith(t, fakeServer{}, records(declared(), second), "ccy", "proj", "4@alpha", here)
	if code != term.ExitOK || out != "ccy-alpha-proj-4\n" ||
		!strings.Contains(errOut, "ccy-fixed") || !strings.Contains(errOut, "ccy-alpha-proj-4") {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
}

func TestAnUnreadableRecordIsSaidAndNotTaken(t *testing.T) {
	reg := records(declared())
	reg.reading.Problems = []registry.Problem{{File: "/reg/ccy-broken", Err: errors.New("is empty")}}
	code, out, errOut := runWith(t, fakeServer{}, reg, "ccy", "proj", "4@alpha", here)
	if code != term.ExitOK || out != "ccy-fixed yes\n" || !strings.Contains(errOut, "/reg/ccy-broken") {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
}

func TestRunFailsWhenTheRegistryCannotBeRead(t *testing.T) {
	code, out, errOut := runWith(t, fakeServer{}, fakeRecords{err: errors.New("registry boom")}, "ccy", "proj", "", here)
	if code != term.ExitFailure || out != "" || !strings.Contains(errOut, "registry boom") {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
}
