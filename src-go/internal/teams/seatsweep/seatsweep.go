// Package seatsweep is `ccy-teams seats sweep` and the owner's user units that run it once a
// minute (Plan 00209 DESIGN-v2-C-host.md 3.7.5, decision DC21; LAUNCH-ccy.md item 6, decision
// L6). The sweep passes teamchat's seat sweep (teamchat/sweep) over every checkout of the
// owner's seat store: a seat whose session is over is released, a seat its session no longer
// desires is released as a leave, and a seat a restorable session desires is kept, its
// disconnected notice posted once for a run that ended. It runs as the owner and makes no root
// call; the health tick, which runs as root, cannot read these seats (C24).
//
// The units' text is Go's, as teamchat-admin's is: play-claude-yolo.yml asks `seats units list`
// and `seats units print <name>` and writes what they print into the owner's systemd user
// directory.
package seatsweep

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/sweep"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Store is one checkout's seat store as the sweep uses it.
type Store interface {
	sweep.Store
	Close() error
}

// OpenStore is the real seat store of cfg.
func OpenStore(cfg seat.Config) (Store, error) {
	store, err := seat.Open(cfg)
	if err != nil {
		return nil, err
	}
	return store, nil
}

// Cmd is `ccy-teams seats sweep`, `seats units list` and `seats units print <name>`.
type Cmd struct {
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(string) string
	// Parts is the seat store's clock, credential, session records, announcer and reach; the
	// sweep fills in where the store is and which checkout each pass is over.
	Parts seat.Config
	// Open opens the seat store of one checkout.
	Open func(cfg seat.Config) (Store, error)
}

// errUsage is a call the units never make: said, exit 64.
var errUsage = errors.New("usage")

const usage = "seats sweep, seats units list, or seats units print <name>"

// Run dispatches one verb.
func (c *Cmd) Run(ctx context.Context, args []string) term.ExitCode {
	var err error
	switch {
	case len(args) == 1 && args[0] == "sweep":
		err = c.sweep(ctx)
	case len(args) == 2 && args[0] == "units" && args[1] == "list":
		var b strings.Builder
		for _, u := range Units() {
			b.WriteString(u.Name + "\t" + u.Path + "\n")
		}
		err = c.print(b.String())
	case len(args) == 3 && args[0] == "units" && args[1] == "print":
		err = c.printUnit(args[2])
	default:
		err = fmt.Errorf("%w: takes %s", errUsage, usage)
	}
	if err == nil {
		return term.ExitOK
	}
	if _, werr := fmt.Fprintf(c.Stderr, "ccy-teams: seats: %v.\n", err); werr != nil {
		return term.ExitFailure
	}
	if errors.Is(err, errUsage) {
		return term.ExitUsage
	}
	return term.ExitFailure
}

func (c *Cmd) print(text string) error {
	_, err := io.WriteString(c.Stdout, text)
	return err
}

func (c *Cmd) printUnit(name string) error {
	units := Units()
	i := slices.IndexFunc(units, func(u Unit) bool { return u.Name == name })
	if i < 0 {
		return fmt.Errorf("%w: no unit %s: the units are %s and %s", errUsage, strconv.Quote(name), ServiceName, TimerName)
	}
	return c.print(units[i].Text)
}

// sweep passes over every checkout of the owner's pool, each on its own: one that cannot be
// swept, or a seat of it that cannot be settled, is said and the others still are.
func (c *Cmd) sweep(ctx context.Context) error {
	home, err := paths.StateHome(c.Getenv("HOME"), c.Getenv("XDG_STATE_HOME"))
	if err != nil {
		return fmt.Errorf("the state home: %w", err)
	}
	store, err := paths.SeatStore(home)
	if err != nil {
		return fmt.Errorf("the seat store: %w", err)
	}
	entries, err := os.ReadDir(store.Pool())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("the pool: %w", err)
	}
	failed := 0
	for _, e := range entries {
		n, err := c.checkout(ctx, home, store, e.Name())
		failed += n
		if err != nil {
			failed++
			if err = c.say(fmt.Errorf("%s/%s: %w", store.Pool(), e.Name(), err)); err != nil {
				return err
			}
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d checkout(s) or seat(s) of the pool were not swept (each reason is above); the next sweep tries again", failed)
	}
	return nil
}

// checkout sweeps the pool of one checkout: the number of its seats not settled, each said, or
// the error that stopped it.
func (c *Cmd) checkout(ctx context.Context, home string, store paths.Store, name string) (int, error) {
	key, err := paths.ParseCheckoutKey(name)
	if err != nil {
		return 0, fmt.Errorf("not a checkout's pool: %w", err)
	}
	cfg := c.Parts
	cfg.StateHome, cfg.RuntimeDir, cfg.Checkout = home, c.Getenv("XDG_RUNTIME_DIR"), key
	s, err := c.Open(cfg)
	if err != nil {
		return 0, err
	}
	results, err := sweep.Sweep(ctx, s, cfg.Sessions, Desired{Store: store})
	err = errors.Join(err, s.Close())
	failed := 0
	for _, r := range results {
		var line error
		switch {
		case r.Err != nil:
			failed++
			line = c.say(fmt.Errorf("seat %s of team %s: %w", r.Seat.Name, r.Seat.Team, r.Err))
		case r.Action != sweep.Kept:
			line = c.print(fmt.Sprintf("%s seat %s of team %s (session %s)\n", r.Action, r.Seat.Name, r.Seat.Team, r.Session))
		}
		if line != nil {
			return failed, errors.Join(err, line)
		}
	}
	return failed, err
}

func (c *Cmd) say(err error) error {
	_, werr := fmt.Fprintf(c.Stderr, "ccy-teams: seats sweep: %v.\n", err)
	return werr
}

// Desired is the sweep's desired seam over the sessions' desired.json (C 3.7.5), files the owner
// writes and a container only reads.
type Desired struct {
	Store paths.Store
}

// Desires is whether the session's desired.json lists the seat's team with its handle's seat.
// A session with no desired.json yet still desires its seats: a launch writes the file after
// its take.
func (d Desired) Desires(session paths.SessionName, s seat.Seat) (bool, error) {
	h, ok := proto.ParseHandle(s.Name.String())
	if !ok {
		return false, fmt.Errorf("pool seat %s is not a handle", s.Name)
	}
	data, err := readCapped(d.Store.DesiredFile(session), state.MaxDesired)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	desired, err := state.DecodeDesired(data)
	if err != nil {
		return false, err
	}
	if desired.Session != session {
		return false, fmt.Errorf("the desired.json of session %s names session %s", session, desired.Session)
	}
	return slices.Contains(desired.Seats, state.DesiredSeat{Team: s.Team, Seat: h.Seat}), nil
}

// readCapped reads a regular file, never through a link, refusing one over limit bytes.
func readCapped(path string, limit int) ([]byte, error) {
	f, err := os.OpenFile(filepath.Clean(path), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("%s is not a regular file", path)
	}
	var data []byte
	if err == nil {
		data, err = io.ReadAll(io.LimitReader(f, int64(limit)+1))
	}
	if err == nil && len(data) > limit {
		err = fmt.Errorf("%s is larger than %d bytes", path, limit)
	}
	return data, errors.Join(err, f.Close())
}
