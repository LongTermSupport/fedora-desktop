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

func TestOneMatchIsPrintedOnStdoutAloneAndAsksNothing(t *testing.T) {
	h := newRig(t, "TEAM\talpha\n")
	if code := h.f.Run(t.Context(), nil); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if h.Out.String() != "alpha\n" {
		t.Fatalf("stdout %q", h.Out.String())
	}
	if len(h.Script.GumLog) != 0 {
		t.Fatalf("for-checkout asks nothing: %v", h.Script.GumLog)
	}
}

func TestSeveralMatchesAreAllPrinted(t *testing.T) {
	h := newRig(t, "TEAM\talpha\nTEAM\tbeta\n")
	if code := h.f.Run(t.Context(), nil); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if h.Out.String() != "alpha\nbeta\n" {
		t.Fatalf("stdout %q", h.Out.String())
	}
}

// Q10: choosing is choose-teams now; for-checkout --choose is gone.
func TestChooseIsNoLongerAnOption(t *testing.T) {
	h := newRig(t, "TEAM\talpha\nTEAM\tbeta\n")
	if code := h.f.Run(t.Context(), []string{"--choose"}); code != term.ExitUsage {
		t.Fatalf("exit %d", code)
	}
	if h.Out.String() != "" || !strings.Contains(h.ErrOut.String(), "takes no arguments") {
		t.Fatalf("stdout %q stderr %q", h.Out.String(), h.ErrOut.String())
	}
}

func TestNoMatchSaysSoAndFails(t *testing.T) {
	h := newRig(t, "")
	if code := h.f.Run(t.Context(), nil); code != term.ExitFailure {
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
