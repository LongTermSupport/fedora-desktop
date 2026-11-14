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
//     the submit pause; a busy box is interrupted with Escape once first, and a box holding
//     unsent text is never typed over: Failed, untouched. A /compact left in the box gets one
//     more Enter.
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

// exitCommand is what a person types to end claude.
const exitCommand = "/exit"

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
	// Prompt bounds the wait for claude's prompt before a line can be typed.
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
	Submit: 200 * time.Millisecond, Poll: time.Second, Prompt: 30 * time.Second,
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

// Target is one session to close.
type Target struct {
	Name      string
	NoCompact bool
}

// Outcome is where a session's close stands.
type Outcome int

// The outcomes.
const (
	Pending Outcome = iota
	Exited
	Stopped
	Failed
)

// Result is how one session's close ended.
type Result struct {
	Name      string
	Outcome   Outcome
	Note      string
	Compacted bool
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
func (c *Closer) Live(ctx context.Context) (map[string]bool, error) {
	list, err := c.Tmux.List(ctx)
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

// job is one session being closed.
type job struct {
	Target

	phase phase
	// file is the transcript the compaction is read from; "" when it is read from the pane,
	// paneWhy saying why.
	file, paneWhy string

	escaped, compactReentered, typed, reentered bool
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
}

func (j *job) done(result Outcome, note string) { j.result, j.note = result, note }

// Close closes every target and returns their results in order (C1).
func (c *Closer) Close(ctx context.Context, targets []Target) []Result {
	jobs := make([]*job, len(targets))
	for i, t := range targets {
		jobs[i] = &job{Target: t}
	}
	c.prepare(jobs)
	c.run(ctx, jobs)
	results := make([]Result, len(jobs))
	for i, j := range jobs {
		note := j.note
		if j.compactNote != "" && j.result != Failed {
			note = join(note, j.compactNote)
		}
		results[i] = Result{Name: j.Name, Outcome: j.result, Note: note, Compacted: j.compacted}
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
		if j.NoCompact {
			j.phase = phaseExit
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
			j.done(Failed, "the session records could not be read: "+err.Error()+noteUncompacted)
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
		j.done(Failed, "its session record could not be read: "+err.Error()+noteUncompacted)
		return
	case !discover.LaunchedWithBus(rec):
		j.paneWhy = "it was launched without the agent team bus, so its conversation is not recorded"
		return
	}
	if j.file, err = c.Transcript(rec); err != nil {
		j.done(Failed, "its transcript could not be found: "+err.Error()+noteUncompacted)
		return
	}
	last, err := c.Transcripts.CompactedLast(j.file)
	if err != nil {
		j.done(Failed, "its transcript could not be read: "+err.Error()+noteUncompacted)
		return
	}
	if last {
		j.phase = phaseExit
	}
}

// run reads every pane, then again every poll, until each session has ended, been stopped or
// failed.
func (c *Closer) run(ctx context.Context, jobs []*job) {
	start := c.Now()
	for _, j := range jobs {
		j.deadline = start.Add(c.Waits.Prompt)
	}
	for {
		live, err := c.Live(ctx)
		if err != nil {
			for _, j := range jobs {
				if j.result == Pending {
					j.done(Failed, err.Error()+"; the sessions could not be watched any further")
				}
			}
			return
		}
		left := false
		for _, j := range jobs {
			if j.result == Pending {
				c.step(ctx, j, live)
				left = left || j.result == Pending
			}
		}
		if !left {
			return
		}
		c.Sleep(c.Waits.Poll)
	}
}

// step is one look at a session and what follows from it.
func (c *Closer) step(ctx context.Context, j *job, live map[string]bool) {
	if !live[j.Name] {
		c.ended(j)
		return
	}
	pane, err := c.Tmux.CapturePane(ctx, j.Name, true)
	if err != nil {
		// A session that ended since the list was read is seen gone on the next pass. Any other
		// failure kills nothing: the pane may hold unsent text.
		if !strings.Contains(err.Error(), "can't find") {
			j.done(Failed, err.Error()+"; nothing was typed or stopped")
		}
		return
	}
	prefix, _, found := strings.Cut(j.Name, "-")
	if !found {
		prefix = j.Name
	}
	verdict := restore.Judge(prefix, true, screen.Plain(pane), restore.ContainerUp, restore.GoingOutcome{})
	switch verdict.State {
	case restore.StateDead, restore.StateNotStarted:
		c.closeWindow(ctx, j, verdict)
		return
	case restore.StateWaitingAtPrompt:
		if verdict.Detail == composeStop {
			j.done(Exited, noteCompose)
			return
		}
		c.hardStop(ctx, j, "it was at the launcher's "+verdict.Detail+" question, so claude was not running; its tmux session was killed")
		return
	case restore.StateOK, restore.StateStarting, restore.StateSettingGoing, restore.StateNotSetGoing:
	}
	if err = c.look(ctx, j, pane); err != nil {
		j.done(Failed, err.Error())
		return
	}
	if j.result != Pending || c.Now().Before(j.deadline) {
		return
	}
	c.timedOut(ctx, j)
}

// look is what the session's phase and screen call for.
func (c *Closer) look(ctx context.Context, j *job, pane string) error {
	state := screen.ClaudeState(pane)
	switch j.phase {
	case phaseCompact:
		return c.ready(ctx, j, state, CompactText)
	case phaseCompacting:
		return c.compacting(ctx, j, state)
	case phaseExit:
	}
	if err := c.answerBackgroundWork(ctx, j, pane); err != nil {
		return err
	}
	if j.typed && state == screen.Typed && !j.reentered {
		j.reentered = true
		return c.Tmux.SendEnter(ctx, j.Name)
	}
	if j.typed {
		return nil
	}
	return c.ready(ctx, j, state, exitCommand)
}

// ready types line at a ready box, presses Escape once on a busy one, and fails over unsent
// text (C4, C8).
func (c *Closer) ready(ctx context.Context, j *job, state screen.State, line string) error {
	switch state {
	case screen.Ready:
		return c.typeLine(ctx, j, line)
	case screen.Busy:
		if !j.escaped {
			j.escaped = true
			return c.Tmux.SendEscape(ctx, j.Name)
		}
	case screen.Typed:
		j.done(Failed, whyUnsent)
	case screen.NotDrawn:
	}
	return nil
}

// typeLine submits the line, each Enter on its own after the submit pause, checking the box took
// it (tmuxccy.Submit), and starts the wait that follows it.
func (c *Closer) typeLine(ctx context.Context, j *job, line string) error {
	at := c.Now()
	err := c.Tmux.Submit(ctx, j.Name, line, tmuxccy.SubmitPace{Pause: c.Waits.Submit, Sleep: c.Sleep})
	if errors.Is(err, tmuxccy.ErrNotSubmitted) {
		return errors.New(err.Error() + "; it is still running: attach it (ccy-sessions)")
	}
	if err != nil {
		return err
	}
	if line == exitCommand {
		j.typed, j.deadline = true, c.Now().Add(c.Waits.Exit)
		return nil
	}
	j.phase, j.compactAt, j.deadline = phaseCompacting, at, c.Now().Add(c.Waits.Compact)
	j.quiet.Reset()
	return nil
}

// compacting watches a typed /compact until its end (C4-C6).
func (c *Closer) compacting(ctx context.Context, j *job, state screen.State) error {
	now := c.Now()
	if state == screen.Typed {
		if !j.compactReentered {
			j.compactReentered, j.stuckBy = true, now.Add(c.Waits.Settle)
			return c.Tmux.SendEnter(ctx, j.Name)
		}
		if !now.Before(j.stuckBy) {
			j.done(Failed, "its /compact stayed in its input box after a second Enter, so /exit was not typed and it is still running: attach it (ccy-sessions)")
		}
		return nil
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
		j.done(Failed, "its compaction could not be read: "+err.Error()+"; /exit was not typed and it is still running: attach it (ccy-sessions)")
	case done:
		c.compacted(j, noteCompacted)
	case !idle:
		j.boundaryBy = time.Time{}
	case j.boundaryBy.IsZero():
		j.boundaryBy = now.Add(c.Waits.Settle)
	case !now.Before(j.boundaryBy):
		j.done(Failed, "its /compact ended with no compact_boundary in its transcript within "+Seconds(c.Waits.Settle)+
			", so /exit was not typed and it is still running: attach it (ccy-sessions)")
	}
	return nil
}

// compacted moves a session on to its /exit, with a fresh prompt wait.
func (c *Closer) compacted(j *job, note string) {
	j.phase, j.compacted, j.compactNote = phaseExit, true, note
	j.escaped, j.deadline = false, c.Now().Add(c.Waits.Prompt)
}

// timedOut ends a session whose wait is over, saying which wait it was: a compaction is never
// interrupted (C7); any other wait stops the session (C8).
func (c *Closer) timedOut(ctx context.Context, j *job) {
	switch {
	case j.phase == phaseCompacting:
		j.done(Failed, "its /compact did not complete within "+Seconds(c.Waits.Compact)+
			", so /exit was not typed and it is still running: attach it (ccy-sessions)")
	case j.typed:
		late := "it did not end within " + Seconds(c.Waits.Exit) + " of /exit"
		if j.asked && !j.answered {
			late += noteAskedOffExit
		}
		c.hardStop(ctx, j, late+noteKilledSuffix)
	default:
		c.hardStop(ctx, j, "its prompt was not ready within "+Seconds(c.Waits.Prompt)+noteKilledSuffix)
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
	if err := c.Tmux.SendEnter(ctx, j.Name); err != nil {
		return err
	}
	j.answered = true
	j.deadline = c.Now().Add(c.Waits.Exit)
	return nil
}

// ended is a session whose tmux session has gone.
func (c *Closer) ended(j *job) {
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
	if err := c.Tmux.KillSession(ctx, j.Name); err != nil && !errors.Is(err, tmuxccy.ErrSessionGone) {
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
	err := c.Tmux.KillSession(ctx, j.Name)
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
func (q *Quiet) Saw(state screen.State) { q.sawBusy = q.sawBusy || state == screen.Busy }

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
