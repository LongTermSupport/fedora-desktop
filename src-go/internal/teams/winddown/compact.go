package winddown

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/conversation"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/screen"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/transcript"
)

// compactText is what a wound-down session is compacted with (WC2): /compact's own argument,
// one line, the space after the command closing Claude's completion menu as set-going's does.
const compactText = "/compact This session is being wound down before a shutdown, so keep what is needed to " +
	"resume the work later: what was in progress, where each unfinished piece stands, what comes next, " +
	"and the decisions made; drop the rest."

// whyLxcContext is why an LXC member is compacted whatever its context (WC3).
const whyLxcContext = "its transcript is in its container, so its context is not read; the end was read from its pane"

// prepareCompaction finds, before anything is typed, each host session's transcript and the
// compact floor (WC2), and returns the floor as the preview names it ("" when none was read).
// A job that cannot be compacted keeps why, and is still wound down.
func (w *WindDown) prepareCompaction(ctx context.Context, jobs []*job) string {
	var host []*job
	for _, j := range jobs {
		if !j.lxc {
			host = append(host, j)
		}
	}
	if len(host) == 0 {
		return ""
	}
	floor, err := w.Floor(ctx)
	floorTokens, valid := floor.Tokens()
	if err == nil && !valid {
		err = errors.New("the compact floor '" + floor.String() + "' is not a whole number of tokens")
	}
	if err != nil {
		for _, j := range host {
			j.noCompact = err.Error()
		}
		return ""
	}
	store, storeErr := w.Registry()
	for _, j := range host {
		j.floor, j.floorTokens = floor, floorTokens
		if storeErr != nil {
			j.noCompact = "the session records could not be read: " + storeErr.Error()
			continue
		}
		if j.file, err = w.transcriptOf(store, j.name); err != nil {
			j.noCompact = err.Error()
		}
	}
	return floor.String()
}

// transcriptOf is the conversation file a host session is in: the one its SessionStart hook
// recorded, never the newest in a folder several sessions share.
func (w *WindDown) transcriptOf(store *registry.Store, name string) (string, error) {
	rec, err := store.Get(name)
	if err != nil {
		return "", errors.New("its session record could not be read: " + err.Error())
	}
	home := conversation.ClaudeHome(w.Getenv)
	id, err := conversation.For(rec, home)
	if err != nil {
		return "", errors.New(strings.TrimPrefix(err.Error(), rec.Name+": "))
	}
	dir, err := transcript.Dir(rec.Prefix, rec.Dir, home)
	if err != nil {
		return "", err
	}
	return transcript.For(dir, id)
}

// compactStep is one look at a session whose wind-down went in (WC2 to WC4a).
func (w *WindDown) compactStep(ctx context.Context, j *job, state screen.State, now time.Time) error {
	if !now.Before(j.windowBy) {
		w.windowEnded(j)
		return nil
	}
	switch j.phase {
	case phaseAnswer:
		if w.idleAgain(j, state, now) {
			w.answered(j)
		}
	case phaseCompactWait:
		if state == screen.Ready {
			at := w.Now()
			if err := w.typeLine(ctx, j, compactText, phaseCompact); err != nil {
				return err
			}
			j.compactAt, j.sawBusy, j.quietBy = at, false, time.Time{}
		}
	case phaseCompact:
		return w.compacting(ctx, j, state, now)
	case phaseIdle, phaseGoal, phaseText, phaseQueued:
	}
	return nil
}

// answered decides, once the session has answered, whether it is compacted.
func (w *WindDown) answered(j *job) {
	switch {
	case j.noCompact != "":
		j.fail(j.after("it was not compacted: " + j.noCompact))
		return
	case j.lxc:
		j.phase = phaseQueued
		return
	}
	tokens, err := w.Transcripts.ContextTokens(j.file)
	if err != nil {
		j.fail(j.after("it was not compacted: " + err.Error()))
		return
	}
	if tokens < j.floorTokens {
		j.finish(false, "not compacted: its context, "+strconv.FormatInt(tokens, 10)+" tokens, is below the floor, "+j.floor.String()+" tokens")
		return
	}
	j.tokens, j.phase = tokens, phaseQueued
}

// compacting watches a typed /compact: a line left in the box is entered once more; a host
// session is done when its transcript records the compaction, and an LXC member when its pane
// is idle again (WC3).
func (w *WindDown) compacting(ctx context.Context, j *job, state screen.State, now time.Time) error {
	if state == screen.Typed {
		return w.reenter(ctx, j, now, j.after("/compact stayed in its input box after a second Enter"))
	}
	if j.lxc {
		if w.idleAgain(j, state, now) {
			j.finish(true, "compacted ("+whyLxcContext+")")
		}
		return nil
	}
	idle := w.idleAgain(j, state, now)
	done, err := w.Transcripts.CompactedSince(j.file, j.compactAt.Unix())
	switch {
	case err != nil:
		j.fail(j.after("its compaction could not be read: " + err.Error()))
	case done:
		j.finish(true, "compacted (context "+strconv.FormatInt(j.tokens, 10)+" tokens, floor "+j.floor.String()+" tokens)")
	case idle:
		j.fail(j.after("its compaction ended with no compact_boundary in its transcript; look at it (ccy-sessions)"))
	}
	return nil
}

// idleAgain is whether the session is idle and empty again after the phase's line: idle after a
// busy look, or idle for the settle bound when no busy look was caught.
func (w *WindDown) idleAgain(j *job, state screen.State, now time.Time) bool {
	switch state {
	case screen.Ready:
		if j.sawBusy {
			return true
		}
		if j.quietBy.IsZero() {
			j.quietBy = now.Add(w.Waits.Settle)
		}
		return !now.Before(j.quietBy)
	case screen.Busy:
		j.sawBusy = true
	case screen.Typed, screen.NotDrawn:
	}
	j.quietBy = time.Time{}
	return false
}

// windowEnded is a session the window ran out on: reported, not a failure, and not interrupted
// (WC4a).
func (w *WindDown) windowEnded(j *job) {
	window := "at the end of the " + seconds(w.Waits.Window) + " window"
	if j.noCompact != "" {
		j.fail(j.after("it was not compacted: " + j.noCompact))
		return
	}
	switch j.phase {
	case phaseCompact:
		j.finish(false, "its compaction had not finished "+window)
	case phaseQueued, phaseCompactWait:
		j.finish(false, "not compacted: its turn to compact had not come, or its box was not idle, "+window)
	case phaseIdle, phaseGoal, phaseText, phaseAnswer:
		j.finish(false, "not compacted: it was still answering the wind-down "+window)
	}
}
