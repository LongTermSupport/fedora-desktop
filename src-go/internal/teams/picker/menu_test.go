package picker

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// menuRows are the rows of the menu fzf call n was shown, without the Back row.
func (h rig) menuRows(n int) []string {
	rows := h.Script.Rows[n]
	shown := make([]string, len(rows)-1)
	for i, row := range rows[:len(rows)-1] {
		shown[i] = strings.SplitN(row, "\t", 2)[0]
	}
	return shown
}

func TestEnterOnAPersonWhoHasNeverSignedInOffersSigningInFirst(t *testing.T) {
	h := newRig(t)
	h.Script.Fzf = []string{"|owner", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	rows := h.menuRows(1)
	if len(rows) == 0 || rows[0] != "Sign in: set a password (shown once)   (Ctrl-P)" {
		t.Fatalf("menu %q", rows)
	}
	if !slices.Contains(rows, "Open alpha in Element   (Ctrl-E)") {
		t.Fatalf("menu %q", rows)
	}
	if last := h.Script.Rows[1][len(h.Script.Rows[1])-1]; last != "Back to the list" {
		t.Fatalf("the menu ends with %q", last)
	}
	if h.Fake.Called(teamtest.SudoBus + "human password") {
		t.Fatal("Esc in the menu changes nothing")
	}
}

func TestEnterOnAPersonWithSessionsOffersElementFirstAndNamesWhatAPasswordEnds(t *testing.T) {
	h := newRig(t)
	h.Script.Fzf = []string{"|guest", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	rows := h.menuRows(1)
	want := []string{
		"Open alpha in Element   (Ctrl-E)",
		"New password — signs out guest's 2 Element sessions   (Ctrl-P)",
	}
	if len(rows) < 2 || !slices.Equal(rows[:2], want) {
		t.Fatalf("menu %q", rows)
	}
}

func TestATeamsMenuHeaderShowsItsWholeDescription(t *testing.T) {
	h := newRig(t)
	long := "Coordinates the release of every example service across all regions"
	h.OnDeclare("declared", "TEAM\talpha\t6167\tpresent\talpha.agent-bus.internal\thttp://192.0.2.10:6167\t\""+long+"\"\n")
	h.Script.Fzf = []string{"|Coordinates the release", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if !strings.Contains(h.HeaderOf(1), "What to do with team alpha:") || !strings.Contains(h.HeaderOf(1), long) {
		t.Fatalf("header %q", h.HeaderOf(1))
	}
}

func TestATeamsMenuOpensEveryTeamInElement(t *testing.T) {
	h := newRig(t)
	h.Script.Fzf = []string{"|alpha", "|Open every team in Element", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if rows := h.menuRows(1); !slices.Contains(rows, "Open every team in Element, a window each") {
		t.Fatalf("menu %q", rows)
	}
	if !strings.Contains(h.ErrOut.String(), "alpha has no Element profile on this desktop yet") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestEscInTheMenuGoesBackToTheListAndNothingIsAsked(t *testing.T) {
	h := newRig(t)
	h.Script.Fzf = []string{"|guest", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if len(h.Script.Rows) != 3 {
		t.Fatalf("list, menu, list again: drawn %d times", len(h.Script.Rows))
	}
	if strings.Contains(h.ErrOut.String(), "Press Enter") {
		t.Fatalf("nothing was done, so nothing is held on screen: %q", h.ErrOut.String())
	}
}

func TestOpeningElementFromTheMenuStartsItApartAndKeepsTheBoard(t *testing.T) {
	h := newRig(t)
	teamtest.MustWrite(t, h.o.ElementLauncher("alpha"), "[Desktop Entry]\n")
	h.On("systemd-run", "", 0)
	h.On(teamtest.SudoBus+"human devices alpha owner", "DEVICE\ta\t-\n", 0)
	h.Script.Fzf = []string{"|guest", "|Open alpha in Element", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if !h.Fake.Called("systemd-run") {
		t.Fatal("Element was not started")
	}
	if len(h.Script.Rows) != 3 {
		t.Fatalf("the board is drawn again once Element has started: %d draws", len(h.Script.Rows))
	}
	if !strings.Contains(h.ErrOut.String(), "Element is starting; its own messages go to ") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestNewPasswordFromTheMenuAsksFirstAndDefaultsToNo(t *testing.T) {
	h := newRig(t)
	h.Script.Fzf = []string{"|guest", "|New password", "|EXIT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if !strings.Contains(h.HeaderOf(2), "This signs out guest's 2 Element sessions.") ||
		!slices.Contains(h.Script.FzfArgs[2], "--bind=start:last") {
		t.Fatalf("header %q", h.HeaderOf(2))
	}
	if h.Fake.Called(teamtest.SudoBus + "human password") {
		t.Fatal("declined, so no password was set")
	}
}

func TestEveryKeyIsInTheMenuOfTheRowsItActsOn(t *testing.T) {
	h := newRig(t)
	r := h.run1(t)
	seen := map[string]bool{}
	for _, row := range r.snap.Rows {
		keys := map[string]bool{}
		for _, a := range r.actionsFor(row) {
			keys[a.key] = true
			seen[a.key] = true
			if a.label == "" || a.short == "" {
				t.Fatalf("an action with no words: %+v", a)
			}
		}
		if !keys["ctrl-e"] || !keys["ctrl-n"] || !keys["?"] {
			t.Fatalf("every row can open Element, start a session and show help: %s %v", row.Member, keys)
		}
	}
	for _, key := range pickerKeys {
		if !seen[key] {
			t.Fatalf("%s is in no row's menu", key)
		}
	}
	team := h.row(t, r.snap, board.NoDescription)
	if labels := h.labels(r, team); !slices.Contains(labels, "Take alpha down   (Ctrl-X)") {
		t.Fatalf("team menu %q", labels)
	}
	seat := h.row(t, r.snap, "dev")
	if !slices.Contains(h.labels(r, seat), "Attach this terminal to the seat's session") {
		t.Fatalf("seat menu %q", h.labels(r, seat))
	}
}

// labels are the menu lines of row.
func (h rig) labels(r *run, row board.Row) []string {
	acts := r.actionsFor(row)
	out := make([]string, len(acts))
	for i, a := range acts {
		out[i] = a.line()
	}
	return out
}

func TestTheFooterFollowsTheRowUnderTheCursorInWords(t *testing.T) {
	h := newRig(t)
	h.Script.Fzf = []string{"|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	args := h.Script.FzfArgs[0]
	i := slices.IndexFunc(args, func(a string) bool { return strings.HasPrefix(a, "--bind=focus:transform-header^") })
	if i < 0 {
		t.Fatalf("no binding that changes the footer with the row: %q", args)
	}
	bind := args[i]
	for _, want := range []string{
		"This row: Ctrl-E open alpha in Element", "Ctrl-P new password", "Ctrl-P set a password",
		"Ctrl-X take alpha down", "attach its session via Enter", "Enter lists what can be done to the row",
	} {
		if !strings.Contains(bind+h.HeaderOf(0), want) {
			t.Fatalf("footer lacks %q: %q", want, bind)
		}
	}
	if !strings.Contains(h.HeaderOf(0), "This row: ") || !strings.Contains(h.HeaderOf(0), "TEAM ") {
		t.Fatalf("the first row's footer and the column heading are shown at once: %q", h.HeaderOf(0))
	}
}

func TestASeatsMenuOffersMakingItTheCoordinator(t *testing.T) {
	h := newRig(t)
	r := h.run1(t)
	labels := h.labels(r, h.row(t, r.snap, "1"))
	if !slices.Contains(labels, "Make coordinator") {
		t.Fatalf("seat menu %q", labels)
	}
	if slices.Contains(labels, "Stop being coordinator") {
		t.Fatalf("a seat that is not the coordinator offers to stop being it: %q", labels)
	}
	footer := r.footerFor(h.row(t, r.snap, "1"))
	if !strings.Contains(footer, "make it the coordinator via Enter") {
		t.Fatalf("footer %q", footer)
	}
}

func TestTheCoordinatorsSeatMenuOffersToStopBeingIt(t *testing.T) {
	h := newRig(t)
	h.On(teamtest.SudoBus+"list alpha", strings.Replace(teamtest.BusList,
		"MEMBER\tproj.1+local.podman\tdev", "MEMBER\tproj.1+local.podman\tcoordinator", 1), 0)
	r := h.run1(t)
	labels := h.labels(r, h.row(t, r.snap, "1"))
	if !slices.Contains(labels, "Stop being coordinator") || slices.Contains(labels, "Make coordinator") {
		t.Fatalf("seat menu %q", labels)
	}
}

func TestChoosingMakeCoordinatorChangesTheRoleThroughSudo(t *testing.T) {
	h := newRig(t)
	h.On("sudo -n -v", "", 0)
	h.On(teamtest.SudoBus+"set-role alpha", "", 0)
	h.Script.Fzf = []string{"|YES"}
	r := h.run1(t)
	if _, err := r.do(context.Background(), keyCoordinator, h.row(t, r.snap, "1")); err != nil {
		t.Fatal(err)
	}
	if !h.Fake.Called(teamtest.SudoBus + "set-role alpha proj.1+local.podman --role=coordinator") {
		t.Fatalf("calls %v", h.Fake.Calls())
	}
}

func TestEnterOnAMemberThatIsNotACcySeatOffersParkingAndTheRest(t *testing.T) {
	h := newRig(t)
	r := h.run1(t)
	labels := h.labels(r, h.row(t, r.snap, "proj.box+role.lxc"))
	for _, want := range []string{"Park proj.box+role.lxc   (Ctrl-X)", "Open alpha in Element   (Ctrl-E)"} {
		if !slices.Contains(labels, want) {
			t.Fatalf("menu %q lacks %q", labels, want)
		}
	}
}
