package chooser

import (
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// A team named on `ccy --teams` that this machine does not have leads into making it, or into
// choosing a team this machine has, or into starting without it; it never only ends the run.

const (
	unknownTeam   = "nosuch"
	createUnknown = "CHOOSE:Create team " + unknownTeam + " now"
	pickInstead   = "CHOOSE:Choose one of this machine's teams instead"
	cancelAll     = "CHOOSE:Cancel: start nothing"
)

// unknownRig is the rig with alpha and beta declared and nosuch on no homeserver.
func unknownRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t, alphaCovers+betaWiden)
	r.On("systemctl is-active agent-bus-hs@"+unknownTeam+".service", "inactive\n", 3)
	return r
}

// menu is the gum choose of the unknown-team question, as it was run.
func (r *rig) menu(t *testing.T) string {
	t.Helper()
	for _, line := range r.Script.GumLog {
		if strings.Contains(line, "Create team "+unknownTeam+" now") {
			return line
		}
	}
	t.Fatalf("no question about %s was asked: %v", unknownTeam, r.Script.GumLog)
	return ""
}

func TestAnUnknownTeamIsOfferedToBeCreatedAndTheTeamMadeIsTaken(t *testing.T) {
	r := unknownRig(t)
	r.created = unknownTeam
	r.Script.Gum = []string{createUnknown}
	exit(t, r.run("--type=podman", unknownTeam), term.ExitOK, r)
	if r.payload.String() != "1@"+unknownTeam+"\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
	if !slices.Equal(r.createNames, []string{unknownTeam}) {
		t.Fatalf("the wizard was given %q, want the name typed", r.createNames)
	}
	contains(t, "stderr", r.stderr(), "This machine has no team called "+unknownTeam+".")
	contains(t, "question", r.menu(t), "Choose one of this machine's teams instead", "Cancel: start nothing")
	if strings.Contains(r.menu(t), "Start on") {
		t.Fatalf("a team alone in the list has nothing to start without it: %s", r.menu(t))
	}
	if want := "New session › Team " + unknownTeam + " › Not on this machine"; r.Script.Headings[0] != want {
		t.Fatalf("heading %q, want %q", r.Script.Headings[0], want)
	}
}

// The person may change the name in the wizard: the team made is the one taken.
func TestTheTeamTheWizardMadeIsTakenUnderItsOwnName(t *testing.T) {
	r := unknownRig(t)
	r.created = "fresh"
	r.Script.Gum = []string{createUnknown}
	exit(t, r.run("--type=podman", unknownTeam), term.ExitOK, r)
	if r.payload.String() != "1@fresh\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
}

func TestCancellingTheWizardComesBackToTheQuestion(t *testing.T) {
	r := unknownRig(t)
	r.createCode = term.ExitCancelled
	r.Script.Gum = []string{createUnknown, cancelAll}
	exit(t, r.run("--type=podman", unknownTeam), term.ExitOK, r)
	contains(t, "stderr", r.stderr(), "Nothing was created; back to the choices.", nothingChosen)
	r.lacksOutput(t)
	if r.seatsAsked != nil {
		t.Fatalf("seats %v", r.seatsAsked)
	}
}

func TestAWizardThatFailsForAnUnknownTeamEndsTheRunWithItsCode(t *testing.T) {
	r := unknownRig(t)
	r.createCode = 78
	r.Script.Gum = []string{createUnknown}
	exit(t, r.run("--type=podman", unknownTeam), 78, r)
	r.lacksOutput(t)
}

func TestAnUnknownTeamCanBeSwappedForOneThisMachineHas(t *testing.T) {
	r := unknownRig(t)
	r.Script.Gum = []string{pickInstead, "CHOOSE:beta"}
	exit(t, r.run("--type=podman", unknownTeam), term.ExitOK, r)
	if r.payload.String() != "1@beta\n" || r.creates != 0 {
		t.Fatalf("payload %q creates %d", r.payload.String(), r.creates)
	}
	pick := r.Script.GumLog[1]
	contains(t, "pick", pick, "alpha", "beta")
}

// A team the list already names is not offered again in the unknown one's place.
func TestTheTeamsOfferedInsteadLeaveOutThoseTheListNames(t *testing.T) {
	r := unknownRig(t)
	r.Script.Gum = []string{pickInstead, "CHOOSE:beta"}
	exit(t, r.run("--type=podman", "3@alpha,"+unknownTeam), term.ExitOK, r)
	if r.payload.String() != "3@alpha,1@beta\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
	if pick := r.Script.GumLog[1]; strings.Contains(pick, "alpha") {
		t.Fatalf("alpha is in the list already: %s", pick)
	}
}

func TestLeavingTheChoiceOfAnotherTeamComesBackToTheQuestion(t *testing.T) {
	r := unknownRig(t)
	r.Script.Gum = []string{pickInstead, "CANCEL", cancelAll}
	exit(t, r.run("--type=podman", unknownTeam), term.ExitOK, r)
	r.lacksOutput(t)
	if len(r.Script.GumLog) != 3 {
		t.Fatalf("gum %v", r.Script.GumLog)
	}
}

func TestAnUnknownTeamCanBeLeftOutAndTheOthersStarted(t *testing.T) {
	r := unknownRig(t)
	r.Script.Gum = []string{"CHOOSE:Start on 3@alpha, beta without " + unknownTeam}
	exit(t, r.run("--type=podman", "3@alpha,"+unknownTeam+",beta"), term.ExitOK, r)
	if r.payload.String() != "3@alpha,1@beta\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
	if want := "New session › Team 2 of 3: " + unknownTeam + " › Not on this machine"; r.Script.Headings[0] != want {
		t.Fatalf("heading %q, want %q", r.Script.Headings[0], want)
	}
}

func TestEscOnTheUnknownTeamQuestionStartsNothingAndSucceeds(t *testing.T) {
	for _, answer := range []string{"CANCEL", cancelAll} {
		r := unknownRig(t)
		r.Script.Gum = []string{answer}
		exit(t, r.run("--type=podman", "3@alpha,"+unknownTeam), term.ExitOK, r)
		contains(t, "stderr", r.stderr(), nothingChosen)
		r.lacksOutput(t)
	}
}

func TestTheQuestionComesBackThreeTimesAtMostThenFails(t *testing.T) {
	r := unknownRig(t)
	r.createCode = term.ExitCancelled
	r.Script.Gum = []string{createUnknown, createUnknown, createUnknown}
	exit(t, r.run("--type=podman", unknownTeam), term.ExitFailure, r)
	contains(t, "stderr", r.stderr(), "ccy-teams: nothing was settled for "+unknownTeam+" in 3 tries; nothing started.")
	if r.creates != 3 {
		t.Fatalf("creates %d", r.creates)
	}
	r.lacksOutput(t)
}

// A team declared here whose homeserver is down is this machine's: its seat is asked as before,
// and agent-bus says what is wrong with it.
func TestADeclaredTeamThatIsDownIsNotTreatedAsUnknown(t *testing.T) {
	r := unknownRig(t)
	r.On("systemctl is-active agent-bus-hs@beta.service", "inactive\n", 3)
	exit(t, r.run("--type=podman", "beta"), term.ExitOK, r)
	if r.payload.String() != "1@beta\n" || len(r.Script.GumLog) != 0 {
		t.Fatalf("payload %q gum %v", r.payload.String(), r.Script.GumLog)
	}
}

func TestAHostVarsThatCannotBeReadIsReportedForATeamNotUp(t *testing.T) {
	r := unknownRig(t)
	r.WriteHostVars(t, teamsVars(alphaCovers+"    port: port\n"))
	exit(t, r.run("--type=podman", unknownTeam), 78, r)
	contains(t, "stderr", r.stderr(), "the port is not a number")
	r.lacksOutput(t)
}
