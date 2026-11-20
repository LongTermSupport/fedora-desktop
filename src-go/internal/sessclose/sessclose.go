// Package sessclose closes a ccy or cc session the way a person would (Plan 00191 Task 4.12):
// /compact typed into its tmux pane, the compaction waited for, then /exit, so the next resume
// starts from a small context while this one's prompt cache is still warm. It is the one copy
// of that logic: `ccy-teams shutdown`, `ccy --stop` and a ccy-sessions row's Close call it. It
// acts only through tmuxccy (the pane, the keys, the session list) and reads the session's
// record and transcript; it starts no tmux command of its own.
//
// Intended behaviour, each held by a test:
//
//   - C1. Close takes the sessions to close, each with NoCompact or not, and returns one Result
//     per target in their order: Exited (claude ended), Stopped (its tmux session was killed)
//     or Failed (a Note says why and what to do), with whether it was compacted first.
//   - C2. NoCompact sends /exit alone and reads no transcript.
//   - C3. Otherwise the session's own transcript is found first (its record's conversation,
//     never the newest in a shared folder); one that has not grown since its last compaction
//     (transcript.CompactedLast) goes straight to /exit, with no compaction noted.
//   - C4. At a ready, empty box /compact is typed with the close instruction, then Enter after
//     the submit pause. A busy box is never interrupted (no Escape): its turn is waited for, up
//     to Waits.Prompt, and a box holding unsent text is never typed over: Failed, untouched. A
//     /compact left in the box gets one more Enter.
//   - C5. The compaction's end is its compact_boundary in the transcript after the time it was
//     typed. A session that records no conversation (no record, or launched without the agent
//     team bus) is watched on its pane instead: busy then ready again, or ready for the settle
//     bound; the note names that source.
//   - C6. A pane ready again with no boundary within the settle bound fails the close.
//   - C7. A compaction not complete within Waits.Compact (10 min; the longest measured took
//     254 s), or a transcript that cannot be found or read, is a loud failure naming the
//     session's state: /exit is not typed, the session is left running and never killed, and
//     --no-compact is named as how to end it uncompacted.
//   - C8. Then /exit (ccy-teams shutdown's SD10-SD13, moved here): typed at a ready box; claude's
//     "Background work is running" question answered "Exit and stop tasks" once when the cursor
//     is on it; a launcher that has exited has its window closed; a session at a launcher
//     question, whose prompt never readies within Waits.Prompt or that has not ended within
//     Waits.Exit of /exit, is stopped: its tmux session killed and its record removed.
//   - C9. Live is the sessions on ccy's tmux server; Limits the bounds the Closer works to.
//   - C10. Seconds is a bound as a sentence reads it ("60 s").
//
// A session in an LXC container (a Target with a Guest; Plan 00257 SPEC-team-close.md TC2, TC9)
// is closed the same way on the tmux server it is on there:
//
//   - G1. No session record is read for it: its conversation is in the container, so its
//     compaction's end is read from its pane (C5), the note saying so.
//   - G2. It is never killed and nothing of it is stopped: where a host session would be stopped
//     (C8), it fails, named as left running in its container. Not ended within Waits.Exit of
//     /exit is "exit refused"; a prompt not ready within Waits.Prompt is "busy past the bound".
//   - G3. A container whose server cannot be listed fails its own sessions as "unreachable",
//     and no other.
//   - G4. A pane with no claude running (ended, at a launcher question) fails it untouched.
//
// The wind-down before a planned reboot (fedora-desktop#290) is the same compaction without the
// close: the session is compacted while its prompt cache is warm and left running, and the
// reboot ends it; the restore brings it back.
//
//   - W1. Compact winds one session down, CompactAll several, watched together against one
//     deadline: WindDownText is typed into an idle, empty box, its end waited for as C5-C6,
//     and the session is left running. /exit is never typed, nothing is killed, no record is
//     removed. An idle box is first given the settle bound, so a warning being typed into it
//     lands before the /compact.
//   - W2. A session in a turn is never interrupted: the turn is waited for, then it is
//     compacted. Result.Busy says it was in one.
//   - W3. At the deadline a session not yet compacted is Unfinished, saying whether it was in a
//     turn, held unsent text or had its compaction still running (its transcript read once
//     more first); it is left as it is.
//   - W4. Skipped, with why and nothing typed: a conversation with nothing since its last
//     compaction (C3), a session whose claude is not running, one that ended. A record or
//     transcript that cannot be read is Failed with nothing typed.
//   - W5. A failure after the /compact went in names the session as still running.
//   - W6. Results come back one per session, in order.
package sessclose

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/conversation"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/screen"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/discover"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/transcript"
)

// CompactText is what a session is compacted with before it exits: /compact's own argument, one
// line, the space after the command closing Claude's completion menu.
const CompactText = "/compact This session is being closed, so keep what is needed to resume the work " +
	"later: what was in progress, where each unfinished piece stands, what comes next, and the " +
	"decisions made; drop the rest."

// WindDownText is what a session is compacted with before a planned reboot (W1): it is resumed
// afterwards on what it keeps.
const WindDownText = "/compact The machine is going down and this session will be resumed afterwards, so keep " +
	"what is needed to carry on the work: what was in progress, where each unfinished piece stands, " +
	"what comes next, and the decisions made; drop the rest."

// exitCommand is what a person types to end claude.
const exitCommand = "/exit"

// prefixDirect is restore.Judge's word for claude run with no launcher, as it runs in a
// container.
const prefixDirect = "cc"

// composeStop is the launcher's "Stop compose services?" question (screen.KnownPrompts): it is
// asked after claude has ended.
const composeStop = "compose-stop"

// What a result says of a session.
const (
	whyUnsent        = "its input box holds unsent text; attach it (ccy-sessions), send or clear it, and close it again"
	noteByItself     = "ended by itself before /exit was typed"
	noteCompose      = "its launcher asks whether to stop compose services: attach it (ccy-sessions) to answer"
	noteEndedAfter   = "claude ended and its launcher reported an error, so the window was closed"
	noteEndedBefore  = "it had already ended (its launcher reported an error), so the window was closed"
	noteKilledSuffix = ", so its tmux session was killed"
	noteCompacted    = "compacted first"
	noteUncompacted  = "; nothing was typed and it is still running; close it with --no-compact to end it without compacting"
	noteUntouched    = "; nothing was typed and it is still running"
	noteLeftClose    = "/exit was not typed and it is still running: attach it (ccy-sessions)"
	noteLeftStay     = "it is still running: attach it (ccy-sessions)"
	byDeadline       = " by the wind-down's deadline"
	noteAskedOffExit = `: claude was asking "Background work is running" (a background task of the session) ` +
		`with the cursor off "Exit and stop tasks"`
)

// Waits are the pauses and the bounds of a close.
type Waits struct {
	// Submit is the pause between typed text and Enter: an Enter arriving with the text is read
	// as part of it.
	Submit time.Duration
	// Poll is how often the panes are read again.
	Poll time.Duration
	// Prompt bounds the wait for claude's prompt before a line can be typed: a turn running is
	// waited for, never interrupted, so it is the team wind-down's bound for a busy session.
	Prompt time.Duration
	// Compact bounds the wait for a typed /compact to complete.
	Compact time.Duration
	// Settle bounds a ready pane with no compact_boundary yet, a pane ready with no busy look
	// caught, and a /compact left in the box after its second Enter.
	Settle time.Duration
	// Exit bounds the wait for a session to end after /exit.
	Exit time.Duration
}

// DefaultWaits are the production waits.
var DefaultWaits = Waits{
	Submit: 200 * time.Millisecond, Poll: time.Second, Prompt: 300 * time.Second,
	Compact: 600 * time.Second, Settle: 30 * time.Second, Exit: 60 * time.Second,
}

// Transcripts reads a conversation's compactions (transcript.Cache).
type Transcripts interface {
	CompactedLast(path string) (bool, error)
	CompactedSince(path string, since int64) (bool, error)
}

// Closing is what a caller closes sessions through: a Closer, or a test's stand-in.
type Closing interface {
	Close(ctx context.Context, targets []Target) []Result
	Limits() Waits
}

// WindingDown is what a caller winds sessions down through before a planned reboot: a Closer,
// or a test's stand-in.
type WindingDown interface {
	CompactAll(ctx context.Context, names []string, by time.Time) []Result
}

// Target is one session to close: on ccy's tmux server by its session name, or, with Guest, in
// an LXC container.
type Target struct {
	Name      string
	NoCompact bool
	Guest     *Guest
}

// Guest is a session in an LXC container (G1-G5): the tmux server it is on there (reached
// through lxc-attach), its session's name on it, and where it is, in words ("container box1").
// Name is then only the result's label.
type Guest struct {
	Tmux    *tmuxccy.Tmux
	Session string
	Where   string
}

// Outcome is where a session's close stands.
type Outcome int

// The outcomes.
const (
	Pending Outcome = iota
	Exited
	Stopped
	Failed
	// Compacted is a wound-down session compacted and left running (W1).
	Compacted
	// Unfinished is a wound-down session not compacted by the deadline (W3).
	Unfinished
	// Skipped is a wound-down session there was nothing to compact in (W4).
	Skipped
)

// Result is how one session's close or wind-down ended. Busy is a wound-down session that was
// in a turn when first looked at (W2).
type Result struct {
	Name      string
	Outcome   Outcome
	Note      string
	Compacted bool
	Busy      bool
}

// Closer closes sessions. Every dependency is a field so a test stands in for each.
type Closer struct {
	Tmux        *tmuxccy.Tmux
	Registry    func() (*registry.Store, error)
	Transcripts Transcripts
	// Transcript is the file of the conversation a recorded session is in.
	Transcript func(registry.Record) (string, error)
	Now        func() time.Time
	Sleep      func(time.Duration)
	Waits      Waits
}

// New is the production Closer over a tmux server and the session registry; getenv says where
// Claude Code keeps a host session's transcripts.
func New(t *tmuxccy.Tmux, reg func() (*registry.Store, error), getenv func(string) string) *Closer {
	home := conversation.ClaudeHome(getenv)
	return &Closer{
		Tmux: t, Registry: reg, Transcripts: transcript.NewCache(),
		Transcript: func(rec registry.Record) (string, error) { return conversation.Transcript(rec, home) },
		Now:        time.Now, Sleep: time.Sleep, Waits: DefaultWaits,
	}
}

// Limits is the bounds the Closer works to, for a caller's preview.
func (c *Closer) Limits() Waits { return c.Waits }

// Live is the sessions on ccy's tmux server, by name (C9).
func (c *Closer) Live(ctx context.Context) (map[string]bool, error) { return liveOn(ctx, c.Tmux) }

// unwatched fails a session whose server could not be listed: a container's as unreachable
// (G3).
func (c *Closer) unwatched(j *job, err error) {
	if j.Guest != nil {
		j.done(Failed, "unreachable: "+err.Error()+"; the session could not be watched any further")
		return
	}
	j.done(Failed, err.Error()+"; the sessions could not be watched any further")
}

// liveOn is the sessions on the tmux server t, by name.
func liveOn(ctx context.Context, t *tmuxccy.Tmux) (map[string]bool, error) {
	list, err := t.List(ctx)
	if err != nil {
		return nil, err
	}
	live := map[string]bool{}
	for _, sess := range list {
		live[sess.Name] = true
	}
	return live, nil
}

// Seconds is a bound as a sentence reads it: "60 s".
func Seconds(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', -1, 64) + " s"
}

// phase is how far a session's close has gone.
type phase int

const (
	phaseCompact phase = iota
	phaseCompacting
	phaseExit
)

// job is one session being closed: tmux is the server it is on and pane its session's name
// there.
type job struct {
	Target

	tmux *tmuxccy.Tmux
	pane string

	phase phase
	// file is the transcript the compaction is read from; "" when it is read from the pane,
	// paneWhy saying why.
	file, paneWhy string

	compactReentered, typed, reentered bool
	// asked is claude's background work question seen after /exit; answered, its "Exit and
	// stop tasks" chosen.
	asked, answered bool
	compactAt       time.Time
	quiet           Quiet
	// boundaryBy is when a ready pane with no boundary fails; stuckBy when a /compact still in
	// the box after its second Enter does.
	boundaryBy, stuckBy time.Time
	deadline            time.Time

	result      Outcome
	note        string
	compactNote string
	compacted   bool

	// stay is a wind-down (W1): compacted and left running, its deadline the wind-down's. busy
	// is a turn seen running before the /compact; last the box as last seen before it.
	stay bool
	busy bool
	last screen.State
}

func (j *job) done(result Outcome, note string) { j.result, j.note = result, note }

// left is what a failure after the /compact went in says of the session (C7, W5).
func (j *job) left() string {
	switch {
	case j.Guest != nil:
		return "/exit was not typed and it is still running in " + j.Guest.Where + ": attach it there"
	case j.stay:
		return noteLeftStay
	}
	return noteLeftClose
}

// leftIn ends the note on a session in a container that is left running where a host session
// would be stopped (G2).
func (j *job) leftIn() string {
	return ", so it was left running in " + j.Guest.Where + ": attach it there and type /exit"
}

// newJob is a target's job, on the server its session is on.
func (c *Closer) newJob(t Target, by time.Time) *job {
	j := &job{Target: t, tmux: c.Tmux, pane: t.Name, deadline: by}
	if t.Guest != nil {
		j.tmux, j.pane = t.Guest.Tmux, t.Guest.Session
	}
	return j
}

// untouched ends a note on a session nothing was typed into.
func (j *job) untouched() string {
	if j.stay {
		return noteUntouched
	}
	return noteUncompacted
}

// Close closes every target and returns their results in order (C1).
func (c *Closer) Close(ctx context.Context, targets []Target) []Result {
	jobs := make([]*job, len(targets))
	by := c.Now().Add(c.Waits.Prompt)
	for i, t := range targets {
		jobs[i] = c.newJob(t, by)
	}
	return c.results(ctx, jobs)
}

// Compact winds one session down before a planned reboot: compacted by the deadline and left
// running (W1-W5).
func (c *Closer) Compact(ctx context.Context, name string, by time.Time) Result {
	return c.CompactAll(ctx, []string{name}, by)[0]
}

// CompactAll winds the sessions down together against the one deadline, and returns their
// results in order (W6).
func (c *Closer) CompactAll(ctx context.Context, names []string, by time.Time) []Result {
	jobs := make([]*job, len(names))
	for i, name := range names {
		jobs[i] = c.newJob(Target{Name: name}, by)
		jobs[i].stay = true
	}
	return c.results(ctx, jobs)
}

// results runs the jobs and returns their results in order.
func (c *Closer) results(ctx context.Context, jobs []*job) []Result {
	c.prepare(jobs)
	c.run(ctx, jobs)
	results := make([]Result, len(jobs))
	for i, j := range jobs {
		note := j.note
		if j.compactNote != "" && j.result != Failed {
			note = join(note, j.compactNote)
		}
		results[i] = Result{Name: j.Name, Outcome: j.result, Note: note, Compacted: j.compacted, Busy: j.busy}
	}
	return results
}

func join(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}

// prepare finds, before anything is typed, where each compaction's end will be read (C2, C3,
// C5, C7).
func (c *Closer) prepare(jobs []*job) {
	var compacting []*job
	for _, j := range jobs {
		switch {
		case j.NoCompact:
			j.phase = phaseExit
			continue
		case j.Guest != nil:
			j.paneWhy = "it runs in " + j.Guest.Where + ", whose conversation this host does not read"
			continue
		}
		compacting = append(compacting, j)
	}
	if len(compacting) == 0 {
		return
	}
	store, err := c.Registry()
	if err != nil {
		for _, j := range compacting {
			j.done(Failed, "the session records could not be read: "+err.Error()+j.untouched())
		}
		return
	}
	for _, j := range compacting {
		c.prepareOne(store, j)
	}
}

func (c *Closer) prepareOne(store *registry.Store, j *job) {
	rec, err := store.Get(j.Name)
	switch {
	case errors.Is(err, registry.ErrNoRecord):
		j.paneWhy = "it has no session record, so its conversation is not known"
		return
	case err != nil:
		j.done(Failed, "its session record could not be read: "+err.Error()+j.untouched())
		return
	case !discover.LaunchedWithBus(rec):
		j.paneWhy = "it was launched without the agent team bus, so its conversation is not recorded"
		return
	}
	if j.file, err = c.Transcript(rec); err != nil {
		j.done(Failed, "its transcript could not be found: "+err.Error()+j.untouched())
		return
	}
	last, err := c.Transcripts.CompactedLast(j.file)
	if err != nil {
		j.done(Failed, "its transcript could not be read: "+err.Error()+j.untouched())
		return
	}
	switch {
	case last && j.stay:
		j.done(Skipped, "nothing since its last compaction, so it was not compacted again")
	case last:
		j.phase = phaseExit
	}
}

// run reads every pane, then again every poll, until each session has ended, been stopped or
// failed.
func (c *Closer) run(ctx context.Context, jobs []*job) {
	for {
		lives := map[*tmuxccy.Tmux]map[string]bool{}
		failed := map[*tmuxccy.Tmux]error{}
		left := false
		for _, j := range jobs {
			if j.result != Pending {
				continue
			}
			live, read := lives[j.tmux]
			if _, bad := failed[j.tmux]; !read && !bad {
				var err error
				if live, err = liveOn(ctx, j.tmux); err != nil {
					failed[j.tmux] = err
				}
				lives[j.tmux] = live
			}
			if err := failed[j.tmux]; err != nil {
				c.unwatched(j, err)
				continue
			}
			c.step(ctx, j, live)
			left = left || j.result == Pending
		}
		if !left {
			return
		}
		c.Sleep(c.Waits.Poll)
	}
}

// step is one look at a session and what follows from it.
func (c *Closer) step(ctx context.Context, j *job, live map[string]bool) {
	if !live[j.pane] {
		c.ended(j)
		return
	}
	pane, err := j.tmux.CapturePane(ctx, j.pane, true)
	if err != nil {
		// A session that ended since the list was read is seen gone on the next pass. Any other
		// failure kills nothing: the pane may hold unsent text.
		if !errors.Is(err, tmuxccy.ErrSessionGone) {
			j.done(Failed, err.Error()+"; nothing was typed or stopped")
		}
		return
	}
	prefix, _, found := strings.Cut(j.Name, "-")
	switch {
	case j.Guest != nil:
		prefix = prefixDirect
	case !found:
		prefix = j.Name
	}
	verdict := restore.Judge(prefix, true, screen.Plain(pane), restore.ContainerUp, restore.GoingOutcome{})
	if c.notClaude(ctx, j, verdict) {
		return
	}
	if err = c.look(ctx, j, pane); err != nil {
		j.done(Failed, err.Error())
		return
	}
	if j.result != Pending || c.Now().Before(j.deadline) {
		return
	}
	if j.stay {
		c.unfinished(j)
		return
	}
	c.timedOut(ctx, j)
}

// notClaude ends a session whose pane shows no claude running, and says whether it did: a
// wind-down skips it untouched (W4); a close closes its window or stops it (C8).
func (c *Closer) notClaude(ctx context.Context, j *job, verdict restore.Verdict) bool {
	switch verdict.State {
	case restore.StateDead, restore.StateNotStarted, restore.StateWaitingAtPrompt:
		if j.stay {
			j.done(Skipped, "claude is not running in it ("+verdict.Detail+"); nothing was typed")
			return true
		}
	case restore.StateOK, restore.StateStarting, restore.StateSettingGoing, restore.StateNotSetGoing:
		return false
	}
	if j.Guest != nil {
		c.guestNotClaude(j, verdict)
		return true
	}
	switch {
	case verdict.State != restore.StateWaitingAtPrompt:
		c.closeWindow(ctx, j, verdict)
	case verdict.Detail == composeStop:
		j.done(Exited, noteCompose)
	default:
		c.hardStop(ctx, j, "it was at the launcher's "+verdict.Detail+" question, so claude was not running; its tmux session was killed")
	}
	return true
}

// guestNotClaude is a session in a container whose pane shows no claude running: after /exit
// it ended; otherwise it fails. Its window is left as it is either way (G4).
func (c *Closer) guestNotClaude(j *job, verdict restore.Verdict) {
	if j.typed && verdict.Ended() {
		j.done(Exited, "claude ended; its window is left in "+j.Guest.Where)
		return
	}
	untouched := "; nothing was stopped"
	if j.phase == phaseCompact {
		untouched = "; nothing was typed or stopped"
	}
	j.done(Failed, "claude is not running in it ("+verdict.Detail+")"+untouched)
}

// look is what the session's phase and screen call for.
func (c *Closer) look(ctx context.Context, j *job, pane string) error {
	state := screen.ClaudeState(pane)
	switch j.phase {
	case phaseCompact:
		if j.stay {
			return c.waitTurn(ctx, j, state)
		}
		return c.ready(ctx, j, state, CompactText)
	case phaseCompacting:
		return c.compacting(ctx, j, state)
	case phaseExit:
	}
	if err := c.answerBackgroundWork(ctx, j, pane); err != nil {
		return err
	}
	if !j.typed {
		return c.ready(ctx, j, state, exitCommand)
	}
	switch state {
	case screen.Typed:
		if !j.reentered {
			j.reentered = true
			return j.tmux.SendEnter(ctx, j.pane)
		}
	case screen.Ready, screen.Busy, screen.NotDrawn:
	}
	return nil
}

// ready types line at a ready box, waits on a busy one (its turn is never interrupted), and fails
// over unsent text (C4, C8).
func (c *Closer) ready(ctx context.Context, j *job, state screen.State, line string) error {
	switch state {
	case screen.Ready:
		return c.typeLine(ctx, j, line)
	case screen.Typed:
		j.done(Failed, whyUnsent)
	case screen.Busy, screen.NotDrawn:
	}
	return nil
}

// waitTurn types the wind-down's /compact once the box is idle and empty: at once after a turn
// seen running, else after the settle bound, so a warning being typed in lands first. A busy
// box is never interrupted, and unsent text never typed over (W1, W2).
func (c *Closer) waitTurn(ctx context.Context, j *job, state screen.State) error {
	j.last = state
	switch state {
	case screen.Busy:
		j.busy = true
	case screen.Ready, screen.Typed, screen.NotDrawn:
	}
	if !j.quiet.Idle(state, c.Now(), c.Waits.Settle) {
		return nil
	}
	return c.typeLine(ctx, j, WindDownText)
}

// unfinished is a wound-down session the deadline came for, saying where it stood; nothing is
// done to it. A compaction's transcript is read once more, so one that ended on this look
// counts (W3).
func (c *Closer) unfinished(j *job) {
	if j.phase == phaseCompacting {
		if j.file != "" {
			if done, err := c.Transcripts.CompactedSince(j.file, j.compactAt.Unix()); err == nil && done {
				c.stayed(j, "")
				return
			}
		}
		j.done(Unfinished, "its compaction had not finished"+byDeadline+"; it is still running")
		return
	}
	switch j.last {
	case screen.Busy:
		j.done(Unfinished, "it was still in a turn"+byDeadline+", so /compact was not typed")
	case screen.Typed:
		j.done(Unfinished, "its input box held unsent text"+byDeadline+", so /compact was not typed")
	case screen.Ready, screen.NotDrawn:
		j.done(Unfinished, "its prompt was not idle"+byDeadline+", so /compact was not typed")
	}
}

// stayed is a wound-down session whose compaction ended: compacted and left running.
func (c *Closer) stayed(j *job, note string) {
	j.compacted = true
	j.done(Compacted, note)
}

// typeLine submits the line, each Enter on its own after the submit pause, checking the box took
// it (tmuxccy.Submit), and starts the wait that follows it. A screen with no box after the Enter
// (/exit's question, its held window, Claude Code ending) is left to that wait, which reads each
// screen it knows and is bounded.
func (c *Closer) typeLine(ctx context.Context, j *job, line string) error {
	at := c.Now()
	err := j.tmux.Submit(ctx, j.pane, line, tmuxccy.SubmitPace{Pause: c.Waits.Submit, Sleep: c.Sleep})
	if errors.Is(err, tmuxccy.ErrNotSubmitted) {
		if j.Guest != nil {
			return errors.New(err.Error() + "; it is still running in " + j.Guest.Where + ": attach it there")
		}
		return errors.New(err.Error() + "; it is still running: attach it (ccy-sessions)")
	}
	if err != nil && !errors.Is(err, tmuxccy.ErrBoxNotDrawn) {
		return err
	}
	if line == exitCommand {
		j.typed, j.deadline = true, c.Now().Add(c.Waits.Exit)
		return nil
	}
	j.phase, j.compactAt = phaseCompacting, at
	if !j.stay {
		j.deadline = c.Now().Add(c.Waits.Compact)
	}
	j.quiet.Reset()
	return nil
}

// compacting watches a typed /compact until its end (C4-C6).
func (c *Closer) compacting(ctx context.Context, j *job, state screen.State) error {
	now := c.Now()
	switch state {
	case screen.Typed:
		if !j.compactReentered {
			j.compactReentered, j.stuckBy = true, now.Add(c.Waits.Settle)
			return j.tmux.SendEnter(ctx, j.pane)
		}
		if !now.Before(j.stuckBy) {
			j.done(Failed, "its /compact stayed in its input box after a second Enter, so "+j.left())
		}
		return nil
	case screen.Ready, screen.Busy, screen.NotDrawn:
	}
	idle := j.quiet.Idle(state, now, c.Waits.Settle)
	if j.file == "" {
		if idle {
			c.compacted(j, noteCompacted+" (its end read from its pane: "+j.paneWhy+")")
		}
		return nil
	}
	done, err := c.Transcripts.CompactedSince(j.file, j.compactAt.Unix())
	switch {
	case err != nil:
		j.done(Failed, "its compaction could not be read: "+err.Error()+"; "+j.left())
	case done:
		c.compacted(j, noteCompacted)
	case !idle:
		j.boundaryBy = time.Time{}
	case j.boundaryBy.IsZero():
		j.boundaryBy = now.Add(c.Waits.Settle)
	case !now.Before(j.boundaryBy):
		j.done(Failed, "its /compact ended with no compact_boundary in its transcript within "+Seconds(c.Waits.Settle)+
			", so "+j.left())
	}
	return nil
}

// compacted moves a session on to its /exit, with a fresh prompt wait; a wound-down session is
// done, left running (W1).
func (c *Closer) compacted(j *job, note string) {
	if j.stay {
		if note == noteCompacted {
			note = ""
		}
		c.stayed(j, note)
		return
	}
	j.phase, j.compacted, j.compactNote = phaseExit, true, note
	j.deadline = c.Now().Add(c.Waits.Prompt)
}

// timedOut ends a session whose wait is over, saying which wait it was: a compaction is never
// interrupted (C7); any other wait stops the session (C8).
func (c *Closer) timedOut(ctx context.Context, j *job) {
	switch {
	case j.phase == phaseCompacting:
		j.done(Failed, "its /compact did not complete within "+Seconds(c.Waits.Compact)+", so "+j.left())
	case j.typed:
		late := "it did not end within " + Seconds(c.Waits.Exit) + " of /exit"
		if j.asked && !j.answered {
			late += noteAskedOffExit
		}
		if j.Guest != nil {
			j.done(Failed, "exit refused: "+late+j.leftIn())
			return
		}
		c.hardStop(ctx, j, late+noteKilledSuffix)
	default:
		late := "its prompt was not ready within " + Seconds(c.Waits.Prompt)
		if j.Guest != nil {
			j.done(Failed, "busy past the bound: "+late+j.leftIn())
			return
		}
		c.hardStop(ctx, j, late+noteKilledSuffix)
	}
}

// answerBackgroundWork answers claude's "Background work is running" question, which /exit
// brings up while a background task of the session runs: the close was asked for, so Enter
// chooses "Exit and stop tasks", once, and only while the cursor is on it. The wait for the end
// starts again from the answer.
func (c *Closer) answerBackgroundWork(ctx context.Context, j *job, pane string) error {
	if !j.typed || j.answered {
		return nil
	}
	shown, exitFocused := screen.AtBackgroundWorkExit(pane)
	j.asked = j.asked || shown
	if !exitFocused {
		return nil
	}
	if err := j.tmux.SendEnter(ctx, j.pane); err != nil {
		return err
	}
	j.answered = true
	j.deadline = c.Now().Add(c.Waits.Exit)
	return nil
}

// ended is a session whose tmux session has gone.
func (c *Closer) ended(j *job) {
	if j.stay {
		j.done(Skipped, "it ended before it was compacted")
		return
	}
	if j.typed {
		j.done(Exited, "")
		return
	}
	j.done(Exited, noteByItself)
}

// closeWindow closes a pane whose launcher has exited, as its Enter would. A launcher that
// stopped where nobody could answer keeps its record (restore.DetailUnattendedStop), and closing its
// window does not change that.
func (c *Closer) closeWindow(ctx context.Context, j *job, verdict restore.Verdict) {
	if !verdict.Ended() {
		j.done(Failed, "its pane cannot be judged ("+verdict.Detail+")")
		return
	}
	if err := j.tmux.KillSession(ctx, j.pane); err != nil && !errors.Is(err, tmuxccy.ErrSessionGone) {
		j.done(Failed, err.Error())
		return
	}
	if j.typed {
		j.done(Exited, noteEndedAfter)
		return
	}
	j.done(Exited, noteEndedBefore)
}

// hardStop is ccy-sessions' End: the tmux session killed, then its record removed, since a
// killed pane never reaches the trampoline that would remove it.
func (c *Closer) hardStop(ctx context.Context, j *job, note string) {
	err := j.tmux.KillSession(ctx, j.pane)
	if errors.Is(err, tmuxccy.ErrSessionGone) {
		c.ended(j)
		return
	}
	if err != nil {
		j.done(Failed, err.Error())
		return
	}
	store, err := c.Registry()
	if err == nil {
		err = store.Remove(j.Name)
	}
	if err != nil {
		j.done(Failed, "its tmux session was killed, but its session record could not be removed: "+err.Error())
		return
	}
	j.done(Stopped, note)
}

// Quiet is the pane's word that a typed line has been dealt with: idle again after a busy look,
// or idle for the settle bound when no busy look was caught (a turn quicker than a poll).
type Quiet struct {
	sawBusy bool
	by      time.Time
}

// Reset starts again for a new line.
func (q *Quiet) Reset() { *q = Quiet{} }

// Saw records a look made elsewhere: a busy one counts as the busy look.
func (q *Quiet) Saw(state screen.State) {
	switch state {
	case screen.Busy:
		q.sawBusy = true
	case screen.Ready, screen.Typed, screen.NotDrawn:
	}
}

// Idle is whether the box is idle and empty again at now.
func (q *Quiet) Idle(state screen.State, now time.Time, settle time.Duration) bool {
	switch state {
	case screen.Ready:
		if q.sawBusy {
			return true
		}
		if q.by.IsZero() {
			q.by = now.Add(settle)
		}
		return !now.Before(q.by)
	case screen.Busy:
		q.sawBusy = true
	case screen.Typed, screen.NotDrawn:
	}
	q.by = time.Time{}
	return false
}
