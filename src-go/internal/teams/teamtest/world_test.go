package teamtest

import (
	"testing"
)

func TestPopulatedWorldIsAHostWithTeamAlphaUp(t *testing.T) {
	w := Populated(t)
	teams, states, err := w.Host.Declare.Overview(t.Context(), w.Root, w.Host.AppliedDir, []string{"alpha"})
	if err != nil || len(teams) != 1 || teams[0].Name != "alpha" || len(states) != 1 {
		t.Fatalf("overview %+v %+v %v", teams, states, err)
	}
	units, err := w.Host.Systemd.HomeserverUnits(t.Context())
	if err != nil || len(units) != 1 || units[0].Team != "alpha" {
		t.Fatalf("units %+v %v", units, err)
	}
}

func TestSpawnedCountsWhatRanSinceTheLastCall(t *testing.T) {
	w := Populated(t)
	if _, err := w.Host.Systemd.HomeserverUnits(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := w.Spawned(); got["systemctl"] != 1 || len(got) != 1 {
		t.Fatalf("first %v", got)
	}
	if got := w.Spawned(); len(got) != 0 {
		t.Fatalf("second %v", got)
	}
}

func TestThePlayLockIsTheWorldsOwnAndCanBeHeldAsAnotherRunHoldsIt(t *testing.T) {
	w := New(t)
	if w.PlayLockHeld(t) {
		t.Fatal("a new world's play lock is held")
	}
	release := w.HoldPlayLock(t)
	if !w.PlayLockHeld(t) {
		t.Fatal("held, it reads free")
	}
	release()
	release()
	if w.PlayLockHeld(t) {
		t.Fatal("released, it reads held")
	}
}

func TestOnDeclareScriptsTheTwoHalvesOfTheOverview(t *testing.T) {
	w := New(t)
	w.OnDeclare("declared", DeclaredAlpha)
	w.OnDeclare("status", "STATUS\talpha\tcurrent\t-\n")
	teams, states, err := w.Host.Declare.Overview(t.Context(), w.Root, w.Host.AppliedDir, nil)
	if err != nil || len(teams) != 1 || len(states) != 1 {
		t.Fatalf("overview %+v %+v %v", teams, states, err)
	}
	w.OnDeclare("limited", "")
	if !w.Fake.Called("env PYTHONPATH") {
		t.Fatal("the overview ran through declare.py")
	}
}
