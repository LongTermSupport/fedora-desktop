package choose

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

// The launch summary asks only whether a remembered seat is taken and what role it holds.
func TestProbeSaysAFreeSeatIsNotTakenAndNamesItsRole(t *testing.T) {
	h := newRig(t)
	taken, role, err := h.c.Probe(t.Context(), "alpha", "1", "podman")
	if err != nil || taken || role != "worker" {
		t.Fatalf("taken %v role %q err %v", taken, role, err)
	}
}

func TestProbeSaysAHeldSeatIsTaken(t *testing.T) {
	h := newRig(t)
	taken, _, err := h.c.Probe(t.Context(), "alpha", "dev", "")
	if err != nil || !taken {
		t.Fatalf("taken %v err %v", taken, err)
	}
}

func TestProbeSaysACoordinatorsRole(t *testing.T) {
	h := newRig(t)
	h.On(teamtest.SudoBus+"list alpha", roster("coordinator", "worker"), 0)
	_, role, err := h.c.Probe(t.Context(), "alpha", "1", "")
	if err != nil || role != "coordinator" {
		t.Fatalf("role %q err %v", role, err)
	}
}

func TestProbeCallsANewSeatAWorkerAndNotTaken(t *testing.T) {
	h := newRig(t)
	taken, role, err := h.c.Probe(t.Context(), "alpha", "3", "")
	if err != nil || taken || role != "worker" {
		t.Fatalf("taken %v role %q err %v", taken, role, err)
	}
}

func TestProbeReportsATeamItCannotListAsAnError(t *testing.T) {
	h := newRig(t)
	h.On(seatChoose, "", 1)
	if _, _, err := h.c.Probe(t.Context(), "alpha", "1", ""); err == nil {
		t.Fatal("an unreadable team was not an error")
	}
}
