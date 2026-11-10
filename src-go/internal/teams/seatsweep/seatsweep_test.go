package seatsweep

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const testClaim = "00112233aabbccdd"

// stillClock is a clock that moves only when the code sleeps.
type stillClock struct{ now time.Time }

func (c *stillClock) Now() time.Time { return c.now }

func (c *stillClock) Sleep(ctx context.Context, d time.Duration) error {
	c.now = c.now.Add(d)
	return ctx.Err()
}

// sessions is the session records: a session is restorable while it is listed true.
type sessions map[string]bool

func (s sessions) Restorable(n paths.SessionName) (bool, error) { return s[n.String()], nil }

// announcer counts the notices it is asked for.
type announcer struct{ asked int }

func (a *announcer) Disconnected(context.Context, seat.Notice) (seat.Outcome, error) {
	a.asked++
	return seat.Outcome{Posted: true}, nil
}

// world is a scratch home with a seat store and the Cmd over it.
type world struct {
	t       *testing.T
	home    string
	env     map[string]string
	store   paths.Store
	out     bytes.Buffer
	errOut  bytes.Buffer
	cmd     *Cmd
	live    sessions
	notices *announcer
}

func newWorld(t *testing.T) *world {
	t.Helper()
	home := t.TempDir()
	store, err := paths.SeatStore(filepath.Join(home, ".local", "state"))
	if err != nil {
		t.Fatal(err)
	}
	clk := &stillClock{now: time.UnixMilli(1_800_000_000_000)}
	w := &world{
		t: t, home: home, store: store, live: sessions{}, notices: &announcer{},
		env: map[string]string{"HOME": home, "XDG_RUNTIME_DIR": filepath.Join(home, "run")},
	}
	w.cmd = &Cmd{
		Stdout: &w.out, Stderr: &w.errOut, Getenv: func(k string) string { return w.env[k] },
		Parts: seat.Config{
			Clock: clk, Credential: seat.Password{Clock: clk}, Sessions: w.live, Announcer: w.notices,
			Reach: seat.VersionsReach{Clock: clk},
		},
		Open: OpenStore,
	}
	return w
}

func (w *world) key(checkout string) paths.CheckoutKey {
	w.t.Helper()
	key, err := paths.CheckoutKeyOf(checkout)
	if err != nil {
		w.t.Fatal(err)
	}
	return key
}

func (w *world) write(path, data string) {
	w.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		w.t.Fatal(err)
	}
}

// hold makes session the holder of a pool seat of the checkout, as a take leaves it.
func (w *world) hold(key paths.CheckoutKey, team, name, session string) paths.PoolSeat {
	w.t.Helper()
	pool := w.store.PoolSeat(key, mustTeam(w.t, team), mustSeat(w.t, name))
	w.write(pool.HeldRecord(), `{"v":1,"session":"`+session+`","claim":"`+testClaim+`","since_ms":1}`)
	return pool
}

func (w *world) desire(session, seats string) {
	w.t.Helper()
	w.write(w.store.DesiredFile(mustSession(w.t, session)),
		`{"v":1,"session":"`+session+`","generation":2,"written_ms":1,"seats":[`+seats+`],"farewell":[],"inbound":"accept"}`)
}

func (w *world) run(args ...string) term.ExitCode {
	w.t.Helper()
	return w.cmd.Run(w.t.Context(), args)
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return err == nil
}

func mustTeam(t *testing.T, s string) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

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

// C 3.7.5, DC21: the sweep passes over every checkout of the pool. A seat whose session is over
// is released; a seat its session no longer desires is released as a leave; a seat a
// restorable session desires (or whose desired.json the launch has not written yet) is kept.
func TestSweepActsOnEveryCheckoutOfThePool(t *testing.T) {
	w := newWorld(t)
	a, b := w.key("/work/a"), w.key("/work/b")
	over := w.hold(a, "ops", "repo.1+site.podman", "s1")
	left := w.hold(b, "ops", "repo.2+site.podman", "s2")
	kept := w.hold(b, "dev", "repo.3+site.podman", "s2")
	starting := w.hold(b, "web", "repo.4+site.podman", "s3")
	w.live["s2"], w.live["s3"] = true, true
	w.desire("s2", `{"team":"dev","seat":"3"}`)
	if code := w.run("sweep"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.errOut.String())
	}
	if exists(t, over.Held()) || exists(t, left.Held()) || !exists(t, kept.Held()) || !exists(t, starting.Held()) {
		t.Fatalf("held/: over %v left %v kept %v starting %v", exists(t, over.Held()), exists(t, left.Held()),
			exists(t, kept.Held()), exists(t, starting.Held()))
	}
	for _, want := range []string{
		"released seat repo.1+site.podman of team ops (session s1)\n",
		"left seat repo.2+site.podman of team ops (session s2)\n",
	} {
		if !strings.Contains(w.out.String(), want) {
			t.Errorf("stdout lacks %q: %q", want, w.out.String())
		}
	}
	if strings.Count(w.out.String(), "\n") != 2 || w.errOut.Len() != 0 || w.notices.asked != 0 {
		t.Fatalf("stdout %q stderr %q notices %d", w.out.String(), w.errOut.String(), w.notices.asked)
	}
}

// A host with no seat store, or one with nothing placed yet, has nothing to sweep.
func TestSweepWithNoPoolDoesNothing(t *testing.T) {
	w := newWorld(t)
	if code := w.run("sweep"); code != term.ExitOK || w.out.Len() != 0 || w.errOut.Len() != 0 {
		t.Fatalf("exit %d: %q %q", code, w.out.String(), w.errOut.String())
	}
}

// One checkout that cannot be swept is said and fails the run, and the others are still swept.
func TestSweepGoesOnPastACheckoutItCannotSweep(t *testing.T) {
	w := newWorld(t)
	over := w.hold(w.key("/work/a"), "ops", "repo.1+site.podman", "s1")
	w.write(w.store.Pool()+"/stray", "")
	if code := w.run("sweep"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if exists(t, over.Held()) {
		t.Fatal("the good checkout was not swept")
	}
	if !strings.Contains(w.errOut.String(), "stray") || !strings.Contains(w.errOut.String(), "1 checkout(s) or seat(s) of the pool were not swept") {
		t.Fatalf("stderr %q", w.errOut.String())
	}
}

// A seat whose sweep fails is said, with its team and seat, and the run fails.
func TestSweepSaysASeatItCouldNotSettle(t *testing.T) {
	w := newWorld(t)
	w.hold(w.key("/work/a"), "ops", "repo.1+site.podman", "s1")
	w.live["s1"] = true
	w.write(w.store.DesiredFile(mustSession(t, "s1")), "not json")
	if code := w.run("sweep"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.errOut.String(), "seat repo.1+site.podman of team ops") {
		t.Fatalf("stderr %q", w.errOut.String())
	}
}

// A store that will not open is said, and the run fails.
func TestSweepSaysAStoreThatWillNotOpen(t *testing.T) {
	w := newWorld(t)
	w.hold(w.key("/work/a"), "ops", "repo.1+site.podman", "s1")
	w.cmd.Open = func(seat.Config) (Store, error) { return nil, errors.New("no store") }
	if code := w.run("sweep"); code != term.ExitFailure || !strings.Contains(w.errOut.String(), "no store") {
		t.Fatalf("exit %d: %q", code, w.errOut.String())
	}
}

// The seat store is found from HOME: without one the sweep cannot run.
func TestSweepNeedsAHome(t *testing.T) {
	w := newWorld(t)
	delete(w.env, "HOME")
	if code := w.run("sweep"); code != term.ExitFailure || !strings.Contains(w.errOut.String(), "state home") {
		t.Fatalf("exit %d: %q", code, w.errOut.String())
	}
}

// A pool that cannot be read (here, a file where the directory should be) fails the run.
func TestSweepSaysAPoolItCannotRead(t *testing.T) {
	w := newWorld(t)
	w.write(w.store.Pool(), "")
	if code := w.run("sweep"); code != term.ExitFailure || !strings.Contains(w.errOut.String(), "the pool") {
		t.Fatalf("exit %d: %q", code, w.errOut.String())
	}
}

func TestUsageErrorsAre64(t *testing.T) {
	for _, args := range [][]string{nil, {"nope"}, {"sweep", "x"}, {"units"}, {"units", "list", "x"}, {"units", "print"}, {"units", "print", "nope"}} {
		w := newWorld(t)
		if code := w.run(args...); code != term.ExitUsage || !strings.Contains(w.errOut.String(), "ccy-teams: seats") {
			t.Fatalf("%v: exit %d: %q", args, code, w.errOut.String())
		}
	}
}

// The play learns the user units from `seats units list` and `units print`: their names, where
// under the owner's home each goes, and the text.
func TestUnitsListNamesBothUnitsAndWhereThePlayPutsThem(t *testing.T) {
	w := newWorld(t)
	if code := w.run("units", "list"); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	want := "teamchat-seats.service\t.config/systemd/user/teamchat-seats.service\n" +
		"teamchat-seats.timer\t.config/systemd/user/teamchat-seats.timer\n"
	if w.out.String() != want {
		t.Fatalf("stdout %q", w.out.String())
	}
}

// C 3.7.5: the timer once a minute from 30 s after it starts; the service a oneshot of
// `ccy-teams seats sweep` with a two-minute bound; L6: skipped while the owner has no pool.
func TestUnitsPrintIsTheDesignsText(t *testing.T) {
	store, err := paths.SeatStore("/state")
	if err != nil {
		t.Fatal(err)
	}
	pool := "%S" + strings.TrimPrefix(store.Pool(), "/state")
	for name, wants := range map[string][]string{
		"teamchat-seats.timer": {"[Timer]\nOnActiveSec=30s\nOnUnitActiveSec=60s\nAccuracySec=5s\n", "[Install]\nWantedBy=timers.target\n"},
		"teamchat-seats.service": {
			"ConditionPathIsDirectory=" + pool + "\n", "Type=oneshot\n",
			"ExecStart=%h/.local/bin/ccy-teams seats sweep\n", "TimeoutStartSec=120\n",
		},
	} {
		w := newWorld(t)
		if code := w.run("units", "print", name); code != term.ExitOK {
			t.Fatalf("%s: exit %d", name, code)
		}
		for _, want := range wants {
			if !strings.Contains(w.out.String(), want) {
				t.Errorf("%s lacks %q:\n%s", name, want, w.out.String())
			}
		}
	}
}

// The service runs ccy-teams where play-claude-yolo.yml installs it, and that play writes what
// `seats units` prints and runs the timer.
func TestThePlayInstallsWhatTheUnitsName(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "playbooks", "imports", "play-claude-yolo.yml"))
	if err != nil {
		t.Fatal(err)
	}
	play := string(data)
	for _, want := range []string{
		`go_command_dest: "/home/{{ user_login }}/.local/bin/ccy-teams"`,
		`argv: ["/home/{{ user_login }}/.local/bin/ccy-teams", seats, units, list]`,
		`argv: ["/home/{{ user_login }}/.local/bin/ccy-teams", seats, units, print,`,
		"name: " + TimerName + "\n        scope: user\n        enabled: true\n        state: started\n",
	} {
		if !strings.Contains(play, want) {
			t.Errorf("play-claude-yolo.yml lacks %q", want)
		}
	}
	if !strings.Contains(service, "ExecStart=%h/.local/bin/ccy-teams ") {
		t.Fatal("the service does not run the ccy-teams the play installs")
	}
}

// The desired seam: a missing desired.json is still desired (the launch writes it after its
// take); otherwise the seat is desired when its team and the handle's seat part are listed.
func TestDesiredReadsTheSessionsDesiredJSON(t *testing.T) {
	w := newWorld(t)
	d := Desired{Store: w.store}
	s1 := mustSession(t, "s1")
	ops := seat.Seat{Team: mustTeam(t, "ops"), Name: mustSeat(t, "repo.1+site.podman")}
	if got, err := d.Desires(s1, ops); err != nil || !got {
		t.Fatalf("no desired.json: %v %v", got, err)
	}
	w.desire("s1", `{"team":"ops","seat":"1"}`)
	if got, err := d.Desires(s1, ops); err != nil || !got {
		t.Fatalf("listed: %v %v", got, err)
	}
	other := seat.Seat{Team: mustTeam(t, "ops"), Name: mustSeat(t, "repo.2+site.podman")}
	if got, err := d.Desires(s1, other); err != nil || got {
		t.Fatalf("another seat: %v %v", got, err)
	}
	dev := seat.Seat{Team: mustTeam(t, "dev"), Name: mustSeat(t, "repo.1+site.podman")}
	if got, err := d.Desires(s1, dev); err != nil || got {
		t.Fatalf("another team: %v %v", got, err)
	}
}

func TestDesiredRefusesWhatItCannotJudge(t *testing.T) {
	w := newWorld(t)
	d := Desired{Store: w.store}
	s1 := mustSession(t, "s1")
	ops := seat.Seat{Team: mustTeam(t, "ops"), Name: mustSeat(t, "repo.1+site.podman")}
	w.write(w.store.DesiredFile(mustSession(t, "s2")),
		`{"v":1,"session":"s1","generation":2,"written_ms":1,"seats":[],"farewell":[],"inbound":"accept"}`)
	if _, err := d.Desires(mustSession(t, "s2"), ops); err == nil || !strings.Contains(err.Error(), "names session s1") {
		t.Fatalf("another session's file: %v", err)
	}
	w.write(w.store.DesiredFile(s1), strings.Repeat(" ", 128<<10))
	if _, err := d.Desires(s1, ops); err == nil {
		t.Fatal("a file over its cap")
	}
	if err := os.Remove(w.store.DesiredFile(s1)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("elsewhere", w.store.DesiredFile(s1)); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Desires(s1, ops); err == nil {
		t.Fatal("a link is followed")
	}
	if _, err := d.Desires(s1, seat.Seat{Team: ops.Team, Name: mustSeat(t, "plain")}); err == nil {
		t.Fatal("a pool seat that is not a handle")
	}
}
