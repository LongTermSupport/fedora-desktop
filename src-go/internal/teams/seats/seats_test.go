package seats

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func (h rig) attachable() {
	h.On("tmux -L ccy list-clients", "", 0)
	h.On("tmux -L ccy set-hook", "", 0)
	h.On("tmux -L ccy attach-session", "", 0)
}

func TestAttachSeatAttachesALiveSessionAndEndsThePicker(t *testing.T) {
	h := newRig(t)
	h.attachable()
	snap := h.pass(t)
	res, err := h.s.AttachSeat(context.Background(), snap, h.row(t, snap, "1"))
	if code, ok := flow.ExitCodeOf(err); !ok || code != 0 || res != flow.Done {
		t.Fatalf("%v %v %v", res, err, h.ErrOut.String())
	}
	if !h.Fake.Called("tmux -L ccy attach-session -t =ccy-proj") {
		t.Fatalf("calls %v", h.Fake.Calls())
	}
}

// Plan 00203 M5: the picker's Running sessions view attaches a session by its name, through
// the same checks as a seat's attach.
func TestAttachSessionAttachesALiveSessionByName(t *testing.T) {
	h := newRig(t)
	h.attachable()
	snap := h.pass(t)
	res, err := h.s.AttachSession(context.Background(), snap, "ccy-proj")
	if code, ok := flow.ExitCodeOf(err); !ok || code != 0 || res != flow.Done {
		t.Fatalf("%v %v %v", res, err, h.ErrOut.String())
	}
	if !h.Fake.Called("tmux -L ccy attach-session -t =ccy-proj") {
		t.Fatalf("calls %v", h.Fake.Calls())
	}
	h = newRig(t)
	snap = h.pass(t)
	res, err = h.s.AttachSession(context.Background(), snap, "ccy-gone")
	if err != nil || res != flow.Refused ||
		!strings.Contains(h.ErrOut.String(), "'ccy-gone' is not running in ccy's tmux any more, so there is nothing to attach.\n") {
		t.Fatalf("%v %v %q", res, err, h.ErrOut.String())
	}
}

func TestAttachSeatRefusesWhatCannotBeAttached(t *testing.T) {
	cases := map[string]string{
		"dev":                 "Seat proj.dev+local.podman in alpha has no running session to attach. Ctrl-N starts one.\n",
		"proj.2+local.podman": "proj.2+local.podman is a ccy member whose checkout is not known here, so there is no session to attach.\n",
	}
	for member, want := range cases {
		h := newRig(t)
		snap := h.pass(t)
		res, err := h.s.AttachSeat(context.Background(), snap, h.row(t, snap, member))
		if err != nil || res != flow.Refused {
			t.Fatalf("%s: %v %v", member, res, err)
		}
		if !strings.Contains(h.ErrOut.String(), want+"Press Enter to go back to the list.\n") {
			t.Fatalf("%s: stderr %q", member, h.ErrOut.String())
		}
	}

	h := newRig(t)
	h.On("tmux -L ccy list-sessions", "ccy-proj 1 "+h.Checkout()+"\n", 0)
	snap := h.pass(t)
	res, err := h.s.AttachSeat(context.Background(), snap, h.row(t, snap, "1"))
	if err != nil || res != flow.Refused {
		t.Fatalf("%v %v", res, err)
	}
	if !strings.Contains(h.ErrOut.String(), "'ccy-proj' is open in another terminal. ccy-sessions takes it over (Ctrl-T).\n") ||
		h.Fake.Called("tmux -L ccy attach-session") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestAttachSeatSaysSoWhenTheSessionWasTakenMeanwhile(t *testing.T) {
	h := newRig(t)
	h.On("tmux -L ccy list-clients", "/dev/pts/9 100\n", 0)
	snap := h.pass(t)
	res, err := h.s.AttachSeat(context.Background(), snap, h.row(t, snap, "1"))
	if err != nil || res != flow.Refused {
		t.Fatalf("%v %v", res, err)
	}
	for _, want := range []string{
		"ERROR: 'ccy-proj' is open in another terminal, or gone. It can be attached from one terminal only.\n",
		"'ccy-proj' was taken or ended just now — the list has been refreshed.\n",
	} {
		if !strings.Contains(h.ErrOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.ErrOut.String(), want)
		}
	}
}

func newSessionHarness(t *testing.T) rig {
	t.Helper()
	h := newRig(t)
	h.inProject()
	return h
}

func TestNewSessionOffersFreeSeatsAndANewOneAndExecsTheLauncherInTheTop(t *testing.T) {
	h := newSessionHarness(t)
	h.Script.Fzf = []string{"|new"}
	snap := h.pass(t)
	res, err := h.s.NewSession(context.Background(), snap, "alpha")
	if code, ok := flow.ExitCodeOf(err); !ok || code != 0 || res != flow.Done {
		t.Fatalf("%v %v: %s", res, err, h.ErrOut.String())
	}
	if want := []string{"proj.dev+local.podman   free\tdev@alpha", "Seat 3@alpha   new\t3@alpha", "Exit"}; !slices.Equal(h.Script.Rows[0], want) {
		t.Fatalf("rows %q", h.Script.Rows[0])
	}
	if len(h.Execs) != 1 || h.Execs[0].Dir != h.Checkout() || h.Execs[0].Path != "/x/launcher" ||
		!slices.Equal(h.Execs[0].Argv, []string{"/x/launcher", "--teams", "3@alpha"}) {
		t.Fatalf("execs %+v", h.Execs)
	}
	if !strings.Contains(h.ErrOut.String(), "Starting ccy --teams 3@alpha in "+h.Checkout()+".\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
	header := h.HeaderOf(0)
	if !strings.Contains(header, "Enter start   Esc or q back") ||
		!strings.Contains(header, "Runs ccy --teams <seat>@alpha in "+h.Checkout()+"; a new seat is created by that launch (sudo).") {
		t.Fatalf("header %q", header)
	}
}

func TestNewSessionTakesAFreeSeat(t *testing.T) {
	h := newSessionHarness(t)
	h.Script.Fzf = []string{"|free"}
	snap := h.pass(t)
	if _, err := h.s.NewSession(context.Background(), snap, "alpha"); err != nil && !strings.Contains(err.Error(), "exit") {
		t.Fatal(err)
	}
	if len(h.Execs) != 1 || !slices.Equal(h.Execs[0].Argv, []string{"/x/launcher", "--teams", "dev@alpha"}) {
		t.Fatalf("execs %+v", h.Execs)
	}
}

func TestNewSessionNumbersAfterThisRepositoryOnly(t *testing.T) {
	h := newSessionHarness(t)
	h.On(teamtest.SudoBus+"list alpha", teamtest.BusList+"MEMBER\tother.7+local.podman\tdev\tjoin\tactive\nMEMBER\tproj.9+local.lxc\tdev\tjoin\tactive\n", 0)
	h.Script.Fzf = []string{"|new"}
	snap := h.pass(t)
	if _, err := h.s.NewSession(context.Background(), snap, "alpha"); err != nil && !strings.Contains(err.Error(), "exit") {
		t.Fatal(err)
	}
	if !slices.Contains(h.Script.Rows[0], "Seat 3@alpha   new\t3@alpha") {
		t.Fatalf("rows %q: another repository's seat 7 must not count", h.Script.Rows[0])
	}
}

func TestNewSessionCancelledStartsNothing(t *testing.T) {
	h := newSessionHarness(t)
	h.Script.Fzf = []string{"|ABORT"}
	snap := h.pass(t)
	res, err := h.s.NewSession(context.Background(), snap, "alpha")
	if err != nil || res != flow.Done || len(h.Execs) != 0 {
		t.Fatalf("%v %v %v", res, err, h.Execs)
	}
}

func TestNewSessionNeverStartsInAContainersCheckout(t *testing.T) {
	h := newRig(t)
	top := h.InLxcCheckout()
	snap := h.pass(t)
	res, err := h.s.NewSession(context.Background(), snap, "alpha")
	if err != nil || res != flow.Refused || len(h.Execs) != 0 {
		t.Fatalf("%v %v %+v", res, err, h.Execs)
	}
	if !strings.Contains(h.ErrOut.String(), top+" is LXC container "+teamtest.LxcContainer+"'s checkout") ||
		!strings.Contains(h.ErrOut.String(), "ccy-teams lxc-add") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
	if len(h.Script.Rows) != 0 {
		t.Fatalf("seats were offered: %q", h.Script.Rows)
	}
}

func TestNewSessionRefusesWhenItCannotTellWhoseCheckoutItIs(t *testing.T) {
	h := newRig(t)
	h.InLxcCheckout()
	h.OnErr("sudo -n lxc-config lxc.lxcpath", "sudo: a password is required\n", 1)
	snap := h.pass(t)
	res, err := h.s.NewSession(context.Background(), snap, "alpha")
	if err != nil || res != flow.Refused || len(h.Execs) != 0 || !strings.Contains(h.ErrOut.String(), "password is required") {
		t.Fatalf("%v %v %+v %q", res, err, h.Execs, h.ErrOut.String())
	}
}

func TestNewSessionRefusals(t *testing.T) {
	h := newRig(t)
	snap := h.pass(t)
	res, err := h.s.NewSession(context.Background(), snap, "alpha")
	if err != nil || res != flow.Refused || !strings.Contains(h.ErrOut.String(),
		h.Root+" is not a project folder (no git repository), so ccy cannot be started here.\nPress Enter to go back to the list.\n") {
		t.Fatalf("%v %v %q", res, err, h.ErrOut.String())
	}

	h = newSessionHarness(t)
	h.On("systemctl is-active agent-bus-hs@alpha.service", "inactive\n", 3)
	snap = h.pass(t)
	res, err = h.s.NewSession(context.Background(), snap, "alpha")
	if err != nil || res != flow.Refused || !strings.Contains(h.ErrOut.String(), "alpha's homeserver is down, so no session can join it.\n") {
		t.Fatalf("%v %v %q", res, err, h.ErrOut.String())
	}

	h = newSessionHarness(t)
	snap = h.pass(t)
	h.Fake.On([]string{"agent-bus", "seat", "list"}, execx.Result{
		Stdout: []byte("SEAT\talpha\t1\theld\tself\tproj.1+local.podman\n"), ExitCode: 78,
	})
	res, err = h.s.NewSession(context.Background(), snap, "alpha")
	if err != nil || res != flow.Refused || !strings.Contains(h.ErrOut.String(),
		"Not every seat of this checkout could be read (above), so no seat is offered: one of them may be the seat a new session would take.\n") {
		t.Fatalf("%v %v %q", res, err, h.ErrOut.String())
	}
	if len(h.Script.Rows) != 0 {
		t.Fatal("no seat is offered")
	}
}

func TestToggleRestoreFlipsTheRecordAndKeepsItsSeats(t *testing.T) {
	h := newRig(t)
	snap := h.pass(t)
	res, err := h.s.ToggleRestore(context.Background(), snap, h.row(t, snap, "1"))
	if err != nil || res != flow.Done {
		t.Fatalf("%v %v", res, err)
	}
	store, err := h.s.Registry()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := store.Get("ccy-proj")
	if err != nil || rec.Restore || !slices.Equal(rec.Args, []string{"--teams", "1@alpha"}) {
		t.Fatalf("record %+v %v", rec, err)
	}
	if !strings.Contains(h.ErrOut.String(), "Session ccy-proj will not be started again after a reboot.\nPress Enter to go back to the list.\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}

	snap = h.pass(t)
	if _, err = h.s.ToggleRestore(context.Background(), snap, h.row(t, snap, "1")); err != nil {
		t.Fatal(err)
	}
	rec, err = store.Get("ccy-proj")
	if err != nil || !rec.Restore {
		t.Fatalf("record %+v %v", rec, err)
	}
	if !strings.Contains(h.ErrOut.String(), "Session ccy-proj will be started again after a reboot.\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestToggleRestoreOnARowWithNoSessionSaysSo(t *testing.T) {
	h := newRig(t)
	snap := h.pass(t)
	if _, err := h.s.ToggleRestore(context.Background(), snap, h.row(t, snap, "proj.box+role.lxc")); err != nil {
		t.Fatal(err)
	}
	want := "Ctrl-R changes whether a seat's ccy session comes back after a reboot, and this row has no session.\nPress Enter to go back to the list.\n"
	if !strings.Contains(h.ErrOut.String(), want) {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestToggleRestoreReportsARecordThatIsGone(t *testing.T) {
	h := newRig(t)
	snap := h.pass(t)
	store, err := h.s.Registry()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Remove("ccy-proj"); err != nil {
		t.Fatal(err)
	}
	row := h.row(t, snap, "1")
	row.Restore = "yes"
	snap.SeatRestore[row.Dir+"|1@alpha"] = true
	if _, err := h.s.ToggleRestore(context.Background(), snap, row); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.ErrOut.String(), "ERROR: session ccy-proj has no record in") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestToggleRestoreReportsAnUnlocatableRegistry(t *testing.T) {
	h := newRig(t)
	snap := h.pass(t)
	h.s.Registry = func() (*registry.Store, error) { return nil, &registry.RelativeBaseError{Value: "x"} }
	if _, err := h.s.ToggleRestore(context.Background(), snap, h.row(t, snap, "1")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.ErrOut.String(), "ERROR: session registry base 'x' is not an absolute path.") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestRemoveSeatAsksThenRemovesInItsCheckout(t *testing.T) {
	h := newRig(t)
	h.On("agent-bus seat remove", "", 0)
	h.Script.Fzf = []string{"|YES"}
	snap := h.pass(t)
	res, err := h.s.RemoveOrPark(context.Background(), snap, h.row(t, snap, "1"))
	if err != nil || res != flow.Done {
		t.Fatalf("%v %v", res, err)
	}
	var remove execx.Cmd
	for _, c := range h.Fake.Calls() {
		if strings.HasPrefix(c.Line(), "agent-bus seat remove") {
			remove = c
		}
	}
	if remove.Line() != "agent-bus seat remove 1@alpha" || remove.Dir != h.Checkout() {
		t.Fatalf("remove %+v", remove)
	}
	if !strings.Contains(h.HeaderOf(0), "proj.1+local.podman is parked (its token revoked; its history kept) and the seat's directory in "+h.Checkout()+" deleted. Launching into 1@alpha again brings it back.") {
		t.Fatalf("header %q", h.HeaderOf(0))
	}
}

func TestRemoveSeatNoRemovesNothingAndFailureIsSaid(t *testing.T) {
	h := newRig(t)
	h.Script.Fzf = []string{"|EXIT"}
	snap := h.pass(t)
	if _, err := h.s.RemoveOrPark(context.Background(), snap, h.row(t, snap, "1")); err != nil {
		t.Fatal(err)
	}
	if h.Fake.Called("agent-bus seat remove") {
		t.Fatal("nothing is removed on a no")
	}

	h = newRig(t)
	h.OnErr("agent-bus seat remove", "seat is busy", 1)
	h.Script.Fzf = []string{"|YES"}
	snap = h.pass(t)
	if _, err := h.s.RemoveOrPark(context.Background(), snap, h.row(t, snap, "1")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.ErrOut.String(), "The seat was not removed, for the reason above.\nPress Enter to go back to the list.\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestParkAMemberNeedsSudoThenParks(t *testing.T) {
	h := newRig(t)
	h.On("sudo -n -v", "", 0)
	h.On(teamtest.SudoBus+"park-member alpha proj.box+role.lxc", "", 0)
	h.Script.Fzf = []string{"|YES"}
	snap := h.pass(t)
	res, err := h.s.RemoveOrPark(context.Background(), snap, h.row(t, snap, "proj.box+role.lxc"))
	if err != nil || res != flow.Done || !h.Fake.Called(teamtest.SudoBus+"park-member alpha proj.box+role.lxc") {
		t.Fatalf("%v %v %v", res, err, h.Fake.Calls())
	}
	if !strings.Contains(h.HeaderOf(0), "Its token is revoked; its account, role and room are kept.") {
		t.Fatalf("header %q", h.HeaderOf(0))
	}
}

func TestParkAMemberWhoseCheckoutIsUnknown(t *testing.T) {
	h := newRig(t)
	h.On("sudo -n -v", "", 0)
	h.On(teamtest.SudoBus+"park-member alpha proj.2+local.podman", "", 0)
	h.Script.Fzf = []string{"|YES"}
	snap := h.pass(t)
	if _, err := h.s.RemoveOrPark(context.Background(), snap, h.row(t, snap, "proj.2+local.podman")); err != nil {
		t.Fatal(err)
	}
	if !h.Fake.Called(teamtest.SudoBus + "park-member alpha proj.2+local.podman") {
		t.Fatalf("calls %v", h.Fake.Calls())
	}
}

func TestParkRefusalsAreSaid(t *testing.T) {
	h := newRig(t)
	h.OnErr("sudo -n -v", "sudo: a password is required", 1)
	h.OnErr("sudo -v", "sudo: no", 1)
	h.Script.Fzf = []string{"|YES"}
	snap := h.pass(t)
	if _, err := h.s.RemoveOrPark(context.Background(), snap, h.row(t, snap, "proj.box+role.lxc")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.ErrOut.String(), "proj.box+role.lxc was not parked.\n") || h.Fake.Called(teamtest.SudoBus+"park-member") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}

	h = newRig(t)
	h.On("sudo -n -v", "", 0)
	h.OnErr(teamtest.SudoBus+"park-member", "no such member", 1)
	h.Script.Fzf = []string{"|YES"}
	snap = h.pass(t)
	if _, err := h.s.RemoveOrPark(context.Background(), snap, h.row(t, snap, "proj.box+role.lxc")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.ErrOut.String(), "proj.box+role.lxc was not parked, for the reason above.\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestRemoveOrParkOnAHumanAndOnATeamAndOnAnUnreadableSeat(t *testing.T) {
	h := newRig(t)
	snap := h.pass(t)
	if _, err := h.s.RemoveOrPark(context.Background(), snap, h.row(t, snap, "owner")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.ErrOut.String(), "A team's people are its agent_bus_teams entry's humans: change them there, then ccy-teams apply alpha.\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}

	h = newRig(t)
	snap = h.pass(t)
	// a team that cannot be read cannot be taken down: remove answers exit 1
	h.OnErr(h.DeclarePrefix("overview"), "declare: no", 78)
	if _, err := h.s.RemoveOrPark(context.Background(), snap, h.row(t, snap, board.NoDescription)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.ErrOut.String(), "(ccy-teams remove alpha failed, exit 1: see above.)\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}

	h = newRig(t)
	snap = h.pass(t)
	snap.Incomplete = "a seat is unreadable"
	if _, err := h.s.RemoveOrPark(context.Background(), snap, h.row(t, snap, "proj.2+local.podman")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.ErrOut.String(), "proj.2+local.podman may be one of the seats that could not be read (above), so it is not parked here: remove it with agent-bus seat remove in its checkout.\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
	if len(h.Script.Rows) != 0 {
		t.Fatal("nothing is asked")
	}
}

func TestRemoveOrParkCancelledRemoveEndsWith130(t *testing.T) {
	h := newRig(t)
	// Ctrl-C at the first question of the take-down
	h.Script.Gum = []string{"YES", "YES", "CANCEL"}
	snap := h.pass(t)
	_, err := h.s.RemoveOrPark(context.Background(), snap, h.row(t, snap, board.NoDescription))
	if code, ok := flow.ExitCodeOf(err); !ok || code != term.ExitCancelled {
		t.Fatalf("%v", err)
	}
}

func TestStartCcyWithNoSeatsRunsABareTeamsLaunch(t *testing.T) {
	h := newRig(t)
	err := h.s.StartCcy(h.Checkout(), "")
	if code, ok := flow.ExitCodeOf(err); !ok || code != term.ExitOK {
		t.Fatalf("%v", err)
	}
	if len(h.Execs) != 1 || !slices.Equal(h.Execs[0].Argv, []string{"/x/launcher", "--teams"}) {
		t.Fatalf("execs %+v", h.Execs)
	}
}
