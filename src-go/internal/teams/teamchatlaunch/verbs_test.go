package teamchatlaunch

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/system"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// fakeHolder is the seat store the exit is given: its holdings, and every call made of it.
type fakeHolder struct {
	holdings []seat.Holding
	calls    []string
	err      error
	listErr  error
	closed   bool
}

func (f *fakeHolder) Holdings() ([]seat.Holding, error) { return f.holdings, f.listErr }

func (f *fakeHolder) Release(_ context.Context, r seat.Release) error {
	f.calls = append(f.calls, "release "+r.Seat.Name.String()+" "+r.Session.String())
	return f.err
}

func (f *fakeHolder) Announce(_ context.Context, s seat.Seat, n paths.SessionName) error {
	f.calls = append(f.calls, "announce "+s.Name.String()+" "+n.String())
	return f.err
}

func (f *fakeHolder) Restarted(_ context.Context, s seat.Seat, n paths.SessionName) error {
	f.calls = append(f.calls, "restarted "+s.Name.String()+" "+n.String())
	return f.err
}

func (f *fakeHolder) Close() error {
	f.closed = true
	return nil
}

type verbsWorld struct {
	t        *testing.T
	verbs    *Verbs
	out, err *bytes.Buffer
	exec     *execx.Fake
	taker    *fakeTaker
	holder   *fakeHolder
	systems  map[string]string
	initArgs []string
	forgot   []string
	checkout string
	store    paths.Store
	key      paths.CheckoutKey
}

func newVerbs(t *testing.T) *verbsWorld {
	t.Helper()
	home := t.TempDir()
	checkout, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := paths.SeatStore(filepath.Join(home, ".local", "state"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := paths.CheckoutKeyOf(checkout)
	if err != nil {
		t.Fatal(err)
	}
	w := &verbsWorld{
		t: t, out: &bytes.Buffer{}, err: &bytes.Buffer{}, exec: &execx.Fake{}, taker: &fakeTaker{},
		holder: &fakeHolder{}, systems: map[string]string{"alpha": system.Teamchat, "v1team": system.AgentBus},
		checkout: checkout, store: store, key: key,
	}
	env := map[string]string{"HOME": home, "XDG_RUNTIME_DIR": filepath.Join(home, "run")}
	w.verbs = &Verbs{
		Stdout: w.out, Stderr: w.err, Getenv: func(k string) string { return env[k] },
		Getwd: func() (string, error) { return checkout, nil },
		Resolve: func(_ context.Context, wd string, teams []string) ([]system.Line, error) {
			if wd != checkout {
				t.Errorf("resolved in %s", wd)
			}
			var lines []system.Line
			for _, team := range teams {
				sys, ok := w.systems[team]
				if !ok {
					return nil, errors.New("no such team")
				}
				if sys == "" {
					return nil, system.ErrUndecided
				}
				lines = append(lines, system.Line{Given: team, ID: team, System: sys})
			}
			return lines, nil
		},
		Membership: func(_ context.Context, args []string) (string, term.ExitCode) {
			w.initArgs = args
			if args[0] == "refused" {
				return "", term.ExitUsage
			}
			return "/run/m/" + args[0], term.ExitOK
		},
		Status: func(paths.TeamID) (status.Record, bool, error) { return status.Record{}, false, nil },
		NowMS:  func() int64 { return 7 },
		Exec:   w.exec,
		Taker:  func(seat.Config) Taker { return w.taker },
		Holder: func(cfg seat.Config) (Holder, error) {
			if cfg.Checkout != key || cfg.RuntimeDir != env["XDG_RUNTIME_DIR"] {
				t.Errorf("the store of %+v", cfg)
			}
			return w.holder, nil
		},
		Forget: func(session string) error {
			w.forgot = append(w.forgot, session)
			w.holder.calls = append(w.holder.calls, "forget "+session)
			return nil
		},
	}
	return w
}

func (w *verbsWorld) pool(team, name string) {
	w.t.Helper()
	id, err := paths.ParseTeamID(team)
	if err != nil {
		w.t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(w.store.PoolTeam(w.key, id), name), 0o700); err != nil {
		w.t.Fatal(err)
	}
}

func (w *verbsWorld) run(args ...string) term.ExitCode {
	w.out.Reset()
	w.err.Reset()
	return w.verbs.Run(context.Background(), args)
}

func (w *verbsWorld) lines() []string {
	return strings.Split(strings.TrimSuffix(w.out.String(), "\n"), "\n")
}

func TestLaunchArgsOnAgentBusIsTheMembershipMount(t *testing.T) {
	w := newVerbs(t)
	for z, suffix := range map[string]string{"Z": ",Z", "": ""} {
		if code := w.run("launch-args", "s1", w.checkout, "1@v1team", z, "--", "-v", "/x:/y"); code != term.ExitOK {
			t.Fatalf("exit %d: %s", code, w.err)
		}
		want := []string{
			w.checkout + "/.claude/ccy/pingbus/seats",
			"-v", "/run/m/s1:/run/ccy/membership:ro" + suffix, "-e", "PINGBUS_MEMBERSHIP=/run/ccy/membership",
		}
		if !slices.Equal(w.lines(), want) || !slices.Equal(w.initArgs, []string{"s1", w.checkout, "1@v1team"}) {
			t.Fatalf("got %q (init %q)", w.lines(), w.initArgs)
		}
	}
	if code := w.run("launch-args", "s1", w.checkout, "", "", "--"); code != term.ExitOK || w.initArgs[2] != "" {
		t.Fatalf("no seats: exit %d, init %q", code, w.initArgs)
	}
	if code := w.run("launch-args", "refused", w.checkout, "", "", "--"); code != term.ExitUsage || w.out.Len() != 0 {
		t.Fatalf("a refused init: exit %d, printed %q", code, w.out)
	}
}

func TestLaunchArgsOnTeamchatTakesAndPrintsTheControlDirectory(t *testing.T) {
	w := newVerbs(t)
	w.pool("alpha", "repo.1+site.podman")
	if code := w.run("launch-args", "s1", w.checkout, "1@alpha", "Z", "--", "-v", "/x:/y"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err)
	}
	session := mustSession(t, "s1")
	want := append([]string{w.store.Control(session)}, RunArgs(w.store, session, true)...)
	if !slices.Equal(w.lines(), want) {
		t.Fatalf("got %q\nwant %q", w.lines(), want)
	}
	if len(w.taker.took) != 1 || w.initArgs != nil {
		t.Fatalf("took %+v, init %q", w.taker.took, w.initArgs)
	}
}

func TestLaunchArgsRefusals(t *testing.T) {
	w := newVerbs(t)
	w.pool("alpha", "repo.1+site.podman")
	w.systems["later"] = ""
	for name, c := range map[string]struct {
		args []string
		want term.ExitCode
	}{
		"no separator":      {[]string{"s1", w.checkout, "1@alpha", "Z"}, term.ExitUsage},
		"a strange relabel": {[]string{"s1", w.checkout, "1@alpha", "z", "--"}, term.ExitUsage},
		"undecided":         {[]string{"s1", w.checkout, "1@later", "", "--"}, term.ExitConfig},
		"unknown":           {[]string{"s1", w.checkout, "1@nowhere", "", "--"}, term.ExitFailure},
		"a mount":           {[]string{"s1", w.checkout, "1@alpha", "", "--", "-v", w.store.Root() + ":/s"}, term.ExitConfig},
		"no such seat":      {[]string{"s1", w.checkout, "2@alpha", "", "--"}, term.ExitUsage},
		"a bad session":     {[]string{"../s", w.checkout, "1@alpha", "", "--"}, term.ExitUsage},
	} {
		if code := w.run(append([]string{"launch-args"}, c.args...)...); code != c.want || w.out.Len() != 0 || w.err.Len() == 0 {
			t.Errorf("%s: exit %d (want %d), out %q, err %q", name, code, c.want, w.out, w.err)
		}
	}
	for err, want := range map[error]term.ExitCode{
		seat.ErrHeld: term.ExitBusy, seat.ErrBusy: term.ExitBusy, seat.ErrRetired: term.ExitConfig,
		errors.New("disk"): term.ExitFailure,
	} {
		w.taker.err = err
		if code := w.run("launch-args", "s1", w.checkout, "1@alpha", "", "--"); code != want {
			t.Errorf("%v: exit %d, want %d", err, code, want)
		}
	}
}

func TestSeatCheckAndTakeHandAgentBusItsOwnCall(t *testing.T) {
	w := newVerbs(t)
	for _, args := range [][]string{
		{"seat-check", "1@v1team", "--resolved"},
		{"seat-check", ""},
		{"seat-check", " 1@v1team , X!"},
		{"seat-check", "1@v1team,,2@v1team"},
		{"seat-take", "1@v1team", "--no-prompt"},
	} {
		w.exec = &execx.Fake{}
		w.verbs.Exec = w.exec
		if code := w.run(args...); code != term.ExitOK {
			t.Fatalf("%q: exit %d: %s", args, code, w.err)
		}
		verb := strings.TrimPrefix(args[0], "seat-")
		want := append([]string{"seat", verb}, args[1:]...)
		got := w.exec.Execs()
		if len(got) != 1 || got[0].Name != AgentBusCommand || !slices.Equal(got[0].Args, want) {
			t.Fatalf("%q: exec %+v", args, got)
		}
	}
	w.exec.ExecError(&execx.NotInstalledError{Name: AgentBusCommand})
	if code := w.run("seat-take", "1@v1team"); code != term.ExitConfig {
		t.Fatalf("agent-bus missing: exit %d", code)
	}
}

func TestSeatCheckOnTeamchatPrintsTheCanonicalList(t *testing.T) {
	w := newVerbs(t)
	if code := w.run("seat-check", " 1@alpha ", "--resolved"); code != term.ExitOK || w.out.String() != "1@alpha\n" {
		t.Fatalf("exit %d, %q: %s", code, w.out, w.err)
	}
	if code := w.run("seat-check", "alpha"); code != term.ExitUsage || !strings.Contains(w.err.String(), "<seat>@<team>") {
		t.Fatalf("a team alone: exit %d, %s", code, w.err)
	}
	if code := w.run("seat-take", "1@alpha", "--no-prompt"); code != term.ExitOK || w.out.Len() != 0 {
		t.Fatalf("take: exit %d, %q", code, w.out)
	}
	if len(w.exec.Execs()) != 0 {
		t.Fatalf("agent-bus was run: %+v", w.exec.Execs())
	}
	w.systems["later"] = ""
	if code := w.run("seat-check", "1@later"); code != term.ExitConfig {
		t.Fatalf("undecided: exit %d", code)
	}
	for _, args := range [][]string{{"seat-check"}, {"seat-take", "a", "b", "c"}, {"nothing"}, {}} {
		if code := w.run(args...); code != term.ExitUsage {
			t.Errorf("%q: exit %d", args, code)
		}
	}
}

func holding(t *testing.T, session, name string) seat.Holding {
	t.Helper()
	team, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	return seat.Holding{
		Seat: seat.Seat{Team: team, Name: mustSeat(t, name)}, Complete: true,
		Record: seat.HeldRecord{Session: mustSession(t, session)},
	}
}

func TestBusDisconnectedOnAgentBusExecsPingbus(t *testing.T) {
	w := newVerbs(t)
	args := []string{"seat", "disconnected", "--root", w.checkout + "/.claude/ccy/pingbus/seats", "--session", "s1", "--since", "5"}
	if code := w.run(append([]string{"bus-disconnected", "0", "/kit/pingbus"}, args...)...); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err)
	}
	got := w.exec.Execs()
	if len(got) != 1 || got[0].Name != "/kit/pingbus" || !slices.Equal(got[0].Args, args) || w.holder.calls != nil {
		t.Fatalf("exec %+v, store %q", got, w.holder.calls)
	}
}

func (w *verbsWorld) disconnect(status string) term.ExitCode {
	session := mustSession(w.t, "s1")
	return w.run("bus-disconnected", status, "/kit/pingbus", "seat", "disconnected",
		"--root", w.store.Control(session), "--session", "s1", "--since", "5")
}

func TestBusDisconnectedOnTeamchatEndsTheSessionsSeats(t *testing.T) {
	for status, want := range map[string][]string{
		"0":   {"forget s1", "release repo.1+site.podman s1", "release repo.3+site.podman s1"},
		"137": {"forget s1", "release repo.1+site.podman s1", "release repo.3+site.podman s1"},
		"75":  {"restarted repo.1+site.podman s1", "restarted repo.3+site.podman s1"},
		"":    {"announce repo.1+site.podman s1", "announce repo.3+site.podman s1"},
	} {
		w := newVerbs(t)
		w.holder.holdings = []seat.Holding{
			holding(t, "s1", "repo.1+site.podman"), holding(t, "s2", "repo.2+site.podman"),
			holding(t, "s1", "repo.3+site.podman"),
			{Seat: holding(t, "s1", "repo.4+site.podman").Seat},
		}
		if code := w.disconnect(status); code != term.ExitOK {
			t.Fatalf("%q: exit %d: %s", status, code, w.err)
		}
		if !slices.Equal(w.holder.calls, want) || !w.holder.closed || len(w.exec.Execs()) != 0 {
			t.Errorf("%q: %q (closed %v)", status, w.holder.calls, w.holder.closed)
		}
	}
}

func TestBusDisconnectedOnTeamchatReportsEachFailure(t *testing.T) {
	w := newVerbs(t)
	w.holder.holdings = []seat.Holding{holding(t, "s1", "repo.1+site.podman"), holding(t, "s1", "repo.3+site.podman")}
	w.holder.err = errors.New("homeserver away")
	if code := w.disconnect("0"); code != term.ExitFailure || strings.Count(w.err.String(), "homeserver away") != 2 {
		t.Fatalf("exit %d: %s", code, w.err)
	}
	w.holder.listErr = errors.New("unreadable pool")
	if code := w.disconnect("0"); code != term.ExitFailure || !strings.Contains(w.err.String(), "unreadable pool") {
		t.Fatalf("exit %d: %s", code, w.err)
	}
	w.holder.listErr = nil
	w.holder.calls = nil
	w.verbs.Forget = func(string) error { return errors.New("registry gone") }
	if code := w.disconnect("0"); code != term.ExitFailure || len(w.holder.calls) != 0 {
		t.Fatalf("a failed forget released: exit %d, %q", code, w.holder.calls)
	}
	w.verbs.Holder = func(seat.Config) (Holder, error) { return nil, errors.New("no store") }
	if code := w.disconnect("0"); code != term.ExitFailure {
		t.Fatalf("no store: exit %d", code)
	}
	for _, args := range [][]string{
		{"bus-disconnected", "0"},
		{"bus-disconnected", "0", "/kit/pingbus", "seat", "disconnected", "--root"},
		{"bus-disconnected", "0", "/kit/pingbus", "seat", "disconnected", "--session", "../x", "--root", "/r"},
	} {
		if code := w.run(args...); code != term.ExitUsage {
			t.Errorf("%q: exit %d", args, code)
		}
	}
}
