// Package wsimedia is wsi-pause-media: the media players that are playing when a
// speech-to-text recording starts are paused for it, and played again when it is over (Plan
// 00212). The panel starts it beside each recorder and sends it TERM once the microphone has
// closed; it reaches the players over MPRIS on the session bus through busctl.
//
// What it does, numbered as its tests are:
//
//   - M1. The players are the session-bus names beginning `org.mpris.MediaPlayer2.`
//     (ListNames), in name order. Each is reached through its unique name (GetNameOwner), so
//     a player that quits and is replaced under the same name is a different player; two
//     names owned by one connection are one player.
//   - M2. A player whose PlaybackStatus is Playing and whose CanPause is true is paused
//     (Pause on its unique name) and remembered. Any other player is not touched.
//   - M3. Nothing paused: exit at once, since there is nothing to resume.
//   - M4. Otherwise it waits until told the recording is over: SIGTERM, SIGINT or SIGHUP, or
//     its parent process ending (polled each second: GNOME Shell gone). A signal that arrives
//     while it is still pausing waits in the channel and is acted on once the pausing is done.
//   - M5. Each remembered player is played again (Play) only if its unique name is still on
//     the bus and its PlaybackStatus is Paused. Gone, Stopped or Playing (changed by hand
//     during the recording) is left as it is, with a line on stderr.
//   - M6. A busctl failure on one player is reported (`wsi-pause-media: <player>: <busctl's
//     words>`) and the others are still paused or resumed, each being independent; the exit
//     is then 1. A bus that cannot be listed is exit 1, with nothing paused.
//   - M7. Any argument but a lone -h/--help is a usage error, exit 64, nothing run. stdout is
//     never written; every message goes to stderr.
package wsimedia

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	tool        = "wsi-pause-media"
	usage       = "usage: wsi-pause-media   (pauses the media players that are playing until TERM, then plays them again)\n"
	busctl      = "busctl"
	dbusName    = "org.freedesktop.DBus"
	dbusPath    = "/org/freedesktop/DBus"
	mprisPrefix = "org.mpris.MediaPlayer2."
	mprisPath   = "/org/mpris/MediaPlayer2"
	playerIface = "org.mpris.MediaPlayer2.Player"
	playing     = "Playing"
	paused      = "Paused"
	// parentPoll is how often the parent is looked for: GNOME Shell gone ends the hold.
	parentPoll = time.Second
)

// busArgs are on every busctl run: the session bus, JSON replies, and a player that does not
// answer is a failure rather than a hang.
var busArgs = []string{"--user", "--json=short", "--timeout=5"}

// Env is everything a run touches outside its own memory.
type Env struct {
	Runner execx.Runner
	Stderr io.Writer
	// Signals carries TERM, INT and HUP; the main subscribes before anything runs (M4).
	Signals <-chan os.Signal
	Getppid func() int
	After   func(time.Duration) <-chan time.Time
}

// NewEnv is the production environment, reading signals from signals.
func NewEnv(signals <-chan os.Signal) Env {
	return Env{Runner: execx.OS{}, Stderr: os.Stderr, Signals: signals, Getppid: os.Getppid, After: time.After}
}

// App is one invocation.
type App struct {
	Env Env
}

// held is one paused player: the name it was found by and the connection that owns it.
type held struct {
	name, owner string
}

// run is one invocation's state: whether anything failed, and the first stderr write error.
type run struct {
	env      Env
	failed   bool
	writeErr error
}

// Run is M1-M7.
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
	players, err := r.pause(ctx)
	if err != nil {
		r.say("cannot list the session bus: " + err.Error())
		return r.exit(term.ExitFailure)
	}
	if len(players) == 0 {
		r.say("nothing is playing; nothing to pause")
		return r.exit(term.ExitOK)
	}
	r.wait(parent)
	r.resume(ctx, players)
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

// problem is M6: one player's failure, reported; the run goes on and ends 1.
func (r *run) problem(name string, err error) {
	r.failed = true
	r.say(name + ": " + err.Error())
}

// pause is M1 and M2: the players paused, in name order.
func (r *run) pause(ctx context.Context) ([]held, error) {
	names, err := r.listNames(ctx)
	if err != nil {
		return nil, err
	}
	var out []held
	seen := map[string]bool{}
	for _, name := range names {
		if !strings.HasPrefix(name, mprisPrefix) {
			continue
		}
		owner, err := r.owner(ctx, name)
		if err != nil {
			r.problem(name, err)
			continue
		}
		if seen[owner] {
			continue
		}
		seen[owner] = true
		status, canPause, err := r.state(ctx, owner)
		if err != nil {
			r.problem(name, err)
			continue
		}
		if status != playing || !canPause {
			continue
		}
		if err := r.player(ctx, owner, "Pause"); err != nil {
			r.problem(name, err)
			continue
		}
		r.say("paused " + name)
		out = append(out, held{name: name, owner: owner})
	}
	return out, nil
}

// wait is M4.
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

// resume is M5.
func (r *run) resume(ctx context.Context, players []held) {
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
