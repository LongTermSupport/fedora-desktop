package login

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

const wantHow = "To sign in: ccy-teams, Enter on alpha, choose Open alpha in Element: it offers each person who has never signed in a password."

func TestSignInGapOffersAPasswordToAPersonWithNoSessions(t *testing.T) {
	h := newRig(t)
	h.passwordable()
	h.Script.Gum = []string{"YES"}
	h.Script.Fzf = []string{"|EXIT"}
	if err := h.l.SignInGap(t.Context(), []string{"alpha"}, true); err != nil {
		t.Fatal(err)
	}
	got := h.ErrOut.String()
	if !strings.Contains(got, "owner has never signed in to alpha.") || !strings.Contains(got, "Password    a-new-password\n") {
		t.Fatalf("stderr %q", got)
	}
	if strings.Contains(got, "guest") {
		t.Fatalf("a person with a session was offered a password: %q", got)
	}
}

func TestSignInGapDeclinedSaysHowToSignInLater(t *testing.T) {
	h := newRig(t)
	h.Script.Gum = []string{"NO"}
	if err := h.l.SignInGap(t.Context(), []string{"alpha"}, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.ErrOut.String(), wantHow) || strings.Contains(h.ErrOut.String(), "Password ") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestSignInGapWithoutATerminalAsksNothingAndSaysExactlyHow(t *testing.T) {
	h := newRig(t)
	if err := h.l.SignInGap(t.Context(), []string{"alpha"}, false); err != nil {
		t.Fatal(err)
	}
	if len(h.Script.GumLog) != 0 {
		t.Fatalf("asked: %q", h.Script.GumLog)
	}
	if !strings.Contains(h.ErrOut.String(), "owner has never signed in to alpha.") || !strings.Contains(h.ErrOut.String(), wantHow) {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestSignInGapThatCannotReadSessionsSaysSoWithTheCommand(t *testing.T) {
	h := newRig(t)
	h.OnErr(teamtest.SudoBus+"human devices alpha owner", "homeserver is down", 1)
	if err := h.l.SignInGap(t.Context(), []string{"alpha"}, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.ErrOut.String(), "Whether owner has signed in to alpha could not be read") ||
		!strings.Contains(h.ErrOut.String(), wantHow) {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestSignInGapWhenTheRosterCannotBeReadSaysSoWithTheCommand(t *testing.T) {
	h := newRig(t)
	h.OnErr(teamtest.SudoBus+"list alpha", "homeserver is down", 1)
	if err := h.l.SignInGap(t.Context(), []string{"alpha"}, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.ErrOut.String(), "Who is signed in to alpha could not be read") ||
		!strings.Contains(h.ErrOut.String(), wantHow) {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}
