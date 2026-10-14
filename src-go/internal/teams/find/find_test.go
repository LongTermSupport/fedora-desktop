package find

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// rig is the fake machine, on a terminal, in a GitHub checkout of example-org/proj.
type rig struct {
	*teamtest.World
	*teamtest.Console

	f *Find
}

func newRig(t *testing.T, covering string) rig {
	t.Helper()
	w := teamtest.Populated(t)
	c := teamtest.Interactive(t, w)
	w.On("git rev-parse --is-inside-work-tree", "true\n", 0)
	w.On("git remote get-url origin", "git@example.com:example-org/proj.git\n", 0)
	w.OnDeclare("github-remote", "example-org/proj\n")
	w.OnDeclare("teams-for", covering)
	return rig{World: w, Console: c, f: New(w.Host)}
}

func TestOneMatchIsPrintedAndSaidOnStderr(t *testing.T) {
	h := newRig(t, "TEAM\talpha\n")
	if code := h.f.Run(t.Context(), []string{"--choose"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if h.Out.String() != "alpha\n" {
		t.Fatalf("stdout %q", h.Out.String())
	}
	if !strings.Contains(h.ErrOut.String(), "alpha") || !strings.Contains(h.ErrOut.String(), "example-org/proj") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
	if len(h.Script.GumLog) != 0 {
		t.Fatalf("one match asks nothing: %v", h.Script.GumLog)
	}
}

func TestSeveralMatchesAreAllPrintedWithoutChoose(t *testing.T) {
	h := newRig(t, "TEAM\talpha\nTEAM\tbeta\n")
	if code := h.f.Run(t.Context(), nil); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if h.Out.String() != "alpha\nbeta\n" {
		t.Fatalf("stdout %q", h.Out.String())
	}
}

func TestSeveralMatchesWithChooseAskTheHumanAndPrintTheChoice(t *testing.T) {
	h := newRig(t, "TEAM\talpha\nTEAM\tbeta\n")
	h.Script.Gum = []string{"CHOOSE:beta"}
	if code := h.f.Run(t.Context(), []string{"--choose"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if h.Out.String() != "beta\n" {
		t.Fatalf("stdout %q", h.Out.String())
	}
	if len(h.Script.GumLog) != 1 || !strings.Contains(h.Script.GumLog[0], "alpha beta") {
		t.Fatalf("gum %v", h.Script.GumLog)
	}
}

func TestChoosingNothingIsCancelled(t *testing.T) {
	h := newRig(t, "TEAM\talpha\nTEAM\tbeta\n")
	h.Script.Gum = []string{"CANCEL"}
	if code := h.f.Run(t.Context(), []string{"--choose"}); code != term.ExitCancelled {
		t.Fatalf("exit %d", code)
	}
	if h.Out.String() != "" {
		t.Fatalf("stdout %q", h.Out.String())
	}
}

func TestSeveralMatchesWithNobodyToAskNameThemAndFail(t *testing.T) {
	h := newRig(t, "TEAM\talpha\nTEAM\tbeta\n")
	h.Host.IsTerminal = func(any) bool { return false }
	h.Host.Term.IsTerminal = h.Host.IsTerminal
	if code := h.f.Run(t.Context(), []string{"--choose"}); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if h.Out.String() != "" || !strings.Contains(h.ErrOut.String(), "alpha, beta") {
		t.Fatalf("stdout %q stderr %q", h.Out.String(), h.ErrOut.String())
	}
}

func TestNoMatchSaysSoAndFails(t *testing.T) {
	h := newRig(t, "")
	if code := h.f.Run(t.Context(), []string{"--choose"}); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"No team", "example-org/proj", "ccy-teams"} {
		if !strings.Contains(h.ErrOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.ErrOut.String(), want)
		}
	}
	if h.Out.String() != "" {
		t.Fatalf("stdout %q", h.Out.String())
	}
}

func TestNotAGitCheckoutFails(t *testing.T) {
	h := newRig(t, "TEAM\talpha\n")
	h.On("git rev-parse --is-inside-work-tree", "", 128)
	if code := h.f.Run(t.Context(), nil); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.ErrOut.String(), "not a git checkout") || h.Out.String() != "" {
		t.Fatalf("stdout %q stderr %q", h.Out.String(), h.ErrOut.String())
	}
}

func TestNotAGithubCheckoutFails(t *testing.T) {
	h := newRig(t, "TEAM\talpha\n")
	h.OnDeclare("github-remote", "")
	if code := h.f.Run(t.Context(), nil); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.ErrOut.String(), "GitHub") || h.Out.String() != "" {
		t.Fatalf("stdout %q stderr %q", h.Out.String(), h.ErrOut.String())
	}
}

func TestNoOriginFails(t *testing.T) {
	h := newRig(t, "TEAM\talpha\n")
	h.On("git remote get-url origin", "", 2)
	if code := h.f.Run(t.Context(), nil); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.ErrOut.String(), "GitHub") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestUnknownOptionIsAUsageError(t *testing.T) {
	h := newRig(t, "TEAM\talpha\n")
	if code := h.f.Run(t.Context(), []string{"--bogus"}); code != term.ExitUsage {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.ErrOut.String(), "for-checkout") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}
