package chooser

import (
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const deltaWiden = "CHOICE\tdelta\twiden\t\"Ships the images\"\n"

const joinBeta = "CHOOSE:Join without adding (beta will treat " + repo + " references as external)"

// S-B3: the list is a multi-select screen and says how to mark.
func TestTheListIsMultiSelectAndSaysHowToMarkSeveral(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden)
	r.Script.Fzf = []string{"|alpha"}
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	if !slices.Contains(r.Script.FzfArgs[0], "--multi") {
		t.Fatalf("fzf args %v", r.Script.FzfArgs[0])
	}
	contains(t, "header", r.HeaderOf(0), "Tab marks several", "Enter takes them (or the highlighted one)", "Esc leaves")
}

// S-C1 and S-B12: two teams marked: a seat chosen for each, in list order, one canonical list.
func TestTwoMarkedTeamsGetASeatEachInListOrder(t *testing.T) {
	r := newRig(t, betaWiden+alphaCovers+deltaWiden)
	r.Script.Fzf = []string{"|beta&&alpha"}
	r.Script.Gum = []string{joinBeta}
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	if r.payload.String() != "1@alpha,1@beta\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
	if !slices.Equal(r.seatsAsked, []string{"alpha/podman", "beta/podman"}) {
		t.Fatalf("seats %v", r.seatsAsked)
	}
	// Each team's questions say which of the teams it is, so a seat is never chosen blind.
	if want := []string{"New session › Team 1 of 2: alpha", "New session › Team 2 of 2: beta"}; !slices.Equal(r.seatTrails, want) {
		t.Fatalf("seat headings %q, want %q", r.seatTrails, want)
	}
	r.Script.HeadingAt(0, "New session › Teams\nfor "+repo+" (this checkout)")
	r.Script.HeadingAt(1, "New session › Team 2 of 2: beta › Scope\nteam beta · does not cover "+repo+" (this checkout)")
}

// S-B9: every Add of one run is applied by one play run.
func TestTwoWideningTeamsAreAddedAndAppliedInOneRun(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden+deltaWiden)
	r.OnDeclare("add-repo", "CHANGED\tteam x trusts "+repo+"\n")
	r.Script.Fzf = []string{"|beta&&delta"}
	r.Script.Gum = []string{"CHOOSE:Add " + repo + " to beta's scope", "CHOOSE:Add " + repo + " to delta's scope"}
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	if len(r.applied) != 1 || !slices.Equal(r.applied[0], []string{"beta", "delta"}) {
		t.Fatalf("applied %v", r.applied)
	}
	if r.payload.String() != "1@beta,1@delta\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
	if len(r.Script.GumLog) != 2 {
		t.Fatalf("one scope question per widening team: %v", r.Script.GumLog)
	}
}

// S-B10: leaving one of two out keeps the other.
func TestLeavingOneOfTwoMarkedTeamsOutKeepsTheOther(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden)
	r.Script.Fzf = []string{"|alpha&&beta"}
	r.Script.Gum = []string{"CHOOSE:Leave beta out"}
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	if r.payload.String() != "1@alpha\n" || len(r.Script.Rows) != 1 {
		t.Fatalf("payload %q rounds %d", r.payload.String(), len(r.Script.Rows))
	}
}

// S-B6: a blocked mark is explained and the list comes back with the other marks kept.
func TestABlockedMarkIsExplainedAndTheOtherMarksAreKept(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden+gammaExclude)
	r.Script.Fzf = []string{"|alpha&&gamma", "|beta"}
	r.Script.Gum = []string{joinBeta}
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	contains(t, "stderr", r.stderr(), "gamma excludes "+repo+" on purpose", "Keeping alpha")
	if len(r.Script.Rows) != 2 {
		t.Fatalf("rounds %d", len(r.Script.Rows))
	}
	if strings.Contains(strings.Join(r.Script.Rows[1], "\n"), "alpha ") {
		t.Fatalf("a kept team is offered again: %q", r.Script.Rows[1])
	}
	if r.payload.String() != "1@alpha,1@beta\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
}

// An answer the owner gave is kept: a team left out at the scope question is not silently held for
// the next round.
func TestATeamLeftOutAfterAHeldRoundIsNotHeldAgain(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden+gammaExclude+deltaWiden)
	r.Script.Fzf = []string{"|beta&&gamma", "|delta", "|alpha"}
	r.Script.Gum = []string{"CHOOSE:Leave beta out", "CHOOSE:Leave delta out"}
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	if len(r.Script.Rows) != 3 {
		t.Fatalf("rounds %d", len(r.Script.Rows))
	}
	if !strings.Contains(strings.Join(r.Script.Rows[2], "\n"), "beta ") {
		t.Fatalf("beta, which was left out, is not offered again: %q", r.Script.Rows[2])
	}
	if r.payload.String() != "1@alpha\n" || len(r.Script.GumLog) != 2 {
		t.Fatalf("payload %q gum %v", r.payload.String(), r.Script.GumLog)
	}
}

func TestEscAfterMarksWereKeptTakesNothing(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden+gammaExclude)
	r.Script.Fzf = []string{"|alpha&&gamma", "|ABORT"}
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	contains(t, "stderr", r.stderr(), "No team chosen; nothing started.")
	r.lacksOutput(t)
	if r.seatsAsked != nil {
		t.Fatalf("seats %v", r.seatsAsked)
	}
}

func TestBlockedMarksThreeRoundsRunOutAndFailEvenWithKeptMarks(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden+gammaExclude)
	r.Script.Fzf = []string{"|alpha&&gamma", "|gamma", "|gamma"}
	exit(t, r.run("--type=podman"), term.ExitFailure, r)
	r.lacksOutput(t)
}

// S-B11: Create marked together with another team takes both.
func TestCreateMarkedWithATeamTakesBoth(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden)
	r.choicesAfterCreate("CHOICE\tfresh\tcovers\t\"New\"\n")
	r.created = "fresh"
	r.Script.Fzf = []string{"|alpha&&+ Create a team"}
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	if r.payload.String() != "1@alpha,1@fresh\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
}

func TestCancellingTheWizardKeepsTheOtherMarks(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden)
	r.createCode = term.ExitCancelled
	r.Script.Fzf = []string{"|alpha&&+ Create a team", "|beta"}
	r.Script.Gum = []string{joinBeta}
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	if r.payload.String() != "1@alpha,1@beta\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
}

// Leaving the seat chooser of any one team starts nothing for all of them.
func TestLeavingTheSeatChooserOfOneOfTwoTeamsStartsNothing(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden)
	r.seatLeft["beta"] = true
	r.Script.Fzf = []string{"|alpha&&beta"}
	r.Script.Gum = []string{joinBeta}
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	r.lacksOutput(t)
}

// The owner's feedback: with several teams, every question says which team it is about and where
// it stands in the sequence, and a team's lines sit under its own heading.
func TestEachTeamsQuestionsCarryItsHeadingAndPlaceInTheSequence(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden+deltaWiden)
	r.Script.Fzf = []string{"|alpha&&beta&&delta"}
	r.Script.Gum = []string{joinBeta, "CHOOSE:Join without adding (delta will treat " + repo + " references as external)"}
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	if want := []string{"New session › Team 1 of 3: alpha", "New session › Team 2 of 3: beta", "New session › Team 3 of 3: delta"}; !slices.Equal(r.seatTrails, want) {
		t.Fatalf("seat headings %q, want %q", r.seatTrails, want)
	}
	if len(r.Script.GumLog) != 2 {
		t.Fatalf("scope questions %q", r.Script.GumLog)
	}
	r.Script.HeadingAt(1, "New session › Team 2 of 3: beta › Scope\nteam beta · does not cover "+repo+" (this checkout)")
	r.Script.HeadingAt(2, "New session › Team 3 of 3: delta › Scope\nteam delta · does not cover "+repo+" (this checkout)")
	out := r.stderr()
	beta, note, delta := strings.Index(out, "New session › Team 2 of 3: beta › Scope\n"), strings.Index(out, "beta: "+repo+" references external"),
		strings.Index(out, "New session › Team 3 of 3: delta › Scope\n")
	if beta < 0 || note < beta || delta < note {
		t.Fatalf("beta's note is not under beta's heading, before delta's:\n%s", out)
	}
}

func TestAListOfOneTeamAloneIsHeadedByTheTeamAlone(t *testing.T) {
	r := newRig(t, alphaCovers)
	exit(t, r.run("--type=podman", "alpha"), term.ExitOK, r)
	if !slices.Equal(r.seatTrails, []string{"New session › Team alpha"}) {
		t.Fatalf("headings %q", r.seatTrails)
	}
	r = newRig(t, alphaCovers)
	exit(t, r.run("--type=podman", "alpha,2@beta,delta"), term.ExitOK, r)
	if !slices.Equal(r.seatTrails, []string{"New session › Team 1 of 3: alpha", "New session › Team 3 of 3: delta"}) {
		t.Fatalf("headings %q", r.seatTrails)
	}
}
