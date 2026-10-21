package hot

import (
	"reflect"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func (r *rig) labelled(lines string) {
	r.Fake.OnFunc([]string{"podman", "ps", "--filter", "label=ccy-session", "--format"}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{Stdout: []byte(lines)}, nil
	})
}

func TestListPrintsASessionLineForEachRunningSession(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.m.roles["alpha\tproj.3+local.podman"] = coord
	r.labelled("abc123\trunning\t" + session + "\nold1\trunning\tccy-old\ngone\texited\tccy-gone\n")
	if code := r.run(t, "list"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	want := "SESSION\tccy-old\tccy\t-\t-\told or --no-bus: relaunch it to change it while it runs\n" +
		"SESSION\t" + session + "\tccy\t" + r.Checkout() + "\talpha/3/coordinator\tok\n"
	if got := r.Out.String(); got != want {
		t.Fatalf("stdout\n%q\nwant\n%q", got, want)
	}
}

func TestListSaysWhatIsWrongWithASession(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.labelled("abc123\trunning\t" + session + "\n")
	r.On("systemctl is-active agent-bus-hs@alpha.service", "inactive\n", 3)
	if code := r.run(t, "list"); code != term.ExitOK || !strings.HasSuffix(r.Out.String(), "\talpha down\n") {
		t.Fatalf("down: exit %d: %q", code, r.Out.String())
	}
	r.m.alive, r.m.keeper = false, false
	if code := r.run(t, "list"); code != term.ExitOK ||
		!strings.HasSuffix(r.Out.String(), "\talpha/3/-\tunguarded: ccy-teams session repair "+session+"\n") {
		t.Fatalf("unguarded: exit %d: %q", code, r.Out.String())
	}
}

func TestListSaysWhenTheKeepersReportCannotBeRead(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.labelled("abc123\trunning\t" + session + "\n")
	r.m.report = "{not json"
	if code := r.run(t, "list"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	got := r.Out.String()
	if !strings.Contains(got, "its seat keeper's report cannot be read: ") || !strings.Contains(got, membership.ReportPath(r.Checkout(), session)) {
		t.Fatalf("a report that cannot be read is said, with its path: %q", got)
	}
	if rows, err := r.x.Sessions(t.Context()); err != nil || len(rows) != 1 || strings.Contains(rows[0].State, "ok") {
		t.Fatalf("the row is not ok: %+v %v", rows, err)
	}
}

func TestListIsQuietAboutAReportTheKeeperHasNotWrittenYet(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.labelled("abc123\trunning\t" + session + "\n")
	r.m.report = "-"
	if code := r.run(t, "list"); code != term.ExitOK || strings.Contains(r.Out.String(), "report") {
		t.Fatalf("exit %d: %q", code, r.Out.String())
	}
}

// Fable's M6 review B1, the ccy half: a ccy --no-bus under the name of an ended ccy session finds
// that session's membership; the record its own launch wrote says --no-bus, so the list reads it
// as a session off the bus, in no team.
func TestListReadsNoMembershipForACcyNoBusUnderAReusedName(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.labelled("abc123\trunning\t" + session + "\n")
	rec, err := r.store.Get(session)
	if err != nil {
		t.Fatal(err)
	}
	rec.Args = []string{"--no-bus"}
	if err = r.store.Write(rec); err != nil {
		t.Fatal(err)
	}
	if code := r.run(t, "list"); code != term.ExitOK ||
		r.Out.String() != "SESSION\t"+session+"\tccy\t-\t-\told or --no-bus: relaunch it to change it while it runs\n" {
		t.Fatalf("exit %d: %q", code, r.Out.String())
	}
	if in, err := r.x.SessionsIn(t.Context(), "alpha"); err != nil || len(in) != 0 {
		t.Fatalf("in alpha: %v %v", in, err)
	}
}

func TestSessionsInATeamAndTakingOneOff(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.labelled("abc123\trunning\t" + session + "\nold1\trunning\tccy-old\n")
	in, err := r.x.SessionsIn(t.Context(), "alpha")
	if err != nil || len(in) != 1 || in[0] != session {
		t.Fatalf("in alpha: %v %v", in, err)
	}
	if in, err = r.x.SessionsIn(t.Context(), "beta"); err != nil || len(in) != 0 {
		t.Fatalf("in beta: %v %v", in, err)
	}
	r.m.roles["alpha\tproj.3+local.podman"] = coord
	if !r.x.TakeOut(t.Context(), session, "alpha") {
		t.Fatalf("not taken off:\n%s", r.ErrOut.String())
	}
	if len(r.desired(t).Seats) != 0 || r.m.role("alpha", "proj.3+local.podman") != worker {
		t.Fatal("the team being taken down: its coordinator is demoted as it leaves")
	}
}

func TestRepairStartsTheKeeperAgainAndWaitsForItsSeats(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.m.alive, r.m.keeper = false, false
	if code := r.run(t, "repair", session, "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if !r.Fake.Called("podman exec -d abc123 pingbus seat keep") {
		t.Fatalf("calls %v", r.Fake.Calls())
	}
	wantLines(t, r.ErrOut.String(), "✓ keeper: running again, 1 seat held (0.0 s)")
	if code := r.run(t, "repair", session, "--yes"); code != term.ExitOK {
		t.Fatalf("rerun exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "- keeper: running", "Nothing to do: "+session+"'s seat keeper is running.")
}

func TestRepairWhoseKeeperDoesNotComeBackSaysSo(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.m.alive, r.m.keeper = false, false
	r.On("podman exec -d", "", 0)
	if code := r.run(t, "repair", session, "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✗ keeper: not running after 10 s: see /tmp/pingbus-home/keeper.log in the session")
}

// M5 (section 5.1): the picker's Running sessions view reads the same sessions `session list`
// prints, with whether each can be changed and whether its seat keeper has stopped.
func TestSessionsAreWhatTheListShows(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.m.roles["alpha\tproj.3+local.podman"] = coord
	r.labelled("abc123\trunning\t" + session + "\nold1\trunning\tccy-old\ngone\texited\tccy-gone\n")
	got, err := r.x.Sessions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "ccy-old" || got[1].Name != session {
		t.Fatalf("sessions %+v", got)
	}
	old := Session{Name: "ccy-old", Kind: "ccy", Checkout: "-", State: "old or --no-bus: relaunch it to change it while it runs"}
	if !reflect.DeepEqual(got[0], old) {
		t.Fatalf("old session %+v", got[0])
	}
	seated := Session{
		Name: session, Kind: "ccy", Checkout: r.Checkout(), State: "ok", Controllable: true,
		Teams: []Cell{{Team: "alpha", Seat: "3", Role: coord}},
	}
	if !reflect.DeepEqual(got[1], seated) {
		t.Fatalf("session %+v", got[1])
	}
	r.m.alive, r.m.keeper = false, false
	if got, err = r.x.Sessions(t.Context()); err != nil || !got[1].Unguarded || !got[1].Controllable {
		t.Fatalf("unguarded: %+v %v", got, err)
	}
}

func TestSessionsNeedAnAbsoluteRuntimeDirectory(t *testing.T) {
	r := newRig(t)
	r.Env["XDG_RUNTIME_DIR"] = "relative"
	if _, err := r.x.Sessions(t.Context()); err == nil || !strings.Contains(err.Error(), "XDG_RUNTIME_DIR") {
		t.Fatalf("err %v", err)
	}
}

// On a terminal `session list` is the table for a person, and says once what it cannot list.
func TestListOnATerminalIsTheTable(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.labelled("old1\trunning\tccy-old\n")
	r.Host.IsTerminal = func(any) bool { return true }
	if code := r.run(t, "list"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	heading, lines, err := SessionLines([]Session{{Name: "ccy-old", Checkout: "-", State: "old or --no-bus: relaunch it to change it while it runs"}}, r.Host.Home)
	if err != nil {
		t.Fatal(err)
	}
	if want := heading + "\n" + lines[0] + "\n"; r.Out.String() != want {
		t.Fatalf("stdout\n%q\nwant\n%q", r.Out.String(), want)
	}
	if strings.Count(r.ErrOut.String(), OnlyTmuxLine) != 1 {
		t.Fatalf("stderr %q", r.ErrOut.String())
	}
}

// The table `session list` prints and the picker's rows are one layout: the heading and each
// row aligned in the same columns, the checkout shown from the home directory.
func TestSessionLinesAlignUnderTheHeading(t *testing.T) {
	heading, lines, err := SessionLines([]Session{
		{Name: "ccy-a-1", Checkout: "/home/u/repo-a", State: "ok", Teams: []Cell{{"alpha", "2", coord}, {"beta", "1", worker}}},
		{Name: "ccy-long-name-2", Checkout: "-", State: "old or --no-bus"},
	}, "/home/u")
	if err != nil {
		t.Fatal(err)
	}
	if heading != "SESSION          CHECKOUT  TEAMS (seat, role)                         STATE" {
		t.Fatalf("heading %q", heading)
	}
	want := []string{
		"ccy-a-1          ~/repo-a  alpha (2, coordinator) · beta (1, worker)  ok",
		"ccy-long-name-2  -         -                                          old or --no-bus",
	}
	if len(lines) != 2 || lines[0] != want[0] || lines[1] != want[1] {
		t.Fatalf("lines\n%q\nwant\n%q", lines, want)
	}
}
