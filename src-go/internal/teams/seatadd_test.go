package teams

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// `ccy-teams seats add` is dispatched to the seat add verb, wired with every seam.
func TestDispatchSeatsAdd(t *testing.T) {
	h := newHarness(t)
	c := h.app.SeatAdd
	if c == nil || c.Getwd == nil || c.Adder == nil || c.Adder.Status == nil || c.Adder.Origin == nil ||
		c.Adder.Site == nil || c.Adder.Issuer == nil || c.Adder.Place == nil {
		t.Fatalf("seats add is not wired: %+v", c)
	}
	// Register R5: a launch with no free seat adds one through the same adder.
	if h.app.Chooser.Teamchat == nil || h.app.Chooser.Teamchat.NewSeat == nil {
		t.Fatal("the chooser's NewSeat is not wired, so a bare --teams <team> with no free seat ends at 78")
	}
	if code := h.app.Run([]string{"seats", "add"}); code != term.ExitUsage {
		t.Fatalf("exit %d, want 64: %s", code, h.ErrOut.String())
	}
	if !strings.Contains(h.ErrOut.String(), "ccy-teams seats add <team>") {
		t.Errorf("stderr %q", h.ErrOut.String())
	}
}
