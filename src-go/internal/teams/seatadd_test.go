package teams

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/placeissued"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seatadd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
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

// fakeIssuer records the seat it is asked to issue and answers a document the fake placement
// reads back.
type fakeIssuer struct{ asked []seatadd.Issue }

func (f *fakeIssuer) Issue(_ context.Context, is seatadd.Issue) ([]byte, error) {
	f.asked = append(f.asked, is)
	return []byte(is.Handle.String()), nil
}

// Register R5, through the App: a bare `choose-teams --type=podman <team>` on a teamchat team
// with no seat in this checkout runs the wired seatadd.Adder (its issue and placement faked), for
// the working directory's checkout, and the launch's payload is the new seat alone: the adder's
// notes reach stderr, never stdout.
func TestChooseTeamsAddsASeatThroughTheRealAdder(t *testing.T) {
	const team = "lts-v2"
	h := newHarness(t)
	teamtest.Interactive(t, h.World)
	h.On("git rev-parse --is-inside-work-tree", "true\n", 0)
	h.On("git rev-parse --show-toplevel", h.Checkout()+"\n", 0)
	h.On("git remote get-url origin", "git@github.com:example-org/proj.git\n", 0)
	h.WriteHostVars(t, teamtest.HostVarsAlpha)
	if err := os.MkdirAll(h.Checkout(), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(h.Checkout())
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	v2 := func(paths.TeamID) (status.Record, bool, error) {
		return status.Record{Origin: status.OriginV2}, true, nil
	}
	h.app.System.Status = v2
	adder := h.app.SeatAdd.Adder
	issuer := &fakeIssuer{}
	var origins []string
	var placed []string
	adder.Status, adder.Issuer = v2, issuer
	adder.Origin = func(_ context.Context, dir string) (string, error) {
		origins = append(origins, dir)
		return "git@github.com:example-org/proj.git", nil
	}
	adder.Site = func() (string, error) { return "", nil }
	adder.Place = func(_ context.Context, _ placeissued.Deps, doc io.Reader) (placeissued.Placed, error) {
		data, readErr := io.ReadAll(doc)
		if readErr != nil {
			return placeissued.Placed{}, readErr
		}
		placed = append(placed, string(data))
		name, parseErr := paths.ParseSeatName(string(data))
		return placeissued.Placed{Seat: name}, parseErr
	}

	if code := h.app.Run([]string{"choose-teams", "--type=podman", team}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if h.Out.String() != "1@"+team+"\n" {
		t.Fatalf("stdout %q, want the seat alone", h.Out.String())
	}
	if len(origins) != 1 || filepath.Clean(origins[0]) != filepath.Clean(wd) {
		t.Fatalf("the checkout asked of git: %v, want the working directory %s", origins, wd)
	}
	if len(issuer.asked) != 1 || issuer.asked[0].Handle.String() != "proj.1+local.podman" || len(placed) != 1 {
		t.Fatalf("issued %+v, placed %v", issuer.asked, placed)
	}
	for _, note := range []string{"Seat 1 of team " + team + " for this checkout: proj.1+local.podman", "Seat 1@" + team + " is in this checkout's pool", "This ccy launch takes seat 1@" + team + "."} {
		if !strings.Contains(h.ErrOut.String(), note) {
			t.Errorf("stderr lacks %q: %s", note, h.ErrOut.String())
		}
	}
	if strings.Contains(h.ErrOut.String(), "Launch it here") {
		t.Errorf("an automatic add tells the person to launch what is launching: %s", h.ErrOut.String())
	}
}
