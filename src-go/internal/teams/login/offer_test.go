package login

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

func TestNewPasswordFailsWhenTheTeamsCannotBeRead(t *testing.T) {
	h := newRig(t)
	if err := os.Remove(filepath.Join(h.Root, "helpers", "agent_bus", "declare.py")); err != nil {
		t.Fatal(err)
	}
	if err := h.l.NewPassword(t.Context(), "alpha", "owner"); err == nil {
		t.Fatal("no error")
	}
}

func TestNewPasswordShowsTheLoginOfAPersonOfTheTeamJustMade(t *testing.T) {
	h := newRig(t)
	h.passwordable()
	if err := h.l.NewPassword(t.Context(), "alpha", "owner"); err != nil {
		t.Fatal(err)
	}
	if want := "Password    a-new-password\n"; !strings.Contains(h.ErrOut.String(), want) {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestAnOfferAfterCreateIsOnlyAsFailedAsTheFlowSaid(t *testing.T) {
	plain := errors.New("plain")
	cases := []struct {
		in   error
		want error
		text string
	}{
		{nil, nil, ""},
		{flow.Leave(term.ExitOK), nil, ""},
		{flow.Leave(term.ExitCancelled), ui.ErrCancelled, ""},
		{flow.Leave(term.ExitFailure), nil, "it stopped with exit 1"},
		{plain, plain, ""},
	}
	for _, c := range cases {
		got := offerResult(c.in)
		switch {
		case c.text != "":
			if got == nil || got.Error() != c.text {
				t.Fatalf("%v: got %v", c.in, got)
			}
		case !errors.Is(got, c.want):
			t.Fatalf("%v: got %v, want %v", c.in, got, c.want)
		}
	}
}

func TestOpenLaunchesElementAndAnswersWhatTheOfferNeeds(t *testing.T) {
	h := newRig(t)
	h.withElementProfile(t)
	if err := h.l.Open(t.Context(), "alpha"); err != nil {
		t.Fatalf("a launch that started is success: %v", err)
	}
}
