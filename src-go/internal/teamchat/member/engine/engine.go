// Package engine is the link's state goroutine (Plan 00209 B 2.4.3, 2.7.3; SPEC-n3 LN4, LN5,
// LN8, P11): the one owner of every held team's store, its items' states and their content in
// memory. Team goroutines hand it judged batches through team.Engine; the link's socket
// handlers hand it requests through Serve, Ack and Return; the link's removal goes through
// Remove.
//
// The goroutine makes no network call and calls no team method, so a slow or unreachable
// homeserver never stalls it, and a team goroutine waiting on it can never wait on itself. A
// request that needs the network (verify, history, a restored item's re-fetch) makes that
// call outside the goroutine, on the team's own client, bounded by the request's deadline;
// one team failing leaves every other team's answer whole (an offline member never blocks
// anything). Every change a later answer depends on is persisted before the answer.
package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/lock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// Config is what the engine is built from.
type Config struct {
	// Root is the member's state root, the seats below it.
	Root *homefs.Root
	// RunRoot is the run's directory, where status.json is written.
	RunRoot *homefs.Root
	Layout  paths.Layout
	// Run is this link's run.
	Run   paths.RunID
	Clock clock.Clock
	Log   io.Writer
	// NewTeam makes a held team's goroutine; it starts nothing until Run.
	NewTeam func(TeamConfig) (team.Team, error)
	// NewEnds makes the ends over the engine, which they record into (EN1).
	NewEnds func(team.Engine) team.Ends
	// Delivery is every team's delivery: socket, wait (a refuse launch) or none (plain).
	Delivery string
	Plain    bool
	// FirstSessionStart is closed by the link on the first accepted SessionStart, or at once
	// under --plain.
	FirstSessionStart <-chan struct{}
	// OnState is told each held team's report whenever it changes, on the state goroutine;
	// it must not block.
	OnState func(wire.State)
	// Runtime is the session's resident cost, for status.
	Runtime func() (records.Runtime, error)
	// Forge checks a send's reference at the forge (n3d-D11); nil refuses a referenced ping as
	// forge-down.
	Forge team.Forge
}

// TeamConfig is what one team goroutine is made from.
type TeamConfig struct {
	Team paths.TeamID
	Seat paths.Seat
	// Lock is the held seat; nil for a team made only for its notices (an owed leave).
	Lock *lock.SeatLock
	// RemovedUserIDs are the user ids this team was removed under in this session: SY1 (c)
	// only for a pair whose user id is among them (n3c-D13).
	RemovedUserIDs    []string
	Engine            team.Engine
	Ends              team.Ends
	Root              *homefs.Root
	Plain             bool
	FirstSessionStart <-chan struct{}
	Clock             clock.Clock
	Log               io.Writer
}

// RecordWriter is where a request's records go, in order.
type RecordWriter interface {
	Record(r records.Record) error
	Flush() error
}

// ErrStopped is a call after the state goroutine has ended.
var ErrStopped = errors.New("engine: the state goroutine has ended")

// Engine is the state goroutine and what it owns.
type Engine struct {
	cfg  Config
	ends team.Ends
	ops  chan func()
	done chan struct{}

	logMu   sync.Mutex
	logLost int

	// Everything below is the state goroutine's alone.
	teams   map[paths.TeamID]*held
	refused map[paths.TeamID]paths.RunID
	desired string
	waiters []chan struct{}
	acks    map[string]bool
	sent    map[paths.TeamID]wire.State
	// timer disarms the one TIMEOUT timer, nil while none is armed (n3d-D7).
	timer func() bool
}

// New is an engine over cfg; its goroutine runs from Run.
func New(cfg Config) (*Engine, error) {
	switch {
	case cfg.Root == nil || cfg.RunRoot == nil:
		return nil, errors.New("engine: no root")
	case cfg.Clock == nil || cfg.Log == nil:
		return nil, errors.New("engine: no clock or log")
	case cfg.NewTeam == nil || cfg.NewEnds == nil:
		return nil, errors.New("engine: no team or ends maker")
	case cfg.OnState == nil || cfg.Runtime == nil:
		return nil, errors.New("engine: no state report or runtime reader")
	case cfg.Delivery != state.DeliverySocket && cfg.Delivery != state.DeliveryWait && cfg.Delivery != state.DeliveryNone:
		return nil, fmt.Errorf("engine: delivery %q is not socket, wait or none", cfg.Delivery)
	}
	if _, err := cfg.Run.MarshalText(); err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}
	e := &Engine{
		cfg: cfg, ops: make(chan func()), done: make(chan struct{}),
		teams: map[paths.TeamID]*held{}, refused: map[paths.TeamID]paths.RunID{},
		desired: state.DesiredApplied, acks: map[string]bool{}, sent: map[paths.TeamID]wire.State{},
	}
	e.ends = cfg.NewEnds(e)
	return e, nil
}

// Ends is the ends the engine was built with.
func (e *Engine) Ends() team.Ends { return e.ends }

// Run is the state goroutine, until ctx ends. Every call made after it returns is ErrStopped.
func (e *Engine) Run(ctx context.Context) error {
	defer close(e.done)
	for {
		select {
		case op := <-e.ops:
			op()
		case <-ctx.Done():
			return nil
		}
	}
}

// do runs f on the state goroutine and waits for it. Once accepted, f runs to its end: it is
// local work only. A context already ended is refused before the goroutine is asked.
func (e *Engine) do(ctx context.Context, f func()) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	finished := make(chan struct{})
	op := func() {
		defer close(finished)
		f()
	}
	select {
	case e.ops <- op:
	case <-ctx.Done():
		return ctx.Err()
	case <-e.done:
		return ErrStopped
	}
	<-finished
	return nil
}

// logf writes one line to link.log. A log that cannot be written loses the line, never the
// work, and the next line written counts the lines lost.
func (e *Engine) logf(format string, args ...any) {
	e.logMu.Lock()
	defer e.logMu.Unlock()
	line := fmt.Sprintf("engine: "+format+"\n", args...)
	if e.logLost > 0 {
		line = fmt.Sprintf("engine: %d log lines were lost\n", e.logLost) + line
	}
	if _, err := io.WriteString(e.cfg.Log, line); err != nil {
		e.logLost++
		return
	}
	e.logLost = 0
}
