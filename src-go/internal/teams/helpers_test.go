package teams

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func TestFreshTermsAreNotShared(t *testing.T) {
	h := newHarness(t)
	if a, b := h.app.freshTerm(), h.app.freshTerm(); a == b || a.Err != h.errOut {
		t.Fatal("a fresh Term is its own, over the App's stderr")
	}
}

func TestWireFillsOnlyWhatIsMissing(t *testing.T) {
	fake := &execx.Fake{}
	app := &App{Runner: fake}
	app.wire()
	if app.Term == nil || app.Sudo == nil || app.Systemd == nil || app.Tmux == nil || app.Git == nil ||
		app.Declare == nil || app.Bus == nil || app.Registry == nil || app.Getenv == nil ||
		app.Stdout == nil || app.Stderr == nil || app.Stdin == nil {
		t.Fatalf("an adapter was left unset: %+v", app)
	}
	kept := app.Git
	app.wire()
	if app.Git != kept {
		t.Fatal("wire replaced an adapter that was already set")
	}
}

func TestReportErrorPrintsTheReasonOnce(t *testing.T) {
	h := newHarness(t)
	h.app.reportErr(stringError("declare: nope"))
	if h.errOut.String() != "declare: nope\n" {
		t.Fatalf("stderr %q", h.errOut.String())
	}
	if strings.Contains(h.errOut.String(), "ERROR") {
		t.Fatal("the reason is printed as it is")
	}
}

type stringError string

func (e stringError) Error() string { return string(e) }
