package wsimedia

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// player is one MPRIS connection on the fake bus.
type player struct {
	status   string
	canPause bool
	pid      int
	gone     bool // the connection has left the bus
}

// bus is a fake session bus answering busctl as the real one prints its JSON: names maps each
// well-known name to its owner's unique name, players each unique name to its state.
type bus struct {
	names   map[string]string
	players map[string]*player
	// fail makes the busctl run whose argv ends with the key fail with the value as its words.
	fail map[string]string
	// calls is each Pause or Play, as "<unique name> <method>".
	calls []string
}

func newBus() *bus {
	return &bus{names: map[string]string{}, players: map[string]*player{}, fail: map[string]string{}}
}

// add puts a player on the bus; its process ID is ten times the number after the dot of its
// unique name (":1.20" is process 200).
func (b *bus) add(name, owner, status string, canPause bool) {
	b.names[name] = owner
	if b.players[owner] == nil {
		n, err := strconv.Atoi(owner[strings.LastIndex(owner, ".")+1:])
		if err != nil {
			panic(err)
		}
		b.players[owner] = &player{status: status, canPause: canPause, pid: n * 10}
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
	call := []string{"call", dbusName, dbusPath, dbusName}
	switch {
	case slices.Equal(args, slices.Concat(call, []string{"ListNames"})):
		return b.listNames(), nil
	case len(args) == 7 && slices.Equal(args[:6], slices.Concat(call, []string{"GetNameOwner", "s"})):
		return b.owner(args[6])
	case len(args) == 7 && slices.Equal(args[:6], slices.Concat(call, []string{"GetConnectionUnixProcessID", "s"})):
		return b.pid(args[6])
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

func (b *bus) pid(owner string) (execx.Result, error) {
	p := b.players[owner]
	if p == nil || p.gone {
		return refused("Call failed: The connection does not exist")
	}
	return execx.Result{Stdout: fmt.Appendf(nil, "{\"type\":\"u\",\"data\":[%d]}\n", p.pid)}, nil
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

// fakeStream is one sink input on the fake sound server.
type fakeStream struct {
	index          int
	pid            string
	binary, name   string
	corked         bool
	channels       []string
	volume         []int
	gone           bool
	readBackOffset int // what the server's rounding adds to a volume it is set to
}

// audio is a fake pactl: the sink inputs as `pactl --format=json list sink-inputs` prints
// them, and set-sink-input-volume changing them.
type audio struct {
	streams []*fakeStream
	fail    map[string]string // as bus.fail
	// sets is each volume change, as "<index> <v>...".
	sets []string
}

func (a *audio) add(index int, pid string, volume ...int) *fakeStream {
	channels := []string{"front-left", "front-right"}
	if len(volume) == 1 {
		channels = []string{"mono"}
	}
	s := &fakeStream{index: index, pid: pid, channels: channels, volume: volume}
	a.streams = append(a.streams, s)
	return s
}

func (a *audio) find(index int) *fakeStream {
	for _, s := range a.streams {
		if s.index == index && !s.gone {
			return s
		}
	}
	return nil
}

func (a *audio) answer(c execx.Cmd) (execx.Result, error) {
	line := strings.Join(c.Args, " ")
	for key, words := range a.fail {
		if strings.HasSuffix(line, key) {
			return refused(words)
		}
	}
	switch {
	case slices.Equal(c.Args, []string{"--format=json", "list", "sink-inputs"}):
		return a.list(), nil
	case len(c.Args) > 2 && c.Args[0] == "set-sink-input-volume":
		return a.set(c.Args[1], c.Args[2:])
	}
	return execx.Result{}, fmt.Errorf("unscripted pactl: %s", line)
}

func (a *audio) set(index string, values []string) (execx.Result, error) {
	n, err := strconv.Atoi(index)
	if err != nil {
		return execx.Result{}, err
	}
	s := a.find(n)
	if s == nil {
		return refused("Failure: No such entity")
	}
	var vol []int
	for _, v := range values {
		raw, err := strconv.Atoi(v)
		if err != nil {
			return execx.Result{}, fmt.Errorf("not a raw volume: %s", v)
		}
		vol = append(vol, raw+s.readBackOffset)
	}
	if len(vol) != len(s.channels) {
		return refused("Failure: Invalid argument")
	}
	s.volume = vol
	a.sets = append(a.sets, index+" "+strings.Join(values, " "))
	return execx.Result{}, nil
}

func (a *audio) list() execx.Result {
	out := []map[string]any{}
	for _, s := range a.streams {
		if s.gone {
			continue
		}
		vol := map[string]any{}
		for i, ch := range s.channels {
			if i < len(s.volume) {
				vol[ch] = map[string]any{"value": s.volume[i], "value_percent": "x%", "db": "0 dB"}
			}
		}
		props := map[string]string{"media.name": "Playback"}
		for k, v := range map[string]string{"application.process.id": s.pid, "application.process.binary": s.binary, "application.name": s.name} {
			if v != "" {
				props[k] = v
			}
		}
		out = append(out, map[string]any{
			"index": s.index, "driver": "PipeWire", "client": "41", "sink": 56, "corked": s.corked, "mute": false,
			"channel_map": strings.Join(s.channels, ","), "volume": vol, "properties": props,
		})
	}
	data, err := json.Marshal(out)
	if err != nil {
		panic(err)
	}
	return execx.Result{Stdout: append(data, '\n')}
}

// world is one run: the bus, the sound server, the process table, the stderr, and what happens
// while the command holds.
type world struct {
	bus     *bus
	audio   *audio
	proc    fstest.MapFS
	stderr  bytes.Buffer
	signals chan os.Signal
	// during runs at the first wait for the parent (the recording is under way).
	during func(*world)
	ppids  []int // what Getppid answers, call by call; the last answer repeats
	ppidAt int
	waited int
	runner *execx.Fake
}

func newWorld() *world {
	w := &world{
		bus: newBus(), audio: &audio{fail: map[string]string{}}, proc: fstest.MapFS{},
		signals: make(chan os.Signal, 1), ppids: []int{100},
	}
	w.runner = &execx.Fake{}
	w.runner.OnFunc([]string{"busctl"}, w.bus.answer)
	w.runner.OnFunc([]string{"pactl"}, w.audio.answer)
	return w
}

// child records that pid's parent process is parent.
func (w *world) child(pid, parent int) {
	w.proc[strconv.Itoa(pid)+"/stat"] = &fstest.MapFile{Data: fmt.Appendf(nil, "%d (a (b) c) S %d 1 1 0 -1\n", pid, parent)}
}

func (w *world) env() Env {
	return Env{
		Runner: w.runner, Stderr: &w.stderr, Signals: w.signals, Proc: w.proc,
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
				w.during(w)
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

// stop sends the panel's release before the run, so it waits in the channel (V6).
func (w *world) stop() *world {
	w.signals <- syscall.SIGTERM
	return w
}

func wantExit(t *testing.T, w *world, got, want term.ExitCode) {
	t.Helper()
	if got != want {
		t.Fatalf("exit %d, want %d; stderr %q", got, want, w.stderr.String())
	}
}

func wantCalls(t *testing.T, b *bus, want ...string) {
	t.Helper()
	if !slices.Equal(b.calls, want) {
		t.Fatalf("Pause/Play calls %q, want %q", b.calls, want)
	}
}

func wantSets(t *testing.T, a *audio, want ...string) {
	t.Helper()
	if !slices.Equal(a.sets, want) {
		t.Fatalf("volume changes %q, want %q", a.sets, want)
	}
}

func wantSaid(t *testing.T, w *world, want string) {
	t.Helper()
	if !strings.Contains(w.stderr.String(), want) {
		t.Fatalf("stderr %q does not say %q", w.stderr.String(), want)
	}
}

func TestV1V2V3LowersTheStreamsOfOnlyThePlayingPlayersToAThirdAndRestoresThem(t *testing.T) {
	w := newWorld().stop()
	w.bus.add("org.mpris.MediaPlayer2.spotify", ":1.20", "Playing", true) // process 200
	w.bus.add("org.mpris.MediaPlayer2.vlc", ":1.30", "Paused", true)      // process 300
	w.bus.add("org.gnome.Shell", ":1.60", "Playing", true)                // not a player
	w.audio.add(42, "200", 45875, 45875)
	w.audio.add(43, "300", 65536, 65536)
	w.audio.add(44, "600", 65536, 65536)
	wantExit(t, w, w.run(t), term.ExitOK)
	wantSets(t, w.audio, "42 15292 15292", "42 45875 45875")
	wantCalls(t, w.bus)
	wantSaid(t, w, "lowered org.mpris.MediaPlayer2.spotify: stream 42 from 70.0% to 23.3%")
	wantSaid(t, w, "restored org.mpris.MediaPlayer2.spotify: stream 42 to 70.0%")
}

func TestV1PlayersAreTakenInNameOrderAndTwoNamesOfOneConnectionAreOnePlayer(t *testing.T) {
	w := newWorld().stop()
	w.bus.add("org.mpris.MediaPlayer2.spotify", ":1.20", "Playing", true)
	w.bus.add("org.mpris.MediaPlayer2.firefox.instance_1_9", ":1.50", "Playing", true)
	w.bus.add("org.mpris.MediaPlayer2.firefox.instance_1_10", ":1.50", "Playing", true)
	w.audio.add(1, "200", 300, 300)
	w.audio.add(2, "500", 600, 600)
	wantExit(t, w, w.run(t), term.ExitOK)
	wantSets(t, w.audio, "2 200 200", "1 100 100", "2 600 600", "1 300 300")
}

func TestV2AStreamFromADescendantOfThePlayersProcessIsItsStream(t *testing.T) {
	w := newWorld().stop()
	w.bus.add("org.mpris.MediaPlayer2.chromium.instance7", ":1.30", "Playing", true) // process 300
	w.child(310, 305)
	w.child(305, 300)
	w.child(400, 1)
	w.audio.add(5, "310", 3000, 3000)
	w.audio.add(6, "400", 3000, 3000) // another program's sound
	w.audio.add(7, "999", 3000, 3000) // a process gone from /proc
	wantExit(t, w, w.run(t), term.ExitOK)
	wantSets(t, w.audio, "5 1000 1000", "5 3000 3000")
}

func TestV2TheWalkUpTheProcessTreeIsBounded(t *testing.T) {
	w := newWorld().stop()
	w.bus.add("org.mpris.MediaPlayer2.a", ":1.1", "Playing", true) // process 10
	w.child(50, 51)
	w.child(51, 50) // a loop no real table has; the walk still ends
	w.audio.add(1, "50", 900, 900)
	wantExit(t, w, w.run(t), term.ExitOK)
	wantSets(t, w.audio)
	wantCalls(t, w.bus, ":1.1 Pause", ":1.1 Play")
}

func TestV7AStreamWhoseChannelsChangedIsLeftAsItIs(t *testing.T) {
	w := newWorld().stop()
	w.bus.add("org.mpris.MediaPlayer2.a", ":1.1", "Playing", true)
	w.audio.add(1, "10", 900, 900)
	w.during = func(w *world) {
		s := w.audio.find(1)
		s.channels, s.volume = []string{"mono"}, []int{300}
	}
	wantExit(t, w, w.run(t), term.ExitOK)
	wantSets(t, w.audio, "1 300 300")
	wantSaid(t, w, "stream 1 is at 0.5% now, not the 0.5% it was lowered to; left as it is")
}

func TestV2ACorkedStreamIsNotLowered(t *testing.T) {
	w := newWorld().stop()
	w.bus.add("org.mpris.MediaPlayer2.firefox", ":1.30", "Playing", true)
	w.audio.add(5, "300", 3000, 3000).corked = true
	w.audio.add(6, "300", 3000, 3000)
	wantExit(t, w, w.run(t), term.ExitOK)
	wantSets(t, w.audio, "6 1000 1000", "6 3000 3000")
}

func TestV2WithNoStreamOfItsProcessTheStreamsNamedAsThePlayerAreItsStreams(t *testing.T) {
	w := newWorld().stop()
	w.bus.add("org.mpris.MediaPlayer2.spotify", ":1.20", "Playing", true)
	w.bus.add("org.mpris.MediaPlayer2.Lollypop", ":1.40", "Playing", true)
	w.audio.add(8, "9000", 900, 900).binary = "spotify"
	w.audio.add(9, "9001", 900, 900).name = "lollypop"
	w.audio.add(10, "", 900, 900).name = "Spotify" // no process ID: matched by name only
	w.audio.add(11, "9003", 900, 900).name = "Totem"
	wantExit(t, w, w.run(t), term.ExitOK)
	wantSets(t, w.audio, "9 300 300", "8 300 300", "10 300 300", "9 900 900", "8 900 900", "10 900 900")
}

func TestV2TheNameIsUsedOnlyWhenNoStreamOfTheProcessIsFound(t *testing.T) {
	w := newWorld().stop()
	w.bus.add("org.mpris.MediaPlayer2.firefox", ":1.30", "Playing", true)
	w.audio.add(1, "300", 900, 900)
	w.audio.add(2, "777", 900, 900).binary = "firefox" // another firefox, not this player's
	wantExit(t, w, w.run(t), term.ExitOK)
	wantSets(t, w.audio, "1 300 300", "1 900 900")
}

func TestV2AStreamIsLoweredOnceForTheFirstPlayerThatClaimsIt(t *testing.T) {
	w := newWorld().stop()
	w.bus.add("org.mpris.MediaPlayer2.firefox", ":1.30", "Playing", true)
	w.bus.add("org.mpris.MediaPlayer2.firefox.instance2", ":1.40", "Playing", true)
	w.audio.add(1, "9999", 900, 900).binary = "firefox"
	wantExit(t, w, w.run(t), term.ExitOK)
	wantSets(t, w.audio, "1 300 300", "1 900 900")
	wantCalls(t, w.bus, ":1.40 Pause", ":1.40 Play") // the second found no stream left
}

func TestV3EachChannelIsLoweredOnItsOwnSoTheBalanceIsKept(t *testing.T) {
	w := newWorld().stop()
	w.bus.add("org.mpris.MediaPlayer2.a", ":1.1", "Playing", true)
	w.audio.add(1, "10", 60000, 30001)
	w.audio.add(2, "10", 7)
	wantExit(t, w, w.run(t), term.ExitOK)
	wantSets(t, w.audio, "1 20000 10000", "2 2", "1 60000 30001", "2 7")
}

func TestV4APlayingPlayerWithNoStreamIsPausedAndPlayedAgain(t *testing.T) {
	w := newWorld().stop()
	w.bus.add("org.mpris.MediaPlayer2.a", ":1.1", "Playing", true)
	w.bus.add("org.mpris.MediaPlayer2.b", ":1.2", "Playing", true)
	w.audio.add(1, "20", 900, 900)
	wantExit(t, w, w.run(t), term.ExitOK)
	wantCalls(t, w.bus, ":1.1 Pause", ":1.1 Play")
	wantSets(t, w.audio, "1 300 300", "1 900 900")
	wantSaid(t, w, "org.mpris.MediaPlayer2.a: no audio stream found; paused instead")
}

func TestV4V5APlayerThatCannotPauseAndHasNoStreamIsLeftAndNothingWaits(t *testing.T) {
	w := newWorld() // no signal: a wait would hang the test
	w.bus.add("org.mpris.MediaPlayer2.radio", ":1.4", "Playing", false)
	wantExit(t, w, w.run(t), term.ExitOK)
	if w.waited != 0 {
		t.Fatalf("waited %d times with nothing changed", w.waited)
	}
	wantCalls(t, w.bus)
	wantSaid(t, w, "org.mpris.MediaPlayer2.radio: no audio stream found, and it cannot pause; left as it is")
}

func TestV4APactlThatCannotBeReadPausesEveryPlayingPlayerAndIsExitOne(t *testing.T) {
	for _, tc := range []struct {
		name  string
		pactl func(execx.Cmd) (execx.Result, error)
		said  string
	}{
		{"not installed", func(execx.Cmd) (execx.Result, error) {
			return execx.Result{}, &execx.NotInstalledError{Name: "pactl"}
		}, "pactl is not installed"},
		{"no server", func(execx.Cmd) (execx.Result, error) {
			return refused("Connection failure: Connection refused")
		}, "pactl exited 1: Connection failure"},
		{"not JSON", func(execx.Cmd) (execx.Result, error) {
			return execx.Result{Stdout: []byte("Sink Input #42\n")}, nil
		}, "pactl's reply is not its JSON"},
	} {
		w := newWorld().stop()
		w.bus.add("org.mpris.MediaPlayer2.a", ":1.1", "Playing", true)
		w.bus.add("org.mpris.MediaPlayer2.b", ":1.2", "Playing", true)
		w.runner.OnFunc([]string{"pactl"}, tc.pactl)
		wantExit(t, w, w.run(t), term.ExitFailure)
		wantCalls(t, w.bus, ":1.1 Pause", ":1.2 Pause", ":1.1 Play", ":1.2 Play")
		wantSaid(t, w, "cannot read the audio streams, so the players are paused instead: "+tc.said)
	}
}

func TestV5NothingPlayingExitsAtOnceWithoutReadingTheStreams(t *testing.T) {
	w := newWorld()
	w.bus.add("org.mpris.MediaPlayer2.vlc", ":1.30", "Stopped", true)
	w.audio.add(1, "300", 900, 900)
	wantExit(t, w, w.run(t), term.ExitOK)
	if w.waited != 0 || w.runner.Called("pactl") {
		t.Fatalf("waited %d times, or ran pactl, with nothing playing", w.waited)
	}
	wantSaid(t, w, "nothing is playing; nothing to lower")
}

func TestV6WaitsForTheSignalThenRestores(t *testing.T) {
	for _, sig := range []os.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP} {
		w := newWorld()
		w.bus.add("org.mpris.MediaPlayer2.spotify", ":1.20", "Playing", true)
		w.audio.add(1, "200", 900, 900)
		polls := 0
		env := w.env()
		after := env.After
		env.After = func(d time.Duration) <-chan time.Time {
			polls++
			if polls == 3 {
				w.signals <- sig // the recording ends after two quiet polls
			}
			return after(d)
		}
		wantExit(t, w, App{Env: env}.Run(context.Background(), nil), term.ExitOK)
		wantSets(t, w.audio, "1 300 300", "1 900 900")
		if polls < 3 {
			t.Fatalf("%v: restored after %d polls, before the signal", sig, polls)
		}
	}
}

func TestV6TheParentEndingIsTheEndOfTheRecording(t *testing.T) {
	w := newWorld() // no signal
	w.ppids = []int{100, 100, 100, 1}
	w.bus.add("org.mpris.MediaPlayer2.spotify", ":1.20", "Playing", true)
	w.audio.add(1, "200", 900, 900)
	wantExit(t, w, w.run(t), term.ExitOK)
	wantSets(t, w.audio, "1 300 300", "1 900 900")
	wantSaid(t, w, "the parent process has ended")
}

func TestV7AStreamTheOwnerChangedOrThatEndedIsLeftAsItIs(t *testing.T) {
	w := newWorld().stop()
	w.bus.add("org.mpris.MediaPlayer2.a", ":1.1", "Playing", true)
	w.audio.add(1, "10", 900, 900) // changed by hand
	w.audio.add(2, "10", 900, 900) // ended
	w.audio.add(3, "10", 900, 900) // its index now another program's
	w.audio.add(4, "10", 900, 900) // untouched
	w.during = func(w *world) {
		w.audio.find(1).volume = []int{1200, 1200}
		w.audio.find(2).gone = true
		w.audio.find(3).pid = "4242"
	}
	wantExit(t, w, w.run(t), term.ExitOK)
	wantSets(t, w.audio, "1 300 300", "2 300 300", "3 300 300", "4 300 300", "4 900 900")
	wantSaid(t, w, "org.mpris.MediaPlayer2.a: stream 1 is at 1.8% now, not the 0.5% it was lowered to; left as it is")
	wantSaid(t, w, "org.mpris.MediaPlayer2.a: stream 2 has ended; nothing to restore")
	wantSaid(t, w, "org.mpris.MediaPlayer2.a: stream 3 has ended; nothing to restore")
}

func TestV7AVolumeWithinOnePercentOfTheOneSetIsTheServersRoundingAndIsRestored(t *testing.T) {
	for _, tc := range []struct {
		offset int
		sets   []string
	}{
		{655, []string{"1 15292 15292", "1 45875 45875"}},
		{-655, []string{"1 15292 15292", "1 45875 45875"}},
		{656, []string{"1 15292 15292"}},
	} {
		w := newWorld().stop()
		w.bus.add("org.mpris.MediaPlayer2.a", ":1.1", "Playing", true)
		w.audio.add(1, "10", 45875, 45875).readBackOffset = tc.offset
		w.during = func(w *world) { w.audio.find(1).readBackOffset = 0 }
		wantExit(t, w, w.run(t), term.ExitOK)
		wantSets(t, w.audio, tc.sets...)
	}
}

func TestV7V9AVolumeChangeThatFailsIsReportedAndIsExitOne(t *testing.T) {
	w := newWorld().stop()
	w.bus.add("org.mpris.MediaPlayer2.a", ":1.1", "Playing", true)
	w.audio.add(1, "10", 900, 900)
	w.audio.add(2, "10", 900, 900)
	w.audio.add(3, "10", 900, 900)
	w.audio.fail["set-sink-input-volume 1 300 300"] = "Failure: Access denied"
	w.audio.fail["set-sink-input-volume 3 900 900"] = "Failure: Timeout"
	wantExit(t, w, w.run(t), term.ExitFailure)
	wantSets(t, w.audio, "2 300 300", "3 300 300", "2 900 900")
	wantSaid(t, w, "wsi-lower-media-volume: org.mpris.MediaPlayer2.a: stream 1: pactl exited 1: Failure: Access denied")
	wantSaid(t, w, "wsi-lower-media-volume: org.mpris.MediaPlayer2.a: stream 3: pactl exited 1: Failure: Timeout")
}

func TestV7V9StreamsThatCannotBeListedAtTheEndAreExitOneAndThePlayersStillResume(t *testing.T) {
	w := newWorld().stop()
	w.bus.add("org.mpris.MediaPlayer2.a", ":1.1", "Playing", true)
	w.bus.add("org.mpris.MediaPlayer2.b", ":1.2", "Playing", true)
	w.audio.add(1, "10", 900, 900)
	w.during = func(w *world) { w.audio.fail["list sink-inputs"] = "Connection failure: Connection refused" }
	wantExit(t, w, w.run(t), term.ExitFailure)
	wantSets(t, w.audio, "1 300 300")
	wantCalls(t, w.bus, ":1.2 Pause", ":1.2 Play")
	wantSaid(t, w, "cannot read the audio streams, so no volume is restored: pactl exited 1")
}

func TestV8LeavesAPausedPlayerTheOwnerChangedOrClosedDuringTheRecording(t *testing.T) {
	w := newWorld().stop()
	for i, n := range []string{"a", "b", "c", "d"} {
		w.bus.add("org.mpris.MediaPlayer2."+n, ":1."+strconv.Itoa(i+1), "Playing", true)
	}
	w.during = func(w *world) {
		w.bus.players[":1.1"].status = "Stopped"
		w.bus.players[":1.2"].status = "Playing" // played again by hand
		w.bus.players[":1.3"].gone = true
	}
	wantExit(t, w, w.run(t), term.ExitOK)
	wantCalls(t, w.bus, ":1.1 Pause", ":1.2 Pause", ":1.3 Pause", ":1.4 Pause", ":1.4 Play")
	wantSaid(t, w, "org.mpris.MediaPlayer2.a is Stopped now")
	wantSaid(t, w, "org.mpris.MediaPlayer2.c has gone")
}

func TestV8APlayerReplacedUnderTheSameNameIsNotResumed(t *testing.T) {
	w := newWorld().stop()
	w.bus.add("org.mpris.MediaPlayer2.spotify", ":1.20", "Playing", true)
	w.during = func(w *world) {
		w.bus.players[":1.20"].gone = true
		w.bus.add("org.mpris.MediaPlayer2.spotify", ":1.99", "Paused", true)
		w.bus.names["org.mpris.MediaPlayer2.spotify"] = ":1.99"
	}
	wantExit(t, w, w.run(t), term.ExitOK)
	wantCalls(t, w.bus, ":1.20 Pause")
}

func TestV9APauseThatFailsIsReportedAndTheOthersStillPauseAndResume(t *testing.T) {
	w := newWorld().stop()
	w.bus.add("org.mpris.MediaPlayer2.a", ":1.1", "Playing", true)
	w.bus.add("org.mpris.MediaPlayer2.b", ":1.2", "Playing", true)
	w.bus.add("org.mpris.MediaPlayer2.c", ":1.3", "Playing", true)
	w.bus.fail[":1.1 "+mprisPath+" "+playerIface+" Pause"] = "Call failed: Not supported"
	wantExit(t, w, w.run(t), term.ExitFailure)
	wantCalls(t, w.bus, ":1.2 Pause", ":1.3 Pause", ":1.2 Play", ":1.3 Play")
	wantSaid(t, w, "wsi-lower-media-volume: org.mpris.MediaPlayer2.a: busctl exited 1: Call failed: Not supported")
}

func TestV9APlayerThatCannotBeReadIsSkippedWithOneLineAndTheRunStaysExitZero(t *testing.T) {
	w := newWorld().stop()
	for i, n := range []string{"a", "b", "c", "d"} {
		w.bus.add("org.mpris.MediaPlayer2."+n, ":1."+strconv.Itoa(i+1), "Playing", true)
	}
	w.bus.fail["GetNameOwner s org.mpris.MediaPlayer2.a"] = "Call failed: no such name"
	w.bus.fail[":1.3 "+mprisPath+" "+playerIface+" PlaybackStatus CanPause"] = "Failed to get property: no such object"
	w.bus.fail["GetConnectionUnixProcessID s :1.4"] = "Call failed: The connection does not exist"
	wantExit(t, w, w.run(t), term.ExitOK)
	wantCalls(t, w.bus, ":1.2 Pause", ":1.2 Play")
	wantSaid(t, w, "wsi-lower-media-volume: org.mpris.MediaPlayer2.a: skipped: busctl exited 1: Call failed: no such name")
	wantSaid(t, w, "wsi-lower-media-volume: org.mpris.MediaPlayer2.c: skipped: busctl exited 1: Failed to get property: no such object")
	wantSaid(t, w, "wsi-lower-media-volume: org.mpris.MediaPlayer2.d: skipped: busctl exited 1: Call failed: The connection does not exist")
}

func TestV9AStreamWhoseVolumeCannotBeReadIsSkippedAtTheStartAndAFailureAtTheEnd(t *testing.T) {
	w := newWorld().stop()
	w.bus.add("org.mpris.MediaPlayer2.a", ":1.1", "Playing", true)
	w.audio.add(1, "10", 900).channels = []string{"front-left", "front-right"} // one volume for two
	w.audio.add(2, "10", 900, 900)
	wantExit(t, w, w.run(t), term.ExitOK)
	wantSets(t, w.audio, "2 300 300", "2 900 900")
	wantSaid(t, w, "wsi-lower-media-volume: stream 1: skipped: no volume for its channel front-right")

	w = newWorld().stop()
	w.bus.add("org.mpris.MediaPlayer2.a", ":1.1", "Playing", true)
	w.audio.add(1, "10", 900, 900)
	w.during = func(w *world) { w.audio.find(1).channels = []string{} }
	wantExit(t, w, w.run(t), term.ExitFailure)
	wantSets(t, w.audio, "1 300 300")
	wantSaid(t, w, "org.mpris.MediaPlayer2.a: stream 1: no channels")
}

func TestV9AResumeThatFailsIsExitOneAndTheOthersAreStillResumed(t *testing.T) {
	for _, tc := range []struct{ suffix, words string }{
		{":1.1 " + mprisPath + " " + playerIface + " Play", "Call failed: Timeout"},
		{":1.1 " + mprisPath + " " + playerIface + " PlaybackStatus CanPause", "Failed to get property: Timeout"},
	} {
		w := newWorld().stop()
		w.bus.add("org.mpris.MediaPlayer2.a", ":1.1", "Playing", true)
		w.bus.add("org.mpris.MediaPlayer2.b", ":1.2", "Playing", true)
		w.during = func(w *world) { w.bus.fail[tc.suffix] = tc.words }
		wantExit(t, w, w.run(t), term.ExitFailure)
		wantSaid(t, w, "org.mpris.MediaPlayer2.a: busctl exited 1: "+tc.words)
		if !slices.Contains(w.bus.calls, ":1.2 Play") {
			t.Fatalf("%s: b was not resumed: %q", tc.words, w.bus.calls)
		}
	}
}

func TestV9ABusThatCannotBeListedAtTheEndIsExitOneAndTheVolumeIsStillRestored(t *testing.T) {
	w := newWorld().stop()
	w.bus.add("org.mpris.MediaPlayer2.a", ":1.1", "Playing", true)
	w.bus.add("org.mpris.MediaPlayer2.b", ":1.2", "Playing", true)
	w.audio.add(1, "10", 900, 900)
	w.during = func(w *world) { w.bus.fail["ListNames"] = "Failed to connect to bus" }
	wantExit(t, w, w.run(t), term.ExitFailure)
	wantCalls(t, w.bus, ":1.2 Pause")
	wantSets(t, w.audio, "1 300 300", "1 900 900")
	wantSaid(t, w, "cannot list the session bus, so nothing is resumed")
}

func TestV9ABusThatCannotBeListedIsExitOneWithNothingChanged(t *testing.T) {
	w := newWorld()
	w.bus.add("org.mpris.MediaPlayer2.a", ":1.1", "Playing", true)
	w.audio.add(1, "10", 900, 900)
	w.bus.fail["ListNames"] = "Failed to connect to user scope bus via local transport: No such file or directory"
	wantExit(t, w, w.run(t), term.ExitFailure)
	wantCalls(t, w.bus)
	wantSets(t, w.audio)
	wantSaid(t, w, "cannot list the session bus")
}

func TestV9BusctlThatCannotRunIsExitOne(t *testing.T) {
	w := newWorld()
	w.runner.OnFunc([]string{"busctl"}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{}, &execx.NotInstalledError{Name: "busctl"}
	})
	wantExit(t, w, w.run(t), term.ExitFailure)
	wantSaid(t, w, "busctl is not installed")
}

func TestV9RepliesThatAreNotBusctlsJSONAreFailures(t *testing.T) {
	for _, tc := range []struct{ name, verb, reply string }{
		{"list not JSON", "ListNames", `nope`},
		{"list of the wrong type", "ListNames", `{"type":"s","data":[["a"]]}`},
		{"list of no values", "ListNames", `{"type":"as","data":[]}`},
		{"list data not names", "ListNames", `{"type":"as","data":"x"}`},
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
		if code := w.run(t); code != term.ExitFailure {
			t.Fatalf("%s: exit %d, want 1; stderr %q", tc.name, code, w.stderr.String())
		}
		wantCalls(t, w.bus)
	}
}

func TestV9RepliesOfOnePlayerThatAreNotBusctlsJSONSkipThatPlayer(t *testing.T) {
	for _, tc := range []struct{ name, verb, reply string }{
		{"owner not JSON", "GetNameOwner", `nope`},
		{"owner not unique", "GetNameOwner", `{"type":"s","data":["not-unique"]}`},
		{"status of the wrong type", "get-property", `{"type":"b","data":true}`},
		{"status without CanPause", "get-property", `{"type":"s","data":"Playing"}`},
		{"process not JSON", "GetConnectionUnixProcessID", `nope`},
		{"process of no values", "GetConnectionUnixProcessID", `{"type":"u","data":[]}`},
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
		if code := w.run(t); code != term.ExitOK {
			t.Fatalf("%s: exit %d, want 0; stderr %q", tc.name, code, w.stderr.String())
		}
		wantCalls(t, w.bus)
		wantSaid(t, w, "org.mpris.MediaPlayer2.a: skipped: ")
	}
}

func TestV10ArgumentsAreUsageErrorsAndHelpIsTheUsage(t *testing.T) {
	w := newWorld()
	wantExit(t, w, w.run(t, "--help"), term.ExitOK)
	wantSaid(t, w, "usage: wsi-lower-media-volume")
	for _, args := range [][]string{{"x"}, {"-h", "x"}, {"--verbose"}} {
		w := newWorld()
		wantExit(t, w, w.run(t, args...), term.ExitUsage)
		if len(w.runner.Calls()) != 0 {
			t.Fatalf("%q: ran %d commands", args, len(w.runner.Calls()))
		}
	}
}

func TestV10AnUnwritableStderrIsExitOne(t *testing.T) {
	w := newWorld()
	env := w.env()
	env.Stderr = failingWriter{}
	if code := (App{Env: env}).Run(context.Background(), []string{"--help"}); code != term.ExitFailure {
		t.Fatalf("--help: exit %d, want 1", code)
	}
	// A hold whose every line is lost still lowers and restores, and ends 1
	w = newWorld().stop()
	w.bus.add("org.mpris.MediaPlayer2.a", ":1.1", "Playing", true)
	w.audio.add(1, "10", 900, 900)
	env = w.env()
	env.Stderr = failingWriter{}
	if code := (App{Env: env}).Run(context.Background(), nil); code != term.ExitFailure {
		t.Fatalf("hold: exit %d, want 1", code)
	}
	wantSets(t, w.audio, "1 300 300", "1 900 900")
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestParentOfReadsTheProcessTable(t *testing.T) {
	w := newWorld()
	w.child(310, 305)
	w.proc["7/stat"] = &fstest.MapFile{Data: []byte("7 (x) S\n")}
	w.proc["8/stat"] = &fstest.MapFile{Data: []byte("8 (x) S nine\n")}
	w.proc["9/stat"] = &fstest.MapFile{Data: []byte("9 no parenthesis\n")}
	if got, err := parentOf(w.proc, 310); err != nil || got != 305 {
		t.Fatalf("parentOf(310) = %d, %v; want 305", got, err)
	}
	for _, pid := range []int{6, 7, 8, 9} {
		if _, err := parentOf(w.proc, pid); err == nil {
			t.Fatalf("parentOf(%d): no error", pid)
		}
	}
}

func TestNewEnvIsTheRealProcess(t *testing.T) {
	signals := make(chan os.Signal)
	env := NewEnv(signals)
	if env.Getppid() != os.Getppid() || env.Signals != signals || env.Stderr != os.Stderr {
		t.Fatal("NewEnv is not this process")
	}
	if _, ok := env.Runner.(execx.OS); !ok {
		t.Fatalf("runner %T, want execx.OS", env.Runner)
	}
	if got, err := parentOf(env.Proc, os.Getpid()); err != nil || got != os.Getppid() {
		t.Fatalf("this process's parent from /proc: %d, %v; want %d", got, err, os.Getppid())
	}
	if env.After(0) == nil {
		t.Fatal("After returned nil")
	}
}
