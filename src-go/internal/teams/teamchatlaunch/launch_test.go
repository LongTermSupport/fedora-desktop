package teamchatlaunch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

func mustSeat(t *testing.T, s string) paths.SeatName {
	t.Helper()
	n, err := paths.ParseSeatName(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func mustSession(t *testing.T, s string) paths.SessionName {
	t.Helper()
	n, err := paths.ParseSessionName(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestMountSourcesReadsEveryVolumeForm(t *testing.T) {
	args := []string{
		"-v", "/a:/x:ro,Z", "--volume", "/b:/y", "--volume=/c:/z", "-v=/d:/w",
		"--mount", "type=bind,source=/e,target=/t", "--mount=type=bind,src=/f,dst=/u",
		"-e", "NAME", "--label", "x=/g:/h",
	}
	got := MountSources(args)
	want := []string{"/a", "/b", "/c", "/d", "/e", "/f"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestGuardRefusesAMountThatReachesTheSeatStore(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".local", "state", "teamchat", "seats")
	if err := os.MkdirAll(filepath.Join(root, "pool"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "link")
	if err := os.Symlink(filepath.Join(root, "pool"), link); err != nil {
		t.Fatal(err)
	}
	for name, src := range map[string]string{
		"an ancestor": home, "the root": root, "inside": filepath.Join(root, "pool"),
		"a link inside": link, "inside, not yet made": filepath.Join(root, "absent", "x"),
	} {
		if err := Guard(root, []string{"/elsewhere", src}); !errors.Is(err, ErrMount) {
			t.Fatalf("%s (%s): got %v, want ErrMount", name, src, err)
		}
	}
	other := filepath.Join(home, "repo")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Guard(root, []string{other, root + "-other", "/elsewhere"}); err != nil {
		t.Fatalf("a sibling: %v", err)
	}
}

func TestChooseSeatIsThePoolSeatWithThatSeatPart(t *testing.T) {
	pool := make([]paths.SeatName, 0, 3)
	pool = append(pool, mustSeat(t, "repo.1+site.podman"), mustSeat(t, "repo.2+site.podman"))
	got, err := ChooseSeat(pool, "2")
	if err != nil || got.String() != "repo.2+site.podman" {
		t.Fatalf("got %v %v", got, err)
	}
	if _, err := ChooseSeat(pool, "3"); !errors.Is(err, ErrSeat) || !strings.Contains(err.Error(), "repo.1+site.podman") {
		t.Fatalf("no such seat: got %v", err)
	}
	pool = append(pool, mustSeat(t, "repo.2+site.host"))
	if _, err := ChooseSeat(pool, "2"); !errors.Is(err, ErrSeat) {
		t.Fatalf("two handles with seat 2: got %v", err)
	}
}

func TestHandleStateIsTheRostersOrNoRoster(t *testing.T) {
	name := mustSeat(t, "repo.1+site.podman")
	if s, err := HandleState(status.Record{}, false, name); err != nil || s != seat.StateNoRoster {
		t.Fatalf("no record: %v %v", s, err)
	}
	for st, want := range map[string]seat.HandleState{
		"enrolled": seat.StateActive, "parked": seat.StateParked, "lost": seat.StateLost, "retired": seat.StateRetired,
	} {
		rec := status.Record{Members: []status.Member{{Handle: name.String(), State: st}}}
		if s, err := HandleState(rec, true, name); err != nil || s != want {
			t.Fatalf("%s: %v %v", st, s, err)
		}
	}
	if _, err := HandleState(status.Record{}, true, name); !errors.Is(err, ErrSeat) {
		t.Fatalf("not on the roster: %v", err)
	}
	rec := status.Record{Members: []status.Member{{Handle: name.String(), State: "claimed"}}}
	if _, err := HandleState(rec, true, name); !errors.Is(err, ErrSeat) {
		t.Fatalf("claimed: %v", err)
	}
}

func TestDesiredIsGenerationOneAcceptingWithTheSeats(t *testing.T) {
	session := mustSession(t, "ccy-proj-1")
	team, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	data, err := Desired(session, []Chosen{{Team: team, Name: mustSeat(t, "repo.2+site.podman"), Seat: "2"}}, 1234)
	if err != nil {
		t.Fatal(err)
	}
	d, err := state.DecodeDesired(data)
	if err != nil {
		t.Fatal(err)
	}
	if d.Generation != 1 || d.Inbound != state.InboundAccept || d.WrittenMS != 1234 || len(d.Seats) != 1 ||
		d.Seats[0].Seat != "2" || d.Seats[0].Team != team || len(d.Farewell) != 0 || d.Session != session {
		t.Fatalf("got %+v", d)
	}
}

func TestRunArgsAreTheTwoBindsTheEnvironmentAndTheStopTimeout(t *testing.T) {
	store, err := paths.SeatStore("/home/u/.local/state")
	if err != nil {
		t.Fatal(err)
	}
	session := mustSession(t, "s1")
	got := RunArgs(store, session, true)
	want := []string{
		"-v", store.Session(session) + ":" + paths.SessionSeatMount + ":rw,Z",
		"-v", store.Control(session) + ":" + MembershipMount + ":ro,Z",
		"-e", "PINGBUS_MEMBERSHIP=off", "-e", "TEAMCHAT_MEMBERSHIP=" + MembershipMount,
		"--stop-timeout", "20",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if plain := RunArgs(store, session, false); plain[1] != store.Session(session)+":"+paths.SessionSeatMount+":rw" {
		t.Fatalf("no relabel: %q", plain)
	}
}

// fakeTaker records what Launch asked of the store.
type fakeTaker struct {
	took []seat.Take
	err  error
}

func (f *fakeTaker) TakeAll(_ context.Context, takes []seat.Take) error {
	f.took = takes
	return f.err
}

type launchWorld struct {
	launch *Launch
	taker  *fakeTaker
	home   string
}

func newLaunchWorld(t *testing.T) launchWorld {
	t.Helper()
	home := t.TempDir()
	store, err := paths.SeatStore(filepath.Join(home, ".local", "state"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := paths.CheckoutKeyOf("/work/repo")
	if err != nil {
		t.Fatal(err)
	}
	team, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(store.PoolTeam(key, team)+"/repo.1+site.podman", 0o700); err != nil {
		t.Fatal(err)
	}
	taker := &fakeTaker{}
	return launchWorld{home: home, taker: taker, launch: &Launch{
		Store: store, Checkout: key, Taker: taker, NowMS: func() int64 { return 99 },
		Status: func(paths.TeamID) (status.Record, bool, error) { return status.Record{}, false, nil },
	}}
}

func TestLaunchTakesWritesDesiredAndPrintsTheArgs(t *testing.T) {
	w := newLaunchWorld(t)
	session := mustSession(t, "s1")
	args, err := w.launch.Run(context.Background(), session, "1@alpha", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	took := w.taker.took
	if len(took) != 1 || took[0].Seat.Name.String() != "repo.1+site.podman" || took[0].State != seat.StateNoRoster || took[0].Session != session {
		t.Fatalf("took %+v", took)
	}
	data, err := os.ReadFile(w.launch.Store.DesiredFile(session))
	if err != nil {
		t.Fatal(err)
	}
	d, err := state.DecodeDesired(data)
	if err != nil || d.Seats[0].Seat != "1" {
		t.Fatalf("desired %s: %v", data, err)
	}
	info, err := os.Stat(w.launch.Store.DesiredFile(session))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", info, err)
	}
	if !slices.Equal(args, RunArgs(w.launch.Store, session, true)) {
		t.Fatalf("args %q", args)
	}
}

func TestLaunchRefusesBeforeTakingAnything(t *testing.T) {
	w := newLaunchWorld(t)
	session := mustSession(t, "s1")
	if _, err := w.launch.Run(context.Background(), session, "1@alpha", []string{"-v", w.home + ":/h"}, false); !errors.Is(err, ErrMount) {
		t.Fatalf("mount: got %v", err)
	}
	for _, list := range []string{"", "9@alpha", "alpha", "1@alpha,2@alpha"} {
		if _, err := w.launch.Run(context.Background(), session, list, nil, false); err == nil {
			t.Fatalf("%q: no refusal", list)
		}
	}
	if w.taker.took != nil {
		t.Fatalf("took %+v", w.taker.took)
	}
}

func TestLaunchWritesNoDesiredWhenTheTakeFails(t *testing.T) {
	w := newLaunchWorld(t)
	w.taker.err = seat.ErrHeld
	session := mustSession(t, "s1")
	if _, err := w.launch.Run(context.Background(), session, "1@alpha", nil, false); !errors.Is(err, seat.ErrHeld) {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(w.launch.Store.DesiredFile(session)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("desired written: %v", err)
	}
}
