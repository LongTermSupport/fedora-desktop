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

// secondUnknown is a second team this machine does not have, for a list naming two.
const secondUnknown = "other"

// unknownRig is the rig with alpha and beta declared and nosuch and other on no homeserver;
// each is a team name the team file would accept.
func unknownRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t, alphaCovers+betaWiden)
	for _, team := range []string{unknownTeam, secondUnknown} {
		r.On("systemctl is-active agent-bus-hs@"+team+".service", "inactive\n", 3)
		r.On(r.DeclarePrefix("check")+" --file "+r.Host.HostVars+" --team "+team, "", 0)
	}
	return r
}

// questions are the gum chooses about a team this machine does not have, in order.
func (r *rig) questions() []string {
	var asked []string
	for _, line := range r.Script.GumLog {
		if strings.Contains(line, "Cancel: start nothing") {
			asked = append(asked, line)
		}
	}
	return asked
}

// A name the team file would refuse is a usage mistake, said with the reason: there is nothing
// to create under it, so nothing is asked.
func TestAMalformedTeamNameIsRefusedBeforeAnythingIsAsked(t *testing.T) {
	r := unknownRig(t)
	r.On("systemctl is-active agent-bus-hs@Bad_Name.service", "inactive\n", 3)
	r.OnErr(r.DeclarePrefix("check")+" --file "+r.Host.HostVars+" --team Bad_Name", "declare: --team: not a team name", 64)
	exit(t, r.run("--type=podman", "Bad_Name"), term.ExitUsage, r)
	contains(t, "stderr", r.stderr(), "not a team name")
	if len(r.Script.GumLog) != 0 || r.creates != 0 {
		t.Fatalf("gum %v creates %d", r.Script.GumLog, r.creates)
	}
	r.lacksOutput(t)
}

// With two unknown teams, the second question is about the list as it stands: the first, left
// out, is not offered to start on.
func TestATeamLeftOutIsNotOfferedAgainForTheNextUnknownTeam(t *testing.T) {
	r := unknownRig(t)
	r.Script.Gum = []string{"CHOOSE:Start on " + secondUnknown + " without " + unknownTeam, cancelAll}
	exit(t, r.run("--type=podman", unknownTeam+","+secondUnknown), term.ExitOK, r)
	asked := r.questions()
	if len(asked) != 2 {
		t.Fatalf("questions %v", asked)
	}
	if strings.Contains(asked[1], "Start on") {
		t.Fatalf("nothing is left to start without %s: %s", secondUnknown, asked[1])
	}
	r.lacksOutput(t)
}

// A team swapped in for the first unknown team is in the list: the second is not offered it.
func TestATeamSwappedInIsNotOfferedAgain(t *testing.T) {
	r := unknownRig(t)
	r.Script.Gum = []string{pickInstead, "CHOOSE:beta", pickInstead, "CHOOSE:alpha"}
	exit(t, r.run("--type=podman", unknownTeam+","+secondUnknown), term.ExitOK, r)
	if r.payload.String() != "1@beta,1@alpha\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
	if pick := r.Script.GumLog[3]; strings.Contains(pick, "beta") {
		t.Fatalf("beta is in the list already: %s", pick)
	}
	if asked := r.questions(); !strings.Contains(asked[1], "Start on 1@beta without "+secondUnknown) {
		t.Fatalf("the second question is not about the list as it stands: %s", asked[1])
	}
}

// A team whose homeserver is not up would only fail in the unknown one's place: it is not
// offered, and with none up the choice is not offered at all.
func TestATeamThatIsNotUpIsNotOfferedInstead(t *testing.T) {
	r := unknownRig(t)
	r.On("systemctl is-active agent-bus-hs@beta.service", "inactive\n", 3)
	r.Script.Gum = []string{pickInstead, "CHOOSE:alpha"}
	exit(t, r.run("--type=podman", unknownTeam), term.ExitOK, r)
	if pick := r.Script.GumLog[1]; strings.Contains(pick, "beta") {
		t.Fatalf("beta is not up: %s", pick)
	}
	r = unknownRig(t)
	for _, team := range []string{"alpha", "beta"} {
		r.On("systemctl is-active agent-bus-hs@"+team+".service", "inactive\n", 3)
	}
	r.Script.Gum = []string{cancelAll}
	exit(t, r.run("--type=podman", unknownTeam), term.ExitOK, r)
	if strings.Contains(r.menu(t), "Choose one of this machine's teams instead") {
		t.Fatalf("no team is up to choose: %s", r.menu(t))
	}
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
