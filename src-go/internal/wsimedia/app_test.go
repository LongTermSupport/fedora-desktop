package wsimedia

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// player is one MPRIS connection on the fake bus.
type player struct {
	status   string
	canPause bool
	gone     bool // the connection has left the bus
}

// bus is a fake session bus answering busctl as the real one prints its JSON: names maps each
// well-known name to its owner's unique name, players each unique name to its state.
type bus struct {
	names   map[string]string
	players map[string]*player
	// fail makes the busctl run whose argv contains the key fail with the value as its words.
	fail map[string]string
	// calls is each Pause or Play, as "<unique name> <method>".
	calls []string
}

func newBus() *bus {
	return &bus{names: map[string]string{}, players: map[string]*player{}, fail: map[string]string{}}
}

func (b *bus) add(name, owner, status string, canPause bool) {
	b.names[name] = owner
	if b.players[owner] == nil {
		b.players[owner] = &player{status: status, canPause: canPause}
	}
}

func refused(words string) (execx.Result, error) {
	return execx.Result{ExitCode: 1, Stderr: []byte(words + "\n")}, nil
}

func (b *bus) answer(c execx.Cmd) (execx.Result, error) {
	line := strings.Join(c.Args, " ")
	for key, words := range b.fail {
		if strings.HasSuffix(line, key) {
			return refused(words)
		}
	}
	if !slices.Equal(c.Args[:3], []string{"--user", "--json=short", "--timeout=5"}) {
		return execx.Result{}, fmt.Errorf("busctl without the user bus, JSON and timeout: %s", line)
	}
	args := c.Args[3:]
	switch {
	case slices.Equal(args, []string{"call", dbusName, dbusPath, dbusName, "ListNames"}):
		return b.listNames(), nil
	case len(args) == 7 && slices.Equal(args[:6], []string{"call", dbusName, dbusPath, dbusName, "GetNameOwner", "s"}):
		return b.owner(args[6])
	case len(args) == 6 && args[0] == "get-property" && slices.Equal(args[2:], []string{mprisPath, playerIface, "PlaybackStatus", "CanPause"}):
		return b.properties(args[1])
	case len(args) == 5 && args[0] == "call" && slices.Equal(args[2:4], []string{mprisPath, playerIface}):
		return b.method(args[1], args[4])
	}
	return execx.Result{}, fmt.Errorf("unscripted busctl: %s", line)
}

func (b *bus) listNames() execx.Result {
	all := []string{"org.freedesktop.DBus"}
	for name, owner := range b.names {
		if !b.players[owner].gone {
			all = append(all, name)
		}
	}
	for owner, p := range b.players {
		if !p.gone {
			all = append(all, owner)
		}
	}
	slices.Sort(all)
	return execx.Result{Stdout: []byte(`{"type":"as","data":[["` + strings.Join(all, `","`) + `"]]}` + "\n")}
}

func (b *bus) owner(name string) (execx.Result, error) {
	owner, ok := b.names[name]
	if !ok || b.players[owner].gone {
		return refused("Call failed: Could not get owner of name '" + name + "': no such name")
	}
	return execx.Result{Stdout: []byte(`{"type":"s","data":["` + owner + `"]}` + "\n")}, nil
}

func (b *bus) properties(owner string) (execx.Result, error) {
	p := b.players[owner]
	if p == nil || p.gone {
		return refused("Failed to get property PlaybackStatus on interface " + playerIface + ": The name " + owner + " was not provided by any .service files")
	}
	return execx.Result{Stdout: fmt.Appendf(nil, "{\"type\":\"s\",\"data\":%q}\n{\"type\":\"b\",\"data\":%t}\n", p.status, p.canPause)}, nil
}

func (b *bus) method(owner, method string) (execx.Result, error) {
	p := b.players[owner]
	if p == nil || p.gone {
		return refused("Call failed: The name " + owner + " was not provided by any .service files")
	}
	b.calls = append(b.calls, owner+" "+method)
	switch method {
	case "Pause":
		p.status = "Paused"
	case "Play":
		p.status = "Playing"
	}
	return execx.Result{}, nil
}

// world is one run: the bus, the stderr, and what happens while the command holds.
type world struct {
	bus     *bus
	stderr  bytes.Buffer
	signals chan os.Signal
	// during runs at the first wait for the parent (the recording is under way).
	during  func(*bus)
	ppids   []int // what Getppid answers, call by call; the last answer repeats
	ppidAt  int
	waited  int
	runner  *execx.Fake
	timeout chan time.Time
}

func newWorld() *world {
	w := &world{bus: newBus(), signals: make(chan os.Signal, 1), ppids: []int{100}, timeout: make(chan time.Time)}
	w.runner = &execx.Fake{}
	w.runner.OnFunc([]string{"busctl"}, w.bus.answer)
	return w
}

func (w *world) env() Env {
	return Env{
		Runner: w.runner, Stderr: &w.stderr, Signals: w.signals,
		Getppid: func() int {
			pid := w.ppids[min(w.ppidAt, len(w.ppids)-1)]
			w.ppidAt++
			return pid
		},
		After: func(d time.Duration) <-chan time.Time {
			if d != parentPoll {
				panic(fmt.Sprintf("After(%v): only the parent poll waits", d))
			}
			w.waited++
			if w.waited == 1 && w.during != nil {
				w.during(w.bus)
			}
			ch := make(chan time.Time, 1)
			ch <- time.Time{}
			return ch
		},
	}
}

func (w *world) run(t *testing.T, args ...string) term.ExitCode {
	t.Helper()
	return App{Env: w.env()}.Run(context.Background(), args)
}

// stop sends the panel's release before the run, so it waits in the channel (M4).
func (w *world) stop() *world {
	w.signals <- syscall.SIGTERM
	return w
}

func wantCalls(t *testing.T, b *bus, want ...string) {
	t.Helper()
	if !slices.Equal(b.calls, want) {
		t.Fatalf("Pause/Play calls %q, want %q", b.calls, want)
	}
}

func wantSaid(t *testing.T, w *world, want string) {
	t.Helper()
	if !strings.Contains(w.stderr.String(), want) {
		t.Fatalf("stderr %q does not say %q", w.stderr.String(), want)
	}
}

func TestM1M2PausesOnlyThePlayingPlayersThatCanPauseByTheirUniqueNames(t *testing.T) {
	w := newWorld().stop()
	w.bus.add("org.mpris.MediaPlayer2.spotify", ":1.20", "Playing", true)
	w.bus.add("org.mpris.MediaPlayer2.vlc", ":1.30", "Paused", true)
	w.bus.add("org.mpris.MediaPlayer2.radio", ":1.40", "Playing", false)
	w.bus.add("org.mpris.MediaPlayer2.firefox.instance_1_9", ":1.50", "Playing", true)
	w.bus.add("org.gnome.Shell", ":1.60", "Playing", true) // not a player
	if code := w.run(t); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.stderr.String())
	}
	// Name order: firefox before spotify; each paused, then each resumed
	wantCalls(t, w.bus, ":1.50 Pause", ":1.20 Pause", ":1.50 Play", ":1.20 Play")
}

func TestM1TwoNamesOfOneConnectionAreOnePlayer(t *testing.T) {
	w := newWorld().stop()
	w.bus.add("org.mpris.MediaPlayer2.app", ":1.7", "Playing", true)
	w.bus.add("org.mpris.MediaPlayer2.app.instance2", ":1.7", "Playing", true)
	if code := w.run(t); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.stderr.String())
	}
	wantCalls(t, w.bus, ":1.7 Pause", ":1.7 Play")
}

func TestM3NothingPlayingExitsAtOnceWithoutWaiting(t *testing.T) {
	w := newWorld() // no signal: a wait would hang the test
	w.bus.add("org.mpris.MediaPlayer2.vlc", ":1.30", "Stopped", true)
	if code := w.run(t); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.stderr.String())
	}
	if w.waited != 0 {
		t.Fatalf("waited %d times with nothing paused", w.waited)
	}
	wantCalls(t, w.bus)
	wantSaid(t, w, "nothing is playing")
}

func TestM4WaitsForTheSignalThenResumes(t *testing.T) {
	for _, sig := range []os.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP} {
		w := newWorld()
		w.bus.add("org.mpris.MediaPlayer2.spotify", ":1.20", "Playing", true)
		polls := 0
		w.during = func(*bus) {}
		env := w.env()
		after := env.After
		env.After = func(d time.Duration) <-chan time.Time {
			polls++
			if polls == 3 {
				w.signals <- sig // the recording ends after two quiet polls
			}
			return after(d)
		}
		if code := (App{Env: env}).Run(context.Background(), nil); code != term.ExitOK {
			t.Fatalf("%v: exit %d, stderr %q", sig, code, w.stderr.String())
		}
		wantCalls(t, w.bus, ":1.20 Pause", ":1.20 Play")
		if polls < 3 {
			t.Fatalf("%v: resumed after %d polls, before the signal", sig, polls)
		}
	}
}

func TestM4TheParentEndingIsTheEndOfTheRecording(t *testing.T) {
	w := newWorld() // no signal
	w.ppids = []int{100, 100, 100, 1}
	w.bus.add("org.mpris.MediaPlayer2.spotify", ":1.20", "Playing", true)
	if code := w.run(t); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.stderr.String())
	}
	wantCalls(t, w.bus, ":1.20 Pause", ":1.20 Play")
	wantSaid(t, w, "parent")
}

func TestM5LeavesAPlayerTheOwnerChangedOrClosedDuringTheRecording(t *testing.T) {
	w := newWorld().stop()
	w.bus.add("org.mpris.MediaPlayer2.a", ":1.1", "Playing", true)
	w.bus.add("org.mpris.MediaPlayer2.b", ":1.2", "Playing", true)
	w.bus.add("org.mpris.MediaPlayer2.c", ":1.3", "Playing", true)
	w.bus.add("org.mpris.MediaPlayer2.d", ":1.4", "Playing", true)
	w.during = func(b *bus) {
		b.players[":1.1"].status = "Stopped"
		b.players[":1.2"].status = "Playing" // played again by hand
		b.players[":1.3"].gone = true
	}
	if code := w.run(t); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.stderr.String())
	}
	wantCalls(t, w.bus, ":1.1 Pause", ":1.2 Pause", ":1.3 Pause", ":1.4 Pause", ":1.4 Play")
	wantSaid(t, w, "org.mpris.MediaPlayer2.a is Stopped now")
	wantSaid(t, w, "org.mpris.MediaPlayer2.c has gone")
}

func TestM5APlayerReplacedUnderTheSameNameIsNotResumed(t *testing.T) {
	w := newWorld().stop()
	w.bus.add("org.mpris.MediaPlayer2.spotify", ":1.20", "Playing", true)
	w.during = func(b *bus) {
		b.players[":1.20"].gone = true
		b.add("org.mpris.MediaPlayer2.spotify", ":1.99", "Paused", true)
		b.names["org.mpris.MediaPlayer2.spotify"] = ":1.99"
	}
	if code := w.run(t); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.stderr.String())
	}
	wantCalls(t, w.bus, ":1.20 Pause")
}

func TestM6OneFailingPlayerIsReportedAndTheOthersStillPauseAndResume(t *testing.T) {
	w := newWorld().stop()
	w.bus.add("org.mpris.MediaPlayer2.a", ":1.1", "Playing", true)
	w.bus.add("org.mpris.MediaPlayer2.b", ":1.2", "Playing", true)
	w.bus.add("org.mpris.MediaPlayer2.c", ":1.3", "Playing", true)
	w.bus.fail[":1.1 "+mprisPath+" "+playerIface+" Pause"] = "Call failed: Not supported"
	w.bus.fail["GetNameOwner s org.mpris.MediaPlayer2.c"] = "Call failed: no such name"
	if code := w.run(t); code != term.ExitFailure {
		t.Fatalf("exit %d, want 1; stderr %q", code, w.stderr.String())
	}
	wantCalls(t, w.bus, ":1.2 Pause", ":1.2 Play")
	wantSaid(t, w, "wsi-pause-media: org.mpris.MediaPlayer2.a: busctl exited 1: Call failed: Not supported")
	wantSaid(t, w, "wsi-pause-media: org.mpris.MediaPlayer2.c: busctl exited 1: Call failed: no such name")
}

func TestM6AResumeThatFailsIsExitOneAndTheOthersAreStillResumed(t *testing.T) {
	for _, tc := range []struct{ suffix, words string }{
		{":1.1 " + mprisPath + " " + playerIface + " Play", "Call failed: Timeout"},
		{":1.1 " + mprisPath + " " + playerIface + " PlaybackStatus CanPause", "Failed to get property: Timeout"},
	} {
		w := newWorld().stop()
		w.bus.add("org.mpris.MediaPlayer2.a", ":1.1", "Playing", true)
		w.bus.add("org.mpris.MediaPlayer2.b", ":1.2", "Playing", true)
		w.during = func(b *bus) { b.fail[tc.suffix] = tc.words }
		if code := w.run(t); code != term.ExitFailure {
			t.Fatalf("%s: exit %d, want 1; stderr %q", tc.words, code, w.stderr.String())
		}
		wantSaid(t, w, "org.mpris.MediaPlayer2.a: busctl exited 1: "+tc.words)
		if !slices.Contains(w.bus.calls, ":1.2 Play") {
			t.Fatalf("%s: b was not resumed: %q", tc.words, w.bus.calls)
		}
	}
}

func TestM6ABusThatCannotBeListedAtTheEndIsExitOne(t *testing.T) {
	w := newWorld().stop()
	w.bus.add("org.mpris.MediaPlayer2.a", ":1.1", "Playing", true)
	w.during = func(b *bus) { b.fail["ListNames"] = "Failed to connect to bus" }
	if code := w.run(t); code != term.ExitFailure {
		t.Fatalf("exit %d, want 1; stderr %q", code, w.stderr.String())
	}
	wantCalls(t, w.bus, ":1.1 Pause")
	wantSaid(t, w, "cannot list the session bus, so nothing is resumed")
}

func TestM6ABusThatCannotBeListedIsExitOneWithNothingPaused(t *testing.T) {
	w := newWorld()
	w.bus.add("org.mpris.MediaPlayer2.a", ":1.1", "Playing", true)
	w.bus.fail["ListNames"] = "Failed to connect to user scope bus via local transport: No such file or directory"
	if code := w.run(t); code != term.ExitFailure {
		t.Fatalf("exit %d, want 1; stderr %q", code, w.stderr.String())
	}
	wantCalls(t, w.bus)
	wantSaid(t, w, "cannot list the session bus")
}

func TestM6BusctlThatCannotRunIsExitOne(t *testing.T) {
	w := newWorld()
	w.runner = &execx.Fake{}
	w.runner.OnFunc([]string{"busctl"}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{}, &execx.NotInstalledError{Name: "busctl"}
	})
	if code := w.run(t); code != term.ExitFailure {
		t.Fatalf("exit %d, want 1", code)
	}
	wantSaid(t, w, "busctl is not installed")
}

func TestM6RepliesThatAreNotBusctlsJSONAreFailures(t *testing.T) {
	for _, tc := range []struct{ name, verb, reply string }{
		{"list not JSON", "ListNames", `nope`},
		{"list of the wrong type", "ListNames", `{"type":"s","data":[["a"]]}`},
		{"list of no values", "ListNames", `{"type":"as","data":[]}`},
		{"list data not names", "ListNames", `{"type":"as","data":"x"}`},
		{"owner not JSON", "GetNameOwner", `nope`},
		{"owner not unique", "GetNameOwner", `{"type":"s","data":["not-unique"]}`},
		{"status of the wrong type", "get-property", `{"type":"b","data":true}`},
		{"status without CanPause", "get-property", `{"type":"s","data":"Playing"}`},
	} {
		w := newWorld()
		w.bus.add("org.mpris.MediaPlayer2.a", ":1.1", "Playing", true)
		answer := w.bus.answer
		w.runner.OnFunc([]string{"busctl"}, func(c execx.Cmd) (execx.Result, error) {
			if slices.Contains(c.Args, tc.verb) {
				return execx.Result{Stdout: []byte(tc.reply + "\n")}, nil
			}
			return answer(c)
		})
		name := tc.name
		if code := w.run(t); code != term.ExitFailure {
			t.Fatalf("%s: exit %d, want 1; stderr %q", name, code, w.stderr.String())
		}
		wantCalls(t, w.bus)
	}
}

func TestM7ArgumentsAreUsageErrorsAndHelpIsTheUsage(t *testing.T) {
	w := newWorld()
	if code := w.run(t, "--help"); code != term.ExitOK {
		t.Fatalf("--help: exit %d", code)
	}
	wantSaid(t, w, "usage: wsi-pause-media")
	for _, args := range [][]string{{"x"}, {"-h", "x"}, {"--verbose"}} {
		w := newWorld()
		if code := w.run(t, args...); code != term.ExitUsage {
			t.Fatalf("%q: exit %d, want 64", args, code)
		}
		if len(w.runner.Calls()) != 0 {
			t.Fatalf("%q: ran %d commands", args, len(w.runner.Calls()))
		}
	}
}

func TestM7AnUnwritableStderrIsExitOne(t *testing.T) {
	w := newWorld()
	env := w.env()
	env.Stderr = failingWriter{}
	if code := (App{Env: env}).Run(context.Background(), []string{"--help"}); code != term.ExitFailure {
		t.Fatalf("--help: exit %d, want 1", code)
	}
	// A hold whose every line is lost still pauses and resumes, and ends 1
	w = newWorld().stop()
	w.bus.add("org.mpris.MediaPlayer2.a", ":1.1", "Playing", true)
	env = w.env()
	env.Stderr = failingWriter{}
	if code := (App{Env: env}).Run(context.Background(), nil); code != term.ExitFailure {
		t.Fatalf("hold: exit %d, want 1", code)
	}
	wantCalls(t, w.bus, ":1.1 Pause", ":1.1 Play")
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestNewEnvIsTheRealProcess(t *testing.T) {
	signals := make(chan os.Signal)
	env := NewEnv(signals)
	if env.Getppid() != os.Getppid() || env.Signals != signals || env.Stderr != os.Stderr {
		t.Fatal("NewEnv is not this process")
	}
	if _, ok := env.Runner.(execx.OS); !ok {
		t.Fatalf("runner %T, want execx.OS", env.Runner)
	}
	if env.After(0) == nil {
		t.Fatal("After returned nil")
	}
}
