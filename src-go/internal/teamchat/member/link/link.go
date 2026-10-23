// Package link is the member's link (Plan 00209 B 2.4.3-2.4.4, 2.10; SPEC-n3 LN1, LN2, LN4,
// LN5, LN7, LN8's link half): the process the keeper starts with the control socket on fd 3.
// It holds link.lock, watches the keeper by a pidfd, takes each seat the keeper hands it to the
// engine (the state goroutine, which makes no network call), serves link.sock, keeps
// session.json, and ends the session's teams when the keeper says end or dies.
//
// An offline member blocks nothing: an add completes once its store is loaded, never waiting on
// the network, and ready goes once launch's seats adds are handled or 10 s have passed; a
// removal answers within the engine's 4 s; the leave and the ends are bounded (30 s, 5 s).
package link

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/failpoint"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/engine"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/inbox"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/lock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	// addsWait bounds the wait for launch and its seats adds (n3c-D24): then ready goes with
	// what was added, and a later add is a hot add.
	addsWait = 10 * time.Second
	// endWait bounds the session's end: the ends and presence (T20).
	endWait = 5 * time.Second
	// leaveWait bounds a removal's presence and leave, before left (LN8).
	leaveWait = 30 * time.Second
	// offline is the presence a team is left in.
	offline = "offline"
	// howKeeperDied is the end of a session whose keeper is gone (LN1).
	howKeeperDied = "keeper-died"
)

// Parent is the keeper watched by a pidfd: its pid, and a channel closed when it exits.
type Parent interface {
	PID() int
	Exited() <-chan struct{}
	Close() error
}

// Config is what a link runs over.
type Config struct {
	// StateRoot holds the seats and the conversation record; RunRoot holds the run's directory.
	StateRoot, RunRoot *homefs.Root
	Layout             paths.Layout
	Run                paths.RunID
	Control            *wire.Conn
	Clock              clock.Clock
	Proc               Proc
	// Parent opens the keeper's pidfd and checks the keeper is still the parent.
	Parent func() (Parent, error)
	// UID is the member's uid, the only peer link.sock serves.
	UID int
	// Kit is the link's source digest, which a request's kit must be.
	Kit paths.Digest
	// RuntimeDir is $XDG_RUNTIME_DIR, the inbox sockets'; Home is ~, for ~/.claude.
	RuntimeDir, Home string
	Log              io.Writer
	Engine           func(engine.Config) (*engine.Engine, error)
	NewTeam          func(engine.TeamConfig) (team.Team, error)
	NewEnds          func(engine.EndsConfig) team.Ends

	// started, when set, is told the link once it is ready (tests).
	started func(*link)
}

// received is one control message, its add's descriptor, or the reading's end.
type received struct {
	m   wire.Msg
	fd  *os.File
	err error
}

var errKeeperExited = errors.New("link: the keeper's pidfd fired")

type link struct {
	cfg      Config
	parent   Parent
	launch   wire.Launch
	delivery string
	eng      *engine.Engine
	srv      *server
	session  *Session
	msgs     chan received
	// quit ends the reader and the background goroutines; closeQuit closes it, once.
	quit      chan struct{}
	closeQuit func()
	// reader is the control socket's reader, waited for once quit is closed and the socket is.
	reader sync.WaitGroup
	bg     sync.WaitGroup
	sendMu sync.Mutex
	logMu  sync.Mutex

	first     chan struct{}
	firstOnce sync.Once

	mu    sync.Mutex
	token string

	stateMu   sync.Mutex
	states    map[paths.TeamID]wire.State
	stateWake chan struct{}

	// render is the latest unusable.render count the engine told, not yet written when dirty.
	renderMu    sync.Mutex
	render      int64
	renderDirty bool
	renderWake  chan struct{}

	hooks hookMemory
	mem   membership
}

// Run is the link (LN1): link.lock (78 if another link holds it), the keeper's pidfd, the
// launch and its adds, link.sock and ready, then every keeper message until end or the
// keeper's death, which end the session's teams.
func Run(ctx context.Context, cfg Config) term.ExitCode {
	ll, err := lock.TakeLink(ctx, cfg.Clock, cfg.RunRoot, cfg.Layout.Run(cfg.Run))
	if err != nil {
		return refuse(cfg.Log, "take link.lock", err)
	}
	quit := make(chan struct{})
	l := &link{
		cfg: cfg, msgs: make(chan received), quit: quit, closeQuit: sync.OnceFunc(func() { close(quit) }), first: make(chan struct{}),
		states: map[paths.TeamID]wire.State{}, stateWake: make(chan struct{}, 1), renderWake: make(chan struct{}, 1),
		mem: membership{wake: make(chan struct{}, 1)},
	}
	code := l.run(ctx)
	if err := ll.Release(); err != nil {
		l.logf("link: release link.lock: %v", err)
	}
	return code
}

// refuse logs why the link cannot run and is 78.
func refuse(log io.Writer, what string, err error) term.ExitCode {
	if _, logErr := fmt.Fprintf(log, "link: %s: %v\n", what, err); logErr != nil {
		return exits.ExitConfig
	}
	return exits.ExitConfig
}

func (l *link) run(parentCtx context.Context) term.ExitCode {
	parent, err := l.cfg.Parent()
	if err != nil {
		return refuse(l.cfg.Log, "watch the keeper", err)
	}
	l.parent = parent
	defer func() {
		l.logErr("close the keeper's pidfd", parent.Close())
	}()
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()
	l.reader.Go(l.read)
	defer l.stopReading()
	hold, stopHold := l.cfg.Clock.Hold(addsWait)
	defer stopHold()
	launch, code, ok := l.awaitLaunch(hold)
	if !ok {
		return code
	}
	if code := l.build(launch); code != term.ExitOK {
		return code
	}
	engDone := make(chan error, 1)
	go func() { engDone <- l.eng.Run(ctx) }()
	l.bg.Go(l.reportStates)
	l.bg.Go(l.recordRender)
	defer l.teardown(ctx, cancel, engDone)
	pending := l.awaitAdds(ctx, hold, launch.Seats)
	stopHold()
	ln, err := listen(l.cfg.RunRoot, l.cfg.Layout.Run(l.cfg.Run))
	if err != nil {
		return refuse(l.cfg.Log, "serve link.sock", err)
	}
	l.srv = newServer(ln, l.eng, l.cfg.Clock, l.cfg.UID, l.cfg.Kit, l.cfg.Log)
	l.srv.hook = l.serveHook
	l.bg.Go(func() { l.srv.run(ctx) })
	l.bg.Go(func() { l.announceTeams(ctx) })
	l.send(wire.Msg{Kind: wire.KindReady, Ready: &wire.Ready{}})
	if l.cfg.started != nil {
		l.cfg.started(l)
	}
	if pending != nil && l.dispatch(ctx, *pending) {
		return term.ExitOK
	}
	for {
		var r received
		select {
		case r = <-l.msgs:
		case <-parent.Exited():
			r = received{err: errKeeperExited}
		}
		if l.dispatch(ctx, r) {
			return term.ExitOK
		}
	}
}

// read is the control socket's reader, until its end.
func (l *link) read() {
	for {
		m, fd, err := l.cfg.Control.Recv()
		select {
		case l.msgs <- received{m: m, fd: fd, err: err}:
		case <-l.quit:
			if fd != nil {
				l.closeFile(fd)
			}
			return
		}
		if err != nil && !errors.Is(err, wire.ErrProtocol) {
			return
		}
	}
}

// awaitLaunch is the keeper's first message, which must be launch within addsWait; a keeper
// gone before it is a link with nothing to end (0).
func (l *link) awaitLaunch(hold <-chan time.Time) (wire.Launch, term.ExitCode, bool) {
	for {
		select {
		case r := <-l.msgs:
			switch {
			case errors.Is(r.err, wire.ErrProtocol):
				l.logf("link: %v", r.err)
				continue
			case r.err != nil:
				l.logf("link: the keeper went before the launch: %v", r.err)
				return wire.Launch{}, term.ExitOK, false
			case r.m.Kind != wire.KindLaunch:
				l.closeFile(r.fd)
				return wire.Launch{}, refuse(l.cfg.Log, "start", fmt.Errorf("the first message is %s, not launch", r.m.Kind)), false
			}
			return *r.m.Launch, term.ExitOK, true
		case <-hold:
			return wire.Launch{}, refuse(l.cfg.Log, "start", errors.New("no launch in 10 s")), false
		case <-l.parent.Exited():
			l.logf("link: the keeper went before the launch")
			return wire.Launch{}, term.ExitOK, false
		}
	}
}

// build makes the session's facts and the engine for launch.
func (l *link) build(launch wire.Launch) term.ExitCode {
	l.launch = launch
	plain := launch.Launch == state.LaunchPlain
	switch {
	case plain:
		l.delivery = state.DeliveryNone
		l.firstOnce.Do(func() { close(l.first) })
	case launch.Inbound == state.InboundRefuse:
		l.delivery = state.DeliveryWait
	default:
		l.delivery = state.DeliverySocket
	}
	claudeDir := l.cfg.Home + "/.claude"
	if launch.ClaudeConfigDir != nil {
		claudeDir = *launch.ClaudeConfigDir
	}
	var err error
	l.session, err = newSession(sessionDeps{
		runRoot: l.cfg.RunRoot, stateRoot: l.cfg.StateRoot, layout: l.cfg.Layout, run: l.cfg.Run,
		proc: l.cfg.Proc, clock: l.cfg.Clock, runtimeDir: l.cfg.RuntimeDir, claudeDir: claudeDir,
		cwd: launch.Cwd, onClaude: l.tellClaude, onStarted: l.tellStarted, log: l.cfg.Log,
	})
	if err != nil {
		return refuse(l.cfg.Log, "read session.json", err)
	}
	var dialer inbox.Dialer
	if l.delivery == state.DeliverySocket {
		dialer = inbox.NewDialer(l.cfg.Clock)
	}
	l.eng, err = l.cfg.Engine(engine.Config{
		Root: l.cfg.StateRoot, RunRoot: l.cfg.RunRoot, Layout: l.cfg.Layout, Run: l.cfg.Run,
		Clock: l.cfg.Clock, Log: l.cfg.Log, NewTeam: l.cfg.NewTeam, NewEnds: l.cfg.NewEnds,
		Delivery: l.delivery, Plain: plain, FirstSessionStart: l.first,
		OnState: l.onState, Runtime: l.runtime,
		Inbox: dialer, Unusable: l.session.Unusable(), OnUnusable: l.onUnusable,
		OnTeams: l.onTeams,
	})
	l.setGeneration(launch.Generation)
	if err != nil {
		return refuse(l.cfg.Log, "make the engine", err)
	}
	return term.ExitOK
}

// awaitAdds handles launch's seats adds (n3c-D24): a failed add counts, an owed leave (which
// the keeper sends before its adds) is handled in place, and the wait ends at addsWait or at
// another kind of message, which is returned to be handled after ready.
func (l *link) awaitAdds(ctx context.Context, hold <-chan time.Time, seats int) *received {
	for n := 0; n < seats; {
		select {
		case r := <-l.msgs:
			if errors.Is(r.err, wire.ErrProtocol) {
				l.logf("link: %v", r.err)
				continue
			}
			if r.err == nil && r.m.Kind == wire.KindOwed {
				l.owed(ctx, r.m.Owed)
				continue
			}
			if r.err != nil || r.m.Kind != wire.KindAdd {
				l.logf("link: %d of %d adds came before another message; ready without the rest", n, seats)
				return &r
			}
			l.add(ctx, r.m.Add, r.fd)
			n++
		case <-hold:
			l.logf("link: %d of %d adds came in 10 s; ready without the rest", n, seats)
			return nil
		case <-l.parent.Exited():
			return &received{err: errKeeperExited}
		}
	}
	return nil
}

// dispatch handles one keeper message; true when the link is to exit.
func (l *link) dispatch(ctx context.Context, r received) bool {
	switch {
	case errors.Is(r.err, wire.ErrProtocol):
		l.logf("link: %v", r.err)
		return false
	case r.err != nil:
		l.logf("link: the keeper is gone (%v); the session ends", r.err)
		l.finish(ctx, howKeeperDied)
		return true
	}
	m := r.m
	switch m.Kind {
	case wire.KindAdd:
		l.add(ctx, m.Add, r.fd)
	case wire.KindRemove:
		l.remove(ctx, m.Remove)
	case wire.KindOwed:
		l.owed(ctx, m.Owed)
	case wire.KindPID:
		l.session.SetSessionPID(*m.PID)
	case wire.KindClaude:
		l.session.SetClaude(*m.Claude)
	case wire.KindToken:
		l.mu.Lock()
		l.token = m.Token.Token
		l.mu.Unlock()
		// The socket and transcript come from session.json, so a restarted link delivers
		// again before any hook.
		f := l.session.Inbox()
		f.Token = m.Token.Token
		l.logErr("tell the engine the session", l.eng.SetSession(ctx, f))
	case wire.KindDesired:
		l.setGeneration(m.Desired.Generation)
		l.logErr("set the desired membership", l.eng.SetDesired(ctx, *m.Desired))
	case wire.KindRefused:
		l.logErr("show a refused add", l.eng.Refuse(ctx, m.Refused.Team, m.Refused.HolderRun))
	case wire.KindEnd:
		l.finish(ctx, m.End.How)
		return true
	default:
		l.logf("link: a %s message is not the keeper's to send", m.Kind)
	}
	return false
}

// add takes a seat's descriptor as its lock and gives the engine the team; a failure is the
// team's, logged, and never the link's.
func (l *link) add(ctx context.Context, a *wire.Add, fd *os.File) {
	sl, err := lock.AdoptSeat(l.cfg.StateRoot, fd, l.cfg.Layout.Seat(a.Team))
	if err != nil {
		l.logf("link: add %s: adopt the seat: %v", a.Team, err)
		return
	}
	if err := l.eng.Add(ctx, a.Team, sl, a.RemovedUserIDs); err != nil {
		l.logf("link: add %s: %v", a.Team, err)
	}
}

// remove is LN8's link half: the engine's removal, removed to the keeper, then from memory
// within leaveWait presence offline, the leave when the farewell is released, and left. Every
// remove is answered: one the engine could not make goes with no user id and posts nothing.
func (l *link) remove(ctx context.Context, rm *wire.Remove) {
	l.noteFarewell(rm.Team, rm.Farewell)
	removed, err := l.eng.Remove(ctx, rm.Team)
	if err != nil {
		l.logf("link: remove %s: %v", rm.Team, err)
	}
	if removed.UserID == "" {
		l.logf("link: remove %s: no user id; the keeper deletes the store", rm.Team)
	}
	l.send(wire.Msg{Kind: wire.KindRemoved, Removed: &wire.Removed{Team: rm.Team, UserID: removed.UserID}})
	failpoint.Hit(failpoint.LinkAfterRemoveAnswered)
	post := rm.Farewell != nil && *rm.Farewell == state.FarewellReleased
	l.bg.Go(func() { l.leave(ctx, rm.Team, removed.Notices, post, rm.Txn) })
}

// owed posts a leave still owed, by the keeper's txn, on a team made but never run.
func (l *link) owed(ctx context.Context, o *wire.Owed) {
	l.bg.Go(func() {
		t, err := l.cfg.NewTeam(engine.TeamConfig{
			Team: o.Team, Seat: l.cfg.Layout.Seat(o.Team), Run: l.cfg.Run, Engine: l.eng, Ends: l.eng.Ends(),
			Root: l.cfg.StateRoot, Plain: l.launch.Launch == state.LaunchPlain,
			FirstSessionStart: l.first, Clock: l.cfg.Clock, Log: l.cfg.Log,
		})
		var n team.Notices
		if err != nil {
			l.logf("link: owed %s: %v", o.Team, err)
		} else {
			n = t.Notices()
		}
		l.leave(ctx, o.Team, n, true, o.Txn)
	})
}

// leave is presence offline and, when post, the leave notice, within leaveWait; then left.
func (l *link) leave(ctx context.Context, t paths.TeamID, n team.Notices, post bool, txn string) {
	bound, cancel := context.WithCancel(ctx)
	stop := l.cfg.Clock.AfterFunc(leaveWait, cancel)
	defer stop()
	defer cancel()
	if n != nil {
		l.logErr("set "+t.String()+" offline", n.SetPresence(bound, n.Anchors().Self, offline))
		if post {
			l.logErr("post "+t.String()+"'s leave", l.eng.Ends().Leave(bound, t, n, txn))
		}
	}
	l.send(wire.Msg{Kind: wire.KindLeft, Left: &wire.Left{Team: t}})
}

// finish ends the session's teams (EN1) and sets them offline, within endWait.
func (l *link) finish(ctx context.Context, how string) {
	bound, cancel := context.WithCancel(ctx)
	stop := l.cfg.Clock.AfterFunc(endWait, cancel)
	defer stop()
	defer cancel()
	teams, err := l.eng.EndTeams(bound)
	l.logErr("list the teams to end", err)
	end := team.End{How: how, Kind: l.launch.Kind, SessionEndReason: l.session.EndReason()}
	for _, err := range l.eng.Ends().End(bound, end, teams) {
		l.logErr("end the session", err)
	}
	var wg sync.WaitGroup
	for _, t := range teams {
		wg.Go(func() {
			l.logErr("set "+t.Team.String()+" offline", t.Notices.SetPresence(bound, t.Notices.Anchors().Self, offline))
		})
	}
	wg.Wait()
}

// teardown stops serving, shuts the engine and its teams, and waits for the link's goroutines.
func (l *link) teardown(ctx context.Context, cancel context.CancelFunc, engDone <-chan error) {
	if l.srv != nil {
		l.logErr("close link.sock", l.srv.close())
	}
	l.logErr("shut the engine", l.eng.Shutdown(ctx))
	cancel()
	l.logErr("the state goroutine", <-engDone)
	l.closeQuit()
	l.bg.Wait()
}

// stopReading ends the reader on every path out of run: quit, then the control socket, whose
// close ends a Recv, and the wait for it.
func (l *link) stopReading() {
	l.closeQuit()
	if err := l.cfg.Control.Close(); err != nil {
		l.logf("link: close the control socket: %v", err)
	}
	l.reader.Wait()
}

func (l *link) closeFile(f *os.File) {
	if f != nil {
		l.logErr("close a descriptor", f.Close())
	}
}

// send is one message to the keeper; a failure is logged (the keeper's end shows as EOF).
func (l *link) send(m wire.Msg) {
	l.sendMu.Lock()
	defer l.sendMu.Unlock()
	if err := l.cfg.Control.Send(m, nil); err != nil {
		l.logf("link: send %s: %v", m.Kind, err)
	}
}

func (l *link) tellClaude(p state.Process) {
	l.send(wire.Msg{Kind: wire.KindClaude, Claude: &p})
}

func (l *link) tellStarted() {
	l.firstOnce.Do(func() { close(l.first) })
	l.send(wire.Msg{Kind: wire.KindStarted, Started: &wire.Started{}})
}

// heldToken is the inbox socket token the keeper gave, in memory only.
func (l *link) heldToken() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.token
}

// onState is the engine's report of a team, on its state goroutine: kept, the latest per team,
// for reportStates, so the goroutine never waits on the socket.
func (l *link) onState(s wire.State) {
	l.stateMu.Lock()
	l.states[s.Team] = s
	l.stateMu.Unlock()
	select {
	case l.stateWake <- struct{}{}:
	default:
	}
}

// reportStates sends the keeper each team's latest state, until the link ends.
func (l *link) reportStates() {
	for {
		select {
		case <-l.quit:
			return
		case <-l.stateWake:
		}
		l.stateMu.Lock()
		states := l.states
		l.states = map[paths.TeamID]wire.State{}
		l.stateMu.Unlock()
		teams := make([]paths.TeamID, 0, len(states))
		for t := range states {
			teams = append(teams, t)
		}
		slices.SortFunc(teams, func(a, b paths.TeamID) int { return strings.Compare(a.String(), b.String()) })
		for _, t := range teams {
			s := states[t]
			l.send(wire.Msg{Kind: wire.KindState, State: &s})
		}
	}
}

// onUnusable is the engine's counts, on its state goroutine: the render count kept for
// recordRender, so the goroutine never waits on a file.
func (l *link) onUnusable(u state.Unusable) {
	l.renderMu.Lock()
	l.render, l.renderDirty = u.Render, true
	l.renderMu.Unlock()
	select {
	case l.renderWake <- struct{}{}:
	default:
	}
}

// recordRender writes each render count told into session.json, and the last one at the end.
func (l *link) recordRender() {
	for {
		select {
		case <-l.quit:
			l.writeRender()
			return
		case <-l.renderWake:
			l.writeRender()
		}
	}
}

func (l *link) writeRender() {
	l.renderMu.Lock()
	n, dirty := l.render, l.renderDirty
	l.renderDirty = false
	l.renderMu.Unlock()
	if dirty {
		l.logErr("record the render count", l.session.RecordRender(n))
	}
}

// runtime is the keeper's and the link's resident cost, for status (B40).
func (l *link) runtime() (records.Runtime, error) {
	self, err := l.cfg.Proc.Cost(os.Getpid())
	keeper, keeperErr := l.cfg.Proc.Cost(l.parent.PID())
	if err = errors.Join(err, keeperErr); err != nil {
		return records.Runtime{}, err
	}
	up := func(start int64) int64 { return max(int64(l.cfg.Clock.Boot()/time.Second)-start/ticksPerSecond, 0) }
	var conns int64
	if l.srv != nil {
		conns = l.srv.connections()
	}
	return records.Runtime{
		Keeper: records.KeeperCost{RSSKiB: keeper.RSSKiB, CPUMS: keeper.CPUMS, Wakeups: keeper.Wakeups, FDs: keeper.FDs, UptimeS: up(keeper.StartTicks)},
		Link: records.LinkCost{
			RSSKiB: self.RSSKiB, CPUMS: self.CPUMS, Wakeups: self.Wakeups, FDs: self.FDs,
			Conns: conns, UptimeS: up(self.StartTicks),
		},
	}, nil
}

func (l *link) logErr(what string, err error) {
	if err != nil {
		l.logf("link: %s: %v", what, err)
	}
}

func (l *link) logf(format string, args ...any) {
	l.logMu.Lock()
	defer l.logMu.Unlock()
	// link.log is the log; a line it cannot take has nowhere else to go.
	if _, err := fmt.Fprintf(l.cfg.Log, format+"\n", args...); err != nil {
		return
	}
}
