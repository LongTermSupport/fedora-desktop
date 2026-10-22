package teamsync

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"io/fs"
	"log"
	"math/big"
	"reflect"
	"sync"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/bundle"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The backoff of SY8: 1 s doubling to 30 s, a quarter of it at most taken off as jitter, and
// down once a streak of failures is 15 s old.
const (
	firstBackoff = time.Second
	maxBackoff   = 30 * time.Second
	jitterShare  = 4
	downAfter    = 15 * time.Second
)

// Config is what a team goroutine is built from.
type Config struct {
	Team paths.TeamID
	Seat paths.Seat
	// Root is the member's state directory, held open, below which the seat's pair is read.
	Root *homefs.Root
	// Run is this link's run: a store whose run is this one gets no Close and no BeginRun.
	Run    paths.RunID
	Engine team.Engine
	Ends   team.Ends
	Clock  clock.Clock
	// Watcher is the seat directory's changes, each a Wake; nil for none.
	Watcher Watcher
	// RemovedUserIDs are the accounts the keeper removed earlier, whose fresh store is a re-add
	// (SY1 (c)).
	RemovedUserIDs []string
	// Plain posts back at once, with no session to wait for.
	Plain bool
	// FirstSessionStart is closed at the session's first SessionStart, which back waits for.
	FirstSessionStart <-chan struct{}
	// Log is link.log; nil discards.
	Log io.Writer
	// HTTP builds the team's client; nil is matrix.NewMember.
	HTTP func(matrix.Config) (*matrix.Client, error)
	// Jitter is how much of a backoff is taken off; nil is crypto/rand, up to a quarter.
	Jitter func(time.Duration) time.Duration
}

// Watcher is a source of seat directory changes.
type Watcher interface{ Changes() <-chan struct{} }

var (
	// errRestart starts the session again at once: the epoch moved, a freeze, a new pair.
	errRestart = errors.New("teamsync: the session starts again")
	// errUntrusted is a main room or roster the server will not give.
	errUntrusted = errors.New("teamsync: the main room or its roster is missing or refused")
)

// Team is one team goroutine. Run owns the sync; Notices, Verify, History, Refetch and
// FlushFloor may be called from other goroutines.
type Team struct {
	cfg  Config
	log  *log.Logger
	res  *resolver
	wake chan struct{}

	mu       sync.Mutex
	pair     bundle.Pair
	client   *matrix.Client
	roster   proto.Roster
	verified bool
	floor    floorMarker

	// The team goroutine's own state.
	health      team.Health
	failing     bool
	failStart   time.Duration
	delay       time.Duration
	authRetried bool
	persistSaid bool
	backStarted bool
	markerSaid  bool
	pendingSaid map[string]bool
	unheard     *bounded[struct{}]
	flood       proto.Window
	freeze      freezeDetector

	// Test seams.
	onPoll   func(since string)
	onWait   func(time.Duration)
	readPair func(*homefs.Root, paths.Seat) (bundle.Pair, error)
	judge    func(proto.Judge, proto.Value) proto.Verdict
	pages    pageBound
}

// pageBound is a backwards read's page size and page count.
type pageBound struct{ size, max int }

// floorMarker is SY10's state: the floor to put, and when it was last put.
type floorMarker struct {
	ms, epoch int64
	known     bool
	dirty     bool
	put       bool
	putAt     time.Duration
}

// New is the team goroutine for cfg; it makes no request until Run or a notice.
func New(cfg Config) (*Team, error) {
	if cfg.Team.String() == "" || cfg.Seat.Dir() == "" || cfg.Root == nil || cfg.Engine == nil || cfg.Ends == nil || cfg.Clock == nil {
		return nil, errors.New("teamsync: the team, seat, root, engine, ends and clock are all needed")
	}
	if cfg.HTTP == nil {
		cfg.HTTP = matrix.NewMember
	}
	if cfg.Jitter == nil {
		cfg.Jitter = cryptoJitter
	}
	l := newLogger(cfg.Log, cfg.Team)
	return &Team{
		cfg: cfg, log: l, res: newResolver(l), wake: make(chan struct{}, 1),
		pendingSaid: map[string]bool{}, unheard: newBounded[struct{}](memorySize),
		flood: proto.Window{Times: map[string][]int64{}}, freeze: freezeDetector{clk: cfg.Clock},
		onPoll: func(string) {}, onWait: func(time.Duration) {}, readPair: bundle.ReadPair,
		judge: func(j proto.Judge, v proto.Value) proto.Verdict { return j.Judge(v) },
		pages: pageBound{size: pageSize, max: maxPages},
	}, nil
}

func cryptoJitter(d time.Duration) time.Duration {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(d/jitterShare)+1))
	if err != nil {
		return 0
	}
	return time.Duration(n.Int64())
}

// Wake retries a stopped team, or one waiting for its pair.
func (t *Team) Wake() {
	select {
	case t.wake <- struct{}{}:
	default:
	}
}

// Run syncs the team until ctx ends or the engine removes it; a stop waits here for a Wake.
func (t *Team) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	defer wg.Wait()
	defer cancel()
	if t.cfg.Watcher != nil {
		wg.Go(func() { t.forward(ctx) })
	}
	for {
		err := t.session(ctx, &wg)
		switch code, stop := stopCode(err); {
		case ctx.Err() != nil || errors.Is(err, team.ErrRemoved):
			return nil
		case errors.Is(err, errRestart):
		case errors.Is(err, bundle.ErrTorn) || errors.Is(err, fs.ErrNotExist):
			if !t.await(ctx) {
				return nil
			}
		case errors.Is(err, matrix.ErrAuthRefused) && t.retryAuth():
		case stop:
			t.stop(code, err)
			if !t.await(ctx) {
				return nil
			}
			t.authRetried = false
			t.setHealth(func(h *team.Health) { h.Stopped = "" })
		default:
			if !t.backoff(ctx, err) {
				return nil
			}
		}
	}
}

func (t *Team) forward(ctx context.Context) {
	for {
		select {
		case <-t.cfg.Watcher.Changes():
			t.Wake()
		case <-ctx.Done():
			return
		}
	}
}

func (t *Team) await(ctx context.Context) bool {
	select {
	case <-t.wake:
		return true
	case <-ctx.Done():
		return false
	}
}

// stopCode is the exits code that stops the team for err, if it is a permanent failure (SY8).
func stopCode(err error) (string, bool) {
	if e, ok := errors.AsType[*exits.Error](err); ok {
		return e.Code, true
	}
	switch {
	case errors.Is(err, matrix.ErrAuthRefused):
		return exits.CodeToken, true
	case errors.Is(err, matrix.ErrForbidden):
		return exits.CodeForbidden, true
	case errors.Is(err, errUntrusted) || isTrustError(err):
		return exits.CodeUntrusted, true
	}
	return "", false
}

func isTrustError(err error) bool {
	var trust *proto.TrustError
	var roster *proto.RosterError
	return errors.As(err, &trust) || errors.As(err, &roster)
}

// retryAuth is n3c-D12: the first 401 re-reads the pair, and a pair that changed (the token
// placed before its seat.json) is tried at once; an unchanged pair, or a second 401, stops.
func (t *Team) retryAuth() bool {
	if t.authRetried {
		return false
	}
	t.authRetried = true
	p, err := t.readPair(t.cfg.Root, t.cfg.Seat)
	if err != nil {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return !reflect.DeepEqual(p, t.pair)
}

func (t *Team) stop(code string, err error) {
	t.log.Printf("stopped (%s): %s", code, matrix.Class(err))
	t.setHealth(func(h *team.Health) { h.Stopped = code })
}

// backoff is SY8's transient path: a wait of the streak's delay, which a Wake ends early, and
// down once the streak is downAfter old.
func (t *Team) backoff(ctx context.Context, err error) bool {
	now := t.cfg.Clock.Boot()
	if !t.failing {
		t.failing, t.failStart, t.delay = true, now, firstBackoff
	}
	if !t.health.Down && now-t.failStart >= downAfter {
		t.log.Printf("down: %s", matrix.Class(err))
		t.setHealth(func(h *team.Health) { h.Down = true })
	}
	d := t.delay - t.cfg.Jitter(t.delay)
	t.delay = min(2*t.delay, maxBackoff)
	ch, stop := t.cfg.Clock.Hold(d)
	defer stop()
	t.onWait(d)
	select {
	case <-ch:
	case <-t.wake:
	case <-ctx.Done():
		return false
	}
	return true
}

// recovered ends a failure streak once an answer is handled.
func (t *Team) recovered() {
	t.authRetried = false
	if !t.failing {
		return
	}
	t.failing = false
	if t.health.Down {
		t.log.Printf("back up")
		t.setHealth(func(h *team.Health) { h.Down = false })
	}
}

func (t *Team) setHealth(change func(*team.Health)) {
	change(&t.health)
	t.cfg.Engine.SetHealth(t.cfg.Team, t.health)
}

// current is the client of the pair as it reads now (MS3: read on each use): the one in use
// while the pair is unchanged, else a new one, the old one closed.
func (t *Team) current() (*matrix.Client, bundle.Pair, error) {
	p, err := t.readPair(t.cfg.Root, t.cfg.Seat)
	if err != nil {
		return nil, bundle.Pair{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.client != nil && reflect.DeepEqual(p, t.pair) {
		return t.client, p, nil
	}
	b := p.Bundle.Bundle
	c, err := t.cfg.HTTP(matrix.Config{BaseURL: b.BaseURL, PlainHTTPHosts: b.PlainHTTPHosts, Token: p.Token.Reveal(), Clock: t.cfg.Clock, Timers: t.cfg.Clock})
	if err != nil {
		return nil, bundle.Pair{}, err
	}
	if t.client != nil {
		t.client.Close()
	}
	t.client, t.pair = c, p
	return c, p, nil
}

func anchorsOf(tm paths.TeamID, p bundle.Pair) proto.Anchors {
	b := p.Bundle.Bundle
	return proto.Anchors{Server: b.ServerName, Team: tm.String(), MainRoom: b.MainRoom, Self: b.UserID}
}

func (t *Team) setRoster(r proto.Roster, self string) {
	t.mu.Lock()
	t.roster, t.verified = r, true
	t.mu.Unlock()
	if role := r.Members[self].Role; role != t.health.Role {
		t.setHealth(func(h *team.Health) { h.Role = role })
	}
}

// freezeDetector is SY12: a growth of boot minus mono past maxSkewGrowth since the last reading,
// or a request answered more than maxOverdue past its deadline, is a freeze or a suspend.
type freezeDetector struct {
	clk  clock.Clock
	skew time.Duration
	read bool
}

const (
	maxSkewGrowth   = 2 * time.Second
	maxOverdue      = 10 * time.Second
	requestDeadline = 45 * time.Second
)

func (f *freezeDetector) reset() { f.read = false }

func (f *freezeDetector) grew() time.Duration {
	skew := f.clk.Boot() - f.clk.Mono()
	var g time.Duration
	if f.read {
		g = skew - f.skew
	}
	f.skew, f.read = skew, true
	return g
}

// atStart reads the clocks at a request's start.
func (f *freezeDetector) atStart() (time.Duration, bool) {
	g := f.grew()
	return g, g > maxSkewGrowth
}

// atAnswer reads them at its answer, for a request started at boot time started.
func (f *freezeDetector) atAnswer(started time.Duration) (time.Duration, bool) {
	if g := f.grew(); g > maxSkewGrowth {
		return g, true
	}
	if over := f.clk.Boot() - (started + requestDeadline); over > maxOverdue {
		return over, true
	}
	return 0, false
}
