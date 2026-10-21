package board

import (
	"maps"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

// SPEC-view.md V41: an open's start-up (the first Refresh, the offers' Refresh and the first
// Collect) shares one read of the units, the declaration and the verdict; the passes after it
// read again.
func TestAnOpensStartUpSharesOneReadOfTheTeams(t *testing.T) {
	h := populated(t)
	h.c.HoldTeams()
	for range 2 {
		if _, err := h.c.Refresh(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.c.Collect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := h.Spawned(); got["systemctl"] != 1 || got["declare.py status"] != 1 {
		t.Fatalf("the start-up started %v, want one systemctl and one declare.py status", got)
	}
	if _, err := h.c.Collect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := h.Spawned(); got["systemctl"] != 1 || got["declare.py status"] != 0 {
		t.Fatalf("the pass after the start-up started %v, want one systemctl and no declare.py", got)
	}
}

func TestWithoutAStartUpEveryPassReadsTheUnits(t *testing.T) {
	h := populated(t)
	for range 2 {
		if _, err := h.c.Refresh(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if got := h.Spawned(); got["systemctl"] != 2 {
		t.Fatalf("two passes started %v, want two systemctl", got)
	}
}

// Something done in between (an apply, a remove, a create, an edit) is told to ForgetTeams, and
// the next pass reads the teams again and sees it.
func TestAChangeDuringTheStartUpIsReadAgain(t *testing.T) {
	h := populated(t)
	h.c.HoldTeams()
	if _, err := h.c.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	h.On("systemctl list-units", teamtest.UnitAlpha+"agent-bus-hs@beta.service loaded active running Homeserver beta\n", 0)
	h.c.ForgetTeams()
	s, err := h.c.Refresh(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !s.Loaded["beta"] {
		t.Fatalf("the read after the change did not see it: %v", s.Loaded)
	}
	if _, err = h.c.Collect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := h.Spawned(); got["systemctl"] != 2 {
		t.Fatalf("started %v, want two systemctl: one before the change, one after", got)
	}
}

// Changed is what ops is told after a flow that may have changed a team: both what is kept of
// who is signed in and the start-up's read go.
func TestChangedForgetsTheDevicesAndTheStartUpsRead(t *testing.T) {
	h := populated(t)
	h.c.HoldTeams()
	if _, err := h.c.Collect(t.Context()); err != nil {
		t.Fatal(err)
	}
	h.c.HoldTeams()
	if _, err := h.c.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	h.Spawned()
	h.c.Changed()
	if _, err := h.c.Collect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := h.Spawned(); got["systemctl"] != 1 || got["agent-bus human devices"] != 3 {
		t.Fatalf("after Changed the pass started %v, want the units and every person's devices read again", got)
	}
}

func TestAFailedReadIsNotSharedWithTheStartUpsNextPass(t *testing.T) {
	h := populated(t)
	h.c.HoldTeams()
	h.On("systemctl list-units", "", 1)
	if _, err := h.c.Refresh(t.Context()); err == nil {
		t.Fatal("a failed systemctl read passed")
	}
	h.On("systemctl list-units", teamtest.UnitAlpha, 0)
	if _, err := h.c.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := h.Spawned(); !maps.Equal(got, map[string]int{"systemctl": 2, "declare.py status": 1}) {
		t.Fatalf("started %v", got)
	}
}
