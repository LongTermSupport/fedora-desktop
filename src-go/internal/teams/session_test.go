package teams

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// pass is one collection of the world, and the picker state the key handlers act on.
func (h *pickerHarness) pass(t *testing.T) *pickerRun {
	t.Helper()
	s, err := h.app.collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return &pickerRun{a: h.app, snap: s}
}

// row is the row of member in the pass.
func (r *pickerRun) row(t *testing.T, member string) Row {
	t.Helper()
	for _, row := range r.snap.Rows {
		if row.Member == member {
			return row
		}
	}
	t.Fatalf("no row for %s", member)
	return Row{}
}

func (h *pickerHarness) attachable() {
	h.on("tmux -L ccy list-clients", "", 0)
	h.on("tmux -L ccy set-hook", "", 0)
	h.on("tmux -L ccy attach-session", "", 0)
}

func TestAttachSeatAttachesALiveSessionAndEndsThePicker(t *testing.T) {
	h := newPickerHarness(t)
	h.attachable()
	r := h.pass(t)
	res, err := r.attachSeat(context.Background(), r.row(t, "1"))
	if code, ok := exitCodeOf(err); !ok || code != 0 || res != done {
		t.Fatalf("%v %v %v", res, err, h.errOut.String())
	}
	if !h.fake.Called("tmux -L ccy attach-session -t =ccy-proj") {
		t.Fatalf("calls %v", h.fake.Calls())
	}
}

func TestAttachSeatRefusesWhatCannotBeAttached(t *testing.T) {
	cases := map[string]string{
		"dev":                 "Seat dev@alpha has no running session to attach. Ctrl-N starts one.\n",
		"proj.2+local.podman": "proj.2+local.podman is a ccy member whose checkout is not known here, so there is no session to attach.\n",
	}
	for member, want := range cases {
		h := newPickerHarness(t)
		r := h.pass(t)
		res, err := r.attachSeat(context.Background(), r.row(t, member))
		if err != nil || res != refused {
			t.Fatalf("%s: %v %v", member, res, err)
		}
		if !strings.Contains(h.errOut.String(), want+"Press Enter to go back to the list.\n") {
			t.Fatalf("%s: stderr %q", member, h.errOut.String())
		}
	}

	h := newPickerHarness(t)
	h.on("tmux -L ccy list-sessions", "ccy-proj 1 "+h.checkout()+"\n", 0)
	r := h.pass(t)
	res, err := r.attachSeat(context.Background(), r.row(t, "1"))
	if err != nil || res != refused {
		t.Fatalf("%v %v", res, err)
	}
	if !strings.Contains(h.errOut.String(), "'ccy-proj' is open in another terminal. ccy-sessions takes it over (Ctrl-T).\n") ||
		h.fake.Called("tmux -L ccy attach-session") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestAttachSeatSaysSoWhenTheSessionWasTakenMeanwhile(t *testing.T) {
	h := newPickerHarness(t)
	h.on("tmux -L ccy list-clients", "/dev/pts/9 100\n", 0)
	r := h.pass(t)
	res, err := r.attachSeat(context.Background(), r.row(t, "1"))
	if err != nil || res != refused {
		t.Fatalf("%v %v", res, err)
	}
	for _, want := range []string{
		"ERROR: 'ccy-proj' is open in another terminal, or gone. It can be attached from one terminal only.\n",
		"'ccy-proj' was taken or ended just now — the list has been refreshed.\n",
	} {
		if !strings.Contains(h.errOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.errOut.String(), want)
		}
	}
}

func newSessionHarness(t *testing.T) *pickerHarness {
	t.Helper()
	h := newPickerHarness(t)
	h.inProject()
	return h
}

func TestNewSessionOffersFreeSeatsAndANewOneAndExecsTheLauncherInTheTop(t *testing.T) {
	h := newSessionHarness(t)
	h.script.fzf = []string{"|new"}
	r := h.pass(t)
	res, err := r.newSession(context.Background(), "alpha")
	if code, ok := exitCodeOf(err); !ok || code != 0 || res != done {
		t.Fatalf("%v %v: %s", res, err, h.errOut.String())
	}
	if want := []string{"Seat dev@alpha   free", "Seat 3@alpha   new", "Exit"}; !slices.Equal(h.script.rows[0], want) {
		t.Fatalf("rows %q", h.script.rows[0])
	}
	if len(h.execs) != 1 || h.execs[0].dir != h.checkout() || h.execs[0].path != "/x/launcher" ||
		!slices.Equal(h.execs[0].argv, []string{"/x/launcher", "--teams", "3@alpha"}) {
		t.Fatalf("execs %+v", h.execs)
	}
	if !strings.Contains(h.errOut.String(), "Starting ccy --teams 3@alpha in "+h.checkout()+".\n") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
	header := h.headerOf(0)
	if !strings.Contains(header, "Enter start   Esc or q back") ||
		!strings.Contains(header, "Runs ccy --teams <seat>@alpha in "+h.checkout()+"; a new seat is created by that launch (sudo).") {
		t.Fatalf("header %q", header)
	}
}

func TestNewSessionTakesAFreeSeat(t *testing.T) {
	h := newSessionHarness(t)
	h.script.fzf = []string{"|free"}
	r := h.pass(t)
	if _, err := r.newSession(context.Background(), "alpha"); err != nil && !strings.Contains(err.Error(), "exit") {
		t.Fatal(err)
	}
	if len(h.execs) != 1 || !slices.Equal(h.execs[0].argv, []string{"/x/launcher", "--teams", "dev@alpha"}) {
		t.Fatalf("execs %+v", h.execs)
	}
}

func TestNewSessionNumbersAfterThisRepositoryOnly(t *testing.T) {
	h := newSessionHarness(t)
	h.on(sudoBus+"list alpha", busList+"MEMBER\tother.7+local.podman\tdev\tjoin\tactive\nMEMBER\tproj.9+local.lxc\tdev\tjoin\tactive\n", 0)
	h.script.fzf = []string{"|new"}
	r := h.pass(t)
	if _, err := r.newSession(context.Background(), "alpha"); err != nil && !strings.Contains(err.Error(), "exit") {
		t.Fatal(err)
	}
	if !slices.Contains(h.script.rows[0], "Seat 3@alpha   new") {
		t.Fatalf("rows %q: another repository's seat 7 must not count", h.script.rows[0])
	}
}

func TestNewSessionCancelledStartsNothing(t *testing.T) {
	h := newSessionHarness(t)
	h.script.fzf = []string{"|ABORT"}
	r := h.pass(t)
	res, err := r.newSession(context.Background(), "alpha")
	if err != nil || res != done || len(h.execs) != 0 {
		t.Fatalf("%v %v %v", res, err, h.execs)
	}
}

func TestNewSessionRefusals(t *testing.T) {
	h := newPickerHarness(t)
	r := h.pass(t)
	res, err := r.newSession(context.Background(), "alpha")
	if err != nil || res != refused || !strings.Contains(h.errOut.String(),
		h.root+" is not a project folder (no git repository), so ccy cannot be started here.\nPress Enter to go back to the list.\n") {
		t.Fatalf("%v %v %q", res, err, h.errOut.String())
	}

	h = newSessionHarness(t)
	h.on("systemctl is-active agent-bus-hs@alpha.service", "inactive\n", 3)
	r = h.pass(t)
	res, err = r.newSession(context.Background(), "alpha")
	if err != nil || res != refused || !strings.Contains(h.errOut.String(), "alpha's homeserver is down, so no session can join it.\n") {
		t.Fatalf("%v %v %q", res, err, h.errOut.String())
	}

	h = newSessionHarness(t)
	r = h.pass(t)
	h.fake.On([]string{"agent-bus", "seat", "list"}, execx.Result{
		Stdout: []byte("SEAT\talpha\t1\theld\tself\tproj.1+local.podman\n"), ExitCode: 78,
	})
	res, err = r.newSession(context.Background(), "alpha")
	if err != nil || res != refused || !strings.Contains(h.errOut.String(),
		"Not every seat of this checkout could be read (above), so no seat is offered: one of them may be the seat a new session would take.\n") {
		t.Fatalf("%v %v %q", res, err, h.errOut.String())
	}
	if len(h.script.rows) != 0 {
		t.Fatal("no seat is offered")
	}
}

func TestToggleRestoreFlipsTheRecordAndKeepsItsSeats(t *testing.T) {
	h := newPickerHarness(t)
	r := h.pass(t)
	res, err := r.toggleRestore(context.Background(), r.row(t, "1"))
	if err != nil || res != done {
		t.Fatalf("%v %v", res, err)
	}
	store, err := h.app.Registry()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := store.Get("ccy-proj")
	if err != nil || rec.Restore || !slices.Equal(rec.Args, []string{"--teams", "1@alpha"}) {
		t.Fatalf("record %+v %v", rec, err)
	}
	if !strings.Contains(h.errOut.String(), "Session ccy-proj will not be started again after a reboot.\nPress Enter to go back to the list.\n") {
		t.Fatalf("stderr %q", h.errOut.String())
	}

	r = h.pass(t)
	if _, err = r.toggleRestore(context.Background(), r.row(t, "1")); err != nil {
		t.Fatal(err)
	}
	rec, err = store.Get("ccy-proj")
	if err != nil || !rec.Restore {
		t.Fatalf("record %+v %v", rec, err)
	}
	if !strings.Contains(h.errOut.String(), "Session ccy-proj will be started again after a reboot.\n") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestToggleRestoreOnARowWithNoSessionSaysSo(t *testing.T) {
	h := newPickerHarness(t)
	r := h.pass(t)
	if _, err := r.toggleRestore(context.Background(), r.row(t, "proj.box+role.lxc")); err != nil {
		t.Fatal(err)
	}
	want := "Ctrl-R changes whether a seat's ccy session comes back after a reboot, and this row has no session.\nPress Enter to go back to the list.\n"
	if !strings.Contains(h.errOut.String(), want) {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestToggleRestoreReportsARecordThatIsGone(t *testing.T) {
	h := newPickerHarness(t)
	r := h.pass(t)
	store, err := h.app.Registry()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Remove("ccy-proj"); err != nil {
		t.Fatal(err)
	}
	row := r.row(t, "1")
	row.Restore = "yes"
	r.snap.SeatRestore[row.Dir+"|1@alpha"] = "yes"
	if _, err := r.toggleRestore(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.errOut.String(), "ERROR: session ccy-proj has no record in") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestToggleRestoreReportsAnUnlocatableRegistry(t *testing.T) {
	h := newPickerHarness(t)
	r := h.pass(t)
	h.app.Registry = func() (*registry.Store, error) { return nil, &registry.RelativeBaseError{Value: "x"} }
	if _, err := r.toggleRestore(context.Background(), r.row(t, "1")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.errOut.String(), "ERROR: session registry base 'x' is not an absolute path.") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestRemoveSeatAsksThenRemovesInItsCheckout(t *testing.T) {
	h := newPickerHarness(t)
	h.on("agent-bus seat remove", "", 0)
	h.script.fzf = []string{"|YES"}
	r := h.pass(t)
	res, err := r.removeOrPark(context.Background(), r.row(t, "1"))
	if err != nil || res != done {
		t.Fatalf("%v %v", res, err)
	}
	var remove execx.Cmd
	for _, c := range h.fake.Calls() {
		if strings.HasPrefix(c.Line(), "agent-bus seat remove") {
			remove = c
		}
	}
	if remove.Line() != "agent-bus seat remove 1@alpha" || remove.Dir != h.checkout() {
		t.Fatalf("remove %+v", remove)
	}
	if !strings.Contains(h.headerOf(0), "proj.1+local.podman is parked (its token revoked; its history kept) and the seat's directory in "+h.checkout()+" deleted. Launching into 1@alpha again brings it back.") {
		t.Fatalf("header %q", h.headerOf(0))
	}
}

func TestRemoveSeatNoRemovesNothingAndFailureIsSaid(t *testing.T) {
	h := newPickerHarness(t)
	h.script.fzf = []string{"|EXIT"}
	r := h.pass(t)
	if _, err := r.removeOrPark(context.Background(), r.row(t, "1")); err != nil {
		t.Fatal(err)
	}
	if h.fake.Called("agent-bus seat remove") {
		t.Fatal("nothing is removed on a no")
	}

	h = newPickerHarness(t)
	h.onErr("agent-bus seat remove", "seat is busy", 1)
	h.script.fzf = []string{"|YES"}
	r = h.pass(t)
	if _, err := r.removeOrPark(context.Background(), r.row(t, "1")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.errOut.String(), "The seat was not removed, for the reason above.\nPress Enter to go back to the list.\n") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestParkAMemberNeedsSudoThenParks(t *testing.T) {
	h := newPickerHarness(t)
	h.on("sudo -n -v", "", 0)
	h.on(sudoBus+"park-member alpha proj.box+role.lxc", "", 0)
	h.script.fzf = []string{"|YES"}
	r := h.pass(t)
	res, err := r.removeOrPark(context.Background(), r.row(t, "proj.box+role.lxc"))
	if err != nil || res != done || !h.fake.Called(sudoBus+"park-member alpha proj.box+role.lxc") {
		t.Fatalf("%v %v %v", res, err, h.fake.Calls())
	}
	if !strings.Contains(h.headerOf(0), "Its token is revoked; its account, role and room are kept.") {
		t.Fatalf("header %q", h.headerOf(0))
	}
}

func TestParkAMemberWhoseCheckoutIsUnknown(t *testing.T) {
	h := newPickerHarness(t)
	h.on("sudo -n -v", "", 0)
	h.on(sudoBus+"park-member alpha proj.2+local.podman", "", 0)
	h.script.fzf = []string{"|YES"}
	r := h.pass(t)
	if _, err := r.removeOrPark(context.Background(), r.row(t, "proj.2+local.podman")); err != nil {
		t.Fatal(err)
	}
	if !h.fake.Called(sudoBus + "park-member alpha proj.2+local.podman") {
		t.Fatalf("calls %v", h.fake.Calls())
	}
}

func TestParkRefusalsAreSaid(t *testing.T) {
	h := newPickerHarness(t)
	h.onErr("sudo -n -v", "sudo: a password is required", 1)
	h.onErr("sudo -v", "sudo: no", 1)
	h.script.fzf = []string{"|YES"}
	r := h.pass(t)
	if _, err := r.removeOrPark(context.Background(), r.row(t, "proj.box+role.lxc")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.errOut.String(), "proj.box+role.lxc was not parked.\n") || h.fake.Called(sudoBus+"park-member") {
		t.Fatalf("stderr %q", h.errOut.String())
	}

	h = newPickerHarness(t)
	h.on("sudo -n -v", "", 0)
	h.onErr(sudoBus+"park-member", "no such member", 1)
	h.script.fzf = []string{"|YES"}
	r = h.pass(t)
	if _, err := r.removeOrPark(context.Background(), r.row(t, "proj.box+role.lxc")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.errOut.String(), "proj.box+role.lxc was not parked, for the reason above.\n") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestRemoveOrParkOnAHumanAndOnATeamAndOnAnUnreadableSeat(t *testing.T) {
	h := newPickerHarness(t)
	r := h.pass(t)
	if _, err := r.removeOrPark(context.Background(), r.row(t, "owner")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.errOut.String(), "A team's people are its agent_bus_teams entry's humans: change them there, then ccy-teams apply alpha.\n") {
		t.Fatalf("stderr %q", h.errOut.String())
	}

	h = newPickerHarness(t)
	h.ops.removeRC = 1
	r = h.pass(t)
	if _, err := r.removeOrPark(context.Background(), r.row(t, "-")); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(h.ops.calls, []string{"remove alpha"}) || !strings.Contains(h.errOut.String(), "(ccy-teams remove alpha failed, exit 1: see above.)\n") {
		t.Fatalf("ops %v stderr %q", h.ops.calls, h.errOut.String())
	}

	h = newPickerHarness(t)
	r = h.pass(t)
	r.snap.Incomplete = "a seat is unreadable"
	if _, err := r.removeOrPark(context.Background(), r.row(t, "proj.2+local.podman")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.errOut.String(), "proj.2+local.podman may be one of the seats that could not be read (above), so it is not parked here: remove it with agent-bus seat remove in its checkout.\n") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
	if len(h.script.rows) != 0 {
		t.Fatal("nothing is asked")
	}
}

func TestRemoveOrParkCancelledRemoveEndsWith130(t *testing.T) {
	h := newPickerHarness(t)
	h.ops.removeRC = term.ExitCancelled
	r := h.pass(t)
	_, err := r.removeOrPark(context.Background(), r.row(t, "-"))
	if code, ok := exitCodeOf(err); !ok || code != term.ExitCancelled {
		t.Fatalf("%v", err)
	}
}
