// Package wsimedia is wsi-lower-media-volume: the media that is playing when a speech-to-text
// recording starts is lowered to a third of its volume for it, and its volume restored when it
// is over (Plan 00212). The panel starts it beside each recorder and sends it TERM once the
// microphone has closed. The players are found over MPRIS on the session bus through busctl,
// their sound as the sound server's sink inputs through pactl; the output (sink) volume is
// never touched.
//
// What it does, numbered as its tests are:
//
//   - V1. The players are the session-bus names beginning `org.mpris.MediaPlayer2.`
//     (ListNames), in name order. Each is reached through its unique name (GetNameOwner), so
//     a player that quits and is replaced under the same name is a different player; two
//     names owned by one connection are one player.
//   - V2. A player whose PlaybackStatus is Playing is playing; any other is not touched. Its
//     streams are the sink inputs (`pactl --format=json list sink-inputs`, read once before
//     any change) that are not corked and whose application.process.id is the player's
//     process (GetConnectionUnixProcessID) or a descendant of it, read from /proc. When that
//     finds none, those whose application.process.binary or application.name is, ignoring
//     case, the first word of the player's name after the prefix. A stream is lowered once,
//     for the first player that claims it.
//   - V3. Each of its streams is lowered to a third of its volume, each channel rounded to the
//     nearest unit (`pactl set-sink-input-volume <index> <v>...` in channel_map order), and
//     the volume before and after is remembered with the stream's index and process.
//   - V4. A playing player with no stream, or every playing player when pactl cannot be read,
//     is paused (Pause) if its CanPause is true, and remembered; else it is left playing.
//   - V5. Nothing lowered and nothing paused: exit at once.
//   - V6. Otherwise it waits until told the recording is over: SIGTERM, SIGINT or SIGHUP, or
//     its parent process ending (polled each second: GNOME Shell gone). A signal that arrives
//     while it is still lowering waits in the channel and is acted on once that is done.
//   - V7. The sink inputs are listed again. Each lowered stream still there (same index, same
//     process) whose every channel is within 1% (655 of 65536) of what it was set to is set
//     back to exactly the volume before; one at another volume (the owner changed it) is left,
//     and one gone is skipped, each with a line on stderr.
//   - V8. Each paused player is played again (Play) only if its unique name is still on the
//     bus and its PlaybackStatus is Paused. Gone, Stopped or Playing (changed by hand during
//     the recording) is left as it is, with a line on stderr.
//   - V9. A player or stream that cannot be read (no owner, no object, no CanPause, no
//     process, a channel with no volume) is skipped with one stderr line (`<tool>: <player>:
//     skipped: <words>`) and the others carry on; the exit stays 0. A Pause, Play or volume
//     change that fails, pactl that cannot be read, or a state that cannot be read at the end,
//     is reported the same way without "skipped", the others still go on, and the exit is 1.
//     A bus that cannot be listed (busctl missing, no session bus) is exit 1, nothing changed.
//   - V10. Any argument but a lone -h/--help is a usage error, exit 64, nothing run. stdout is
//     never written; every message goes to stderr.
package wsimedia

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	tool        = "wsi-lower-media-volume"
	usage       = "usage: wsi-lower-media-volume   (lowers the media that is playing to a third of its volume until TERM, then restores it)\n"
	busctl      = "busctl"
	pactl       = "pactl"
	dbusName    = "org.freedesktop.DBus"
	dbusPath    = "/org/freedesktop/DBus"
	mprisPrefix = "org.mpris.MediaPlayer2."
	mprisPath   = "/org/mpris/MediaPlayer2"
	playerIface = "org.mpris.MediaPlayer2.Player"
	playing     = "Playing"
	paused      = "Paused"
	// parentPoll is how often the parent is looked for: GNOME Shell gone ends the hold.
	parentPoll = time.Second
	// volumeNorm is pactl's raw volume for 100%.
	volumeNorm = 65536
	// tolerance is how far a stream's volume may be from the one set and still be ours (V7):
	// the server keeps volumes as floats, so a raw volume read back can differ by a unit or two.
	tolerance = volumeNorm / 100
	// maxAncestors bounds the walk up the process tree (V2).
	maxAncestors = 64
)

// busArgs are on every busctl run: the session bus, JSON replies, and a player that does not
// answer is a failure rather than a hang.
var busArgs = []string{"--user", "--json=short", "--timeout=5"}

// Env is everything a run touches outside its own memory.
type Env struct {
	Runner execx.Runner
	Stderr io.Writer
	// Signals carries TERM, INT and HUP; the main subscribes before anything runs (V6).
	Signals <-chan os.Signal
	Getppid func() int
	// Proc is /proc: a stream's process is walked up to its ancestors (V2).
	Proc  fs.FS
	After func(time.Duration) <-chan time.Time
}

// NewEnv is the production environment, reading signals from signals.
func NewEnv(signals <-chan os.Signal) Env {
	return Env{
		Runner: execx.OS{}, Stderr: os.Stderr, Signals: signals, Getppid: os.Getppid,
		Proc: os.DirFS("/proc"), After: time.After,
	}
}

// App is one invocation.
type App struct {
	Env Env
}

// mpris is one playing player: the name it was found by, the connection that owns it and
// that connection's process.
type mpris struct {
	name, owner string
	canPause    bool
	pid         int
}

// stream is one sink input as read from pactl.
type stream struct {
	index        int
	corked       bool
	pid          string
	binary, name string
	volume       []int
}

// lowered is one stream this run lowered, with the volume before and the volume it set.
type lowered struct {
	player        string
	index         int
	pid           string
	before, after []int
}

// run is one invocation's state: whether anything failed, and the first stderr write error.
type run struct {
	env      Env
	failed   bool
	writeErr error
}

// Run is V1-V10.
func (a App) Run(ctx context.Context, args []string) term.ExitCode {
	r := &run{env: a.Env}
	switch {
	case len(args) == 1 && (args[0] == "-h" || args[0] == "--help"):
		r.write(usage)
		return r.exit(term.ExitOK)
	case len(args) > 0:
		r.write(usage)
		return r.exit(term.ExitUsage)
	}
	parent := a.Env.Getppid()
	players, err := r.playing(ctx)
	if err != nil {
		r.say("cannot list the session bus: " + err.Error())
		return r.exit(term.ExitFailure)
	}
	if len(players) == 0 {
		r.say("nothing is playing; nothing to lower")
		return r.exit(term.ExitOK)
	}
	lows, held := r.lower(ctx, players)
	if len(lows) == 0 && len(held) == 0 {
		r.say("nothing lowered or paused; nothing to restore")
		return r.exit(term.ExitOK)
	}
	r.wait(parent)
	r.restore(ctx, lows)
	r.resume(ctx, held)
	return r.exit(term.ExitOK)
}

// exit is code, or 1 once stderr could not be written, or once anything failed in a run that
// would otherwise succeed.
func (r *run) exit(code term.ExitCode) term.ExitCode {
	if r.writeErr != nil || (code == term.ExitOK && r.failed) {
		return term.ExitFailure
	}
	return code
}

func (r *run) write(s string) {
	if r.writeErr != nil {
		return
	}
	if _, err := io.WriteString(r.env.Stderr, s); err != nil {
		r.writeErr = err
	}
}

func (r *run) say(msg string) { r.write(tool + ": " + msg + "\n") }

// problem is V9: one player's or stream's failure, reported; the run goes on and ends 1.
func (r *run) problem(name string, err error) {
	r.failed = true
	r.say(name + ": " + err.Error())
}

// skip is V9's other half: what cannot be read is named on stderr and left out; exit stays 0.
func (r *run) skip(name string, err error) { r.say(name + ": skipped: " + err.Error()) }

// playing is V1 and V2's first half: the playing players, in name order, with their processes.
func (r *run) playing(ctx context.Context) ([]mpris, error) {
	names, err := r.listNames(ctx)
	if err != nil {
		return nil, err
	}
	var out []mpris
	seen := map[string]bool{}
	for _, name := range names {
		if !strings.HasPrefix(name, mprisPrefix) {
			continue
		}
		owner, err := r.owner(ctx, name)
		if err != nil {
			r.skip(name, err)
			continue
		}
		if seen[owner] {
			continue
		}
		seen[owner] = true
		status, canPause, err := r.state(ctx, owner)
		if err != nil {
			r.skip(name, err)
			continue
		}
		if status != playing {
			continue
		}
		pid, err := r.processOf(ctx, owner)
		if err != nil {
			r.skip(name, err)
			continue
		}
		out = append(out, mpris{name: name, owner: owner, canPause: canPause, pid: pid})
	}
	return out, nil
}

// lower is V2-V4: each player's streams lowered, or the player paused when none is found.
func (r *run) lower(ctx context.Context, players []mpris) ([]lowered, []held) {
	streams, unreadable, err := r.streams(ctx)
	if err != nil {
		r.failed = true
		r.say("cannot read the audio streams, so the players are paused instead: " + err.Error())
	}
	for _, index := range slices.Sorted(maps.Keys(unreadable)) {
		r.skip("stream "+strconv.Itoa(index), unreadable[index])
	}
	var lows []lowered
	var holds []held
	claimed := map[int]bool{}
	for _, p := range players {
		mine := r.streamsOf(p, streams, claimed)
		if len(mine) == 0 {
			if h, ok := r.pauseInstead(ctx, p, err == nil); ok {
				holds = append(holds, h)
			}
			continue
		}
		for _, s := range mine {
			after := make([]int, len(s.volume))
			for i, v := range s.volume {
				after[i] = (v + 1) / 3
			}
			if err := r.setVolume(ctx, s.index, after); err != nil {
				r.problem(p.name, fmt.Errorf("stream %d: %w", s.index, err))
				continue
			}
			r.say(fmt.Sprintf("lowered %s: stream %d from %s to %s", p.name, s.index, percent(s.volume), percent(after)))
			lows = append(lows, lowered{player: p.name, index: s.index, pid: s.pid, before: s.volume, after: after})
		}
	}
	return lows, holds
}

// pauseInstead is V4: a player whose sound was not found is paused, if it can be.
func (r *run) pauseInstead(ctx context.Context, p mpris, streamsRead bool) (held, bool) {
	if !p.canPause {
		r.say(p.name + ": no audio stream found, and it cannot pause; left as it is")
		return held{}, false
	}
	if streamsRead {
		r.say(p.name + ": no audio stream found; paused instead")
	}
	if err := r.player(ctx, p.owner, "Pause"); err != nil {
		r.problem(p.name, err)
		return held{}, false
	}
	r.say("paused " + p.name)
	return held{name: p.name, owner: p.owner}, true
}

// streamsOf is V2's second half: the uncorked streams of p's process tree, or else of its name,
// that no earlier player claimed; they are claimed for p.
func (r *run) streamsOf(p mpris, streams []stream, claimed map[int]bool) []stream {
	free := func(s stream) bool { return !s.corked && !claimed[s.index] }
	var mine []stream
	for _, s := range streams {
		if free(s) && r.descends(s.pid, p.pid) {
			mine = append(mine, s)
		}
	}
	if len(mine) == 0 {
		word := strings.ToLower(strings.SplitN(strings.TrimPrefix(p.name, mprisPrefix), ".", 2)[0])
		for _, s := range streams {
			if free(s) && (strings.ToLower(s.binary) == word || strings.ToLower(s.name) == word) {
				mine = append(mine, s)
			}
		}
	}
	for _, s := range mine {
		claimed[s.index] = true
	}
	return mine
}

// descends is whether the process pid (a stream's, as pactl prints it) is ancestor or one of
// its descendants. A process that cannot be read is not.
func (r *run) descends(pid string, ancestor int) bool {
	n, err := strconv.Atoi(pid)
	if err != nil {
		return false
	}
	for range maxAncestors {
		if n == ancestor {
			return true
		}
		if n <= 1 {
			return false
		}
		if n, err = parentOf(r.env.Proc, n); err != nil {
			return false
		}
	}
	return false
}

// wait is V6.
func (r *run) wait(parent int) {
	for {
		select {
		case sig := <-r.env.Signals:
			r.say(sig.String() + ": the recording is over")
			return
		case <-r.env.After(parentPoll):
			if r.env.Getppid() != parent {
				r.say("the parent process has ended: the recording is over")
				return
			}
		}
	}
}

// restore is V7.
func (r *run) restore(ctx context.Context, lows []lowered) {
	if len(lows) == 0 {
		return
	}
	streams, unreadable, err := r.streams(ctx)
	if err != nil {
		r.failed = true
		r.say("cannot read the audio streams, so no volume is restored: " + err.Error())
		return
	}
	for _, l := range lows {
		if err, bad := unreadable[l.index]; bad {
			r.problem(l.player, fmt.Errorf("stream %d: %w", l.index, err))
			continue
		}
		i := slices.IndexFunc(streams, func(s stream) bool { return s.index == l.index && s.pid == l.pid })
		if i < 0 {
			r.say(fmt.Sprintf("%s: stream %d has ended; nothing to restore", l.player, l.index))
			continue
		}
		if now := streams[i].volume; !near(now, l.after) {
			r.say(fmt.Sprintf("%s: stream %d is at %s now, not the %s it was lowered to; left as it is",
				l.player, l.index, percent(now), percent(l.after)))
			continue
		}
		if err := r.setVolume(ctx, l.index, l.before); err != nil {
			r.problem(l.player, fmt.Errorf("stream %d: %w", l.index, err))
			continue
		}
		r.say(fmt.Sprintf("restored %s: stream %d to %s", l.player, l.index, percent(l.before)))
	}
}

// near is whether every channel of now is within tolerance of set.
func near(now, set []int) bool {
	if len(now) != len(set) {
		return false
	}
	for i := range now {
		if d := now[i] - set[i]; d > tolerance || d < -tolerance {
			return false
		}
	}
	return true
}

// percent is a volume as a person reads it: its first channel, in percent.
func percent(volume []int) string {
	return strconv.FormatFloat(float64(volume[0])*100/volumeNorm, 'f', 1, 64) + "%"
}

// held is one paused player: the name it was found by and the connection that owns it.
type held struct {
	name, owner string
}

// resume is V8.
func (r *run) resume(ctx context.Context, players []held) {
	if len(players) == 0 {
		return
	}
	names, err := r.listNames(ctx)
	if err != nil {
		r.failed = true
		r.say("cannot list the session bus, so nothing is resumed: " + err.Error())
		return
	}
	for _, p := range players {
		if !slices.Contains(names, p.owner) {
			r.say(p.name + " has gone; nothing to resume")
			continue
		}
		status, _, err := r.state(ctx, p.owner)
		if err != nil {
			r.problem(p.name, err)
			continue
		}
		if status != paused {
			r.say(p.name + " is " + status + " now; left as it is")
			continue
		}
		if err := r.player(ctx, p.owner, "Play"); err != nil {
			r.problem(p.name, err)
			continue
		}
		r.say("resumed " + p.name)
	}
}

// sinkInput is the part of one pactl JSON sink input this reads.
type sinkInput struct {
	Index      int    `json:"index"`
	Corked     bool   `json:"corked"`
	ChannelMap string `json:"channel_map"`
	Volume     map[string]struct {
		Value int `json:"value"`
	} `json:"volume"`
	Properties struct {
		PID    string `json:"application.process.id"`
		Binary string `json:"application.process.binary"`
		Name   string `json:"application.name"`
	} `json:"properties"`
}

// streams is every sink input pactl lists, and those whose volume cannot be read with why.
func (r *run) streams(ctx context.Context) ([]stream, map[int]error, error) {
	res, err := r.env.Runner.Run(ctx, execx.Cmd{Name: pactl, Args: []string{"--format=json", "list", "sink-inputs"}})
	if err != nil {
		return nil, nil, err
	}
	var inputs []sinkInput
	if err := json.Unmarshal(res.Stdout, &inputs); err != nil {
		return nil, nil, fmt.Errorf("pactl's reply is not its JSON: %w", err)
	}
	var out []stream
	unreadable := map[int]error{}
	for _, in := range inputs {
		volume, err := in.volumes()
		if err != nil {
			unreadable[in.Index] = err
			continue
		}
		out = append(out, stream{
			index: in.Index, corked: in.Corked, volume: volume,
			pid: in.Properties.PID, binary: in.Properties.Binary, name: in.Properties.Name,
		})
	}
	return out, unreadable, nil
}

// volumes is the raw volume of each channel, in channel_map order.
func (in sinkInput) volumes() ([]int, error) {
	if in.ChannelMap == "" {
		return nil, errors.New("no channels")
	}
	var out []int
	for ch := range strings.SplitSeq(in.ChannelMap, ",") {
		v, ok := in.Volume[ch]
		if !ok {
			return nil, fmt.Errorf("no volume for its channel %s", ch)
		}
		out = append(out, v.Value)
	}
	return out, nil
}

// setVolume sets a stream's raw volume, channel by channel.
func (r *run) setVolume(ctx context.Context, index int, volume []int) error {
	args := make([]string, 0, 2+len(volume))
	args = append(args, "set-sink-input-volume", strconv.Itoa(index))
	for _, v := range volume {
		args = append(args, strconv.Itoa(v))
	}
	_, err := r.env.Runner.Run(ctx, execx.Cmd{Name: pactl, Args: args})
	return err
}

// parentOf is the parent of process pid, from <pid>/stat in proc. The command name, field 2,
// is in parentheses and may hold spaces and parentheses, so the fields after it are read
// from after the last ')'.
func parentOf(proc fs.FS, pid int) (int, error) {
	data, err := fs.ReadFile(proc, strconv.Itoa(pid)+"/stat")
	if err != nil {
		return 0, err
	}
	end := bytes.LastIndexByte(data, ')')
	if end < 0 {
		return 0, fmt.Errorf("process %d: its stat has no command name", pid)
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 2 {
		return 0, fmt.Errorf("process %d: its stat has no parent", pid)
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, fmt.Errorf("process %d: its parent %q is not a number", pid, fields[1])
	}
	return ppid, nil
}

// busctl runs busctl on the session bus; a failure is its ExitError, which carries its words.
func (r *run) busctl(ctx context.Context, args ...string) ([]byte, error) {
	res, err := r.env.Runner.Run(ctx, execx.Cmd{Name: busctl, Args: slices.Concat(busArgs, args)})
	if err != nil {
		return nil, err
	}
	return res.Stdout, nil
}

// reply is one busctl JSON value.
type reply[T any] struct {
	Type string `json:"type"`
	Data T      `json:"data"`
}

func decode[T any](d *json.Decoder, typ string) (T, error) {
	var v reply[T]
	if err := d.Decode(&v); err != nil {
		var zero T
		return zero, fmt.Errorf("busctl's reply is not its JSON: %w", err)
	}
	if v.Type != typ {
		var zero T
		return zero, fmt.Errorf("busctl's reply is of type %q, not %q", v.Type, typ)
	}
	return v.Data, nil
}

// listNames is every name on the session bus, in order.
func (r *run) listNames(ctx context.Context) ([]string, error) {
	out, err := r.busctl(ctx, "call", dbusName, dbusPath, dbusName, "ListNames")
	if err != nil {
		return nil, err
	}
	data, err := decode[[][]string](json.NewDecoder(bytes.NewReader(out)), "as")
	if err != nil {
		return nil, err
	}
	if len(data) != 1 {
		return nil, fmt.Errorf("ListNames answered %d values, not 1", len(data))
	}
	names := slices.Clone(data[0])
	slices.Sort(names)
	return names, nil
}

// owner is the unique name of the connection that owns name.
func (r *run) owner(ctx context.Context, name string) (string, error) {
	out, err := r.busctl(ctx, "call", dbusName, dbusPath, dbusName, "GetNameOwner", "s", name)
	if err != nil {
		return "", err
	}
	data, err := decode[[]string](json.NewDecoder(bytes.NewReader(out)), "s")
	if err != nil {
		return "", err
	}
	if len(data) != 1 || !strings.HasPrefix(data[0], ":") {
		return "", fmt.Errorf("GetNameOwner answered %q, not one unique name", data)
	}
	return data[0], nil
}

// processOf is the process ID of the connection owner.
func (r *run) processOf(ctx context.Context, owner string) (int, error) {
	out, err := r.busctl(ctx, "call", dbusName, dbusPath, dbusName, "GetConnectionUnixProcessID", "s", owner)
	if err != nil {
		return 0, err
	}
	data, err := decode[[]int](json.NewDecoder(bytes.NewReader(out)), "u")
	if err != nil {
		return 0, err
	}
	if len(data) != 1 {
		return 0, fmt.Errorf("GetConnectionUnixProcessID answered %d values, not 1", len(data))
	}
	return data[0], nil
}

// state is a player's PlaybackStatus and CanPause.
func (r *run) state(ctx context.Context, owner string) (string, bool, error) {
	out, err := r.busctl(ctx, "get-property", owner, mprisPath, playerIface, "PlaybackStatus", "CanPause")
	if err != nil {
		return "", false, err
	}
	d := json.NewDecoder(bytes.NewReader(out))
	status, err := decode[string](d, "s")
	if err != nil {
		return "", false, err
	}
	canPause, err := decode[bool](d, "b")
	if err != nil {
		return "", false, errors.Join(errors.New("no CanPause in the reply"), err)
	}
	return status, canPause, nil
}

// player calls a method of the MPRIS Player interface on the connection owner.
func (r *run) player(ctx context.Context, owner, method string) error {
	_, err := r.busctl(ctx, "call", owner, mprisPath, playerIface, method)
	return err
}
