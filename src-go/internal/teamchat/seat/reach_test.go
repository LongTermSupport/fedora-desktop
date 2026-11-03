package seat

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
)

// Plan 00246 X10: creating a seat for a URL this machine cannot reach fails fast and names the
// URL; launching into a seat that exists does not ask the homeserver first.

// X10: only a placement that creates a pool seat asks the homeserver; a reissue over an existing
// seat and a take do not.
func TestOnlyACreatedSeatReachesItsHomeserver(t *testing.T) {
	w := stubbed(t)
	if err := w.place(testPassword); err != nil {
		t.Fatal(err)
	}
	asked := w.probe.calls()
	if len(asked) != 1 || asked[0].BaseURL != w.bundle().BaseURL || asked[0].Team != w.seat.Team {
		t.Fatalf("a new seat asked %+v, want its bundle once", asked)
	}
	if err := w.place(otherPassword); err != nil {
		t.Fatal(err)
	}
	w.take("s1")
	if n := len(w.probe.calls()); n != 1 {
		t.Errorf("a reissue and a take asked the homeserver %d more times", n-1)
	}
}

// X10: a creation whose probe fails is ErrUnreachable naming the URL and keeping the cause, and
// writes nothing: the claim is empty, no pool seat and no checkout record are made.
func TestAProbeThatFailsRefusesTheCreation(t *testing.T) {
	w := stubbed(t)
	w.probe.err = errors.New("no route to host")
	names, err := w.fill(w.seat, w.bundle())
	if !errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), w.bundle().BaseURL) || !strings.Contains(err.Error(), "no route to host") {
		t.Fatalf("a probe that failed: %v", err)
	}
	if len(names) != 0 || exists(t, w.pool().Dir()) || exists(t, w.checkoutRecord()) {
		t.Errorf("a refused creation wrote %v, a pool seat or a record", names)
	}
}

// X10: with the real probe, a creation for a homeserver that does not answer fails fast with the
// client's own error under ErrUnreachable and the URL named, and succeeds once it answers.
func TestPlacingASeatForAnUnreachableURLFailsFastNamingIt(t *testing.T) {
	w := newWorld(t)
	if err := w.hs.Stop(); err != nil {
		t.Fatal(err)
	}
	names, err := w.fill(w.seat, w.bundle())
	if !errors.Is(err, ErrUnreachable) || !errors.Is(err, matrix.ErrUnreachable) || !strings.Contains(err.Error(), w.hs.URL()) {
		t.Fatalf("a creation for a homeserver that is down: %v", err)
	}
	if len(names) != 0 || exists(t, w.pool().Dir()) || exists(t, w.checkoutRecord()) {
		t.Errorf("a refused creation wrote %v, a pool seat or a record", names)
	}
	if err = w.hs.Start(); err != nil {
		t.Fatal(err)
	}
	if err = w.place(testPassword); err != nil {
		t.Errorf("a creation once the homeserver answers: %v", err)
	}
}

// X10: a reissue over an existing seat is not a creation: the owner's credential reaches a
// machine whose homeserver is down, which only an operation that targets the homeserver reports.
func TestReissuingASeatDoesNotNeedTheHomeserver(t *testing.T) {
	w := newWorld(t)
	if err := w.place(testPassword); err != nil {
		t.Fatal(err)
	}
	if err := w.hs.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := w.place(otherPassword); err != nil {
		t.Fatalf("a reissue while the homeserver is down: %v", err)
	}
	if w.readFile(w.pool().Password()) != otherPassword {
		t.Errorf("the new password is not in place")
	}
}

// X10: a take of an existing seat does not ask the homeserver first. Its login does; when that
// finds the homeserver down the take is ErrUnreachable naming the URL (and not a reissue), so a
// launcher can start without the seat and take again, which then works.
func TestATakeWhileTheHomeserverIsDownIsUnreachableAndCanBeTakenAgain(t *testing.T) {
	w := newWorld(t)
	if err := w.place(testPassword); err != nil {
		t.Fatal(err)
	}
	if err := w.hs.Stop(); err != nil {
		t.Fatal(err)
	}
	_, err := w.tryTake("s1", StateActive)
	if !errors.Is(err, ErrUnreachable) || errors.Is(err, ErrReissue) || !strings.Contains(err.Error(), w.hs.URL()) {
		t.Fatalf("a take while the homeserver is down: %v", err)
	}
	if exists(t, w.sessionSeat("s1").Dir()) {
		t.Errorf("a take that found no homeserver made the session's seat directory")
	}
	if err = w.hs.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err = w.tryTake("s1", StateActive); err != nil {
		t.Errorf("a take once the homeserver answers: %v", err)
	}
}

// X10, X16: the real probe asks an https homeserver with the CA its bundle pins: the right CA
// reaches it, another team's CA is ErrUnreachable naming the URL.
func TestTheProbeOverTLSUsesTheBundlesCA(t *testing.T) {
	ca, other := mustCA(t, "team-a"), mustCA(t, "team-b")
	clk := newClock()
	h := startTLSHomeserver(t, mustLeaf(t, ca, "127.0.0.1"), clk)
	reach := VersionsReach{Clock: clk}
	pinned := bundleOf(t, "ops", h.hs.URL(), string(ca.CertPEM()))
	if err := reach.Reach(t.Context(), pinned); err != nil {
		t.Errorf("the right CA: %v", err)
	}
	wrong := bundleOf(t, "ops", h.hs.URL(), string(other.CertPEM()))
	if err := reach.Reach(t.Context(), wrong); !errors.Is(err, matrix.ErrUnreachable) || !strings.Contains(err.Error(), "unknown authority") {
		t.Errorf("another CA: %v, want a TLS verification failure", err)
	}
	w := newWorld(t)
	w.homeserver = h
	if _, err := w.fill(w.seat, wrong); !errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), h.hs.URL()) {
		t.Errorf("a creation pinned to another CA: %v", err)
	}
	if _, err := w.fill(w.seat, pinned); err != nil {
		t.Errorf("a creation pinned to the right CA: %v", err)
	}
}

// X10: the probe asks the versions of the homeserver and nothing else, with no token: it is the
// one unauthenticated call, so it needs no credential and changes nothing on the homeserver.
func TestTheProbeAsksOnlyTheVersions(t *testing.T) {
	w := newWorld(t)
	if err := (VersionsReach{Clock: w.clock}).Reach(t.Context(), w.bundle()); err != nil {
		t.Fatal(err)
	}
	reqs := w.hs.Requests()
	if len(reqs) != 1 || reqs[0].Method != http.MethodGet || !strings.HasSuffix(reqs[0].Path, "/versions") || reqs[0].Header.Get("Authorization") != "" {
		t.Errorf("the probe sent %+v", reqs)
	}
}
