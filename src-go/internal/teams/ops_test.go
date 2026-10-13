package teams

import (
	"errors"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestDefaultOpsAreTheAppsOwnStubs(t *testing.T) {
	h := newHarness(t)
	if h.app.Ops == nil {
		t.Fatal("wire sets the operator")
	}
	ctx := t.Context()
	if code := h.app.Ops.create(ctx, nil); code != term.ExitFailure {
		t.Fatalf("create exit %d", code)
	}
	if code := h.app.Ops.apply(ctx, []string{"--all"}); code != term.ExitFailure {
		t.Fatalf("apply exit %d", code)
	}
	if code := h.app.Ops.remove(ctx, []string{"alpha"}); code != term.ExitFailure {
		t.Fatalf("remove exit %d", code)
	}
	want := "ccy-teams: create is not built yet in the Go port.\n" +
		"ccy-teams: apply is not built yet in the Go port.\n" +
		"ccy-teams: remove is not built yet in the Go port.\n"
	if h.errOut.String() != want {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestDefaultOpsSayTheTeamFlowsAreNotBuilt(t *testing.T) {
	h := newHarness(t)
	if err := h.app.Ops.applyTeams(t.Context(), []string{"alpha"}); !errors.Is(err, errOpNotBuilt) {
		t.Fatalf("applyTeams: %v", err)
	}
	if err := h.app.Ops.runElementPlay(t.Context(), []string{"alpha"}); !errors.Is(err, errOpNotBuilt) {
		t.Fatalf("runElementPlay: %v", err)
	}
	want := "ccy-teams: apply is not built yet in the Go port.\n" +
		"ccy-teams: the Element play is not built yet in the Go port.\n"
	if h.errOut.String() != want {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestWireKeepsAnOperatorThatIsAlreadySet(t *testing.T) {
	h := newHarness(t)
	kept := &fakeOps{}
	h.app.Ops = kept
	h.app.wire()
	if h.app.Ops != teamOperator(kept) {
		t.Fatal("wire replaced the operator")
	}
}
