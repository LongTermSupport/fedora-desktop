package sessions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/screen"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/transcript"
)

// What set-going types. The compact's instruction is /compact's own argument, as the
// supervisor's is; the space after the command also closes Claude's completion menu, so the
// Enter submits the line instead of completing the command name.
const (
	setGoingCompact  = "/compact This session was restored after a reboot. After compacting, carry on with the work that was in progress."
	setGoingContinue = "continue"
)

var wholeTokens = regexp.MustCompile(`^[1-9][0-9]*$`)

// lookKind is what a restored session's pane shows.
type lookKind int

const (
	lookReady lookKind = iota
	lookUnreadable
	lookDead
	lookWaiting
	lookAttached
	lookNotDrawn
	lookBusy
	lookTyped
)

// look is one look at a session's pane: what it shows, and for a dead or waiting pane which.
type look struct {
	kind   lookKind
	detail string
}

// Why a session left alone is an expected outcome rather than a fault.
const (
	whyWaitingAtPrompt = "it waits at a launch prompt only a person can answer (an SSH key or passphrase, a token); answer it in the session"
	whyAttached        = "a person is attached to it, so nothing is typed over them"
	whyTyped           = "its input box holds text a person typed, so nothing is typed over it"
	whyBusy            = "it has taken input since this boot began and is working on it, so there is nothing to set going"
	// A launch prompt after this run typed /compact: the session restarted mid-compaction.
	whyWaitingAfterCompaction = "it restarted during its compaction and now waits at a launch prompt only a person can answer; answer it in the session"
)

// expectedWhy is why leaving a session that looks like this alone is expected: it is a person's
// (at a prompt, attached, text typed). Empty for every other look, which is a fault when the
// session is left alone. Busy is not here: a spinner is no evidence of work (a launch still
// loading, or a hung turn), so setOne asks the transcript instead.
func (l look) expectedWhy() string {
	switch l.kind {
	case lookWaiting:
		return whyWaitingAtPrompt
	case lookAttached:
		return whyAttached
	case lookTyped:
		return whyTyped
	case lookReady, lookUnreadable, lookDead, lookNotDrawn, lookBusy:
	}
	return ""
}

// waitedOn is what a session not yet ready was last waited on: the reason, and why leaving it
// alone at the deadline is expected (empty when that would be a fault).
type waitedOn struct {
	reason string
	why    string
}

// afterCompactionReason is why a session is left alone when it is not ready after its
// compaction.
func (l look) afterCompactionReason() string {
	switch l.kind {
	case lookAttached:
		return "attached-after-compaction"
	case lookTyped:
		return "typed-after-compaction"
	case lookDead, lookWaiting:
		return l.detail + "-after-compaction"
	case lookUnreadable:
		return "unreadable-after-compaction"
	case lookReady, lookNotDrawn, lookBusy:
		return ""
	}
	return ""
}

// goingRun is one set-going: the manifest as it now is, what each session not yet ready was
// waited on, how many were left alone as expected, and each fault ("name (reason)").
type goingRun struct {
	app      *App
	store    *restore.Store
	boot     string
	floor    int64
	manifest restore.Manifest
	waited   map[string]waitedOn
	expected int
	faults   []string
}

// setGoing types each restored session's first input once its prompt has drawn: /compact when
// its context is at or above the floor, else continue. It waits (SetGoingWait at most) for the
// prompts to draw. One set-going runs at a time: a second restore in the same boot starts
// another, which waits here and then finds whatever that restore added still pending.
func (a *App) setGoing(ctx context.Context, args []string) term.ExitCode {
	if len(args) > 0 {
		return a.usageError("'set-going' takes no arguments.")
	}
	regdir, err := registry.Dir(a.Getenv)
	if err != nil {
		a.failErr(err)
		return term.ExitFailure
	}
	store := restore.NewStore(regdir, a.cfg.BootIDFile)
	release, err := store.LockSetGoing(a.cfg.SetGoingWait)
	if err != nil {
		a.failErr(err)
		return term.ExitFailure
	}
	defer func() {
		if releaseErr := release(); releaseErr != nil {
			a.failErr(releaseErr)
		}
	}()
	g := &goingRun{app: a, store: store, waited: map[string]waitedOn{}}
	if err = g.begin(); err != nil {
		return term.ExitFailure
	}
	work := g.workLeft()
	if len(work) == 0 {
		a.sayf("No session this boot's restore started is waiting to be set going.\n")
		return term.ExitOK
	}
	if err = g.setFloor(work); err != nil {
		return term.ExitFailure
	}
	if err = g.loop(ctx, work); err != nil {
		return term.ExitFailure
	}
	return g.verdict()
}

// verdict is the run's exit: a session left alone as expected is reported and is not a
// failure; every fault is named and fails the unit.
func (g *goingRun) verdict() term.ExitCode {
	if g.expected > 0 {
		g.app.sayf("%d restored session(s) were left alone as expected, each named above; that is not a fault.\n", g.expected)
	}
	if len(g.faults) > 0 {
		g.app.fail(fmt.Sprintf("%d restored session(s) could not be set going: %s. verify-restore reports them.", len(g.faults), strings.Join(g.faults, ", ")))
		return term.ExitFailure
	}
	return term.ExitOK
}

// begin reads this boot's manifest. A manifest from an earlier boot says nothing about this one.
func (g *goingRun) begin() error {
	a := g.app
	m, err := g.store.Read()
	if err != nil {
		a.failErr(err)
		a.fail("this boot's restore recorded no sessions, so nothing can be set going; journalctl --user -u ccy-sessions-restore -b --no-pager | cat says why.")
		return errReported
	}
	g.manifest = m
	if g.boot, err = g.store.BootID(); err != nil {
		a.failErr(err)
		return errReported
	}
	if m.Boot != g.boot {
		a.fail("the restore manifest is from an earlier boot: this boot's restore failed before writing one (journalctl --user -u ccy-sessions-restore -b --no-pager | cat), so nothing is set going.")
		return errReported
	}
	return nil
}

// workLeft is the names of the manifest's sessions still to act on: pending or compacting.
func (g *goingRun) workLeft() []string {
	var names []string
	for _, e := range g.manifest.Entries {
		if e.Going == restore.GoingPending || e.Going == restore.GoingCompacting {
			names = append(names, e.Name)
		}
	}
	return names
}

// setFloor reads the compact floor. Without a whole number of tokens no session can be set
// going: each is left alone, with the reason.
func (g *goingRun) setFloor(work []string) error {
	floor, ok := g.app.cfg.CompactFloor.Tokens()
	if ok {
		g.floor = floor
		return nil
	}
	g.app.fail(fmt.Sprintf("CCY_RESTORE_COMPACT_FLOOR_TOKENS is '%s', not a whole number of tokens, so no restored session can be set going. ccy-sessions-set-going.service sets it from ccy_restore_compact_floor_tokens (play-claude-yolo.yml).", g.app.cfg.CompactFloor))
	if leaveErr := g.leaveAll(work, "no-compact-floor"); leaveErr != nil {
		return leaveErr
	}
	return errReported
}

// leaveAll leaves every named session alone for the same reason, as the entry now stands.
func (g *goingRun) leaveAll(names []string, reason string) error {
	for _, name := range names {
		i := g.manifest.Index(name)
		if i < 0 {
			continue
		}
		if err := g.leaveAlone(name, g.manifest.Entries[i].Going, reason); err != nil {
			return err
		}
	}
	return nil
}

// loop looks at the sessions again and again until each is set going or left alone, or the
// deadline passes.
func (g *goingRun) loop(ctx context.Context, work []string) error {
	a := g.app
	deadline := a.Now().Add(a.cfg.SetGoingWait)
	for {
		live, attached, err := g.liveSessions(ctx)
		if err != nil {
			a.failErr(err)
			a.fail("the live session list could not be read, so no further session can be set going.")
			if leaveErr := g.leaveAll(work, "session-list-unreadable"); leaveErr != nil {
				return leaveErr
			}
			return errReported
		}
		if err = g.pass(ctx, work, live, attached); err != nil {
			return err
		}
		if g.manifest, err = g.store.Read(); err != nil {
			a.failErr(err)
			return errReported
		}
		if work = g.workLeft(); len(work) == 0 {
			return nil
		}
		if !a.Now().Before(deadline) {
			return g.giveUp(work)
		}
		a.Pause(a.cfg.Poll, nil)
	}
}

// giveUp leaves every session still waiting alone, naming what it waited on. One still at a
// launch prompt is a person's to answer, as expected; any other wait that never ended is a fault.
func (g *goingRun) giveUp(work []string) error {
	waitedFor := strconv.FormatFloat(g.app.cfg.SetGoingWait.Seconds(), 'f', -1, 64)
	for _, name := range work {
		i := g.manifest.Index(name)
		if i < 0 {
			continue
		}
		going := g.manifest.Entries[i].Going
		w := waitedOn{reason: "prompt-not-drawn"}
		if going == restore.GoingCompacting {
			w.reason = "compaction-not-finished"
		}
		if g.waited[name].reason != "" {
			w = g.waited[name]
		}
		if err := g.leave(name, going, w.reason+"-after-"+waitedFor+"s", w.why); err != nil {
			return err
		}
	}
	return nil
}

// liveSessions is which sessions are live and how many terminals are on each.
func (g *goingRun) liveSessions(ctx context.Context) (map[string]bool, map[string]int, error) {
	sessions, err := g.app.Tmux.List(ctx)
	if err != nil {
		return nil, nil, err
	}
	live, attached := map[string]bool{}, map[string]int{}
	for _, s := range sessions {
		live[s.Name], attached[s.Name] = true, s.Attached
	}
	return live, attached, nil
}

// pass acts once on each session still to go.
func (g *goingRun) pass(ctx context.Context, work []string, live map[string]bool, attached map[string]int) error {
	for _, name := range work {
		i := g.manifest.Index(name)
		if i < 0 {
			continue
		}
		var err error
		switch g.manifest.Entries[i].Going {
		case restore.GoingPending:
			err = g.setOne(ctx, name, live[name], attached[name])
		case restore.GoingCompacting:
			err = g.continueAfterCompaction(ctx, name, live[name], attached[name])
		case restore.GoingNone, restore.GoingCompact, restore.GoingContinue, restore.GoingUntouched, restore.GoingKeep:
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// goingSet records what was done with one session, under the manifest's lock, only while its
// entry still says expect. Afterwards the manifest is read again as it now is.
func (g *goingRun) goingSet(name string, expect, going restore.Going, at time.Time, detail, transcriptPath string) error {
	a := g.app
	err := g.store.Update(g.boot, name, expect, restore.Change{Going: going, At: at, Detail: detail, Transcript: transcriptPath})
	switch {
	case err == nil:
	case errors.Is(err, restore.ErrNoLongerApplies):
		a.sayf("%s: its entry in the restore manifest changed while it was being set going (a second restore this boot); that entry stands.\n", name)
	default:
		a.failErr(err)
		a.fail("the restore manifest could not be updated for " + name + ".")
		return errReported
	}
	m, readErr := g.store.Read()
	if readErr != nil {
		a.failErr(readErr)
		return errReported
	}
	g.manifest = m
	return nil
}

// leaveAlone is set-going not touching the session because something went wrong: a fault,
// recorded as untouched with the reason, named on stderr and in the run's failure.
func (g *goingRun) leaveAlone(name string, expect restore.Going, reason string) error {
	g.faults = append(g.faults, name+" ("+reason+")")
	g.app.sayf("left %s untouched: %s\n", name, reason)
	return g.goingSet(name, expect, restore.GoingUntouched, time.Time{}, reason, "")
}

// leaveAsExpected is set-going not touching the session because that is right (a person has it,
// or must answer it, or it needs nothing): recorded as untouched with the reason, and named on
// stderr with why. It does not fail the run.
func (g *goingRun) leaveAsExpected(name string, expect restore.Going, reason, why string) error {
	g.expected++
	g.app.sayf("left %s alone, as expected: %s; %s\n", name, reason, why)
	return g.goingSet(name, expect, restore.GoingUntouched, time.Time{}, reason, "")
}

// leave leaves the session alone as expected when there is a why, else as a fault.
func (g *goingRun) leave(name string, expect restore.Going, reason, why string) error {
	if why != "" {
		return g.leaveAsExpected(name, expect, reason, why)
	}
	return g.leaveAlone(name, expect, reason)
}

// typeInto types the text into the session's pane, then presses Enter on its own after the
// submit delay, the way the supervisor submits a line: an Enter arriving with the text is read
// as part of it.
func (a *App) typeInto(ctx context.Context, name, text string) error {
	if err := a.Tmux.SendKeys(ctx, name, text); err != nil {
		a.failErr(err)
		return err
	}
	a.Pause(a.cfg.SubmitDelay, nil)
	if err := a.Tmux.SendEnter(ctx, name); err != nil {
		a.failErr(err)
		return err
	}
	return nil
}

// lookAt is what the session's pane shows.
func (g *goingRun) lookAt(ctx context.Context, name, prefix string, attached int) look {
	a := g.app
	pane, err := a.Tmux.CapturePane(ctx, name, true)
	if err != nil {
		a.failErr(err)
		return look{kind: lookUnreadable}
	}
	verdict := restore.Judge(prefix, true, screen.Plain(pane), restore.ContainerUp, restore.GoingOutcome{})
	switch verdict.State {
	case restore.StateDead:
		return look{kind: lookDead, detail: verdict.Detail}
	case restore.StateWaitingAtPrompt:
		return look{kind: lookWaiting, detail: verdict.Detail}
	case restore.StateOK, restore.StateStarting, restore.StateSettingGoing, restore.StateNotSetGoing:
		// The pane is neither dead nor at a prompt; what it is doing is read below.
	}
	// A person attached is at the keyboard: whatever they are doing, nothing is typed over it.
	if attached > 0 {
		return look{kind: lookAttached}
	}
	switch screen.ClaudeState(pane) {
	case screen.Ready:
		return look{kind: lookReady}
	case screen.Busy:
		return look{kind: lookBusy}
	case screen.Typed:
		return look{kind: lookTyped}
	case screen.NotDrawn:
	}
	return look{kind: lookNotDrawn}
}

// conversationOwner is the other session already set going on this conversation. Two sessions
// in one project that both --continue resume the same conversation: only one may drive it.
func (g *goingRun) conversationOwner(file, name string) string {
	for _, e := range g.manifest.Entries {
		if e.Name == name || e.Transcript != file {
			continue
		}
		switch e.Going {
		case restore.GoingCompacting, restore.GoingCompact, restore.GoingContinue:
			return e.Name
		case restore.GoingNone, restore.GoingPending, restore.GoingUntouched, restore.GoingKeep:
		}
	}
	return ""
}

// claudeHome is where cc keeps its transcripts.
func (a *App) claudeHome() string {
	if dir := a.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	return a.Getenv("HOME") + "/.claude"
}

// setOne sets one pending session going once its prompt has drawn: /compact at or above the
// floor (followed by continue when it has no supervisor, see continueAfterCompaction), else
// continue. It is left pending while there is something to wait for; anything that cannot be
// read or sent leaves it untouched, with the reason.
func (g *goingRun) setOne(ctx context.Context, name string, live bool, attached int) error {
	i := g.manifest.Index(name)
	if i < 0 {
		return nil
	}
	e := g.manifest.Entries[i]
	if !live {
		return g.leaveAlone(name, restore.GoingPending, "session-not-running")
	}
	l := g.lookAt(ctx, name, e.Prefix, attached)
	if reason, wait, ready := g.notReady(l); !ready && l.kind != lookBusy {
		if wait {
			g.waited[name] = waitedOn{reason: reason, why: l.expectedWhy()}
			return nil
		}
		return g.leave(name, restore.GoingPending, reason, l.expectedWhy())
	}
	file, err := g.transcriptOf(e)
	if err != nil {
		g.app.failErr(err)
		return g.leaveAlone(name, restore.GoingPending, "no-transcript")
	}
	if l.kind == lookBusy {
		return g.judgeBusy(name, file)
	}
	if owner := g.conversationOwner(file, name); owner != "" {
		return g.leaveAsExpected(name, restore.GoingPending, "shares-conversation-with-"+owner,
			"only one session may drive a conversation, and "+owner+" is set going on it")
	}
	tokens, err := g.app.Transcripts.ContextTokens(file)
	if err != nil {
		g.app.failErr(err)
		return g.leaveAlone(name, restore.GoingPending, "context-unreadable")
	}
	going, text := restore.GoingContinue, setGoingContinue
	if tokens >= g.floor {
		going, text = restore.GoingCompact, setGoingCompact
		// Nothing carries a session with no supervisor on after its compaction: it waits as
		// compacting, and continueAfterCompaction types continue once the compaction ends.
		if !e.Supervised {
			going = restore.GoingCompacting
		}
	}
	// Taken before typing, so whatever the input writes to the transcript is at or after it.
	at := g.app.Now()
	if err = g.app.typeInto(ctx, name, text); err != nil {
		return g.leaveAlone(name, restore.GoingPending, "send-failed")
	}
	g.app.sayf("set %s going: %s (context %d tokens, compact floor %s, supervised: %s).\n",
		name, going, tokens, g.app.cfg.CompactFloor, term.YesNo(e.Supervised))
	return g.goingSet(name, restore.GoingPending, going, at, strconv.FormatInt(tokens, 10), file)
}

// judgeBusy decides a pending session showing a spinner by what its transcript shows. Input
// taken since this boot began means it is working: left alone, as expected. None means the
// spinner is a launch still loading or a hung turn: waited on, so an idle prompt is set going
// on a later pass and one still busy at the deadline is a fault.
func (g *goingRun) judgeBusy(name, file string) error {
	since, err := g.bootStart()
	if err != nil {
		g.app.failErr(err)
		return g.leaveAlone(name, restore.GoingPending, "boot-time-unreadable")
	}
	took, err := g.app.Transcripts.TookInputSince(file, since)
	if err != nil {
		g.app.failErr(err)
		return g.leaveAlone(name, restore.GoingPending, "transcript-unreadable")
	}
	if took {
		return g.leaveAsExpected(name, restore.GoingPending, "busy-before-set-going", whyBusy)
	}
	g.waited[name] = waitedOn{reason: "busy-without-input"}
	return nil
}

// bootStart is when this boot began (Unix seconds, btime in /proc/stat): the earliest moment a
// restored session can have taken input, so input at or after it is work since the restore and
// input before it is the conversation's past.
func (g *goingRun) bootStart() (int64, error) {
	path := g.app.cfg.ProcStatFile
	if path == "" {
		path = "/proc/stat"
	}
	unreadable := errors.New("this boot's start time could not be read from " + path)
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return 0, fmt.Errorf("%w: %w", unreadable, err)
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		if value, found := strings.CutPrefix(line, "btime "); found {
			start, convErr := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if convErr != nil || start <= 0 {
				return 0, fmt.Errorf("%w: btime %q is not a time", unreadable, value)
			}
			return start, nil
		}
	}
	return 0, fmt.Errorf("%w: it has no btime line", unreadable)
}

// notReady says why a pending session cannot be typed into yet: reason, and whether it is only
// worth waiting for (wait), or done when it is ready.
func (g *goingRun) notReady(l look) (string, bool, bool) {
	switch l.kind {
	case lookReady:
		return "", false, true
	case lookUnreadable:
		return "screen-unreadable", false, false
	case lookDead:
		return l.detail, false, false
	case lookWaiting:
		return "waiting-at-prompt-" + l.detail, true, false
	case lookNotDrawn:
		return "prompt-not-drawn", true, false
	case lookAttached:
		return "attached", false, false
	case lookBusy:
		return "busy-before-set-going", false, false
	case lookTyped:
		return "input-box-not-empty", false, false
	}
	return "", false, true
}

// transcriptOf is the conversation file the session resumes: the --resume'd one, else the
// newest.
func (g *goingRun) transcriptOf(e restore.Entry) (string, error) {
	dir, err := transcript.Dir(e.Prefix, e.Dir, g.app.claudeHome())
	if err != nil {
		return "", fmt.Errorf("transcript directory: %w", err)
	}
	file, err := transcript.For(dir, e.Resume)
	if err != nil {
		return "", fmt.Errorf("transcript: %w", err)
	}
	return file, nil
}

// continueAfterCompaction is for a session with no supervisor that was typed /compact: once the
// transcript shows the compaction finished, it types continue.
func (g *goingRun) continueAfterCompaction(ctx context.Context, name string, live bool, attached int) error {
	i := g.manifest.Index(name)
	if i < 0 {
		return nil
	}
	e := g.manifest.Entries[i]
	if !live {
		return g.leaveAlone(name, restore.GoingCompacting, "session-ended-while-compacting")
	}
	finished, err := g.app.Transcripts.CompactedSince(e.Transcript, e.AtUnix())
	if err != nil {
		g.app.failErr(err)
		return g.leaveAlone(name, restore.GoingCompacting, "context-unreadable")
	}
	if !finished {
		g.waited[name] = waitedOn{reason: "compaction-not-finished"}
		return nil
	}
	l := g.lookAt(ctx, name, e.Prefix, attached)
	switch l.kind {
	case lookReady:
		return g.typeContinue(ctx, e)
	case lookBusy:
		// Something has already set it working after the compaction; that is the point.
		g.app.sayf("%s is already working after its compaction; nothing more typed.\n", name)
		return g.goingSet(name, restore.GoingCompacting, restore.GoingCompact, e.At, e.Detail, e.Transcript)
	case lookNotDrawn:
		g.waited[name] = waitedOn{reason: "prompt-not-back-after-compaction"}
		return nil
	case lookWaiting:
		return g.leaveAsExpected(name, restore.GoingCompacting, l.afterCompactionReason(), whyWaitingAfterCompaction)
	case lookAttached, lookTyped, lookDead, lookUnreadable:
		return g.leave(name, restore.GoingCompacting, l.afterCompactionReason(), l.expectedWhy())
	}
	return nil
}

// typeContinue types continue into a session whose compaction has finished.
func (g *goingRun) typeContinue(ctx context.Context, e restore.Entry) error {
	at := g.app.Now()
	if err := g.app.typeInto(ctx, e.Name, setGoingContinue); err != nil {
		return g.leaveAlone(e.Name, restore.GoingCompacting, "send-failed-after-compaction")
	}
	g.app.sayf("set %s going after its compaction: continue.\n", e.Name)
	return g.goingSet(e.Name, restore.GoingCompacting, restore.GoingContinue, at, e.Detail, e.Transcript)
}
