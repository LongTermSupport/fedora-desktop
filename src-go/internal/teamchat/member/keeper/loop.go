package keeper

import (
	"errors"
	"os"
	"syscall"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/failpoint"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// link is one start of the link: its process and the keeper's end of its control socket.
type link struct {
	proc       execx.Started
	conn       *wire.Conn
	startedAt  time.Duration
	endOnly    bool
	ready      bool
	killed     bool
	readyTimer *timer
}

// timer is one of the loop's waits: dead once it has fired or been cancelled, so a firing
// that was already on its way does nothing.
type timer struct {
	stop func() bool
	dead bool
}

// after runs f on the loop after d.
func (k *keeper) after(d time.Duration, f func()) *timer {
	t := &timer{}
	k.timers[t] = struct{}{}
	t.stop = k.cfg.Clock.AfterFunc(d, func() {
		k.post(func() {
			if !t.dead {
				t.dead = true
				delete(k.timers, t)
				f()
			}
		})
	})
	return t
}

// post queues f for the loop from a timer's goroutine, never waiting on the loop: once the loop
// has ended, f is never run.
func (k *keeper) post(f func()) {
	k.firedMu.Lock()
	k.firedQ = append(k.firedQ, f)
	k.firedMu.Unlock()
	select {
	case k.fired <- struct{}{}:
	default:
	}
}

// runFired runs the timers' queued work in order, until the keeper has finished.
func (k *keeper) runFired() {
	k.firedMu.Lock()
	queued := k.firedQ
	k.firedQ = nil
	k.firedMu.Unlock()
	for _, f := range queued {
		if k.finished {
			return
		}
		f()
	}
}

// cancel disarms t, so a test counting the clock's waiters sees it gone.
func (k *keeper) cancel(t *timer) {
	if t == nil || t.dead {
		return
	}
	t.dead = true
	delete(k.timers, t)
	t.stop()
}

// send posts f to the loop, or drops it once the loop has ended.
func (k *keeper) send(f func()) {
	select {
	case k.events <- f:
	case <-k.done:
	}
}

// loop is steps 6-8: the link started and supervised, CMD started once it is ready, until the
// end has run.
func (k *keeper) loop() term.ExitCode {
	k.turnEnded()
	if err := k.startLink(false); err != nil {
		k.logf("start the link: %v", err)
		k.linkDown()
	}
	k.turnEnded()
	for !k.finished {
		var exited <-chan struct{}
		if k.claudeAlive() {
			exited = k.claude.Exited()
		}
		select {
		case f := <-k.events:
			f()
		case <-k.fired:
			k.runFired()
		case s := <-k.cfg.Signals:
			k.onSignal(s)
		case ev, ok := <-k.watch:
			k.onWatch(ev, ok)
		case <-exited:
			k.claudeExited()
		}
		k.turnEnded()
	}
	close(k.done)
	// The link and CMD have been waited for and the link's socket closed, so these end.
	k.bg.Wait()
	if k.exitErr != nil {
		return exits.Write(k.stderr, k.exitErr)
	}
	return k.status
}

// turnEnded marks the end of the launch steps and of each of the loop's turns, the first
// being the link's start.
func (k *keeper) turnEnded() {
	if k.cfg.turned != nil {
		k.cfg.turned(k.run)
	}
}

// finish ends the keeper: every wait disarmed, every lock closed, the run removed unless kept.
func (k *keeper) finish(keep bool) {
	for t := range k.timers {
		k.cancel(t)
	}
	if k.exitErr != nil {
		k.logf("launch failed: %v", k.exitErr)
	}
	k.close(keep)
	k.finished = true
}

// linkDown fails the launch: the link exited or was not ready in time (RN2).
func (k *keeper) linkDown() {
	k.exitErr = fault(exits.CodeRuntime, msgLinkDown, exits.Name(k.run.LinkLog()))
	k.finish(true)
}

// kitEnv is the kit's environment for the link and CMD.
func (k *keeper) kitEnv() []string {
	path := k.kit.String() + "/bin"
	if p := k.cfg.Env(envPath); p != "" {
		path += ":" + p
	}
	return []string{envPath + "=" + path, paths.EnvKit + "=" + k.kit.String()}
}

// startLink starts the link in its own session, its output appended to link.log, fd 3 its
// control socket, and greets it; an end-only start is given the end at once.
func (k *keeper) startLink(endOnly bool) error {
	if k.linkLog == nil {
		f, err := openAppend(k.run.LinkLog())
		if err != nil {
			return err
		}
		k.linkLog = f
	}
	mine, theirs, err := wire.Pair()
	if err != nil {
		return err
	}
	conn, err := wire.NewConn(mine)
	if err != nil {
		return errors.Join(err, theirs.Close())
	}
	cmd := k.cfg.LinkCmd(k.run, k.kit)
	cmd.NewSession = true
	cmd.Stdout, cmd.Stderr = k.linkLog, k.linkLog
	cmd.ExtraFiles = []*os.File{theirs}
	cmd.Env = append(cmd.Env, k.kitEnv()...)
	proc, err := k.cfg.Starter.Start(cmd)
	if cerr := theirs.Close(); cerr != nil {
		k.logf("close the link's end: %v", cerr)
	}
	if err != nil {
		return errors.Join(err, conn.Close())
	}
	l := &link{proc: proc, conn: conn, startedAt: k.cfg.Clock.Boot(), endOnly: endOnly}
	k.link = l
	if k.linkStatus == state.LinkRestarting && k.nextMS == nil {
		// keeper.json's restarting link names when it is due; this one is due now.
		due := l.startedAt.Milliseconds()
		k.nextMS = &due
	}
	k.logf("link %d started", proc.Pid())
	k.bg.Go(func() {
		st, werr := proc.Wait()
		k.send(func() { k.linkExited(l, st, werr) })
	})
	k.bg.Go(func() { k.read(l) })
	k.greet(l)
	if endOnly {
		k.endTimer = k.after(endWait, k.killLink)
	} else {
		l.readyTimer = k.after(readyWait, func() { k.notReady(l) })
	}
	return nil
}

// read posts each of the link's messages to the loop; a message the protocol refuses is
// logged and the reading goes on, and the socket's end ends it.
func (k *keeper) read(l *link) {
	for {
		m, fd, err := l.conn.Recv()
		if errors.Is(err, wire.ErrProtocol) {
			k.send(func() { k.logf("link: %v", err) })
			continue
		}
		if err != nil {
			return
		}
		k.send(func() { k.onMsg(l, m, fd) })
	}
}

// tell sends m to l; a failure is logged, and the link's exit follows it.
func (k *keeper) tell(l *link, m wire.Msg, fd *os.File) {
	if err := l.conn.Send(m, fd); err != nil {
		k.logf("send %s: %v", m.Kind, err)
	}
}

// greet is the link's start: launch, each owed notice, each held seat's add, then what is
// known of the session; an end-only start gets launch, the adds and the end (n3c-D24: Seats
// is the number of adds that follow).
func (k *keeper) greet(l *link) {
	var adds []*seat
	var fds []*os.File
	for _, s := range k.seats {
		if s.lock == nil || s.removing != nil || s.owedTxn != "" {
			continue
		}
		fd, err := s.lock.Dup()
		if err != nil {
			k.logf("seat %s: dup its lock: %v", s.team, err)
			continue
		}
		adds, fds = append(adds, s), append(fds, fd)
	}
	k.tell(l, wire.Msg{Kind: wire.KindLaunch, Launch: k.launchMsg(len(adds))}, nil)
	if !l.endOnly {
		for _, s := range k.seats {
			if s.owedTxn != "" {
				k.tell(l, wire.Msg{Kind: wire.KindOwed, Owed: &wire.Owed{Team: s.team, Txn: s.owedTxn}}, nil)
			}
		}
	}
	for i, s := range adds {
		k.sendAdd(l, s, fds[i])
	}
	if l.endOnly {
		k.tell(l, k.endMsg(), nil)
		return
	}
	if k.cmd != nil {
		k.tell(l, wire.Msg{Kind: wire.KindPID, PID: &k.cmdProc}, nil)
	}
	if k.claudeProc != nil {
		k.tell(l, wire.Msg{Kind: wire.KindClaude, Claude: k.claudeProc}, nil)
	}
	if k.token != nil {
		k.tell(l, wire.Msg{Kind: wire.KindToken, Token: k.token}, nil)
	}
}

func (k *keeper) launchMsg(seats int) *wire.Launch {
	launch := state.LaunchPlain
	if k.cfg.Launch.Mode == cli.LaunchClaude {
		launch = state.LaunchClaude
	}
	var gen int64
	if k.desired != nil {
		gen = k.desired.Generation
	}
	return &wire.Launch{
		Run: k.runID, Kind: string(k.cfg.Launch.Kind), Launch: launch, Inbound: k.inbound,
		Generation: gen, ClaudeConfigDir: k.claudeConfigDir(), Cwd: k.cfg.Cwd, Seats: seats,
	}
}

// sendAdd hands l the seat with its lock's descriptor, closed once sent.
func (k *keeper) sendAdd(l *link, s *seat, fd *os.File) {
	ids := append([]string{}, k.removedIDs[s.team]...)
	k.tell(l, wire.Msg{Kind: wire.KindAdd, Add: &wire.Add{Team: s.team, Seat: s.name, RemovedUserIDs: ids}}, fd)
	k.closeQuietly(fd)
}

// killLink ends the link deliberately: its exit is not counted.
func (k *keeper) killLink() {
	if l := k.link; l != nil {
		l.killed = true
		k.signalLink(l, syscall.SIGKILL)
	}
}

func (k *keeper) signalLink(l *link, sig syscall.Signal) {
	if err := l.proc.Signal(sig); err != nil {
		k.logf("signal the link: %v", err)
	}
}

// notReady is the 30 s ready bound: at launch it fails the launch, later it counts as a crash.
func (k *keeper) notReady(l *link) {
	k.logf("the link was not ready within %v", readyWait)
	if k.phase == launching {
		k.killLink()
		return
	}
	k.signalLink(l, syscall.SIGKILL)
}

// onMsg is one message of the link's (B 2.4.2, n3c-D6).
func (k *keeper) onMsg(l *link, m wire.Msg, fd *os.File) {
	if fd != nil {
		k.closeQuietly(fd)
	}
	if l != k.link {
		return
	}
	switch m.Kind {
	case wire.KindReady:
		k.onReady(l)
	case wire.KindStarted:
		if !k.started {
			k.logf("Claude Code's first SessionStart")
		}
		k.started = true
	case wire.KindState:
		failpoint.Hit(failpoint.KeeperLinkWriting)
		k.onState(*m.State)
	case wire.KindRemoved:
		k.onRemoved(*m.Removed)
	case wire.KindLeft:
		k.onLeft(m.Left.Team)
	case wire.KindToken:
		k.token = m.Token
	case wire.KindClaude:
		k.onClaude(*m.Claude)
	default:
		k.logf("the link sent %s, which the keeper does not take", m.Kind)
	}
}

func (k *keeper) onReady(l *link) {
	if l.ready || l.endOnly {
		k.logf("the link sent ready again")
		return
	}
	l.ready = true
	k.cancel(l.readyTimer)
	if k.phase == ending {
		return
	}
	k.linkStatus, k.nextMS = state.LinkRunning, nil
	k.writeKeeper()
	k.writeReports()
	k.logf("the link is ready")
	if k.phase == launching {
		k.phase = running
		k.startCmd()
	}
	k.reconcile()
}

// linkExited is the link's end: at launch a failed launch, at the end the last step, and
// while running a supervised restart (KP2). Any exit settles each seat being removed (LN8).
func (k *keeper) linkExited(l *link, st execx.ExitStatus, err error) {
	if err != nil {
		k.logf("wait for the link: %v", err)
	}
	k.logf("the link exited %d", st.Code)
	k.closeQuietly(l.conn)
	k.cancel(l.readyTimer)
	if k.link == l {
		k.link = nil
	}
	k.settleRemovals()
	switch k.phase {
	case launching:
		if k.stopped {
			k.status = term.ExitCode(128 + int(k.stopSig))
			k.finish(false)
			return
		}
		k.linkDown()
	case ending:
		k.cancel(k.endTimer)
		k.finish(false)
	case running:
		k.supervise(l, st)
	}
}

func (k *keeper) supervise(l *link, st execx.ExitStatus) {
	switch {
	case l.killed:
		k.restartNow()
	case st.Code == int(exits.ExitConfig) && st.Signal == 0:
		k.fail(state.WhyConfig)
	default:
		k.crashed(k.cfg.Clock.Boot() - l.startedAt)
	}
}

// crashed counts an exit: a link that ran 10 min starts the count afresh, the sixth exit is
// a crash loop, and each other waits 1 s << (count-1) (KP2).
func (k *keeper) crashed(ran time.Duration) {
	if ran >= stableRun {
		k.restarts = 0
	}
	k.restarts++
	if k.restarts >= failedAfter {
		k.fail(state.WhyCrashLoop)
		return
	}
	delay := time.Second << (k.restarts - 1)
	next := (k.cfg.Clock.Boot() + delay).Milliseconds()
	k.linkStatus, k.nextMS = state.LinkRestarting, &next
	k.writeKeeper()
	k.writeReports()
	k.restartTimer = k.after(delay, func() {
		k.restartTimer = nil
		k.restartNow()
	})
}

// restartNow starts the link again; a start that fails counts as an exit 1.
func (k *keeper) restartNow() {
	k.linkStatus = state.LinkRestarting
	if err := k.startLink(false); err != nil {
		k.logf("restart the link: %v", err)
		k.crashed(0)
	}
}

// fail is a link failed for good: keeper.json and every report say so first, then every seat
// lock is closed (T23) and the desired membership is no longer followed.
func (k *keeper) fail(why string) {
	k.linkStatus, k.why, k.nextMS = state.LinkFailed, &why, nil
	k.writeKeeper()
	k.writeReports()
	k.logf("the link failed: %s", why)
	for _, s := range k.seats {
		k.cancelSeat(s)
		k.closeSeatLock(s)
	}
	k.seats = nil
}

// startCmd is step 7: CMD in the keeper's process group with the terminal's streams.
func (k *keeper) startCmd() {
	env := append([]string{paths.EnvRuntime + "=" + k.run.Dir()}, k.kitEnv()...)
	c := execx.Cmd{
		Name: k.argv[0], Args: k.argv[1:], Env: env,
		Stdin: k.cfg.Stdin, Stdout: k.stdout, Stderr: k.stderr,
	}
	proc, err := k.cfg.Starter.Start(c)
	if err != nil {
		k.logf("start CMD: %v", err)
		k.cmdDone, k.cmdStatus = true, execx.ExitStatus{Code: exitNoCmd}
		k.failedCmd = k.cfg.Launch.Mode == cli.LaunchClaude
		k.end()
		return
	}
	k.cmd = proc
	ticks, err := k.cfg.Proc.StartTicks(proc.Pid())
	if err != nil {
		k.logf("read CMD's start time: %v", err)
	}
	k.cmdProc = state.Process{PID: proc.Pid(), StartTicks: ticks}
	if err := k.writeSessionPID(&k.cmdProc); err != nil {
		k.logf("session.pid: %v", err)
	}
	if k.link != nil {
		k.tell(k.link, wire.Msg{Kind: wire.KindPID, PID: &k.cmdProc}, nil)
	}
	k.logf("CMD %d started", proc.Pid())
	k.bg.Go(func() {
		st, werr := proc.Wait()
		k.send(func() { k.cmdExited(st, werr) })
	})
}

func (k *keeper) claudeAlive() bool { return k.claude != nil && !k.claudeDone }

// onClaude is Claude Code's process, which the link learned at SessionStart: held by a pidfd,
// checked by its start time; CMD's own pid is CMD.
func (k *keeper) onClaude(p state.Process) {
	if k.claudeProc != nil {
		k.logf("the link named Claude Code again; ignored")
		return
	}
	if k.cmd != nil && p.PID == k.cmd.Pid() {
		k.logf("Claude Code is CMD %d", p.PID)
		k.claudeProc = &p
		return
	}
	fd, err := k.cfg.Pidfds.Open(p.PID)
	if err != nil {
		k.logf("open a pidfd on Claude Code %d: %v", p.PID, err)
		return
	}
	if ticks, terr := k.cfg.Proc.StartTicks(p.PID); terr != nil || ticks != p.StartTicks {
		k.logf("Claude Code %d is not the process the link named", p.PID)
		k.closeQuietly(fd)
		return
	}
	k.claude, k.claudeProc = fd, &p
	k.logf("Claude Code %d is held by a pidfd", p.PID)
}

func (k *keeper) cmdExited(st execx.ExitStatus, err error) {
	if err != nil {
		k.logf("wait for CMD: %v", err)
	}
	k.cmdDone, k.cmdStatus = true, st
	k.logf("CMD exited %d", st.Code)
	if k.cfg.Launch.Mode == cli.LaunchClaude && !k.started && !k.stopped {
		k.failedCmd = true
	}
	if k.claudeAlive() && !k.stopped {
		k.orphan(func(sig syscall.Signal) { k.signalClaude(sig) })
	}
	k.maybeEnd()
}

func (k *keeper) claudeExited() {
	k.claudeDone = true
	k.logf("Claude Code exited")
	if !k.cmdDone && !k.stopped {
		k.orphan(k.signalCmd)
	}
	k.maybeEnd()
}

// orphan ends the one of CMD and Claude Code still running once the other has: TERM after
// 3 s, KILL 5 s later (KP1).
func (k *keeper) orphan(sig func(syscall.Signal)) {
	k.afterTerm = k.after(orphanTerm, func() {
		sig(syscall.SIGTERM)
		k.afterKill = k.after(orphanKill, func() { sig(syscall.SIGKILL) })
	})
}

func (k *keeper) maybeEnd() {
	if k.phase == running && k.cmdDone && !k.claudeAlive() {
		k.end()
	}
}

func (k *keeper) signalCmd(sig syscall.Signal) {
	if k.cmd == nil || k.cmdDone {
		return
	}
	k.cmdSent[sig] = true
	if err := k.cmd.Signal(sig); err != nil {
		k.logf("signal CMD: %v", err)
	}
}

func (k *keeper) signalClaude(sig syscall.Signal) {
	if !k.claudeAlive() {
		return
	}
	if err := k.claude.Signal(sig); err != nil {
		k.logf("signal Claude Code: %v", err)
	}
}

// onSignal: TERM and HUP stop the session; INT and QUIT are the terminal's, for CMD alone.
func (k *keeper) onSignal(s os.Signal) {
	if sig, ok := s.(syscall.Signal); ok && (sig == syscall.SIGTERM || sig == syscall.SIGHUP) {
		k.stop(sig)
		return
	}
	k.logf("signal %v ignored", s)
}

// stop forwards a stop once: to Claude Code by its pidfd when known, else to CMD; TERM to
// whichever still runs at 5 s, KILL at 7 s (KP1). While launching it kills the link.
func (k *keeper) stop(sig syscall.Signal) {
	if k.stopped {
		k.logf("signal %v: the stop is already under way", sig)
		return
	}
	k.stopped, k.stopSig = true, sig
	k.logf("stop: %v", sig)
	if k.phase == launching {
		k.killLink()
		return
	}
	if k.phase == ending {
		return
	}
	k.cancel(k.afterTerm)
	k.cancel(k.afterKill)
	if k.claudeAlive() {
		k.signalClaude(sig)
	} else {
		k.signalCmd(sig)
	}
	both := func(s syscall.Signal) {
		k.signalClaude(s)
		k.signalCmd(s)
	}
	k.afterTerm = k.after(stopTermAfter, func() { both(syscall.SIGTERM) })
	k.afterKill = k.after(stopKillAfter, func() { both(syscall.SIGKILL) })
}

// how is the end's kind (KP3).
func (k *keeper) how() string {
	st := k.cmdStatus
	switch {
	case k.failedCmd:
		return wire.EndLaunchFailed
	case k.stopped:
		return wire.EndStop
	case k.cfg.Launch.Kind == cli.KindCCY && st.Code == int(exits.ExitBusy) && st.Signal == 0:
		return wire.EndRestart
	case st.Signal != 0 && !k.cmdSent[st.Signal]:
		return wire.EndKilled
	}
	return wire.EndExit
}

func (k *keeper) endMsg() wire.Msg {
	return wire.Msg{Kind: wire.KindEnd, End: &wire.End{How: k.how(), Status: k.cmdStatus.Code}}
}

// end is the session's end (KP3): the end to a live link, an end-only start for a link
// waiting to restart, none for a failed one; the link has 5 s before it is killed.
func (k *keeper) end() {
	k.phase = ending
	k.cancel(k.afterTerm)
	k.cancel(k.afterKill)
	for _, s := range k.seats {
		k.cancelSeat(s)
	}
	k.status = term.ExitCode(k.cmdStatus.Code)
	if k.failedCmd {
		k.status = exits.ExitConfig
		k.launchFailed()
	}
	k.logf("the session ended: %s, status %d", k.how(), k.cmdStatus.Code)
	failpoint.Hit(failpoint.KeeperBeforeEnd)
	switch {
	case k.link != nil:
		k.tell(k.link, k.endMsg(), nil)
		k.endTimer = k.after(endWait, k.killLink)
	case k.linkStatus == state.LinkFailed:
		k.finish(false)
	default:
		k.cancel(k.restartTimer)
		if err := k.startLink(true); err != nil {
			k.logf("start the link for the end: %v", err)
			k.finish(false)
		}
	}
}

// launchFailed records a launch failure (B 2.4.1 step 7): each seat's lock record and report.
func (k *keeper) launchFailed() {
	at := k.cfg.Clock.WallMS()
	for _, s := range k.seats {
		if s.lock == nil {
			continue
		}
		rec := state.LockRecord{V: state.Version, Run: k.runID, StartedMS: s.startedMS, LaunchFailedMS: &at}
		if err := state.WriteLockRecord(s.lock, rec); err != nil {
			k.logf("seat %s: write its lock record: %v", s.team, err)
		}
	}
	k.writeReports()
	k.logf("launch failed: CMD exited %d before Claude Code's first SessionStart", k.cmdStatus.Code)
}
