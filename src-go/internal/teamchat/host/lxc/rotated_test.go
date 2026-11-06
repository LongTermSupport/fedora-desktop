package lxc

import (
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// Plan 00246 SPEC-cross-network X33: a CA rotation marks every LXC seat that holds a handle for
// a reissue, which the next apply makes with the new CA and places, as it does for a bundle that
// was never placed.

func rotatedRecord(t *testing.T) lxcstate.Record {
	t.Helper()
	team, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	box, err := paths.ParseContainerName("box1")
	if err != nil {
		t.Fatal(err)
	}
	user, err := paths.ParseGuestUser("dev")
	if err != nil {
		t.Fatal(err)
	}
	e := func(seat int, handle, status string) lxcstate.Entry {
		return lxcstate.Entry{Container: box, Checkout: "/srv/thing", Seat: seat, User: user, Handle: handle, Status: status, SinceMS: 1_700_000_000_000}
	}
	return lxcstate.Record{V: 1, Team: team, Members: []lxcstate.Entry{
		e(1, "thing.1+local.lxc", lxcstate.StatusJoined),
		e(2, "thing.2+local.lxc", lxcstate.StatusOffline),
		e(3, "", lxcstate.StatusDeferred),
		e(4, "thing.4+local.lxc", lxcstate.StatusLeft),
	}}
}

func TestX33ARotationMarksEverySeatHoldingAHandleForAReissue(t *testing.T) {
	rec := rotatedRecord(t)
	got, handles := CARotated(rec, 42)
	if want := []string{"thing.1+local.lxc", "thing.2+local.lxc"}; !slices.Equal(handles, want) {
		t.Fatalf("handles %v, want %v", handles, want)
	}
	for _, e := range got.Members {
		marked := slices.Contains(handles, e.Handle)
		if marked != (e.Status == lxcstate.StatusDeferred && e.Why == WhyCARotated) {
			t.Errorf("entry %+v", e)
		}
		was, found := rec.Find(e.Container, e.Checkout, e.Seat)
		if !found {
			t.Fatalf("an entry was added: %+v", e)
		}
		if !marked && (was.Status != e.Status || was.Why != e.Why) {
			t.Errorf("an unmarked entry changed: %+v, was %+v", e, was)
		}
	}
	if _, err := got.Encode(); err != nil {
		t.Fatalf("the marked record does not encode: %v", err)
	}
}

func TestX33AMarkedSeatIsReissuedAtTheNextApply(t *testing.T) {
	got, handles := CARotated(rotatedRecord(t), 42)
	if len(handles) == 0 {
		t.Fatal("nothing marked")
	}
	for _, e := range got.Members {
		if e.Why != WhyCARotated {
			continue
		}
		m := &member{prior: e, handle: e.Handle}
		j, ok, err := joinOf(m, map[string]string{e.Handle: apply.MemberEnrolled})
		if err != nil || !ok || j.Op != apply.OpMemberReissue || j.Handle != e.Handle {
			t.Fatalf("%s: %+v, %v, %v", e.Handle, j, ok, err)
		}
	}
}
