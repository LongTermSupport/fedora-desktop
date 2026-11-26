package system

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
)

// answersBase is the member URL of the bundles these tests place.
const answersBase = "http://127.0.0.1:8448"

// placeBundle makes team's pool seat repo.1+site.podman in the world's checkout with its
// seat.json, the bundle a take would log in with.
func placeBundle(t *testing.T, w scratch, team string) {
	t.Helper()
	checkout := filepath.Join(w.home, "repo")
	placeSeat(t, w.home, checkout, team)
	id := mustTeam(t, team)
	server := paths.ServerName(id)
	issuer, err := paths.ParseDigest("0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	b := seat.Bundle{
		V: seat.BundleVersion, Team: id, ServerName: server, UserID: "@repo.1+site.podman:" + server,
		AdminID: "@admin:" + server, MainRoom: "!abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ", BaseURL: answersBase,
		DeviceID: paths.SeatDevice, IssuedMS: 1_700_000_000_000, Issuer: issuer,
	}
	data, err := b.Encode()
	if err != nil {
		t.Fatal(err)
	}
	store, err := paths.SeatStore(filepath.Join(w.home, ".local", "state"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := paths.CheckoutKeyOf(checkout)
	if err != nil {
		t.Fatal(err)
	}
	name, err := paths.ParseSeatName("repo.1+site.podman")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.PoolSeat(key, id, name).Bundle(), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// reachFunc is a seat.Reacher from a function.
type reachFunc func(ctx context.Context, b seat.Bundle) error

func (f reachFunc) Reach(ctx context.Context, b seat.Bundle) error { return f(ctx, b) }

// Issue 430: the chooser offers a team only when its bus answers. Answers asks the homeserver a
// pool seat's own bundle names, as the seat's member would reach it: "" when it answers, the
// words of why not when it does not.
func TestAnswersAsksThePoolSeatsOwnHomeserver(t *testing.T) {
	w := world(t)
	placeBundle(t, w, "live")
	placeBundle(t, w, "dead")
	var asked []string
	w.cmd.Timers = clock.NewFake(0, 0)
	w.cmd.Reach = reachFunc(func(_ context.Context, b seat.Bundle) error {
		asked = append(asked, b.Team.String()+" "+b.BaseURL)
		if b.Team.String() == "dead" {
			return errors.New("connection refused")
		}
		return nil
	})
	why, err := w.cmd.Answers(t.Context(), "live")
	if err != nil || why != "" {
		t.Fatalf("a team whose bus answers: %q %v", why, err)
	}
	why, err = w.cmd.Answers(t.Context(), "dead")
	if err != nil || !strings.Contains(why, "does not answer") || !strings.Contains(why, answersBase) || !strings.Contains(why, "connection refused") {
		t.Fatalf("a team whose bus does not answer: %q %v", why, err)
	}
	if strings.Join(asked, ",") != "live "+answersBase+",dead "+answersBase {
		t.Fatalf("asked %v", asked)
	}
}

// A probe that hangs ends at AnswerLimit on the injected timers, and says it had no answer.
func TestAnswersEndsAHangingProbeAtItsBound(t *testing.T) {
	w := world(t)
	placeBundle(t, w, "slow")
	fake := clock.NewFake(0, 0)
	w.cmd.Timers = fake
	w.cmd.Reach = reachFunc(func(ctx context.Context, _ seat.Bundle) error {
		<-ctx.Done()
		return ctx.Err()
	})
	type answer struct {
		why string
		err error
	}
	got := make(chan answer, 1)
	go func() {
		why, err := w.cmd.Answers(t.Context(), "slow")
		got <- answer{why, err}
	}()
	if err := fake.AwaitDue(t.Context(), AnswerLimit); err != nil {
		t.Fatal(err)
	}
	fake.Advance(AnswerLimit)
	a := <-got
	if a.err != nil || !strings.Contains(a.why, "no answer within "+AnswerLimit.String()) {
		t.Fatalf("a hanging probe: %q %v", a.why, a.err)
	}
}

// A pool seat whose seat.json cannot be read cannot be judged: an error naming it, never a team
// called live or dead on no evidence; and an unwired probe refuses.
func TestAnswersThatCannotJudgeAreErrors(t *testing.T) {
	w := world(t)
	placeSeat(t, w.home, filepath.Join(w.home, "repo"), "bare")
	w.cmd.Timers = clock.NewFake(0, 0)
	w.cmd.Reach = reachFunc(func(context.Context, seat.Bundle) error { return nil })
	if why, err := w.cmd.Answers(t.Context(), "bare"); err == nil || !strings.Contains(err.Error(), "seat.json") {
		t.Fatalf("a seat with no seat.json: %q %v", why, err)
	}
	// No seat is no probe: the words say no seat was found, never that the bus did not answer.
	if why, err := w.cmd.Answers(t.Context(), "none"); err != nil || !strings.Contains(why, "no pool seat of it was found") ||
		strings.Contains(why, "does not answer") {
		t.Fatalf("a team with no seat here: %q %v", why, err)
	}
	if _, err := w.cmd.Answers(t.Context(), "not a team"); err == nil {
		t.Fatal("a name outside the team grammar was judged")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	placeBundle(t, w, "live")
	w.cmd.Reach = reachFunc(func(ctx context.Context, _ seat.Bundle) error { return ctx.Err() })
	if _, err := w.cmd.Answers(ctx, "live"); err == nil {
		t.Fatal("a probe whose caller ended called the team dead")
	}
	w.cmd.Reach = nil
	if _, err := w.cmd.Answers(t.Context(), "live"); err == nil {
		t.Fatal("an unwired probe judged a team")
	}
}
